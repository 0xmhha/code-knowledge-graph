package golang_test

import (
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestCanonicalID_DistinguishesSameNameAcrossPackages guards Phase 1 of the
// symbol-identity design: the same method name in two different packages must
// get distinct, import-path-qualified canonical ids, even though their short
// qualified_name leaves (coll1.Set.Size / coll2.Other.Size) only differ by the
// leaf package — and leaf packages themselves collide on real codebases.
func TestCanonicalID_DistinguishesSameNameAcrossPackages(t *testing.T) {
	g, err := gop.LoadAndResolve("testdata/resolve")
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	var c1, c2 string
	for _, n := range g.Nodes {
		switch n.QualifiedName {
		case "coll1.Set.Size":
			c1 = n.CanonicalID
		case "coll2.Other.Size":
			c2 = n.CanonicalID
		}
	}
	if c1 == "" || c2 == "" {
		t.Fatalf("missing canonical ids: coll1.Set.Size=%q coll2.Other.Size=%q", c1, c2)
	}
	if c1 == c2 {
		t.Errorf("same-name methods in different packages share canonical id %q", c1)
	}
	if want := "ckgresolve.test/coll1.(*Set).Size"; c1 != want {
		t.Errorf("coll1.Set.Size canonical id = %q, want %q", c1, want)
	}
}

// TestResolveSameNameMethodPrefersReceiverType guards the name-collision fix:
// coll1.Set.Quorum calls its own receiver's Size(), while coll2.Other.Size is a
// same-named decoy in another package. The typed resolver must bind the call to
// coll1.Set.Size and never to coll2.Other.Size (the V0 bare-name resolver bound
// such calls to whichever same-named node was indexed last).
func TestResolveSameNameMethodPrefersReceiverType(t *testing.T) {
	g, err := gop.LoadAndResolve("testdata/resolve")
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	var srcID, wantDst, decoyDst string
	for _, n := range g.Nodes {
		switch n.QualifiedName {
		case "coll1.Set.Quorum":
			srcID = n.ID
		case "coll1.Set.Size":
			wantDst = n.ID
		case "coll2.Other.Size":
			decoyDst = n.ID
		}
	}
	if srcID == "" || wantDst == "" || decoyDst == "" {
		t.Fatalf("missing nodes: src=%q want=%q decoy=%q", srcID, wantDst, decoyDst)
	}
	var toWant, toDecoy bool
	for _, e := range g.Edges {
		if e.Src != srcID || (e.Type != types.EdgeCalls && e.Type != types.EdgeInvokes) {
			continue
		}
		switch e.Dst {
		case wantDst:
			toWant = true
		case decoyDst:
			toDecoy = true
		}
	}
	if !toWant {
		t.Errorf("expected coll1.Set.Quorum -calls-> coll1.Set.Size (same receiver type)")
	}
	if toDecoy {
		t.Errorf("coll1.Set.Quorum must NOT bind to coll2.Other.Size (bare-name collision)")
	}
}

// TestResolveInterfaceMethodNotBareName guards defect-A fix #1: an interface
// dispatch call (h.Hash() where h is the Hasher interface) must bind to
// coll1.Hasher.Hash, never to the same-named decoy coll1.Thing.Hash. The bare
// "interface_method" path used to keep the bare callee name, which the V0
// resolver bound to whichever ".Hash" node was indexed last.
func TestResolveInterfaceMethodNotBareName(t *testing.T) {
	g, err := gop.LoadAndResolve("testdata/resolve")
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	var srcID, wantDst, decoyDst string
	for _, n := range g.Nodes {
		switch n.QualifiedName {
		case "coll1.UseHasher":
			srcID = n.ID
		case "coll1.Hasher.Hash":
			wantDst = n.ID
		case "coll1.Thing.Hash":
			decoyDst = n.ID
		}
	}
	if srcID == "" || wantDst == "" || decoyDst == "" {
		t.Fatalf("missing nodes: src=%q want=%q decoy=%q", srcID, wantDst, decoyDst)
	}
	var toWant, toDecoy bool
	for _, e := range g.Edges {
		if e.Src != srcID || (e.Type != types.EdgeCalls && e.Type != types.EdgeInvokes) {
			continue
		}
		switch e.Dst {
		case wantDst:
			toWant = true
		case decoyDst:
			toDecoy = true
		}
	}
	if !toWant {
		t.Errorf("expected coll1.UseHasher -invokes-> coll1.Hasher.Hash (interface method)")
	}
	if toDecoy {
		t.Errorf("coll1.UseHasher must NOT bind to coll1.Thing.Hash (bare-name collision)")
	}
}

// TestResolveBuiltinEmitsNoCallEdge guards defect-A fix #2: a builtin call
// (len(xs)) must not produce a call edge to a same-named method node
// (coll1.counter.len). Builtins have no graph node, so guessing one is a
// false edge — the kind of cross-subsystem noise that pollutes find_callees.
func TestResolveBuiltinEmitsNoCallEdge(t *testing.T) {
	g, err := gop.LoadAndResolve("testdata/resolve")
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	var srcID, builtinDecoy string
	for _, n := range g.Nodes {
		switch n.QualifiedName {
		case "coll1.CountBuiltin":
			srcID = n.ID
		case "coll1.counter.len":
			builtinDecoy = n.ID
		}
	}
	if srcID == "" || builtinDecoy == "" {
		t.Fatalf("missing nodes: src=%q decoy=%q", srcID, builtinDecoy)
	}
	for _, e := range g.Edges {
		if e.Src == srcID && e.Dst == builtinDecoy &&
			(e.Type == types.EdgeCalls || e.Type == types.EdgeInvokes) {
			t.Errorf("builtin len() must NOT bind to coll1.counter.len")
		}
	}
}

func TestResolveCrossFileCall(t *testing.T) {
	root := "testdata/resolve"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	var srcID, dstID string
	for _, n := range g.Nodes {
		if n.QualifiedName == "b.Hello" {
			srcID = n.ID
		}
		if n.QualifiedName == "a.Greet" {
			dstID = n.ID
		}
	}
	if srcID == "" || dstID == "" {
		t.Fatalf("missing nodes: srcID=%q dstID=%q", srcID, dstID)
	}
	found := false
	for _, e := range g.Edges {
		if e.Type == types.EdgeCalls && e.Src == srcID && e.Dst == dstID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected edge b.Hello -calls-> a.Greet")
	}
}
