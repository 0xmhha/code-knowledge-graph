// Package buildpipe orchestrates the full Pass 1..4 build (spec §4.7):
// detect → parse → resolve → graph build/validate → cluster → score → persist.
// V0 supports a full rebuild only — incremental updates are not wired here.
package buildpipe

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/internal/detect"
	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/link"
	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	solp "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/internal/score"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

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

	// (2) cache routing
	//
	// Two paths only:
	//   - runShortCircuit: 100% cache hit (no parse, no graph rewrite, manifest
	//     timestamp refresh). This is the load-bearing speedup case — CI re-runs
	//     on unchanged source finish in <1s instead of cold rebuild time.
	//   - runCold: any miss or removal → full rebuild.
	//
	// runIncremental (the partial-rebuild path) is intentionally NOT routed
	// here. Empirical reproduction (testdata/synthetic, modifying vault.go)
	// showed cross-file `calls` edges where the caller is cached and callee
	// is dirty are silently dropped (cold: 2 calls → warm-incremental: 0).
	// Root cause: cached files are not re-parsed, so their pending refs are
	// not re-emitted; Pass 2 only sees pending refs from dirty files. The
	// existing reloadCachedEdges drops cross-file edges spanning dirty↔cached
	// because the dirty-side node IDs don't match (content-hash IDs change).
	// runIncremental + helpers are kept as dead code for the eventual
	// re-enable (see WORK-PLAN.md C1 / Phase 2 reverse-reference index OR a
	// "persisted pending refs" approach) — until then the safe fallback is
	// cold rebuild on any non-full-hit case.
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
		log.Info("Cache: partial hit; falling back to cold rebuild for correctness",
			"hits", decisions.Hits, "misses", decisions.Misses, "removed", decisions.Removed)
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
	parseErrs := 0
	if shouldRun("go", opt.Languages) && len(goFiles) > 0 {
		rg, n, err := runGoPipeline(opt.SrcRoot, goFiles, log)
		if err != nil {
			return persist.Manifest{}, fmt.Errorf("go pipeline: %w", err)
		}
		parseErrs += n
		resolved = append(resolved, rg)
	}
	// solParser is retained across the language passes so that the
	// cross-language linker (T20) can read Solidity ABI sigs after graph.Build.
	// nil signals "no Sol pipeline ran" — xlang stage is skipped in that case.
	var solParser *solp.Parser
	if shouldRun("ts", opt.Languages) && len(files.TS) > 0 {
		rg, n, err := runTSPipeline(opt.SrcRoot, files.TS, log)
		if err != nil {
			return persist.Manifest{}, fmt.Errorf("ts pipeline: %w", err)
		}
		parseErrs += n
		resolved = append(resolved, rg)
	}
	if shouldRun("sol", opt.Languages) && len(files.Sol) > 0 {
		rg, n, p, err := runSolPipeline(opt.SrcRoot, files.Sol, log)
		if err != nil {
			return persist.Manifest{}, fmt.Errorf("sol pipeline: %w", err)
		}
		parseErrs += n
		solParser = p
		resolved = append(resolved, rg)
	}

	// (4) graph build + validate
	g, err := graph.Build(resolved)
	if err != nil {
		return persist.Manifest{}, fmt.Errorf("graph.Build: %w", err)
	}
	if err := graph.Validate(g); err != nil {
		return persist.Manifest{}, fmt.Errorf("graph.Validate: %w", err)
	}

	// (4b) cross-language linking: Sol -> TS binds_to edges (spec §4.7.3).
	// Re-validate after appending so any dangling refs are caught defensively.
	if solParser != nil {
		abi := convertABI(solParser.ABI())
		xlEdges := link.SolToTS(g.Nodes, abi)
		g.Edges = append(g.Edges, xlEdges...)
		if err := graph.Validate(g); err != nil {
			return persist.Manifest{}, fmt.Errorf("validate after xlang: %w", err)
		}
		log.Info("xlang linked", "binds_to", len(xlEdges))
	}

	// (5) cluster
	pkgTree := cluster.BuildPkgTree(g)
	topicTree := cluster.BuildTopicTree(g, []float64{0.5, 1.0, 2.0}, 42)

	// (6) score
	score.Compute(g)

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

