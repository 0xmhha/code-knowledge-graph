// Package buildpipe — incremental.go drives the A3 file-level cache build path
// (spec v0.2 § 4 Phase 1). Two entry points:
//
//   - runCold: full rebuild (legacy V0 path, used on --no-cache or unusable cache).
//   - runIncremental: parse only dirty files, reload cached node sets from DB,
//     then rerun Pass 2 / cluster / score across the merged graph.
//
// Phase 1 simplifications (per spec, deferred to C1+):
//   - PageRank/Leiden recompute on ANY dirt (no <1% change-ratio shortcut).
//   - Cross-language Sol↔TS link rebuilt whenever any TS or Sol file is dirty.
//   - Reverse-reference index for partial Pass 2 invalidation: NOT implemented
//     (Phase 2, C1's job). Pass 2 Resolve always sees the full per-language node set.
package buildpipe

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/internal/detect"
	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	solp "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// discoveryAll returns every discovered file in slash form with language tag.
// Output is sorted by path so cache-decision logging is deterministic.
func discoveryAll(srcRoot string, languages []string) ([]DiscoveredFile, persist.Manifest, int, int, int, error) {
	var lc persist.Manifest // language counts only — caller fills full manifest later
	files, err := detect.Walk(srcRoot)
	if err != nil {
		return nil, lc, 0, 0, 0, fmt.Errorf("detect: %w", err)
	}
	goFiles, err := detect.GoFiles(srcRoot)
	if err != nil {
		return nil, lc, 0, 0, 0, fmt.Errorf("detect go: %w", err)
	}
	out := make([]DiscoveredFile, 0, len(goFiles)+len(files.TS)+len(files.Sol))
	if shouldRun("go", languages) {
		for _, p := range goFiles {
			out = append(out, DiscoveredFile{Path: filepath.ToSlash(p), Language: "go"})
		}
	}
	if shouldRun("ts", languages) {
		for _, p := range files.TS {
			out = append(out, DiscoveredFile{Path: filepath.ToSlash(p), Language: "ts"})
		}
	}
	if shouldRun("sol", languages) {
		for _, p := range files.Sol {
			out = append(out, DiscoveredFile{Path: filepath.ToSlash(p), Language: "sol"})
		}
	}
	return out, lc, len(goFiles), len(files.TS), len(files.Sol), nil
}

// readOldManifestFromDB returns nil if graph.db is missing or unreadable —
// that's a cold-start signal, not an error. A truncated manifest yields
// (nil, error) so the caller can decide between propagation and silent
// fallback to full rebuild.
func readOldManifestFromDB(dbPath string) *persist.Manifest {
	if _, err := os.Stat(dbPath); err != nil {
		return nil
	}
	store, err := persist.OpenReadOnly(dbPath)
	if err != nil {
		return nil
	}
	defer store.Close()
	m, err := store.GetManifest()
	if err != nil {
		return nil
	}
	return &m
}

