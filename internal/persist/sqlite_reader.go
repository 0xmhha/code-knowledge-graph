package persist

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// HierarchyRow is the wire shape returned by LoadHierarchy. ParentID may be
// empty for top-level topic communities (resolution=0), so callers must
// treat "" as a sentinel for "root".
type HierarchyRow struct {
	ParentID   string `json:"parent_id"`
	ChildID    string `json:"child_id"`
	Level      int    `json:"level"`
	TopicLabel string `json:"topic_label,omitempty"`
}

// GetNode fetches a node by ID. Returns sql.ErrNoRows if not found.
func (s *sqliteStore) GetNode(id string) (types.Node, error) {
	row := s.db.QueryRow(`SELECT id, type, name, qualified_name, COALESCE(canonical_id,''), file_path,
		start_line, end_line, start_byte, end_byte, language, visibility,
		signature, doc_comment, complexity, in_degree, out_degree, pagerank,
		usage_score, confidence, sub_kind, COALESCE(attrs,'') FROM nodes WHERE id = ?`, id)
	var n types.Node
	var conf, attrs string
	err := row.Scan(&n.ID, &n.Type, &n.Name, &n.QualifiedName, &n.CanonicalID, &n.FilePath,
		&n.StartLine, &n.EndLine, &n.StartByte, &n.EndByte, &n.Language,
		&n.Visibility, &n.Signature, &n.DocComment, &n.Complexity,
		&n.InDegree, &n.OutDegree, &n.PageRank, &n.UsageScore,
		&conf, &n.SubKind, &attrs)
	if err != nil {
		return n, err
	}
	n.Confidence = types.Confidence(conf)
	unmarshalNodeAttrs(attrs, &n)
	return n, nil
}

// FindByCanonicalID fetches the single node whose canonical_id matches exactly.
// canonical_id is the globally-unique, import-path-qualified identity (ADR-0001),
// so the match is unambiguous. Returns found=false (nil error) when nothing
// matches or canonicalID is empty. LIMIT 1 is defensive — canonical_id is unique
// by construction, but it keeps the contract single-valued regardless.
func (s *sqliteStore) FindByCanonicalID(canonicalID string) (types.Node, bool, error) {
	var n types.Node
	if canonicalID == "" {
		return n, false, nil
	}
	row := s.db.QueryRow(`SELECT id, type, name, qualified_name, COALESCE(canonical_id,''), file_path,
		start_line, end_line, start_byte, end_byte, language, visibility,
		signature, doc_comment, complexity, in_degree, out_degree, pagerank,
		usage_score, confidence, sub_kind, COALESCE(attrs,'') FROM nodes
		WHERE canonical_id = ? LIMIT 1`, canonicalID)
	var conf, attrs string
	err := row.Scan(&n.ID, &n.Type, &n.Name, &n.QualifiedName, &n.CanonicalID, &n.FilePath,
		&n.StartLine, &n.EndLine, &n.StartByte, &n.EndByte, &n.Language,
		&n.Visibility, &n.Signature, &n.DocComment, &n.Complexity,
		&n.InDegree, &n.OutDegree, &n.PageRank, &n.UsageScore,
		&conf, &n.SubKind, &attrs)
	if err == sql.ErrNoRows {
		return n, false, nil
	}
	if err != nil {
		return n, false, err
	}
	n.Confidence = types.Confidence(conf)
	unmarshalNodeAttrs(attrs, &n)
	return n, true, nil
}

// DistinctFilePaths returns the unique file_path values recorded on nodes
// for the given language. Used by `ckg audit` to compare the DB's actual
// file inclusion set against an authoritative reference (e.g. the Go build
// system's go/packages.Load output). Empty slice when no rows match.
//
// The `file_path != ”` predicate is defensive — currently every node-emitting
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
		ph := make([]string, len(excludeTypes))
		for i, t := range excludeTypes {
			ph[i] = "?"
			args = append(args, t)
		}
		whereClause = " WHERE type NOT IN (" + strings.Join(ph, ",") + ")"
	}
	args = append(args, limit)
	rows, err := s.db.Query(
		`SELECT `+nodeColumns+` FROM nodes`+whereClause+` ORDER BY `+col+` DESC, id ASC LIMIT ?`,
		args...)
	if err != nil {
		return nil, fmt.Errorf("top nodes (metric=%q): %w", metric, err)
	}
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
		_ = rows.Close()

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

