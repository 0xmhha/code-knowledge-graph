package persist

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/pkg/bm25"
)

// Migrate creates tables if they don't already exist, then applies any
// schema 1.6 → 1.7 column additions (dispatch_kind on edges + pending_refs)
// idempotently.
//
// Why not bake every additive change into schema.sql alone: SQLite executes
// CREATE TABLE IF NOT EXISTS as a no-op when the table already exists,
// even if the existing definition is missing newer columns. Pre-1.7 DBs
// that already have an `edges` table without `dispatch_kind` would silently
// keep the old shape; explicit ALTER TABLE here brings them up to current.
//
// Schema 1.8 → 1.9 (W1 of schema-1.9-spec): no DDL change — TypeScript
// HTTP server endpoint detection reuses the existing NodeEndpoint + edge
// `listens_on` rows. The schema version recognition lives in
// buildpipe/cache.go.SchemaVersion; ManifestUsable forces a cold rebuild
// on first 1.9 run so pre-1.9 DBs don't carry forward a stale TS graph.
// This Migrate stub is intentionally empty for 1.9 — kept as a documented
// landing site for future 1.9 column additions (e.g. W2 http_calls suffix
// match needs no new column, W4 Topic node reuses NodeMessageType-shaped
// columns).
//
// Schema 1.9 → 1.10 (within-language semantics Phase 4, 2026-05-11): no
// DDL change — the new enum literals (`NodeAwaitPoint`, `EdgeAwaits`,
// `EdgeOverrides`) ride the existing `nodes.type` / `edges.type` TEXT
// columns. The bump is purely a cache-key contributor so pre-1.10 DBs
// drop into the cold path on first build with the new binary; once the
// Phase 5 detectors land they will populate the slot under the same
// schema version. No Migrate work required here.
func (s *sqliteStore) Migrate() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	if err := ensureDispatchKindColumn(s.db, "edges"); err != nil {
		return fmt.Errorf("migrate dispatch_kind on edges: %w", err)
	}
	if err := ensureDispatchKindColumn(s.db, "pending_refs"); err != nil {
		return fmt.Errorf("migrate dispatch_kind on pending_refs: %w", err)
	}
	// W-C W11 V7 (2026-05-19): nodes.attrs JSON blob carries every
	// types.Node marker that doesn't have its own column. Pre-1.9
	// DBs need ALTER ADD.
	if err := ensureAttrsColumn(s.db); err != nil {
		return fmt.Errorf("migrate attrs on nodes: %w", err)
	}
	if err := ensureSearchTokensColumn(s.db); err != nil {
		return fmt.Errorf("migrate search_tokens on nodes: %w", err)
	}
	if err := ensureCanonicalIDColumn(s.db); err != nil {
		return fmt.Errorf("migrate canonical_id on nodes: %w", err)
	}
	if err := ensureSimpleNameColumn(s.db); err != nil {
		return fmt.Errorf("migrate simple_name on nodes: %w", err)
	}
	return nil
}

// simpleName returns the last dotted segment of a qualified name (the symbol's
// short name), or the whole string when undotted. Mirrors simpleName() in
// internal/parse/golang/resolve.go so suffix lookups match the same candidates
// the resolver does.
func simpleName(qname string) string {
	if i := strings.LastIndex(qname, "."); i >= 0 {
		return qname[i+1:]
	}
	return qname
}

