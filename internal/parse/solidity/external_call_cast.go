package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
)

// Sol W-C W10 V5 (2026-05-19) — cast / wrapper shape detection
// for HasExternalCall.
//
// W10 V4 lit up HasExternalCall on the enclosing callable when a
// bare-identifier receiver (`target.call(...)`) resolved to an
// address-typed Sol scope variable through resolveLowLevelCallRef.
// Sol code frequently uses cast wrappers around the receiver:
//
//	address(t).call(data)
//	payable(t).call(data)
//	address(uint160(uint256(slot))).delegatecall(data)
//
// The cast expression always evaluates to an address (or address
// payable), so by construction these are arbitrary-address dispatch
// surfaces regardless of the inner argument's static type. V5
// detects the cast shape directly and marks HasExternalCall on the
// enclosing callable without going through receiver-type
// resolution.
//
// Shape (per tree-sitter-solidity v1.2.11 AST):
//
//	member_expression
//	  property: identifier "call" | "delegatecall" | "staticcall"
//	  object: expression
//	    type_cast_expression            ← `address(...)` cast
//	      primitive_type "address"
//	      call_argument …
//	  | expression
//	    payable_conversion_expression   ← `payable(...)` cast
//	      call_argument …
//
// The walker doesn't reach into the cast's argument; the cast itself
// is sufficient evidence of arbitrary-address dispatch.

func (v *declVisitor) runExternalCallCastMarker() {
	const q = `(member_expression) @member`
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
			if names[c.Index] != "member" {
				continue
			}
			member := c.Node
			property := member.ChildByFieldName("property")
			if property == nil || property.Kind() != "identifier" {
				continue
			}
			if !isLowLevelMethod(property.Utf8Text(v.src)) {
				continue
			}
			parent := member.Parent()
			if parent != nil && parent.Kind() == "expression" {
				parent = parent.Parent()
			}
			if parent == nil || parent.Kind() != "call_expression" {
				continue
			}
			object := member.ChildByFieldName("object")
			inner := unwrapExpression(object)
			if !isAddressCastCall(inner, v.src) {
				continue
			}
			fnQ, fnStart, ok := nearestFunctionQnameAndStart(&member, v.src)
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
			v.nodes[i].HasExternalCall = true
		}
	}
}

// isAddressCastCall reports whether n is one of Sol's address-cast
// wrapper shapes — `type_cast_expression` with a primitive_type
// "address" first child (covers `address(...)`) or
// `payable_conversion_expression` (covers `payable(...)`). Either
// wrapper produces an address (or address payable) at the outer
// call site regardless of the inner argument's static type.
func isAddressCastCall(n *sitter.Node, src []byte) bool {
	if n == nil {
		return false
	}
	switch n.Kind() {
	case "payable_conversion_expression":
		return true
	case "type_cast_expression":
		// First named child of a type_cast_expression is the target
		// type; only `address(...)` qualifies for V5.
		first := n.NamedChild(0)
		if first == nil {
			return false
		}
		if first.Kind() == "primitive_type" {
			return first.Utf8Text(src) == "address"
		}
		// Sol grammar occasionally classifies the address keyword
		// as a bare identifier in cast position — accept both.
		if first.Kind() == "identifier" {
			return first.Utf8Text(src) == "address"
		}
	}
	return false
}
