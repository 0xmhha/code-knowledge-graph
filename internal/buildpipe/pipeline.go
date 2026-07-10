// Package buildpipe orchestrates the full Pass 1..4 build (spec §4.7):
// detect → parse → resolve → graph build/validate → cluster → score → persist.
// Three routing paths: cold rebuild, short-circuit (all-cached), and incremental
// (partial-hit — reuse cached files, re-parse dirty). See Run for routing logic.
package buildpipe

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/internal/detect"
	"github.com/0xmhha/code-knowledge-graph/internal/filterlist"
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
//
// goFuncFieldTouches (W-A): per Go Function/Method node ID → set of struct-
// Field node IDs whose values are read/written in the body. Sourced from
// the Go parser's FuncFieldTouches() accessor on the cold path; nil on
// the incremental path (which short-circuits the propagation pass even
// when opt.LockPropagation is true — incremental cache lacks per-function
// field touches for cached files). lockPropagation=false makes the map
// irrelevant.
func emitDerivedPasses(g *graph.Graph, srcRoot string, solParser *solp.Parser,
	log *slog.Logger, strict bool,
	goFuncFieldTouches map[string]map[string]struct{}, lockPropagation bool,
	temporalDepth int,
) (*cluster.PkgTree, *cluster.TopicTree, map[string][]byte, error) {
	if solParser != nil {
		abi := convertABI(solParser.ABI())
		xlEdges := link.SolToTS(g.Nodes, abi)
		g.Edges = append(g.Edges, xlEdges...)
		if _, err := validateAndSanitize(g, log, "xlang", strict); err != nil {
			return nil, nil, nil, err
		}
		log.Info("xlang linked", "binds_to", len(xlEdges))
	}
	// W2 (schema 1.9 §6.9): HTTP client → server Endpoint matching.
	// Cascade resolves AMBIGUOUS placeholder Endpoints emitted by the per-
	// language parsers (Go: distributed.go, TS: http_client.go) against the
	// real server-side Endpoints from W1. See internal/link/http_match.go
	// for the algorithm + §3.3 exact-match decision.
	newNodes, newEdges, httpResult := link.MatchHTTPClients(g.Nodes, g.Edges)
	g.Nodes = newNodes
	g.Edges = newEdges
	if _, err := validateAndSanitize(g, log, "http_match", strict); err != nil {
		return nil, nil, nil, err
	}
	log.Info("http client matching",
		"rewired", httpResult.Rewired,
		"specific_hits", httpResult.SpecificHits,
		"wildcard_hits", httpResult.WildcardHits,
		"ambiguous_retained", httpResult.AmbiguousRetained,
		"placeholders_dropped", httpResult.PlaceholdersDropped)
	hunkBlobs, err := emitTemporalEdges(g, srcRoot, log, temporalDepth)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("temporal: %w", err)
	}
	if _, err := validateAndSanitize(g, log, "temporal", strict); err != nil {
		return nil, nil, nil, err
	}
	// W-A (D1 Stage B): cross-function lock propagation. Runs BEFORE
	// cluster/score so the propagated edges contribute to PageRank /
	// in-degree statistics consistently with the existing intra-function
	// edges. Opt-in (Options.LockPropagation) — when off the call is a
	// structural no-op that touches neither g.Edges nor the validation
	// gate. validateAndSanitize is invoked after emit so any propagator
	// bug producing dangling endpoints is caught at lenient/strict gates.
	if lockPropagation {
		n := propagateLockedFieldAccess(g, goFuncFieldTouches, log)
		if n > 0 {
			if _, err := validateAndSanitize(g, log, "lock_propagation", strict); err != nil {
				return nil, nil, nil, err
			}
		}
	}
	pkgTree := cluster.BuildPkgTree(g)
	topicTree := cluster.BuildTopicTree(g, []float64{0.5, 1.0, 2.0}, 42)
	score.Compute(g)
	return pkgTree, topicTree, hunkBlobs, nil
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
	// StrictValidate, when true, fails the build on the first dangling edge or
	// schema violation (legacy v0.x behaviour). Default false: dangling edges
	// are dropped with a warning, schema violations still abort. Lenient mode
	// is required for dogfooding self-analysis, where parser bugs would
	// otherwise prevent graph.db from being written and block measurement.
	StrictValidate bool
	// FilesFromPath is the optional path to a JSON include/exclude filter
	// (see internal/filterlist). When set, only files matching the filter
	// reach the parsers. Empty means "use heuristic discovery as before".
	FilesFromPath string
	// LockPropagation enables D1 Stage B cross-function lock propagation
	// (W-A, Within-language semantics Phase 5). When true, the cold build
	// path walks the Go call graph from every lock-holding function up to
	// lockPropagationMaxDepth=5 hops and emits accessed_under_lock(field,
	// mutex) edges for fields touched in reachable callee bodies. Default
	// false (opt-in per W-A §5.0 Q5) so existing builds are byte-identical.
	// Incremental cache path skips propagation regardless of this flag —
	// run with --no-cache when the flag is on to measure full effect.
	// Spec: docs/design/go-cross-function-lock-propagation.md.
	LockPropagation bool

	// PolicyFile is the optional path to a governance/protocol policy
	// YAML (pkg/policy). When set, the cold build loads + resolves the
	// policy entries against the parsed graph and emits NodePolicy +
	// EdgeGovernedBy rows so an LLM can answer "what policy governs
	// this code?" without leaving the graph. Empty means "no policy
	// enrichment" — existing builds stay byte-identical. P1 #4 (see
	// docs/PROJECT-BLUEPRINT-ALIGNMENT.md §4.2). Incremental cache
	// path skips policy resolution for the same reason it skips lock
	// propagation — the policy file's content is decoupled from the
	// per-file parse output, so re-resolving is a cold-only step
	// today; run with --no-cache when the policy file changes.
	PolicyFile string

	// SecurityPatternFile is the optional path to a security risk
	// pattern YAML (pkg/security). Sibling to PolicyFile but for the
	// P1 #5 risk-surface axis: reentrancy, access-control, Byzantine,
	// overflow patterns flagged against specific code symbols. Empty
	// means "no security enrichment". Same cold-only constraint as
	// PolicyFile — re-run with --no-cache when the YAML changes.
	SecurityPatternFile string

	// TemporalDepth caps the per-file commit count the temporal pass (G6)
	// walks when emitting Commit/Hunk nodes and changed_in/blame edges.
	// 0 (the zero value) means "use the built-in default" (temporalDepthDefault
	// = 10). Raising it deepens commit-level history at a roughly linear cost
	// in Commit/Hunk nodes, changed_in edges, and graph size; node_prs symbol
	// history is independent of this cap (see pr_history.go).
	TemporalDepth int
}

