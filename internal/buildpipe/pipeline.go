// Package buildpipe orchestrates the full Pass 1..4 build (spec §4.7):
// detect → parse → resolve → graph build/validate → cluster → score → persist.
// Three routing paths: cold rebuild, short-circuit (all-cached), and incremental
// (partial-hit — reuse cached files, re-parse dirty). See Run for routing logic.
package buildpipe

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/internal/detect"
	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/link"
	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	solp "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/internal/score"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// emitDerivedPasses runs the post-graph.Build derived passes against g IN
// MEMORY: cross-language link (Sol→TS binds_to), G6 Temporal (commit nodes
// + changed_in/blame), cluster (pkg + topic), and score. Returns the cluster
// outputs so the caller can persist them.
//
// Both runCold and the partial-cache rebuild path call this — the v2 bug
// (temporal emitting to g but never persisting under incremental) is
// structurally impossible because both paths feed the same g into the same
// downstream persist step.
//
// solParser nil = skip xlang. Cold passes the parser when Sol files exist;
// incremental passes nil when no TS/Sol file is dirty (the cached binds_to
// edges are reloaded directly into g instead). DB-side drops for temporal
// and binds_to are the caller's responsibility (cold wipes everything via
// openColdStore; incremental issues targeted DeleteEdgesByType).
func emitDerivedPasses(g *graph.Graph, srcRoot string, solParser *solp.Parser,
	log *slog.Logger) (*cluster.PkgTree, *cluster.TopicTree, error) {
	if solParser != nil {
		abi := convertABI(solParser.ABI())
		xlEdges := link.SolToTS(g.Nodes, abi)
		g.Edges = append(g.Edges, xlEdges...)
		if err := graph.Validate(g); err != nil {
			return nil, nil, fmt.Errorf("validate after xlang: %w", err)
		}
		log.Info("xlang linked", "binds_to", len(xlEdges))
	}
	if err := emitTemporalEdges(g, srcRoot, log, 0); err != nil {
		return nil, nil, fmt.Errorf("temporal: %w", err)
	}
	if err := graph.Validate(g); err != nil {
		return nil, nil, fmt.Errorf("validate after temporal: %w", err)
	}
	pkgTree := cluster.BuildPkgTree(g)
	topicTree := cluster.BuildTopicTree(g, []float64{0.5, 1.0, 2.0}, 42)
	score.Compute(g)
	return pkgTree, topicTree, nil
}

// Options controls one ckg build invocation.
type Options struct {
	SrcRoot    string
	OutDir     string
	Languages  []string // {"auto"} | subset of {"go","ts","sol"}
	Logger     *slog.Logger
	CKGVersion string
	// NoCache forces a full rebuild — bypasses the A3 incremental cache and
	// wipes graph.db at start. Use when the cache is suspect, or for clean
	// benchmark runs.
	NoCache bool
	// RebuildMetrics forces PageRank/Leiden recompute even when the cache
	// would otherwise reuse them. Phase 1 ALWAYS recomputes when any file
	// is dirty (see Run below) — this flag is the explicit operator escape
	// for the "all-cached but I want fresh metrics" case.
	RebuildMetrics bool
	// DBDSN is an optional PostgreSQL DSN (e.g. "postgres://user:pass@host/db").
	// When set, the build persists to PostgreSQL instead of a local SQLite file.
	// OutDir is still used for manifest.json; --no-cache and incremental work the
	// same way (NodesByFilePath reads from PG with ORDER BY start_line).
	DBDSN string
}

