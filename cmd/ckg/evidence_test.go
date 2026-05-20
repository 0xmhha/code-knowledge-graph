package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/evidence"
)

// TestRenderEvidenceText covers the text-format formatter independently
// of BuildPack — drives a hand-crafted Pack so the assertions are
// stable regardless of corpus / BM25 / git fixture state.
func TestRenderEvidenceText(t *testing.T) {
	pack := &evidence.Pack{
		Intent: "panel jitter",
		Hits: []evidence.Hit{
			{
				Commit: evidence.CommitInfo{
					SHA:        "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
					Subject:    "fix: panel jitter",
					AuthorTime: 1700000000,
					IssueIDs:   []string{"GH-42"},
				},
				Hunks: []evidence.HunkRow{
					{
						ID: "h1", FilePath: "Panel.tsx", StartLine: 10, EndLine: 14,
						PatchText: "@@ -10,4 +10,5 @@\n-old\n+new\n+also new\n context\n",
					},
				},
			},
		},
	}
	var buf bytes.Buffer
	renderEvidenceText(&buf, pack)
	out := buf.String()
	for _, want := range []string{
		"1 commit(s):",
		"aaaa1111aaaa", // truncated SHA
		"[GH-42]",      // issue badge
		"fix: panel jitter",
		"Panel.tsx L10-14:",
		"+new", // patch body
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

// TestRenderEvidenceText_TruncatesLongPatch ensures the >previewLines
// guard fires and surfaces "… (N more lines)". Stops shell users from
// drowning in 500-line patches by default.
func TestRenderEvidenceText_TruncatesLongPatch(t *testing.T) {
	bigPatch := "@@ +1,20 @@\n" + strings.Repeat("+ line\n", 20)
	pack := &evidence.Pack{
		Hits: []evidence.Hit{{
			Commit: evidence.CommitInfo{SHA: "bbbb2222bbbb", Subject: "huge"},
			Hunks: []evidence.HunkRow{{
				ID: "h2", FilePath: "x.go", StartLine: 1, EndLine: 1,
				PatchText: bigPatch,
			}},
		}},
	}
	var buf bytes.Buffer
	renderEvidenceText(&buf, pack)
	out := buf.String()
	if !strings.Contains(out, "more lines)") {
		t.Errorf("expected truncation marker; got:\n%s", out)
	}
}

// TestRenderEvidenceText_Empty handles the "no hits" path so shell
// scripts can detect zero results without parsing JSON.
func TestRenderEvidenceText_Empty(t *testing.T) {
	var buf bytes.Buffer
	renderEvidenceText(&buf, &evidence.Pack{})
	if got := strings.TrimSpace(buf.String()); got != "(no hits)" {
		t.Errorf("empty pack output = %q, want %q", got, "(no hits)")
	}
}

// TestEvidenceCmd_RequiresIntentOrIssue locks in the contract: the
// command refuses to run when neither --intent nor --issue is set.
// Mirrors the server-side handleEvidence guard.
func TestEvidenceCmd_RequiresIntentOrIssue(t *testing.T) {
	cmd := newEvidenceCmd()
	cmd.SetArgs([]string{"--graph", "/tmp/nonexistent"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error when intent and issue both empty")
	}
	if !strings.Contains(err.Error(), "intent") || !strings.Contains(err.Error(), "issue") {
		t.Errorf("error %q should mention both --intent and --issue", err)
	}
}

// TestEvidenceCmd_RejectsUnknownFormat covers the format validation
// path — we want a clear early failure rather than a silent text
// fallback when the user mistypes.
func TestEvidenceCmd_RejectsUnknownFormat(t *testing.T) {
	cmd := newEvidenceCmd()
	cmd.SetArgs([]string{"--graph", "/tmp/nonexistent", "--intent", "anything", "--format", "yaml"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for unknown format")
	}
	if !strings.Contains(err.Error(), "yaml") {
		t.Errorf("error should echo the bad format value; got %q", err)
	}
}
