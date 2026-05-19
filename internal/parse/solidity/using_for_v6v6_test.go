package solidity_test

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V6 — named-import alias path hint for homonym
// disambiguation.
//
// `import {origMul as mul} from "./math_alpha.sol";` creates a
// named-import alias `mul`. A `using {mul as *}` directive in the
// same file binds via the importAliases map (alias `mul` ->
// original `origMul`). V6 also records the source path
// "./math_alpha.sol" in importPaths so resolveUsingForRef can
// prefer the math_alpha.sol candidate over the math_beta.sol
// homonym.
//
// Expected: EdgeUsesFor.dst is the math_alpha.sol origMul.
func TestUsingForV6V6_NamedImportAliasHomonym(t *testing.T) {
	nodes, edges := parseResolveMultiSol(t, "testdata/using_for_v6v6",
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
	if dst.Name != "origMul" {
		t.Errorf("dst name: got %q want \"origMul\"", dst.Name)
	}
	if !strings.HasSuffix(dst.FilePath, "math_alpha.sol") {
		t.Errorf("expected dst from math_alpha.sol, got file path %q", dst.FilePath)
	}
}