// runIncremental implements the partial-cache build path (G6 v3). Caller
// must have already determined the cache base is usable (ManifestUsable) and
// at least one file is cached (so we have something to reuse).
//
// Flow (post-v3):
//  1. DROP temporal + xlang edges from DB — they are always rebuilt and
//     would otherwise duplicate or stale.
//  2. DELETE dirty + removed files' nodes — FK CASCADE wipes their edges,
//     blobs, AND pending_refs in the same statement.
//  3. Per-language Pass 1 (dirty only) + reload cached nodes + cached
//     pending_refs from DB → synthesise ParseResults so Pass 2 sees the
//     cold-equivalent input set.
//  4. graph.Build merges + dedups by (Type, Src, Dst, Line) keep-first.
//     Reloaded edges come FIRST in parts ordering so dedup picks the row
//     that already exists in DB (ID != 0) — fresh duplicates (ID == 0)
//     would otherwise be re-INSERTed and double the row count.
//  5. emitDerivedPasses runs the same xlang/temporal/cluster/score pipeline
//     cold uses — v2's "emitted-vs-DB 0" bug for changed_in is structurally
//     impossible because both paths share this helper now.
//  6. persistIncrementalArtifacts inserts new nodes + new edges (ID==0
//     filter) + cluster + topic + per-dirty-file blobs. Dirty pending refs
//     are inserted last (cached refs survived FK CASCADE — no re-insert).
func runIncremental(opt Options, log *slog.Logger,
	decisions CacheDecisions,
	goCount, tsCount, solCount int) (persist.Manifest, error) {
	log.Info(decisions.FormatLogLine())
	dbPath := filepath.Join(opt.OutDir, "graph.db")
	store, err := persist.Open(dbPath)
	if err != nil {
		return persist.Manifest{}, err
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		return persist.Manifest{}, err
	}
	dirtyByLang, cachedByLang := partitionByLang(decisions)

	// (1) Drop always-rebuilt edges from DB so the in-memory re-emit + ID==0
	// filter at persist time produces exactly one DB row per logical edge.
	// changed_in/blame come from the temporal pass; binds_to from xlang.
	for _, t := range []string{"changed_in", "blame", "binds_to"} {
		if err := store.DeleteEdgesByType(t); err != nil {
			return persist.Manifest{}, fmt.Errorf("clear %s: %w", t, err)
		}
	}

	// (2) Drop nodes/edges/blobs/pending_refs for dirty + removed files via
	// CASCADE. Pending refs are removed because their src_id FK references
	// a dropped node.
	for _, p := range append(decisions.DirtyPaths(), decisions.RemovedPaths()...) {
		if err := store.DeleteNodesByFilePath(p); err != nil {
			return persist.Manifest{}, fmt.Errorf("delete %s: %w", p, err)
		}
	}

	// (3) Per-language Pass 1 + reload cached nodes & pending_refs.
	resolved, dirtyPending, parseErrs, _, solParser, err := runLanguagePipelines(
		opt.SrcRoot, dirtyByLang, cachedByLang, store, log)
	if err != nil {
		return persist.Manifest{}, err
	}

	reloadedFromDB, cachedNodeIDs, err := reloadCachedEdges(store, cachedByLang)
	if err != nil {
		return persist.Manifest{}, err
	}

	// (4) graph.Build: prepend the reloaded-edges ResolvedGraph so dedup
	// keep-first prefers DB-resident edges (ID != 0) over freshly emitted
	// duplicates (ID == 0). Without this ordering, fresh dups slip past the
	// ID==0 filter at persist time and get re-INSERTed, doubling rows.
	parts := make([]*parse.ResolvedGraph, 0, len(resolved)+1)
	parts = append(parts, &parse.ResolvedGraph{Edges: reloadedFromDB})
	parts = append(parts, resolved...)

	g, err := graph.Build(parts)
	if err != nil {
		return persist.Manifest{}, fmt.Errorf("graph.Build: %w", err)
	}
	if err := graph.Validate(g); err != nil {
		return persist.Manifest{}, fmt.Errorf("graph.Validate: %w", err)
	}

	// (5) Derived passes — xlang, temporal, cluster, score. Same helper as
	// cold path; DB drops in step (1) ensure the in-memory re-emission
	// translates cleanly to the persist step.
	pkgTree, topicTree, err := emitDerivedPasses(g, opt.SrcRoot, solParser, log)
	if err != nil {
		return persist.Manifest{}, err
	}

	// (6) Persist + new pending_refs for dirty files only.
	if err := persistIncrementalArtifacts(store, opt.SrcRoot, g, pkgTree, topicTree,
		decisions.DirtyPaths(), cachedNodeIDs); err != nil {
		return persist.Manifest{}, err
	}
	if err := store.InsertPendingRefs(dirtyPending); err != nil {
		return persist.Manifest{}, fmt.Errorf("persist pending_refs: %w", err)
	}

	m := buildManifestSkeleton(opt, goCount, tsCount, solCount, g, pkgTree, parseErrs)
	m.Files = buildFileEntries(decisions, g.Nodes, g.Edges)
	setStaleness(&m, log)
	if err := store.SetManifest(m); err != nil {
		return persist.Manifest{}, err
	}
	if err := writeManifestJSON(filepath.Join(opt.OutDir, "manifest.json"), m); err != nil {
		return persist.Manifest{}, err
	}
	log.Info("incremental build complete",
		"nodes", len(g.Nodes), "edges", len(g.Edges),
		"reparsed", len(decisions.DirtyPaths()),
		"removed", len(decisions.RemovedPaths()),
		"reused_from_cache", len(decisions.CachedPaths()))
	return m, nil
}

