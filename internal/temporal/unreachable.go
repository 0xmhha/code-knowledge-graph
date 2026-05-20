// Package temporal — unreachable.go collects hunks from commits that
// exist in the local git object store but are NOT reachable from HEAD.
// Those commits live in two surfaces:
//
//  1. `git reflog --all --pretty=%H`        — local HEAD/branch movement
//     records. Captures force-pushed-away SHAs that haven't been GC'd
//     yet (default 90 days). Misses commits that landed via fetch but
//     were never moved into a ref's history.
//
//  2. `git fsck --no-reflogs --unreachable` — dangling objects that
//     no ref or reflog points at. Catches the second category above
//     and any commit explicitly excluded from a fetch's tip walk.
//
// Together: a near-complete view of the local object store's
// "history humans rolled back". Used to populate the schema-1.8
// §11.3 "AMBIGUOUS" hunk class — see docs/design/hunk-graph.md
// for the storage / retrieval / recovery layering.
//
// Distinct from LoadHunks: that pass walks `git log HEAD --` only and
// produces the EXTRACTED-confidence baseline. This pass is additive —
// the caller merges the two result sets into one Commit/Hunk emission.
package temporal

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// unreachableCommitsDefault caps the unreachable walk so a repo with
// thousands of GC-pending commits doesn't blow up the build. 100 is
// generous for the recovery use case (the user just needs the most
// recent overwrite that an agent might have wiped) while staying well
// under the worst-case fsck output size.
const unreachableCommitsDefault = 100

// LoadUnreachableHunks returns commits + hunks for SHAs reachable via
// reflog or fsck-unreachable but NOT from HEAD. maxCommits ≤ 0 uses
// unreachableCommitsDefault.
//
// Returns (nil, nil, nil) for non-git directories — same graceful
// degrade contract as LoadHunks. Other git failures bubble up.
//
// Performance: reflog + fsck typically run in < 1s on repos with
// 10K+ commits. The per-SHA `git show` is ~50ms each, so a 100-commit
// cap keeps the worst-case under 5s on commodity hardware.
func LoadUnreachableHunks(repoRoot string, maxCommits int) ([]CommitInfo, []HunkInfo, error) {
	if maxCommits <= 0 {
		maxCommits = unreachableCommitsDefault
	}
	if !isGitCheckout(repoRoot) {
		return nil, nil, nil
	}
	reachable, err := readReachableSet(repoRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("rev-list HEAD: %w", err)
	}
	candidates := map[string]struct{}{}
	for sha := range readReflogSHAs(repoRoot) {
		candidates[sha] = struct{}{}
	}
	for sha := range readFsckUnreachable(repoRoot) {
		candidates[sha] = struct{}{}
	}
	// Drop everything HEAD already reaches — that's the EXTRACTED set
	// LoadHunks already covers.
	var unreachable []string
	for sha := range candidates {
		if _, hit := reachable[sha]; hit {
			continue
		}
		unreachable = append(unreachable, sha)
	}
	if len(unreachable) == 0 {
		return nil, nil, nil
	}
	if len(unreachable) > maxCommits {
		// Stable order so cap doesn't pick a different subset on each
		// run (reflog is naturally most-recent-first; alphabetic on
		// the SHA string preserves determinism).
		sortStrings(unreachable)
		unreachable = unreachable[:maxCommits]
	}

	var commits []CommitInfo
	var hunks []HunkInfo
	for _, sha := range unreachable {
		ci, hs, err := loadCommitWithHunks(repoRoot, sha)
		if err != nil {
			// Skip individual failures — a SHA might fail because of
			// shallow-clone truncation or a transient git error. The
			// rest of the unreachable set is still useful.
			continue
		}
		commits = append(commits, ci)
		hunks = append(hunks, hs...)
	}
	return commits, hunks, nil
}

