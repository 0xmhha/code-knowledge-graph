package filterlist

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllow_NilFilter(t *testing.T) {
	var f *FilterList
	if !f.Allow("any/path.go") {
		t.Error("nil filter must allow everything")
	}
}

func TestAllow_IncludeOnly(t *testing.T) {
	f := &FilterList{Include: []string{"internal/**/*.go"}}
	cases := map[string]bool{
		"internal/parse/golang/parser.go": true,
		"internal/buildpipe/pipeline.go":  true,
		"cmd/ckg/build.go":                false,
		"docs/README.md":                  false,
	}
	for path, want := range cases {
		if got := f.Allow(path); got != want {
			t.Errorf("Allow(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestAllow_ExcludeOverridesInclude(t *testing.T) {
	f := &FilterList{
		Include: []string{"internal/**/*.go"},
		Exclude: []string{"**/testdata/**", "**/*_test.go"},
	}
	cases := map[string]bool{
		"internal/parse/golang/parser.go":          true,
		"internal/parse/golang/parser_test.go":     false,
		"internal/parse/golang/testdata/sample.go": false,
		"internal/parse/golang/testdata/x/y/z.go":  false,
		"cmd/ckg/build.go":                         false,
	}
	for path, want := range cases {
		if got := f.Allow(path); got != want {
			t.Errorf("Allow(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestAllow_EmptyIncludeAllAllowed(t *testing.T) {
	f := &FilterList{Exclude: []string{"vendor/**"}}
	if !f.Allow("internal/foo.go") {
		t.Error("empty include should accept non-excluded paths")
	}
	if f.Allow("vendor/x/y.go") {
		t.Error("excluded path leaked through")
	}
}

func TestLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "files.json")
	if err := os.WriteFile(path, []byte(`{"include":["**/*.go"],"exclude":["**/testdata/**"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Include) != 1 || f.Include[0] != "**/*.go" {
		t.Errorf("Include not parsed: %+v", f.Include)
	}
	if !f.Allow("internal/foo.go") {
		t.Error("path should match include")
	}
	if f.Allow("internal/testdata/foo.go") {
		t.Error("testdata path should be excluded")
	}
}

func TestLoad_EmptyPath(t *testing.T) {
	f, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if f != nil {
		t.Errorf("empty path should return nil filter, got %+v", f)
	}
}

func TestFilterPaths_PreservesOrder(t *testing.T) {
	f := &FilterList{Exclude: []string{"**/testdata/**"}}
	in := []string{
		"a/x.go",
		"a/testdata/y.go",
		"b/z.go",
	}
	out := f.FilterPaths(in)
	if len(out) != 2 || out[0] != "a/x.go" || out[1] != "b/z.go" {
		t.Errorf("FilterPaths produced %v", out)
	}
}
