package detect_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/detect"
)

func TestCKGIgnoreMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".ckgignore"),
		[]byte("vendor/\nnode_modules/\n*.generated.*\nbuild/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := detect.LoadCKGIgnore(dir)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		rel  string
		want bool
	}{
		{"vendor/x.go", true},
		{"vendor", true},
		{"src/foo.generated.ts", true},
		{"build/main.js", true},
		{"src/foo.go", false},
		{"README.md", false},
		// Nested directory patterns: a "dir/" entry must match the dir at
		// any depth, not just the top level. Regression guard for the
		// node_modules-not-ignored bug that polluted the self-graph with
		// 270k vendor TS nodes.
		{"web/viewer-next/node_modules/foo.ts", true},
		{"web/viewer-next/node_modules", true},
		{"a/b/c/vendor/lib.go", true},
		// Pattern as a substring should NOT match (segment boundaries).
		{"src/node_modulesx/foo.ts", false},
		{"src/xnode_modules/foo.ts", false},
	}
	for _, tc := range cases {
		if got := c.Match(tc.rel); got != tc.want {
			t.Errorf("Match(%q) = %v, want %v", tc.rel, got, tc.want)
		}
	}
}
