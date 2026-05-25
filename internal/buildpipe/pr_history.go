package buildpipe

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// ScanPRHistory walks the git log under srcRoot, isolates commits whose
// subject carries the canonical "(#NNN)" PR-number suffix (catches both
// squash-merge and GitHub's "Merge pull request #NNN from …" forms),
// and returns the map of node ID → PR breadcrumbs for nodes whose
// [StartLine, EndLine] range overlaps the commit's touched line ranges.
//
// The output drives node_prs persistence (StoreWriter.InsertNodePRs)
// and the public Reader.GetNodePRs accessor (ckg-NEW-4). Empty input
// (no nodes / non-git tree / no PR-tagged commits) returns an empty
// map without an error — PR breadcrumb is optional metadata.
//
// Algorithm
//
//  1. `git config --get remote.origin.url` → owner/name (best effort).
//  2. `git log --no-merges --pretty=$FORMAT -nMAX` over the build root
//     captures every commit; alongside it we run the same with
//     `--merges` so GitHub-style merge commits (which carry "(#NNN)"
//     in the subject) survive in repos that still use --no-ff merges.
//     The 1000-commit cap matches G6 temporal's `--max-count=10`-per-
//     file working window scaled up by 100× for whole-repo scope and
//     bounded so stable-net's 50k commits don't blow the I/O budget.
//  3. PR-number regex `\(#(\d+)\)` runs against the subject.
//  4. For each PR-tagged commit, `git show <SHA> --unified=0
//     --pretty=` returns the patch; @@-headers carry the new-file
//     line range. Range overlap (commit hunk ∩ node line range) is
//     the matching key.
//  5. Title comes from the subject after a heuristic strip of the
//     "Merge pull request #NNN from …" or trailing "(#NNN)" patterns.
//     Summary is the first non-empty body line (often the squash-
//     merge PR description's first sentence). Both fall back to "".
//
// Cost is dominated by step 4 — one `git show` per PR-tagged commit.
// On ckg's own repo (4 PRs) this is millisecond-scale; on stable-net
// (~80 PRs across the 50k-commit history that survive the cap) the
// per-commit cost adds up but stays under a second.
func ScanPRHistory(srcRoot string, nodes []types.Node) (map[string][]types.PRRef, error) {
	out := map[string][]types.PRRef{}
	if srcRoot == "" || len(nodes) == 0 {
		return out, nil
	}

	repo := scanGitRemoteRepo(srcRoot)
	commits, err := listPRCommits(srcRoot)
	if err != nil {
		// Non-git source tree, or git not on PATH — treat as "no PR
		// history" rather than a build failure. The breadcrumb is
		// strictly additive metadata.
		return out, nil
	}
	if len(commits) == 0 {
		return out, nil
	}

	// Pre-index nodes by file_path so each commit's touched files
	// route to O(file_nodes) overlap checks instead of an O(all_nodes)
	// scan. The per-file slice is sorted by StartLine so overlap can
	// short-circuit when the commit hunk is below the file's first
	// node.
	nodesByFile := indexNodesByFile(nodes)

	for _, c := range commits {
		ranges, perr := patchLineRanges(srcRoot, c.SHA)
		if perr != nil || len(ranges) == 0 {
			continue
		}
		ref := types.PRRef{
			Number:      c.Number,
			Title:       c.Title,
			Summary:     c.Summary,
			BaseSHA:     c.BaseSHA,
			HeadSHA:     c.HeadSHA,
			MergedAtUTC: c.MergedAt,
			Repo:        repo,
		}
		for file, hunks := range ranges {
			fileNodes := nodesByFile[file]
			if len(fileNodes) == 0 {
				continue
			}
			for _, h := range hunks {
				for _, n := range fileNodes {
					if n.StartLine > h.End {
						// fileNodes is sorted; subsequent nodes start
						// even later, none overlaps this hunk.
						break
					}
					if rangesOverlap(n.StartLine, n.EndLine, h.Start, h.End) {
						out[n.ID] = append(out[n.ID], ref)
					}
				}
			}
		}
	}

	dedupePRRefsByNode(out)
	return out, nil
}

