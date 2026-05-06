package golang_test

import (
	"strings"
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestImplements_ValueAndPointerReceivers asserts that the implements pass
// detects satisfaction via BOTH value-receiver and pointer-receiver methods.
// The fixture has:
//   - Hello implements Greeter (value receiver) — must produce an edge
//   - World implements Greeter (pointer receiver only) — must produce an edge
//   - Doer does NOT implement Greeter — must NOT produce an edge
func TestImplements_ValueAndPointerReceivers(t *testing.T) {
	root := "testdata/implements"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	nodeByID := make(map[string]*types.Node, len(g.Nodes))
	for i := range g.Nodes {
		nodeByID[g.Nodes[i].ID] = &g.Nodes[i]
	}

	type pair struct{ implName, ifaceName string }
	have := map[pair]bool{}
	implEdges := edgesByType(g.Edges, types.EdgeImplements)
	for _, e := range implEdges {
		src := nodeByID[e.Src]
		dst := nodeByID[e.Dst]
		if src == nil || dst == nil {
			t.Errorf("dangling implements edge: src=%q dst=%q", e.Src, e.Dst)
			continue
		}
		have[pair{src.Name, dst.Name}] = true
	}

	if !have[pair{"Hello", "Greeter"}] {
		t.Errorf("missing implements edge Hello -> Greeter (value-receiver satisfaction)")
	}
	if !have[pair{"World", "Greeter"}] {
		t.Errorf("missing implements edge World -> Greeter (pointer-receiver satisfaction)")
	}
	if have[pair{"Doer", "Greeter"}] {
		t.Errorf("unexpected implements edge Doer -> Greeter (Doer has no Greet method)")
	}
}

// TestImplements_SrcTypeIsConcrete asserts every implements edge's Src is
// a Struct/TypeAlias/Enum (i.e. NOT an Interface). Interface→interface
// relationships are modeled as extends, not implements.
func TestImplements_SrcTypeIsConcrete(t *testing.T) {
	root := "testdata/implements"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	nodeByID := make(map[string]*types.Node, len(g.Nodes))
	for i := range g.Nodes {
		nodeByID[g.Nodes[i].ID] = &g.Nodes[i]
	}
	for _, e := range edgesByType(g.Edges, types.EdgeImplements) {
		src := nodeByID[e.Src]
		if src == nil {
			t.Errorf("dangling implements src: %q", e.Src)
			continue
		}
		if src.Type == types.NodeInterface {
			t.Errorf("implements edge src is Interface (%q) — should be extends, not implements",
				src.QualifiedName)
		}
		switch src.Type {
		case types.NodeStruct, types.NodeTypeAlias, types.NodeEnum:
			// ok
		default:
			t.Errorf("implements edge src is %s (%q) — want Struct/TypeAlias/Enum",
				src.Type, src.QualifiedName)
		}
	}
}

// TestImplements_DstIsInterface asserts every implements edge's Dst is an
// Interface node. The schema validator enforces this with the
// "implements-bad-dst" rule; this test pins the parser-side guarantee.
func TestImplements_DstIsInterface(t *testing.T) {
	root := "testdata/implements"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	nodeByID := make(map[string]*types.Node, len(g.Nodes))
	for i := range g.Nodes {
		nodeByID[g.Nodes[i].ID] = &g.Nodes[i]
	}
	for _, e := range edgesByType(g.Edges, types.EdgeImplements) {
		dst := nodeByID[e.Dst]
		if dst == nil {
			t.Errorf("dangling implements dst: %q", e.Dst)
			continue
		}
		if dst.Type != types.NodeInterface {
			t.Errorf("implements edge dst is %s (%q) — want Interface",
				dst.Type, dst.QualifiedName)
		}
	}
}

// TestImplements_NoSelfEdges asserts no edge has Src == Dst — both
// implements and extends should skip self-relationships.
func TestImplements_NoSelfEdges(t *testing.T) {
	root := "testdata/implements"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	for _, e := range g.Edges {
		if e.Type != types.EdgeImplements && e.Type != types.EdgeExtends {
			continue
		}
		if e.Src == e.Dst {
			t.Errorf("self-edge %s: src==dst==%q", e.Type, e.Src)
		}
	}
}

// TestImplements_ExtendsForEmbeddedInterface asserts that embedding one
// interface inside another produces an extends edge (not an implements
// edge). Closer embeds Goodbye in the fixture.
func TestImplements_ExtendsForEmbeddedInterface(t *testing.T) {
	root := "testdata/implements"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	nodeByID := make(map[string]*types.Node, len(g.Nodes))
	for i := range g.Nodes {
		nodeByID[g.Nodes[i].ID] = &g.Nodes[i]
	}
	var found bool
	for _, e := range edgesByType(g.Edges, types.EdgeExtends) {
		src := nodeByID[e.Src]
		dst := nodeByID[e.Dst]
		if src == nil || dst == nil {
			continue
		}
		if src.Name == "Closer" && dst.Name == "Goodbye" {
			if src.Type != types.NodeInterface || dst.Type != types.NodeInterface {
				t.Errorf("extends Closer->Goodbye types: src=%s dst=%s, want Interface->Interface",
					src.Type, dst.Type)
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("missing extends edge Closer -> Goodbye")
	}

	// Counter-check: NO implements edge should connect Closer → Goodbye.
	for _, e := range edgesByType(g.Edges, types.EdgeImplements) {
		src := nodeByID[e.Src]
		dst := nodeByID[e.Dst]
		if src == nil || dst == nil {
			continue
		}
		if src.Name == "Closer" && dst.Name == "Goodbye" {
			t.Errorf("Closer->Goodbye should be extends, got implements edge")
		}
	}
}

// TestImplements_CrossPackageSatisfaction asserts the implements pass emits
// an edge when the concrete type and interface live in different packages.
// This is the production case (sqliteStore in internal/persist implements
// persist.Store / pkg/types.StoreReader across files and would-be packages).
// Without it, the most common real-world satisfaction relationship — interface
// in pkg A, struct in pkg B — silently produces zero edges.
func TestImplements_CrossPackageSatisfaction(t *testing.T) {
	root := "testdata/implements_xpkg"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	nodeByID := make(map[string]*types.Node, len(g.Nodes))
	for i := range g.Nodes {
		nodeByID[g.Nodes[i].ID] = &g.Nodes[i]
	}

	type pair struct{ implQ, ifaceQ string }
	have := map[pair]bool{}
	for _, e := range edgesByType(g.Edges, types.EdgeImplements) {
		src := nodeByID[e.Src]
		dst := nodeByID[e.Dst]
		if src == nil || dst == nil {
			t.Errorf("dangling implements edge: src=%q dst=%q", e.Src, e.Dst)
			continue
		}
		have[pair{src.QualifiedName, dst.QualifiedName}] = true
	}
	if !have[pair{"impl.MemStore", "defs.Store"}] {
		t.Errorf("missing cross-package implements edge impl.MemStore -> defs.Store; got %+v", have)
	}
}

// TestImplements_NoEmptyInterfaceNoise asserts that the empty interface
// (Anything) does not collect implements edges. Every type satisfies it,
// so emitting them would be O(N) low-signal noise.
func TestImplements_NoEmptyInterfaceNoise(t *testing.T) {
	root := "testdata/implements"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	nodeByID := make(map[string]*types.Node, len(g.Nodes))
	for i := range g.Nodes {
		nodeByID[g.Nodes[i].ID] = &g.Nodes[i]
	}
	for _, e := range edgesByType(g.Edges, types.EdgeImplements) {
		dst := nodeByID[e.Dst]
		if dst == nil {
			continue
		}
		if strings.HasSuffix(dst.QualifiedName, ".Anything") {
			t.Errorf("implements edge into empty interface Anything: %+v", e)
		}
	}
}
