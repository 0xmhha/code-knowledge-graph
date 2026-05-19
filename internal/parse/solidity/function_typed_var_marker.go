package solidity

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Sol W-C W8 V3 (2026-05-19) — function-typed parameter / local marker.
//
// W8 V2 set IsFunctionTyped on NodeField for state variables declared
// with a function type. V3 extends the signal to callables that
// receive function-typed values via parameter or local variable.
//
// Detection shape (matches W8 V2):
//
//	parameter | variable_declaration
//	  type: type_name
//	    parameter         ← function-type signature input
//	    return_parameter  ← function-type signature output
//
// The walker queries every `parameter` and `variable_declaration` node,
// inspects its `type` field for a parameter/return_parameter child
// (the function-type signature shape Sol grammar emits in place of
// the usual primitive_type / user_defined_type), then walks parents
// up to the enclosing function/modifier and sets HasFunctionTypedVar
// on the corresponding Node row.
//
// Why mark the containing callable, not the parameter itself:
// parameters and locals aren't first-class graph nodes in V0
// (paramTypes / localVarTypes are side-channel maps consumed by Pass 2
// for dispatch resolution, not edges). The marker on the enclosing
// callable lets security tooling answer "which functions accept or
// allocate function pointers?" without re-parsing source.
//
// The nested parameters inside a function-type signature itself (the
// signature's input/output types) carry primitive_type / user_defined_type
// children rather than parameter/return_parameter, so they don't trigger
// the marker even though the outer query matches them.

func (v *declVisitor) runFunctionTypedVarMarker() {
	const q = `[(parameter) (variable_declaration)] @v`
	query, qErr := sitter.NewQuery(v.lang, q)
	if qErr != nil {
		return
	}
	defer query.Close()
	cur := sitter.NewQueryCursor()
	defer cur.Close()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	// fnByID: enclosing function/modifier ID → set of identifier
	// names this callable declares as function-typed (param or local).
	// Used in the second pass to mark HasFunctionPointerCall on the
	// same callable when a call_expression invokes one of these names.
	fnByID := map[string]map[string]bool{}
	affected := map[string]bool{}
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		for _, c := range m.Captures {
			if names[c.Index] != "v" {
				continue
			}
			node := c.Node
			typeNode := node.ChildByFieldName("type")
			if typeNode == nil {
				continue
			}
			if !typeNameIsFunctionTyped(typeNode) {
				continue
			}
			fnQ, fnStart, ok := nearestFunctionQnameAndStart(&node, v.src)
			if !ok {
				continue
			}
			fnID := parse.MakeID(fnQ, "sol", fnStart)
			affected[fnID] = true
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				if fnByID[fnID] == nil {
					fnByID[fnID] = map[string]bool{}
				}
				fnByID[fnID][nameNode.Utf8Text(v.src)] = true
			}
		}
	}
	// W-C W8 V5 (2026-05-19): collect contract-scope function-typed
	// state variables so a call_expression whose callee resolves to
	// a state-var name (`handler(x)` inside the same contract) also
	// lights up the marker.
	stateVarByContract := v.collectFunctionTypedStateVars()
	pointerCallers := v.findFunctionPointerCallers(fnByID, stateVarByContract)
	if len(affected) == 0 && len(pointerCallers) == 0 {
		return
	}
	for i := range v.nodes {
		if affected[v.nodes[i].ID] {
			v.nodes[i].HasFunctionTypedVar = true
		}
		if pointerCallers[v.nodes[i].ID] {
			v.nodes[i].HasFunctionPointerCall = true
		}
	}
}

