package types

import "time"

// PRRef is a build-time-derived reference from a graph node to a PR
// whose merge commit touched lines overlapping that node's source
// range. Surfaces "the recent changes around this symbol" without
// dragging the agent through `git log` itself.
//
// Built by internal/buildpipe.ScanPRHistory from `git log --merges`
// output: the merge commit's title is parsed for the canonical
// (#NNN) suffix; merge-commit timestamp + parent SHAs supply the
// remaining fields. PR title + the first line of the commit body
// (Background-style summary) come from the commit message itself —
// no gh API call is required for the 80% case (squash-merge
// workflows). Future iterations may opt in to gh enrichment for
// fuller summaries.
//
// # Temporal slicing
//
// MergedAtUTC drives the [store.Reader.GetNodePRs] cutoff filter
// (ckg-NEW-3). A cks scenario evaluating "what did the agent know
// at base_sha?" must not be allowed to see PRs merged after that
// timestamp, even though ckg's index aggregated every PR in
// history. The cutoff is applied at the SQL layer
// (`WHERE merged_at < ?`) so leakage is structurally impossible
// from the consumer's vantage point.
//
// # Field semantics
//
//   - Number: PR number from the (#NNN) match. 0 when the parser
//     couldn't extract one — emit the row anyway so consumers can
//     still see the merge commit (Title + summary carry context).
//   - Title: PR title — first non-empty line of the merge commit
//     message after the conventional "Merge pull request #NNN
//     from …" prefix is stripped, or the raw subject when the
//     prefix isn't present.
//   - Summary: optional short body excerpt. Empty when the merge
//     commit has no body or the parser couldn't isolate a sensible
//     first-line summary.
//   - BaseSHA / HeadSHA: parents of the merge commit (BaseSHA is
//     the first parent — the branch being merged into; HeadSHA is
//     the second parent — the feature branch's tip). Both empty
//     for non-merge fallbacks (rare; included for API completeness).
//   - MergedAtUTC: committer time of the merge commit, UTC.
//   - Repo: "owner/name" derived from the build root's git remote
//     `origin` URL when available; empty otherwise.
type PRRef struct {
	Number      int       `json:"number"`
	Title       string    `json:"title,omitempty"`
	Summary     string    `json:"summary,omitempty"`
	BaseSHA     string    `json:"base_sha,omitempty"`
	HeadSHA     string    `json:"head_sha,omitempty"`
	MergedAtUTC time.Time `json:"merged_at"`
	Repo        string    `json:"repo,omitempty"`
}
