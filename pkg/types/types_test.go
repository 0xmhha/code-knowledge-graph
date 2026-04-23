package types_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func TestNodeTypeCount(t *testing.T) {
	if got, want := len(types.AllNodeTypes()), 29; got != want {
		t.Fatalf("AllNodeTypes count = %d, want %d", got, want)
	}
}

func TestEdgeTypeCount(t *testing.T) {
	if got, want := len(types.AllEdgeTypes()), 22; got != want {
		t.Fatalf("AllEdgeTypes count = %d, want %d", got, want)
	}
}

func TestConfidenceValid(t *testing.T) {
	for _, c := range []types.Confidence{types.ConfExtracted, types.ConfInferred, types.ConfAmbiguous} {
		if !c.Valid() {
			t.Errorf("Confidence(%q) should be valid", c)
		}
	}
	if types.Confidence("BOGUS").Valid() {
		t.Error("Confidence(BOGUS) should be invalid")
	}
}
