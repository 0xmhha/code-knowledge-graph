package solidity_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	sol "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// dispatchWant captures one EdgeInvokes assertion as (caller.qname,
// interface.method.qname). The W3 emit direction is caller → interface
// method (§3.4), so caller = edge.Src.QualifiedName, target =
// edge.Dst.QualifiedName.
type dispatchWant struct {
	caller string
	target string
}

// TestSolDispatch_SingleFile covers W3 interface-dispatch detection for
// all same-file fixtures.
//
// Confidence policy (§5.0 Q5): every resolved EdgeInvokes is
// ConfAmbiguous regardless of file boundary — runtime address determines
// the actual dispatch target, not file location.
func TestSolDispatch_SingleFile(t *testing.T) {
	cases := []struct {
		file      string
		wants     []dispatchWant
		wantTotal int // total EdgeInvokes count
	}{
		{
			file: "simple_dispatch.sol",
			wants: []dispatchWant{
				{"Caller.send", "IERC20.transfer"},
				{"Caller.check", "IERC20.balanceOf"},
			},
			wantTotal: 2,
		},
		{
			file: "chained_dispatch.sol",
			wants: []dispatchWant{
				{"Router.route", "IFoo.bar"},
				{"Router.route", "IBar.baz"},
				{"Router.proxy", "IFoo.something"},
			},
			wantTotal: 3,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			dir := filepath.Join("testdata", "dispatch")
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
			for _, n := range resolved.Nodes {
				idToQName[n.ID] = n.QualifiedName
			}

			gotTuples := map[dispatchWant]bool{}
			var invokeCount int
			for _, e := range resolved.Edges {
				if e.Type != types.EdgeInvokes {
					continue
				}
				invokeCount++
				gotTuples[dispatchWant{
					caller: idToQName[e.Src],
					target: idToQName[e.Dst],
				}] = true
				// §5.0 Q5: every interface-dispatch edge is AMBIGUOUS,
				// same-file or cross-file alike.
				if e.Confidence != types.ConfAmbiguous {
					t.Errorf("edge %s invokes %s confidence: got %v, want %v",
						idToQName[e.Src], idToQName[e.Dst], e.Confidence,
						types.ConfAmbiguous)
				}
			}
			if invokeCount != tc.wantTotal {
				t.Errorf("EdgeInvokes count: got %d, want %d (have %v)",
					invokeCount, tc.wantTotal, gotTuples)
			}
			for _, w := range tc.wants {
				if !gotTuples[w] {
					t.Errorf("missing edge: %s invokes %s; got %v",
						w.caller, w.target, gotTuples)
				}
			}
		})
	}
}

// TestSolDispatch_CrossFile verifies that interface-dispatch resolution
// works across file boundaries — the interface lives in one file, the
// caller in another. Per §5.0 Q5 confidence is still AMBIGUOUS (not
// INFERRED) because file boundary is not the source of uncertainty for
// W3 — runtime address binding is.
func TestSolDispatch_CrossFile(t *testing.T) {
	dir := filepath.Join("testdata", "dispatch")
	files := []string{"cross_file_iface.sol", "cross_file_caller.sol"}
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
		if e.Type != types.EdgeInvokes {
			continue
		}
		if idToQName[e.Src] == "ExternalCaller.run" &&
			idToQName[e.Dst] == "IExternalAPI.execute" {
			found = true
			if e.Confidence != types.ConfAmbiguous {
				t.Errorf("cross-file EdgeInvokes confidence: got %v, want %v",
					e.Confidence, types.ConfAmbiguous)
			}
		}
	}
	if !found {
		t.Errorf("expected EdgeInvokes ExternalCaller.run → IExternalAPI.execute not found")
	}
}

// TestSolDispatch_NoFalsePositive verifies the predicate's
// rejection cases stay rejected:
//
//  1. `address(this)` — primitive type cast, identifier is not an
//     interface name → no edge.
//  2. `super.foo()` — member_expression but object is a plain
//     identifier, not a call_expression → no edge.
//  3. Unknown identifier `Unknown(addr).foo()` — type name does not
//     resolve to any NodeInterface → no edge (drop policy).
//
// Failure here would mean the detector is over-emitting AMBIGUOUS
// edges, polluting the LLM-filter wrapper's signal-to-noise ratio.
func TestSolDispatch_NoFalsePositive(t *testing.T) {
	// Inline fixture — kept here (not in testdata/) because it documents
	// a *negative* contract and is unlikely to be reused as a build
	// input. Mirrors the inline approach the Go parser uses for
	// rejection tests.
	src := []byte(`pragma solidity ^0.8.20;

contract Base {
    function greet() public virtual returns (uint) { return 1; }
}

contract Caller is Base {
    function pure_local() external returns (address) {
        return address(this);          // primitive cast — must not emit
    }

    function via_super() public override returns (uint) {
        return super.greet();          // super.foo() — object is identifier
    }

    function via_unknown(address a) external returns (uint) {
        // Unknown(...) is not declared anywhere in the build — the
        // resolver must drop the edge rather than emit one for a
        // mystery interface name.
        return Unknown(a).foo();
    }
}`)
	dir := "testdata/dispatch"
	p := sol.New(dir)
	res, err := p.ParseFile(filepath.Join(dir, "_inline.sol"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	resolved, err := p.Resolve([]*parse.ParseResult{res})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var invokeCount int
	idToQName := map[string]string{}
	for _, n := range resolved.Nodes {
		idToQName[n.ID] = n.QualifiedName
	}
	for _, e := range resolved.Edges {
		if e.Type != types.EdgeInvokes {
			continue
		}
		invokeCount++
		t.Errorf("unexpected EdgeInvokes: %s → %s",
			idToQName[e.Src], idToQName[e.Dst])
	}
	if invokeCount != 0 {
		t.Errorf("EdgeInvokes count: got %d, want 0", invokeCount)
	}
}

// TestSolDispatch_W1W2Regression locks the W1 / W2 edge counts on the W3
// fixtures so a regression that disturbs prior detectors fails loudly.
// The W3 detector is purely additive (new PendingRefs, new resolver
// branch); W1 EdgeExtends/EdgeImplements and W2 EdgeOverrides counts on
// W3 fixtures must stay at zero (the dispatch fixtures contain neither
// inheritance nor override modifiers).
func TestSolDispatch_W1W2Regression(t *testing.T) {
	files := []string{
		"simple_dispatch.sol",
		"chained_dispatch.sol",
	}
	for _, f := range files {
		f := f
		t.Run(f, func(t *testing.T) {
			dir := filepath.Join("testdata", "dispatch")
			src, err := os.ReadFile(filepath.Join(dir, f))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			p := sol.New(dir)
			res, err := p.ParseFile(filepath.Join(dir, f), src)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			resolved, err := p.Resolve([]*parse.ParseResult{res})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			for _, e := range resolved.Edges {
				switch e.Type {
				case types.EdgeExtends, types.EdgeImplements, types.EdgeOverrides:
					t.Errorf("unexpected W1/W2 edge in %s: type=%s", f, e.Type)
				}
			}
		})
	}
}
