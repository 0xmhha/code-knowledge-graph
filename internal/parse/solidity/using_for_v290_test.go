package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.9 — contract-scope bare free-function alias.
//
// Alias-shape axis at contract scope after the W6 V3 NodeFunction
// fallback in resolveUsingForRef:
//
//   alias-entry shape              | example                | result
//   -------------------------------+------------------------+--------
//   library-qualified (Lib.member) | {Math.add, Math.sub}   | V2.6: 1
//   operator-form (Lib.m as +)     | {Math.add as +}        | V2.20: 1
//   bare (free-fn name only)       | {addPlusOne}           | V2.9:  1 (V3)
//
// V0 query `(using_directive (type_alias (identifier) @lib) ...)`
// captures `addPlusOne` as the @lib identifier. Pre-V3, the
// resolveUsingForRef lookup against byName[NodeContract] missed
// (free functions are NodeFunction). The V3 fallback to
// byName[NodeFunction] now resolves to the free function, lifting
// the historic 0-edge lock to one EdgeUsesFor per container.

// TestUsingForV290_ContractScopeBareFunctionAlias — `contract Calc
// { using {addPlusOne} for uint256; }` now resolves to the free
// function `addPlusOne`.
func TestUsingForV290_ContractScopeBareFunctionAlias(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v290", "probe_bare_function_alias.sol")

	// (a) Lock: 1 EdgeUsesFor (Calc → addPlusOne) after W6 V3.
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
	want := uf{src: "Calc", dst: "addPlusOne"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("expected one EdgeUsesFor %+v, got %v", want, got)
	}

	// (b) Lock: 0 EdgeCalls via using-for path. The bare alias
	// can't drive V1.0 dispatch since there's no library to look
	// up `addPlusOne.addPlusOne` against. The naked receiver call
	// `x.addPlusOne()` may surface as something else (e.g. an
	// AMBIGUOUS unresolved call), but it must not produce a using-
	// for-mediated EdgeCalls from `Calc.compute` to `addPlusOne`.
	qnameByID := map[string]string{}
	for _, n := range nodes {
		qnameByID[n.ID] = n.QualifiedName
	}
	for _, e := range edges {
		if e.Type != types.EdgeCalls {
			continue
		}
		srcQ := qnameByID[e.Src]
		dstQ := qnameByID[e.Dst]
		if srcQ == "Calc.compute" && dstQ == "addPlusOne" {
			t.Errorf("unexpected EdgeCalls Calc.compute → addPlusOne (V2.9 bare alias should not drive dispatch): %+v", e)
		}
	}

	// (c) Surround-safety: all declarations index.
	seenFreeFn := false
	seenCalc := false
	seenCompute := false
	for _, n := range nodes {
		switch n.QualifiedName {
		case "addPlusOne":
			seenFreeFn = true
		case "Calc":
			seenCalc = true
		case "Calc.compute":
			seenCompute = true
		}
	}
	if !seenFreeFn {
		t.Errorf("free function `addPlusOne` not indexed (V2.9 surround-safety)")
	}
	if !seenCalc {
		t.Errorf("contract `Calc` not indexed (V2.9 surround-safety)")
	}
	if !seenCompute {
		t.Errorf("function `Calc.compute` not indexed (V2.9 surround-safety)")
	}
}
