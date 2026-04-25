package eval

import "strings"

// PrecisionRecall returns precision and recall when comparing got and want
// as unordered string sets.
func PrecisionRecall(got, want []string) (precision, recall float64) {
	wantSet := map[string]struct{}{}
	for _, w := range want {
		wantSet[w] = struct{}{}
	}
	gotSet := map[string]struct{}{}
	for _, g := range got {
		gotSet[g] = struct{}{}
	}
	tp := 0
	for g := range gotSet {
		if _, ok := wantSet[g]; ok {
			tp++
		}
	}
	if len(gotSet) > 0 {
		precision = float64(tp) / float64(len(gotSet))
	}
	if len(wantSet) > 0 {
		recall = float64(tp) / float64(len(wantSet))
	}
	return
}

// RubricCheck performs naive case-insensitive substring matching of each
// rubric item's keywords against the output text. V0 is intentionally
// crude — manual review is expected for high-stakes scoring.
func RubricCheck(output string, rubric []string) (hits, total int) {
	low := strings.ToLower(output)
	for _, item := range rubric {
		total++
		// Match if at least 60% of the rubric item's words appear in output.
		words := strings.Fields(strings.ToLower(item))
		match := 0
		for _, w := range words {
			if len(w) < 4 {
				continue // skip stop-words
			}
			if strings.Contains(low, w) {
				match++
			}
		}
		if float64(match)/float64(max(1, len(words))) >= 0.6 {
			hits++
		}
	}
	return
}
