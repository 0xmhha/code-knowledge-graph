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
	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

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
	FindSymbol(name, lang string, exact bool) ([]types.Node, error)
	NodesByIDs(ids []string) ([]types.Node, error)
	QueryNodes(parent string, limit int) ([]types.Node, error)
	DistinctFilePaths(language string) ([]string, error)

	// Edge queries
	QueryEdgesByType(t string) ([]types.Edge, error)
	QueryEdgesForNodes(ids []string) ([]types.Edge, error)

	// Traversal
	NeighborhoodByQname(qname string, depth int, reverse bool, edgeTypes ...string) ([]types.Node, []types.Edge, error)
	SubgraphByQname(qname string, depth int) ([]types.Node, []types.Edge, error)

	// Search
	Search(q string, limit int) ([]types.Node, error)
	SearchFTS(q string, limit int) ([]types.Node, error)

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
