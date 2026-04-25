package typescript_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
)

type golden struct {
	NodeQnamesSubset []string          `json:"node_qnames_subset"`
	NodeTypes        map[string]string `json:"node_types"`
}

func TestTSParser_Extensions(t *testing.T) {
	p := tsp.New(".")
	exts := p.Extensions()
	if len(exts) == 0 {
		t.Fatal("Extensions() returned empty slice")
	}
	found := false
	for _, e := range exts {
		if e == ".ts" {
			found = true
		}
	}
	if !found {
		t.Errorf("Extensions() does not contain \".ts\": %v", exts)
	}
}

// TestTSParser_Resolve exercises the cross-file name-based resolution pass.
// We hand-build two ParseResults: one with a function definition, another with
// a CallSite PendingRef whose TargetQName matches the function's Name.
func TestTSParser_Resolve(t *testing.T) {
	p := tsp.New(".")

	defNode := types.Node{
		ID:            "fn1",
		Type:          types.NodeFunction,
		Name:          "doWork",
		QualifiedName: "mod.doWork",
		FilePath:      "a.ts",
		Language:      "typescript",
		Confidence:    types.ConfExtracted,
	}
	callNode := types.Node{
		ID:            "cs1",
		Type:          types.NodeCallSite,
		Name:          "doWork",
		QualifiedName: "mod.main#doWork",
		FilePath:      "b.ts",
		Language:      "typescript",
		Confidence:    types.ConfExtracted,
	}

	r1 := &parse.ParseResult{
		Path:  "a.ts",
		Nodes: []types.Node{defNode},
	}
	r2 := &parse.ParseResult{
		Path:  "b.ts",
		Nodes: []types.Node{callNode},
		Pending: []parse.PendingRef{
			{
				SrcID:       "cs1",
				EdgeType:    types.EdgeCalls,
				TargetQName: "doWork",
				Line:        5,
			},
		},
	}

	rg, err := p.Resolve([]*parse.ParseResult{r1, r2})
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	// Expect 2 nodes (both nodes merged) and at least 1 resolved edge.
	if len(rg.Nodes) != 2 {
		t.Errorf("want 2 nodes, got %d", len(rg.Nodes))
	}
	found := false
	for _, e := range rg.Edges {
		if e.Src == "cs1" && e.Dst == "fn1" && e.Type == types.EdgeCalls {
			found = true
		}
	}
	if !found {
		t.Errorf("expected resolved edge cs1→fn1 of type %q; edges: %v", types.EdgeCalls, rg.Edges)
	}
}

// TestTSParser_Resolve_NoPendingRefs verifies Resolve works with no pending refs.
func TestTSParser_Resolve_NoPendingRefs(t *testing.T) {
	p := tsp.New(".")
	r := &parse.ParseResult{
		Path: "x.ts",
		Nodes: []types.Node{
			{ID: "n1", Type: types.NodeClass, Name: "Foo", QualifiedName: "Foo",
				FilePath: "x.ts", Language: "typescript", Confidence: types.ConfExtracted},
		},
	}
	rg, err := p.Resolve([]*parse.ParseResult{r})
	if err != nil {
		t.Fatalf("Resolve: unexpected error: %v", err)
	}
	if len(rg.Nodes) != 1 {
		t.Errorf("want 1 node, got %d", len(rg.Nodes))
	}
	if len(rg.Edges) != 0 {
		t.Errorf("want 0 edges, got %d", len(rg.Edges))
	}
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