// runGoPipeline drives Pass 1 (per-file ParseFile) + Pass 2 (Resolve) for Go.
// Returns the resolved graph, count of files that failed to read or parse,
// and any fatal Resolve error.
func runGoPipeline(srcRoot string, files []string, log *slog.Logger) (*parse.ResolvedGraph, int, error) {
	p := gop.New(srcRoot)
	results := []*parse.ParseResult{}
	errs := 0
	for _, rel := range files {
		full := filepath.Join(srcRoot, rel)
		src, err := os.ReadFile(full)
		if err != nil {
			log.Warn("read file", "path", full, "err", err)
			errs++
			continue
		}
		r, err := p.ParseFile(full, src)
		if err != nil {
			log.Warn("parse file", "path", full, "err", err)
			errs++
			continue
		}
		stampFilePath(r)
		results = append(results, r)
	}
	rg, err := p.Resolve(results)
	return rg, errs, err
}

// stampFilePath populates Edge.FilePath for every per-file edge that lacks
// one, drawing from the ParseResult.Path the parser already recorded.
// Required by the A3 incremental cache: EdgesByFilePath reloads cached
// edges by file_path, and the parsers historically left it blank because
// the V0 schema didn't surface the field. Stamping is idempotent — pre-set
// FilePaths (e.g. on edges with line numbers) are preserved.
//
// Stamping per-file edges is safe: an edge emitted while parsing file X
// belongs to X by construction. Cross-file edges come from Pass 2 (Resolve),
// not per-file ParseFile, so this stamping doesn't touch them.
func stampFilePath(r *parse.ParseResult) {
	rel := filepath.ToSlash(r.Path)
	if rel == "" {
		return
	}
	for i := range r.Edges {
		if r.Edges[i].FilePath == "" {
			r.Edges[i].FilePath = rel
		}
	}
}

// runTSPipeline drives Pass 1 + Pass 2 for TypeScript / JavaScript.
// Returns the resolved graph, count of files that failed to read or parse,
// and any fatal Resolve error. Mirrors runGoPipeline.
func runTSPipeline(srcRoot string, files []string, log *slog.Logger) (*parse.ResolvedGraph, int, error) {
	p := tsp.New(srcRoot)
	results := []*parse.ParseResult{}
	errs := 0
	for _, rel := range files {
		full := filepath.Join(srcRoot, rel)
		src, err := os.ReadFile(full)
		if err != nil {
			log.Warn("ts read", "path", full, "err", err)
			errs++
			continue
		}
		r, err := p.ParseFile(full, src)
		if err != nil {
			log.Warn("ts parse", "path", full, "err", err)
			errs++
			continue
		}
		stampFilePath(r)
		results = append(results, r)
	}
	rg, err := p.Resolve(results)
	return rg, errs, err
}

// runSolPipeline drives Pass 1 + Pass 2 for Solidity. Returns the parser
// instance so callers can read the accumulated ABI for cross-language linking.
func runSolPipeline(srcRoot string, files []string, log *slog.Logger) (*parse.ResolvedGraph, int, *solp.Parser, error) {
	p := solp.New(srcRoot)
	results := []*parse.ParseResult{}
	errs := 0
	for _, rel := range files {
		full := filepath.Join(srcRoot, rel)
		src, err := os.ReadFile(full)
		if err != nil {
			log.Warn("sol read", "path", full, "err", err)
			errs++
			continue
		}
		r, err := p.ParseFile(full, src)
		if err != nil {
			log.Warn("sol parse", "path", full, "err", err)
			errs++
			continue
		}
		stampFilePath(r)
		results = append(results, r)
	}
	rg, err := p.Resolve(results)
	return rg, errs, p, err
}

