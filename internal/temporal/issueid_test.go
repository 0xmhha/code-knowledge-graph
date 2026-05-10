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
		"Fixes #123":                                    {"GH-123"},
		"Closes #45 and reopens #67":                    {"GH-45", "GH-67"},
		"feat: panel re-mount fix (#80)":                {"GH-80"},
		// Bracketed Linear/Jira/internal style
		"[INGEST-401] retry budget rework":              {"INGEST-401"},
		"[ABC-456] follow-up to [DEF-789]":              {"ABC-456", "DEF-789"},
		// Bare Jira-style at line start
		"INGEST-789: kafka backpressure":                {"INGEST-789"},
		"WEM-12345: hardfork bootstrap":                 {"WEM-12345"},
		// URL form
		"Closes https://github.com/foo/bar/issues/42":   {"GH-foo/bar#42"},
		"merge https://github.com/etcd-io/etcd/issues/9": {"GH-etcd-io/etcd#9"},
		// Mixed: multiple patterns in one subject
		// WEM-3 is mid-line so the Jira-prefix regex (line-start only)
		// correctly skips it — matching it would also catch noise like
		// "version SOME-123 mentioned" mid-sentence.
		"Fixes #1 and [ABC-2] per WEM-3: deadline":      {"ABC-2", "GH-1"},
		// No patterns — returns nil, not zero-length slice
		"refactor RPC client to be context-aware":        nil,
		"":                                               nil,
		// False-positive guards
		"version 1.0#123 release":                        nil, // no separator before #
		"abc INGEST-7 trailing":                          nil, // not at line start
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

// TestDecodeIssueIDs_NonIssueDocComment ensures plain doc_comment
// text on non-Hunk nodes (e.g. Function doc strings) doesn't get
// mistaken for issue data — only the `issues:` prefix triggers
// parsing.
func TestDecodeIssueIDs_NonIssueDocComment(t *testing.T) {
	cases := []string{
		"// regular function comment",
		"Some doc with a #123 reference",
		"issues",  // missing colon
		"issue:ABC-1", // singular prefix
	}
	for _, in := range cases {
		if got := DecodeIssueIDs(in); got != nil {
			t.Errorf("DecodeIssueIDs(%q) = %v, want nil", in, got)
		}
	}
}
