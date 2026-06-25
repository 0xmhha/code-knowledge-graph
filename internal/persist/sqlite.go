package persist

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// sqliteStore wraps a SQLite database for the CKG graph. It is the concrete
// implementation behind the Store / StoreReader / StoreWriter interfaces
// (see store_interface.go). The struct is unexported because consumers
// should depend on the interfaces — the only way to obtain an instance is
// via Open / OpenReadOnly, both of which return through the interface
// boundary in practice (callers use `:=`).
//
// The receiver methods are split across files by axis:
//   - sqlite.go        (this file) — lifecycle (Open/Close)
//   - sqlite_migrate.go — schema migration + ensure-column helpers
//   - sqlite_writer.go  — Insert*/Delete* + RebuildFTS + writer-side types
//   - sqlite_reader.go  — Get/Query/Find/Neighborhood/Subgraph + HierarchyRow
//   - sqlite_fts.go     — Search/SearchFTS/SearchSubstr + FTS rewriter
//   - sqlite_helpers.go — placeholders/scan*/mapKeys/anys + nodeColumns const
//
// All files are in the same `persist` package so they share the unexported
// sqliteStore receiver.
type sqliteStore struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite file at path.
//
// PRAGMAs are passed via DSN so modernc.org/sqlite applies them per-connection.
// This is required because PRAGMA foreign_keys / journal_mode are connection-scoped:
// setting them once via Migrate() would not propagate to other pooled connections,
// leaving FK constraints unenforced and WAL inactive on most queries.
func Open(path string) (*sqliteStore, error) {
	// busy_timeout makes a contended connection wait instead of failing
	// immediately with SQLITE_BUSY (concurrent build writers + readers on WAL).
	// synchronous=NORMAL is the recommended durability level under WAL: safe
	// across app crashes, only a power loss can lose the last commit — an
	// acceptable trade for a rebuildable graph, and a meaningful write speedup.
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %s: %w", path, err)
	}
	return &sqliteStore{db: db}, nil
}

// OpenReadOnly opens a SQLite file in read-only mode (used by serve/mcp).
// FK pragma is enforced per-connection via DSN; WAL/synchronous are omitted
// because read-only mode cannot mutate journal state. busy_timeout still
// helps a reader wait out a concurrent checkpoint instead of erroring.
func OpenReadOnly(path string) (*sqliteStore, error) {
	dsn := path + "?mode=ro&immutable=1&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite ro at %s: %w", path, err)
	}
	return &sqliteStore{db: db}, nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }
