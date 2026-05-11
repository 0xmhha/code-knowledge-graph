package solidity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	sol "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// overrideWant captures one EdgeOverrides assertion as (child.qname,
// parent.qname). The W2 emit direction is child → parent (§5.0 Q4), so
// child = edge.Src.QualifiedName, parent = edge.Dst.QualifiedName.
type overrideWant struct {
	child  string
	parent string
}

// TestSolOverrides_SingleFile covers W2 declaration-time override
// detection for all same-file cases — simple, super-call chain,
// virtual-only (no override), explicit multi-parent. Same-file
// resolutions must land as ConfExtracted per §2.2.
func TestSolOverrides_SingleFile(t *testing.T) {
	cases := []struct {
		file      string
		wants     []overrideWant
		subKinds  map[string]string // qname → expected SubKind
		wantTotal int               // total EdgeOverrides count
	}{
		{
			file: "simple_override.sol",
			wants: []overrideWant{
				{"Child.foo", "Parent.foo"},
			},
			subKinds: map[string]string{
				"Parent.foo": "virtual",
				"Child.foo":  "override",
			},
			wantTotal: 1,
		},
		{
			file:  "virtual_no_override.sol",
			wants: nil,
			subKinds: map[string]string{
				"Base.compute": "virtual",
				"Base.plain":   "function",
			},
			wantTotal: 0,
		},
		{
			file: "super_call.sol",
			wants: []overrideWant{
				{"Mid.greet", "Base.greet"},
				{"Top.greet", "Mid.greet"},
			},
			subKinds: map[string]string{
				"Base.greet": "virtual",
				"Mid.greet":  "virtual_override",
				"Top.greet":  "override",
			},
			wantTotal: 2,
		},
		{
			file: "multiple_inheritance_override.sol",
			wants: []overrideWant{
				{"C.foo", "A.foo"},
				{"C.foo", "B.foo"},
			},
			subKinds: map[string]string{
				"A.foo": "virtual",
				"B.foo": "virtual",
				"C.foo": "override",
			},
			wantTotal: 2,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			dir := filepath.Join("testdata", "overrides")
			src, err := os.ReadFile(filepath.Join(dir, tc.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			p := sol.New(dir)
			res, err := p.ParseFile(filepath.Join(dir, tc.file), src)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			resolved, err := p.Resolve([]*parse.ParseResult{res})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}

			idToQName := map[string]string{}
			subKindOf := map[string]string{}
			for _, n := range resolved.Nodes {
				idToQName[n.ID] = n.QualifiedName
				if n.Type == types.NodeFunction {
					subKindOf[n.QualifiedName] = n.SubKind
				}
			}

			// SubKind assertions — every named function must carry the
			// expected SubKind. Catches regressions in functionSubKind
			// mapping or scanFunctionModifiers walk.
			for qname, want := range tc.subKinds {
				got, ok := subKindOf[qname]
				if !ok {
					t.Errorf("function %q not found in parse result; got SubKinds: %v",
						qname, subKindOf)
					continue
				}
				if got != want {
					t.Errorf("function %q SubKind: got %q, want %q",
						qname, got, want)
				}
			}

			// EdgeOverrides assertions.
			gotTuples := map[overrideWant]bool{}
			var overrideCount int
			for _, e := range resolved.Edges {
				if e.Type != types.EdgeOverrides {
					continue
				}
				overrideCount++
				gotTuples[overrideWant{
					child:  idToQName[e.Src],
					parent: idToQName[e.Dst],
				}] = true
				// Same-file resolution must be EXTRACTED (§2.2).
				if e.Confidence != types.ConfExtracted {
					t.Errorf("edge %s overrides %s confidence: got %v, want %v",
						idToQName[e.Src], idToQName[e.Dst], e.Confidence,
						types.ConfExtracted)
				}
			}
			if overrideCount != tc.wantTotal {
				t.Errorf("EdgeOverrides count: got %d, want %d (have %v)",
					overrideCount, tc.wantTotal, gotTuples)
			}
			for _, w := range tc.wants {
				if !gotTuples[w] {
					t.Errorf("missing edge: %s overrides %s; got %v",
						w.child, w.parent, gotTuples)
				}
			}
		})
	}
}

// TestSolOverrides_CrossFile verifies that EdgeOverrides spanning a file
// boundary lands at ConfInferred (§2.2). cross_file_child.sol overrides
// a virtual function declared in cross_file_parent.sol.
func TestSolOverrides_CrossFile(t *testing.T) {
	dir := filepath.Join("testdata", "overrides")
	files := []string{"cross_file_parent.sol", "cross_file_child.sol"}
	p := sol.New(dir)
	results := make([]*parse.ParseResult, 0, len(files))
	for _, f := range files {
		src, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		r, err := p.ParseFile(filepath.Join(dir, f), src)
		if err != nil {
			t.Fatalf("ParseFile %s: %v", f, err)
		}
		results = append(results, r)
	}
	resolved, err := p.Resolve(results)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	idToQName := map[string]string{}
	for _, n := range resolved.Nodes {
		idToQName[n.ID] = n.QualifiedName
	}

	var found bool
	for _, e := range resolved.Edges {
		if e.Type != types.EdgeOverrides {
			continue
		}
		if idToQName[e.Src] == "ChildVault.deposit" &&
			idToQName[e.Dst] == "BaseVault.deposit" {
			found = true
			if e.Confidence != types.ConfInferred {
				t.Errorf("cross-file EdgeOverrides confidence: got %v, want %v",
					e.Confidence, types.ConfInferred)
			}
		}
	}
	if !found {
		t.Errorf("expected EdgeOverrides ChildVault.deposit → BaseVault.deposit not found")
	}
}

// TestSolOverrides_W1Regression locks the W1 inheritance edge counts on
// the W2 fixtures — W2 must not change EdgeExtends / EdgeImplements
// emission. The W2 fixtures all use single-parent or interface-free
// multi-parent inheritance; we assert a fixed (child, parent) tuple set
// per fixture so any regression that drops or duplicates a W1 edge fails
// loudly.
func TestSolOverrides_W1Regression(t *testing.T) {
	type extWant struct {
		child  string
		parent string
	}
	cases := []struct {
		file  string
		wants []extWant
	}{
		{"simple_override.sol", []extWant{{"Child", "Parent"}}},
		{"virtual_no_override.sol", nil},
		{"super_call.sol", []extWant{
			{"Mid", "Base"},
			{"Top", "Mid"},
		}},
		{"multiple_inheritance_override.sol", []extWant{
			{"C", "A"},
			{"C", "B"},
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			dir := filepath.Join("testdata", "overrides")
			src, err := os.ReadFile(filepath.Join(dir, tc.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			p := sol.New(dir)
			res, err := p.ParseFile(filepath.Join(dir, tc.file), src)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			resolved, err := p.Resolve([]*parse.ParseResult{res})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}

			idToName := map[string]string{}
			for _, n := range resolved.Nodes {
				idToName[n.ID] = n.Name
			}
			got := map[extWant]bool{}
			for _, e := range resolved.Edges {
				if e.Type != types.EdgeExtends && e.Type != types.EdgeImplements {
					continue
				}
				got[extWant{
					child:  idToName[e.Src],
					parent: idToName[e.Dst],
				}] = true
			}
			if len(got) != len(tc.wants) {
				t.Errorf("EdgeExtends/EdgeImplements count: got %d, want %d (have %v)",
					len(got), len(tc.wants), got)
			}
			for _, w := range tc.wants {
				if !got[w] {
					t.Errorf("missing W1 edge: %s → %s; got %v", w.child, w.parent, got)
				}
			}
		})
	}
}