// FindSymbol returns nodes whose qualified_name matches name. When exact is
// true, only equality matches are returned; when false, a LIKE '%.<name>'
// suffix match is also accepted (so "Foo" hits "pkg.Foo"). Capped at 100
// rows to bound MCP response size.
//
// opts.Language pushes a `language = ?` predicate when non-empty.
// opts.Kinds pushes a `type IN (...)` predicate when non-empty — CKG-4
// fix that removes cks Stage 2's N round-trips for `arch_explain` intent
// (one query per requested SymbolKind). Empty Kinds returns every type.
func (s *sqliteStore) FindSymbol(name string, exact bool, opts FindSymbolOptions) ([]types.Node, error) {
	args := []any{}
	q := `SELECT ` + nodeColumns + ` FROM nodes WHERE 1=1 `
	if exact {
		q += `AND qualified_name = ? `
		args = append(args, name)
	} else {
		// simple_name equi-join replaces a leading-wildcard LIKE so the
		// suffix match ("Foo" hits "pkg.Foo") uses idx_nodes_simple_name.
		// COLLATE NOCASE preserves the old LIKE's case-insensitivity.
		q += `AND (qualified_name = ? OR simple_name = ? COLLATE NOCASE) `
		args = append(args, name, name)
	}
	if opts.Language != "" {
		q += `AND language = ? `
		args = append(args, opts.Language)
	}
	if len(opts.Kinds) > 0 {
		q += `AND type IN (` + placeholders(len(opts.Kinds)) + `) `
		for _, k := range opts.Kinds {
			args = append(args, string(k))
		}
	}
	q += `LIMIT 100`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("find symbol %q: %w", name, err)
	}
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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
	defer func() { _ = rows.Close() }()
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

// GetNodePRs returns the PR breadcrumbs for a single node, descending
// by merge timestamp, optionally truncated to merges before cutoff
// (ckg-NEW-3). The cutoff comparison runs in SQL as a string comparison
// against the RFC3339-UTC text stored in merged_at — lexicographic
// ordering coincides with chronological ordering for that format, so
// no datetime() coercion is needed.
func (s *sqliteStore) GetNodePRs(nodeID string, cutoff time.Time) ([]types.PRRef, error) {
	sql := `SELECT number,
		COALESCE(title, ''), COALESCE(summary, ''),
		COALESCE(base_sha, ''), COALESCE(head_sha, ''),
		merged_at, COALESCE(repo, '')
		FROM node_prs WHERE node_id = ?`
	args := []any{nodeID}
	if !cutoff.IsZero() {
		sql += ` AND merged_at < ?`
		args = append(args, cutoff.UTC().Format(time.RFC3339))
	}
	sql += ` ORDER BY merged_at DESC`
	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, fmt.Errorf("node_prs for %s: %w", nodeID, err)
	}
	defer func() { _ = rows.Close() }()
	var out []types.PRRef
	for rows.Next() {
		var (
			r        types.PRRef
			mergedAt string
		)
		if err := rows.Scan(&r.Number, &r.Title, &r.Summary,
			&r.BaseSHA, &r.HeadSHA, &mergedAt, &r.Repo); err != nil {
			return nil, fmt.Errorf("scan node_pr: %w", err)
		}
		if mergedAt != "" {
			t, perr := time.Parse(time.RFC3339, mergedAt)
			if perr == nil {
				r.MergedAtUTC = t.UTC()
			}
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate node_prs: %w", err)
	}
	return out, nil
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
	defer func() { _ = rows.Close() }()
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
	// The simple_name arm matches the short name (last dotted segment) via an
	// indexed equi-join — the same candidates the old `LIKE '%.' || target`
	// found and the resolver's simpleName() uses, but without the leading
	// wildcard that defeated the index.
	q := `SELECT DISTINCT pr.file_path
	      FROM pending_refs pr
	      INNER JOIN nodes n ON (
	          n.qualified_name = pr.target_qname
	          OR n.simple_name = pr.target_qname COLLATE NOCASE
	      )
	      WHERE n.file_path IN (` + ph + `)
	        AND pr.file_path NOT IN (` + ph + `)`
	rows, err := s.db.Query(q, allArgs...)
	if err != nil {
		return nil, fmt.Errorf("reverse deps for %d paths: %w", len(dirtyPaths), err)
	}
	defer func() { _ = rows.Close() }()
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
