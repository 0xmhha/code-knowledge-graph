// Package temporal — hunks.go extracts unified-diff hunks from `git log -p`,
// the foundation for the CKS G6 Hunk-graph (schema 1.8 H1 stage). Each
// HunkInfo is one contiguous block of changed lines in one file in one
// commit; the buildpipe layer turns these into NodeHunk rows + has_hunk /
// adjacent edges, with the gzip-compressed unified-diff text persisted as
// a blob keyed by the Hunk's node ID.
//
// Why a fresh collector instead of extending LoadHistory: LoadHistory uses
// `git log --raw` to enumerate (commit, file) pairs — it never sees the
// patch body. The hunk pass needs `git log -p` to materialise diff text;
// the two streams have incompatible parse states. Keeping them separate
// lets each pass stay simple and lets the build pipeline run them
// concurrently if the budget ever requires it.
//
// Repos that aren't git checkouts return nil + nil error so callers
// degrade gracefully (mirrors LoadHistory's contract).
package temporal

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// hunkCommitsDefault bounds the commit walk for cold-rebuild hunk
// extraction. 200 most-recent reachable commits is the design §3.7
// budget for the self-graph (~178 commits, ~700 hunks, ~700KB blob mass).
// Callers can override via LoadHunks(maxCommits=...).
const hunkCommitsDefault = 200

// HunkInfo is one unified-diff hunk extracted from a single commit. The
// fields mirror the @@ -OldStart,OldLines +NewStart,NewLines @@ header:
//
//   - OldStart/OldLines: pre-image range in the parent file. Zero/zero
//     when the file is newly added in this commit.
//   - NewStart/NewLines: post-image range in the file at this commit.
//     Zero/zero when the file is deleted in this commit.
//   - Added/Removed: literal '+' / '-' line counts inside the hunk body
//     (excluding the `--- a/...` / `+++ b/...` file headers and the
//     `\ No newline at end of file` marker).
//   - Patch: raw bytes of the hunk INCLUDING the `@@` header line and a
//     trailing newline. NOT gzipped — the buildpipe layer applies the
//     §11.6 64KB truncation and gzip compression before persisting.
//   - Index: 0-based per-(commit, file) hunk position. Stable within
//     one parse — used as the third coordinate of the Hunk node ID so
//     multiple hunks per commit-file pair don't collide.
//
// SHA is the full 40-char hex commit ID. FilePath is the post-image
// (b/) side of the `diff --git` header — for renames under --no-renames
// the file appears as a deletion of the old path + an addition of the
// new path, so SHA × FilePath × Index uniquely identifies any hunk.
type HunkInfo struct {
	SHA      string
	FilePath string
	Index    int
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	Added    int
	Removed  int
	Binary   bool
	Patch    []byte
}

// LoadHunks runs `git log -p` over the most-recent maxCommits HEAD-reachable
// commits and parses the patch stream into HunkInfo records. maxCommits<=0
// uses hunkCommitsDefault.
//
// Returns (nil, nil) if repoRoot isn't a git checkout — same graceful-degrade
// contract as LoadHistory. Other git failures bubble up as errors.
//
// Why --no-renames: matches LoadHistory's flag set so changed_in (file-level)
// and has_hunk (per-hunk) agree on the path identity. Renames appear as
// delete+add hunk pairs; the H2 modifies-edge pass that lands later in this
// schema bump can re-link them via AST overlap if needed.
//
// Why --no-merges: merge commits' patches are noisy (they show the resolution
// against one parent, not the actual code change), and the few cases where
// a merge introduces real code surface elsewhere via the merged branch's
// own commits. Excluding them halves the hunk count on heavily-rebased
// branches without losing signal.
func LoadHunks(repoRoot string, maxCommits int) ([]HunkInfo, error) {
	if maxCommits <= 0 {
		maxCommits = hunkCommitsDefault
	}
	if !isGitCheckout(repoRoot) {
		return nil, nil
	}
	cmd := exec.Command("git", "-C", repoRoot,
		"log", "--no-renames", "--no-color", "--no-merges",
		"--pretty=format:COMMIT %H %at %s",
		"-p", "--unified=3",
		fmt.Sprintf("-n%d", maxCommits),
		"HEAD", "--", ".")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("git log -p stdout pipe: %w", err)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git log -p start: %w", err)
	}
	hunks, parseErr := parseHunkStream(stdout)
	// Drain pipe + wait so the subprocess exits cleanly even on parse errors.
	_, _ = io.Copy(io.Discard, stdout)
	if waitErr := cmd.Wait(); waitErr != nil && parseErr == nil {
		return nil, fmt.Errorf("git log -p wait: %w (stderr: %s)",
			waitErr, strings.TrimSpace(stderrBuf.String()))
	}
	return hunks, parseErr
}

