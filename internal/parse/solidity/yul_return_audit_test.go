package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V11 — Yul return/revert with `address()` as memory
// pointer is semantically a bug but not a low-level-call
// surface. The low-level-call walker (W10 V3/V4) restricts to
// `delegatecall`, `call`, `staticcall` opcodes and the self-
// delegate walker (V9/V10) additionally requires `delegatecall`
// — so `return(address(), N)` should NOT light up either marker.
//
// V11 locks the absence of false positives: a function whose
// only assembly content is `return(address(), 32)` keeps
// HasLowLevelCall=false and HasSelfDelegatecallDead=false, but
// HasAssembly=true (the function does contain an assembly
// block, which is a true positive).
func TestYulReturnAudit_NoFalsePositive(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/yul_receiver", "yul_return_audit.sol")

	var got types.Node
	for _, n := range nodes {
		if n.QualifiedName == "YulReturnAudit.bogusReturn" && n.Type == types.NodeFunction {
			got = n
			break
		}
	}
	if got.ID == "" {
		t.Fatalf("YulReturnAudit.bogusReturn not indexed")
	}
	if !got.HasAssembly {
		t.Errorf("HasAssembly: got false, want true (function does contain an assembly block)")
	}
	if got.HasLowLevelCall {
		t.Errorf("HasLowLevelCall: got true, want false (no call/delegatecall/staticcall opcode)")
	}
	if got.HasSelfDelegatecallDead {
		t.Errorf("HasSelfDelegatecallDead: got true, want false (no delegatecall opcode)")
	}
}
