package eval_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/eval"
)

func TestPrecisionRecall(t *testing.T) {
	want := []string{"a", "b", "c"}
	got := []string{"a", "b", "x"}
	p, r := eval.PrecisionRecall(got, want)
	if p < 0.66 || p > 0.67 || r < 0.66 || r > 0.67 {
		t.Errorf("p=%.2f r=%.2f", p, r)
	}
}

func TestRubricMatchesItems(t *testing.T) {
	rubric := []string{
		"uses Snapshot mutex correctly",
		"validates input addr",
	}
	output := "We acquire the Snapshot.lock and then validate input addr before mutating."
	hits, total := eval.RubricCheck(output, rubric)
	// Item 2 matches: "input" + "addr" = 2/3 = 67% >= 60% threshold; item 1 only matches "snapshot" = 1/4 = 25%.
	if hits != 1 || total != 2 {
		t.Errorf("hits=%d/%d", hits, total)
	}
}
