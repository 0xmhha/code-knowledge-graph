package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Sol W-C W10 V2 (2026-05-18) / V3 (2026-05-19) — Yul-level
// low-level call receiver resolution and shadow guard.
//
// V0 marked HasAssembly on callables with inline assembly; V1.1
// enumerated which Yul EVM builtins appeared. V2 closes the
// dispatch loop: for Yul `delegatecall` / `call` / `staticcall`
// where the target argument is a `yul_path` (or bare
// `yul_identifier`) that maps to a Sol state-var / parameter /
// local of Contract or Interface type, emit EdgeInvokes
// ConfAmbiguous with DispatchKind="low_level_call".
//
// V3 (2026-05-19) closes the two soft spots V2 left:
//
//   (1) Yul let-binding shadow guard. A yul-local `let r := ...`
//       declaration shadows a Sol-scope identifier of the same
//       name inside the assembly_statement. The walker collects
//       all in-scope yul_variable_declaration names from the
//       enclosing yul_blocks and skips the emit when the receiver
//       identifier matches — avoiding silent wrong-target edges.
//
//   (2) HasLowLevelCall marker on the enclosing callable for every
//       yul delegatecall / call / staticcall regardless of whether
//       the receiver argument resolves to a known Sol scope. This
//       lights up the W8 V1 marker for Yul-side calls the same way
//       Sol-side `.call` invocations do, so security tooling can
//       run "which functions perform a low-level call" queries
//       across both surfaces with one signal.
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
// V3 limitations remaining:
//   - Address-typed receivers still drop at the byName step
//     (lookupReceiverType returns "address" which isn't a known
//     Contract or Interface type — same as W7.1). The W8 V1
//     marker is the substitute signal for those receivers.
//   - The let-binding sweep collects siblings of every yul_block
//     ancestor; nested-block shadowing where a later block
//     re-binds the same name is captured conservatively (the call
//     always drops if the name appears anywhere in scope).

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
	hasLowLevel := map[string]bool{}
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
			fnQ, fnStart, ok := nearestFunctionQnameAndStart(&callNode, v.src)
			if !ok {
				continue
			}
			fnID := parse.MakeID(fnQ, "sol", fnStart)
			// V3: mark the enclosing callable regardless of whether the
			// receiver argument resolves. This mirrors the W8 V1
			// HasLowLevelCall semantics on the Sol side.
			hasLowLevel[fnID] = true
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
			// V3 shadow guard: if the receiver identifier matches a
			// yul let-binding in scope, drop the emit. The let binding
			// shadows any same-name Sol identifier; without this guard
			// the Pass 2 resolver would silently match a Sol scope
			// receiver of the same name and produce a wrong edge.
			if yulLetBindingsInScope(&callNode, v.src)[receiverName] {
				continue
			}
			v.pending = append(v.pending, parse.PendingRef{
				SrcID:        fnID,
				EdgeType:     types.EdgeInvokes,
				TargetQName:  receiverName + "|" + opName,
				Line:         int(callNode.StartPosition().Row) + 1,
				ByteOffset:   int(callNode.StartByte()),
				DispatchKind: dispatchKindLowLevelCall,
			})
		}
	}
	if len(hasLowLevel) == 0 {
		return
	}
	for i := range v.nodes {
		if hasLowLevel[v.nodes[i].ID] {
			v.nodes[i].HasLowLevelCall = true
		}
	}
}

// yulLetBindingsInScope walks the parent chain of a yul node up to
// the enclosing assembly_statement and returns every yul_identifier
// declared by a yul_variable_declaration (`let <names> := ...`) that
// could shadow a Sol-scope identifier at the node's position. The
// sweep is conservative: it includes let-bindings declared in
// sibling positions after the node (Yul's per-block scoping makes
// that strictly out-of-scope) since the W10 V3 contract is to drop
// rather than risk a silent wrong edge.
func yulLetBindingsInScope(n *sitter.Node, src []byte) map[string]bool {
	bindings := map[string]bool{}
	if n == nil {
		return bindings
	}
	for cur := n.Parent(); cur != nil; cur = cur.Parent() {
		if cur.Kind() == "assembly_statement" {
			collectYulLetBindings(cur, src, bindings)
			break
		}
	}
	return bindings
}

// collectYulLetBindings descends a Yul subtree and adds every
// LHS-identifier of a yul_variable_declaration to the bindings set.
// Multi-binding form `let a, b := f()` contributes both names; the
// scan stops at the first non-yul_identifier child to skip the RHS
// expression's referenced identifiers.
func collectYulLetBindings(n *sitter.Node, src []byte, out map[string]bool) {
	if n == nil {
		return
	}
	if n.Kind() == "yul_variable_declaration" {
		for j := uint(0); j < n.NamedChildCount(); j++ {
			child := n.NamedChild(j)
			if child == nil {
				continue
			}
			if child.Kind() != "yul_identifier" {
				break
			}
			out[child.Utf8Text(src)] = true
		}
		return
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		collectYulLetBindings(n.NamedChild(i), src, out)
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
