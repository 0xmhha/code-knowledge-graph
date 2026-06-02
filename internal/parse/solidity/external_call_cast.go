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
	defer func() { query.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	affected := map[string]bool{}
	// W-C W10 V8 (2026-05-19): track self-reference casts
	// separately so HasSelfReentrantCall takes precedence over
	// HasExternalCall when the cast argument is `this`.
	// W-C W10 V9 (2026-05-19): additionally track self casts whose
	// method is `delegatecall` — `address(this).delegatecall(...)`
	// is effectively dead code and warrants a separate marker.
	selfAffected := map[string]bool{}
	selfDelegatecallDead := map[string]bool{}
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
			methodName := property.Utf8Text(v.src)
			// W-C W10 V12 (2026-05-19): also accept value-
			// transfer methods (send / transfer) for the self-
			// cast detection. `payable(this).transfer(value)`
			// is a self-reentrant surface even though the
			// existing low-level call branch wouldn't admit it.
			// Arbitrary-address transfers (`payable(target)
			// .transfer(...)`) stay on the V5 HasValueTransfer
			// path and don't trigger HasExternalCall — the
			// value-transfer marker is its own signal.
			if !isLowLevelMethod(methodName) && !isValueTransferMethod(methodName) {
				continue
			}
			parent := member.Parent()
			if parent != nil && parent.Kind() == "expression" {
				parent = parent.Parent()
			}
			// W-C W10 V18 (2026-05-21): Sol 0.7+ writes value / gas
			// overrides as `.call{value: x, gas: y}(...)`. In the
			// tree-sitter grammar that wraps the `.call` member into a
			// struct_expression (with another expression node above the
			// struct on the parent chain) before reaching the outer
			// call_expression. The chain is:
			//
			//   member_expression           (`payable(this).call`)
			//   → expression
			//   → struct_expression         (`.call{value: x}`)
			//   → expression
			//   → call_expression           (`.call{value: x}(args)`)
			//
			// Without these hops the walker bails at the call_expression
			// check and the security marker silently disappears on every
			// modern self-call — a false negative on the syntax cks
			// consumers actually ship today.
			if parent != nil && parent.Kind() == "struct_expression" {
				parent = parent.Parent()
				if parent != nil && parent.Kind() == "expression" {
					parent = parent.Parent()
				}
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
			id := parse.MakeID(fnQ, "sol", fnStart)
			if isSelfCast(inner, v.src) {
				selfAffected[id] = true
				if methodName == "delegatecall" {
					selfDelegatecallDead[id] = true
				}
			} else if isLowLevelMethod(methodName) {
				// Arbitrary-address low-level call (V5 path).
				// Arbitrary-address value-transfer is already
				// covered by W8 V1's HasValueTransfer marker
				// — don't double-count it on HasExternalCall.
				affected[id] = true
			}
		}
	}
	if len(affected) == 0 && len(selfAffected) == 0 {
		return
	}
	for i := range v.nodes {
		if affected[v.nodes[i].ID] {
			v.nodes[i].HasExternalCall = true
		}
		if selfAffected[v.nodes[i].ID] {
			v.nodes[i].HasSelfReentrantCall = true
		}
		if selfDelegatecallDead[v.nodes[i].ID] {
			v.nodes[i].HasSelfDelegatecallDead = true
		}
	}
}

// isSelfCast reports whether a cast wrapper's argument is the
// `this` keyword (a bare identifier or wrapped expression
// resolving to `this`). The cast `payable(this).call(...)` /
// `address(this).call(...)` re-enters the same contract, so the
// security signal is reentrancy (HasSelfReentrantCall) rather
// than arbitrary-address dispatch (HasExternalCall).
func isSelfCast(castNode *sitter.Node, src []byte) bool {
	if castNode == nil {
		return false
	}
	// Find the cast's single call_argument child.
	for i := uint(0); i < castNode.NamedChildCount(); i++ {
		child := castNode.NamedChild(i)
		if child == nil || child.Kind() != "call_argument" {
			continue
		}
		arg := unwrapExpression(child.NamedChild(0))
		if arg == nil {
			return false
		}
		// Sol's `this` is exposed as identifier "this" in the AST.
		if arg.Kind() == "identifier" && arg.Utf8Text(src) == "this" {
			return true
		}
		return false
	}
	return false
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
