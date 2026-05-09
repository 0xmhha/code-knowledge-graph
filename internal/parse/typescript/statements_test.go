package typescript_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestTSStatements_AllFiveKinds covers each statement kind the runBody-
// Statements pass emits — mirroring the Go parser's statements.go. One
// sample function exercises every branch so a future schema bump is
// caught loudly even before the cmd/ckg integration tests run.
func TestTSStatements_AllFiveKinds(t *testing.T) {
	src := []byte(`
function exercise(n: number): number {
  if (n > 10) {
    return -1;
  }
  for (let i = 0; i < n; i++) {
    console.log(i);
  }
  for (const x of [1, 2, 3]) {
    console.log(x);
  }
  while (n > 0) {
    n--;
  }
  do {
    n++;
  } while (n < 5);
  switch (n) {
    case 1:
      return 1;
    default:
      return 0;
  }
}
`)
	p := tsp.New(".")
	r, err := p.ParseFile(filepath.Join(".", "stmts.ts"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	counts := countByType(r.Nodes)
	// 1 IfStmt, 4 LoopStmt (for / for-of / while / do), 1 SwitchStmt,
	// 4 ReturnStmt (-1, 1, 0 + the one trailing the switch),
	// 2 CallSite (console.log invocations).
	wantMin := map[types.NodeType]int{
		types.NodeIfStmt:     1,
		types.NodeLoopStmt:   4,
		types.NodeSwitchStmt: 1,
		types.NodeReturnStmt: 3,
		types.NodeCallSite:   2,
	}
	for nt, min := range wantMin {
		if counts[nt] < min {
			t.Errorf("expected ≥%d %s nodes, got %d", min, nt, counts[nt])
		}
	}

	// Each statement node has a `contains` edge from a Function/Method.
	// Build (statement-id → in-degree-of-contains) and check >= 1.
	stmtIDs := map[string]bool{}
	for _, n := range r.Nodes {
		switch n.Type {
		case types.NodeIfStmt, types.NodeLoopStmt,
			types.NodeSwitchStmt, types.NodeReturnStmt, types.NodeCallSite:
			stmtIDs[n.ID] = true
		}
	}
	containedFrom := map[string]string{}
	for _, e := range r.Edges {
		if e.Type == types.EdgeContains && stmtIDs[e.Dst] {
			containedFrom[e.Dst] = e.Src
		}
	}
	for id := range stmtIDs {
		if containedFrom[id] == "" {
			t.Errorf("statement node %s missing `contains` edge from enclosing fn", id)
		}
	}
}

// TestTSStatements_LoopSubKindsDistinct verifies the LoopStmt SubKind
// disambiguates the four loop shapes — useful for downstream queries
// that want to ask "show me all `for-of` iterations" specifically.
func TestTSStatements_LoopSubKindsDistinct(t *testing.T) {
	src := []byte(`
function loops(arr: number[], obj: any): void {
  for (let i = 0; i < arr.length; i++) {}
  for (const x of arr) {}
  for (const k in obj) {}
  let n = 0;
  while (n < 10) { n++; }
  do { n--; } while (n > 0);
}
`)
	p := tsp.New(".")
	r, err := p.ParseFile(filepath.Join(".", "loops.ts"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	subKinds := map[string]int{}
	for _, n := range r.Nodes {
		if n.Type == types.NodeLoopStmt {
			subKinds[n.SubKind]++
		}
	}
	for _, want := range []string{"for", "for-of", "for-in", "while", "do"} {
		if subKinds[want] != 1 {
			t.Errorf("expected exactly 1 LoopStmt with SubKind=%q, got %d (full map: %v)",
				want, subKinds[want], subKinds)
		}
	}
}

// TestTSStatements_CallSiteAnchorsPendingRef cements the schema change:
// the pending ref source is now a CallSite ID, not the enclosing
// function ID. The CallSite is in turn `contains`-edged from the
// enclosing function, so cross-language consumers can still trace
// "which function fired this call" with one extra hop.
func TestTSStatements_CallSiteAnchorsPendingRef(t *testing.T) {
	src := []byte(`
function helper() { return 1; }
function caller() {
  helper();
}
`)
	p := tsp.New(".")
	r, err := p.ParseFile(filepath.Join(".", "anchor.ts"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(r.Pending) != 1 {
		t.Fatalf("expected 1 PendingRef, got %d", len(r.Pending))
	}
	pr := r.Pending[0]
	// Find the node by SrcID — it must be a CallSite.
	var srcNode *types.Node
	for i := range r.Nodes {
		if r.Nodes[i].ID == pr.SrcID {
			srcNode = &r.Nodes[i]
			break
		}
	}
	if srcNode == nil {
		t.Fatalf("PendingRef.SrcID %s does not match any emitted node", pr.SrcID)
	}
	if srcNode.Type != types.NodeCallSite {
		t.Errorf("expected CallSite as Pending.SrcID, got %s", srcNode.Type)
	}
	// And the CallSite must be contained-by a Function.
	var containerType types.NodeType
	for _, e := range r.Edges {
		if e.Type == types.EdgeContains && e.Dst == pr.SrcID {
			for _, n := range r.Nodes {
				if n.ID == e.Src {
					containerType = n.Type
					break
				}
			}
			break
		}
	}
	if containerType != types.NodeFunction && containerType != types.NodeMethod {
		t.Errorf("CallSite container should be Function/Method, got %s", containerType)
	}
}

func countByType(nodes []types.Node) map[types.NodeType]int {
	out := map[types.NodeType]int{}
	for _, n := range nodes {
		out[n.Type]++
	}
	return out
}

// TestTSStatements_NoEmissionAtTopLevel — statement nodes are emitted
// only INSIDE function/method bodies. A top-level `if (...)` or
// `for (...)` at module scope does not get a node, matching the Go
// parser's behaviour (Go has no top-level statements; TS does, but
// surfacing them would require an alternative anchor).
func TestTSStatements_NoEmissionAtTopLevel(t *testing.T) {
	src := []byte(`
const flag = true;
if (flag) {
  console.log("module-level if");
}
for (let i = 0; i < 3; i++) {
  console.log(i);
}
`)
	p := tsp.New(".")
	r, err := p.ParseFile(filepath.Join(".", "top.ts"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, n := range r.Nodes {
		switch n.Type {
		case types.NodeIfStmt, types.NodeLoopStmt,
			types.NodeSwitchStmt, types.NodeReturnStmt, types.NodeCallSite:
			t.Errorf("expected no statement nodes at module top level, got %s (%s)",
				n.Type, n.QualifiedName)
		}
	}
	// And no Pending refs either — top-level calls have no enclosing fn.
	if len(r.Pending) != 0 {
		t.Errorf("expected 0 PendingRefs at top level, got %d", len(r.Pending))
	}
	// Sanity: parse didn't error out.
	if len(r.Nodes) == 0 {
		t.Errorf("expected at least the File node, got 0")
	}
	_ = parse.PendingRef{} // import keep
}
