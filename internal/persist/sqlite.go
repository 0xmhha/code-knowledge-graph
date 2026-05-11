package persist

import (
	"database/sql"
	_ "embed"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

//go:embed schema.sql
var schemaSQL string

// sqliteStore wraps a SQLite database for the CKG graph. It is the concrete
// implementation behind the Store / StoreReader / StoreWriter interfaces
// (see store_interface.go). The struct is unexported because consumers
// should depend on the interfaces — the only way to obtain an instance is
// via Open / OpenReadOnly, both of which return through the interface
// boundary in practice (callers use `:=`).
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
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %s: %w", path, err)
	}
	return &sqliteStore{db: db}, nil
}

// OpenReadOnly opens a SQLite file in read-only mode (used by serve/mcp).
// FK pragma is enforced per-connection via DSN; WAL is omitted because read-only
// mode cannot mutate journal state.
func OpenReadOnly(path string) (*sqliteStore, error) {
	dsn := path + "?mode=ro&immutable=1&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite ro at %s: %w", path, err)
	}
	return &sqliteStore{db: db}, nil
}

// Close releases the underlying database handle.
func (s *sqliteStore) Close() error { return s.db.Close() }

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
	// Schema 1.9: no migrations required (see func docstring).
	return nil
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
	defer rows.Close()
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

// InsertNodes bulk-inserts nodes (transactional).
func (s *sqliteStore) InsertNodes(nodes []types.Node) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO nodes
		(id, type, name, qualified_name, file_path, start_line, end_line,
		 start_byte, end_byte, language, visibility, signature, doc_comment,
		 complexity, in_degree, out_degree, pagerank, usage_score, confidence, sub_kind)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, n := range nodes {
		if _, err := stmt.Exec(n.ID, n.Type, n.Name, n.QualifiedName, n.FilePath,
			n.StartLine, n.EndLine, n.StartByte, n.EndByte, n.Language,
			n.Visibility, n.Signature, n.DocComment, n.Complexity,
			n.InDegree, n.OutDegree, n.PageRank, n.UsageScore,
			string(n.Confidence), n.SubKind); err != nil {
			return fmt.Errorf("insert node %s: %w", n.ID, err)
		}
	}
	return tx.Commit()
}

// GetNode fetches a node by ID. Returns sql.ErrNoRows if not found.
func (s *sqliteStore) GetNode(id string) (types.Node, error) {
	row := s.db.QueryRow(`SELECT id, type, name, qualified_name, file_path,
		start_line, end_line, start_byte, end_byte, language, visibility,
		signature, doc_comment, complexity, in_degree, out_degree, pagerank,
		usage_score, confidence, sub_kind FROM nodes WHERE id = ?`, id)
	var n types.Node
	var conf string
	err := row.Scan(&n.ID, &n.Type, &n.Name, &n.QualifiedName, &n.FilePath,
		&n.StartLine, &n.EndLine, &n.StartByte, &n.EndByte, &n.Language,
		&n.Visibility, &n.Signature, &n.DocComment, &n.Complexity,
		&n.InDegree, &n.OutDegree, &n.PageRank, &n.UsageScore,
		&conf, &n.SubKind)
	if err != nil {
		return n, err
	}
	n.Confidence = types.Confidence(conf)
	return n, nil
}

// ClusterEdge mirrors cluster.Edge to avoid making persist's exported surface
// reach across packages. cluster.PersistClusterEdge is a structurally identical
// type defined in the cluster package; InsertPkgTreeFromCluster bridges them.
type ClusterEdge struct {
	ParentID, ChildID string
	Level             int
}

// TopicTreeInput abstracts the per-resolution view of a topic tree so persist
// can consume it without importing cluster types directly. *cluster.TopicTree
// satisfies this interface (see internal/cluster/persist_adapter.go).
type TopicTreeInput interface {
	ResolutionsCount() int
	ResolutionGamma(i int) float64
	ResolutionMembers(i int) map[string][]string // label -> []nodeID
}