// partitionByLang groups dirty/cached file paths by language. Map iteration
// order is undefined but each per-language slice preserves discovery order.
func partitionByLang(decisions CacheDecisions) (dirty, cached map[string][]string) {
	dirty = map[string][]string{}
	cached = map[string][]string{}
	for _, d := range decisions.Decisions {
		switch d.Class {
		case classDirty:
			dirty[d.Language] = append(dirty[d.Language], d.Path)
		case classCached:
			cached[d.Language] = append(cached[d.Language], d.Path)
		}
	}
	return
}

// runLanguagePipelines fans out the per-language Pass 1 + Pass 2 work,
// returning the merged resolved-graph slice + dirty pending refs (caller
// persists them after node insert) + accumulated parse error count + a flag
// for whether any TS/Sol file changed (drives xlang rebuild decision) + the
// Sol parser instance for ABI extraction.
func runLanguagePipelines(srcRoot string, dirty, cached map[string][]string,
	store persist.Store, log *slog.Logger) ([]*parse.ResolvedGraph, []persist.PendingRefRow, int, bool, *solp.Parser, error) {
	resolved := []*parse.ResolvedGraph{}
	dirtyPending := []persist.PendingRefRow{}
	parseErrs := 0
	tsOrSolDirty := false
	var solParser *solp.Parser

	if files := dirty["go"]; len(files) > 0 || hasCached(cached, "go") {
		rg, pending, n, err := runGoPipelineIncremental(srcRoot, files, cached["go"], store, log)
		if err != nil {
			return nil, nil, 0, false, nil, fmt.Errorf("go incremental: %w", err)
		}
		parseErrs += n
		resolved = append(resolved, rg)
		dirtyPending = append(dirtyPending, pending...)
	}
	if files := dirty["ts"]; len(files) > 0 || hasCached(cached, "ts") {
		if len(files) > 0 {
			tsOrSolDirty = true
		}
		rg, pending, n, err := runTSPipelineIncremental(srcRoot, files, cached["ts"], store, log)
		if err != nil {
			return nil, nil, 0, false, nil, fmt.Errorf("ts incremental: %w", err)
		}
		parseErrs += n
		resolved = append(resolved, rg)
		dirtyPending = append(dirtyPending, pending...)
	}
	if files := dirty["sol"]; len(files) > 0 || hasCached(cached, "sol") {
		if len(files) > 0 {
			tsOrSolDirty = true
		}
		rg, pending, n, p, err := runSolPipelineIncremental(srcRoot, files, cached["sol"], store, log)
		if err != nil {
			return nil, nil, 0, false, nil, fmt.Errorf("sol incremental: %w", err)
		}
		parseErrs += n
		solParser = p
		resolved = append(resolved, rg)
		dirtyPending = append(dirtyPending, pending...)
	}
	return resolved, dirtyPending, parseErrs, tsOrSolDirty, solParser, nil
}

