package persist

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// PostgresExporter reads a SQLite graph (via StoreReader) and pushes all
// nodes, edges and blobs to a PostgreSQL database in a single one-shot
// transfer. It is intentionally write-only: the target schema is created
// on first run (IF NOT EXISTS), so re-running against an already-populated
// database is idempotent at the DDL level but will conflict on PK inserts.
// Callers that need upsert semantics should truncate the target tables first.
type PostgresExporter struct{}

// pgSchema is the DDL applied to the target PostgreSQL database before any
// data is inserted. Tables mirror the SQLite schema with all node and edge
// fields. pg_trgm is applied best-effort: the EXTENSION creation is silently
// skipped when the extension is unavailable (e.g. RDS instances without the
// contrib pack).
const pgSchema = `
CREATE TABLE IF NOT EXISTS nodes (
    id            TEXT    PRIMARY KEY,
    kind          TEXT    NOT NULL,
    name          TEXT    NOT NULL,
    qname         TEXT    NOT NULL,
    file_path     TEXT    NOT NULL DEFAULT '',
    start_line    INTEGER NOT NULL DEFAULT 0,
    end_line      INTEGER NOT NULL DEFAULT 0,
    start_byte    INTEGER NOT NULL DEFAULT 0,
    end_byte      INTEGER NOT NULL DEFAULT 0,
    language      TEXT    NOT NULL DEFAULT '',
    visibility    TEXT    NOT NULL DEFAULT '',
    signature     TEXT    NOT NULL DEFAULT '',
    doc_comment   TEXT    NOT NULL DEFAULT '',
    complexity    INTEGER NOT NULL DEFAULT 0,
    in_degree     INTEGER NOT NULL DEFAULT 0,
    out_degree    INTEGER NOT NULL DEFAULT 0,
    pagerank      DOUBLE PRECISION NOT NULL DEFAULT 0,
    usage_score   DOUBLE PRECISION NOT NULL DEFAULT 0,
    confidence    TEXT    NOT NULL DEFAULT 'EXTRACTED',
    sub_kind      TEXT    NOT NULL DEFAULT '',
    -- attrs (W-C W11 V9, 2026-05-19): JSON-blob carrying every
    -- types.Node marker that doesn't have its own column.
    -- Mirrors the SQLite nodes.attrs added under schema 1.9.
    attrs         TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS edges (
    id            TEXT    PRIMARY KEY,
    type          TEXT    NOT NULL,
    src           TEXT    NOT NULL,
    dst           TEXT    NOT NULL,
    file_path     TEXT    NOT NULL DEFAULT '',
    line          INTEGER NOT NULL DEFAULT 0,
    count         INTEGER NOT NULL DEFAULT 1,
    confidence    TEXT    NOT NULL DEFAULT 'EXTRACTED',
    dispatch_kind TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS blobs (
    node_id TEXT PRIMARY KEY,
    body    BYTEA NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_nodes_qname ON nodes (qname);
CREATE INDEX IF NOT EXISTS idx_nodes_file  ON nodes (file_path);
CREATE INDEX IF NOT EXISTS idx_nodes_kind  ON nodes (kind);
CREATE INDEX IF NOT EXISTS idx_edges_src   ON edges (src);
CREATE INDEX IF NOT EXISTS idx_edges_dst   ON edges (dst);
CREATE INDEX IF NOT EXISTS idx_edges_type  ON edges (type);
`

// pgTrgmDDL is applied only when the pg_trgm extension is available.
// Failures are logged as warnings and do not abort the export.
// NOTE: CREATE EXTENSION cannot run inside a transaction in PostgreSQL,
// so this block is executed outside the schema transaction.
const pgTrgmDDL = `
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_nodes_name_trgm  ON nodes USING GIN (name  gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_nodes_qname_trgm ON nodes USING GIN (qname gin_trgm_ops);
`

// DSNHost extracts the host portion of a PostgreSQL DSN for safe logging
// (avoids printing credentials in log output). It handles both URL format
// (postgres://user:pass@host/db) and key=value format (host=localhost dbname=mydb).
// Returns "<unparseable>" on any parse failure.
func DSNHost(dsn string) string {
	cfg, err := pgconn.ParseConfig(dsn)
	if err != nil {
		return "<unparseable>"
	}
	return cfg.Host
}

// Export reads all nodes, edges and blobs from store and inserts them into
// the PostgreSQL database reachable at dsn. It creates the schema on first
// call. The operation is not wrapped in a single transaction to keep memory
// pressure bounded; partial exports leave the target in a consistent (though
// potentially incomplete) state.
func (e *PostgresExporter) Export(ctx context.Context, dsn string, store StoreReader, log *slog.Logger) error {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer func() { pool.Close() }()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}
	log.Info("connected to postgres")

	if err := applySchema(ctx, pool, log); err != nil {
		return fmt.Errorf("apply pg schema: %w", err)
	}

	nodes, err := loadAllNodes(store)
	if err != nil {
		return fmt.Errorf("load nodes from sqlite: %w", err)
	}
	log.Info("loaded nodes from sqlite", slog.Int("count", len(nodes)))

	if err := insertNodes(ctx, pool, nodes, log); err != nil {
		return fmt.Errorf("insert nodes: %w", err)
	}

	edges, err := loadAllEdges(store)
	if err != nil {
		return fmt.Errorf("load edges from sqlite: %w", err)
	}
	log.Info("loaded edges from sqlite", slog.Int("count", len(edges)))

	if err := insertEdges(ctx, pool, edges, log); err != nil {
		return fmt.Errorf("insert edges: %w", err)
	}

	if err := exportBlobs(ctx, pool, store, nodes, log); err != nil {
		return fmt.Errorf("export blobs: %w", err)
	}

	log.Info("export complete")
	return nil
}