// InsertPkgTree bulk-inserts package-tree edges.
func (s *sqliteStore) InsertPkgTree(edges []ClusterEdge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO pkg_tree (parent_id, child_id, level) VALUES (?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range edges {
		if _, err := stmt.Exec(e.ParentID, e.ChildID, e.Level); err != nil {
			return fmt.Errorf("insert pkg_tree %s->%s: %w", e.ParentID, e.ChildID, err)
		}
	}
	return tx.Commit()
}

// InsertPkgTreeFromCluster adapts cluster.PersistClusterEdge slices to the
// internal ClusterEdge type and delegates to InsertPkgTree.
func (s *sqliteStore) InsertPkgTreeFromCluster(edges []cluster.PersistClusterEdge) error {
	out := make([]ClusterEdge, len(edges))
	for i, e := range edges {
		out[i] = ClusterEdge(e)
	}
	return s.InsertPkgTree(out)
}

// InsertTopicTree persists multi-resolution Leiden communities. Existing rows
// are dropped first so a full rebuild matches V0 expectations.
func (s *sqliteStore) InsertTopicTree(t TopicTreeInput) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM topic_tree`); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO topic_tree (parent_id, child_id, resolution, topic_label) VALUES (?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for i := 0; i < t.ResolutionsCount(); i++ {
		members := t.ResolutionMembers(i)
		for label, ids := range members {
			for _, id := range ids {
				if _, err := stmt.Exec(nil, id, i, label); err != nil {
					return fmt.Errorf("insert topic_tree %s@%d: %w", id, i, err)
				}
			}
		}
	}
	return tx.Commit()
}

// InsertBlobs stores per-node source slices keyed by node ID.
func (s *sqliteStore) InsertBlobs(blobs map[string][]byte) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO blobs (node_id, source) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for id, b := range blobs {
		if _, err := stmt.Exec(id, b); err != nil {
			return fmt.Errorf("insert blob %s: %w", id, err)
		}
	}
	return tx.Commit()
}

// RebuildFTS reloads the FTS5 virtual table from the nodes content table.
func (s *sqliteStore) RebuildFTS() error {
	_, err := s.db.Exec(`INSERT INTO nodes_fts(nodes_fts) VALUES('rebuild')`)
	return err
}

// DistinctFilePaths returns the unique file_path values recorded on nodes
// for the given language. Used by `ckg audit` to compare the DB's actual
// file inclusion set against an authoritative reference (e.g. the Go build
// system's go/packages.Load output). Empty slice when no rows match.
//
// The `file_path != ''` predicate is defensive — currently every node-emitting
// site populates FilePath unconditionally — but kept so that introducing a
// new node type (e.g. cross-file aggregator) without a file_path won't
// silently inflate the audit set with empty-string paths.
func (s *sqliteStore) DistinctFilePaths(language string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT file_path FROM nodes WHERE language = ? AND file_path != ''`,
		language)
	if err != nil {
		return nil, fmt.Errorf("distinct file_path (lang=%q): %w", language, err)
	}
	defer rows.Close()
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

// QueryEdgesByType returns all edges whose type matches t. Used by tests
// and downstream consumers (eval/MCP) that want to pull edges by relation
// kind without scanning the full table.
//
// dispatch_kind (schema 1.7) is COALESCE'd to the empty string so pre-1.7
// DBs (where the column doesn't exist post-ALTER, or the row was inserted
// before the migration ran) still scan cleanly.
func (s *sqliteStore) QueryEdgesByType(t string) ([]types.Edge, error) {
	rows, err := s.db.Query(`SELECT id, src, dst, type, file_path, line, count, confidence, COALESCE(dispatch_kind,'')
		FROM edges WHERE type = ?`, t)
	if err != nil {
		return nil, fmt.Errorf("query edges by type %q: %w", t, err)
	}
	defer rows.Close()
	var out []types.Edge
	for rows.Next() {
		var e types.Edge
		var fp sql.NullString
		var line sql.NullInt64
		var conf string
		if err := rows.Scan(&e.ID, &e.Src, &e.Dst, &e.Type, &fp, &line, &e.Count, &conf, &e.DispatchKind); err != nil {
			return nil, fmt.Errorf("scan edge row: %w", err)
		}
		if fp.Valid {
			e.FilePath = fp.String
		}
		if line.Valid {
			e.Line = int(line.Int64)
		}
		e.Confidence = types.Confidence(conf)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edge rows: %w", err)
	}
	return out, nil
}

// HierarchyRow is the wire shape returned by LoadHierarchy. ParentID may be
// empty for top-level topic communities (resolution=0), so callers must
// treat "" as a sentinel for "root".
type HierarchyRow struct {
	ParentID   string `json:"parent_id"`
	ChildID    string `json:"child_id"`
	Level      int    `json:"level"`
	TopicLabel string `json:"topic_label,omitempty"`
}

// LoadHierarchy returns the package tree (kind="pkg") or topic tree
// (kind="topic") as a flat slice. The two trees share the wire shape so the
// viewer can swap data sources without reshaping.
func (s *sqliteStore) LoadHierarchy(kind string) ([]HierarchyRow, error) {
	var query string
	switch kind {
	case "pkg":
		query = `SELECT parent_id, child_id, level, '' FROM pkg_tree`
	case "topic":
		query = `SELECT COALESCE(parent_id,''), child_id, resolution, COALESCE(topic_label,'') FROM topic_tree`
	default:
		return nil, fmt.Errorf("unknown hierarchy kind %q", kind)
	}
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query hierarchy %q: %w", kind, err)
	}
	defer rows.Close()
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

