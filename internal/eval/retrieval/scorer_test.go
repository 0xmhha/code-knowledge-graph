package retrieval

import "testing"

// TestComputeScore_PerfectMatch — when got matches expected exactly,
// every metric hits 1.0 and the diagnostic sets are empty. This is
// the "shouldn't regress" baseline behaviour the gate is built on.
func TestComputeScore_PerfectMatch(t *testing.T) {
	expected := []string{"a", "b", "c"}
	got := []string{"a", "b", "c"}
	s := ComputeScore(expected, got)
	if s.Recall != 1.0 || s.Precision != 1.0 || s.F1 != 1.0 {
		t.Errorf("perfect match: got R=%v P=%v F1=%v, want all 1.0", s.Recall, s.Precision, s.F1)
	}
	if len(s.Missing) != 0 || len(s.Extra) != 0 {
		t.Errorf("perfect match: Missing=%v Extra=%v, want empty", s.Missing, s.Extra)
	}
}

// TestComputeScore_PartialMatch checks the typical regression shape:
// most expected hits are returned but one is missing, plus the result
// set carries one extra noise hit. Both diagnostic lists are populated
// so a failing fixture surfaces exactly which symbols moved.
func TestComputeScore_PartialMatch(t *testing.T) {
	expected := []string{"a", "b", "c", "d"}
	got := []string{"a", "b", "c", "z"} // missing d, extra z
	s := ComputeScore(expected, got)

	if s.Recall != 0.75 {
		t.Errorf("recall = %v, want 0.75 (3 of 4 expected)", s.Recall)
	}
	if s.Precision != 0.75 {
		t.Errorf("precision = %v, want 0.75 (3 of 4 got)", s.Precision)
	}
	if s.F1 != 0.75 {
		t.Errorf("f1 = %v, want 0.75 (R=P case)", s.F1)
	}
	if len(s.Missing) != 1 || s.Missing[0] != "d" {
		t.Errorf("Missing = %v, want [d]", s.Missing)
	}
	if len(s.Extra) != 1 || s.Extra[0] != "z" {
		t.Errorf("Extra = %v, want [z]", s.Extra)
	}
}

// TestComputeScore_EmptyGot — probe returned nothing. Recall=0
// (everything was missed) and precision is conventionally 0 here
// because there's no intersection to divide by anyway. F1 must be 0
// rather than NaN — downstream JSON / threshold gates can't handle NaN.
func TestComputeScore_EmptyGot(t *testing.T) {
	s := ComputeScore([]string{"a", "b"}, []string{})
	if s.Recall != 0.0 {
		t.Errorf("empty got: recall = %v, want 0.0", s.Recall)
	}
	if s.Precision != 0.0 {
		t.Errorf("empty got: precision = %v, want 0.0", s.Precision)
	}
	if s.F1 != 0.0 {
		t.Errorf("empty got: F1 = %v, want 0.0 (must not be NaN)", s.F1)
	}
	if len(s.Missing) != 2 {
		t.Errorf("empty got: Missing length = %d, want 2 (everything missed)", len(s.Missing))
	}
}

// TestComputeScore_Dedupe — duplicate IDs on either side must not
// inflate the ratios. This matters because real probe outputs can
// re-list a node visited via multiple paths in BFS.
func TestComputeScore_Dedupe(t *testing.T) {
	expected := []string{"a", "a", "b"}
	got := []string{"a", "b", "b"}
	s := ComputeScore(expected, got)
	if s.Recall != 1.0 || s.Precision != 1.0 {
		t.Errorf("dedupe: got R=%v P=%v, want 1.0/1.0 ({a,b} == {a,b})", s.Recall, s.Precision)
	}
}

// TestComputeScore_DeterministicOrdering pins the Missing / Extra
// list ordering. Go's map iteration is random; without explicit sort
// the JSON baseline would churn even when the math is identical, so
// the regression gate would fail-noise.
func TestComputeScore_DeterministicOrdering(t *testing.T) {
	expected := []string{"c", "a", "b"}
	got := []string{"x", "y"}
	s := ComputeScore(expected, got)

	if !isSorted(s.Missing) {
		t.Errorf("Missing not sorted: %v", s.Missing)
	}
	if !isSorted(s.Extra) {
		t.Errorf("Extra not sorted: %v", s.Extra)
	}
}

func isSorted(xs []string) bool {
	for i := 1; i < len(xs); i++ {
		if xs[i-1] > xs[i] {
			return false
		}
	}
	return true
}