// Run executes the full pipeline. Side effects: writes OutDir/graph.db
// and OutDir/manifest.json. Returns the persisted Manifest summary so the
// caller can print stats without re-reading SQLite.
//
// Cache routing (A3 Phase 1):
//   - --no-cache OR no prior manifest OR schema/version mismatch → cold rebuild
//   - all-cached AND no removals → short-circuit (timestamp refresh only)
//   - mixed dirty/cached → incremental (parse only dirty, reuse cached node sets)
func Run(opt Options) (persist.Manifest, error) {
	log := opt.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	if err := os.MkdirAll(opt.OutDir, 0o755); err != nil {
		return persist.Manifest{}, fmt.Errorf("mkdir out: %w", err)
	}

	// (1) detect — discovery is shared by all three paths.
	// TS/Sol use extension-based discovery (detect.Walk); Go uses
	// go/packages.Load (detect.GoFiles) to honor build constraints. See
	// pipeline_test.go for the 41-file drift this eliminates.
	log.Debug("discovery.start", "src", opt.SrcRoot)
	discovery, _, goCount, tsCount, solCount, err := discoveryAll(opt.SrcRoot, opt.Languages)
	if err != nil {
		return persist.Manifest{}, err
	}
	log.Info("detected files", "go", goCount, "ts", tsCount, "sol", solCount)
	log.Debug("discovery.end", "total", goCount+tsCount+solCount)

	// (2) cache routing — three paths (G6 v4, schema 1.5):
	//
	//   - runShortCircuit: 100% cache hit, no removals (manifest timestamp
	//     refresh only). Load-bearing for CI re-runs on unchanged source.
	//   - runIncremental: partial-hit — parse only dirty files, reload cached
	//     nodes in declaration order (NodesByFilePath ORDER BY start_line —
	//     G6 v4 fix for H3 root cause), merge + rerun Pass 2.
	//   - runCold: --no-cache, missing manifest, schema/version mismatch.
	dbPath := filepath.Join(opt.OutDir, "graph.db")
	old := readOldManifestFromDB(dbPath, opt.DBDSN)
	if !opt.NoCache && ManifestUsable(old, opt.CKGVersion) {
		decisions, derr := DiffManifest(opt.SrcRoot, discovery, old, opt.CKGVersion)
		if derr != nil {
			return persist.Manifest{}, fmt.Errorf("cache diff: %w", derr)
		}
		if decisions.IsAllCached() {
			return runShortCircuit(opt, log, decisions, old, goCount, tsCount, solCount)
		}
		return runIncremental(opt, log, decisions, goCount, tsCount, solCount)
	}
	if opt.NoCache {
		log.Info("Cache: bypassed (--no-cache); full rebuild")
	}
	return runCold(opt, log, discovery)
}