// reloadCachedEdges pulls every edge persisted under a cached file's
// file_path AND every cross-file edge (file_path="") whose endpoints are
// both in cached files' node sets. Returns the unioned edge slice + the
// node-ID set so callers can dedupe inserts later.
//
// Cross-file edges spanning a dirty endpoint are NOT reloaded — they were
// either cascade-deleted (dirty src) or will be re-emitted by Resolve from
// the dirty side (dirty dst). Edges that need re-emission from the cached
// side are a Phase 2 problem (reverse-reference index → C1).
func reloadCachedEdges(store persist.Store, cachedByLang map[string][]string) ([]types.Edge, map[string]bool, error) {
	reloaded := []types.Edge{}
	cachedNodeIDs := map[string]bool{}
	for _, paths := range cachedByLang {
		for _, p := range paths {
			es, err := store.EdgesByFilePath(p)
			if err != nil {
				return nil, nil, fmt.Errorf("reload edges for %s: %w", p, err)
			}
			reloaded = append(reloaded, es...)
			ns, err := store.NodesByFilePath(p)
			if err != nil {
				return nil, nil, fmt.Errorf("reload nodes for %s: %w", p, err)
			}
			for _, n := range ns {
				cachedNodeIDs[n.ID] = true
			}
		}
	}
	if len(cachedNodeIDs) == 0 {
		return reloaded, cachedNodeIDs, nil
	}
	ids := make([]string, 0, len(cachedNodeIDs))
	for id := range cachedNodeIDs {
		ids = append(ids, id)
	}
	xEdges, err := store.QueryEdgesForNodes(ids)
	if err != nil {
		return nil, nil, fmt.Errorf("reload cross-file edges: %w", err)
	}
	seenID := map[int64]bool{}
	for _, e := range reloaded {
		seenID[e.ID] = true
	}
	for _, e := range xEdges {
		if e.FilePath != "" || seenID[e.ID] {
			continue
		}
		// Drop cross-file edges that span dirty↔cached; the dirty side
		// will re-emit (or has cascaded out) what it needs.
		if !cachedNodeIDs[e.Src] || !cachedNodeIDs[e.Dst] {
			continue
		}
		reloaded = append(reloaded, e)
		seenID[e.ID] = true
	}
	return reloaded, cachedNodeIDs, nil
}

// persistIncrementalArtifacts handles the incremental-path inserts: nodes
// (filtered to exclude cached node IDs to avoid INSERT OR REPLACE cascade),
// edges (filtered to exclude reloaded edges via Edge.ID==0 discriminator),
// pkg/topic trees (full replace), per-dirty-file blobs, FTS rebuild.
func persistIncrementalArtifacts(store persist.Store, srcRoot string,
	g *graph.Graph, pkgTree *cluster.PkgTree, topicTree TopicTreeForPersist,
	dirtyPaths []string, cachedNodeIDs map[string]bool) error {
	// Nodes: skip those already in DB (cached). Re-emitted dirty parse
	// nodes that share an ID with a cached one (e.g. shared Package node)
	// are skipped — DB row already represents them.
	newNodes := make([]types.Node, 0)
	for _, n := range g.Nodes {
		if cachedNodeIDs[n.ID] {
			continue
		}
		newNodes = append(newNodes, n)
	}
	if err := store.InsertNodes(newNodes); err != nil {
		return err
	}
	// Edges: ID==0 → freshly produced this build; ID!=0 → reloaded from DB.
	newEdges := make([]types.Edge, 0, len(g.Edges))
	for _, e := range g.Edges {
		if e.ID != 0 {
			continue
		}
		newEdges = append(newEdges, e)
	}
	if err := store.InsertEdges(newEdges); err != nil {
		return err
	}
	if err := store.InsertPkgTreeFromCluster(pkgTree.PersistEdges()); err != nil {
		return err
	}
	if err := store.InsertTopicTree(topicTree); err != nil {
		return err
	}
	if err := store.InsertBlobs(extractBlobsForFiles(srcRoot, g.Nodes, dirtyPaths)); err != nil {
		return err
	}
	return store.RebuildFTS()
}

// TopicTreeForPersist re-exposes persist.TopicTreeInput under a buildpipe-
// local alias so persistIncrementalArtifacts can take it as a typed param
// without leaking the persist package detail to every caller.
type TopicTreeForPersist = persist.TopicTreeInput

// runShortCircuit handles the all-cached, no-removed case: nothing to parse,
// nothing to delete. Just refresh the manifest timestamp + staleness.
func runShortCircuit(opt Options, log *slog.Logger, decisions CacheDecisions,
	old *persist.Manifest, goCount, tsCount, solCount int) (persist.Manifest, error) {
	log.Info(decisions.FormatLogLine() + " (no source changes; manifest timestamp refreshed)")
	dbPath := filepath.Join(opt.OutDir, "graph.db")
	store, err := persist.Open(dbPath)
	if err != nil {
		return persist.Manifest{}, err
	}
	defer store.Close()
	// Old manifest fields stay; bump timestamp + recompute staleness.
	m := *old
	m.BuildTimestamp = time.Now().UTC().Format(time.RFC3339)
	m.Languages = map[string]int{"go": goCount, "ts": tsCount, "sol": solCount}
	setStaleness(&m, log)
	if err := store.SetManifest(m); err != nil {
		return persist.Manifest{}, err
	}
	if err := writeManifestJSON(filepath.Join(opt.OutDir, "manifest.json"), m); err != nil {
		return persist.Manifest{}, err
	}
	return m, nil
}

