package persist

import "testing"

// TestRewriteFTSQuery_TrailingPunctuation locks B1 fix (2026-05-11 stablenet
// VERIFICATION_REPORT §3.1): natural-language task descriptions ending in
// `.` or other punctuation would propagate the punctuation into the
// generated FTS5 prefix expression (`validated.*`), which FTS5 rejects
// with `syntax error near "."`. trimFTSToken now strips trailing/leading
// non-alnum characters before the `*` suffix is appended.
func TestRewriteFTSQuery_TrailingPunctuation(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// Single-token branch.
		{"trailing period single", "validated.", "validated*"},
		{"trailing comma single", "consensus,", "consensus*"},
		{"leading period single", ".consensus", "consensus*"},

		// Multi-token branch — each token sanitised independently, joined with " OR ".
		{"natural sentence with period",
			"How does block validation work in consensus.",
			"How OR does* OR block* OR validation* OR work* OR consensus*"},
		{"trailing semicolon multi",
			"WBFT prepare quorum;",
			"WBFT* OR prepare* OR quorum*"},

		// Identifier-internal punctuation is intentionally left alone — these
		// rely on the caller's sigil-escape path (early-return in rewriteFTSQuery
		// when the input contains *"():). The TrimFunc only touches boundary
		// characters.
		{"identifier with dot stays",
			"validate function",
			"validate* OR function*"},

		// Single-char tokens are dropped (existing semantics retained).
		{"short tokens dropped",
			"a b validated.",
			"validated*"},

		// All-punctuation input — falls back to the raw query (no useful tokens).
		{"all punctuation", ".,;", ".,;"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := rewriteFTSQuery(tc.in)
			if got != tc.want {
				t.Errorf("rewriteFTSQuery(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestTrimFTSToken locks the trimming primitive itself: leading/trailing
// non-alnum (plus optional `_`) is stripped; identifier-internal chars
// are preserved. Pure-punctuation tokens collapse to "".
func TestTrimFTSToken(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"validated.", "validated"},
		{".validated", "validated"},
		{"validated", "validated"},
		{"foo_bar", "foo_bar"},
		{"foo_bar.", "foo_bar"},
		{"...", ""},
		{"", ""},
		{"a", "a"},
		{"FOO123", "FOO123"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := trimFTSToken(tc.in)
			if got != tc.want {
				t.Errorf("trimFTSToken(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
