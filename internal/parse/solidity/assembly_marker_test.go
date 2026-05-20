package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W10 V0 — Node.HasAssembly marker for callables containing
// `assembly { ... }` blocks.
//
// V0 detects presence only — any assembly_statement descendant of a
// function / modifier / constructor / fallback body flips the flag.
// Yul-internal op detection (which builtins are called, target
// resolution for delegatecall / call / staticcall, storage-slot
// references via sstore / sload) are V1+.
//
// The flag is a NodeFunction / NodeModifier metadata bit. Functions
// without assembly leave it at the zero default (false, omitted from
// JSON).

func TestAssemblyMarker_HasAssembly(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/assembly_marker", "inline_assembly.sol")

	want := map[string]bool{
		"Proxy.delegate":   true,
		"Proxy.readSlot":   true,
		"Proxy.plain":      false,
		"Proxy.guard":      true,
		"Proxy.plainGuard": false,
	}

	got := map[string]bool{}
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction && n.Type != types.NodeModifier {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = n.HasAssembly
			seen[n.QualifiedName] = true
		}
	}

	for qn, wantFlag := range want {
		if !seen[qn] {
			t.Errorf("W10 missing node %q", qn)
			continue
		}
		if got[qn] != wantFlag {
			t.Errorf("W10 node %q HasAssembly: got %v, want %v", qn, got[qn], wantFlag)
		}
	}
}
