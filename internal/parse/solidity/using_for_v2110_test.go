package solidity_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V2.11 — bare path-only import (Solidity default form).
//
// V1.28 / V1.29 / V2.10 covered the three import-with-clause forms:
//   - V1.28 named alias    `import {Lib as L} from "./lib.sol"`
//   - V1.29 namespace      `import "./lib.sol" as L`
//   - V2.10 mixed (V2.10)  `import {LibA, LibB as L2} from "./lib.sol"`
//
// V2.11 closes the import-shape coverage with the *default* form:
//
//   import "./lib.sol";
//
// Semantically the bare path-only form imports all top-level
// declarations of the partner file under their original names. It
// records no entries in importAliases or namespaceAliases, and
// dispatches through the global byName[NodeContract] index. This
// is also the form most Solidity code actually uses.
//
// V2.11 is primarily a regression guard — V1.14's cross-file
// dispatch infrastructure was designed for the global index, so
// nothing should be specific to the curly-brace form. But after
// V2.10's hidden positional-zip bug surfaced, locking the default
// form makes the alias-machinery test surface complete.

// TestUsingForV2110_BarePathOnlyImport — `import "./lib.sol";`
// cross-file. Verifies the default form drives the same EdgeUsesFor
// + V1.0 dispatch path as the curly-brace forms.
func TestUsingForV2110_BarePathOnlyImport(t *testing.T) {
	dir := filepath.Join("testdata", "using_for_v2110")
	files := []string{"bare_libs.sol", "bare_caller.sol"}
	nodes, edges := parseResolveMultiSol(t, dir, files)

	qnameByID := map[string]string{}
	for _, n := range nodes {
		qnameByID[n.ID] = n.QualifiedName
	}

	// (a) EdgeUsesFor: Vault → SafeMath, ConfInferred (cross-file).
	seenUF := false
	for _, e := range edges {
		if e.Type != types.EdgeUsesFor {
			continue
		}
		if qnameByID[e.Src] == "Vault" && qnameByID[e.Dst] == "SafeMath" {
			seenUF = true
			if e.Confidence != types.ConfInferred {
				t.Errorf("cross-file V2.11 EdgeUsesFor confidence: got %v, want ConfInferred",
					e.Confidence)
			}
		}
	}
	if !seenUF {
		t.Errorf("missing EdgeUsesFor Vault → SafeMath (V2.11 bare path-only import)")
	}

	// (b) EdgeCalls: Vault.compute → SafeMath.add via V1.0 dispatch.
	edge, ok := findUsingForCall(nodes, edges, "Vault.compute", "SafeMath.add")
	if !ok {
		t.Fatalf("missing EdgeCalls Vault.compute → SafeMath.add (V2.11 bare path-only dispatch)")
	}
	if edge.Confidence != types.ConfInferred {
		t.Errorf("cross-file V2.11 EdgeCalls confidence: got %v, want ConfInferred",
			edge.Confidence)
	}

	// (c) Surround-safety: declarations still index normally.
	seenLib := false
	seenAdd := false
	seenVault := false
	seenCompute := false
	for _, n := range nodes {
		switch n.QualifiedName {
		case "SafeMath":
			seenLib = true
		case "SafeMath.add":
			seenAdd = true
		case "Vault":
			seenVault = true
		case "Vault.compute":
			seenCompute = true
		}
	}
	if !seenLib {
		t.Errorf("library `SafeMath` not indexed (V2.11 surround-safety)")
	}
	if !seenAdd {
		t.Errorf("function `SafeMath.add` not indexed (V2.11 surround-safety)")
	}
	if !seenVault {
		t.Errorf("contract `Vault` not indexed (V2.11 surround-safety)")
	}
	if !seenCompute {
		t.Errorf("function `Vault.compute` not indexed (V2.11 surround-safety)")
	}

	// (d) Negative guards: nothing should leak into importAliases or
	// namespaceAliases for the bare form. We can't directly inspect
	// those internal maps, but a phantom Vault → "bare_libs" (file
	// name leaked as library) would surface here as a stray edge.
	for _, e := range edges {
		if e.Type != types.EdgeUsesFor {
			continue
		}
		dst := qnameByID[e.Dst]
		if dst != "SafeMath" {
			t.Errorf("unexpected EdgeUsesFor Vault → %s (V2.11 should only target SafeMath)", dst)
		}
	}
}
