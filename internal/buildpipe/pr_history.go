package buildpipe

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
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
//     Summary is the cleaned commit body — trailers (Signed-off-by:,
//     Co-authored-by:, Generated with…) are dropped and the result is
//     capped at bodyExcerptMaxBytes on a line boundary. This carries
//     the "왜" history that CKV's semantic search ingests; see
//     docs/PROJECT-BLUEPRINT-ALIGNMENT.md §4.2. Both fall back to "".
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

	// Pass 2 (ckg-NEW-4b): override definition-node attribution with
	// drift-free git-`-L` line-history. The Pass-1 overlap above compares a
	// node's CURRENT [StartLine,EndLine] against each PR commit's historical
	// hunk range; when code moves between the commit and HEAD those ranges
	// diverge, so a definition can miss PRs that genuinely changed it (and
	// pick up PRs that now sit at the old lines). `git log -L<a>,<b>:<file>`
	// follows the line range backwards through history, so attribution is
	// position-independent. File and statement nodes keep the Pass-1 result
	// (a File node spans the whole file, so overlap is already exact for it).
	overrideDefinitionHistory(srcRoot, repo, nodes, out)

	dedupePRRefsByNode(out)
	return out, nil
}

// defHistoryNodeTypes are the node kinds for which symbol-precise (git-`-L`)
// change history is worth the per-node `git log` cost: callables and type
// declarations — the symbols an agent asks "how/why did this change?" about.
// Sub-symbol nodes (Field/Variable/Constant/Parameter) and statement nodes
// keep the cheaper Pass-1 overlap attribution.
var defHistoryNodeTypes = map[types.NodeType]bool{
	types.NodeFunction:    true,
	types.NodeMethod:      true,
	types.NodeStruct:      true,
	types.NodeInterface:   true,
	types.NodeConstructor: true,
	types.NodeModifier:    true,
	types.NodeContract:    true,
	types.NodeEvent:       true,
	types.NodeClass:       true,
	types.NodeTypeAlias:   true,
	types.NodeEnum:        true,
}

// lineHistoryMaxCommits bounds the per-node `git log -L` walk so a hot file's
// god-function doesn't drag the whole pass. Mirrors the temporal G6 per-file
// cap intent; deeper genealogy is truncated newest-first.
const lineHistoryMaxCommits = 40

// overrideDefinitionHistory recomputes PR attribution for definition nodes via
// `git log -L` and replaces their entry in out. Runs a bounded worker pool of
// `git log` subprocesses. On a git failure for a node it leaves the Pass-1
// entry untouched; on success it sets the git-`-L` result (possibly empty,
// meaning the symbol's lines were never touched by a PR-tagged commit).
func overrideDefinitionHistory(srcRoot, repo string, nodes []types.Node, out map[string][]types.PRRef) {
	defs := make([]types.Node, 0, len(nodes))
	for _, n := range nodes {
		if defHistoryNodeTypes[n.Type] && n.FilePath != "" && n.StartLine >= 1 && n.EndLine >= n.StartLine {
			defs = append(defs, n)
		}
	}
	if len(defs) == 0 {
		return
	}
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	sem := make(chan struct{}, workers)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, n := range defs {
		wg.Add(1)
		sem <- struct{}{}
		go func(n types.Node) {
			defer wg.Done()
			defer func() { <-sem }()
			refs, ok := lineHistoryRefs(srcRoot, repo, n)
			if !ok {
				return // git failed for this node — keep the Pass-1 baseline
			}
			mu.Lock()
			if len(refs) > 0 {
				out[n.ID] = refs
			} else {
				delete(out, n.ID)
			}
			mu.Unlock()
		}(n)
	}
	wg.Wait()
}

