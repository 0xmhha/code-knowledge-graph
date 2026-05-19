package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
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
			affected[parse.MakeID(fnQ, "sol", fnStart)] = true
		}
	}
	if len(affected) == 0 {
		return
	}
	for i := range v.nodes {
		if affected[v.nodes[i].ID] {
			v.nodes[i].HasFunctionTypedVar = true
		}
	}
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
