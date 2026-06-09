package golang_test

import (
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

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