// ensureSimpleNameColumn ALTER-adds nodes.simple_name on pre-1.22 DBs and
// ensures its index exists. Idempotent. The column is populated at write time
// (InsertNodes); existing rows are repopulated by the cold rebuild the
// SchemaVersion 1.22 bump forces, so no backfill is needed here.
func ensureSimpleNameColumn(db *sql.DB) error {
	has, err := columnExists(db, "nodes", "simple_name")
	if err != nil {
		return err
	}
	if !has {
		if _, err := db.Exec(`ALTER TABLE nodes ADD COLUMN simple_name TEXT`); err != nil {
			return fmt.Errorf("alter nodes add simple_name: %w", err)
		}
	}
	// COLLATE NOCASE so the index serves the case-insensitive suffix lookups
	// (the old LIKE was case-insensitive); the equi-joins compare with the
	// same collation.
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_nodes_simple_name ON nodes(simple_name COLLATE NOCASE)`); err != nil {
		return fmt.Errorf("create idx_nodes_simple_name: %w", err)
	}
	return nil
}

// columnExists reports whether table has a column of the given name.
func columnExists(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, fmt.Errorf("table_info(%s): %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, fmt.Errorf("scan table_info: %w", err)
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate table_info: %w", err)
	}
	return false, nil
}

// ensureCanonicalIDColumn ALTER-adds nodes.canonical_id on pre-1.16 DBs.
// Idempotent (PRAGMA table_info detect-then-no-op), mirroring the attrs and
// search_tokens migrations.
func ensureCanonicalIDColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(nodes)`)
	if err != nil {
		return fmt.Errorf("table_info(nodes): %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info: %w", err)
		}
		if name == "canonical_id" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table_info: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE nodes ADD COLUMN canonical_id TEXT`); err != nil {
		return fmt.Errorf("alter nodes add canonical_id: %w", err)
	}
	return nil
}

// ensureAttrsColumn ALTER-adds nodes.attrs on pre-1.9 DBs.
// Idempotent: detects the column via PRAGMA table_info and no-ops
// when already present.
func ensureAttrsColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(nodes)`)
	if err != nil {
		return fmt.Errorf("table_info(nodes): %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info: %w", err)
		}
		if name == "attrs" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table_info: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE nodes ADD COLUMN attrs TEXT`); err != nil {
		return fmt.Errorf("alter nodes add attrs: %w", err)
	}
	return nil
}

// ensureSearchTokensColumn ALTER-adds nodes.search_tokens on pre-1.13 DBs.
func ensureSearchTokensColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(nodes)`)
	if err != nil {
		return fmt.Errorf("table_info(nodes): %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info: %w", err)
		}
		if name == "search_tokens" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table_info: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE nodes ADD COLUMN search_tokens TEXT`); err != nil {
		return fmt.Errorf("alter nodes add search_tokens: %w", err)
	}
	return nil
}

// buildSearchTokens generates space-separated camelCase/snake_case split
// tokens from name and qualified_name. Indexed by FTS5 so prefix queries
// like "deposit*" match "HandleDeposit" via the split token "deposit".
func buildSearchTokens(name, qname string) string {
	seen := map[string]struct{}{}
	var tokens []string
	for _, input := range []string{name, qname} {
		for _, t := range bm25.Tokenize(input) {
			if _, dup := seen[t]; dup {
				continue
			}
			seen[t] = struct{}{}
			tokens = append(tokens, t)
		}
	}
	return strings.Join(tokens, " ")
}

// ensureDispatchKindColumn ALTER-adds <table>.dispatch_kind on schema-1.6
// DBs. Idempotent: detects the column via PRAGMA table_info and no-ops when
// already present. Used by Migrate() to bring forward both the edges and
// pending_refs tables.
//
// table is interpolated directly because PRAGMA / ALTER TABLE forbid
// parameter binding on identifiers; callers MUST pass a hard-coded literal
// (validated by the switch below as a defence-in-depth measure).
func ensureDispatchKindColumn(db *sql.DB, table string) error {
	switch table {
	case "edges", "pending_refs":
		// allowed
	default:
		return fmt.Errorf("ensureDispatchKindColumn: unknown table %q", table)
	}
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return fmt.Errorf("table_info(%s): %w", table, err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("scan table_info: %w", err)
		}
		if name == "dispatch_kind" {
			return nil // already present
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table_info: %w", err)
	}
	if _, err := db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN dispatch_kind TEXT`); err != nil {
		return fmt.Errorf("alter %s add dispatch_kind: %w", table, err)
	}
	return nil
}
