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
		[]byte("vendor/\n*.generated.*\nbuild/\n"), 0o644); err != nil {
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
	}
	for _, tc := range cases {
		if got := c.Match(tc.rel); got != tc.want {
			t.Errorf("Match(%q) = %v, want %v", tc.rel, got, tc.want)
		}
	}
}
