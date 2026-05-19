package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.8 — file-level free-function form using directive.
//
// 2x2 (scope × alias-shape) matrix after the V2.5 file-level
// operator-form recovery walker landed:
//
//                  | free-function          | operator-form
//   ---------------+------------------------+----------------------
//   file-level     | V2.8 (this): 1 edge    | V2.5: 1 edge (library)
//   contract-scope | V2.6: 1 edge (V0 inc.) | V2.20: 1 edge (recover)
//
// The V2.5 walker (file_level_operator_form.go) parses any file-level
// `using {...} for T [global];` ERROR child by text, so the braced
// free-function form (V2.8) is recovered alongside the operator-form
// variant. Library-method targets reduce to the library name, so
// `using {Math.add} for uint256 global;` resolves to `Calc → Math`.
//
// Surround-safety: library `Math`, function `Math.add`, contract
// `Calc`, and function `Calc.compute` must all still index.

// TestUsingForV280_FileLevelFreeFunctionForm — `using {Math.add} for
// uint256 global;` at file scope. Asserts file-level fan-out to
// every non-library container after V2.5 walker landed.
func TestUsingForV280_FileLevelFreeFunctionForm(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v280", "probe_file_level_free_function.sol")

	// (a) Lock: 1 EdgeUsesFor (Calc → Math) after V2.5 walker.
	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	type uf struct{ src, dst string }
	var got []uf
	for _, e := range edges {
		if e.Type == types.EdgeUsesFor {
			got = append(got, uf{src: byID[e.Src].Name, dst: byID[e.Dst].Name})
		}
	}
	want := uf{src: "Calc", dst: "Math"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("expected one EdgeUsesFor %+v, got %v", want, got)
	}

	// (b) Surround-safety: all surrounding declarations still indexed.
	seenLib := false
	seenAdd := false
	seenCalc := false
	seenCompute := false
	for _, n := range nodes {
		switch n.QualifiedName {
		case "Math":
			seenLib = true
		case "Math.add":
			seenAdd = true
		case "Calc":
			seenCalc = true
		case "Calc.compute":
			seenCompute = true
		}
	}
	if !seenLib {
		t.Errorf("library `Math` not indexed (V2.8 surround-safety)")
	}
	if !seenAdd {
		t.Errorf("function `Math.add` not indexed (V2.8 surround-safety)")
	}
	if !seenCalc {
		t.Errorf("contract `Calc` not indexed (V2.8 surround-safety)")
	}
	if !seenCompute {
		t.Errorf("function `Calc.compute` not indexed (V2.8 surround-safety)")
	}
}