// collectFunctionTypedStateVars returns (contractName -> set of
// state-var names declared with a function type), built from the
// IsFunctionTyped flag W8 V2 stamps on NodeField rows. Used by the
// W8 V5 caller walk to recognise `someStateVar(args)` invocations
// inside contract methods as function-pointer calls.
func (v *declVisitor) collectFunctionTypedStateVars() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, n := range v.nodes {
		if n.Type != types.NodeField || !n.IsFunctionTyped {
			continue
		}
		dot := strings.IndexByte(n.QualifiedName, '.')
		if dot <= 0 || dot == len(n.QualifiedName)-1 {
			continue
		}
		contract := n.QualifiedName[:dot]
		name := n.QualifiedName[dot+1:]
		if out[contract] == nil {
			out[contract] = map[string]bool{}
		}
		out[contract][name] = true
	}
	return out
}

// findFunctionPointerCallers — W-C W8 V4/V5. Walks every
// call_expression whose callee unwraps to a bare identifier and
// checks whether the identifier matches:
//
//   - a function-typed parameter or local declared in the enclosing
//     callable (V4 — per-function fnByID set), OR
//   - a function-typed state variable declared on the enclosing
//     contract (V5 — per-contract stateVarByContract set).
//
// Returns the set of callable IDs that perform at least one
// function-pointer invocation. Bare-identifier callees only —
// state-var dispatch through `obj.field(args)` is still out of
// scope (no member_expression handling) since IsFunctionTyped at
// contract scope means the variable is addressable as a bare name
// from any method on that contract.
func (v *declVisitor) findFunctionPointerCallers(
	fnByID map[string]map[string]bool,
	stateVarByContract map[string]map[string]bool,
) map[string]bool {
	if len(fnByID) == 0 && len(stateVarByContract) == 0 {
		return nil
	}
	const q = `(call_expression) @call`
	query, qErr := sitter.NewQuery(v.lang, q)
	if qErr != nil {
		return nil
	}
	defer query.Close()
	cur := sitter.NewQueryCursor()
	defer cur.Close()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	out := map[string]bool{}
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
			ident := bareIdentifierCallee(&callNode)
			if ident == nil {
				continue
			}
			fnQ, fnStart, ok := nearestFunctionQnameAndStart(&callNode, v.src)
			if !ok {
				continue
			}
			fnID := parse.MakeID(fnQ, "sol", fnStart)
			name := ident.Utf8Text(v.src)
			if pool, has := fnByID[fnID]; has && pool[name] {
				out[fnID] = true
				continue
			}
			contract := nearestContractName(&callNode, v.src)
			if contract != "" {
				if pool, has := stateVarByContract[contract]; has && pool[name] {
					out[fnID] = true
				}
			}
		}
	}
	return out
}

// bareIdentifierCallee returns the callee identifier of a
// call_expression when the callee is a bare identifier (the
// function-pointer invocation shape `local(args)`). Returns nil for
// member_expression callees (`obj.method(args)`), parenthesised /
// chained shapes, and type-cast wrappers — those are out of scope
// for V4 since they don't directly target a function-typed param /
// local declared in the same callable.
func bareIdentifierCallee(callNode *sitter.Node) *sitter.Node {
	if callNode == nil {
		return nil
	}
	callee := callNode.ChildByFieldName("function")
	if callee == nil {
		return nil
	}
	for callee != nil && callee.Kind() == "expression" {
		if callee.NamedChildCount() == 0 {
			return nil
		}
		callee = callee.NamedChild(0)
	}
	if callee != nil && callee.Kind() == "identifier" {
		return callee
	}
	return nil
}

// typeNameIsFunctionTyped checks whether a type_name AST node carries
// the Sol grammar shape for a function type — a `parameter` or
// `return_parameter` named child. Mirrors the inline check in
// runStateVarDecl (W8 V2) so both walkers agree on what counts as
// function-typed.
func typeNameIsFunctionTyped(typeNode *sitter.Node) bool {
	if typeNode == nil {
		return false
	}
	for i := uint(0); i < typeNode.NamedChildCount(); i++ {
		child := typeNode.NamedChild(i)
		if child == nil {
			continue
		}
		if child.Kind() == "parameter" || child.Kind() == "return_parameter" {
			return true
		}
	}
	return false
}
