package temporal

import (
	"reflect"
	"testing"
)

// TestExtractIssueIDs_FourPatterns covers the four regex paths from
// design §10.4. Each pattern should produce the expected normalised
// identifier without leaking variants of the same ID.
func TestExtractIssueIDs_FourPatterns(t *testing.T) {
	cases := map[string][]string{
		// GitHub bare hash
		"Fixes #123":                     {"GH-123"},
		"Closes #45 and reopens #67":     {"GH-45", "GH-67"},
		"feat: panel re-mount fix (#80)": {"GH-80"},
		// Bracketed Linear/Jira/internal style
		"[INGEST-401] retry budget rework": {"INGEST-401"},
		"[ABC-456] follow-up to [DEF-789]": {"ABC-456", "DEF-789"},
		// Bare Jira-style at line start
		"INGEST-789: kafka backpressure": {"INGEST-789"},
		"WEM-12345: hardfork bootstrap":  {"WEM-12345"},
		// URL form
		"Closes https://github.com/foo/bar/issues/42":    {"GH-foo/bar#42"},
		"merge https://github.com/etcd-io/etcd/issues/9": {"GH-etcd-io/etcd#9"},
		// Mixed: multiple patterns in one subject
		// WEM-3 is mid-line so the Jira-prefix regex (line-start only)
		// correctly skips it — matching it would also catch noise like
		// "version SOME-123 mentioned" mid-sentence.
		"Fixes #1 and [ABC-2] per WEM-3: deadline": {"ABC-2", "GH-1"},
		// No patterns — returns nil, not zero-length slice
		"refactor RPC client to be context-aware": nil,
		"": nil,
		// False-positive guards
		"version 1.0#123 release": nil, // no separator before #
		"abc INGEST-7 trailing":   nil, // not at line start
	}
	for subject, want := range cases {
		got := ExtractIssueIDs(subject)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("ExtractIssueIDs(%q) = %v, want %v", subject, got, want)
		}
	}
}

// TestExtractIssueIDs_DedupAndSort verifies a subject containing the
// same ID via two different patterns reports it once, sorted.
func TestExtractIssueIDs_DedupAndSort(t *testing.T) {
	got := ExtractIssueIDs("[ABC-1] follows up #2 alongside ABC-1: same thing")
	want := []string{"ABC-1", "GH-2"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dedup/sort: got %v, want %v", got, want)
	}
}

// TestEncodeDecodeRoundTrip locks in the design §10.4 storage shape:
// EncodeIssueIDs produces `issues:ID1;ID2;…` and DecodeIssueIDs is
// its left-inverse.
func TestEncodeDecodeRoundTrip(t *testing.T) {
	cases := [][]string{
		nil,
		{},
		{"GH-1"},
		{"ABC-100", "GH-2", "WEM-3"},
	}
	for _, in := range cases {
		encoded := EncodeIssueIDs(in)
		if len(in) == 0 {
			if encoded != "" {
				t.Errorf("empty in should encode to empty string, got %q", encoded)
			}
			if got := DecodeIssueIDs(encoded); got != nil {
				t.Errorf("decode empty: got %v, want nil", got)
			}
			continue
		}
		decoded := DecodeIssueIDs(encoded)
		if !reflect.DeepEqual(decoded, in) {
			t.Errorf("round-trip: in %v -> %q -> %v", in, encoded, decoded)
		}
	}
}

