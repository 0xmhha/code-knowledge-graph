package eval

import (
	"strings"
	"testing"
)

func TestFilterHallucinations_ReplacesMarkedSymbol(t *testing.T) {
	hallu := HallucinationResult{Hallucinated: []string{"Token.transfer"}}
	in := "Call Token.transfer to move funds."
	got, warnings := FilterHallucinations(in, hallu)

	wantContains := "[unverified: Token.transfer]"
	if !strings.Contains(got, wantContains) {
		t.Errorf("filtered text missing marker: got %q, want it to contain %q", got, wantContains)
	}
	if !strings.Contains(got, "Call ") || !strings.Contains(got, " to move funds.") {
		t.Errorf("surrounding prose not preserved: got %q", got)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings: got %d, want 1 (%v)", len(warnings), warnings)
	}
}

func TestFilterHallucinations_WordBoundaryProtectsLongerSymbol(t *testing.T) {
	// `Token.transfer` flagged. `Token.transferFrom` must NOT be
	// rewritten: it's a different symbol that happens to share a
	// prefix.
	hallu := HallucinationResult{Hallucinated: []string{"Token.transfer"}}
	in := "Use Token.transferFrom or Token.transfer."
	got, _ := FilterHallucinations(in, hallu)

	if !strings.Contains(got, "Token.transferFrom") {
		t.Errorf("Token.transferFrom must survive: got %q", got)
	}
	if !strings.Contains(got, "[unverified: Token.transfer]") {
		t.Errorf("Token.transfer must be replaced at the end: got %q", got)
	}
}

func TestFilterHallucinations_QnameDivergedGoesToWarnings(t *testing.T) {
	hallu := HallucinationResult{
		QnameDiverged: []string{"Vault.deposit"},
	}
	in := "See Vault.deposit for details."
	got, warnings := FilterHallucinations(in, hallu)

	// QnameDiverged is NOT rewritten in text — the bare name
	// resolved, the dotted form just didn't align. The text stays.
	if got != in {
		t.Errorf("text should be unchanged for qname-diverged: got %q, want %q", got, in)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings: got %d, want 1 (%v)", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "Vault.deposit") {
		t.Errorf("warning should name the symbol: %v", warnings)
	}
}

// Note: filter is not idempotent under naïve re-application.
// `[unverified: Token.transfer]` still contains the substring
// `Token.transfer`, and replaceWholeToken sees it as a fresh
// match. The contract is one-pass: callers feed (raw text, raw
// HallucinationResult) → cleaned text; they do not re-feed the
// cleaned text back through the filter with the same
// hallucination set. Detecting "is this position inside an
// `[unverified: ...]` marker" cleanly is more code than the use
// case justifies — the filter runs once per LLM response in the
// runner, and consumers receive the already-filtered text.

func TestFilterHallucinations_EmptyResult(t *testing.T) {
	got, warnings := FilterHallucinations("Call core.NewBlockChain.", HallucinationResult{})
	if got != "Call core.NewBlockChain." {
		t.Errorf("empty hallucination should be no-op: got %q", got)
	}
	if len(warnings) != 0 {
		t.Errorf("empty hallucination should produce no warnings: %v", warnings)
	}
}