// runCold is the V0-equivalent full-rebuild path: wipe DB, parse every file,
// rebuild every artifact. Always emits a fresh manifest (with Files block).
func runCold(opt Options, log *slog.Logger,
	discovery []DiscoveredFile) (persist.Manifest, error) {
	files, err := detect.Walk(opt.SrcRoot)
	if err != nil {
		return persist.Manifest{}, fmt.Errorf("detect: %w", err)
	}
	goFiles, err := detect.GoFiles(opt.SrcRoot)
	if err != nil {
		return persist.Manifest{}, fmt.Errorf("detect go: %w", err)
	}

	// (2)+(3) parse + link, per language
	resolved := []*parse.ResolvedGraph{}
	allPending := []persist.PendingRefRow{}
	parseErrs := 0
	if shouldRun("go", opt.Languages) && len(goFiles) > 0 {
		log.Debug("pass1.start", "language", "go", "files", len(goFiles))
		rg, pending, n, err := runGoPipeline(opt.SrcRoot, goFiles, log)
		if err != nil {
			return persist.Manifest{}, fmt.Errorf("go pipeline: %w", err)
		}
		parseErrs += n
		resolved = append(resolved, rg)
		allPending = append(allPending, pending...)
		log.Debug("pass1.end", "language", "go", "nodes", len(rg.Nodes), "errs", n)
	}
	// solParser is retained across the language passes so that the
	// cross-language linker (T20) can read Solidity ABI sigs after graph.Build.
	// nil signals "no Sol pipeline ran" — xlang stage is skipped in that case.
	var solParser *solp.Parser
	if shouldRun("ts", opt.Languages) && len(files.TS) > 0 {
		log.Debug("pass1.start", "language", "ts", "files", len(files.TS))
		rg, pending, n, err := runTSPipeline(opt.SrcRoot, files.TS, log)
		if err != nil {
			return persist.Manifest{}, fmt.Errorf("ts pipeline: %w", err)
		}
		parseErrs += n
		resolved = append(resolved, rg)
		allPending = append(allPending, pending...)
		log.Debug("pass1.end", "language", "ts", "nodes", len(rg.Nodes), "errs", n)
	}
	if shouldRun("sol", opt.Languages) && len(files.Sol) > 0 {
		log.Debug("pass1.start", "language", "sol", "files", len(files.Sol))
		rg, pending, n, p, err := runSolPipeline(opt.SrcRoot, files.Sol, log)
		if err != nil {
			return persist.Manifest{}, fmt.Errorf("sol pipeline: %w", err)
		}
		parseErrs += n
		solParser = p
		resolved = append(resolved, rg)
		allPending = append(allPending, pending...)
		log.Debug("pass1.end", "language", "sol", "nodes", len(rg.Nodes), "errs", n)
	}

	// (4) graph build + validate
	log.Debug("pass2.resolve.start", "pending_refs", len(allPending))
	g, err := graph.Build(resolved)
	if err != nil {
		return persist.Manifest{}, fmt.Errorf("graph.Build: %w", err)
	}
	if err := graph.Validate(g); err != nil {
		return persist.Manifest{}, fmt.Errorf("graph.Validate: %w", err)
	}
	log.Debug("pass2.resolve.end", "nodes", len(g.Nodes), "edges", len(g.Edges))

	// (4b/4c/5/6) derived passes — xlang, temporal, cluster, score. Shared
	// helper so the partial-cache rebuild path runs identical in-memory
	// transformations (G6 v3 § 4.4). v2's "emitted-vs-DB 0" bug was caused
	// by temporal living only in cold; the helper makes that recurrence
	// structurally impossible.
	log.Debug("metrics.start")
	pkgTree, topicTree, err := emitDerivedPasses(g, opt.SrcRoot, solParser, log)
	if err != nil {
		return persist.Manifest{}, err
	}
	log.Debug("metrics.end")

	// (7) persist — cold rebuild wipes graph.db so we don't accumulate stale
	// rows. Incremental path lives in incremental.go and reuses prior rows.
	log.Debug("persist.start", "nodes", len(g.Nodes), "edges", len(g.Edges))
	store, err := openColdStore(opt.OutDir, opt.DBDSN)
	if err != nil {
		return persist.Manifest{}, err
	}
	defer store.Close()
	if err := persistColdArtifacts(store, opt.SrcRoot, g, pkgTree, topicTree); err != nil {
		return persist.Manifest{}, err
	}
	// G6 v3 (schema 1.5): persist Pass 1 pending refs so the next partial
	// build can replay Pass 2 over a merged dirty + cached input set without
	// re-parsing cached files. INSERT after persistColdArtifacts so node FKs
	// are satisfied. The cold path always wipes graph.db beforehand
	// (openColdStore.os.Remove), so the table starts empty — IGNORE on the PK
	// in InsertPendingRefs handles the rare emit-twice case.
	if err := store.InsertPendingRefs(allPending); err != nil {
		return persist.Manifest{}, fmt.Errorf("persist pending_refs: %w", err)
	}
	log.Debug("persist.end")

	m := buildManifestSkeleton(opt, len(goFiles), len(files.TS), len(files.Sol),
		g, pkgTree, parseErrs)
	// Files: every discovered file becomes an entry. This is the cache
	// fingerprint that subsequent builds will diff against. We computed
	// SHAs / cache_keys lazily here — once per cold build, so the cost
	// is amortised against the parse pass.
	m.Files = computeColdFileEntries(opt.SrcRoot, opt.CKGVersion, discovery, g.Nodes, g.Edges)
	setStaleness(&m, log)
	if err := store.SetManifest(m); err != nil {
		return persist.Manifest{}, err
	}
	if err := writeManifestJSON(filepath.Join(opt.OutDir, "manifest.json"), m); err != nil {
		return persist.Manifest{}, err
	}
	log.Info("build complete",
		"nodes", len(g.Nodes), "edges", len(g.Edges),
		"pkg_tree_edges", len(pkgTree.Edges),
		"topic_resolutions", len(topicTree.Resolutions))
	return m, nil
}

// openColdStore wipes the backing store and re-opens it for a cold rebuild.
// When dbDsn is set, the store is a PostgreSQL database (wipe via TRUNCATE);
// otherwise it is a local SQLite file (wipe via os.Remove).
func openColdStore(outDir, dbDsn string) (persist.Store, error) {
	if dbDsn != "" {
		return persist.OpenPostgresCold(dbDsn)
	}
	dbPath := filepath.Join(outDir, "graph.db")
	_ = os.Remove(dbPath)
	store, err := persist.Open(dbPath)
	if err != nil {
		return nil, err
	}
	if err := store.Migrate(); err != nil {
		store.Close()
		return nil, err
	}
	return store, nil
}

