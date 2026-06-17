// Package persist defines storage interfaces (StoreReader / StoreWriter /
// Store) and a SQLite implementation. Consumers should depend on the
// interfaces, not the concrete sqliteStore — this is the foundation for
// future backends (e.g. PostgreSQL — see docs/spec-ckg-v0.2.md §3,
// scheduled for B2 in docs/WORK-PLAN.md).
//
// The interfaces are split by role (Interface Segregation Principle):
//
//   - StoreReader: read-only surface used by serve / mcp / eval / audit.
//   - StoreWriter: write surface used by buildpipe (full lifecycle).
//   - Store:       composition of both, for callers that need everything.
//
// A single god interface (~25 methods) was rejected because most consumers
// only need a subset; ISP keeps test fakes and future backends focused.
package persist

import (
	"errors"
	"time"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// ErrInvalidMetric is returned by StoreReader.TopNodes when the metric
// argument is not one of the known column names. Sentinel rather than
// a string-typed error so callers (HTTP handler) can map it to 400.
var ErrInvalidMetric = errors.New("invalid metric: want pagerank|usage")

// StoreReader is the read-only surface. serve, mcp, eval and audit all
// depend on this — none of them write to the graph.
type StoreReader interface {
	// Lifecycle
	Close() error

	// Manifest
	GetManifest() (Manifest, error)

	// Hierarchy
	LoadHierarchy(kind string) ([]HierarchyRow, error)

	// Node queries
	// FindSymbol returns nodes matching name (exact or LIKE-suffix per `exact`).
	// See FindSymbolOptions for filter push-down (Language, Kinds).
	FindSymbol(name string, exact bool, opts FindSymbolOptions) ([]types.Node, error)
	// FindByCanonicalID returns the single node whose canonical_id matches
	// exactly. canonical_id is the globally-unique, import-path-qualified
	// identity (ADR-0001), so the match is unambiguous — unlike FindSymbol's
	// short name, it cannot collide across packages. Returns found=false (nil
	// error) when nothing matches or canonicalID is empty.
	FindByCanonicalID(canonicalID string) (types.Node, bool, error)
	NodesByIDs(ids []string) ([]types.Node, error)
	QueryNodes(parent string, limit int) ([]types.Node, error)
	// TopNodes returns the top-N nodes ranked by metric, descending.
	// Designed for the viewer's boot view: a meaningful initial seed where
	// hub functions/methods/types appear naturally so 1-hop expansion shows
	// real call/import structure rather than 37 disconnected packages.
	//
	// metric ∈ {"pagerank", "usage"} — values map to the nodes.pagerank and
	// nodes.usage_score columns respectively. Unknown metric → ErrInvalidMetric.
	// Result is sorted DESC by the chosen column, ties broken by id ASC for
	// determinism. Limit ≤0 is normalised by callers (HTTP layer caps).
	//
	// excludeTypes (variadic) lets callers drop irrelevant node types from
	// the boot seed without re-fetching client-side. The motivating case is
	// the viewer: with 178 git Commit nodes outranking real symbols by
	// pagerank, ~52% of the top-200 boot was Commit nodes, whose only
	// outgoing edge type (`changed_in`) is off by default — so the canvas
	// rendered Commit halos with no visible edges. Pass excludeTypes=
	// []string{"Commit"} to keep boot focused on symbols. No-op when empty.
	TopNodes(metric string, limit int, excludeTypes ...string) ([]types.Node, error)
	DistinctFilePaths(language string) ([]string, error)

	// Edge queries
	QueryEdgesByType(t string) ([]types.Edge, error)
	QueryEdgesForNodes(ids []string) ([]types.Edge, error)
	// EdgeCountsByType returns total edge count per edge type across the
	// entire graph (no node filter). Used by viewer Track D to show G1..G6
	// distribution next to each pill so users can read "G4 has 19 edges
	// total" at a glance — without it, toggling a sparse axis looks dead
	// because the canvas barely changes. Result is `map[edge_type] = count`.
	EdgeCountsByType() (map[string]int, error)

	// Traversal
	NeighborhoodByQname(qname string, depth int, reverse bool, edgeTypes ...string) ([]types.Node, []types.Edge, error)
	SubgraphByQname(qname string, depth int) ([]types.Node, []types.Edge, error)

	// Search
	Search(q string, limit int) ([]types.Node, error)
	// SearchWithOpts is Search with explicit SearchFTSOptions. Adds
	// AND-mode and Language filtering to the routed search path.
	// Options apply on the FTS branch only; the CJK substring fallback
	// ignores them (substring matching has no multi-token semantics).
	// Returns the same []types.Node shape as Search so callers can
	// migrate incrementally without touching their result handling.
	SearchWithOpts(q string, limit int, opts SearchFTSOptions) ([]types.Node, error)
	// SearchFTS returns FTS matches with BM25-derived relevance scores.
	// See SearchHit for the meaning of Score (normalized) vs RawScore.
	// See SearchFTSOptions for filter push-down + Mode.
	SearchFTS(q string, limit int, opts SearchFTSOptions) ([]SearchHit, error)

	// Source bodies
	GetBlob(id string) ([]byte, error)

	// Per-file lookups (A3 incremental cache, schema 1.2). Used by
	// buildpipe to load nodes/edges/blobs for files whose content hash
	// matched the previous manifest — those rows are reused as-is rather
	// than re-parsing.
	NodesByFilePath(path string) ([]types.Node, error)
	EdgesByFilePath(path string) ([]types.Edge, error)
	BlobsByFilePath(path string) (map[string][]byte, error)
	// PendingRefsByFilePath: G6 v3 partial-cache rebuild reads cached files'
	// unresolved cross-file refs back so Pass 2 Resolve sees the cold-equivalent
	// input. Schema 1.5.
	PendingRefsByFilePath(path string) ([]PendingRefRow, error)

	// ReverseDepsForFiles returns every cached file path that has pending_refs
	// targeting a qualified_name defined in any of dirtyPaths. Used by C1
	// (reverse-reference invalidation) to find which cached files need their
	// pending_refs re-resolved when dirty files change their exported symbols.
	// MUST be called BEFORE deleting dirty nodes — the lookup joins
	// pending_refs to nodes still in DB. Returns nil when dirtyPaths is empty.
	ReverseDepsForFiles(dirtyPaths []string) ([]string, error)

	// Static export (chunked JSON, spec §6.6). On StoreReader rather than
	// StoreWriter because ExportChunked only reads from the store and writes
	// JSON to disk — its sole caller (cmd/ckg/export_static.go) opens via
	// OpenReadOnly, which proves it doesn't need write access to the DB.
	ExportChunked(outDir string, nodeChunkSize, edgeChunkSize int) error

	// AmbiguousMetaNodes returns Hunk + Commit nodes whose confidence is
	// AMBIGUOUS — the §11.3 unreachable-history track populated by
	// LoadUnreachableHunks. Powers the viewer's Recovery panel; deliberately
	// scoped to meta-node types so other AMBIGUOUS rows (e.g. multi-candidate
	// TS resolutions on Function nodes) don't pollute the recovery view.
	// Returns nil + nil when no AMBIGUOUS rows exist (fresh graph).
	AmbiguousMetaNodes() ([]types.Node, error)

	// AllNodes / AllEdges return the full graph. Added for `ckg validate`
	// which reconstructs the in-memory graph from a built DB so it can
	// run validators (schema, future LLM) against persisted state. Avoid
	// these on huge graphs in tight loops; they are intentionally
	// streaming-unaware (callers want everything in memory).
	AllNodes() ([]types.Node, error)
	AllEdges() ([]types.Edge, error)

	// GetNodePRs returns every PR breadcrumb recorded against nodeID
	// whose merge timestamp is strictly before cutoff (ckg-NEW-3
	// temporal slicing). Pass time.Time{} (zero value) to disable the
	// cutoff and return the full history.
	//
	// Order: descending by merge timestamp — most recent first, so
	// "show me the last N changes around this symbol" requires no
	// client-side sort. Empty slice (not error) when the node has no
	// recorded PRs or every match was filtered out by the cutoff.
	//
	// See pkg/types.PRRef for the record schema and the build-time
	// derivation (internal/buildpipe.ScanPRHistory).
	GetNodePRs(nodeID string, cutoff time.Time) ([]types.PRRef, error)
}

// StoreWriter is the write surface used by buildpipe to materialise a graph
// end-to-end (Migrate → Insert* → RebuildFTS → SetManifest).
type StoreWriter interface {
	// Lifecycle
	Close() error
	Migrate() error

	// Bulk insert
	InsertNodes(nodes []types.Node) error
	InsertEdges(edges []types.Edge) error
	InsertBlobs(blobs map[string][]byte) error
	InsertPkgTreeFromCluster(edges []cluster.PersistClusterEdge) error
	InsertTopicTree(t TopicTreeInput) error
	// InsertPendingRefs: G6 v3 — cold path persists every Pass 1 unresolved
	// cross-file ref so the next partial build can replay Pass 2 over a
	// merged dirty + cached input. Schema 1.5.
	InsertPendingRefs(refs []PendingRefRow) error

	// InsertNodePRs writes the PR breadcrumb map (ckg-NEW-2, schema
	// 1.12). Keyed by node ID; the slice value is the full list of
	// PRs whose merge commit overlapped the node's source range.
	// Idempotent — node_prs has PRIMARY KEY (node_id, number), so
	// re-runs with INSERT OR REPLACE rewrite the existing rows.
	InsertNodePRs(byNode map[string][]types.PRRef) error

	// Per-file delete (A3 incremental cache). Drops every node whose
	// file_path matches; FK ON DELETE CASCADE wipes the dependent edges
	// and blobs in the same statement. Caller is responsible for then
	// re-inserting the new parse output.
	DeleteNodesByFilePath(path string) error

	// Per-type edge delete (A3 incremental cache). Used to clear
	// cross-language edges (e.g. binds_to) before they are recomputed —
	// they have no FilePath so the per-file delete cannot reach them.
	DeleteEdgesByType(t string) error

	// Indexing
	RebuildFTS() error

	// Manifest
	SetManifest(m Manifest) error
}

// Store is the union of the read and write surfaces — for callers (e.g.
// buildpipe) that need both. Embedded composition keeps the role surfaces
// reusable in isolation.
type Store interface {
	StoreReader
	StoreWriter
}

// Compile-time assertions that the SQLite implementation satisfies all
// three interfaces. If any of these fail to compile, the interface and
// the concrete struct have drifted — fix the struct or the interface,
// NOT the assertion.
var (
	_ StoreReader = (*sqliteStore)(nil)
	_ StoreWriter = (*sqliteStore)(nil)
	_ Store       = (*sqliteStore)(nil)
)
