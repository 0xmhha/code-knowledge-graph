package solidity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	sol "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// inheritWant is the (child, edge, parent) tuple used by both single-file
// and cross-file inheritance tests. Top-level (rather than function-local)
// so the failure-pretty-printer helper can share the type signature.
type inheritWant struct {
	child    string
	edgeType types.EdgeType
	parent   string
}

// TestSolInheritance_SingleFile covers W1 (Sol inheritance) detection for
// all same-file cases — simple, multiple, interface-implements, abstract.
//
// Spec: docs/design/solidity-inheritance-and-interface-dispatch.md §3.1, §4.1
// Decisions: §5.0 — same-file → EXTRACTED, child→parent direction.
//
// Each fixture is parsed in isolation (a fresh sol.Parser rooted at its
// own dir), then Resolve runs Pass 2 over the single result. The test
// asserts on (Src.Name, EdgeType, Dst.Name) tuples — robust to ID
// reshuffling and edge ordering.
func TestSolInheritance_SingleFile(t *testing.T) {
	type want = inheritWant
	cases := []struct {
		file  string
		wants []want
	}{
		{
			file: "simple_extends.sol",
			wants: []want{
				{"Child", types.EdgeExtends, "Parent"},
			},
		},
		{
			file: "multiple_inheritance.sol",
			wants: []want{
				{"Child", types.EdgeExtends, "A"},
				{"Child", types.EdgeExtends, "B"},
				{"Child", types.EdgeExtends, "C"},
			},
		},
		{
			file: "interface_implements.sol",
			wants: []want{
				{"Impl", types.EdgeImplements, "IERC20"},
				{"Mixed", types.EdgeExtends, "BaseContract"},
				{"Mixed", types.EdgeImplements, "IFoo"},
				{"Mixed", types.EdgeImplements, "IBar"},
				{"IB", types.EdgeExtends, "IA"},
			},
		},
		{
			file: "abstract_extends.sol",
			wants: []want{
				{"AbstractBase", types.EdgeImplements, "IThing"},
				{"Concrete", types.EdgeExtends, "AbstractBase"},
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			dir := filepath.Join("testdata", "inheritance")
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

			// Collect every extends/implements edge into a tuple set so the
			// assertion is order-independent and easy to grep on failure.
			gotTuples := map[want]bool{}
			for _, e := range resolved.Edges {
				if e.Type != types.EdgeExtends && e.Type != types.EdgeImplements {
					continue
				}
				gotTuples[want{
					child:    idToName[e.Src],
					edgeType: e.Type,
					parent:   idToName[e.Dst],
				}] = true
				// Same-file resolution must be EXTRACTED (§2.2). Inferred
				// would mean Resolve mis-detected the file boundary.
				if e.Confidence != types.ConfExtracted {
					t.Errorf("edge %s --%s--> %s confidence: got %v, want %v",
						idToName[e.Src], e.Type, idToName[e.Dst],
						e.Confidence, types.ConfExtracted)
				}
			}
			for _, w := range tc.wants {
				if !gotTuples[w] {
					t.Errorf("missing edge: %s --%s--> %s; got %v",
						w.child, w.edgeType, w.parent, keysOfWant(gotTuples))
				}
			}
		})
	}
}

// TestSolInheritance_CrossFile covers the Pass 2 cross-file PendingRef path
// (cross_file_child.sol inherits BaseToken / IExternal declared in
// cross_file_parent.sol). Both edges must resolve, and per §2.2 must
// be tagged ConfInferred because the resolution crosses a file boundary.
func TestSolInheritance_CrossFile(t *testing.T) {
	dir := filepath.Join("testdata", "inheritance")
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

	idToName := map[string]string{}
	for _, n := range resolved.Nodes {
		idToName[n.ID] = n.Name
	}

	type want struct {
		child    string
		edgeType types.EdgeType
		parent   string
		conf     types.Confidence
	}
	wants := []want{
		{"ChildToken", types.EdgeExtends, "BaseToken", types.ConfInferred},
		{"ChildToken", types.EdgeImplements, "IExternal", types.ConfInferred},
	}

	got := map[want]bool{}
	for _, e := range resolved.Edges {
		if e.Type != types.EdgeExtends && e.Type != types.EdgeImplements {
			continue
		}
		got[want{
			child:    idToName[e.Src],
			edgeType: e.Type,
			parent:   idToName[e.Dst],
			conf:     e.Confidence,
		}] = true
	}
	for _, w := range wants {
		if !got[w] {
			t.Errorf("missing edge: %s --%s--> %s (conf=%v); have %v",
				w.child, w.edgeType, w.parent, w.conf, got)
		}
	}
}

// TestSolInheritance_InterfaceNodeEmitted verifies W1's interface emit
// path: `interface IFoo { ... }` lands as a NodeInterface with
// SubKind="interface". Same-file regression — without this, the W1
// inheritance resolver can't classify `is IFoo` as EdgeImplements.
func TestSolInheritance_InterfaceNodeEmitted(t *testing.T) {
	dir := filepath.Join("testdata", "inheritance")
	src, err := os.ReadFile(filepath.Join(dir, "interface_implements.sol"))
	if err != nil {
		t.Fatal(err)
	}
	p := sol.New(dir)
	res, err := p.ParseFile(filepath.Join(dir, "interface_implements.sol"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	wantIfaces := map[string]bool{
		"IERC20": false, "IFoo": false, "IBar": false, "IA": false, "IB": false,
	}
	for _, n := range res.Nodes {
		if n.Type != types.NodeInterface {
			continue
		}
		if _, ok := wantIfaces[n.Name]; ok {
			wantIfaces[n.Name] = true
			if n.SubKind != "interface" {
				t.Errorf("Interface %s SubKind: got %q, want %q",
					n.Name, n.SubKind, "interface")
			}
		}
	}
	for name, found := range wantIfaces {
		if !found {
			t.Errorf("expected NodeInterface %q in interface_implements.sol", name)
		}
	}
}

// keysOfWant pretty-prints the gotTuples map for failure messages.
func keysOfWant(m map[inheritWant]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k.child+" --"+string(k.edgeType)+"--> "+k.parent)
	}
	return out
}