// parseHunkStream consumes the byte stream from `git log -p
// --pretty=format:COMMIT %H %at %s` and emits one HunkInfo per @@ block
// (plus one zero-content HunkInfo per binary diff for traceability —
// hunk-graph.md §3.6).
//
// State machine (per stream byte):
//
//	START   — scanning for "COMMIT <sha> <ts> <subject>"
//	IN_DIFF — past `diff --git`, before `@@`. Reads optional file headers
//	          (--- / +++ / index / new file mode / Binary files differ).
//	IN_HUNK — after `@@`. Body lines (' '/'+'/'-'/'\') append to Patch
//	          and increment Added/Removed counts.
//
// The transitions are:
//
//	START   ─[COMMIT]─→ START          (sha set, file/idx reset)
//	START   ─[diff --git]─→ IN_DIFF    (file set, idx for this commit reset on first hit)
//	IN_DIFF ─[@@]─→ IN_HUNK            (HunkInfo materialised, header copied to Patch)
//	IN_HUNK ─[@@]─→ IN_HUNK            (flush previous, start new — same file)
//	IN_HUNK ─[diff --git]─→ IN_DIFF    (flush, new file)
//	IN_HUNK ─[COMMIT]─→ START          (flush, new commit)
//	IN_DIFF ─[Binary files]─→ START    (emit zero-content HunkInfo)
//
// Lines that don't trigger a transition while IN_HUNK accumulate into the
// current hunk's Patch buffer.
func parseHunkStream(r io.Reader) ([]HunkInfo, error) {
	scanner := bufio.NewScanner(r)
	// Default buffer is 64KB which truncates merge-commit subjects and very
	// long context-free hunks (e.g. minified vendor blobs). 8MB cap matches
	// the design §11.6 cap × ~128 — enough headroom for any realistic
	// single line a developer hand-wrote.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var (
		out           []HunkInfo
		currentSHA    string
		currentFile   string
		hunkIdxByFile map[string]int
		current       *HunkInfo
		buf           bytes.Buffer
	)

	resetCommit := func(sha string) {
		currentSHA = sha
		currentFile = ""
		hunkIdxByFile = map[string]int{}
	}
	flush := func() {
		if current == nil {
			return
		}
		current.Patch = append([]byte(nil), buf.Bytes()...)
		out = append(out, *current)
		current = nil
		buf.Reset()
	}

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "COMMIT "):
			flush()
			info, ok := parseCommitHeader(line)
			if !ok {
				resetCommit("")
				continue
			}
			resetCommit(info.SHA)
		case strings.HasPrefix(line, "diff --git "):
			flush()
			currentFile = parseDiffGitPath(line)
		case strings.HasPrefix(line, "Binary files "):
			// Binary diffs have no @@; record a zero-content Hunk for
			// traceability so downstream "what touched this file" queries
			// still see them. modifies-edge emission (H2) ignores binary
			// hunks because there's no line interval to overlap.
			if currentSHA == "" || currentFile == "" {
				continue
			}
			idx := hunkIdxByFile[currentFile]
			hunkIdxByFile[currentFile] = idx + 1
			out = append(out, HunkInfo{
				SHA: currentSHA, FilePath: currentFile, Index: idx,
				Binary: true,
			})
		case strings.HasPrefix(line, "@@ "):
			flush()
			oldS, oldL, newS, newL, ok := parseHunkHeader(line)
			if !ok || currentSHA == "" || currentFile == "" {
				continue
			}
			idx := hunkIdxByFile[currentFile]
			hunkIdxByFile[currentFile] = idx + 1
			current = &HunkInfo{
				SHA: currentSHA, FilePath: currentFile, Index: idx,
				OldStart: oldS, OldLines: oldL, NewStart: newS, NewLines: newL,
			}
			buf.WriteString(line)
			buf.WriteByte('\n')
		default:
			if current == nil {
				continue
			}
			// Hunk body line: ' ' (context), '+' (added), '-' (removed),
			// '\' (no-newline marker). Empty lines are legal context.
			if len(line) > 0 {
				switch line[0] {
				case '+':
					current.Added++
				case '-':
					current.Removed++
				}
			}
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return out, fmt.Errorf("git log -p scan: %w", err)
	}
	return out, nil
}