// prCommit is the in-memory record per PR-tagged commit. Captured from
// `git log` once so the per-hunk inner loop doesn't re-parse the same
// text for every file/node pair.
type prCommit struct {
	SHA      string
	BaseSHA  string
	HeadSHA  string
	Number   int
	Title    string
	Summary  string
	MergedAt time.Time
}

var prNumberRE = regexp.MustCompile(`\(#(\d+)\)`)

// listPRCommits runs git log over srcRoot, parses each commit, and
// returns the subset whose subject carries a (#NNN) suffix. We pull
// both --merges and --no-merges so GitHub's "Merge pull request"
// flavour and the squash-merge "Title (#NNN)" flavour both survive.
//
// The pretty-format uses NUL terminators (%x00) between the per-record
// fields and a custom record terminator (`<<<COMMIT_END>>>`) so commit
// bodies that include newlines or pipes don't trip the parser.
func listPRCommits(srcRoot string) ([]prCommit, error) {
	// git's pretty=format cannot transport a literal NUL byte from the
	// host process arg into its formatter — the byte stops the format
	// string mid-parse and only the leading %H is emitted. The %x00
	// placeholder lets git itself emit a NUL inline, which arrives in
	// stdout intact. Record separator uses the same %x01 trick (SOH)
	// because some commit messages contain `<<<COMMIT_END>>>`-like
	// markers and we don't want to false-split.
	const recordSep = "\x01"
	const fieldSep = "\x00"
	format := strings.Join([]string{"%H", "%P", "%cI", "%s", "%b"}, "%x00") + "%x01"
	cmd := exec.Command("git", "-C", srcRoot, "log",
		"--max-count=1000", "--pretty=format:"+format)
	stdout, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	raw := string(stdout)
	records := strings.Split(raw, recordSep)
	out := make([]prCommit, 0, len(records))
	for _, rec := range records {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		fields := strings.SplitN(rec, fieldSep, 5)
		if len(fields) < 5 {
			continue
		}
		subj := fields[3]
		m := prNumberRE.FindStringSubmatch(subj)
		if len(m) < 2 {
			continue
		}
		num, _ := strconv.Atoi(m[1])
		ts, _ := time.Parse(time.RFC3339, fields[2])
		parents := strings.Fields(fields[1])
		base := ""
		head := fields[0]
		if len(parents) >= 1 {
			base = parents[0]
		}
		if len(parents) >= 2 {
			head = parents[1]
		}
		out = append(out, prCommit{
			SHA:      fields[0],
			BaseSHA:  base,
			HeadSHA:  head,
			Number:   num,
			Title:    cleanPRTitle(subj),
			Summary:  firstNonEmptyLine(fields[4]),
			MergedAt: ts.UTC(),
		})
	}
	return out, nil
}

// cleanPRTitle strips the canonical (#NNN) suffix and the
// "Merge pull request #NNN from …" prefix that GitHub injects on
// non-squash merges. Returns the original subject untouched when
// neither pattern matches.
func cleanPRTitle(subj string) string {
	if strings.HasPrefix(subj, "Merge pull request #") {
		// "Merge pull request #NNN from owner/branch" — the title we
		// want lives downstream of this commit in the body; without
		// it the subject is just noise. Return empty so the caller's
		// fallback (Summary first line, then the (#NNN) marker
		// itself) is the surface contract.
		return ""
	}
	return strings.TrimSpace(prNumberRE.ReplaceAllString(subj, ""))
}

// firstNonEmptyLine returns the first non-blank line of a multi-line
// commit body. Empty string when the body is blank.
func firstNonEmptyLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// hunkRange is one [Start, End] new-file line range from a unified
// diff @@-header. Inclusive both ends.
type hunkRange struct {
	Start int
	End   int
}

var hunkHeaderRE = regexp.MustCompile(`^@@ [^@]+\+(\d+)(?:,(\d+))? @@`)

