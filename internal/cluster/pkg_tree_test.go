package cluster_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func TestPkgTreeLevels(t *testing.T) {
	mk := func(id, qname string, t types.NodeType, file string) types.Node {
		return types.Node{ID: id, Type: t, Name: qname, QualifiedName: qname,
			FilePath: file, StartLine: 1, EndLine: 1, EndByte: 1,
			Language: "go", Confidence: types.ConfExtracted}
	}
	g := &graph.Graph{
		Nodes: []types.Node{
			mk("p1", "core", types.NodePackage, "core/x.go"),
			mk("p2", "core/types", types.NodePackage, "core/types/x.go"),
			mk("f1", "core/x.go", types.NodeFile, "core/x.go"),
			mk("fn1", "core.Foo", types.NodeFunction, "core/x.go"),
		},
	}
	tree := cluster.BuildPkgTree(g)
	// Numeric levels follow the plan Step 3 formula:
	//   pkg.level   = strings.Count(qname, "/")
	//   file.level  = parent_pkg.level + 1
	//   decl.level  = file.level + 1
	// For this fixture: p1="core" (0 slashes) -> 0, p2="core/types" (1) -> 1,
	// f1's nearest pkg is "core" (level 0) -> file level 1, fn1 -> level 2.
	// The L0/L1/L2/L3/L4 hierarchy in §5.4 is conceptual (root=pkg → ... →
	// LogicBlock); it does not equate 1:1 to numeric levels for shallow trees.
	if got := tree.LevelOf("p1"); got != 0 {
		t.Errorf("level of root pkg = %d, want 0", got)
	}
	if got := tree.LevelOf("p2"); got != 1 {
		t.Errorf("level of nested pkg = %d, want 1", got)
	}
	if got := tree.LevelOf("f1"); got != 1 {
		t.Errorf("level of file = %d, want 1", got)
	}
	if got := tree.LevelOf("fn1"); got != 2 {
		t.Errorf("level of function = %d, want 2", got)
	}
	parent, ok := tree.Parent("fn1")
	if !ok || parent != "f1" {
		t.Errorf("parent of fn1 = %q (ok=%v), want f1", parent, ok)
	}
}
