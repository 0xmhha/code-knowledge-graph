package cluster_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// minimalGraph returns a graph with a single package node and no structural edges.
func minimalGraph() *graph.Graph {
	return &graph.Graph{
		Nodes: []types.Node{
			{
				ID: "pkg1", Type: types.NodePackage,
				QualifiedName: "mypkg", Name: "mypkg",
				FilePath: "mypkg/x.go", Language: "go",
				Confidence: types.ConfExtracted,
			},
		},
	}
}

// graphWithPkgAndFile returns a graph with a package and file node so that
// BuildPkgTree produces at least one Edge.
func graphWithPkgAndFile() *graph.Graph {
	mk := func(id, qname string, nt types.NodeType, file string) types.Node {
		return types.Node{
			ID: id, Type: nt,
			QualifiedName: qname, Name: qname,
			FilePath: file, Language: "go",
			Confidence: types.ConfExtracted,
		}
	}
	return &graph.Graph{
		Nodes: []types.Node{
			mk("pkg1", "core", types.NodePackage, "core/x.go"),
			mk("file1", "core/x.go", types.NodeFile, "core/x.go"),
		},
	}
}

// ---------------------------------------------------------------------------
// PkgTree.PersistEdges
// ---------------------------------------------------------------------------

func TestPkgTree_PersistEdges_Empty(t *testing.T) {
	tree := cluster.BuildPkgTree(minimalGraph())
	edges := tree.PersistEdges()
	// a pkg-only graph has no file/decl children → zero edges
	if len(edges) != 0 {
		t.Errorf("want 0 edges for pkg-only graph, got %d", len(edges))
	}
}

func TestPkgTree_PersistEdges_FieldMapping(t *testing.T) {
	tree := cluster.BuildPkgTree(graphWithPkgAndFile())
	edges := tree.PersistEdges()
	if len(edges) == 0 {
		t.Fatal("expected at least one PersistClusterEdge, got 0")
	}
	origEdges := tree.Edges
	if len(edges) != len(origEdges) {
		t.Fatalf("PersistEdges len %d != Edges len %d", len(edges), len(origEdges))
	}
	for i, pe := range edges {
		oe := origEdges[i]
		if pe.ParentID != oe.ParentID {
			t.Errorf("[%d] ParentID: got %q want %q", i, pe.ParentID, oe.ParentID)
		}
		if pe.ChildID != oe.ChildID {
			t.Errorf("[%d] ChildID: got %q want %q", i, pe.ChildID, oe.ChildID)
		}
		if pe.Level != oe.Level {
			t.Errorf("[%d] Level: got %d want %d", i, pe.Level, oe.Level)
		}
	}
}

// ---------------------------------------------------------------------------
// TopicTree.ResolutionsCount
// ---------------------------------------------------------------------------

func TestTopicTree_ResolutionsCount(t *testing.T) {
	tt := &cluster.TopicTree{
		Resolutions: []cluster.Resolution{
			{Gamma: 0.5},
			{Gamma: 1.0},
		},
	}
	if got := tt.ResolutionsCount(); got != 2 {
		t.Errorf("ResolutionsCount: got %d, want 2", got)
	}

	empty := &cluster.TopicTree{}
	if got := empty.ResolutionsCount(); got != 0 {
		t.Errorf("ResolutionsCount on empty TopicTree: got %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// TopicTree.ResolutionGamma
// ---------------------------------------------------------------------------

func TestTopicTree_ResolutionGamma(t *testing.T) {
	tt := &cluster.TopicTree{
		Resolutions: []cluster.Resolution{
			{Gamma: 0.25},
			{Gamma: 1.75},
		},
	}
	if got := tt.ResolutionGamma(0); got != 0.25 {
		t.Errorf("ResolutionGamma(0): got %v, want 0.25", got)
	}
	if got := tt.ResolutionGamma(1); got != 1.75 {
		t.Errorf("ResolutionGamma(1): got %v, want 1.75", got)
	}
}

// ---------------------------------------------------------------------------
// TopicTree.ResolutionMembers
// ---------------------------------------------------------------------------

func TestTopicTree_ResolutionMembers(t *testing.T) {
	tt := &cluster.TopicTree{
		Resolutions: []cluster.Resolution{
			{
				Gamma: 1.0,
				Communities: []cluster.Community{
					{Label: "auth", Members: []string{"node1", "node2"}},
					{Label: "db", Members: []string{"node3"}},
				},
			},
		},
	}

	m := tt.ResolutionMembers(0)
	if len(m) != 2 {
		t.Fatalf("ResolutionMembers(0) len: got %d, want 2", len(m))
	}
	auth, ok := m["auth"]
	if !ok {
		t.Fatal("expected key \"auth\" in members map")
	}
	if len(auth) != 2 || auth[0] != "node1" || auth[1] != "node2" {
		t.Errorf("auth members: got %v, want [node1 node2]", auth)
	}
	db, ok := m["db"]
	if !ok {
		t.Fatal("expected key \"db\" in members map")
	}
	if len(db) != 1 || db[0] != "node3" {
		t.Errorf("db members: got %v, want [node3]", db)
	}
}

func TestTopicTree_ResolutionMembers_Empty(t *testing.T) {
	tt := &cluster.TopicTree{
		Resolutions: []cluster.Resolution{
			{Gamma: 0.5, Communities: nil},
		},
	}
	m := tt.ResolutionMembers(0)
	if len(m) != 0 {
		t.Errorf("empty communities: want empty map, got %v", m)
	}
}
