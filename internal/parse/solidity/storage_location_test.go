package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W7.2 V0 — state-var visibility / immutable encoded into
// NodeField.SubKind.
//
// V0 scope (empirical-driven by grammar v1.2.11 AST probe 2026-05-17):
//   - `visibility` child → SubKind = "storage_<vis>" (public / private /
//     internal / external)
//   - `immutable` child → SubKind = "immutable" (takes precedence over
//     visibility if both — semantically immutable is the storage class)
//   - no visibility child and no immutable child → SubKind = ""
//     (V0 doesn't synthesise the Sol default "internal" because the
//     AST has no positive signal — leaving "" lets downstream consumers
//     distinguish "explicitly internal" from "default/unknown")
//
// Out of V0:
//   - `constant` keyword (grammar drops it from AST — needs source-text
//     scan or grammar bump)
//   - parameter location (memory / calldata / storage — same gap)

// TestStorageLocation_StateVarVisibility — 5 state-vars across 4 SubKind
// values (public / private / internal / immutable / "" for default).
func TestStorageLocation_StateVarVisibility(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/storage_location", "state_var_visibility.sol")

	want := map[string]string{
		"C.a": "storage_public",
		"C.b": "storage_private",
		"C.c": "storage_internal",
		"C.d": "immutable",
		"C.e": "",
	}

	got := map[string]string{}
	for _, n := range nodes {
		if n.Type != types.NodeField {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = n.SubKind
		}
	}

	for qn, wantSub := range want {
		gotSub, present := got[qn]
		if !present {
			t.Errorf("W7.2 missing NodeField %q", qn)
			continue
		}
		if gotSub != wantSub {
			t.Errorf("W7.2 NodeField %q SubKind: got %q, want %q", qn, gotSub, wantSub)
		}
	}
}