// TestExtractIssueIDs_CorpusPrecisionRecall measures the extractor
// against a hand-labelled corpus of 30 realistic commit subjects from
// open-source repos (anonymised). Locks in regex precision/recall so
// future tweaks (e.g. broadening the URL pattern) surface immediately
// as a metric drift instead of silently flipping a few results.
//
// Each entry is {subject, expected_ids_sorted}. Recall = (true
// positives) / (true positives + false negatives) per subject; we
// require 100% on this corpus because every entry is constructed to
// trip exactly one of the four §10.4 patterns. Precision = no spurious
// IDs (extractor returns *exactly* the expected set, or fails the
// case).
func TestExtractIssueIDs_CorpusPrecisionRecall(t *testing.T) {
	corpus := []struct {
		subject string
		want    []string
	}{
		// Bare-hash GH style — most common in OSS PRs.
		{"fix(viewer): legend dragging (#80)", []string{"GH-80"}},
		{"docs: add rate-limit guidance Closes #1234", []string{"GH-1234"}},
		{"chore: bump deps #99", []string{"GH-99"}},
		{"refactor: extract auth helper, related #11 and #22", []string{"GH-11", "GH-22"}},
		{"feat: SSO support (#101) and (#202)", []string{"GH-101", "GH-202"}},
		// Bracketed Linear/Jira-style.
		{"[INGEST-401] retry budget rework", []string{"INGEST-401"}},
		{"[ENG-77] pre-flight cache", []string{"ENG-77"}},
		{"[ABC-1] follow-up to [DEF-2]", []string{"ABC-1", "DEF-2"}},
		// Bare Jira-style at line start.
		{"INGEST-789: kafka backpressure", []string{"INGEST-789"}},
		{"WEM-12345: hardfork bootstrap script", []string{"WEM-12345"}},
		{"ENG-1234 add config validation", []string{"ENG-1234"}},
		// URL form — the §10.4 GH-URL pattern.
		{"closes https://github.com/foo/bar/issues/42", []string{"GH-foo/bar#42"}},
		{"see https://github.com/etcd-io/etcd/issues/9 for details", []string{"GH-etcd-io/etcd#9"}},
		// Mixed — multi-pattern subjects.
		{"Fixes #1 and [ABC-2] per WEM-3: deadline", []string{"ABC-2", "GH-1"}}, // WEM-3 mid-line: line-start regex skips
		// ENG-52 is mid-line and reJiraPrefix is line-start-only by
		// design (false-positive guard). Only the bracketed and
		// GH-hash IDs surface.
		{"[ENG-50] and (#51) and ENG-52: triple", []string{"ENG-50", "GH-51"}},
		// Negative cases — should yield nothing.
		{"refactor RPC client to be context-aware", nil},
		{"chore: update go.mod", nil},
		{"version 1.0#123 release", nil},          // no separator before #
		{"abc INGEST-7 trailing", nil},            // not at line start
		{"check the docs at /api/users#123", nil}, // path fragment is not a separator
		// Real-world subjects (from open-source projects, anonymised).
		{"feat(api): rate limiter (closes #543, fixes #544)", []string{"GH-543", "GH-544"}},
		{"WEM-100: enable bootstrap state replay", []string{"WEM-100"}},
		{"merge dev to master (#80)", []string{"GH-80"}},
		{"[OPS-77] disable obsolete cron job", []string{"OPS-77"}},
		{"hotfix: panic in scheduler #999", []string{"GH-999"}},
		// Edge cases the four patterns intentionally tolerate or skip.
		// Both WEM-1 and INGEST-2 are mid-line; reJiraPrefix's
		// line-start anchor skips both. Locked-in to document the
		// guard's coverage cost — flipping this case would mean a
		// regex relaxation went through.
		{"mention WEM-1 mid-paragraph then INGEST-2: real", nil},
		{"v1.2.3 (#5)", []string{"GH-5"}},
		{"empty separators ( #6 ) ", []string{"GH-6"}},
		// Duplicate via two patterns — must deduplicate.
		{"[GH-7] cross-mention #7", []string{"GH-7"}},
		// Long-tail negatives.
		{"   ", nil},
	}

	tp, fp, fn := 0, 0, 0
	for i, tc := range corpus {
		got := ExtractIssueIDs(tc.subject)
		gotSet := toSet(got)
		wantSet := toSet(tc.want)
		for id := range wantSet {
			if gotSet[id] {
				tp++
			} else {
				fn++
				t.Errorf("#%d FN: subject=%q missing %q (got %v)", i, tc.subject, id, got)
			}
		}
		for id := range gotSet {
			if !wantSet[id] {
				fp++
				t.Errorf("#%d FP: subject=%q surfaced unexpected %q (want %v)", i, tc.subject, id, tc.want)
			}
		}
	}
	// Threshold guards: any drift should already fail above as TC errors,
	// but we record summary metrics so a future relaxation surfaces
	// in the test log even if the per-case asserts get loosened.
	t.Logf("corpus metrics: TP=%d FP=%d FN=%d (precision=%.2f%% recall=%.2f%%)",
		tp, fp, fn,
		100*float64(tp)/float64(max(tp+fp, 1)),
		100*float64(tp)/float64(max(tp+fn, 1)),
	)
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func max(a, b int) int { // local — Go 1.20 stdlib doesn't expose math.Max for ints
	if a > b {
		return a
	}
	return b
}

// TestDecodeIssueIDs_NonIssueDocComment ensures plain doc_comment
// text on non-Hunk nodes (e.g. Function doc strings) doesn't get
// mistaken for issue data — only the `issues:` prefix triggers
// parsing.
func TestDecodeIssueIDs_NonIssueDocComment(t *testing.T) {
	cases := []string{
		"// regular function comment",
		"Some doc with a #123 reference",
		"issues",      // missing colon
		"issue:ABC-1", // singular prefix
	}
	for _, in := range cases {
		if got := DecodeIssueIDs(in); got != nil {
			t.Errorf("DecodeIssueIDs(%q) = %v, want nil", in, got)
		}
	}
}
