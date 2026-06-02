package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/fsnotify/fsnotify"
)

// TestIsRelevantEvent locks down the watch filter: only parser-
// supported extensions on Create/Write/Remove/Rename ops should
// trigger a rebuild. Chmod events (editors shuffling permissions on
// save) and non-source files must be ignored — without the filter
// every npm install would cascade into a rebuild storm.
func TestIsRelevantEvent(t *testing.T) {
	cases := []struct {
		name string
		ev   fsnotify.Event
		want bool
	}{
		{"go write", fsnotify.Event{Name: "a/b.go", Op: fsnotify.Write}, true},
		{"ts create", fsnotify.Event{Name: "src/x.ts", Op: fsnotify.Create}, true},
		{"tsx write", fsnotify.Event{Name: "src/x.tsx", Op: fsnotify.Write}, true},
		{"sol remove", fsnotify.Event{Name: "contracts/V.sol", Op: fsnotify.Remove}, true},
		{"proto rename", fsnotify.Event{Name: "api.proto", Op: fsnotify.Rename}, true},
		{"chmod ignored", fsnotify.Event{Name: "a.go", Op: fsnotify.Chmod}, false},
		{"md ignored", fsnotify.Event{Name: "README.md", Op: fsnotify.Write}, false},
		{"json ignored", fsnotify.Event{Name: "package.json", Op: fsnotify.Write}, false},
		{"case-insensitive ext", fsnotify.Event{Name: "A.GO", Op: fsnotify.Write}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRelevantEvent(tc.ev); got != tc.want {
				t.Errorf("isRelevantEvent(%+v) = %v, want %v", tc.ev, got, tc.want)
			}
		})
	}
}

// TestIsSkippedDir documents the curated noise allowlist. We DO want
// to recurse into e.g. .github (if the user keeps Go in there for
// whatever reason) — only the well-known hot dirs are off-limits.
func TestIsSkippedDir(t *testing.T) {
	skipped := []string{".git", ".ckg-data", "node_modules", ".next", "out",
		"playwright-report", "test-results"}
	for _, d := range skipped {
		if !isSkippedDir(d) {
			t.Errorf("isSkippedDir(%q) = false, want true", d)
		}
	}
	kept := []string{"internal", "pkg", "cmd", ".github", "docs", "testdata"}
	for _, d := range kept {
		if isSkippedDir(d) {
			t.Errorf("isSkippedDir(%q) = true, want false", d)
		}
	}
}

// TestAddWatchedDirs walks a tiny temp tree and confirms the
// recursion skip rules in concrete: depth-2 source dirs get added,
// node_modules at any depth gets pruned.
func TestAddWatchedDirs(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{
		"pkg/a",
		"pkg/b/sub",
		"internal/x",
		"node_modules/deep/chain",
		".git/hooks",
	} {
		if err := mkdirAll(filepath.Join(root, p)); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Skipf("fsnotify unavailable: %v", err)
	}
	defer func() { _ = w.Close() }()

	if err := addWatchedDirs(w, root); err != nil {
		t.Fatalf("addWatchedDirs: %v", err)
	}
	// Compare the watched set against the expected dirs. WatchList()
	// returns absolute paths in fsnotify's canonical form; rel-path
	// them against root for stable assertions.
	got := map[string]bool{}
	for _, p := range w.WatchList() {
		rel, _ := filepath.Rel(root, p)
		got[filepath.ToSlash(rel)] = true
	}
	wantAdded := []string{".", "pkg", "pkg/a", "pkg/b", "pkg/b/sub", "internal", "internal/x"}
	for _, w := range wantAdded {
		if !got[w] {
			t.Errorf("expected %q in watch list; got keys %v", w, mapKeysAlphabetical(got))
		}
	}
	wantSkipped := []string{"node_modules", "node_modules/deep", "node_modules/deep/chain",
		".git", ".git/hooks"}
	for _, s := range wantSkipped {
		if got[s] {
			t.Errorf("did not expect %q in watch list", s)
		}
	}
}

// mkdirAll wraps os.MkdirAll with the 0o755 perm bits the test tree
// expects. Existing-dir is a no-op.
func mkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

// mapKeysAlphabetical returns map keys in sorted order so test
// failure messages stay reproducible.
func mapKeysAlphabetical(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
