package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
)

// Sol W10 V0 (2026-05-18) — HasAssembly marker for callables.
//
// Sets Node.HasAssembly = true on every NodeFunction / NodeModifier
// whose body contains at least one `assembly { ... }` block. The
// marker is a presence-only signal — Yul-internal op detection
// (delegatecall, sstore, selfdestruct, etc.) and receiver resolution
// for low-level Yul calls are deferred to V1+.
//
// The walker runs as a post-Pass-1 sweep over assembly_statement
// nodes, resolves each to the enclosing function-like node via
// nearestFunctionQnameAndStart, and mutates v.nodes in place. This
// avoids touching every function emit site (runFunctionDecl,
// runConstructorDecl, runFallbackReceiveDecl, runDecl-for-modifier)
// with the same one-line check.
//
// Tree-sitter-solidity v1.2.11 (vendored) exposes assembly_statement
// as a top-level node kind for the `assembly { ... }` block; its
// children are Yul nodes (yul_function_call, yul_evm_builtin, ...)
// which V0 doesn't introspect.
func (v *declVisitor) runAssemblyMarker() {
	const q = `(assembly_statement) @asm`
	query, qErr := sitter.NewQuery(v.lang, q)
	if qErr != nil {
		return
	}
	defer func() { query.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	affected := map[string]bool{}
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		for _, c := range m.Captures {
			if names[c.Index] != "asm" {
				continue
			}
			asmNode := c.Node
			fnQ, fnStart, ok := nearestFunctionQnameAndStart(&asmNode, v.src)
			if !ok {
				continue
			}
			affected[parse.MakeID(fnQ, "sol", fnStart)] = true
		}
	}
	if len(affected) == 0 {
		return
	}
	for i := range v.nodes {
		if affected[v.nodes[i].ID] {
			v.nodes[i].HasAssembly = true
		}
	}
}
