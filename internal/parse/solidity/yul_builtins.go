package solidity

import (
	"sort"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
)

// Sol W-C W10 V1.1 (2026-05-18) — Yul EVM builtin detection.
//
// W10 V0 flipped a single HasAssembly bool on callables containing
// any `assembly { ... }` block. V1.1 enumerates the security-
// relevant EVM opcodes that actually appear inside the block so
// audits can ask "which functions use delegatecall in Yul" without
// re-parsing source.
//
// V1.1 filters to a critical-set whitelist:
//
//   call          — low-level call inside assembly
//   delegatecall  — proxy / forwarder pattern (highest-risk)
//   sload         — storage read bypassing Sol type system
//   sstore        — storage write bypassing Sol type system
//   staticcall    — read-only low-level call
//   selfdestruct  — contract destruction (highest-risk)
//
// Common non-critical ops (add, mul, calldatacopy, returndatacopy,
// gas, switch / case, etc.) stay invisible. The filter avoids
// surfacing every assembly user as "uses Yul builtins" when most
// blocks are arithmetic or memory copies.
//
// Grammar shape (vendored tree-sitter-solidity v1.2.11, verified via
// V2.18 / W10 probes):
//
//   assembly_statement
//     yul_function_call
//       yul_evm_builtin   "<opname>"
//       ...arguments...
//
// The walker queries `yul_evm_builtin` directly (rather than
// descending through yul_function_call) so deeply nested calls and
// argument-position builtins are all surfaced uniformly.

// criticalYulBuiltins gates what gets recorded. Adding a new entry
// (e.g. "create2", "extcodecopy") is the supported way to extend
// coverage; nothing else in V1.1 needs to change.
var criticalYulBuiltins = map[string]bool{
	"call":         true,
	"delegatecall": true,
	"sload":        true,
	"sstore":       true,
	"staticcall":   true,
	"selfdestruct": true,
}

// runYulBuiltins walks every `yul_evm_builtin` node, keeps only
// those whose identifier is in the critical-set, attributes each to
// its enclosing callable via nearestFunctionQnameAndStart, and
// mutates v.nodes post-Pass-1 with a sorted, deduped slice.
//
// Same idiom as runAssemblyMarker (W10 V0) and runLowLevelCallMarker
// (W8 V1): post-Pass-1 in-place mutation, no edges emitted.
func (v *declVisitor) runYulBuiltins() {
	const q = `(yul_evm_builtin) @op`
	query, qErr := sitter.NewQuery(v.lang, q)
	if qErr != nil {
		return
	}
	defer func() { query.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()

	// funcID → set of builtin names. Set keeps duplicates within a
	// function from inflating the slice (multiple sstore sites in one
	// body still surface as a single "sstore" entry).
	byFunc := map[string]map[string]bool{}

	for {
		m := matches.Next()
		if m == nil {
			break
		}
		for _, c := range m.Captures {
			if names[c.Index] != "op" {
				continue
			}
			node := c.Node
			name := node.Utf8Text(v.src)
			if !criticalYulBuiltins[name] {
				continue
			}
			fnQ, fnStart, ok := nearestFunctionQnameAndStart(&node, v.src)
			if !ok {
				continue
			}
			id := parse.MakeID(fnQ, "sol", fnStart)
			set, exists := byFunc[id]
			if !exists {
				set = map[string]bool{}
				byFunc[id] = set
			}
			set[name] = true
		}
	}

	if len(byFunc) == 0 {
		return
	}
	for i := range v.nodes {
		set, ok := byFunc[v.nodes[i].ID]
		if !ok {
			continue
		}
		out := make([]string, 0, len(set))
		for name := range set {
			out = append(out, name)
		}
		sort.Strings(out)
		v.nodes[i].YulBuiltins = out
	}
}
