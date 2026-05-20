package audit

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeStore is an in-memory stand-in for persist.Store.
type fakeStore struct {
	paths map[string][]string
}

func (f *fakeStore) DistinctFilePaths(lang string) ([]string, error) {
	return append([]string(nil), f.paths[lang]...), nil
}

// writeGoModule materialises a tiny module under root/sub for tests.
func writeGoModule(t *testing.T, root, sub, mod string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+mod+"\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatalf("go.mod: %v", err)
	}
	for rel, body := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
}

func TestRunGo_Parity(t *testing.T) {
	root := t.TempDir()
	writeGoModule(t, root, "mod", "example.test/mod", map[string]string{
		"main.go":  "package main\nfunc main(){}\n",
		"a/a.go":   "package a\nfunc A(){}\n",
		"b/b/b.go": "package b\nfunc B(){}\n",
	})
	db := &fakeStore{paths: map[string][]string{
		"go": {"mod/main.go", "mod/a/a.go", "mod/b/b/b.go"},
	}}
	report, err := RunGo(root, db)
	if err != nil {
		t.Fatalf("RunGo: %v", err)
	}
	if !report.IsParity() {
		t.Errorf("expected parity, got InBuildOnly=%v InDBOnly=%v", report.InBuildOnly, report.InDBOnly)
	}
	if report.BuildCount != 3 || report.DBCount != 3 || report.InBoth != 3 {
		t.Errorf("counts wrong: %+v", report)
	}
}

func TestRunGo_MissingFromDB(t *testing.T) {
	root := t.TempDir()
	writeGoModule(t, root, "mod", "example.test/mod", map[string]string{
		"main.go": "package main\nfunc main(){}\n",
		"a/a.go":  "package a\nfunc A(){}\n",
	})
	db := &fakeStore{paths: map[string][]string{"go": {"mod/main.go"}}}
	report, err := RunGo(root, db)
	if err != nil {
		t.Fatalf("RunGo: %v", err)
	}
	if len(report.InBuildOnly) != 1 || report.InBuildOnly[0] != "mod/a/a.go" {
		t.Errorf("expected InBuildOnly=[mod/a/a.go], got %v", report.InBuildOnly)
	}
}

func TestRunGo_ExtraInDB(t *testing.T) {
	// `//go:build never` is a constraint no toolchain ever satisfies, so
	// the assertion holds regardless of host OS / GOARCH.
	root := t.TempDir()
	writeGoModule(t, root, "mod", "example.test/mod", map[string]string{
		"main.go":  "package main\nfunc main(){}\n",
		"never.go": "//go:build never\n\npackage main\n",
	})
	// detect.Walk would have indexed never.go; go/packages excludes it.
	db := &fakeStore{paths: map[string][]string{
		"go": {"mod/main.go", "mod/never.go"},
	}}
	report, err := RunGo(root, db)
	if err != nil {
		t.Fatalf("RunGo: %v", err)
	}
	if len(report.InDBOnly) != 1 || report.InDBOnly[0] != "mod/never.go" {
		t.Errorf("expected InDBOnly=[mod/never.go], got %v", report.InDBOnly)
	}
	if report.BuildCount != 1 {
		t.Errorf("build oracle should exclude tagged file, got BuildCount=%d", report.BuildCount)
	}
}

func TestRunGo_ErrorOnMissingSrc(t *testing.T) {
	if _, err := RunGo("/no/such/path/exists", &fakeStore{}); err == nil {
		t.Error("expected error for non-existent srcRoot")
	}
}

func TestReport_TextOutput(t *testing.T) {
	parity := Report{BuildCount: 2, DBCount: 2, InBoth: 2}
	var buf bytes.Buffer
	if err := parity.WriteText(&buf); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"build files: 2", "verdict: PARITY"} {
		if !strings.Contains(out, want) {
			t.Errorf("parity text missing %q: %q", want, out)
		}
	}
	if strings.Contains(out, "MISSING") || strings.Contains(out, "EXTRA") {
		t.Errorf("parity text should omit empty lists: %q", out)
	}

	drift := Report{BuildCount: 3, DBCount: 2, InBuildOnly: []string{"a/missing.go"}, InDBOnly: []string{"a/extra.go", "b/extra.go"}, InBoth: 2}
	buf.Reset()
	if err := drift.WriteText(&buf); err != nil {
		t.Fatalf("WriteText drift: %v", err)
	}
	out = buf.String()
	for _, want := range []string{"verdict: DRIFT", "MISSING", "a/missing.go", "EXTRA", "a/extra.go", "b/extra.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("drift text missing %q: %q", want, out)
		}
	}
}

func TestReport_JSONRoundtrip(t *testing.T) {
	r := Report{BuildCount: 2, DBCount: 2, InBuildOnly: []string{"x.go"}, InDBOnly: []string{"y.go"}, InBoth: 1}
	var buf bytes.Buffer
	if err := r.WriteJSON(&buf); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var got Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v -- raw=%q", err, buf.String())
	}
	if got.BuildCount != r.BuildCount || got.DBCount != r.DBCount || got.InBoth != r.InBoth {
		t.Errorf("counts mismatch: got %+v want %+v", got, r)
	}
	if len(got.InBuildOnly) != 1 || got.InBuildOnly[0] != "x.go" {
		t.Errorf("InBuildOnly mismatch: %v", got.InBuildOnly)
	}
	if len(got.InDBOnly) != 1 || got.InDBOnly[0] != "y.go" {
		t.Errorf("InDBOnly mismatch: %v", got.InDBOnly)
	}
}