// convertABI bridges solidity.ABISig (parser output) and link.ABISig (linker
// input) to keep the link package free of any per-language parser imports.
func convertABI(in map[string][]solp.ABISig) map[string][]link.ABISig {
	out := make(map[string][]link.ABISig, len(in))
	for k, v := range in {
		converted := make([]link.ABISig, len(v))
		for i, s := range v {
			converted[i] = link.ABISig{
				ContractName: s.ContractName,
				FunctionName: s.FunctionName,
				ParamTypes:   s.ParamTypes,
			}
		}
		out[k] = converted
	}
	return out
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

// setStaleness records the staleness fingerprint on the manifest. Prefers a
// path-aware git commit SHA (the last commit that modified files under
// m.SrcRoot); falls back to summing mtimes of up to 5 detected files when the
// source root is not a git checkout or has no commit history yet.
//
// The path-aware lookup is required for sub-directories of larger repos
// (monorepos, or tools' own testdata): a plain `git rev-parse HEAD` would
// flip on every unrelated commit elsewhere in the repo, producing
// false-positive stale banners in the viewer.
func setStaleness(m *persist.Manifest, log *slog.Logger) {
	repoRoot, relPath, ok := gitRepoRel(m.SrcRoot)
	if ok {
		commit, err := pathAwareHead(repoRoot, relPath)
		if err == nil && commit != "" {
			m.SrcCommit = commit
			m.SrcRelPath = relPath
			m.StalenessMethod = "git"
			return
		}
		if err != nil {
			log.Warn("path-aware git HEAD failed; falling back to mtime",
				"repo", repoRoot, "rel", relPath, "err", err)
		}
	}
	m.StalenessMethod = "mtime"
	// Mtime fallback only needs a stable, deterministic subset of source
	// files — it doesn't need to mirror the build oracle exactly. detect.Walk
	// is intentionally reused here (rather than detect.GoFiles + the TS/Sol
	// halves of detect.Walk) because the cost of forking another packages.Load
	// for fingerprinting outweighs the cost of a few build-tag-excluded paths
	// landing in the StalenessFiles list — the resulting hash is still
	// deterministic and that's all this codepath needs.
	files, _ := detect.Walk(m.SrcRoot)
	all := append(append([]string{}, files.Go...), files.TS...)
	all = append(all, files.Sol...)
	if len(all) > 5 {
		all = all[:5]
	}
	var sum int64
	for _, rel := range all {
		st, err := os.Stat(filepath.Join(m.SrcRoot, rel))
		if err == nil {
			sum += st.ModTime().UnixNano()
		}
	}
	m.StalenessFiles = all
	m.StalenessMTimeSum = sum
}

// gitRepoRel returns (repoRoot, relPathFromRepoRoot, true) when srcRoot lives
// inside a git checkout, or ("", "", false) otherwise. relPath is normalised
// to forward slashes and is "." when srcRoot is the repo root itself.
//
// Both repoRoot and srcRoot are resolved through filepath.EvalSymlinks before
// computing filepath.Rel — without this, on macOS where /tmp -> /private/tmp
// the rel computation walks ../../../ across the symlink boundary and yields
// a path git cannot resolve.
func gitRepoRel(srcRoot string) (string, string, bool) {
	out, err := exec.Command("git", "-C", srcRoot, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", "", false
	}
	repoRoot := strings.TrimSpace(string(out))
	if repoRoot == "" {
		return "", "", false
	}
	absSrc, err := filepath.Abs(srcRoot)
	if err != nil {
		return "", "", false
	}
	if resolved, err := filepath.EvalSymlinks(absSrc); err == nil {
		absSrc = resolved
	}
	if resolved, err := filepath.EvalSymlinks(repoRoot); err == nil {
		repoRoot = resolved
	}
	rel, err := filepath.Rel(repoRoot, absSrc)
	if err != nil {
		return "", "", false
	}
	return repoRoot, filepath.ToSlash(rel), true
}

// pathAwareHead runs `git -C <repoRoot> log -1 --format=%H -- <relPath>` and
// returns the trimmed SHA. Empty output (no commit ever touched relPath) is
// returned as ("", nil) so callers can fall back to mtime fingerprinting.
func pathAwareHead(repoRoot, relPath string) (string, error) {
	out, err := exec.Command("git", "-C", repoRoot, "log", "-1", "--format=%H", "--", relPath).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// writeManifestJSON pretty-prints the manifest to path for human inspection.
func writeManifestJSON(path string, m persist.Manifest) error {
	buf, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return os.WriteFile(path, buf, 0o644)
}