// openStore opens the backing store for read/write (incremental / short-circuit
// paths). When dbDsn is set, routes to PostgreSQL; otherwise SQLite.
func openStore(outDir, dbDsn string) (persist.Store, error) {
	if dbDsn != "" {
		return persist.OpenPostgres(dbDsn)
	}
	return persist.Open(filepath.Join(outDir, "graph.db"))
}

// persistColdArtifacts performs the bulk-insert phase of a cold rebuild.
// All inserts are unconditional — the DB was just wiped by openColdStore.
func persistColdArtifacts(store persist.Store, srcRoot string,
	g *graph.Graph, pkgTree *cluster.PkgTree, topicTree TopicTreeForPersist) error {
	if err := store.InsertNodes(g.Nodes); err != nil {
		return err
	}
	if err := store.InsertEdges(g.Edges); err != nil {
		return err
	}
	if err := store.InsertPkgTreeFromCluster(pkgTree.PersistEdges()); err != nil {
		return err
	}
	if err := store.InsertTopicTree(topicTree); err != nil {
		return err
	}
	if err := store.InsertBlobs(extractBlobs(srcRoot, g.Nodes)); err != nil {
		return err
	}
	return store.RebuildFTS()
}

// computeColdFileEntries hashes every discovered file and returns FileEntry
// records for the new manifest. Called on cold rebuild so the next build can
// diff against this baseline. EdgeIDs are int64 PRIMARY KEY values assigned
// by the AUTOINCREMENT INSERT just performed.
func computeColdFileEntries(srcRoot, ckgVersion string, discovery []DiscoveredFile, nodes []types.Node, edges []types.Edge) []persist.FileEntry {
	nodesByPath := map[string][]string{}
	for _, n := range nodes {
		if n.FilePath == "" {
			continue
		}
		nodesByPath[n.FilePath] = append(nodesByPath[n.FilePath], n.ID)
	}
	edgesByPath := map[string][]int64{}
	for _, e := range edges {
		if e.FilePath == "" {
			continue
		}
		edgesByPath[e.FilePath] = append(edgesByPath[e.FilePath], e.ID)
	}
	out := make([]persist.FileEntry, 0, len(discovery))
	for _, df := range discovery {
		full := filepath.Join(srcRoot, filepath.FromSlash(df.Path))
		content, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		st, _ := os.Stat(full)
		var mtime int64
		if st != nil {
			mtime = st.ModTime().UnixNano()
		}
		parserVer := parserVersionFor(df.Language)
		out = append(out, persist.FileEntry{
			Path:          df.Path,
			Language:      df.Language,
			SHA256:        SHA256Hex(content),
			CacheKey:      ComputeCacheKey(content, ckgVersion, parserVer),
			MTime:         mtime,
			ParserVersion: parserVer,
			NodeIDs:       nodesByPath[df.Path],
			EdgeIDs:       edgesByPath[df.Path],
		})
	}
	return out
}

// shouldRun returns true when lang is requested explicitly or via the "auto"
// catch-all in opts.
func shouldRun(lang string, opts []string) bool {
	for _, l := range opts {
		if l == "auto" || l == lang {
			return true
		}
	}
	return false
}

// extractBlobs reads every node's source slice (StartByte..EndByte) into a
// per-node blob, caching file contents to amortize IO. Package nodes are
// skipped (they have no syntactic body) and offsets are bounds-checked
// defensively to avoid panics on malformed nodes.
func extractBlobs(root string, nodes []types.Node) map[string][]byte {
	blobs := map[string][]byte{}
	cache := map[string][]byte{}
	for _, n := range nodes {
		if n.Type == types.NodePackage {
			continue
		}
		full := filepath.Join(root, n.FilePath)
		src, ok := cache[full]
		if !ok {
			b, err := os.ReadFile(full)
			if err != nil {
				continue
			}
			cache[full] = b
			src = b
		}
		if n.StartByte < 0 || n.EndByte > len(src) || n.StartByte >= n.EndByte {
			continue
		}
		blobs[n.ID] = append([]byte(nil), src[n.StartByte:n.EndByte]...)
	}
	return blobs
}

// writeManifestJSON pretty-prints the manifest to path for human inspection.
func writeManifestJSON(path string, m persist.Manifest) error {
	buf, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return os.WriteFile(path, buf, 0o644)
}