// validateAndSanitize runs the lenient/strict validation gate against g and
// returns (droppedDanglingCount, error). Schema errors always abort. Dangling
// edges abort only when strict; otherwise they are dropped in place and
// surfaced via warn-level logs grouped by edge type.
func validateAndSanitize(g *graph.Graph, log *slog.Logger, stage string, strict bool) (int, error) {
	report := graph.Inspect(g)
	if report.HasSchemaErrors() {
		return 0, fmt.Errorf("graph.Validate(%s): %w", stage, report.SchemaErrors[0])
	}
	if !report.HasDangling() {
		return 0, nil
	}
	if strict {
		d := report.DanglingEdges[0]
		side := "src"
		if !d.Src && d.Dst {
			side = "dst"
		}
		return 0, fmt.Errorf("graph.Validate(%s): dangling %s on edge of type %s: %s -> %s",
			stage, side, d.Edge.Type, d.Edge.Src, d.Edge.Dst)
	}
	dropped := graph.Sanitize(g, report)
	for et, n := range report.CountByEdgeType() {
		log.Warn("dangling edges dropped", "stage", stage, "edge_type", string(et), "count", n)
	}
	return dropped, nil
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
	filter, err := filterlist.Load(opt.FilesFromPath)
	if err != nil {
		return persist.Manifest{}, err
	}
	discovery, _, goCount, tsCount, solCount, protoCount, err := discoveryAll(opt.SrcRoot, opt.Languages, filter)
	if err != nil {
		return persist.Manifest{}, err
	}
	log.Info("detected files", "go", goCount, "ts", tsCount, "sol", solCount, "proto", protoCount)
	log.Debug("discovery.end", "total", goCount+tsCount+solCount+protoCount)

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
			return runShortCircuit(opt, log, decisions, old, goCount, tsCount, solCount, protoCount)
		}
		return runIncremental(opt, log, decisions, goCount, tsCount, solCount, protoCount)
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
	// --files-from filter: trim every per-language list before parsing.
	filter, err := filterlist.Load(opt.FilesFromPath)
	if err != nil {
		return persist.Manifest{}, err
	}
	if filter != nil {
		preGo, preTS, preSol, preProto := len(goFiles), len(files.TS), len(files.Sol), len(files.Proto)
		goFiles = filter.FilterPaths(goFiles)
		files.TS = filter.FilterPaths(files.TS)
		files.Sol = filter.FilterPaths(files.Sol)
		files.Proto = filter.FilterPaths(files.Proto)
		log.Info("files-from applied",
			"go", preGo, "go_after", len(goFiles),
			"ts", preTS, "ts_after", len(files.TS),
			"sol", preSol, "sol_after", len(files.Sol),
			"proto", preProto, "proto_after", len(files.Proto))
	}

	// (2)+(3) parse + link, per language
	resolved := []*parse.ResolvedGraph{}
	allPending := []persist.PendingRefRow{}
	parseErrs := 0
	// goFuncFieldTouches: W-A side-channel from the Go parser (per
	// Function/Method node ID → set of struct-Field node IDs touched in
	// body). Consumed by emitDerivedPasses' propagateLockedFieldAccess
	// when opt.LockPropagation is true. Stays nil when the Go pipeline
	// doesn't run or the typed-load fallback strips field resolution.
	var goFuncFieldTouches map[string]map[string]struct{}
	if shouldRun("go", opt.Languages) && len(goFiles) > 0 {
		log.Debug("pass1.start", "language", "go", "files", len(goFiles))
		rg, pending, fieldTouches, n, err := runGoPipeline(opt.SrcRoot, goFiles, log)
		if err != nil {
			return persist.Manifest{}, fmt.Errorf("go pipeline: %w", err)
		}
		parseErrs += n
		resolved = append(resolved, rg)
		allPending = append(allPending, pending...)
		goFuncFieldTouches = fieldTouches
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
	// W3a (schema 1.9): .proto schema parser. Hand-rolled lexer + recursive-
	// descent — emits Service/Method/MessageType/Enum/Field/Package nodes plus
	// uses_type pending refs for rpc request/response types. gRPC client/server
	// detection in Go/TS lives in W3b/W3c.
	if shouldRun("proto", opt.Languages) && len(files.Proto) > 0 {
		log.Debug("pass1.start", "language", "proto", "files", len(files.Proto))
		rg, pending, n, err := runProtoPipeline(opt.SrcRoot, files.Proto, log)
		if err != nil {
			return persist.Manifest{}, fmt.Errorf("proto pipeline: %w", err)
		}
		parseErrs += n
		resolved = append(resolved, rg)
		allPending = append(allPending, pending...)
		log.Debug("pass1.end", "language", "proto", "nodes", len(rg.Nodes), "errs", n)
	}

	// (4) graph build + validate
	log.Debug("pass2.resolve.start", "pending_refs", len(allPending))
	g, err := graph.Build(resolved)
	if err != nil {
		return persist.Manifest{}, fmt.Errorf("graph.Build: %w", err)
	}
	if _, err := validateAndSanitize(g, log, "post-build", opt.StrictValidate); err != nil {
		return persist.Manifest{}, err
	}
	log.Debug("pass2.resolve.end", "nodes", len(g.Nodes), "edges", len(g.Edges))

	// (4b/4c/5/6) derived passes — xlang, temporal, cluster, score. Shared
	// helper so the partial-cache rebuild path runs identical in-memory
	// transformations (G6 v3 § 4.4). v2's "emitted-vs-DB 0" bug was caused
	// by temporal living only in cold; the helper makes that recurrence
	// structurally impossible.
	log.Debug("metrics.start")
	pkgTree, topicTree, hunkBlobs, err := emitDerivedPasses(g, opt.SrcRoot, solParser, log, opt.StrictValidate, goFuncFieldTouches, opt.LockPropagation, opt.TemporalDepth)
	if err != nil {
		return persist.Manifest{}, err
	}
	log.Debug("metrics.end")

	// (7) persist — cold rebuild wipes graph.db so we don't accumulate stale
	// rows. Incremental path lives in incremental.go and reuses prior rows.
	log.Debug("persist.start", "nodes", len(g.Nodes), "edges", len(g.Edges))
	store, commitCold, err := openColdStore(opt.OutDir, opt.DBDSN)
	if err != nil {
		return persist.Manifest{}, err
	}
	committed := false
	defer func() {
		if !committed { // error path: close (and discard) the temp store
			_ = store.Close()
		}
	}()
	if err := persistColdArtifacts(store, opt.SrcRoot, g, pkgTree, topicTree, hunkBlobs); err != nil {
		return persist.Manifest{}, err
	}
	// G6 v3 (schema 1.5): persist Pass 1 pending refs so the next partial
	// build can replay Pass 2 over a merged dirty + cached input set without
	// re-parsing cached files. INSERT after persistColdArtifacts so node FKs
	// are satisfied. The cold path builds into a fresh temp DB via openColdStore,
	// so the table starts empty — IGNORE on the PK in InsertPendingRefs handles
	// the rare emit-twice case.
	if err := store.InsertPendingRefs(allPending); err != nil {
		return persist.Manifest{}, fmt.Errorf("persist pending_refs: %w", err)
	}
	// ckg-NEW-2 PR breadcrumb (schema 1.12): scan git log for PR-tagged
	// commits and persist the node ↔ PR mapping. Runs after the node
	// rows land so the node_prs FK has its targets. Failure here is
	// logged but non-fatal — PR metadata is strictly additive and a
	// missing remote (non-git source tree, sparse clone, etc.) must
	// not break the cold build.
	prByNode, prErr := ScanPRHistory(opt.SrcRoot, g.Nodes)
	if prErr != nil {
		log.Warn("scan pr_history failed; node_prs left empty", "err", prErr)
	} else if len(prByNode) > 0 {
		if err := store.InsertNodePRs(prByNode); err != nil {
			return persist.Manifest{}, fmt.Errorf("persist node_prs: %w", err)
		}
		log.Info("pr_history emitted", "nodes_with_prs", len(prByNode))
	}
	// P1 #4 policy metadata (schema 1.14): load the operator-supplied
	// YAML, resolve governs[] qnames against the parsed graph, persist
	// NodePolicy rows + EdgeGovernedBy edges. Runs after the main node
	// insert so the FK on EdgeGovernedBy.src targets resolved code IDs.
	// Failure here is logged but non-fatal — policy enrichment is
	// strictly additive; a malformed YAML must not break the cold
	// build. Empty PolicyFile → skipped silently.
	if policyNodes, policyEdges, perr := loadPolicy(opt.PolicyFile, g.Nodes, log); perr != nil {
		log.Warn("policy enrichment failed; policy nodes/edges left empty",
			"file", opt.PolicyFile, "err", perr)
	} else if len(policyNodes) > 0 {
		if err := store.InsertNodes(policyNodes); err != nil {
			return persist.Manifest{}, fmt.Errorf("persist policy nodes: %w", err)
		}
		if len(policyEdges) > 0 {
			if err := store.InsertEdges(policyEdges); err != nil {
				return persist.Manifest{}, fmt.Errorf("persist policy edges: %w", err)
			}
		}
		log.Info("policy enrichment emitted",
			"policy_nodes", len(policyNodes), "governed_by_edges", len(policyEdges))
	}
	// P1 #5 security pattern annotations (schema 1.15): same load-
	// resolve-persist shape as policy enrichment above. Runs after
	// policy so its own matches[] index reuses the (now larger)
	// g.Nodes set if a future YAML wanted to label a Policy node
	// itself as a security-sensitive area — overlap is permitted but
	// not relied on. Failure is logged-only for the same additive-
	// metadata reason.
	if secNodes, secEdges, serr := loadSecurityPatterns(opt.SecurityPatternFile, g.Nodes, log); serr != nil {
		log.Warn("security enrichment failed; security nodes/edges left empty",
			"file", opt.SecurityPatternFile, "err", serr)
	} else if len(secNodes) > 0 {
		if err := store.InsertNodes(secNodes); err != nil {
			return persist.Manifest{}, fmt.Errorf("persist security nodes: %w", err)
		}
		if len(secEdges) > 0 {
			if err := store.InsertEdges(secEdges); err != nil {
				return persist.Manifest{}, fmt.Errorf("persist security edges: %w", err)
			}
		}
		log.Info("security enrichment emitted",
			"security_nodes", len(secNodes), "has_security_pattern_edges", len(secEdges))
	}
	log.Debug("persist.end")

	m := buildManifestSkeleton(opt, len(goFiles), len(files.TS), len(files.Sol), len(files.Proto),
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
	// Commit the cold store: close + atomically rename graph.db.building over
	// graph.db (SQLite), so a concurrent reader never sees a partial DB (Q2).
	if err := commitCold(); err != nil {
		return persist.Manifest{}, err
	}
	committed = true
	log.Info("build complete",
		"nodes", len(g.Nodes), "edges", len(g.Edges),
		"pkg_tree_edges", len(pkgTree.Edges),
		"topic_resolutions", len(topicTree.Resolutions))
	return m, nil
}

// openColdStore prepares a store for a cold rebuild and returns a commit func
// that must be called on success. For SQLite (Q2, ADR reindex-migration
// 2026-07-10) the build writes to a temporary "graph.db.building" file; commit
// closes the store — which checkpoints and removes the WAL/SHM sidecars on the
// last connection — then atomically renames it over the live graph.db. This
// removes the multi-second window in which the old destructive `os.Remove(dbPath)
// → rewrite` left graph.db absent or half-written for a concurrent reader
// (e.g. `ckg serve`). The fully-atomic cross-file boundary is still the
// versioned-dir + `current` symlink swap owned by the build orchestration; this
// makes even a naive same-dir rebuild safe.
//
// When dbDsn is set the store is a PostgreSQL database wiped in place via
// TRUNCATE; commit is a plain Close (pg atomicity is out of scope here).
func openColdStore(outDir, dbDsn string) (persist.Store, func() error, error) {
	if dbDsn != "" {
		store, err := persist.OpenPostgresCold(dbDsn)
		if err != nil {
			return nil, nil, err
		}
		return store, store.Close, nil
	}
	dbPath := filepath.Join(outDir, "graph.db")
	tmpPath := dbPath + ".building"
	removeSQLiteFiles(tmpPath) // clear artifacts from an aborted prior build
	store, err := persist.Open(tmpPath)
	if err != nil {
		return nil, nil, err
	}
	if err := store.Migrate(); err != nil {
		_ = store.Close()
		return nil, nil, err
	}
	commit := func() error {
		// Close first so WAL is checkpointed into the main file and -wal/-shm
		// are dropped — the temp file becomes a complete standalone DB.
		if err := store.Close(); err != nil {
			return fmt.Errorf("close cold store before rename: %w", err)
		}
		// Rename replaces the live file atomically (same directory / filesystem).
		if err := os.Rename(tmpPath, dbPath); err != nil {
			return fmt.Errorf("rename %s -> graph.db: %w", filepath.Base(tmpPath), err)
		}
		// Drop any stale sidecars left by a prior WAL-mode graph.db so a fresh
		// reader of the renamed file never pairs it with a mismatched -wal.
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
		return nil
	}
	return store, commit, nil
}

// removeSQLiteFiles deletes a SQLite DB file together with its WAL/SHM sidecars.
func removeSQLiteFiles(path string) {
	_ = os.Remove(path)
	_ = os.Remove(path + "-wal")
	_ = os.Remove(path + "-shm")
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
//
// hunkBlobs (schema 1.8 H1) is the gzip-compressed unified-diff text per
// Hunk node ID, returned by emitTemporalEdges. Merged into the regular
// extractBlobs map so a single InsertBlobs round-trip persists both
// CodeNode source slices and hunk patches under the same blobs.node_id PK.
func persistColdArtifacts(store persist.Store, srcRoot string,
	g *graph.Graph, pkgTree *cluster.PkgTree, topicTree TopicTreeForPersist,
	hunkBlobs map[string][]byte) error {
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
	blobs := extractBlobs(srcRoot, g.Nodes)
	maps.Copy(blobs, hunkBlobs)
	if err := store.InsertBlobs(blobs); err != nil {
		return err
	}
	return store.RebuildFTS()
}

// computeColdFileEntries hashes every discovered file and returns FileEntry
// records for the new manifest. Called on cold rebuild so the next build can
// diff against this baseline. EdgeIDs are int64 PRIMARY KEY values assigned
// by the AUTOINCREMENT INSERT just performed.
//
// Meta nodes (Commit, Hunk) are excluded from per-file NodeIDs (schema 1.8
// §11.8 decision): they're emitted wholesale by emitTemporalEdges on every
// build, so attributing them to a file would inflate the manifest and
// trigger spurious cache invalidations whenever that file's content changes.
func computeColdFileEntries(srcRoot, ckgVersion string, discovery []DiscoveredFile, nodes []types.Node, edges []types.Edge) []persist.FileEntry {
	nodesByPath := map[string][]string{}
	for _, n := range nodes {
		if n.FilePath == "" {
			continue
		}
		if isMetaNodeType(n.Type) {
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
// skipped (they have no syntactic body); meta nodes (Commit, Hunk) are
// also skipped (they have no on-disk byte range — Hunk patch bytes come
// from emitTemporalEdges' hunkBlobs map merged into the result by the
// caller, NOT from a file slice). Offsets are bounds-checked defensively
// to avoid panics on malformed nodes.
func extractBlobs(root string, nodes []types.Node) map[string][]byte {
	blobs := map[string][]byte{}
	cache := map[string][]byte{}
	for _, n := range nodes {
		if n.Type == types.NodePackage {
			continue
		}
		if isMetaNodeType(n.Type) {
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
