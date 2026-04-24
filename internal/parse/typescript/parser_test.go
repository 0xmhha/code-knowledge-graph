package typescript_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
)

type golden struct {
	NodeQnamesSubset []string          `json:"node_qnames_subset"`
	NodeTypes        map[string]string `json:"node_types"`
}

func TestTSParseSimpleClass(t *testing.T) {
	dir := "testdata"
	src, err := os.ReadFile(filepath.Join(dir, "simple_class.ts"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := tsp.New(dir).ParseFile(filepath.Join(dir, "simple_class.ts"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	gb, _ := os.ReadFile(filepath.Join(dir, "simple_class_golden.json"))
	var g golden
	_ = json.Unmarshal(gb, &g)

	have := map[string]string{}
	for _, n := range res.Nodes {
		have[n.QualifiedName] = string(n.Type)
	}
	for _, q := range g.NodeQnamesSubset {
		if _, ok := have[q]; !ok {
			t.Errorf("missing node qname %q (have: %v)", q, have)
		}
	}
	for q, want := range g.NodeTypes {
		if got := have[q]; got != want {
			t.Errorf("type for %q = %q, want %q", q, got, want)
		}
	}
}
