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
