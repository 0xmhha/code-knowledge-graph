package solidity_test

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V10 — same-file free-function self-binding audit. A
// file declares its own free function `addOne` and a `using {
// addOne as * } for uint256 global;` directive in the same file.
// Resolution: W6 V3's NodeFunction fallback finds addOne in
// byName[NodeFunction]; pickSameFileCandidate prefers the same-
// file candidate (which IS the only candidate here). The
// EdgeUsesFor should land on Calc -> addOne in single_file.sol
// with ConfExtracted (no cross-file boundary).
func TestUsingForV6V10_SelfBindingSameFile(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v6v10", "single_file.sol")

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
	dst := byID[got[0].Dst]
	if dst.Name != "addOne" {
		t.Errorf("dst name: got %q want \"addOne\"", dst.Name)
	}
	if !strings.HasSuffix(dst.FilePath, "single_file.sol") {
		t.Errorf("dst file path: got %q, expected single_file.sol", dst.FilePath)
	}
	if got[0].Confidence != types.ConfExtracted {
		t.Errorf("confidence: got %v want EXTRACTED (same-file)", got[0].Confidence)
	}
}
