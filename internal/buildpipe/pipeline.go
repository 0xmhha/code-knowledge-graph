// Package buildpipe orchestrates the full Pass 1..4 build (spec §4.7):
// detect → parse → resolve → graph build/validate → cluster → score → persist.
// V0 supports a full rebuild only — incremental updates are not wired here.
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
	discovery, _, goCount, tsCount, solCount, err := discoveryAll(opt.SrcRoot, opt.Languages)
	if err != nil {
		return persist.Manifest{}, err
	}
	log.Info("detected files", "go", goCount, "ts", tsCount, "sol", solCount)

	// (2) cache routing — three paths (G6 v3, schema 1.5):
	//
	//   - runShortCircuit: 100% cache hit, no removals (manifest timestamp
	//     refresh only). Load-bearing for CI re-runs on unchanged source.
	//   - runIncremental: DEAD CODE (D4 escape hatch executed 2026-05-04).
	//     Partial-hit cases fall back to cold rebuild for correctness. See
	//     comment below. Preserved for future v4 attempt (B3 tree-sitter
	//     Tree.Edit or C1 reverse-reference index as prerequisite).
	//   - runCold: --no-cache, missing manifest, schema/version mismatch,
	//     OR partial hit (post-D4 fallback).
	dbPath := filepath.Join(opt.OutDir, "graph.db")
	old := readOldManifestFromDB(dbPath)
	if !opt.NoCache && ManifestUsable(old, opt.CKGVersion) {
		decisions, derr := DiffManifest(opt.SrcRoot, discovery, old, opt.CKGVersion)
		if derr != nil {
			return persist.Manifest{}, fmt.Errorf("cache diff: %w", derr)
		}
		if decisions.IsAllCached() {
			return runShortCircuit(opt, log, decisions, old, goCount, tsCount, solCount)
		}
		// partial hit → cold fallback for correctness (D4 escape hatch executed
		// 2026-05-04). Root cause: NodesByFilePath returns nodes in DB rowid
		// order, not AST declaration order; ambiguous qname resolution produces
		// different Dst nodes between cold and partial paths (+2675 edge
		// over-emit on go-stablenet, § 7.1 FAIL). runIncremental and
		// pending_refs infra are preserved as dead code for future v4 attempt.
		// Fix direction: sort NodesByFilePath by start_line ASC.
		log.Info("Cache: partial hit; falling back to cold rebuild for correctness")
		return runCold(opt, log, discovery, goCount, tsCount, solCount)
	}
	if opt.NoCache {
		log.Info("Cache: bypassed (--no-cache); full rebuild")
	}
	return runCold(opt, log, discovery, goCount, tsCount, solCount)
}

// runCold is the V0-equivalent full-rebuild path: wipe DB, parse every file,
// rebuild every artifact. Always emits a fresh manifest (with Files block).
func runCold(opt Options, log *slog.Logger,
	discovery []DiscoveredFile, goCount, tsCount, solCount int) (persist.Manifest, error) {
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
		rg, pending, n, err := runGoPipeline(opt.SrcRoot, goFiles, log)
		if err != nil {
			return persist.Manifest{}, fmt.Errorf("go pipeline: %w", err)
		}
		parseErrs += n
		resolved = append(resolved, rg)
		allPending = append(allPending, pending...)
	}
	// solParser is retained across the language passes so that the
	// cross-language linker (T20) can read Solidity ABI sigs after graph.Build.
	// nil signals "no Sol pipeline ran" — xlang stage is skipped in that case.
	var solParser *solp.Parser
	if shouldRun("ts", opt.Languages) && len(files.TS) > 0 {
		rg, pending, n, err := runTSPipeline(opt.SrcRoot, files.TS, log)
		if err != nil {
			return persist.Manifest{}, fmt.Errorf("ts pipeline: %w", err)
		}
		parseErrs += n
		resolved = append(resolved, rg)
		allPending = append(allPending, pending...)
	}
	if shouldRun("sol", opt.Languages) && len(files.Sol) > 0 {
		rg, pending, n, p, err := runSolPipeline(opt.SrcRoot, files.Sol, log)
		if err != nil {
			return persist.Manifest{}, fmt.Errorf("sol pipeline: %w", err)
		}
		parseErrs += n
		solParser = p
		resolved = append(resolved, rg)
		allPending = append(allPending, pending...)
	}

	// (4) graph build + validate
	g, err := graph.Build(resolved)
	if err != nil {
		return persist.Manifest{}, fmt.Errorf("graph.Build: %w", err)
	}
	if err := graph.Validate(g); err != nil {
		return persist.Manifest{}, fmt.Errorf("graph.Validate: %w", err)
	}

	// (4b/4c/5/6) derived passes — xlang, temporal, cluster, score. Shared
	// helper so the partial-cache rebuild path runs identical in-memory
	// transformations (G6 v3 § 4.4). v2's "emitted-vs-DB 0" bug was caused
	// by temporal living only in cold; the helper makes that recurrence
	// structurally impossible.
	pkgTree, topicTree, err := emitDerivedPasses(g, opt.SrcRoot, solParser, log)
	if err != nil {
		return persist.Manifest{}, err
	}

	// (7) persist — cold rebuild wipes graph.db so we don't accumulate stale
	// rows. Incremental path lives in incremental.go and reuses prior rows.
	store, err := openColdStore(opt.OutDir)
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

// openColdStore wipes graph.db and re-opens it. Cold path only — incremental
// builds reuse the existing DB.
func openColdStore(outDir string) (persist.Store, error) {
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
