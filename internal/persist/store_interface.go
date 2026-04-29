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
}

// StoreWriter is the write surface used by buildpipe to materialise a graph
// end-to-end (Migrate → Insert* → RebuildFTS → SetManifest) plus the static
// export hatch used by `ckg export-static`.
//
// ExportChunked is included here (rather than as a concrete-only method)
// because exporting to chunked JSON is a write-style lifecycle operation
// every backend should implement — operators of any backend (SQLite, PG,
// etc.) want the static-hosting deliverable.
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

	// Indexing
	RebuildFTS() error

	// Manifest
	SetManifest(m Manifest) error

	// Static export (chunked JSON, spec §6.6)
	ExportChunked(outDir string, nodeChunkSize, edgeChunkSize int) error
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
