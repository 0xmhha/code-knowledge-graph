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

CREATE TABLE IF NOT EXISTS edges (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  src         TEXT NOT NULL REFERENCES nodes(id),
  dst         TEXT NOT NULL REFERENCES nodes(id),
  type        TEXT NOT NULL,
  file_path   TEXT,
  line        INTEGER,
  count       INTEGER NOT NULL DEFAULT 1,
  confidence  TEXT NOT NULL DEFAULT 'EXTRACTED'
);
CREATE INDEX IF NOT EXISTS idx_edges_src  ON edges(src);
CREATE INDEX IF NOT EXISTS idx_edges_dst  ON edges(dst);
CREATE INDEX IF NOT EXISTS idx_edges_type ON edges(type);

CREATE TABLE IF NOT EXISTS pkg_tree (
  parent_id TEXT NOT NULL REFERENCES nodes(id),
  child_id  TEXT NOT NULL REFERENCES nodes(id),
  level     INTEGER NOT NULL,
  PRIMARY KEY (parent_id, child_id)
);
CREATE INDEX IF NOT EXISTS idx_pkg_parent ON pkg_tree(parent_id);

CREATE TABLE IF NOT EXISTS topic_tree (
  parent_id   TEXT,
  child_id    TEXT NOT NULL REFERENCES nodes(id),
  resolution  INTEGER NOT NULL,
  topic_label TEXT,
  PRIMARY KEY (child_id, resolution, parent_id)
);

CREATE TABLE IF NOT EXISTS blobs (
  node_id TEXT PRIMARY KEY REFERENCES nodes(id),
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
