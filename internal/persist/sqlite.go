package persist

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"

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
