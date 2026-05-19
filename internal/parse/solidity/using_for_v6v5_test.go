package solidity_test

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V5 — homonym disambiguation for free-function using-for.
//
// Two files declare a `mul` free function each. The consumer file
// imports math_alpha.sol under the namespace alias `M` and binds
// `M.mul` as the `*` operator for uint256. Pre-V5 the resolver
// looked up the first matching `mul` candidate via
// pickSameFileCandidate; with two cross-file candidates and no
// same-file match, it picked an arbitrary one. V5 records the
// namespace alias source path during runImportAliases and the file-
// level operator-form walker attaches the path as a "||<path>"
// hint on the binding PendingRef. resolveUsingForRef now prefers
// candidates whose file path matches the hint before falling back
// to the same-file heuristic.
//
// Expected: the EdgeUsesFor dst points at math_alpha.sol's mul,
// not math_beta.sol's. Locking this prevents a silent regression
// where the resolver picks the wrong homonym.
func TestUsingForV6V5_HomonymDisambiguation(t *testing.T) {
	nodes, edges := parseResolveMultiSol(t, "testdata/using_for_v6v5",
		[]string{"math_alpha.sol", "math_beta.sol", "consumer.sol"})

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
	if dst.Name != "mul" {
		t.Errorf("dst name: got %q want \"mul\"", dst.Name)
	}
	if !strings.HasSuffix(dst.FilePath, "math_alpha.sol") {
		t.Errorf("expected dst from math_alpha.sol, got file path %q", dst.FilePath)
	}
}
