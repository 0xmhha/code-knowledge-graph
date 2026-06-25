package persist

import (
	"fmt"
	"time"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

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

// InsertNodes bulk-inserts nodes (transactional). The trailing
// attrs column carries the W11 V7 JSON blob (SlotIndex,
// HasAssembly, IsFunctionTyped, HasFunctionPointerCall,
// HasExternalCall, YulBuiltins, HasInheritanceMROFallback, …).
func (s *sqliteStore) InsertNodes(nodes []types.Node) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO nodes
		(id, type, name, qualified_name, canonical_id, file_path, start_line, end_line,
		 start_byte, end_byte, language, visibility, signature, doc_comment,
		 complexity, in_degree, out_degree, pagerank, usage_score, confidence, sub_kind, attrs, search_tokens, simple_name)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, n := range nodes {
		attrs := marshalNodeAttrs(&n)
		tokens := buildSearchTokens(n.Name, n.QualifiedName)
		if _, err := stmt.Exec(n.ID, n.Type, n.Name, n.QualifiedName, n.CanonicalID, n.FilePath,
			n.StartLine, n.EndLine, n.StartByte, n.EndByte, n.Language,
			n.Visibility, n.Signature, n.DocComment, n.Complexity,
			n.InDegree, n.OutDegree, n.PageRank, n.UsageScore,
			string(n.Confidence), n.SubKind, attrs, tokens, simpleName(n.QualifiedName)); err != nil {
			return fmt.Errorf("insert node %s: %w", n.ID, err)
		}
	}
	return tx.Commit()
}

// InsertEdges bulk-inserts edges (transactional). dispatch_kind (schema 1.7,
// Track C P1b) is written as the empty string for non-`invokes` edges; SQLite
// stores it as a regular TEXT value either way.
func (s *sqliteStore) InsertEdges(edges []types.Edge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(`INSERT INTO edges
		(src, dst, type, file_path, line, count, confidence, dispatch_kind)
		VALUES (?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, e := range edges {
		if _, err := stmt.Exec(e.Src, e.Dst, string(e.Type), e.FilePath, e.Line,
			e.Count, string(e.Confidence), e.DispatchKind); err != nil {
			return fmt.Errorf("insert edge %s->%s: %w", e.Src, e.Dst, err)
		}
	}
	return tx.Commit()
}

// InsertPkgTree bulk-inserts package-tree edges.
func (s *sqliteStore) InsertPkgTree(edges []ClusterEdge) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO pkg_tree (parent_id, child_id, level) VALUES (?,?,?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
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
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM topic_tree`); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO topic_tree (parent_id, child_id, resolution, topic_label) VALUES (?,?,?,?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
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
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO blobs (node_id, source) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for id, b := range blobs {
		if _, err := stmt.Exec(id, b); err != nil {
			return fmt.Errorf("insert blob %s: %w", id, err)
		}
	}
	return tx.Commit()
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
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO pending_refs
		(file_path, src_id, target_qname, edge_type, line, hint_file, dispatch_kind)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for _, r := range refs {
		if _, err := stmt.Exec(r.FilePath, r.SrcID, r.TargetQName,
			r.EdgeType, r.Line, r.HintFile, r.DispatchKind); err != nil {
			return fmt.Errorf("insert pending_ref %s→%s: %w", r.SrcID, r.TargetQName, err)
		}
	}
	return tx.Commit()
}

// InsertNodePRs writes PR breadcrumbs for ckg-NEW-2. INSERT OR REPLACE
// (rather than OR IGNORE) because the rebuild path frequently has new
// title/summary text for the same (node_id, number) — keeping the latest
// commit-message-derived metadata is more useful than rejecting the
// update. PRIMARY KEY (node_id, number) bounds duplicates per node.
func (s *sqliteStore) InsertNodePRs(byNode map[string][]types.PRRef) error {
	if len(byNode) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO node_prs
		(node_id, number, title, summary, base_sha, head_sha, merged_at, repo)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for nodeID, refs := range byNode {
		for _, r := range refs {
			if _, err := stmt.Exec(nodeID, r.Number, r.Title, r.Summary,
				r.BaseSHA, r.HeadSHA, r.MergedAtUTC.UTC().Format(time.RFC3339), r.Repo); err != nil {
				return fmt.Errorf("insert node_pr %s #%d: %w", nodeID, r.Number, err)
			}
		}
	}
	return tx.Commit()
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

// RebuildFTS reloads the FTS5 virtual table from the nodes content table.
func (s *sqliteStore) RebuildFTS() error {
	_, err := s.db.Exec(`INSERT INTO nodes_fts(nodes_fts) VALUES('rebuild')`)
	return err
}
