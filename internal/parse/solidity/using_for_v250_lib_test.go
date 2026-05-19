package solidity_test

import (
	"sort"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.5 — file-level operator-form using directive with
// library-method target. The V2.5 walker pattern-matches the
// braced shape at source_file scope (which V2.18's file-level
// recovery declined because identifier "global" precedes the
// type_name in named-child order, breaking source-order extraction)
// and emits a binding pair per (container, library) — same shape
// V2.20 produces at contract scope.
//
// Free-function form `using {mul as +}` is still grammar-recoverable
// by the same walker, but the emitted PendingRefs drop at resolveUsing
// ForRef (which looks up byName[NodeContract] and free functions are
// NodeFunction). TestUsingForV250_OperatorFormLimitation continues to
// lock that limitation; this test covers the library-method form
// where the resolution path completes.
func TestUsingForV250_FileLevelOperatorFormLibrary(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v250", "file_level_library_operator_form.sol")

	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	type uf struct{ src, dst string }
	var got []uf
	for _, e := range edges {
		if e.Type != types.EdgeUsesFor {
			continue
		}
		got = append(got, uf{
			src: byID[e.Src].Name,
			dst: byID[e.Dst].Name,
		})
	}
	sort.Slice(got, func(i, j int) bool {
		if got[i].src != got[j].src {
			return got[i].src < got[j].src
		}
		return got[i].dst < got[j].dst
	})

	// File-level `using {Math.add as +, Math.sub as -} for uint256
	// global;` fans out to every non-library container. Math itself
	// is a library so it is excluded from fan-out. User contract is
	// the only fan-out target. The two function entries collapse to
	// a single Math binding via dedup in parseFileLevelOperatorForm.
	want := []uf{
		{src: "User", dst: "Math"},
	}
	if len(got) != len(want) {
		t.Errorf("EdgeUsesFor count: got %d want %d (got=%v)", len(got), len(want), got)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("EdgeUsesFor[%d]: got %+v, want %+v", i, got[i], want[i])
		}
	}

	wantNodes := []string{
		"Math", "Math.add", "Math.sub",
		"User", "User.compute",
	}
	seen := map[string]bool{}
	for _, n := range nodes {
		seen[n.QualifiedName] = true
	}
	for _, qn := range wantNodes {
		if !seen[qn] {
			t.Errorf("surround-safety: %q not indexed", qn)
		}
	}
}
