package solidity_test

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V9 — combined path-hint + method-hint audit. The
// directive `using {M.SubMath.addOne as +} for uint256 global;`
// imported under `import "./lib.sol" as M` triggers BOTH the
// V5/V6 path hint ("||<path>") and the V8 method hint
// ("\x1e<method>") on the same PendingRef.TargetQName. V9
// audits that resolveUsingForRef decodes both hints
// independently and emits the expected EdgeUsesFor:
//
//   - dst points at lib.sol's SubMath library (path hint
//     selected over the lib_alt.sol homonym).
//   - Edge.DispatchKind carries the method name as
//     `using_for|addOne` (method hint surfaced on the edge).
//
// Re-uses the W6 V7 fixture (same source files exercise both
// hint codepaths) — this test asserts the second axis.
func TestUsingForV6V9_CombinedHints(t *testing.T) {
	nodes, edges := parseResolveMultiSol(t, "testdata/using_for_v6v7",
		[]string{"lib.sol", "lib_alt.sol", "consumer.sol"})

	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	var got []types.Edge
	for _, e := range edges {
		if e.Type == types.EdgeUsesFor {
			got = append(got, e)
		}
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly one EdgeUsesFor, got %d", len(got))
	}
	e := got[0]
	dst := byID[e.Dst]

	// Path hint axis (V5/V6/V7 contract).
	if !strings.HasSuffix(dst.FilePath, "lib.sol") {
		t.Errorf("path hint axis: expected dst from lib.sol, got %q", dst.FilePath)
	}
	if dst.Name != "SubMath" {
		t.Errorf("path hint axis: dst name %q want \"SubMath\"", dst.Name)
	}

	// Method hint axis (V8 contract).
	if !strings.HasPrefix(e.DispatchKind, "using_for") {
		t.Errorf("DispatchKind: missing using_for prefix: %q", e.DispatchKind)
	}
	idx := strings.Index(e.DispatchKind, "|")
	if idx < 0 {
		t.Errorf("DispatchKind missing method suffix: %q", e.DispatchKind)
	} else if method := e.DispatchKind[idx+1:]; method != "addOne" {
		t.Errorf("method hint axis: got %q, want %q", method, "addOne")
	}
}
