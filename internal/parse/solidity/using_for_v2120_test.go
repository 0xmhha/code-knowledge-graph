package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.12 — user-defined value type (UDVT, Solidity 0.8.8+) as
// the receiver type for `using Lib for Amount;`.
//
// UDVTs (`type Amount is uint256;`) are first-class types with no
// implicit conversion to/from the underlying primitive. Library
// functions taking `Amount` parameters become method-callable on
// Amount values through using-for. The question this V cycle locks:
// does V0's `source: (_) @type` capture user_defined_type nodes the
// same way it captures primitive types, and does V1.0 state-var
// dispatch resolve through the resulting binding?
//
// V0 query: `(using_directive (type_alias (identifier) @lib)
//                              source: (_) @type) @stmt`
// Tree-sitter-solidity v1.2.13 wraps UDVT references in a `type_name`
// node containing a `user_defined_type` child — same shape as
// cross-contract type references. `normaliseUsingForType` calls
// `extractTypeNameText` and should yield "Amount".
//
// V1.0 state-var dispatch:
//   - `Amount public balance;` → NodeField with Signature="Amount".
//   - `balance.double()` → V1.0 walker captures (Vault, balance,
//     double).
//   - Pass 2: lookup bindings[Vault]["Amount"] → ["Math"], lookup
//     funcByQName["Math.double"] → Math.double's NodeFunction ID.
//   - Emit EdgeCalls Vault.tick → Math.double.

// TestUsingForV2120_UDVTBinding — UDVT-typed receiver with using-for
// dispatch. Verifies the whole chain works.
func TestUsingForV2120_UDVTBinding(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for_v2120", "value_type_binding.sol")

	qnameByID := map[string]string{}
	for _, n := range nodes {
		qnameByID[n.ID] = n.QualifiedName
	}

	// (a) EdgeUsesFor: Vault → Math (V0 type_alias capture).
	seenUF := false
	for _, e := range edges {
		if e.Type != types.EdgeUsesFor {
			continue
		}
		if qnameByID[e.Src] == "Vault" && qnameByID[e.Dst] == "Math" {
			seenUF = true
		}
	}
	if !seenUF {
		t.Errorf("missing EdgeUsesFor Vault → Math (V2.12 UDVT binding)")
	}

	// (b) EdgeCalls: Vault.tick → Math.double via V1.0 state-var
	// dispatch on Amount-typed `balance`.
	edge, ok := findUsingForCall(nodes, edges, "Vault.tick", "Math.double")
	if !ok {
		t.Fatalf("missing EdgeCalls Vault.tick → Math.double (V2.12 UDVT dispatch)")
	}
	if edge.Confidence != types.ConfExtracted {
		t.Errorf("same-file V2.12 UDVT EdgeCalls confidence: got %v, want ConfExtracted",
			edge.Confidence)
	}

	// (c) Surround-safety: UDVT declaration shouldn't break sibling
	// declarations.
	seenMath := false
	seenDouble := false
	seenVault := false
	seenTick := false
	for _, n := range nodes {
		switch n.QualifiedName {
		case "Math":
			seenMath = true
		case "Math.double":
			seenDouble = true
		case "Vault":
			seenVault = true
		case "Vault.tick":
			seenTick = true
		}
	}
	if !seenMath {
		t.Errorf("library `Math` not indexed (V2.12 surround-safety)")
	}
	if !seenDouble {
		t.Errorf("function `Math.double` not indexed (V2.12 surround-safety)")
	}
	if !seenVault {
		t.Errorf("contract `Vault` not indexed (V2.12 surround-safety)")
	}
	if !seenTick {
		t.Errorf("function `Vault.tick` not indexed (V2.12 surround-safety)")
	}
}