// lineHistoryRefs returns the PR refs whose commits modified node n's source
// line range, following the range across history with `git log -L`. The bool
// is false only when git itself errored (so the caller can preserve the
// Pass-1 baseline rather than wipe it).
func lineHistoryRefs(srcRoot, repo string, n types.Node) ([]types.PRRef, bool) {
	const fieldSep = "\x00"
	const recordSep = "\x01"
	format := strings.Join([]string{"%H", "%P", "%cI", "%s", "%b"}, "%x00") + "%x01"
	cmd := exec.Command("git", "-C", srcRoot, "log", "HEAD",
		fmt.Sprintf("--max-count=%d", lineHistoryMaxCommits),
		"--no-patch", "--format="+format,
		fmt.Sprintf("-L%d,%d:%s", n.StartLine, n.EndLine, n.FilePath))
	stdout, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	var refs []types.PRRef
	seen := map[int]bool{}
	for _, rec := range strings.Split(string(stdout), recordSep) {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		fields := strings.SplitN(rec, fieldSep, 5)
		if len(fields) < 5 {
			continue
		}
		m := prNumberRE.FindStringSubmatch(fields[3])
		if len(m) < 2 {
			continue
		}
		num, _ := strconv.Atoi(m[1])
		if seen[num] {
			continue
		}
		seen[num] = true
		ts, _ := time.Parse(time.RFC3339, fields[2])
		parents := strings.Fields(fields[1])
		base, head := "", fields[0]
		if len(parents) >= 1 {
			base = parents[0]
		}
		if len(parents) >= 2 {
			head = parents[1]
		}
		refs = append(refs, types.PRRef{
			Number:      num,
			Title:       cleanPRTitle(fields[3]),
			Summary:     bodyExcerpt(fields[4]),
			BaseSHA:     base,
			HeadSHA:     head,
			MergedAtUTC: ts.UTC(),
			Repo:        repo,
		})
	}
	return refs, true
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
			Summary:  bodyExcerpt(fields[4]),
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

// bodyExcerptMaxBytes caps the persisted Summary so a runaway PR
// template (CI logs pasted into the description, multi-MB changelogs)
// can't bloat node_prs row size beyond what CKV's semantic search
// usefully consumes. 2 KB comfortably holds the typical "왜 이 변경"
// paragraph plus an acceptance-criteria list; beyond that the excerpt
// is truncated at the nearest line boundary with an ellipsis marker.
const bodyExcerptMaxBytes = 2048

// bodyExcerpt returns the "why" portion of a commit body — the
// multi-line description sandwiched between the subject (already
// captured in Title) and any trailing git trailers (Signed-off-by:,
// Co-authored-by:, etc.). This is the P0 enrichment from
// docs/PROJECT-BLUEPRINT-ALIGNMENT.md §4.2 — previously this field
// captured only the first non-empty body line, which dropped the bulk
// of the rationale that CKV's semantic search depends on.
//
// Transformations, in order:
//  1. Right-trim trailing whitespace on every line.
//  2. Drop lines matching the well-known git trailer set so attribution
//     and review metadata don't leak into the "왜" text.
//  3. Trim leading and trailing blank lines.
//  4. Cap at bodyExcerptMaxBytes; truncation walks back to the
//     previous newline so the excerpt always ends on a clean line
//     and appends "…" as a clipping marker.
//
// Empty body → "". A body that is *entirely* trailers (no real
// description — rare but possible on squash merges with no PR
// description) also returns "" so consumers can fall back to Title.
func bodyExcerpt(body string) string {
	if body == "" {
		return ""
	}
	keep := make([]string, 0, 16)
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		if isTrailerLine(trimmed) {
			continue
		}
		keep = append(keep, trimmed)
	}
	for len(keep) > 0 && strings.TrimSpace(keep[0]) == "" {
		keep = keep[1:]
	}
	for len(keep) > 0 && strings.TrimSpace(keep[len(keep)-1]) == "" {
		keep = keep[:len(keep)-1]
	}
	if len(keep) == 0 {
		return ""
	}
	out := strings.Join(keep, "\n")
	if len(out) <= bodyExcerptMaxBytes {
		return out
	}
	cut := strings.LastIndexByte(out[:bodyExcerptMaxBytes], '\n')
	if cut <= 0 {
		cut = bodyExcerptMaxBytes
	}
	return out[:cut] + "\n…"
}

// isTrailerLine reports whether line is a recognised git trailer that
// should be excluded from the "왜" excerpt. The set is intentionally
// conservative — only the exact, canonical prefixes from common
// workflow tooling (git-interpret-trailers, GitHub web UI, common
// Claude/Anthropic attribution lines) qualify. A prose line that
// happens to contain "Reviewed: foo" without the canonical capitalised
// form and trailing colon is kept.
func isTrailerLine(line string) bool {
	switch {
	case strings.HasPrefix(line, "Signed-off-by:"),
		strings.HasPrefix(line, "Co-authored-by:"),
		strings.HasPrefix(line, "Acked-by:"),
		strings.HasPrefix(line, "Reviewed-by:"),
		strings.HasPrefix(line, "Tested-by:"),
		strings.HasPrefix(line, "Reported-by:"),
		strings.HasPrefix(line, "Suggested-by:"),
		strings.HasPrefix(line, "Cc:"):
		return true
	}
	return strings.HasPrefix(line, "Generated with ")
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
	for line := range strings.SplitSeq(string(stdout), "\n") {
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
	if _, tail, ok := strings.Cut(url, "github.com"); ok {
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