// parseDiffGitPath extracts the post-image (b-side) path from a
// `diff --git a/<path> b/<path>` line. Returns "" on malformed input.
//
// Two shapes git emits:
//   - unquoted: `diff --git a/path b/path` — the common case for ASCII
//     paths without spaces. Search for the last ` b/` token.
//   - quoted:   `diff --git "a/with space" "b/with space"` — git auto-
//     quotes when the path needs escaping (spaces, control chars, etc.).
//     Search for ` "b/` and strip the trailing quote.
func parseDiffGitPath(line string) string {
	rest := strings.TrimPrefix(line, "diff --git ")
	if i := strings.Index(rest, ` "b/`); i >= 0 {
		path := rest[i+4:]
		return strings.TrimSuffix(path, `"`)
	}
	idx := strings.LastIndex(rest, " b/")
	if idx < 0 {
		return ""
	}
	return rest[idx+3:]
}

// parseHunkHeader parses `@@ -OS,OL +NS,NL @@ optional context`. The
// length component is optional (defaults to 1 when omitted, e.g.
// `@@ -42 +42 @@`). Returns (0,0,0,0,false) on any malformed input.
func parseHunkHeader(line string) (oldStart, oldLines, newStart, newLines int, ok bool) {
	if !strings.HasPrefix(line, "@@ ") {
		return 0, 0, 0, 0, false
	}
	rest := line[3:]
	end := strings.Index(rest, " @@")
	if end < 0 {
		return 0, 0, 0, 0, false
	}
	spec := rest[:end]
	parts := strings.Fields(spec)
	if len(parts) != 2 {
		return 0, 0, 0, 0, false
	}
	oldS, oldL, ok1 := parseRange(strings.TrimPrefix(parts[0], "-"))
	newS, newL, ok2 := parseRange(strings.TrimPrefix(parts[1], "+"))
	if !ok1 || !ok2 {
		return 0, 0, 0, 0, false
	}
	return oldS, oldL, newS, newL, true
}

// parseRange parses "<start>" or "<start>,<lines>". Returns (0,0,false)
// on any non-integer component.
func parseRange(s string) (start, lines int, ok bool) {
	if s == "" {
		return 0, 0, false
	}
	idx := strings.IndexByte(s, ',')
	if idx < 0 {
		v, err := strconv.Atoi(s)
		if err != nil {
			return 0, 0, false
		}
		return v, 1, true
	}
	st, err := strconv.Atoi(s[:idx])
	if err != nil {
		return 0, 0, false
	}
	ln, err := strconv.Atoi(s[idx+1:])
	if err != nil {
		return 0, 0, false
	}
	return st, ln, true
}
