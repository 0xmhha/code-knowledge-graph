// Package temporal extracts git-history derived facts (commits + per-file
// touch lists) used to emit CKS G6 Temporal edges (`changed_in`, `blame`)
// in the build pipeline.
//
// V0 scope (E4): single `git log --raw --no-renames` invocation per build,
// streamed and parsed into a per-file commit list. Line-level blame is
// deferred (G6 Phase 2). Repos that aren't git checkouts return an empty
// FileHistory + nil error so callers degrade gracefully.
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

// CommitInfo describes a single git commit. Times are unix seconds (the git
// `%at` author-time format) so they collate with the staleness fingerprint
// already used by the manifest.
type CommitInfo struct {
	SHA       string // full 40-char hex
	Timestamp int64  // unix seconds
	Subject   string // first line of message (already trimmed of trailing \n)
}

// FileHistory is the parsed result of one `git log --raw` invocation:
//
//   - Files: repo-rooted slash-form path → list of commit SHAs that touched
//     that file, most-recent-first, capped at the per-file limit.
//   - Commits: SHA → CommitInfo for every commit referenced from Files.
//
// The maps are nil-safe (callers can range over nil maps).
type FileHistory struct {
	Files   map[string][]string
	Commits map[string]CommitInfo
}

// LoadHistory runs `git -C repoRoot log --raw --no-renames --pretty=format:'COMMIT %H %at %s' HEAD -- .`
// and parses the output into a FileHistory. maxPerFile bounds per-file
// commit count (default 10 if maxPerFile <= 0).
//
// Returns an empty FileHistory + nil error if repoRoot is not a git checkout
// (graceful degrade — temporal edges simply won't be emitted). Other git
// failures bubble up as errors so the caller can log them.
//
// Performance: a single git invocation streams the entire repo history.
// For 2000-file corpora with ~200 commits, output is on the order of 1MB
// and parses in <1s. We deliberately bound per-file count rather than
// passing `-n` to git, because `-n` caps TOTAL commits in the stream
// (not per file) and the goal is "10 most recent per file" regardless of
// how many other files were touched in those commits.
func LoadHistory(repoRoot string, maxPerFile int) (FileHistory, error) {
	if maxPerFile <= 0 {
		maxPerFile = 10
	}
	if !isGitCheckout(repoRoot) {
		return FileHistory{Files: map[string][]string{}, Commits: map[string]CommitInfo{}}, nil
	}
	cmd := exec.Command("git", "-C", repoRoot,
		"log", "--raw", "--no-renames", "--no-color",
		"--pretty=format:COMMIT %H %at %s",
		"HEAD", "--", ".")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return FileHistory{}, fmt.Errorf("git log stdout pipe: %w", err)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Start(); err != nil {
		return FileHistory{}, fmt.Errorf("git log start: %w", err)
	}
	hist, parseErr := parseGitLog(stdout, maxPerFile)
	// Drain pipe + wait so the subprocess exits cleanly even on parse errors.
	_, _ = io.Copy(io.Discard, stdout)
	if waitErr := cmd.Wait(); waitErr != nil && parseErr == nil {
		return FileHistory{}, fmt.Errorf("git log wait: %w (stderr: %s)",
			waitErr, strings.TrimSpace(stderrBuf.String()))
	}
	return hist, parseErr
}

// isGitCheckout returns true when `git -C dir rev-parse --show-toplevel`
// succeeds. Mirrors gitRepoRel in buildpipe but avoids the import cycle.
func isGitCheckout(dir string) bool {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// parseGitLog consumes the stream produced by `git log --raw --no-renames
// --pretty=format:COMMIT %H %at %s` and builds a FileHistory bounded by
// maxPerFile commits per file.
//
// Format (one block per commit):
//
//	COMMIT <full-sha> <unix-seconds> <subject>
//	:<old-mode> <new-mode> <old-sha> <new-sha> <status>\t<path>
//	:... (zero or more file lines)
//	(blank line between commits)
//
// We stream-cap per file: once a file has maxPerFile commits, further
// occurrences are dropped (cheaper than collecting then truncating).
func parseGitLog(r io.Reader, maxPerFile int) (FileHistory, error) {
	hist := FileHistory{
		Files:   map[string][]string{},
		Commits: map[string]CommitInfo{},
	}
	scanner := bufio.NewScanner(r)
	// `--raw` lines are short, but commit subjects can run long. Bump the
	// per-line buffer so we don't truncate a 100KB merge-commit message.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var currentSHA string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "COMMIT ") {
			info, ok := parseCommitHeader(line)
			if !ok {
				continue // ignore malformed header rather than aborting the whole build
			}
			currentSHA = info.SHA
			hist.Commits[currentSHA] = info
			continue
		}
		if !strings.HasPrefix(line, ":") || currentSHA == "" {
			continue
		}
		path, ok := parseRawDiffPath(line)
		if !ok || path == "" {
			continue
		}
		// Most-recent-first ordering: git log emits commits in
		// reverse-chronological order, so the first time we see a file
		// is its most recent touch. Cap reached → drop silently.
		existing := hist.Files[path]
		if len(existing) >= maxPerFile {
			continue
		}
		// Avoid duplicate appends if the same commit lists the same path
		// twice (rare but defensible — e.g. mode changes alongside content).
		if len(existing) > 0 && existing[len(existing)-1] == currentSHA {
			continue
		}
		hist.Files[path] = append(existing, currentSHA)
	}
	if err := scanner.Err(); err != nil {
		return hist, fmt.Errorf("git log scan: %w", err)
	}
	return hist, nil
}

// parseCommitHeader parses one `COMMIT <sha> <ts> <subject>` line.
// Returns (info, true) on success, (zero, false) on any malformed input.
func parseCommitHeader(line string) (CommitInfo, bool) {
	rest := strings.TrimPrefix(line, "COMMIT ")
	// SHA: up to first space.
	sp := strings.IndexByte(rest, ' ')
	if sp <= 0 {
		return CommitInfo{}, false
	}
	sha := rest[:sp]
	if len(sha) != 40 {
		return CommitInfo{}, false
	}
	rest = rest[sp+1:]
	// Timestamp: up to next space.
	sp = strings.IndexByte(rest, ' ')
	if sp <= 0 {
		// Subject can legally be empty; in that case we have only "<sha> <ts>".
		ts, err := strconv.ParseInt(rest, 10, 64)
		if err != nil {
			return CommitInfo{}, false
		}
		return CommitInfo{SHA: sha, Timestamp: ts}, true
	}
	ts, err := strconv.ParseInt(rest[:sp], 10, 64)
	if err != nil {
		return CommitInfo{}, false
	}
	subject := rest[sp+1:]
	if len(subject) > 200 {
		subject = subject[:200]
	}
	return CommitInfo{SHA: sha, Timestamp: ts, Subject: subject}, true
}

// parseRawDiffPath extracts the file path from a `--raw` diff line:
//
//	:100644 100644 abc1234 def5678 M\t<path>
//
// We split on the literal TAB rather than on spaces because git always
// uses TAB as the field separator before the path — paths with spaces
// or unicode are preserved verbatim. Returns ("", false) when the line
// shape doesn't match.
func parseRawDiffPath(line string) (string, bool) {
	tab := strings.IndexByte(line, '\t')
	if tab < 0 || tab+1 >= len(line) {
		return "", false
	}
	return line[tab+1:], true
}
