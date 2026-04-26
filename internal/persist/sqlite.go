package persist

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

//go:embed schema.sql
var schemaSQL string

// Store wraps a SQLite database for the CKG graph.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite file at path.
//
// PRAGMAs are passed via DSN so modernc.org/sqlite applies them per-connection.
// This is required because PRAGMA foreign_keys / journal_mode are connection-scoped:
// setting them once via Migrate() would not propagate to other pooled connections,
// leaving FK constraints unenforced and WAL inactive on most queries.
func Open(path string) (*Store, error) {
	dsn := path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// OpenReadOnly opens a SQLite file in read-only mode (used by serve/mcp).
// FK pragma is enforced per-connection via DSN; WAL is omitted because read-only
// mode cannot mutate journal state.
func OpenReadOnly(path string) (*Store, error) {
	dsn := path + "?mode=ro&immutable=1&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite ro at %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error { return s.db.Close() }

// Migrate creates tables if they don't already exist.
func (s *Store) Migrate() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// InsertNodes bulk-inserts nodes (transactional).
func (s *Store) InsertNodes(nodes []types.Node) error {
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
func (s *Store) GetNode(id string) (types.Node, error) {
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
func (s *Store) InsertPkgTree(edges []ClusterEdge) error {
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
func (s *Store) InsertPkgTreeFromCluster(edges []cluster.PersistClusterEdge) error {
	out := make([]ClusterEdge, len(edges))
	for i, e := range edges {
		out[i] = ClusterEdge(e)
	}
	return s.InsertPkgTree(out)
}

// InsertTopicTree persists multi-resolution Leiden communities. Existing rows
// are dropped first so a full rebuild matches V0 expectations.
func (s *Store) InsertTopicTree(t TopicTreeInput) error {
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
func (s *Store) InsertBlobs(blobs map[string][]byte) error {
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
func (s *Store) RebuildFTS() error {
	_, err := s.db.Exec(`INSERT INTO nodes_fts(nodes_fts) VALUES('rebuild')`)
	return err
}

// QueryEdgesByType returns all edges whose type matches t. Used by tests
// and downstream consumers (eval/MCP) that want to pull edges by relation
// kind without scanning the full table.
func (s *Store) QueryEdgesByType(t string) ([]types.Edge, error) {
	rows, err := s.db.Query(`SELECT id, src, dst, type, file_path, line, count, confidence
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
		if err := rows.Scan(&e.ID, &e.Src, &e.Dst, &e.Type, &fp, &line, &e.Count, &conf); err != nil {
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
func (s *Store) LoadHierarchy(kind string) ([]HierarchyRow, error) {
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
func (s *Store) QueryNodes(parent string, limit int) ([]types.Node, error) {
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

// QueryEdgesForNodes returns every edge that has src OR dst in ids. Used by
// the viewer to expand a neighbourhood by node selection.
func (s *Store) QueryEdgesForNodes(ids []string) ([]types.Edge, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph := placeholders(len(ids))
	q := `SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence
	      FROM edges WHERE src IN (` + ph + `) OR dst IN (` + ph + `)`
	args := make([]any, 0, 2*len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query edges for %d nodes: %w", len(ids), err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// GetBlob returns the raw source slice persisted for node id. Returns
// sql.ErrNoRows when no blob exists (e.g. Package nodes have no body).
func (s *Store) GetBlob(id string) ([]byte, error) {
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
func (s *Store) SearchFTS(q string, limit int) ([]types.Node, error) {
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

// SearchSubstr is a non-FTS fallback for queries the FTS5 unicode61
// tokeniser can't tokenise — primarily CJK input where text contains no
// whitespace separators. It runs `LIKE '%q%'` against name + qualified_name
// and is intentionally O(n) on the nodes table; expect 50–100ms on 200K
// rows. Use only when FTS isn't viable; see docs/VIEWER-ROADMAP.md L1.
func (s *Store) SearchSubstr(q string, limit int) ([]types.Node, error) {
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
// COALESCE'd in the SELECT, so direct scan is safe here too).
func scanEdges(rows *sql.Rows) ([]types.Edge, error) {
	var out []types.Edge
	for rows.Next() {
		var e types.Edge
		var conf string
		if err := rows.Scan(&e.ID, &e.Src, &e.Dst, &e.Type, &e.FilePath, &e.Line, &e.Count, &conf); err != nil {
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
func (s *Store) FindSymbol(name, lang string, exact bool) ([]types.Node, error) {
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
func (s *Store) NeighborhoodByQname(qname string, depth int, reverse bool) ([]types.Node, []types.Edge, error) {
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
			es, err = s.edgesPointingTo(frontier)
		} else {
			es, err = s.edgesFrom(frontier)
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
func (s *Store) SubgraphByQname(qname string, depth int) ([]types.Node, []types.Edge, error) {
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

// edgesFrom returns every edge whose src is in ids.
func (s *Store) edgesFrom(ids []string) ([]types.Edge, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence
		FROM edges WHERE src IN (`+placeholders(len(ids))+`)`, anys(ids)...)
	if err != nil {
		return nil, fmt.Errorf("edges from %d ids: %w", len(ids), err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// edgesPointingTo returns every edge whose dst is in ids.
func (s *Store) edgesPointingTo(ids []string) ([]types.Edge, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT id, src, dst, type, COALESCE(file_path,''), COALESCE(line,0), count, confidence
		FROM edges WHERE dst IN (`+placeholders(len(ids))+`)`, anys(ids)...)
	if err != nil {
		return nil, fmt.Errorf("edges pointing to %d ids: %w", len(ids), err)
	}
	defer rows.Close()
	return scanEdges(rows)
}

// NodesByIDs fetches nodes by primary key. Empty input yields a nil slice
// without hitting the database.
func (s *Store) NodesByIDs(ids []string) ([]types.Node, error) {
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

// InsertEdges bulk-inserts edges (transactional).
func (s *Store) InsertEdges(edges []types.Edge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO edges
		(src, dst, type, file_path, line, count, confidence)
		VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range edges {
		if _, err := stmt.Exec(e.Src, e.Dst, string(e.Type), e.FilePath, e.Line,
			e.Count, string(e.Confidence)); err != nil {
			return fmt.Errorf("insert edge %s->%s: %w", e.Src, e.Dst, err)
		}
	}
	return tx.Commit()
}