// readReachableSet builds {sha → present} for every commit reachable
// from HEAD. Used to subtract from the (reflog ∪ fsck) candidate set.
func readReachableSet(repoRoot string) (map[string]struct{}, error) {
	cmd := exec.Command("git", "-C", repoRoot, "rev-list", "HEAD", "--")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, 4096)
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		s := strings.TrimSpace(string(line))
		if len(s) == 40 {
			set[s] = struct{}{}
		}
	}
	return set, nil
}

// readReflogSHAs enumerates commit SHAs from the local reflog. Returns
// an empty map (not an error) for repos with no reflog — fresh
// checkouts and bare repos commonly have none.
func readReflogSHAs(repoRoot string) map[string]struct{} {
	cmd := exec.Command("git", "-C", repoRoot, "reflog", "--all", "--pretty=%H")
	out, err := cmd.Output()
	if err != nil {
		return map[string]struct{}{}
	}
	set := map[string]struct{}{}
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		s := strings.TrimSpace(string(line))
		if len(s) == 40 {
			set[s] = struct{}{}
		}
	}
	return set
}

// readFsckUnreachable parses `git fsck --no-reflogs --unreachable`
// output, extracting commit SHAs from `unreachable commit <sha>` lines.
// Skips blob/tree dangling entries — only the commit-level SHAs are
// useful for hunk extraction. Returns empty on any error so the caller
// keeps reflog candidates.
func readFsckUnreachable(repoRoot string) map[string]struct{} {
	// fsck writes its findings to stderr, not stdout. Combine streams
	// so we capture both regardless of git version.
	cmd := exec.Command("git", "-C", repoRoot, "fsck", "--no-reflogs", "--unreachable")
	out, _ := cmd.CombinedOutput()
	set := map[string]struct{}{}
	for _, line := range bytes.Split(out, []byte{'\n'}) {
		fields := strings.Fields(string(line))
		// Lines look like: "unreachable commit <40-char-sha>"
		// Older gits also emit "dangling commit <sha>" — fold both.
		if len(fields) == 3 && (fields[0] == "unreachable" || fields[0] == "dangling") &&
			fields[1] == "commit" && len(fields[2]) == 40 {
			set[fields[2]] = struct{}{}
		}
	}
	return set
}

// loadCommitWithHunks runs `git show <sha>` and parses the result via
// parseHunkStream — same parser the HEAD-walk uses, so binary-files /
// rename / mode-only / multi-hunk handling is identical between the
// EXTRACTED and AMBIGUOUS paths.
func loadCommitWithHunks(repoRoot, sha string) (CommitInfo, []HunkInfo, error) {
	cmd := exec.Command("git", "-C", repoRoot,
		"show", "--no-color", "--no-renames",
		"--pretty=format:COMMIT %H %at %s",
		"--unified=3", sha)
	out, err := cmd.Output()
	if err != nil {
		return CommitInfo{}, nil, fmt.Errorf("git show %s: %w", sha, err)
	}
	hunks, err := parseHunkStream(bytes.NewReader(out))
	if err != nil {
		return CommitInfo{}, nil, fmt.Errorf("parse %s: %w", sha, err)
	}
	// Extract the COMMIT header line for the timestamp + subject. The
	// stream parser uses parseCommitHeader internally for the same job —
	// we re-scan here because the parser doesn't expose the parsed info
	// at the per-stream-segment level (it's stateful inside the hunk
	// loop).
	var info CommitInfo
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "COMMIT ") {
			if ci, ok := parseCommitHeader(line); ok {
				info = ci
			}
			break
		}
	}
	if info.SHA == "" {
		// Fallback: at least populate the SHA so downstream Commit-node
		// emission works. Timestamp/Subject stay zero — viewers can
		// still display a "(unreachable)" placeholder.
		info.SHA = sha
	}
	return info, hunks, nil
}

// sortStrings is a small std-lib-free in-place sort to avoid pulling
// "sort" into this file's import set. Used once per build for the
// unreachable cap; an O(n²) bubble is fine at n=100.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
