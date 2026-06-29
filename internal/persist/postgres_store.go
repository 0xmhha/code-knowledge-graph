package persist

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// pgStore is the PostgreSQL implementation of Store / StoreReader / StoreWriter.
// All methods use the pgxpool.Pool for connection pooling. The pool is safe for
// concurrent use; no external locking is required.
//
// Deprecated: the PostgreSQL backend is deprecated (ADR-0003). SQLite is the sole
// maintained backend; pgStore is kept compiling but not at schema/feature parity
// (e.g. no canonical_id / simple_name columns). Do not extend it.
//
// ro marks stores opened via OpenPostgresReadOnly. Write methods panic when ro
// is true so that read-only callers can't accidentally mutate the graph.
type pgStore struct {
	pool *pgxpool.Pool
	ro   bool
}

// Compile-time assertions: pgStore must satisfy all three interfaces.
var (
	_ StoreReader = (*pgStore)(nil)
	_ StoreWriter = (*pgStore)(nil)
	_ Store       = (*pgStore)(nil)
)

// pgStoreSchema is the DDL applied by Migrate(). Column names match the SQLite
// schema (type, qualified_name, source, etc.) so that buildpipe and tests that
// depend on wire-shape parity don't need adaptation.
//
// search_vector (TSVECTOR) is populated lazily by RebuildFTS() — not maintained
// by trigger — which mirrors the SQLite FTS5 content-table model: the caller
// explicitly calls RebuildFTS() after all inserts.
//
// topic_tree.parent_id is TEXT NOT NULL DEFAULT ” (PostgreSQL forbids NULL in
// PRIMARY KEY components). Callers that pass nil parent → ” on insert, and
// read ” → "" on scan.
const pgStoreSchema = `
CREATE TABLE IF NOT EXISTS nodes (
    id             TEXT PRIMARY KEY,
    type           TEXT NOT NULL,
    name           TEXT NOT NULL,
    qualified_name TEXT NOT NULL,
    file_path      TEXT NOT NULL DEFAULT '',
    start_line     INTEGER NOT NULL DEFAULT 0,
    end_line       INTEGER NOT NULL DEFAULT 0,
    start_byte     INTEGER NOT NULL DEFAULT 0,
    end_byte       INTEGER NOT NULL DEFAULT 0,
    language       TEXT NOT NULL DEFAULT '',
    visibility     TEXT,
    signature      TEXT,
    doc_comment    TEXT,
    complexity     INTEGER,
    in_degree      INTEGER NOT NULL DEFAULT 0,
    out_degree     INTEGER NOT NULL DEFAULT 0,
    pagerank       DOUBLE PRECISION NOT NULL DEFAULT 0,
    usage_score    DOUBLE PRECISION NOT NULL DEFAULT 0,
    confidence     TEXT NOT NULL DEFAULT 'EXTRACTED',
    sub_kind       TEXT,
    search_vector  TSVECTOR
);
CREATE INDEX IF NOT EXISTS idx_nodes_qname ON nodes(qualified_name);
CREATE INDEX IF NOT EXISTS idx_nodes_file  ON nodes(file_path);
CREATE INDEX IF NOT EXISTS idx_nodes_type  ON nodes(type);
CREATE INDEX IF NOT EXISTS idx_nodes_fts   ON nodes USING GIN(search_vector);

CREATE TABLE IF NOT EXISTS edges (
    id            BIGSERIAL PRIMARY KEY,
    src           TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    dst           TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    type          TEXT NOT NULL,
    file_path     TEXT,
    line          INTEGER,
    count         INTEGER NOT NULL DEFAULT 1,
    confidence    TEXT NOT NULL DEFAULT 'EXTRACTED',
    dispatch_kind TEXT
);
CREATE INDEX IF NOT EXISTS idx_edges_src  ON edges(src);
CREATE INDEX IF NOT EXISTS idx_edges_dst  ON edges(dst);
CREATE INDEX IF NOT EXISTS idx_edges_type ON edges(type);

CREATE TABLE IF NOT EXISTS blobs (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    source  BYTEA NOT NULL
);

CREATE TABLE IF NOT EXISTS pkg_tree (
    parent_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    child_id  TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    level     INTEGER NOT NULL,
    PRIMARY KEY (parent_id, child_id)
);
CREATE INDEX IF NOT EXISTS idx_pkg_parent ON pkg_tree(parent_id);

CREATE TABLE IF NOT EXISTS topic_tree (
    parent_id   TEXT NOT NULL DEFAULT '',
    child_id    TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    resolution  INTEGER NOT NULL,
    topic_label TEXT,
    PRIMARY KEY (child_id, resolution, parent_id)
);

CREATE TABLE IF NOT EXISTS manifest (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pending_refs (
    file_path     TEXT NOT NULL,
    src_id        TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_qname  TEXT NOT NULL,
    edge_type     TEXT NOT NULL,
    line          INTEGER NOT NULL,
    hint_file     TEXT,
    dispatch_kind TEXT,
    PRIMARY KEY (file_path, src_id, target_qname, edge_type, line)
);
CREATE INDEX IF NOT EXISTS idx_pending_refs_file ON pending_refs(file_path);

-- ckg-NEW-2/3/4 (schema 1.12): PR breadcrumb. Mirrors the SQLite
-- node_prs table (see internal/persist/schema.sql). merged_at uses
-- TIMESTAMP WITH TIME ZONE on the PG side so the column type carries
-- semantics rather than relying on text ordering; the Go layer still
-- normalises to UTC before binding.
CREATE TABLE IF NOT EXISTS node_prs (
    node_id    TEXT    NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    number     INTEGER NOT NULL,
    title      TEXT,
    summary    TEXT,
    base_sha   TEXT,
    head_sha   TEXT,
    merged_at  TIMESTAMP WITH TIME ZONE NOT NULL,
    repo       TEXT,
    PRIMARY KEY (node_id, number)
);
CREATE INDEX IF NOT EXISTS idx_node_prs_merged ON node_prs(merged_at DESC);
`

// pgNodeColumns is the SELECT column list for every node query. COALESCE
// normalises NULL-able columns to empty string / 0 so scanPGNodes can scan
// into value types directly without pgx NullString gymnastics.
const pgNodeColumns = `id, type, name, qualified_name, file_path,
    start_line, end_line, start_byte, end_byte, language,
    COALESCE(visibility,''), COALESCE(signature,''), COALESCE(doc_comment,''),
    COALESCE(complexity,0), in_degree, out_degree, pagerank, usage_score,
    confidence, COALESCE(sub_kind,'')`

// background is the context used by all internal Store operations. Using
// context.Background() (rather than a per-request context) is correct here
// because the store is long-lived server-side infrastructure; individual HTTP
// handlers add their own cancellation atop this.
var background = context.Background()

// ──────────────────────────────────────────────────────────────────────────────
// Factory functions
// ──────────────────────────────────────────────────────────────────────────────