// patchLineRanges runs `git show <SHA> --unified=0 --pretty=` to read
// the raw patch and parses the new-file (`+A,B`) ranges out of each
// @@-header. Keyed by file path (relative to the repo root). Returns
// nil + error only when git itself failed; an empty map is the normal
// "this commit has no patch" outcome (e.g. a pure rename).
func patchLineRanges(srcRoot, sha string) (map[string][]hunkRange, error) {
	out := map[string][]hunkRange{}
	cmd := exec.Command("git", "-C", srcRoot, "show", sha,
		"--unified=0", "--pretty=", "--no-color")
	stdout, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git show %s: %w", sha, err)
	}
	var currentFile string
	for _, line := range strings.Split(string(stdout), "\n") {
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			currentFile = strings.TrimPrefix(line, "+++ b/")
		case strings.HasPrefix(line, "+++ /dev/null"):
			currentFile = ""
		case strings.HasPrefix(line, "@@ "):
			if currentFile == "" {
				continue
			}
			m := hunkHeaderRE.FindStringSubmatch(line)
			if len(m) < 2 {
				continue
			}
			start, _ := strconv.Atoi(m[1])
			length := 1
			if m[2] != "" {
				length, _ = strconv.Atoi(m[2])
			}
			if length == 0 {
				// A 0-length new-side hunk is a pure-delete chunk;
				// `start` is the line BEFORE which the deletion
				// occurred. No new-file line range to record.
				continue
			}
			out[currentFile] = append(out[currentFile], hunkRange{
				Start: start,
				End:   start + length - 1,
			})
		}
	}
	return out, nil
}

// scanGitRemoteRepo runs `git config --get remote.origin.url` and
// reduces the URL to "owner/name". Returns "" on any failure — Repo
// is optional metadata.
//
// Accepts both HTTPS (https://github.com/owner/name.git) and SSH
// (git@github.com:owner/name.git) forms. Strips the .git suffix.
func scanGitRemoteRepo(srcRoot string) string {
	cmd := exec.Command("git", "-C", srcRoot, "config", "--get", "remote.origin.url")
	stdout, err := cmd.Output()
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(stdout))
	url = strings.TrimSuffix(url, ".git")
	if i := strings.Index(url, "github.com"); i >= 0 {
		tail := url[i+len("github.com"):]
		tail = strings.TrimPrefix(tail, "/")
		tail = strings.TrimPrefix(tail, ":")
		return tail
	}
	return ""
}

// indexNodesByFile groups nodes by their FilePath and sorts each
// per-file slice by StartLine so the overlap loop can short-circuit
// once it walks past the commit hunk's end.
func indexNodesByFile(nodes []types.Node) map[string][]types.Node {
	out := map[string][]types.Node{}
	for _, n := range nodes {
		if n.FilePath == "" {
			continue
		}
		out[n.FilePath] = append(out[n.FilePath], n)
	}
	for k := range out {
		sort.Slice(out[k], func(i, j int) bool {
			return out[k][i].StartLine < out[k][j].StartLine
		})
	}
	return out
}

// rangesOverlap reports whether [a1,a2] and [b1,b2] share at least one
// line. Inclusive both ends, matching the [StartLine, EndLine] +
// hunkRange semantics.
func rangesOverlap(a1, a2, b1, b2 int) bool {
	return a1 <= b2 && b1 <= a2
}

// dedupePRRefsByNode collapses duplicate PR rows the overlap loop may
// emit when a node spans multiple hunks of the same commit. The
// PRIMARY KEY (node_id, number) in node_prs would otherwise force
// INSERT OR REPLACE to thrash on duplicates. Sort by MergedAtUTC
// descending so the SQL writer can rely on a stable order for
// pagination.
func dedupePRRefsByNode(byNode map[string][]types.PRRef) {
	for id, refs := range byNode {
		seen := map[int]bool{}
		out := refs[:0]
		for _, r := range refs {
			if seen[r.Number] {
				continue
			}
			seen[r.Number] = true
			out = append(out, r)
		}
		sort.Slice(out, func(i, j int) bool {
			return out[i].MergedAtUTC.After(out[j].MergedAtUTC)
		})
		byNode[id] = out
	}
}