// applySchema wraps the base DDL in a transaction (Issue 6) and attempts
// pg_trgm indexes best-effort outside the transaction (CREATE EXTENSION
// cannot run inside a PG transaction).
func applySchema(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin schema transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, pgSchema); err != nil {
		return fmt.Errorf("apply base schema: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit schema transaction: %w", err)
	}

	// pg_trgm CREATE EXTENSION must run outside a transaction.
	if _, err := pool.Exec(ctx, pgTrgmDDL); err != nil {
		log.Warn("pg_trgm extension unavailable; trigram indexes skipped",
			slog.String("err", err.Error()))
	}
	return nil
}

// loadAllNodes fetches every node from the SQLite store in a single scan.
//
// Earlier this iterated DistinctFilePaths × NodesByFilePath per known
// language — an N+1 over every file, and one that silently dropped nodes in
// languages outside the hardcoded set (e.g. proto). AllNodes is one table
// scan and returns the complete set.
func loadAllNodes(store StoreReader) ([]types.Node, error) {
	return store.AllNodes()
}

// loadAllEdges fetches every edge from the SQLite store in a single scan.
// Earlier this issued one query per EdgeType; AllEdges is one scan.
func loadAllEdges(store StoreReader) ([]types.Edge, error) {
	return store.AllEdges()
}

// insertNodes bulk-copies nodes to PostgreSQL using the COPY protocol.
// COPY is significantly faster than INSERT for large datasets because it
// bypasses per-row parsing overhead on the server.
func insertNodes(ctx context.Context, pool *pgxpool.Pool, nodes []types.Node, log *slog.Logger) error {
	if len(nodes) == 0 {
		log.Info("no nodes to insert")
		return nil
	}

	rows := make([][]any, 0, len(nodes))
	for _, n := range nodes {
		attrs := marshalNodeAttrs(&n)
		rows = append(rows, []any{
			n.ID,
			string(n.Type),
			n.Name,
			n.QualifiedName,
			n.FilePath,
			n.StartLine,
			n.EndLine,
			n.StartByte,
			n.EndByte,
			n.Language,
			n.Visibility,
			n.Signature,
			n.DocComment,
			n.Complexity,
			n.InDegree,
			n.OutDegree,
			n.PageRank,
			n.UsageScore,
			string(n.Confidence),
			n.SubKind,
			attrs,
		})
	}

	cols := []string{
		"id", "kind", "name", "qname", "file_path",
		"start_line", "end_line", "start_byte", "end_byte", "language",
		"visibility", "signature", "doc_comment", "complexity",
		"in_degree", "out_degree", "pagerank", "usage_score",
		"confidence", "sub_kind", "attrs",
	}
	n, err := pool.CopyFrom(
		ctx,
		pgx.Identifier{"nodes"},
		cols,
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("copy nodes: %w", err)
	}
	log.Info("inserted nodes", slog.Int64("count", n))
	return nil
}

// edgeKey derives a stable string key for dedup; edge IDs are SQLite
// AUTOINCREMENT integers — they cannot serve as PG PKs verbatim. We
// synthesise a TEXT PK from src+type+dst+file_path+line to avoid collisions
// between edges with same src/type/dst but different file_path at line 0.
func edgeKey(e types.Edge) string {
	return fmt.Sprintf("%s|%s|%s|%s|%d", e.Src, string(e.Type), e.Dst, e.FilePath, e.Line)
}

// insertEdges bulk-copies edges to PostgreSQL using the COPY protocol.
func insertEdges(ctx context.Context, pool *pgxpool.Pool, edges []types.Edge, log *slog.Logger) error {
	if len(edges) == 0 {
		log.Info("no edges to insert")
		return nil
	}

	rows := make([][]any, 0, len(edges))
	for _, e := range edges {
		rows = append(rows, []any{
			edgeKey(e),
			string(e.Type),
			e.Src,
			e.Dst,
			e.FilePath,
			e.Line,
			e.Count,
			string(e.Confidence),
			e.DispatchKind,
		})
	}

	cols := []string{"id", "type", "src", "dst", "file_path", "line", "count", "confidence", "dispatch_kind"}
	n, err := pool.CopyFrom(
		ctx,
		pgx.Identifier{"edges"},
		cols,
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("copy edges: %w", err)
	}
	log.Info("inserted edges", slog.Int64("count", n))
	return nil
}

// exportBlobs fetches blobs for each node and bulk-copies them to PostgreSQL.
// Nodes without a blob (e.g. Package nodes) are silently skipped.
func exportBlobs(ctx context.Context, pool *pgxpool.Pool, store StoreReader, nodes []types.Node, log *slog.Logger) error {
	type blobRow struct {
		nodeID string
		body   []byte
	}
	var blobs []blobRow
	for _, n := range nodes {
		b, err := store.GetBlob(n.ID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // no blob for this node — expected for Package/Import etc.
			}
			return fmt.Errorf("get blob for node %s: %w", n.ID, err)
		}
		if len(b) > 0 {
			blobs = append(blobs, blobRow{nodeID: n.ID, body: b})
		}
	}

	if len(blobs) == 0 {
		log.Info("no blobs to insert")
		return nil
	}

	rows := make([][]any, 0, len(blobs))
	for _, br := range blobs {
		rows = append(rows, []any{br.nodeID, br.body})
	}

	cols := []string{"node_id", "body"}
	n, err := pool.CopyFrom(
		ctx,
		pgx.Identifier{"blobs"},
		cols,
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("copy blobs: %w", err)
	}
	log.Info("inserted blobs", slog.Int64("count", n))
	return nil
}
