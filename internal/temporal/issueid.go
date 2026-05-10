// Package temporal — issueid.go extracts issue/ticket identifiers from
// commit subject lines for the H4 stage of the hunk-graph series
// (docs/design/hunk-graph.md §10.4). Three regex passes recognise:
//
//   - GitHub-style bare references: `#123`, `Fixes #45`, `(#67)` →
//     normalised as `GH-123` / `GH-45` / `GH-67`.
//
//   - Bracketed Linear / Jira / internal IDs: `[ABC-456]`, `[INGEST-789]`
//     → kept verbatim (`ABC-456`, `INGEST-789`).
//
//   - Unbracketed Jira-style ticket prefix at line start:
//     `INGEST-789: brief subject` → `INGEST-789`. The leading-position
//     constraint avoids accidental matches like `e.g. SOME-123 we use…`
//     in the middle of a sentence (false-positive risk too high there).
//
//   - GitHub issue URLs: `https://github.com/owner/repo/issues/42` →
//     `GH-owner/repo#42`. URL form is rare in subjects (more common in
//     bodies) but we keep the parser symmetric for completeness.
//
// Output is deduped and sorted lexicographically so the encoded
// `doc_comment` stays deterministic across builds — the same commit
// always produces the same issue_ids string regardless of regex
// match order.
package temporal

import (
	"regexp"
	"sort"
	"strings"
)

var (
	// `(?:^|[^A-Za-z0-9])` — guard against substring matches inside
	// identifiers (e.g. `c5#123` where `5#123` is the source). We allow
	// start-of-string OR a non-alphanumeric prefix character.
	reGitHubHash = regexp.MustCompile(`(?:^|[^A-Za-z0-9])#(\d{1,7})`)
	reBracketed  = regexp.MustCompile(`\[([A-Z][A-Z0-9]{1,9}-\d{1,7})\]`)
	// Leading-position only — the (?m) flag treats ^ as line-start.
	// Subject lines on a single string still match because ^ holds at
	// position 0 regardless of the (?m) flag.
	reJiraPrefix = regexp.MustCompile(`(?m)^([A-Z][A-Z0-9]{1,9}-\d{1,7})\b`)
	reGitHubURL  = regexp.MustCompile(`https?://github\.com/([^\s/]+/[^\s/]+)/issues/(\d{1,7})`)
)

// ExtractIssueIDs returns deduped sorted issue identifiers found in
// subject. Returns nil (not a zero-length slice) when no patterns
// match so callers can use `if ids := ExtractIssueIDs(...); ids ==
// nil` for the "no issues" branch.
//
// The patterns are intentionally conservative — over-matching would
// pollute the H3 EvidencePack with spurious "this hunk fixes #123"
// claims that don't reflect the commit's stated intent. False
// negatives are recoverable (a follow-up PR can widen the patterns
// when eval shows demand); false positives ship to the LLM and can
// mislead.
func ExtractIssueIDs(subject string) []string {
	if subject == "" {
		return nil
	}
	set := make(map[string]struct{}, 4)

	for _, m := range reGitHubHash.FindAllStringSubmatch(subject, -1) {
		set["GH-"+m[1]] = struct{}{}
	}
	for _, m := range reBracketed.FindAllStringSubmatch(subject, -1) {
		set[m[1]] = struct{}{}
	}
	for _, m := range reJiraPrefix.FindAllStringSubmatch(subject, -1) {
		set[m[1]] = struct{}{}
	}
	for _, m := range reGitHubURL.FindAllStringSubmatch(subject, -1) {
		set["GH-"+m[1]+"#"+m[2]] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// EncodeIssueIDs serialises ids into the design §10.4 storage shape:
// `issues:ID1;ID2;…`. Returns the empty string when ids is empty so
// callers can assign the result directly to Node.DocComment without a
// nil check (an empty doc_comment is valid).
func EncodeIssueIDs(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return "issues:" + strings.Join(ids, ";")
}

// DecodeIssueIDs is the inverse of EncodeIssueIDs — parses a Hunk's
// doc_comment field back into the slice of issue identifiers. Returns
// nil for any input that doesn't carry the `issues:` prefix (so plain
// doc_comment text on non-Hunk nodes doesn't get mistaken for issue
// data).
func DecodeIssueIDs(docComment string) []string {
	const prefix = "issues:"
	if !strings.HasPrefix(docComment, prefix) {
		return nil
	}
	body := docComment[len(prefix):]
	if body == "" {
		return nil
	}
	parts := strings.Split(body, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
