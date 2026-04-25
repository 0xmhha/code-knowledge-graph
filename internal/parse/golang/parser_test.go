package golang_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
)

type golden struct {
	NodeQnames []string          `json:"node_qnames"`
	NodeTypes  map[string]string `json:"node_types"`
}

func TestParseDeclarationsGolden(t *testing.T) {
	dir := "testdata/declarations"
	p := gop.New(dir)
	src, err := os.ReadFile(filepath.Join(dir, "simple_struct.go"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.ParseFile(filepath.Join(dir, "simple_struct.go"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	gb, err := os.ReadFile(filepath.Join(dir, "simple_struct_golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(gb, &g); err != nil {
		t.Fatal(err)
	}
	gotQ := make([]string, 0, len(res.Nodes))
	gotTypes := map[string]string{}
	for _, n := range res.Nodes {
		// Skip Package and File auto-nodes for golden focus
		if n.Type == "Package" || n.Type == "File" {
			continue
		}
		gotQ = append(gotQ, n.QualifiedName)
		gotTypes[n.QualifiedName] = string(n.Type)
	}
	sort.Strings(gotQ)
	want := append([]string(nil), g.NodeQnames...)
	sort.Strings(want)
	if !equalStr(gotQ, want) {
		t.Errorf("qnames = %v, want %v", gotQ, want)
	}
	for q, wantT := range g.NodeTypes {
		if got := gotTypes[q]; got != wantT {
			t.Errorf("type for %s: got %q, want %q", q, got, wantT)
		}
	}
}

func equalStr(a, b []string) bool {
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

func TestGoParser_Extensions(t *testing.T) {
	p := gop.New(".")
	exts := p.Extensions()
	if len(exts) == 0 {
		t.Fatal("Extensions() returned empty slice")
	}
	found := false
	for _, e := range exts {
		if e == ".go" {
			found = true
		}
	}
	if !found {
		t.Errorf("Extensions() does not contain \".go\": %v", exts)
	}
}