// OpenPostgres opens PostgreSQL for read/write. Used by incremental builds,
// serve, and mcp. The pool is configured with pgxpool defaults (max 4 conns
// on standard hardware).
func OpenPostgres(dsn string) (Store, error) {
	pool, err := pgxpool.New(background, dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := pool.Ping(background); err != nil {
		pool.Close()

		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &pgStore{pool: pool}, nil
}

// OpenPostgresReadOnly opens PostgreSQL in read-only mode. The underlying pool
// is identical — PostgreSQL doesn't have a read-only pool option — but write
// methods panic to catch unintended writes at the call site rather than at the
// server. Callers that only need StoreReader should use this.
func OpenPostgresReadOnly(dsn string) (StoreReader, error) {
	pool, err := pgxpool.New(background, dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres ro: %w", err)
	}
	if err := pool.Ping(background); err != nil {
		pool.Close()

		return nil, fmt.Errorf("ping postgres ro: %w", err)
	}
	return &pgStore{pool: pool, ro: true}, nil
}

// OpenPostgresCold wipes all data in FK-safe order via TRUNCATE … CASCADE,
// then calls Migrate() to ensure the schema is current. Equivalent to
// os.Remove(graph.db) + Open() for the SQLite cold path.
func OpenPostgresCold(dsn string) (Store, error) {
	pool, err := pgxpool.New(background, dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres cold: %w", err)
	}
	if err := pool.Ping(background); err != nil {
		pool.Close()

		return nil, fmt.Errorf("ping postgres cold: %w", err)
	}
	s := &pgStore{pool: pool}
	// Wipe data first (tables may not exist yet on very first run — ignore error).
	// TRUNCATE … CASCADE handles FK ordering automatically.
	_, _ = pool.Exec(background,
		`TRUNCATE TABLE node_prs, pending_refs, topic_tree, pkg_tree, blobs, edges, nodes, manifest CASCADE`)
	if err := s.Migrate(); err != nil {
		pool.Close()

		return nil, fmt.Errorf("migrate postgres cold: %w", err)
	}
	return s, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Lifecycle
// ──────────────────────────────────────────────────────────────────────────────

// Close releases the connection pool.
func (s *pgStore) Close() error {
	s.pool.Close()

	return nil
}

// Migrate applies the schema DDL (CREATE TABLE/INDEX IF NOT EXISTS). Safe to
// call on a populated database — existing objects are not affected.
func (s *pgStore) Migrate() error {
	if _, err := s.pool.Exec(background, pgStoreSchema); err != nil {
		return fmt.Errorf("apply pg schema: %w", err)
	}
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Manifest
// ──────────────────────────────────────────────────────────────────────────────

// SetManifest serialises m to JSON and upserts it under the key 'manifest'.
// One JSON blob per manifest (unlike SQLite's multi-row kv model) keeps the PG
// implementation simple. GetManifest deserialises it back.
func (s *pgStore) SetManifest(m Manifest) error {
	if s.ro {
		panic("pgStore: SetManifest called on read-only store")
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	_, err = s.pool.Exec(background,
		`INSERT INTO manifest (key, value) VALUES ('manifest', $1)
         ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		string(raw))
	if err != nil {
		return fmt.Errorf("upsert manifest: %w", err)
	}
	return nil
}

// GetManifest reads the JSON manifest blob and deserialises it. Returns an
// empty Manifest (not an error) when the table is empty — callers use the zero
// value as a cold-start signal.
func (s *pgStore) GetManifest() (Manifest, error) {
	var raw string
	err := s.pool.QueryRow(background,
		`SELECT value FROM manifest WHERE key = 'manifest'`).Scan(&raw)
	if err != nil {
		// pgx returns pgx.ErrNoRows on empty result — treat as cold start.
		if err == pgx.ErrNoRows {
			return Manifest{}, nil
		}
		return Manifest{}, fmt.Errorf("get manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	return m, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Bulk write
// ──────────────────────────────────────────────────────────────────────────────

// InsertNodes bulk-inserts nodes using parameterised INSERT … ON CONFLICT (id)
// DO UPDATE SET … (upsert). This handles the incremental path where
// dirty-then-cached nodes may share IDs (e.g. Package nodes shared across
// files). The transactional batch keeps throughput reasonable for large graphs.
func (s *pgStore) InsertNodes(nodes []types.Node) error {
	if s.ro {
		panic("pgStore: InsertNodes called on read-only store")
	}
	if len(nodes) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	const q = `INSERT INTO nodes
        (id, type, name, qualified_name, file_path, start_line, end_line,
         start_byte, end_byte, language, visibility, signature, doc_comment,
         complexity, in_degree, out_degree, pagerank, usage_score, confidence, sub_kind)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
        ON CONFLICT (id) DO UPDATE SET
            type=EXCLUDED.type, name=EXCLUDED.name,
            qualified_name=EXCLUDED.qualified_name, file_path=EXCLUDED.file_path,
            start_line=EXCLUDED.start_line, end_line=EXCLUDED.end_line,
            start_byte=EXCLUDED.start_byte, end_byte=EXCLUDED.end_byte,
            language=EXCLUDED.language, visibility=EXCLUDED.visibility,
            signature=EXCLUDED.signature, doc_comment=EXCLUDED.doc_comment,
            complexity=EXCLUDED.complexity, in_degree=EXCLUDED.in_degree,
            out_degree=EXCLUDED.out_degree, pagerank=EXCLUDED.pagerank,
            usage_score=EXCLUDED.usage_score, confidence=EXCLUDED.confidence,
            sub_kind=EXCLUDED.sub_kind`
	for _, n := range nodes {
		batch.Queue(q,
			n.ID, string(n.Type), n.Name, n.QualifiedName, n.FilePath,
			n.StartLine, n.EndLine, n.StartByte, n.EndByte, n.Language,
			n.Visibility, n.Signature, n.DocComment, n.Complexity,
			n.InDegree, n.OutDegree, n.PageRank, n.UsageScore,
			string(n.Confidence), n.SubKind)
	}
	br := s.pool.SendBatch(background, batch)
	defer func() { _ = br.Close() }()
	for i := range nodes {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("insert node %s: %w", nodes[i].ID, err)
		}
	}
	return br.Close()
}

// InsertEdges bulk-inserts edges (ID==0 — fresh from this build). BIGSERIAL
// assigns DB IDs; we don't read them back because the in-memory slice's IDs
// are not used after persist.
func (s *pgStore) InsertEdges(edges []types.Edge) error {
	if s.ro {
		panic("pgStore: InsertEdges called on read-only store")
	}
	if len(edges) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	const q = `INSERT INTO edges (src, dst, type, file_path, line, count, confidence, dispatch_kind)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`
	for _, e := range edges {
		var fp *string
		if e.FilePath != "" {
			fp = &e.FilePath
		}
		var line *int
		if e.Line != 0 {
			line = &e.Line
		}
		batch.Queue(q, e.Src, e.Dst, string(e.Type), fp, line, e.Count, string(e.Confidence), e.DispatchKind)
	}
	br := s.pool.SendBatch(background, batch)
	defer func() { _ = br.Close() }()
	for i := range edges {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("insert edge %s->%s: %w", edges[i].Src, edges[i].Dst, err)
		}
	}
	return br.Close()
}

// InsertBlobs stores per-node source slices keyed by node ID. ON CONFLICT
// upserts so re-running against an existing DB is idempotent.
func (s *pgStore) InsertBlobs(blobs map[string][]byte) error {
	if s.ro {
		panic("pgStore: InsertBlobs called on read-only store")
	}
	if len(blobs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	const q = `INSERT INTO blobs (node_id, source) VALUES ($1, $2)
        ON CONFLICT (node_id) DO UPDATE SET source = EXCLUDED.source`
	for id, b := range blobs {
		batch.Queue(q, id, b)
	}
	br := s.pool.SendBatch(background, batch)
	defer func() { _ = br.Close() }()
	for id := range blobs {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("insert blob %s: %w", id, err)
		}
	}
	return br.Close()
}

// InsertPkgTreeFromCluster persists package-tree edges. Full replace: existing
// rows are truncated before insert so repeated builds converge.
func (s *pgStore) InsertPkgTreeFromCluster(edges []cluster.PersistClusterEdge) error {
	if s.ro {
		panic("pgStore: InsertPkgTreeFromCluster called on read-only store")
	}
	tx, err := s.pool.Begin(background)
	if err != nil {
		return err
	}
	defer tx.Rollback(background) //nolint:errcheck
	if _, err := tx.Exec(background, `DELETE FROM pkg_tree`); err != nil {
		return err
	}
	if len(edges) == 0 {
		return tx.Commit(background)
	}
	batch := &pgx.Batch{}
	const q = `INSERT INTO pkg_tree (parent_id, child_id, level) VALUES ($1,$2,$3)
        ON CONFLICT (parent_id, child_id) DO UPDATE SET level = EXCLUDED.level`
	for _, e := range edges {
		batch.Queue(q, e.ParentID, e.ChildID, e.Level)
	}
	br := tx.SendBatch(background, batch)
	for i := range edges {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()

			return fmt.Errorf("insert pkg_tree %s->%s: %w", edges[i].ParentID, edges[i].ChildID, err)
		}
	}
	if err := br.Close(); err != nil {
		return err
	}
	return tx.Commit(background)
}

// InsertTopicTree persists multi-resolution Leiden communities. Existing rows
// are dropped first so a full rebuild doesn't accumulate stale communities.
// nil parent_id maps to ” (PG PRIMARY KEY cannot contain NULL).
func (s *pgStore) InsertTopicTree(t TopicTreeInput) error {
	if s.ro {
		panic("pgStore: InsertTopicTree called on read-only store")
	}
	tx, err := s.pool.Begin(background)
	if err != nil {
		return err
	}
	defer tx.Rollback(background) //nolint:errcheck
	if _, err := tx.Exec(background, `DELETE FROM topic_tree`); err != nil {
		return err
	}
	batch := &pgx.Batch{}
	const q = `INSERT INTO topic_tree (parent_id, child_id, resolution, topic_label)
        VALUES ($1,$2,$3,$4) ON CONFLICT (child_id, resolution, parent_id) DO NOTHING`
	for i := 0; i < t.ResolutionsCount(); i++ {
		members := t.ResolutionMembers(i)
		for label, ids := range members {
			for _, id := range ids {
				// parent_id is '' (empty string) for root-level communities.
				// SQLite stores NULL; PostgreSQL PK requires non-NULL → map to ''.
				batch.Queue(q, "", id, i, label)
			}
		}
	}
	br := tx.SendBatch(background, batch)
	// We don't know the exact count upfront (nested iteration), so drain
	// by closing — pgx sends all queued statements on Close.
	if err := br.Close(); err != nil {
		return fmt.Errorf("insert topic_tree batch: %w", err)
	}
	return tx.Commit(background)
}

// InsertPendingRefs bulk-inserts pending cross-file reference rows. ON CONFLICT
// DO NOTHING mirrors the SQLite INSERT OR IGNORE semantics: the PK collision
// is benign (same logical ref emitted twice by cold path).
func (s *pgStore) InsertPendingRefs(refs []PendingRefRow) error {
	if s.ro {
		panic("pgStore: InsertPendingRefs called on read-only store")
	}
	if len(refs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	const q = `INSERT INTO pending_refs
        (file_path, src_id, target_qname, edge_type, line, hint_file, dispatch_kind)
        VALUES ($1,$2,$3,$4,$5,$6,$7)
        ON CONFLICT (file_path, src_id, target_qname, edge_type, line) DO NOTHING`
	for _, r := range refs {
		var hf *string
		if r.HintFile != "" {
			hf = &r.HintFile
		}
		var dk *string
		if r.DispatchKind != "" {
			dk = &r.DispatchKind
		}
		batch.Queue(q, r.FilePath, r.SrcID, r.TargetQName, r.EdgeType, r.Line, hf, dk)
	}
	br := s.pool.SendBatch(background, batch)
	defer func() { _ = br.Close() }()
	for i := range refs {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("insert pending_ref %s→%s: %w", refs[i].SrcID, refs[i].TargetQName, err)
		}
	}
	return br.Close()
}

// InsertNodePRs writes ckg-NEW-2 PR breadcrumbs into PG. ON CONFLICT
// UPDATE rather than DO NOTHING because the rebuild path frequently
// has updated title/summary text for the same (node_id, number) — see
// the SQLite mirror for the rationale.
func (s *pgStore) InsertNodePRs(byNode map[string][]types.PRRef) error {
	if s.ro {
		panic("pgStore: InsertNodePRs called on read-only store")
	}
	if len(byNode) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	const q = `INSERT INTO node_prs
        (node_id, number, title, summary, base_sha, head_sha, merged_at, repo)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
        ON CONFLICT (node_id, number) DO UPDATE SET
            title     = EXCLUDED.title,
            summary   = EXCLUDED.summary,
            base_sha  = EXCLUDED.base_sha,
            head_sha  = EXCLUDED.head_sha,
            merged_at = EXCLUDED.merged_at,
            repo      = EXCLUDED.repo`
	count := 0
	for nodeID, refs := range byNode {
		for _, r := range refs {
			batch.Queue(q, nodeID, r.Number, r.Title, r.Summary,
				r.BaseSHA, r.HeadSHA, r.MergedAtUTC.UTC(), r.Repo)
			count++
		}
	}
	br := s.pool.SendBatch(background, batch)
	defer func() { _ = br.Close() }()
	for i := 0; i < count; i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("insert node_pr: %w", err)
		}
	}
	return br.Close()
}

// GetNodePRs is the PG mirror of sqliteStore.GetNodePRs. Uses native
// TIMESTAMPTZ comparison rather than the SQLite text-ordering trick;
// the API contract (descending, cutoff-exclusive) is identical.
func (s *pgStore) GetNodePRs(nodeID string, cutoff time.Time) ([]types.PRRef, error) {
	sql := `SELECT number,
        COALESCE(title, ''), COALESCE(summary, ''),
        COALESCE(base_sha, ''), COALESCE(head_sha, ''),
        merged_at, COALESCE(repo, '')
        FROM node_prs WHERE node_id = $1`
	args := []any{nodeID}
	if !cutoff.IsZero() {
		sql += ` AND merged_at < $2`
		args = append(args, cutoff.UTC())
	}
	sql += ` ORDER BY merged_at DESC`
	rows, err := s.pool.Query(background, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("node_prs for %s: %w", nodeID, err)
	}
	defer func() { rows.Close() }()
	var out []types.PRRef
	for rows.Next() {
		var r types.PRRef
		if err := rows.Scan(&r.Number, &r.Title, &r.Summary,
			&r.BaseSHA, &r.HeadSHA, &r.MergedAtUTC, &r.Repo); err != nil {
			return nil, fmt.Errorf("scan node_pr: %w", err)
		}
		r.MergedAtUTC = r.MergedAtUTC.UTC()
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate node_prs: %w", err)
	}
	return out, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Per-file delete
// ──────────────────────────────────────────────────────────────────────────────

// DeleteNodesByFilePath drops every node whose file_path matches. FK ON DELETE
// CASCADE wipes dependent edges, blobs, pkg_tree, topic_tree, and pending_refs
// in the same statement — identical contract to the SQLite implementation.
func (s *pgStore) DeleteNodesByFilePath(path string) error {
	if s.ro {
		panic("pgStore: DeleteNodesByFilePath called on read-only store")
	}
	if path == "" {
		return nil
	}
	if _, err := s.pool.Exec(background,
		`DELETE FROM nodes WHERE file_path = $1`, path); err != nil {
		return fmt.Errorf("delete nodes by file_path %q: %w", path, err)
	}
	return nil
}

// DeleteEdgesByType drops every edge of the given type. Used by the incremental
// path to clear always-rebuilt cross-language edges (binds_to, changed_in, blame).
func (s *pgStore) DeleteEdgesByType(t string) error {
	if s.ro {
		panic("pgStore: DeleteEdgesByType called on read-only store")
	}
	if t == "" {
		return nil
	}
	if _, err := s.pool.Exec(background,
		`DELETE FROM edges WHERE type = $1`, t); err != nil {
		return fmt.Errorf("delete edges by type %q: %w", t, err)
	}
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// FTS
// ──────────────────────────────────────────────────────────────────────────────

// RebuildFTS updates the search_vector column for every node using PostgreSQL's
// built-in to_tsvector function. The 'english' dictionary normalises stemming
// and stop-words to match the FTS5 behaviour in SQLite.
func (s *pgStore) RebuildFTS() error {
	if s.ro {
		panic("pgStore: RebuildFTS called on read-only store")
	}
	_, err := s.pool.Exec(background, `
        UPDATE nodes SET search_vector = to_tsvector('english',
            coalesce(name,'') || ' ' || coalesce(qualified_name,'') || ' ' ||
            coalesce(signature,'') || ' ' || coalesce(doc_comment,''))`)
	if err != nil {
		return fmt.Errorf("rebuild fts: %w", err)
	}
	return nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Node queries
// ──────────────────────────────────────────────────────────────────────────────

// FindSymbol returns nodes whose qualified_name matches name. When exact is
// false, a LIKE '%.<name>' suffix match is also accepted. Capped at 100
// rows (same as SQLite implementation).
//
// opts.Language pushes a `language = $N` predicate when non-empty.
// opts.Kinds pushes a `type IN ($N, $N+1, ...)` predicate when non-empty —
// CKG-4 fix paralleling the SQLite path.
func (s *pgStore) FindSymbol(name string, exact bool, opts FindSymbolOptions) ([]types.Node, error) {
	args := []any{}
	q := `SELECT ` + pgNodeColumns + ` FROM nodes WHERE 1=1 `
	if exact {
		q += `AND qualified_name = $1 `
		args = append(args, name)
	} else {
		q += `AND (qualified_name = $1 OR qualified_name LIKE $2) `
		args = append(args, name, "%."+name)
	}
	if opts.Language != "" {
		args = append(args, opts.Language)
		q += fmt.Sprintf(`AND language = $%d `, len(args))
	}
	if len(opts.Kinds) > 0 {
		placeholders := make([]string, 0, len(opts.Kinds))
		for _, k := range opts.Kinds {
			args = append(args, string(k))
			placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
		}
		q += `AND type IN (` + strings.Join(placeholders, ",") + `) `
	}
	q += `LIMIT 100`
	rows, err := s.pool.Query(background, q, args...)
	if err != nil {
		return nil, fmt.Errorf("find symbol %q: %w", name, err)
	}
	defer func() { rows.Close() }()
	return scanPGNodes(rows)
}

// FindByCanonicalID — see StoreReader.FindByCanonicalID. The Postgres schema
// does not yet carry the canonical_id column (it is sqlite-only as of schema
// 1.19; Postgres canonical_id parity is a tracked follow-up — see
// docs/symbol-identity-remaining-work.md). Until that lands, this reports
// not-found rather than referencing a column that does not exist.
func (s *pgStore) FindByCanonicalID(_ string) (types.Node, bool, error) {
	return types.Node{}, false, nil
}

// NodesByIDs fetches nodes by primary key. Empty input yields nil without DB hit.
func (s *pgStore) NodesByIDs(ids []string) ([]types.Node, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	q := `SELECT ` + pgNodeColumns + ` FROM nodes WHERE id = ANY($1)`
	rows, err := s.pool.Query(background, q, ids)
	if err != nil {
		return nil, fmt.Errorf("nodes by %d ids: %w", len(ids), err)
	}
	defer func() { rows.Close() }()
	return scanPGNodes(rows)
}

// QueryNodes returns top-level packages (parent=="") or the children of parent
// via pkg_tree join. Limit caps the result set.
func (s *pgStore) QueryNodes(parent string, limit int) ([]types.Node, error) {
	var rows pgx.Rows
	var err error
	if parent == "" {
		rows, err = s.pool.Query(background,
			`SELECT `+pgNodeColumns+` FROM nodes WHERE type='Package' LIMIT $1`, limit)
	} else {
		rows, err = s.pool.Query(background,
			`SELECT `+pgNodeColumns+` FROM nodes n
            JOIN pkg_tree p ON p.child_id = n.id WHERE p.parent_id = $1 LIMIT $2`,
			parent, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("query nodes (parent=%q): %w", parent, err)
	}
	defer func() { rows.Close() }()
	return scanPGNodes(rows)
}

// TopNodes returns the top-N nodes by the chosen ranking metric, descending.
// Mirrors the SQLite implementation — see sqlite.go TopNodes for rationale.
func (s *pgStore) TopNodes(metric string, limit int, excludeTypes ...string) ([]types.Node, error) {
	col, err := topMetricColumn(metric)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 200
	}
	// Postgres uses $N positional placeholders, not '?'. We start at $1 for
	// each excluded type, and the LIMIT placeholder gets $(len+1).
	whereClause := ""
	args := []any{}
	if len(excludeTypes) > 0 {
		placeholders := make([]string, len(excludeTypes))
		for i, t := range excludeTypes {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args = append(args, t)
		}
		whereClause = " WHERE type NOT IN (" + strings.Join(placeholders, ",") + ")"
	}
	limitPos := fmt.Sprintf("$%d", len(args)+1)
	args = append(args, limit)
	rows, err := s.pool.Query(background,
		`SELECT `+pgNodeColumns+` FROM nodes`+whereClause+` ORDER BY `+col+` DESC, id ASC LIMIT `+limitPos,
		args...)
	if err != nil {
		return nil, fmt.Errorf("top nodes (metric=%q): %w", metric, err)
	}
	defer func() { rows.Close() }()
	return scanPGNodes(rows)
}

// DistinctFilePaths returns the unique file_path values for the given language.
// Defensive empty-string filter mirrors the SQLite implementation.
func (s *pgStore) DistinctFilePaths(language string) ([]string, error) {
	rows, err := s.pool.Query(background,
		`SELECT DISTINCT file_path FROM nodes WHERE language = $1 AND file_path != ''`,
		language)
	if err != nil {
		return nil, fmt.Errorf("distinct file_path (lang=%q): %w", language, err)
	}
	defer func() { rows.Close() }()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan file_path: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate file_path rows: %w", err)
	}
	return out, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Edge queries
// ──────────────────────────────────────────────────────────────────────────────

// QueryEdgesByType returns all edges whose type matches t.
func (s *pgStore) QueryEdgesByType(t string) ([]types.Edge, error) {
	rows, err := s.pool.Query(background,
		`SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence, COALESCE(dispatch_kind,'')
        FROM edges WHERE type = $1`, t)
	if err != nil {
		return nil, fmt.Errorf("query edges by type %q: %w", t, err)
	}
	defer func() { rows.Close() }()
	return scanPGEdges(rows)
}

// AmbiguousMetaNodes mirrors sqlite.go's AmbiguousMetaNodes — returns
// Hunk + Commit rows with confidence='AMBIGUOUS' for the viewer
// Recovery panel.
func (s *pgStore) AmbiguousMetaNodes() ([]types.Node, error) {
	rows, err := s.pool.Query(background, `SELECT `+pgNodeColumns+` FROM nodes
		WHERE confidence = 'AMBIGUOUS' AND type IN ('Hunk', 'Commit')
		ORDER BY type, qualified_name`)
	if err != nil {
		return nil, fmt.Errorf("ambiguous meta nodes: %w", err)
	}
	defer func() { rows.Close() }()
	return scanPGNodes(rows)
}

// AllNodes returns every node in the graph. Used by `ckg validate` for
// in-memory reconstruction. Order is unspecified.
func (s *pgStore) AllNodes() ([]types.Node, error) {
	rows, err := s.pool.Query(background, `SELECT `+pgNodeColumns+` FROM nodes`)
	if err != nil {
		return nil, fmt.Errorf("all nodes: %w", err)
	}
	defer func() { rows.Close() }()
	return scanPGNodes(rows)
}

// AllEdges returns every edge in the graph. Pair with AllNodes for full
// graph reconstruction in `ckg validate`.
func (s *pgStore) AllEdges() ([]types.Edge, error) {
	rows, err := s.pool.Query(background,
		`SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence, COALESCE(dispatch_kind,'') FROM edges`)
	if err != nil {
		return nil, fmt.Errorf("all edges: %w", err)
	}
	defer func() { rows.Close() }()
	return scanPGEdges(rows)
}

// EdgeCountsByType returns total edge count per type across the whole
// graph (PG mirror of sqlite.go EdgeCountsByType — see there for rationale).
func (s *pgStore) EdgeCountsByType() (map[string]int, error) {
	rows, err := s.pool.Query(background, `SELECT type, COUNT(*) FROM edges GROUP BY type`)
	if err != nil {
		return nil, fmt.Errorf("edge counts by type: %w", err)
	}
	defer func() { rows.Close() }()
	out := map[string]int{}
	for rows.Next() {
		var t string
		var n int
		if err := rows.Scan(&t, &n); err != nil {
			return nil, fmt.Errorf("scan edge count row: %w", err)
		}
		out[t] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edge count rows: %w", err)
	}
	return out, nil
}

// QueryEdgesForNodes returns every edge that has src OR dst in ids. Chunked
// by queryEdgesChunk (400) for parity with the SQLite implementation;
// PostgreSQL doesn't have a hard parameter limit but chunking bounds memory.
func (s *pgStore) QueryEdgesForNodes(ids []string) ([]types.Edge, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	seen := map[int64]bool{}
	var out []types.Edge
	for start := 0; start < len(ids); start += queryEdgesChunk {
		end := start + queryEdgesChunk
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[start:end]
		rows, err := s.pool.Query(background,
			`SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence, COALESCE(dispatch_kind,'')
            FROM edges WHERE src = ANY($1) OR dst = ANY($1)`,
			chunk)
		if err != nil {
			return nil, fmt.Errorf("query edges chunk [%d:%d]: %w", start, end, err)
		}
		es, err := scanPGEdges(rows)
		rows.Close()

		if err != nil {
			return nil, err
		}
		for _, e := range es {
			if seen[e.ID] {
				continue
			}
			seen[e.ID] = true
			out = append(out, e)
		}
	}
	return out, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Traversal
// ──────────────────────────────────────────────────────────────────────────────

// NeighborhoodByQname implements BFS expansion from any node matching qname.
// Delegates to the same shared helpers as the SQLite implementation.
func (s *pgStore) NeighborhoodByQname(qname string, depth int, reverse bool, edgeTypes ...string) ([]types.Node, []types.Edge, error) {
	roots, err := s.FindSymbol(qname, true, FindSymbolOptions{})
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]types.Node{}
	for _, r := range roots {
		seen[r.ID] = r
	}
	var allEdges []types.Edge
	frontier := mapKeys(seen)
	for d := 0; d < depth; d++ {
		if len(frontier) == 0 {
			break
		}
		var es []types.Edge
		if reverse {
			es, err = s.pgEdgesPointingTo(frontier, edgeTypes)
		} else {
			es, err = s.pgEdgesFrom(frontier, edgeTypes)
		}
		if err != nil {
			return nil, nil, err
		}
		next := []string{}
		ids := []string{}
		for _, e := range es {
			allEdges = append(allEdges, e)
			id := e.Dst
			if reverse {
				id = e.Src
			}
			if _, ok := seen[id]; !ok {
				ids = append(ids, id)
				next = append(next, id)
			}
		}
		ns, _ := s.NodesByIDs(ids)
		for _, n := range ns {
			seen[n.ID] = n
		}
		frontier = next
	}
	out := make([]types.Node, 0, len(seen))
	for _, n := range seen {
		out = append(out, n)
	}
	return out, allEdges, nil
}

// SubgraphByQname returns BFS in both directions up to depth.
func (s *pgStore) SubgraphByQname(qname string, depth int) ([]types.Node, []types.Edge, error) {
	fwdN, fwdE, err := s.NeighborhoodByQname(qname, depth, false)
	if err != nil {
		return nil, nil, err
	}
	revN, revE, err := s.NeighborhoodByQname(qname, depth, true)
	if err != nil {
		return nil, nil, err
	}
	merged := map[string]types.Node{}
	for _, n := range fwdN {
		merged[n.ID] = n
	}
	for _, n := range revN {
		merged[n.ID] = n
	}
	out := make([]types.Node, 0, len(merged))
	for _, n := range merged {
		out = append(out, n)
	}
	return out, append(fwdE, revE...), nil
}

// pgEdgesFrom returns every edge whose src is in ids, filtered by edgeTypes.
func (s *pgStore) pgEdgesFrom(ids []string, edgeTypes []string) ([]types.Edge, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var q string
	var args []any
	if len(edgeTypes) == 0 {
		q = `SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence, COALESCE(dispatch_kind,'')
            FROM edges WHERE src = ANY($1)`
		args = []any{ids}
	} else {
		q = `SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence, COALESCE(dispatch_kind,'')
            FROM edges WHERE src = ANY($1) AND type = ANY($2)`
		args = []any{ids, edgeTypes}
	}
	rows, err := s.pool.Query(background, q, args...)
	if err != nil {
		return nil, fmt.Errorf("edges from %d ids: %w", len(ids), err)
	}
	defer func() { rows.Close() }()
	return scanPGEdges(rows)
}

// pgEdgesPointingTo returns every edge whose dst is in ids, filtered by edgeTypes.
func (s *pgStore) pgEdgesPointingTo(ids []string, edgeTypes []string) ([]types.Edge, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var q string
	var args []any
	if len(edgeTypes) == 0 {
		q = `SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence, COALESCE(dispatch_kind,'')
            FROM edges WHERE dst = ANY($1)`
		args = []any{ids}
	} else {
		q = `SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence, COALESCE(dispatch_kind,'')
            FROM edges WHERE dst = ANY($1) AND type = ANY($2)`
		args = []any{ids, edgeTypes}
	}
	rows, err := s.pool.Query(background, q, args...)
	if err != nil {
		return nil, fmt.Errorf("edges pointing to %d ids: %w", len(ids), err)
	}
	defer func() { rows.Close() }()
	return scanPGEdges(rows)
}

// ──────────────────────────────────────────────────────────────────────────────
// Search
// ──────────────────────────────────────────────────────────────────────────────

// Search routes between FTS (English text) and substring fallback (CJK).
// Uses the same routing logic as the SQLite implementation.
func (s *pgStore) Search(q string, limit int) ([]types.Node, error) {
	return s.SearchWithOpts(q, limit, SearchFTSOptions{})
}

// SearchWithOpts threads SearchFTSOptions through the routed search
// path; the CJK substring fallback drops opts (no multi-token AND/OR
// semantics there). Mirrors sqliteStore.SearchWithOpts for backend parity.
func (s *pgStore) SearchWithOpts(q string, limit int, opts SearchFTSOptions) ([]types.Node, error) {
	if hasNonASCII(q) {
		return s.SearchSubstr(q, limit)
	}
	hits, err := s.SearchFTS(rewriteFTSQuery(q), limit, opts)
	if err != nil {
		return nil, err
	}
	return nodesFromHits(hits), nil
}

// SearchFTS executes a full-text search using the search_vector column and
// returns matches with relevance scores. Uses plainto_tsquery to safely
// handle arbitrary user input without syntax errors (unlike to_tsquery which
// requires well-formed query syntax).
//
// RawScore is ts_rank(search_vector, query) — a non-negative float where
// higher means a stronger match. Note: this scale differs from the SQLite
// backend's -bm25() output; cross-backend comparison of RawScore is unsafe.
// Score is then min-max normalized to [0, 1] within the result set by
// normalizeSearchHits.
//
// opts.Language pushes a `language = $N` predicate into the WHERE clause
// when non-empty — CKG-2 filter push-down so cks no longer has to
// over-fetch and filter client-side.
func (s *pgStore) SearchFTS(q string, limit int, opts SearchFTSOptions) ([]SearchHit, error) {
	// plainto_tsquery is safe for arbitrary input (no special syntax needed).
	// rewriteFTSQuery already strips FTS5-specific sigils that don't apply to PG;
	// for PG we use plainto_tsquery unconditionally for robustness.
	// Strip the trailing '*' that rewriteFTSQuery appends — plainto_tsquery
	// handles prefix matching internally via lexeme stemming.
	qclean := strings.TrimRight(q, "*")
	// Mode="and": over-fetch so the per-token presence filter has
	// recall headroom (mirrors the SQLite backend; see sqlite.go::
	// SearchFTS for the 3× ratio + floor 30 reasoning).
	effectiveLimit := limit
	if opts.Mode == "and" {
		effectiveLimit = max(limit*3, 30)
	}
	// NodeKinds: nil/empty defaults to symbol-only — mirrors the
	// SQLite backend so the public Reader contract behaves the same
	// way regardless of which store is mounted.
	kinds := opts.NodeKinds
	if len(kinds) == 0 {
		kinds = types.SymbolNodeTypes()
	}
	sql := `SELECT ` + pgNodeColumns + `,
            ts_rank(search_vector, plainto_tsquery('english', $1)) AS raw_score
        FROM nodes
        WHERE search_vector @@ plainto_tsquery('english', $1)`
	args := []any{qclean}
	next := 2
	if opts.Language != "" {
		sql += fmt.Sprintf(` AND language = $%d`, next)
		args = append(args, opts.Language)
		next++
	}
	// IN clause with positional placeholders ($N, $N+1, ...).
	inClause := ` AND type IN (`
	for i, k := range kinds {
		if i > 0 {
			inClause += ", "
		}
		inClause += fmt.Sprintf("$%d", next)
		args = append(args, string(k))
		next++
	}
	inClause += ")"
	sql += inClause
	sql += fmt.Sprintf(` ORDER BY raw_score DESC LIMIT $%d`, next)
	args = append(args, effectiveLimit)

	rows, err := s.pool.Query(background, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("fts search %q: %w", q, err)
	}
	defer func() { rows.Close() }()
	hits, err := scanPGSearchHits(rows)
	if err != nil {
		return nil, err
	}
	if opts.Mode == "and" {
		tokens := tokenizeAndQuery(q)
		if len(tokens) > 1 {
			hits = filterHitsByAllTokens(hits, tokens)
		}
		if len(hits) > limit {
			hits = hits[:limit]
		}
	}
	normalizeSearchHits(hits)
	return hits, nil
}

// SearchSubstr is the CJK / non-tokenisable fallback. Uses ILIKE for
// case-insensitive substring matching against name and qualified_name.
func (s *pgStore) SearchSubstr(q string, limit int) ([]types.Node, error) {
	pat := "%" + q + "%"
	rows, err := s.pool.Query(background,
		`SELECT `+pgNodeColumns+`
        FROM nodes
        WHERE name ILIKE $1 OR qualified_name ILIKE $1 LIMIT $2`, pat, limit)
	if err != nil {
		return nil, fmt.Errorf("substring search %q: %w", q, err)
	}
	defer func() { rows.Close() }()
	return scanPGNodes(rows)
}

// ──────────────────────────────────────────────────────────────────────────────
// Blobs
// ──────────────────────────────────────────────────────────────────────────────

// GetBlob returns the raw source slice for the given node ID. Returns
// pgx.ErrNoRows when no blob exists — callers that check for sql.ErrNoRows
// should also handle pgx.ErrNoRows, but the postgres_exporter.go already
// handles both via errors.Is checks.
func (s *pgStore) GetBlob(id string) ([]byte, error) {
	var b []byte
	err := s.pool.QueryRow(background,
		`SELECT source FROM blobs WHERE node_id = $1`, id).Scan(&b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Per-file lookups (incremental cache)
// ──────────────────────────────────────────────────────────────────────────────

// NodesByFilePath returns every node whose file_path equals path, ordered by
// start_line. The ORDER BY is critical for the G6 v4 correctness fix: nodes
// must be returned in declaration order for Pass 2 Resolve to produce stable
// qnames.
func (s *pgStore) NodesByFilePath(path string) ([]types.Node, error) {
	if path == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(background,
		`SELECT `+pgNodeColumns+` FROM nodes WHERE file_path = $1 ORDER BY start_line`, path)
	if err != nil {
		return nil, fmt.Errorf("nodes by file_path %q: %w", path, err)
	}
	defer func() { rows.Close() }()
	return scanPGNodes(rows)
}

// EdgesByFilePath returns every edge whose file_path equals path. Cross-file
// edges (file_path IS NULL) are not returned.
func (s *pgStore) EdgesByFilePath(path string) ([]types.Edge, error) {
	if path == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(background,
		`SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence, COALESCE(dispatch_kind,'')
        FROM edges WHERE file_path = $1`, path)
	if err != nil {
		return nil, fmt.Errorf("edges by file_path %q: %w", path, err)
	}
	defer func() { rows.Close() }()
	return scanPGEdges(rows)
}

// BlobsByFilePath returns blobs for all nodes with the given file_path.
// Non-nil empty map is returned when no blobs exist (same contract as SQLite).
func (s *pgStore) BlobsByFilePath(path string) (map[string][]byte, error) {
	out := map[string][]byte{}
	if path == "" {
		return out, nil
	}
	rows, err := s.pool.Query(background,
		`SELECT b.node_id, b.source FROM blobs b
        JOIN nodes n ON n.id = b.node_id WHERE n.file_path = $1`, path)
	if err != nil {
		return nil, fmt.Errorf("blobs by file_path %q: %w", path, err)
	}
	defer func() { rows.Close() }()
	for rows.Next() {
		var id string
		var b []byte
		if err := rows.Scan(&id, &b); err != nil {
			return nil, fmt.Errorf("scan blob: %w", err)
		}
		out[id] = b
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate blob rows: %w", err)
	}
	return out, nil
}

// PendingRefsByFilePath returns pending_refs rows for the given file_path.
// dispatch_kind (schema 1.7) is COALESCE'd to ” so pre-1.7 NULL rows scan
// cleanly when an older PG dump is replayed against this binary.
func (s *pgStore) PendingRefsByFilePath(path string) ([]PendingRefRow, error) {
	if path == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(background,
		`SELECT file_path, src_id, target_qname, edge_type, line,
        COALESCE(hint_file,''), COALESCE(dispatch_kind,'') FROM pending_refs WHERE file_path = $1`, path)
	if err != nil {
		return nil, fmt.Errorf("pending_refs by file_path %q: %w", path, err)
	}
	defer func() { rows.Close() }()
	var out []PendingRefRow
	for rows.Next() {
		var r PendingRefRow
		if err := rows.Scan(&r.FilePath, &r.SrcID, &r.TargetQName,
			&r.EdgeType, &r.Line, &r.HintFile, &r.DispatchKind); err != nil {
			return nil, fmt.Errorf("scan pending_ref: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending_refs: %w", err)
	}
	return out, nil
}

// ReverseDepsForFiles returns every cached file whose pending_refs target a
// qualified_name defined in any of dirtyPaths. Must be called BEFORE dirty
// nodes are deleted. Returns nil when dirtyPaths is empty.
func (s *pgStore) ReverseDepsForFiles(dirtyPaths []string) ([]string, error) {
	if len(dirtyPaths) == 0 {
		return nil, nil
	}
	// pending_refs.target_qname stores the unresolved AST name (e.g. "Helper"),
	// while nodes.qualified_name is fully-qualified (e.g. "edgepin.Helper").
	// The LIKE arm matches the suffix after the last dot — mirrors simpleName()
	// in resolve.go so C1 finds the same candidates as Pass 2 Resolve does.
	rows, err := s.pool.Query(background,
		`SELECT DISTINCT pr.file_path
		 FROM pending_refs pr
		 INNER JOIN nodes n ON (
		     n.qualified_name = pr.target_qname
		     OR n.qualified_name LIKE ('%.' || pr.target_qname)
		 )
		 WHERE n.file_path = ANY($1)
		   AND pr.file_path != ALL($1)`, dirtyPaths)
	if err != nil {
		return nil, fmt.Errorf("reverse deps for %d paths: %w", len(dirtyPaths), err)
	}
	defer func() { rows.Close() }()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan reverse dep path: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ──────────────────────────────────────────────────────────────────────────────
// Hierarchy
// ──────────────────────────────────────────────────────────────────────────────

// LoadHierarchy returns the package tree (kind="pkg") or topic tree
// (kind="topic") as a flat slice of HierarchyRow.
func (s *pgStore) LoadHierarchy(kind string) ([]HierarchyRow, error) {
	var query string
	switch kind {
	case "pkg":
		query = `SELECT parent_id, child_id, level, '' FROM pkg_tree`
	case "topic":
		// parent_id stored as '' for root — COALESCE here is a no-op but mirrors
		// the SQLite pattern for readability.
		query = `SELECT COALESCE(parent_id,''), child_id, resolution, COALESCE(topic_label,'') FROM topic_tree`
	default:
		return nil, fmt.Errorf("unknown hierarchy kind %q", kind)
	}
	rows, err := s.pool.Query(background, query)
	if err != nil {
		return nil, fmt.Errorf("query hierarchy %q: %w", kind, err)
	}
	defer func() { rows.Close() }()
	var out []HierarchyRow
	for rows.Next() {
		var r HierarchyRow
		if err := rows.Scan(&r.ParentID, &r.ChildID, &r.Level, &r.TopicLabel); err != nil {
			return nil, fmt.Errorf("scan hierarchy row: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hierarchy rows: %w", err)
	}
	return out, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Static export
// ──────────────────────────────────────────────────────────────────────────────

// ExportChunked writes the static JSON export layout to outDir. The
// PostgreSQL implementation queries all data from PG and writes the same
// chunked JSON output as the SQLite implementation.
//
// Directory layout mirrors chunked_export.go:
//
//	outDir/manifest.json
//	outDir/hierarchy/pkg_tree.json
//	outDir/hierarchy/topic_tree.json
//	outDir/nodes/chunk_NNNN.json
//	outDir/edges/chunk_NNNN.json
//	outDir/blobs/<nodeID>.txt
func (s *pgStore) ExportChunked(outDir string, nodeChunkSize, edgeChunkSize int) error {
	for _, sub := range []string{"nodes", "edges", "hierarchy", "blobs"} {
		if err := os.MkdirAll(filepath.Join(outDir, sub), 0o755); err != nil {
			return err
		}
	}

	// Manifest
	m, err := s.GetManifest()
	if err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(outDir, "manifest.json"), m); err != nil {
		return err
	}

	// Hierarchies
	pkg, _ := s.LoadHierarchy("pkg")
	topic, _ := s.LoadHierarchy("topic")
	if err := writeJSONFile(filepath.Join(outDir, "hierarchy", "pkg_tree.json"), pkg); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(outDir, "hierarchy", "topic_tree.json"), topic); err != nil {
		return err
	}

	// Nodes — load all, then chunk
	nrows, err := s.pool.Query(background, `SELECT `+pgNodeColumns+` FROM nodes`)
	if err != nil {
		return err
	}
	nodes, err := scanPGNodes(nrows)
	nrows.Close()

	if err != nil {
		return err
	}
	for i, chunkIdx := 0, 0; i < len(nodes); i, chunkIdx = i+nodeChunkSize, chunkIdx+1 {
		end := i + nodeChunkSize
		if end > len(nodes) {
			end = len(nodes)
		}
		path := filepath.Join(outDir, "nodes", fmt.Sprintf("chunk_%04d.json", chunkIdx))
		if err := writeJSONFile(path, nodes[i:end]); err != nil {
			return err
		}
	}

	// Edges — load all, then chunk
	erows, err := s.pool.Query(background,
		`SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence, COALESCE(dispatch_kind,'') FROM edges`)
	if err != nil {
		return err
	}
	edges, err := scanPGEdges(erows)
	erows.Close()

	if err != nil {
		return err
	}
	for i, chunkIdx := 0, 0; i < len(edges); i, chunkIdx = i+edgeChunkSize, chunkIdx+1 {
		end := i + edgeChunkSize
		if end > len(edges) {
			end = len(edges)
		}
		path := filepath.Join(outDir, "edges", fmt.Sprintf("chunk_%04d.json", chunkIdx))
		if err := writeJSONFile(path, edges[i:end]); err != nil {
			return err
		}
	}

	// Blobs — one raw-text file per node
	brows, err := s.pool.Query(background, `SELECT node_id, source FROM blobs`)
	if err != nil {
		return err
	}
	defer func() { brows.Close() }()
	for brows.Next() {
		var id string
		var b []byte
		if err := brows.Scan(&id, &b); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outDir, "blobs", id+".txt"), b, 0o644); err != nil {
			return err
		}
	}
	return brows.Err()
}

// ──────────────────────────────────────────────────────────────────────────────
// Internal scan helpers
// ──────────────────────────────────────────────────────────────────────────────

// scanPGNodes drains pgx.Rows assuming the SELECT projects pgNodeColumns in
// order. All nullable columns are pre-COALESCE'd in the SQL so we scan into
// value types directly.
func scanPGNodes(rows pgx.Rows) ([]types.Node, error) {
	var out []types.Node
	for rows.Next() {
		var n types.Node
		var conf string
		if err := rows.Scan(
			&n.ID, &n.Type, &n.Name, &n.QualifiedName, &n.FilePath,
			&n.StartLine, &n.EndLine, &n.StartByte, &n.EndByte, &n.Language,
			&n.Visibility, &n.Signature, &n.DocComment, &n.Complexity,
			&n.InDegree, &n.OutDegree, &n.PageRank, &n.UsageScore,
			&conf, &n.SubKind,
		); err != nil {
			return nil, fmt.Errorf("scan node row: %w", err)
		}
		n.Confidence = types.Confidence(conf)
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate node rows: %w", err)
	}
	return out, nil
}

// scanPGSearchHits drains pgx.Rows that project pgNodeColumns followed by
// a trailing raw_score float (ts_rank output). Score is left zero here;
// normalizeSearchHits fills it after the full result set is read.
func scanPGSearchHits(rows pgx.Rows) ([]SearchHit, error) {
	var out []SearchHit
	for rows.Next() {
		var n types.Node
		var conf string
		var raw float64
		if err := rows.Scan(
			&n.ID, &n.Type, &n.Name, &n.QualifiedName, &n.FilePath,
			&n.StartLine, &n.EndLine, &n.StartByte, &n.EndByte, &n.Language,
			&n.Visibility, &n.Signature, &n.DocComment, &n.Complexity,
			&n.InDegree, &n.OutDegree, &n.PageRank, &n.UsageScore,
			&conf, &n.SubKind, &raw,
		); err != nil {
			return nil, fmt.Errorf("scan search hit row: %w", err)
		}
		n.Confidence = types.Confidence(conf)
		out = append(out, SearchHit{Node: n, RawScore: raw})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search hit rows: %w", err)
	}
	return out, nil
}

// scanPGEdges drains pgx.Rows for edge queries (file_path/line are COALESCE'd
// in the SELECT, so direct scan into value types is safe). dispatch_kind
// (schema 1.7) is the trailing column; COALESCE'd to ” in callers so
// pre-1.7 NULL rows scan cleanly.
func scanPGEdges(rows pgx.Rows) ([]types.Edge, error) {
	var out []types.Edge
	for rows.Next() {
		var e types.Edge
		var conf string
		if err := rows.Scan(
			&e.ID, &e.Src, &e.Dst, &e.Type, &e.FilePath, &e.Line, &e.Count, &conf, &e.DispatchKind,
		); err != nil {
			return nil, fmt.Errorf("scan edge row: %w", err)
		}
		e.Confidence = types.Confidence(conf)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edge rows: %w", err)
	}
	return out, nil
}
