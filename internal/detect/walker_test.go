package detect_test

import (
	"sort"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/detect"
)

func TestWalkClassifies(t *testing.T) {
	root := "testdata/sample"
	got, err := detect.Walk(root)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	sortFiles := func(s []string) { sort.Strings(s) }
	sortFiles(got.Go)
	sortFiles(got.TS)
	sortFiles(got.Sol)

	if want := []string{"a.go"}; !equal(got.Go, want) {
		t.Errorf("Go = %v, want %v (vendor/d.go must be ignored)", got.Go, want)
	}
	if want := []string{"b.ts"}; !equal(got.TS, want) {
		t.Errorf("TS = %v, want %v (foo.generated.ts must be ignored)", got.TS, want)
	}
	if want := []string{"c.sol"}; !equal(got.Sol, want) {
		t.Errorf("Sol = %v, want %v", got.Sol, want)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
