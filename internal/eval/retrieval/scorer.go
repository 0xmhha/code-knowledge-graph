package retrieval

// Score is the result of comparing got symbols against an expected set.
// Recall and Precision follow the textbook definitions:
//
//	Recall    = |expected ∩ got| / |expected|
//	Precision = |expected ∩ got| / |got|
//	F1        = harmonic mean of the two
//
// Edge cases (documented because retrieval probes routinely produce
// empty result sets):
//
//   - expected empty:        rejected at fixture load (validateFixture)
//   - got empty + nonempty expected:
//     Recall=0, Precision=0 (no TP, no FP), F1=0
//   - intersection empty:    same as above
//   - perfect match:         Recall=Precision=F1=1.0
type Score struct {
	Recall    float64 `json:"recall"`
	Precision float64 `json:"precision"`
	F1        float64 `json:"f1"`

	// Diagnostic — the set differences, useful when a fixture fails.
	// Missing = expected \ got  (what the probe failed to return)
	// Extra   = got \ expected  (what the probe over-returned)
	Missing []string `json:"missing,omitempty"`
	Extra   []string `json:"extra,omitempty"`
}

// ComputeScore returns recall/precision/F1 plus the symbol-level set
// differences. Both inputs are deduped internally — duplicate IDs in
// expected or got do not skew the ratio.
func ComputeScore(expected, got []string) Score {
	expSet := toSet(expected)
	gotSet := toSet(got)

	intersect := 0
	for sym := range expSet {
		if gotSet[sym] {
			intersect++
		}
	}

	var s Score
	if len(expSet) > 0 {
		s.Recall = float64(intersect) / float64(len(expSet))
	}
	if len(gotSet) > 0 {
		s.Precision = float64(intersect) / float64(len(gotSet))
	}
	if s.Recall+s.Precision > 0 {
		s.F1 = 2 * s.Recall * s.Precision / (s.Recall + s.Precision)
	}

	for sym := range expSet {
		if !gotSet[sym] {
			s.Missing = append(s.Missing, sym)
		}
	}
	for sym := range gotSet {
		if !expSet[sym] {
			s.Extra = append(s.Extra, sym)
		}
	}
	// Sort for deterministic output across runs (map iteration is
	// random in Go; without sorting, the JSON baseline would churn
	// even when the math is identical).
	sortStrings(s.Missing)
	sortStrings(s.Extra)
	return s
}

func toSet(xs []string) map[string]bool {
	out := make(map[string]bool, len(xs))
	for _, x := range xs {
		out[x] = true
	}
	return out
}

func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}
