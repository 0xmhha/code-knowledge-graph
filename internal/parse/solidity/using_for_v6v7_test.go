package solidity_test

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V7 — nested namespace path lookup. The directive
// `using {M.SubMath.addOne as +} for uint256 global;` has a
// 3-segment dotted path. resolveUsingBindingLeading takes the
// first segment after the namespace alias (`SubMath`) as the
// libName. The namespace path hint correctly routes the resolver
// to lib.sol's SubMath even though a homonym lives in
// lib_alt.sol.
//
// V7 honest scope: path hint disambiguation works; the trailing
// method name (`addOne`) is not yet carried on the binding edge
// — the EdgeUsesFor dst is the SubMath library itself. Method-
// name propagation is W6 V8+.
func TestUsingForV6V7_NestedNamespacePath(t *testing.T) {
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
		t.Fatalf("expected exactly one EdgeUsesFor, got %d (%v)", len(got), got)
	}
	dst := byID[got[0].Dst]
	if dst.Name != "SubMath" {
		t.Errorf("dst name: got %q want \"SubMath\"", dst.Name)
	}
	if !strings.HasSuffix(dst.FilePath, "lib.sol") {
		t.Errorf("expected dst from lib.sol, got file path %q", dst.FilePath)
	}
}
