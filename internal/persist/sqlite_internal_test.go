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
			"does* OR block* OR validation* OR work* OR consensus*"},
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

// TestRewriteFTSQuery_PowerUserGateNarrowed locks the second
// smartContext-audit fix (2026-05-22 iteration 2): a task
// description containing natural-language quotes around an
// example identifier (`Include names like "service.Vault.Deposit"`)
// used to flip the power-user gate via the loose
// `ContainsAny(q, "*\"")` check, route the whole query straight
// to FTS5, and produce `syntax error near "."` again — defeating
// the iteration-1 dotted-identifier fix.
//
// The narrowed gate now requires either an explicit `*` or a
// query that is *entirely* phrase-quoted (`"foo bar"`); prose
// with embedded quotes still flows through the rewriter and the
// quote becomes a separator alongside `.`.
func TestRewriteFTSQuery_PowerUserGateNarrowed(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "embedded quote in prose goes through rewriter",
			in:   `find names like "Vault.deposit" or core.NewBlockChain`,
			// `like` is 4 chars so it takes the wildcard tail;
			// `or` is 2 chars so it drops via the stop-word filter.
			want: "find* OR names* OR like* OR Vault* OR deposit* OR core* OR NewBlockChain*",
		},
		{
			name: "wildcard still passes through",
			in:   "Vault.deposit*",
			want: "Vault.deposit*",
		},
		{
			name: "fully phrase-quoted query still passes through",
			in:   `"Vault.deposit phrase"`,
			want: `"Vault.deposit phrase"`,
		},
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

// TestRewriteFTSQuery_DottedIdentifierSplit locks the smartContext
// audit fix (2026-05-22): a task description like "List functions
// that call Vault.deposit" used to tokenise `Vault.deposit` as a
// single ≥4-char field, append `*`, and produce `Vault.deposit*`
// which FTS5 rejects with "syntax error near \".\"". The rewriter
// now splits on `.` in addition to whitespace, so each segment
// becomes its own prefix-matched token.
//
// Symptom that motivated this: δ baseline ran with no smartContext
// context for a full smoke run because store.Search bubbled the
// FTS error up to BuildContext, which the runner silently swallowed.
func TestRewriteFTSQuery_DottedIdentifierSplit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single dotted identifier splits",
			in:   "Vault.deposit",
			want: "Vault* OR deposit*",
		},
		{
			name: "task description with dotted identifier",
			in:   "find callers of Vault.deposit in the synthetic corpus",
			// `the` survives as a 3-char token under the existing
			// drop<3 stop-word heuristic and stays bare (4+ rule
			// for the prefix-wildcard tail).
			want: "find* OR callers* OR Vault* OR deposit* OR synthetic* OR corpus*",
		},
		{
			name: "trailing period still trims (regression check)",
			in:   "consensus.",
			want: "consensus*",
		},
		{
			name: "multi-segment qname splits to all segments",
			in:   "service.Vault.Deposit",
			want: "service* OR Vault* OR Deposit*",
		},
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

// TestRewriteFTSQuery_PowerUserGate locks B3 fix (2026-05-11
// VERIFICATION_REPORT §7.3): the earlier power-user passthrough triggered
// on any of `*"():` chars, which mis-classified natural-language
// descriptions containing parentheses ("Where does (X) get called:") and
// fed the raw `(` straight to FTS5. Power-user passthrough now requires
// `*` or `"` only — `(` `)` `:` flow through the per-token sanitiser.
func TestRewriteFTSQuery_PowerUserGate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		// B3 reproduction: parenthesis in prose no longer triggers passthrough.
		// `get` (3 chars) keeps without `*` per rewriteFTSQuery's length tier.
		{
			"prose with parentheses",
			"Where does (NewBlockChain) get called",
			"Where* OR does* OR NewBlockChain* OR called*",
		},
		// Trailing colon (common in prose) no longer triggers passthrough.
		{
			"prose with trailing colon",
			"investigate WBFT prepare:",
			"investigate* OR WBFT* OR prepare*",
		},
		// `*` still signals power-user — verbatim passthrough preserved.
		{
			"explicit FTS5 wildcard",
			"NewBlock*",
			"NewBlock*",
		},
		// `"` still signals power-user — phrase queries preserved.
		{
			"explicit FTS5 phrase",
			`"exact phrase"`,
			`"exact phrase"`,
		},
		// Combined `*"` — verbatim passthrough preserved.
		{
			"wildcard + phrase",
			`"foo" OR bar*`,
			`"foo" OR bar*`,
		},
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
