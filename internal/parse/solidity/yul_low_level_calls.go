package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Sol W-C W10 V2 (2026-05-18) — Yul-level low-level call receiver
// resolution.
//
// V0 marked HasAssembly on callables with inline assembly; V1.1
// enumerated which Yul EVM builtins appeared. V2 closes the
// dispatch loop: for Yul `delegatecall` / `call` / `staticcall`
// where the target argument is a `yul_path` (or bare
// `yul_identifier`) that maps to a Sol state-var / parameter /
// local of Contract or Interface type, emit EdgeInvokes
// ConfAmbiguous with DispatchKind="low_level_call". The resulting
// edge shape is identical to W7.1's Sol-level emit, so downstream
// consumers can treat Yul and Sol low-level calls uniformly.
//
// Argument positions (per Sol EVM Yul spec):
//
//   delegatecall(gas, addr, argIn, argInSize, argOut, argOutSize)
//   call        (gas, addr, value, argIn, argInSize, argOut, argOutSize)
//   staticcall  (gas, addr, argIn, argInSize, argOut, argOutSize)
//
// In all three, the target address is the second positional
// argument (index 1 after the builtin, so the 3rd named child of
// the yul_function_call when counting the builtin itself).
//
// V2 limitations:
//   - The yul_path mapping is shape-based (first identifier). Yul
//     `let` bindings can shadow Sol identifiers; the walker
//     silently emits whichever Sol scope receiver
//     lookupReceiverType resolves to, which may be wrong in the
//     shadow case. Acceptable V2 trade-off — the Yul scope walker
//     would need to track let-bindings to do better.
//   - Address-typed receivers still drop at the byName step
//     (lookupReceiverType returns "address" which isn't a known
//     Contract or Interface type — same as W7.1).
//
// Re-uses resolveLowLevelCallRef via dispatchKindLowLevelCall;
// downstream filtering for "this came from Yul" is recoverable
// via the source node's HasYulBuiltins slice plus the edge line
// landing inside an assembly_statement.

func (v *declVisitor) runYulLowLevelCalls() {
	const q = `(yul_function_call) @call`
	query, qErr := sitter.NewQuery(v.lang, q)
	if qErr != nil {
		return
	}
	defer query.Close()
	cur := sitter.NewQueryCursor()
	defer cur.Close()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		for _, c := range m.Captures {
			if names[c.Index] != "call" {
				continue
			}
			callNode := c.Node
			if callNode.NamedChildCount() < 3 {
				continue
			}
			builtin := callNode.NamedChild(0)
			if builtin == nil || builtin.Kind() != "yul_evm_builtin" {
				continue
			}
			opName := builtin.Utf8Text(v.src)
			switch opName {
			case "delegatecall", "call", "staticcall":
			default:
				continue
			}
			// Target address is the 2nd positional argument; index 2
			// in the named-children list (after builtin + gas arg).
			targetArg := callNode.NamedChild(2)
			if targetArg == nil {
				continue
			}
			receiverName := extractYulReceiverName(targetArg, v.src)
			if receiverName == "" {
				continue
			}
			fnQ, fnStart, ok := nearestFunctionQnameAndStart(&callNode, v.src)
			if !ok {
				continue
			}
			v.pending = append(v.pending, parse.PendingRef{
				SrcID:        parse.MakeID(fnQ, "sol", fnStart),
				EdgeType:     types.EdgeInvokes,
				TargetQName:  receiverName + "|" + opName,
				Line:         int(callNode.StartPosition().Row) + 1,
				ByteOffset:   int(callNode.StartByte()),
				DispatchKind: dispatchKindLowLevelCall,
			})
		}
	}
}

// extractYulReceiverName reads the Sol-identifier text from a Yul
// argument node. Two shapes count:
//
//   yul_path     — its first named child is a yul_identifier
//                   whose text is the leading Sol identifier
//                   (`a` in `a.b.c`). V0 ignores nested-path
//                   segments and resolves on the leading name only.
//   yul_identifier — bare Sol identifier without any dotted path.
//
// Anything else (numeric literal, nested yul_function_call,
// hex literal) drops as unresolvable at the V0 layer.
func extractYulReceiverName(arg *sitter.Node, src []byte) string {
	if arg == nil {
		return ""
	}
	switch arg.Kind() {
	case "yul_identifier":
		return arg.Utf8Text(src)
	case "yul_path":
		if id := arg.NamedChild(0); id != nil && id.Kind() == "yul_identifier" {
			return id.Utf8Text(src)
		}
	}
	return ""
}