// nodeColumns is the explicit column list used by every SELECT that feeds
// scanNodes. Keeping it in one place avoids SELECT * surprises if the
// schema gains a column later.
const nodeColumns = `id, type, name, qualified_name, file_path,
	start_line, end_line, start_byte, end_byte, language,
	COALESCE(visibility,''), COALESCE(signature,''), COALESCE(doc_comment,''),
	COALESCE(complexity,0), in_degree, out_degree, pagerank, usage_score,
	confidence, COALESCE(sub_kind,'')`

// QueryNodes returns either top-level packages (when parent is empty) or
// the children of parent via the pkg_tree join. Limit caps the result set.
func (s *sqliteStore) QueryNodes(parent string, limit int) ([]types.Node, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if parent == "" {
		rows, err = s.db.Query(`SELECT `+nodeColumns+` FROM nodes WHERE type='Package' LIMIT ?`, limit)
	} else {
		rows, err = s.db.Query(`SELECT `+nodeColumns+` FROM nodes n
			JOIN pkg_tree p ON p.child_id = n.id WHERE p.parent_id = ? LIMIT ?`,
			parent, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("query nodes (parent=%q): %w", parent, err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// TopNodes returns the top-N nodes by the chosen ranking metric, descending.
// Used by the viewer boot path so the initial canvas shows hub symbols
// (functions, methods, types) rather than disconnected Package nodes.
//
// Tie-break by id ASC keeps the result deterministic across calls — without
// it equal-rank rows can swap positions on every query, which would make
// the boot view jitter between page loads.
func (s *sqliteStore) TopNodes(metric string, limit int, excludeTypes ...string) ([]types.Node, error) {
	col, err := topMetricColumn(metric)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 200
	}
	// excludeTypes builds a "type NOT IN (?,?,…)" clause. Building the
	// placeholders by hand (rather than using a helper) is fine here —
	// the values flow through ? binding so this is not an injection
	// vector; we only interpolate the placeholder count itself.
	whereClause := ""
	args := []any{}
	if len(excludeTypes) > 0 {
		placeholders := make([]string, len(excludeTypes))
		for i, t := range excludeTypes {
			placeholders[i] = "?"
			args = append(args, t)
		}
		whereClause = " WHERE type NOT IN (" + strings.Join(placeholders, ",") + ")"
	}
	args = append(args, limit)
	rows, err := s.db.Query(
		`SELECT `+nodeColumns+` FROM nodes`+whereClause+` ORDER BY `+col+` DESC, id ASC LIMIT ?`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("top nodes (metric=%q): %w", metric, err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// topMetricColumn whitelists metric→column names. Whitelist (not f-string)
// because the value reaches a SQL ORDER BY position where parameter binding
// is not allowed — a bug here would be a SQL injection vector.
func topMetricColumn(metric string) (string, error) {
	switch metric {
	case "pagerank":
		return "pagerank", nil
	case "usage":
		return "usage_score", nil
	default:
		return "", ErrInvalidMetric
	}
}

// EdgeCountsByType returns total edge count per type across the whole
// graph. Single GROUP BY query; cheap. Viewer (Track D) uses this to
// render "G2 Semantic 758" badges so users can see axis weight without
// a separate scan of the full edges table.
func (s *sqliteStore) EdgeCountsByType() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT type, COUNT(*) FROM edges GROUP BY type`)
	if err != nil {
		return nil, fmt.Errorf("edge counts by type: %w", err)
	}
	defer rows.Close()
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

// queryEdgesChunk is the per-chunk size for QueryEdgesForNodes. SQLite's
// SQLITE_MAX_VARIABLE_NUMBER caps single-statement parameters; default is 999
// on older builds, 32766 on modernc.org/sqlite — but go-stablenet's 217 K
// nodes would breach either limit when each chunk emits 2N params (src + dst).
// 400 ids per chunk = 800 params, comfortably below the conservative 999
// ceiling and well under the modern 32 K. Per § 3 Q5 in the G6 v3 redesign:
// "chunked QueryEdgesForNodes" is the named fix for this exact bottleneck.
const queryEdgesChunk = 400

// QueryEdgesForNodes returns every edge that has src OR dst in ids. Used by
// the viewer to expand a neighbourhood by node selection AND by the partial-
// cache rebuild path to reload cross-file edges between cached files.
//
// Chunked by queryEdgesChunk because a single IN(?,?,...) expression with
// > 999 placeholders exceeds SQLITE_MAX_VARIABLE_NUMBER on older SQLite
// builds. Edges that match BOTH a src chunk and a dst chunk would be
// returned twice — deduped by the seen-by-id map below.
func (s *sqliteStore) QueryEdgesForNodes(ids []string) ([]types.Edge, error) {
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
		ph := placeholders(len(chunk))
		q := `SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence, COALESCE(dispatch_kind,'')
		      FROM edges WHERE src IN (` + ph + `) OR dst IN (` + ph + `)`
		args := make([]any, 0, 2*len(chunk))
		for _, id := range chunk {
			args = append(args, id)
		}
		for _, id := range chunk {
			args = append(args, id)
		}
		rows, err := s.db.Query(q, args...)
		if err != nil {
			return nil, fmt.Errorf("query edges chunk [%d:%d] of %d: %w", start, end, len(ids), err)
		}
		es, err := scanEdges(rows)
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

// GetBlob returns the raw source slice persisted for node id. Returns
// sql.ErrNoRows when no blob exists (e.g. Package nodes have no body).
func (s *sqliteStore) GetBlob(id string) ([]byte, error) {
	var b []byte
	err := s.db.QueryRow(`SELECT source FROM blobs WHERE node_id = ?`, id).Scan(&b)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// SearchFTS executes an FTS5 MATCH against nodes_fts and returns the joined
// node rows. Caller is responsible for forming a valid FTS5 query string.
//
// The projection is fully qualified with the n.* alias because nodes_fts
// shares column names (name, qualified_name, signature, doc_comment) with
// the nodes content table — bare references would be ambiguous.
func (s *sqliteStore) SearchFTS(q string, limit int) ([]types.Node, error) {
	rows, err := s.db.Query(`SELECT n.id, n.type, n.name, n.qualified_name, n.file_path,
		n.start_line, n.end_line, n.start_byte, n.end_byte, n.language,
		COALESCE(n.visibility,''), COALESCE(n.signature,''), COALESCE(n.doc_comment,''),
		COALESCE(n.complexity,0), n.in_degree, n.out_degree, n.pagerank, n.usage_score,
		n.confidence, COALESCE(n.sub_kind,'')
		FROM nodes_fts f
		JOIN nodes n ON n.rowid = f.rowid
		WHERE nodes_fts MATCH ? LIMIT ?`, q, limit)
	if err != nil {
		return nil, fmt.Errorf("fts search %q: %w", q, err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// Search is the smart search front door used by the HTTP /api/search
// handler AND the MCP `search_text` / `get_context_for_task` tools. It
// routes between FTS5 (English / token-aligned) and a substring fallback
// (CJK / non-tokenisable), and rewrites benign single-token ASCII queries
// to a prefix match (`gene` → `gene*`) so callers don't need to know
// FTS5 syntax. See docs/VIEWER-ROADMAP.md L1/L2 for the option matrix.
//
// Centralising the routing here removes the divergence between
// handleSearch (auto-prefix + CJK) and buildContext (raw FTS, prose
// queries silently `not_found`); both now call this and get the same
// behaviour.
func (s *sqliteStore) Search(q string, limit int) ([]types.Node, error) {
	if hasNonASCII(q) {
		return s.SearchSubstr(q, limit)
	}
	return s.SearchFTS(rewriteFTSQuery(q), limit)
}

// hasNonASCII reports whether q contains any byte ≥ 0x80. Drives the
// CJK-routing branch in Search.
func hasNonASCII(q string) bool {
	for i := 0; i < len(q); i++ {
		if q[i] >= 0x80 {
			return true
		}
	}
	return false
}

// rewriteFTSQuery turns a casual user query into something FTS5 actually
// matches. The default FTS5 semantics on multi-token input is AND, so a
// prose description like "how does block validation work in consensus"
// returns zero hits because no doc contains all seven tokens. We instead:
//
//   - power-user mode: any sigil (* " ( ) :) → pass through verbatim.
//   - single token, length ≥ 2 → append `*` (prefix match: gene → gene*).
//   - multi-token: drop tokens shorter than 3 chars (stop-word heuristic),
//     prefix-tag tokens length ≥ 4, OR them together so any one match
//     surfaces a candidate. The downstream scoring (BM25 + PageRank +
//     usage) re-ranks the candidates so this OR-broadening doesn't
//     degrade quality on terms that are uniquely informative.
//
// Returning q unchanged when no useful tokens survive (`""`, `"a b"`)
// lets FTS5 surface its own no-hits behaviour.
func rewriteFTSQuery(q string) string {
	if strings.ContainsAny(q, `*"():`) {
		return q
	}
	fields := strings.Fields(q)
	if len(fields) == 0 {
		return q
	}
	if len(fields) == 1 {
		if len(fields[0]) >= 2 {
			return fields[0] + "*"
		}
		return q
	}
	parts := make([]string, 0, len(fields))
	for _, t := range fields {
		if len(t) < 3 {
			continue
		}
		if len(t) >= 4 {
			parts = append(parts, t+"*")
		} else {
			parts = append(parts, t)
		}
	}
	if len(parts) == 0 {
		return q
	}
	return strings.Join(parts, " OR ")
}

// SearchSubstr is a non-FTS fallback for queries the FTS5 unicode61
// tokeniser can't tokenise — primarily CJK input where text contains no
// whitespace separators. It runs `LIKE '%q%'` against name + qualified_name
// and is intentionally O(n) on the nodes table; expect 50–100ms on 200K
// rows. Use only when FTS isn't viable; see docs/VIEWER-ROADMAP.md L1.
func (s *sqliteStore) SearchSubstr(q string, limit int) ([]types.Node, error) {
	pat := "%" + q + "%"
	rows, err := s.db.Query(`SELECT n.id, n.type, n.name, n.qualified_name, n.file_path,
		n.start_line, n.end_line, n.start_byte, n.end_byte, n.language,
		COALESCE(n.visibility,''), COALESCE(n.signature,''), COALESCE(n.doc_comment,''),
		COALESCE(n.complexity,0), n.in_degree, n.out_degree, n.pagerank, n.usage_score,
		n.confidence, COALESCE(n.sub_kind,'')
		FROM nodes n
		WHERE n.name LIKE ? OR n.qualified_name LIKE ? LIMIT ?`, pat, pat, limit)
	if err != nil {
		return nil, fmt.Errorf("substring search %q: %w", q, err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// placeholders returns a comma-separated `?,?,?` of length n. n<=0 returns
// "" so callers can detect the empty case before building a malformed IN().
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	out := make([]byte, 0, 2*n-1)
	out = append(out, '?')
	for i := 1; i < n; i++ {
		out = append(out, ',', '?')
	}
	return string(out)
}

// scanNodes drains rows assuming the SELECT projects nodeColumns in order.
// All nullable columns are pre-COALESCE'd at the SQL layer so we can scan
// directly into string/int fields without sql.NullString plumbing.
func scanNodes(rows *sql.Rows) ([]types.Node, error) {
	var out []types.Node
	for rows.Next() {
		var n types.Node
		var conf string
		if err := rows.Scan(&n.ID, &n.Type, &n.Name, &n.QualifiedName, &n.FilePath,
			&n.StartLine, &n.EndLine, &n.StartByte, &n.EndByte, &n.Language,
			&n.Visibility, &n.Signature, &n.DocComment, &n.Complexity,
			&n.InDegree, &n.OutDegree, &n.PageRank, &n.UsageScore,
			&conf, &n.SubKind); err != nil {
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

// scanEdges drains rows produced by QueryEdgesForNodes (file_path/line are
// COALESCE'd in the SELECT, so direct scan is safe here too). dispatch_kind
// is the trailing column added in schema 1.7 (Track C P1b) — empty string
// for every non-`invokes` edge.
func scanEdges(rows *sql.Rows) ([]types.Edge, error) {
	var out []types.Edge
	for rows.Next() {
		var e types.Edge
		var conf string
		if err := rows.Scan(&e.ID, &e.Src, &e.Dst, &e.Type, &e.FilePath, &e.Line, &e.Count, &conf, &e.DispatchKind); err != nil {
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

// FindSymbol returns nodes whose qualified_name matches name. When exact is
// true, only equality matches are returned; when false, a LIKE '%.<name>'
// suffix match is also accepted (so "Foo" hits "pkg.Foo"). lang optionally
// filters by language. Capped at 100 rows to bound MCP response size.
func (s *sqliteStore) FindSymbol(name, lang string, exact bool) ([]types.Node, error) {
	args := []any{}
	q := `SELECT ` + nodeColumns + ` FROM nodes WHERE 1=1 `
	if exact {
		q += `AND qualified_name = ? `
		args = append(args, name)
	} else {
		q += `AND (qualified_name = ? OR qualified_name LIKE ?) `
		args = append(args, name, "%."+name)
	}
	if lang != "" {
		q += `AND language = ? `
		args = append(args, lang)
	}
	q += `LIMIT 100`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("find symbol %q: %w", name, err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// NeighborhoodByQname returns BFS expansion up to depth starting from any
// node whose qualified_name == qname. When reverse is true, expansion follows
// edges backwards (callers); otherwise it follows them forwards (callees).
// Result includes the seed nodes plus all nodes reachable within depth hops.
//
// Optional `edgeTypes` filters which edges count for traversal. Empty
// (the default) follows every edge type — preserves the original
// get_subgraph semantics. Pass e.g. ("calls","invokes") to restrict
// find_callers / find_callees to actual call edges and skip the
// containment / definition relationships that share the same Store.
func (s *sqliteStore) NeighborhoodByQname(qname string, depth int, reverse bool, edgeTypes ...string) ([]types.Node, []types.Edge, error) {
	roots, err := s.FindSymbol(qname, "", true)
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
		var err error
		if reverse {
			es, err = s.edgesPointingTo(frontier, edgeTypes)
		} else {
			es, err = s.edgesFrom(frontier, edgeTypes)
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

// SubgraphByQname returns BFS expansion in BOTH directions up to depth. Node
// set is the union of forward and reverse traversals from qname's roots.
// Always traverses every edge type (passing no filter) so callers like
// `get_subgraph` see the full structural picture.
func (s *sqliteStore) SubgraphByQname(qname string, depth int) ([]types.Node, []types.Edge, error) {
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

// edgesFrom returns every edge whose src is in ids. When edgeTypes is
// non-empty, the result is filtered to those types (e.g. just `calls`).
func (s *sqliteStore) edgesFrom(ids []string, edgeTypes []string) ([]types.Edge, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	q := `SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence, COALESCE(dispatch_kind,'')
		FROM edges WHERE src IN (` + placeholders(len(ids)) + `)`
	args := anys(ids)
	if len(edgeTypes) > 0 {
		q += ` AND type IN (` + placeholders(len(edgeTypes)) + `)`
		args = append(args, anys(edgeTypes)...)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("edges from %d ids: %w", len(ids), err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// edgesPointingTo returns every edge whose dst is in ids. When edgeTypes
// is non-empty, the result is filtered to those types.
func (s *sqliteStore) edgesPointingTo(ids []string, edgeTypes []string) ([]types.Edge, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	q := `SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence, COALESCE(dispatch_kind,'')
		FROM edges WHERE dst IN (` + placeholders(len(ids)) + `)`
	args := anys(ids)
	if len(edgeTypes) > 0 {
		q += ` AND type IN (` + placeholders(len(edgeTypes)) + `)`
		args = append(args, anys(edgeTypes)...)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("edges pointing to %d ids: %w", len(ids), err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// NodesByIDs fetches nodes by primary key. Empty input yields a nil slice
// without hitting the database.
func (s *sqliteStore) NodesByIDs(ids []string) ([]types.Node, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT `+nodeColumns+` FROM nodes WHERE id IN (`+placeholders(len(ids))+`)`, anys(ids)...)
	if err != nil {
		return nil, fmt.Errorf("nodes by %d ids: %w", len(ids), err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// AmbiguousMetaNodes returns Hunk + Commit rows with confidence='AMBIGUOUS'.
// Sorted by start_line DESC so the viewer Recovery panel surfaces the most
// recent unreachable commits first (start_line on Commit rows holds the
// timestamp in their signature column — the SQL ORDER BY is on a column
// the schema enforces NOT NULL).
//
// The dual-type scope (Hunk + Commit) matches the §11.3 contract — other
// AMBIGUOUS rows (TS resolve multi-candidate, Track C unresolvable
// dispatch) are precision signals the LLM should still see and stay
// out of the recovery panel.
func (s *sqliteStore) AmbiguousMetaNodes() ([]types.Node, error) {
	rows, err := s.db.Query(`SELECT ` + nodeColumns + ` FROM nodes
		WHERE confidence = 'AMBIGUOUS' AND type IN ('Hunk', 'Commit')
		ORDER BY type, qualified_name`)
	if err != nil {
		return nil, fmt.Errorf("ambiguous meta nodes: %w", err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// AllNodes returns every node in the graph. Order is unspecified — callers
// (validate) sort if needed. Used by `ckg validate` to reconstruct the
// in-memory graph for SchemaValidator.
func (s *sqliteStore) AllNodes() ([]types.Node, error) {
	rows, err := s.db.Query(`SELECT ` + nodeColumns + ` FROM nodes`)
	if err != nil {
		return nil, fmt.Errorf("all nodes: %w", err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// AllEdges returns every edge in the graph. Pair with AllNodes for full
// graph reconstruction in `ckg validate`. dispatch_kind (schema 1.7) is the
// trailing column; COALESCE'd so pre-1.7 rows scan as empty string.
func (s *sqliteStore) AllEdges() ([]types.Edge, error) {
	rows, err := s.db.Query(`SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence, COALESCE(dispatch_kind,'') FROM edges`)
	if err != nil {
		return nil, fmt.Errorf("all edges: %w", err)
	}
	defer rows.Close()
	var out []types.Edge
	for rows.Next() {
		var e types.Edge
		var fp string
		var line int
		var conf string
		if err := rows.Scan(&e.ID, &e.Src, &e.Dst, &e.Type, &fp, &line, &e.Count, &conf, &e.DispatchKind); err != nil {
			return nil, fmt.Errorf("scan edge row: %w", err)
		}
		e.FilePath = fp
		e.Line = line
		e.Confidence = types.Confidence(conf)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate edge rows: %w", err)
	}
	return out, nil
}

// mapKeys is a generic helper that returns the keys of a map as a slice.
// Used by NeighborhoodByQname to convert the seen-set into a frontier.
func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// anys converts a []string into []any so it can be spread as variadic args
// to (*sql.DB).Query without callers writing the conversion every time.
func anys(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// NodesByFilePath returns every node whose file_path equals path. Empty path
// returns nil (no rows). Used by buildpipe to reload nodes for files that hit
// the A3 incremental cache instead of re-parsing them.
func (s *sqliteStore) NodesByFilePath(path string) ([]types.Node, error) {
	if path == "" {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT `+nodeColumns+` FROM nodes WHERE file_path = ? ORDER BY start_line`, path)
	if err != nil {
		return nil, fmt.Errorf("nodes by file_path %q: %w", path, err)
	}
	defer rows.Close()
	return scanNodes(rows)
}

// EdgesByFilePath returns every edge whose file_path equals path. Edges
// without a file_path (cross-file links emitted by graph.Build) are NOT
// returned — the cache only reuses per-file edges.
func (s *sqliteStore) EdgesByFilePath(path string) ([]types.Edge, error) {
	if path == "" {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence, COALESCE(dispatch_kind,'')
		FROM edges WHERE file_path = ?`, path)
	if err != nil {
		return nil, fmt.Errorf("edges by file_path %q: %w", path, err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// BlobsByFilePath returns blobs keyed by node_id for every node whose
// file_path equals path. Empty result is a non-nil empty map.
func (s *sqliteStore) BlobsByFilePath(path string) (map[string][]byte, error) {
	out := map[string][]byte{}
	if path == "" {
		return out, nil
	}
	rows, err := s.db.Query(`SELECT b.node_id, b.source FROM blobs b
		JOIN nodes n ON n.id = b.node_id WHERE n.file_path = ?`, path)
	if err != nil {
		return nil, fmt.Errorf("blobs by file_path %q: %w", path, err)
	}
	defer rows.Close()
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

// DeleteNodesByFilePath drops every node whose file_path matches. The schema
// 1.2 FK definitions (edges.src/dst, blobs.node_id, pkg_tree.*, topic_tree.*)
// all carry ON DELETE CASCADE, so dependent rows are removed by SQLite
// automatically inside this statement. Pre-1.2 DBs lack CASCADE; Open()
// reports a warning when foreign_key_check fails on the schema invariant.
func (s *sqliteStore) DeleteNodesByFilePath(path string) error {
	if path == "" {
		return nil
	}
	if _, err := s.db.Exec(`DELETE FROM nodes WHERE file_path = ?`, path); err != nil {
		return fmt.Errorf("delete nodes by file_path %q: %w", path, err)
	}
	return nil
}

// DeleteEdgesByType drops every edge of type t. Used by the incremental
// build path to clear cross-language edges (e.g. binds_to) whose endpoints
// span files — they don't carry their own file_path and so are not reached
// by DeleteNodesByFilePath cascade.
func (s *sqliteStore) DeleteEdgesByType(t string) error {
	if t == "" {
		return nil
	}
	if _, err := s.db.Exec(`DELETE FROM edges WHERE type = ?`, t); err != nil {
		return fmt.Errorf("delete edges by type %q: %w", t, err)
	}
	return nil
}

// PendingRefRow is the storage wire shape for parse.PendingRef. Defined in
// persist (rather than reusing parse.PendingRef directly) so persist stays
// import-free of the parse package — buildpipe bridges the two when emitting
// from cold path or reloading for partial-cache rebuild.
//
// G6 v3 (schema 1.5): persisting pending refs lets the partial path replay
// Pass 2 over the merged dirty + cached input set without re-parsing cached
// files. Without this table the cached-side pending refs were silently
// dropped (the v1/v2 cross-file edge regression).
//
// DispatchKind (Track C P1b, schema 1.7): mirrors the edges table column —
// preserves the AST-time dispatch classification across the cache boundary.
// Empty for static `calls`.
type PendingRefRow struct {
	FilePath     string
	SrcID        string
	TargetQName  string
	EdgeType     string
	Line         int
	HintFile     string
	DispatchKind string
}

// InsertPendingRefs bulk-inserts pending_refs rows. INSERT OR IGNORE is used
// because the (file_path, src_id, target_qname, edge_type, line) primary key
// can naturally collide when a single source line emits the same logical ref
// twice (e.g. a doubly-imported symbol surfacing in two pending-ref sites of
// the same file). Cold path always wipes the table beforehand via openColdStore;
// partial path relies on FK CASCADE from DeleteNodesByFilePath. Either way,
// IGNORE guards against PK violations without forcing a SELECT-then-INSERT
// pattern in the hot loop.
func (s *sqliteStore) InsertPendingRefs(refs []PendingRefRow) error {
	if len(refs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO pending_refs
		(file_path, src_id, target_qname, edge_type, line, hint_file, dispatch_kind)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range refs {
		if _, err := stmt.Exec(r.FilePath, r.SrcID, r.TargetQName,
			r.EdgeType, r.Line, r.HintFile, r.DispatchKind); err != nil {
			return fmt.Errorf("insert pending_ref %s→%s: %w", r.SrcID, r.TargetQName, err)
		}
	}
	return tx.Commit()
}

// PendingRefsByFilePath returns every pending_refs row where file_path matches.
// Empty path returns nil. Used by the partial-cache rebuild path: cached files
// have their pending refs reloaded so Pass 2 Resolve sees the same input set
// it would have seen under cold rebuild.
func (s *sqliteStore) PendingRefsByFilePath(path string) ([]PendingRefRow, error) {
	if path == "" {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT file_path, src_id, target_qname, edge_type, line,
		COALESCE(hint_file,''), COALESCE(dispatch_kind,'') FROM pending_refs WHERE file_path = ?`, path)
	if err != nil {
		return nil, fmt.Errorf("pending_refs by file_path %q: %w", path, err)
	}
	defer rows.Close()
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
// nodes are deleted — the query joins pending_refs to nodes still in DB.
// Returns nil when dirtyPaths is empty.
func (s *sqliteStore) ReverseDepsForFiles(dirtyPaths []string) ([]string, error) {
	if len(dirtyPaths) == 0 {
		return nil, nil
	}
	ph := placeholders(len(dirtyPaths))
	// Double the args: one set for the IN(dirty file_path on nodes), one for
	// the NOT IN(dirty file_path on pending_refs — exclude dirty files themselves).
	dirtyArgs := anys(dirtyPaths)
	allArgs := make([]any, 0, len(dirtyArgs)*2)
	allArgs = append(allArgs, dirtyArgs...)
	allArgs = append(allArgs, dirtyArgs...)
	// pending_refs.target_qname stores the unresolved AST name (e.g. "Helper"),
	// while nodes.qualified_name is fully-qualified (e.g. "edgepin.Helper").
	// The LIKE arm matches the suffix after the last dot — mirrors simpleName()
	// in resolve.go so C1 finds the same candidates as Pass 2 Resolve does.
	q := `SELECT DISTINCT pr.file_path
	      FROM pending_refs pr
	      INNER JOIN nodes n ON (
	          n.qualified_name = pr.target_qname
	          OR n.qualified_name LIKE ('%.' || pr.target_qname)
	      )
	      WHERE n.file_path IN (` + ph + `)
	        AND pr.file_path NOT IN (` + ph + `)`
	rows, err := s.db.Query(q, allArgs...)
	if err != nil {
		return nil, fmt.Errorf("reverse deps for %d paths: %w", len(dirtyPaths), err)
	}
	defer rows.Close()
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

// InsertEdges bulk-inserts edges (transactional). dispatch_kind (schema 1.7,
// Track C P1b) is written as the empty string for non-`invokes` edges; SQLite
// stores it as a regular TEXT value either way.
func (s *sqliteStore) InsertEdges(edges []types.Edge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO edges
		(src, dst, type, file_path, line, count, confidence, dispatch_kind)
		VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range edges {
		if _, err := stmt.Exec(e.Src, e.Dst, string(e.Type), e.FilePath, e.Line,
			e.Count, string(e.Confidence), e.DispatchKind); err != nil {
			return fmt.Errorf("insert edge %s->%s: %w", e.Src, e.Dst, err)
		}
	}
	return tx.Commit()
}
