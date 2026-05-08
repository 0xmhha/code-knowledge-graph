-- PRAGMAs are applied per-connection via DSN in Open()/OpenReadOnly()
-- (sqlite PRAGMAs are connection-scoped, not database-scoped). The line below
-- is retained as documentation of intent — actual enforcement comes from the DSN.
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS nodes (
  id             TEXT PRIMARY KEY,
  type           TEXT NOT NULL,
  name           TEXT NOT NULL,
  qualified_name TEXT NOT NULL,
  file_path      TEXT NOT NULL,
  start_line     INTEGER NOT NULL,
  end_line       INTEGER NOT NULL,
  start_byte     INTEGER NOT NULL,
  end_byte       INTEGER NOT NULL,
  language       TEXT NOT NULL,
  visibility     TEXT,
  signature      TEXT,
  doc_comment    TEXT,
  complexity     INTEGER,
  in_degree      INTEGER NOT NULL DEFAULT 0,
  out_degree     INTEGER NOT NULL DEFAULT 0,
  pagerank       REAL    NOT NULL DEFAULT 0,
  usage_score    REAL    NOT NULL DEFAULT 0,
  confidence     TEXT    NOT NULL DEFAULT 'EXTRACTED',
  sub_kind       TEXT
);
CREATE INDEX IF NOT EXISTS idx_nodes_qname ON nodes(qualified_name);
CREATE INDEX IF NOT EXISTS idx_nodes_file  ON nodes(file_path);
CREATE INDEX IF NOT EXISTS idx_nodes_type  ON nodes(type);

-- ON DELETE CASCADE on edges/blobs/pkg_tree/topic_tree FK is required by the
-- A3 incremental cache: when a file's nodes are dropped (because its content
-- changed or the file was removed), every dependent row must follow. Schema
-- bump 1.1 → 1.2 marks this; pre-1.2 DBs are not retroactively migrated —
-- callers detect the missing CASCADE via foreign_key_check at Open() time
-- and a warning steers operators to --no-cache or a clean rebuild.
-- dispatch_kind (schema 1.7, Track C P1b) is an optional metadata column on
-- the edges row. Populated only for the `invokes` edge type — empty string
-- otherwise. Migrate() ALTER-ADDs this column when opening a pre-1.7 DB
-- (sqlite.go's ensureDispatchKindColumn). The CREATE TABLE here describes
-- the post-migration shape; pre-1.7 readers tolerate the column because
-- SELECT projections enumerate columns explicitly (no SELECT *).
CREATE TABLE IF NOT EXISTS edges (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
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

CREATE TABLE IF NOT EXISTS pkg_tree (
  parent_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  child_id  TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  level     INTEGER NOT NULL,
  PRIMARY KEY (parent_id, child_id)
);
CREATE INDEX IF NOT EXISTS idx_pkg_parent ON pkg_tree(parent_id);

CREATE TABLE IF NOT EXISTS topic_tree (
  parent_id   TEXT,
  child_id    TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  resolution  INTEGER NOT NULL,
  topic_label TEXT,
  PRIMARY KEY (child_id, resolution, parent_id)
);

CREATE TABLE IF NOT EXISTS blobs (
  node_id TEXT PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
  source  BLOB NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS nodes_fts USING fts5(
  name, qualified_name, signature, doc_comment,
  content='nodes', content_rowid='rowid'
);

CREATE TABLE IF NOT EXISTS manifest (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- Pending refs (G6 v3, schema 1.5): per-file unresolved cross-file references
-- emitted by Pass 1 and consumed by Pass 2 Resolve. Persisted so the partial-
-- cache rebuild path can reconstruct Pass 2's full input without re-parsing
-- cached files (the v1/v2 silent-drop bug).
--
-- FK src_id → nodes(id) ON DELETE CASCADE matches the file lifecycle: when a
-- dirty/removed file's nodes are dropped via DeleteNodesByFilePath, its
-- pending refs follow automatically — no separate cleanup statement needed.
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
