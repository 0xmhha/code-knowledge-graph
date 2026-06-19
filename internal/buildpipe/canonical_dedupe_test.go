package buildpipe

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestLineQualifyDuplicateCanonicalIDs guards B3: canonical_ids shared by more
// than one node in the same file get a "@<line>" suffix; unique ids are left
// stable and line-independent.
func TestLineQualifyDuplicateCanonicalIDs(t *testing.T) {
	nodes := []types.Node{
		{CanonicalID: "app.min.js:t", StartLine: 10},
		{CanonicalID: "app.min.js:t", StartLine: 42},
		{CanonicalID: "app.min.js:t", StartLine: 99},
		{CanonicalID: "app.min.js:unique", StartLine: 5},
		{CanonicalID: "", StartLine: 7}, // empty id untouched
	}
	lineQualifyDuplicateCanonicalIDs(nodes)

	got := []string{nodes[0].CanonicalID, nodes[1].CanonicalID, nodes[2].CanonicalID}
	want := []string{"app.min.js:t@10", "app.min.js:t@42", "app.min.js:t@99"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dup[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if nodes[3].CanonicalID != "app.min.js:unique" {
		t.Errorf("unique id changed to %q, want app.min.js:unique (stable)", nodes[3].CanonicalID)
	}
	if nodes[4].CanonicalID != "" {
		t.Errorf("empty id became %q, want empty", nodes[4].CanonicalID)
	}
}