// hasCached reports whether the language has any cached files. Encapsulates
// the nil-safe map lookup so call sites stay readable.
func hasCached(m map[string][]string, lang string) bool {
	return len(m[lang]) > 0
}

// runGoPipelineIncremental parses dirtyFiles, then loads cached files' nodes
// AND pending refs from DB and synthesises ParseResults so Pass 2's qIndex
// AND its pending-ref consumer see the full set. Returns ResolvedGraph plus
// the dirty pending refs (caller persists those — cached refs survive in DB
// via FK; only fresh refs from re-parse need INSERT).
//
// G6 v3 (schema 1.5): the "load pending refs back" step is the v1/v2 fix.
// Previously cached files contributed nodes only, so cached-side cross-file
// pending refs were silently dropped — manifesting as the -92 K calls
// regression on go-stablenet.
func runGoPipelineIncremental(srcRoot string, dirtyFiles, cachedFiles []string,
	store persist.Store, log *slog.Logger) (*parse.ResolvedGraph, []persist.PendingRefRow, int, error) {
	p := gop.New(srcRoot)
	if pkgs, err := detect.GoPackages(srcRoot); err == nil {
		p.SetPackages(pkgs)
	} else {
		log.Warn("Go packages typed-load failed; concurrency falls back to AST-only", "err", err)
	}
	results := []*parse.ParseResult{}
	errs := 0
	for _, rel := range dirtyFiles {
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
	dirtyPending := collectPendingRefs(results)
	for _, rel := range cachedFiles {
		nodes, err := store.NodesByFilePath(rel)
		if err != nil {
			return nil, nil, errs, fmt.Errorf("reload go nodes for %s: %w", rel, err)
		}
		refs, err := store.PendingRefsByFilePath(rel)
		if err != nil {
			return nil, nil, errs, fmt.Errorf("reload go pending_refs for %s: %w", rel, err)
		}
		results = append(results, &parse.ParseResult{
			Path: rel, Nodes: nodes, Pending: pendingRefsFromRows(refs),
		})
	}
	rg, err := p.Resolve(results)
	return rg, dirtyPending, errs, err
}

// pendingRefsFromRows converts persist.PendingRefRow back into parse.PendingRef
// so synthesised ParseResults match the shape Pass 2 Resolve consumes natively.
// FilePath isn't a parse.PendingRef field — Resolve doesn't need it (the row's
// SrcID identifies the source node directly).
func pendingRefsFromRows(rows []persist.PendingRefRow) []parse.PendingRef {
	if len(rows) == 0 {
		return nil
	}
	out := make([]parse.PendingRef, len(rows))
	for i, r := range rows {
		out[i] = parse.PendingRef{
			SrcID:       r.SrcID,
			EdgeType:    types.EdgeType(r.EdgeType),
			TargetQName: r.TargetQName,
			HintFile:    r.HintFile,
			Line:        r.Line,
		}
	}
	return out
}

// runTSPipelineIncremental mirrors runGoPipelineIncremental for TypeScript.
func runTSPipelineIncremental(srcRoot string, dirtyFiles, cachedFiles []string,
	store persist.Store, log *slog.Logger) (*parse.ResolvedGraph, []persist.PendingRefRow, int, error) {
	p := tsp.New(srcRoot)
	results := []*parse.ParseResult{}
	errs := 0
	for _, rel := range dirtyFiles {
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
	dirtyPending := collectPendingRefs(results)
	for _, rel := range cachedFiles {
		nodes, err := store.NodesByFilePath(rel)
		if err != nil {
			return nil, nil, errs, fmt.Errorf("reload ts nodes for %s: %w", rel, err)
		}
		refs, err := store.PendingRefsByFilePath(rel)
		if err != nil {
			return nil, nil, errs, fmt.Errorf("reload ts pending_refs for %s: %w", rel, err)
		}
		results = append(results, &parse.ParseResult{
			Path: rel, Nodes: nodes, Pending: pendingRefsFromRows(refs),
		})
	}
	rg, err := p.Resolve(results)
	return rg, dirtyPending, errs, err
}

// runSolPipelineIncremental mirrors runGoPipelineIncremental for Solidity.
// Returns the parser instance for caller use (xlang ABI source).
func runSolPipelineIncremental(srcRoot string, dirtyFiles, cachedFiles []string,
	store persist.Store, log *slog.Logger) (*parse.ResolvedGraph, []persist.PendingRefRow, int, *solp.Parser, error) {
	p := solp.New(srcRoot)
	results := []*parse.ParseResult{}
	errs := 0
	for _, rel := range dirtyFiles {
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
	dirtyPending := collectPendingRefs(results)
	for _, rel := range cachedFiles {
		nodes, err := store.NodesByFilePath(rel)
		if err != nil {
			return nil, nil, errs, p, fmt.Errorf("reload sol nodes for %s: %w", rel, err)
		}
		refs, err := store.PendingRefsByFilePath(rel)
		if err != nil {
			return nil, nil, errs, p, fmt.Errorf("reload sol pending_refs for %s: %w", rel, err)
		}
		results = append(results, &parse.ParseResult{
			Path: rel, Nodes: nodes, Pending: pendingRefsFromRows(refs),
		})
	}
	rg, err := p.Resolve(results)
	return rg, dirtyPending, errs, p, err
}

// extractBlobsForFiles is a filtered version of extractBlobs: only emits blobs
// for nodes whose FilePath is in the wanted set. Used during incremental
// builds to avoid re-reading every cached file's source.
func extractBlobsForFiles(root string, nodes []types.Node, wanted []string) map[string][]byte {
	if len(wanted) == 0 {
		return map[string][]byte{}
	}
	wantSet := make(map[string]bool, len(wanted))
	for _, p := range wanted {
		wantSet[p] = true
	}
	filtered := make([]types.Node, 0)
	for _, n := range nodes {
		if wantSet[n.FilePath] {
			filtered = append(filtered, n)
		}
	}
	return extractBlobs(root, filtered)
}

// buildManifestSkeleton fills the non-Files portion of the new manifest. The
// Files block is set separately so cold and incremental paths share the
// per-file population logic.
func buildManifestSkeleton(opt Options, goCount, tsCount, solCount int,
	g *graph.Graph, pkgTree *cluster.PkgTree, parseErrs int) persist.Manifest {
	return persist.Manifest{
		SchemaVersion:  SchemaVersion,
		CKGVersion:     opt.CKGVersion,
		BuildTimestamp: time.Now().UTC().Format(time.RFC3339),
		SrcRoot:        opt.SrcRoot,
		Languages:      map[string]int{"go": goCount, "ts": tsCount, "sol": solCount},
		Stats: map[string]int{
			"nodes":          len(g.Nodes),
			"edges":          len(g.Edges),
			"pkg_tree_edges": len(pkgTree.Edges),
		},
		ParseErrorsCount: parseErrs,
		ClusteringStatus: "ok",
	}
}

// buildFileEntries assembles the FileEntry slice for the new manifest. Each
// dirty / cached file gets one entry with current SHA + cache key + the IDs
// it produced (looked up from the merged graph). Removed files are excluded.
func buildFileEntries(decisions CacheDecisions, nodes []types.Node, edges []types.Edge) []persist.FileEntry {
	// Build path → []nodeID and path → []edgeID indexes once.
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
	out := make([]persist.FileEntry, 0, len(decisions.Decisions))
	for _, d := range decisions.Decisions {
		if d.Class == classRemoved {
			continue
		}
		out = append(out, persist.FileEntry{
			Path:          d.Path,
			Language:      d.Language,
			SHA256:        d.SHA256,
			CacheKey:      d.CacheKey,
			MTime:         d.MTime,
			ParserVersion: d.ParserVersion,
			NodeIDs:       nodesByPath[d.Path],
			EdgeIDs:       edgesByPath[d.Path],
		})
	}
	return out
}

