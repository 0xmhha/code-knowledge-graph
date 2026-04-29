package detect_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/detect"
)

// writeFile materialises a single file under root, creating parents as needed.
func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// writeGoMod stamps a minimal go.mod under root/sub.
func writeGoMod(t *testing.T, root, sub, mod string) {
	t.Helper()
	writeFile(t, root, filepath.Join(sub, "go.mod"), "module "+mod+"\n\ngo 1.22\n")
}

// TestGoFiles_BuildConstraintExcluded verifies that files gated behind a
// build constraint no toolchain satisfies (`//go:build never`) are excluded
// from the result, while regular files remain.
func TestGoFiles_BuildConstraintExcluded(t *testing.T) {
	root := t.TempDir()
	writeGoMod(t, root, "", "example.test/m")
	writeFile(t, root, "main.go", "package main\nfunc main(){}\n")
	writeFile(t, root, "tagged_only.go", "//go:build never\n\npackage main\n")

	got, err := detect.GoFiles(root)
	if err != nil {
		t.Fatalf("GoFiles: %v", err)
	}
	want := []string{"main.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestGoFiles_BuildIgnoreExcluded ensures `//go:build ignore` build-tool
// files are not picked up — these are conventionally `go run`-only and are
// not part of any compiled package.
func TestGoFiles_BuildIgnoreExcluded(t *testing.T) {
	root := t.TempDir()
	writeGoMod(t, root, "", "example.test/m")
	writeFile(t, root, "main.go", "package main\nfunc main(){}\n")
	writeFile(t, root, "tools.go", "//go:build ignore\n\npackage main\n")

	got, err := detect.GoFiles(root)
	if err != nil {
		t.Fatalf("GoFiles: %v", err)
	}
	want := []string{"main.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestGoFiles_NestedModule confirms that when srcRoot has no go.mod itself
// but a child directory does, files under that child are discovered with
// their relpath measured from srcRoot.
func TestGoFiles_NestedModule(t *testing.T) {
	root := t.TempDir()
	writeGoMod(t, root, "sub", "example.test/sub")
	writeFile(t, root, "sub/main.go", "package main\nfunc main(){}\n")

	got, err := detect.GoFiles(root)
	if err != nil {
		t.Fatalf("GoFiles: %v", err)
	}
	want := []string{"sub/main.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestGoFiles_NoGoMod verifies the empty-result, no-error contract for a
// srcRoot that contains no go.mod anywhere — TS/Sol-only projects must not
// trip the Go discovery path.
func TestGoFiles_NoGoMod(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "README.md", "no go here\n")
	writeFile(t, root, "src/app.ts", "export const x = 1\n")

	got, err := detect.GoFiles(root)
	if err != nil {
		t.Fatalf("GoFiles: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

// TestGoFiles_VendorSkipped checks that the walker does not descend into
// vendor/ directories, even if they contain a go.mod stamp (which some
// vendoring tools leave behind).
func TestGoFiles_VendorSkipped(t *testing.T) {
	root := t.TempDir()
	writeGoMod(t, root, "", "example.test/m")
	writeFile(t, root, "main.go", "package main\nfunc main(){}\n")
	writeGoMod(t, root, "vendor/foo", "example.test/vendor-foo")
	writeFile(t, root, "vendor/foo/foo.go", "package foo\n")

	got, err := detect.GoFiles(root)
	if err != nil {
		t.Fatalf("GoFiles: %v", err)
	}
	want := []string{"main.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestGoFiles_TestdataSkipped confirms that nested go.mod under testdata/ is
// ignored (matching `go list ./...` semantics for the parent module).
func TestGoFiles_TestdataSkipped(t *testing.T) {
	root := t.TempDir()
	writeGoMod(t, root, "", "example.test/m")
	writeFile(t, root, "pkg/main.go", "package pkg\nfunc F(){}\n")
	writeGoMod(t, root, "testdata/fixture", "example.test/fixture")
	writeFile(t, root, "testdata/fixture/main.go", "package main\nfunc main(){}\n")

	got, err := detect.GoFiles(root)
	if err != nil {
		t.Fatalf("GoFiles: %v", err)
	}
	want := []string{"pkg/main.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestGoFiles_DeduplicationAndSorting checks that the result is sorted
// (lexicographic, slash-form) and that the same file surfaced via multiple
// pkg entries (e.g. base + test variant) appears exactly once.
func TestGoFiles_DeduplicationAndSorting(t *testing.T) {
	root := t.TempDir()
	writeGoMod(t, root, "", "example.test/m")
	// Provide a regular file and a test file in the same package — Tests:true
	// will surface the package as multiple pkg entries; main.go appears in
	// the base pkg and again in the test-variant pkg.
	writeFile(t, root, "z/main.go", "package z\nfunc Z(){}\n")
	writeFile(t, root, "z/main_test.go", "package z\nimport \"testing\"\nfunc TestZ(t *testing.T){}\n")
	writeFile(t, root, "a/a.go", "package a\nfunc A(){}\n")

	got, err := detect.GoFiles(root)
	if err != nil {
		t.Fatalf("GoFiles: %v", err)
	}
	want := []string{"a/a.go", "z/main.go", "z/main_test.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// Defensive: confirm sort order matches sort.Strings.
	probe := append([]string(nil), got...)
	sort.Strings(probe)
	if !reflect.DeepEqual(got, probe) {
		t.Errorf("result not sorted: %v", got)
	}
}

// TestGoFiles_TestFilesIncluded asserts that `_test.go` files belonging to
// the module are included (Tests:true must be set).
func TestGoFiles_TestFilesIncluded(t *testing.T) {
	root := t.TempDir()
	writeGoMod(t, root, "", "example.test/m")
	writeFile(t, root, "main.go", "package main\nfunc main(){}\n")
	writeFile(t, root, "main_test.go", "package main\nimport \"testing\"\nfunc TestMain_(t *testing.T){}\n")

	got, err := detect.GoFiles(root)
	if err != nil {
		t.Fatalf("GoFiles: %v", err)
	}
	want := []string{"main.go", "main_test.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestGoFiles_ErrorOnMissingSrc verifies we surface a clear error when the
// caller passes a non-existent path.
func TestGoFiles_ErrorOnMissingSrc(t *testing.T) {
	if _, err := detect.GoFiles("/no/such/path/exists/for-real"); err == nil {
		t.Error("expected error for non-existent srcRoot")
	}
}

// TestGoFiles_GoWorkspace verifies that a srcRoot with a go.work pointing
// to two member modules surfaces files from BOTH members. Closes WORK-PLAN
// G5 (E2 review follow-up) — workspace handling was previously unverified.
//
// Layout:
//
//	root/
//	  go.work          (use ./alpha + ./beta)
//	  alpha/go.mod
//	  alpha/a.go
//	  beta/go.mod
//	  beta/b.go
//
// Expected: GoFiles returns {alpha/a.go, beta/b.go} sorted.
//
// Note: detect.GoFiles walks for go.mod files (not go.work) — the workspace
// root itself has no go.mod, so the walker descends into alpha/ and beta/
// independently. packages.Load("./...") in each module finds its own files.
// Workspace MEMBERS that live OUTSIDE srcRoot (e.g. ../shared) would not be
// surfaced — documented as out-of-scope; if it ever becomes load-bearing,
// add a `go.work` aware discovery pass.
func TestGoFiles_GoWorkspace(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "go.work", "go 1.22\n\nuse (\n\t./alpha\n\t./beta\n)\n")
	writeGoMod(t, root, "alpha", "example.com/alpha")
	writeFile(t, root, "alpha/a.go", "package alpha\n\nfunc A() int { return 1 }\n")
	writeGoMod(t, root, "beta", "example.com/beta")
	writeFile(t, root, "beta/b.go", "package beta\n\nfunc B() int { return 2 }\n")

	got, err := detect.GoFiles(root)
	if err != nil {
		t.Fatalf("GoFiles: %v", err)
	}
	want := []string{"alpha/a.go", "beta/b.go"}
	sort.Strings(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
