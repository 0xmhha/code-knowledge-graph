package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Sol W6 — `using For` library extension binding detection.
//
// Spec: docs/design/solidity-inheritance-and-interface-dispatch.md §4.6
// Dispatch index: docs/DISPATCH-WITHIN-LANG-SEMANTICS.md §2 Phase 5 W-C W6.
//
// Scope (V0 — Q9-1 (b), Q9-2 (a), Q9-3 (a), 2026-05-12):
//
//	contract Foo { using SafeMath for uint256; }   → EdgeUsesFor (Foo → SafeMath)
//	contract Foo { using SafeMath for *; }         → EdgeUsesFor (same shape)
//	contract Foo {
//	  using SafeMath for uint256;
//	  using Address  for address;
//	}                                              → 2 EdgeUsesFor (Foo→SafeMath, Foo→Address)
//
// Per §4.6 V0 limitations:
//   - free-function form `using {Lib.f1, Lib.f2} for T;` is dropped
//     (separate AST shape; V1 follow-up).
//   - file-level using directive (Solidity 0.8.13+ global binding) is
//     out of scope; V0 only recognises directives nested inside a
//     contract / library / interface body.
//   - typeName is parsed by the grammar but V0 does not expose it on
//     the EdgeUsesFor — one edge per directive regardless of typeName
//     ("`using A for X; using A for Y;` ⇒ two A edges, not deduped").
//
// Method-call dispatch resolution (`balance.add(...)` → SafeMath.add
// EdgeCalls) is V1 follow-up — requires receiver type inference
// infrastructure (state-var / parameter declared-type index).
// See §4.6.6 for the V1 carry-over list.
//
// Pass 1 → Pass 2 split: same idiom as W1 inheritance.
//   - Pass 1 emits PendingRef with DispatchKind="using_for", SrcID
//     hashed off the enclosing container's name identifier (matches
//     emitContractLikeNode's ID derivation in abstract_library.go),
//     TargetQName = libraryName.
//   - Pass 2 (resolveUsingForRef in resolve.go) resolves libraryName
//     against byName[NodeContract] (libraries are emitted as
//     NodeContract+SubKind="library" by W4) and emits one EdgeUsesFor
//     edge per match. same-file → ConfExtracted, cross-file →
//     ConfInferred, unresolved → drop.

// runUsingFor walks every `using_directive` match nested inside a
// contract / library / interface body and queues two PendingRefs per
// directive (V1.0 addition 2026-05-12):
//
//  1. dispatchKindUsingFor (V0) — TargetQName=libraryName. Drives the
//     EdgeUsesFor (Contract → Library) emission in Pass 2.
//  2. dispatchKindUsingForTypeBind (V1.0) — TargetQName encodes
//     `<libraryName>|<typeName>`. Carries the bound type so Pass 2 can
//     build a per-contract binding map for method-call resolution.
//     Does not produce a graph edge — pure side-channel data.
//
// Container identifier comes from the same query capture so both
// PendingRefs' SrcID line up with the container's existing node ID.
func (v *declVisitor) runUsingFor() {
	query, qErr := sitter.NewQuery(v.lang, queryUsingFor)
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
		var containerNode *sitter.Node
		var libNode *sitter.Node
		var typeNode *sitter.Node
		for _, c := range m.Captures {
			switch names[c.Index] {
			case "container":
				n := c.Node
				containerNode = &n
			case "lib":
				n := c.Node
				libNode = &n
			case "type":
				n := c.Node
				typeNode = &n
			}
		}
		if containerNode == nil || libNode == nil {
			continue
		}
		// SrcID must align with the container node emitted by
		// runContractDecl / runLibraryDecl / runInterfaceDecl — all
		// hash on (name, "sol", name-node startByte). containerNode is
		// the same `@name` identifier these emit paths use.
		containerName := containerNode.Utf8Text(v.src)
		containerStart := int(containerNode.StartByte())
		srcID := parse.MakeID(containerName, "sol", containerStart)
		libName := libNode.Utf8Text(v.src)
		line := int(libNode.StartPosition().Row) + 1
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:        srcID,
			EdgeType:     types.EdgeUsesFor,
			TargetQName:  libName,
			Line:         line,
			DispatchKind: dispatchKindUsingFor,
		})
		// V1.0 typebind PendingRef. Source field is either type_name
		// (specific binding) or any_source_type (`for *` wildcard); we
		// normalise both into a string token used as the bind-map key
		// (typeName "*" for wildcard, raw text otherwise — matched
		// against NodeField.Signature in Pass 2).
		typeName := normaliseUsingForType(typeNode, v.src)
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:        srcID,
			EdgeType:     types.EdgeUsesFor, // unused for typebind — Resolve routes by DispatchKind
			TargetQName:  libName + "|" + typeName,
			Line:         line,
			DispatchKind: dispatchKindUsingForTypeBind,
		})
	}
}

// normaliseUsingForType returns the bind-map key for the source field of
// a using_directive. Handles three cases:
//
//   - any_source_type (`*` wildcard) → "*" sentinel.
//   - type_name wrapping primitive_type / user_defined_type → the
//     declared type text (matches NodeField.Signature output of
//     extractTypeNameText).
//   - nil or unknown shape → "" (binding map entry is created but won't
//     match any real receiver — Pass 2 binding lookup naturally drops).
func normaliseUsingForType(typeNode *sitter.Node, src []byte) string {
	if typeNode == nil {
		return ""
	}
	if typeNode.Kind() == "any_source_type" {
		return "*"
	}
	// type_name shape — same idiom as extractTypeNameText so the
	// stored binding key compares 1:1 against NodeField.Signature.
	return extractTypeNameText(typeNode, src)
}

// runUsingForCalls — W6 V1.0 method-call dispatch detector (2026-05-12).
// Scans every member_expression that fits the `<identifier>.<identifier>(...)`
// shape (state-variable receiver, V0 limitation §4.6.6 Q9-2 (a)) and
// queues a PendingRef tagged dispatchKindUsingForCall. Pass 2 resolves
// these against the (contractID, typeName) → libraryName binding map
// built from dispatchKindUsingForTypeBind refs.
//
// Predicate: object is identifier, property is identifier, the
// member_expression's parent (or grandparent through `expression`) is a
// call_expression. Anything else (chained calls, parenthesised receivers,
// type casts) is V1.1 follow-up.
//
// Encoding: TargetQName=`<receiverName>|<methodName>`. Pass 2 splits on
// `|`, resolves receiverName against the state-var name table, joins
// with the binding map.
//
// Note: this runs *in addition to* the existing call-site emission that
// produces NodeCallSite via the body-walk passes. EdgeCalls is added
// only when binding resolution succeeds; mismatched receivers (no state
// var of that name, no binding for the type) silently drop, matching
// the strict-purge policy used by W1/W2/W3.
func (v *declVisitor) runUsingForCalls() {
	const query = `(member_expression) @member`
	q, qErr := sitter.NewQuery(v.lang, query)
	if qErr != nil {
		return
	}
	defer q.Close()
	cur := sitter.NewQueryCursor()
	defer cur.Close()
	matches := cur.Matches(q, v.root, v.src)
	names := q.CaptureNames()
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		for _, c := range m.Captures {
			if names[c.Index] != "member" {
				continue
			}
			memberNode := c.Node
			// W1.0/V1.1: state-variable / parameter receiver. Try first
			// because it's the most common shape.
			if receiverName, methodName, ok := matchStateVarMethodCall(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  receiverName + "|" + methodName,
					Line:         int(memberNode.StartPosition().Row) + 1,
					DispatchKind: dispatchKindUsingForCall,
				})
				continue
			}
			// V1.3: chained call shape `<fn>().<method>(...)`. Inner
			// expression is a plain function call (function-position
			// identifier); resolver looks up the inner function's
			// return type and treats it as the receiver type.
			if innerFnName, methodName, ok := matchChainedMethodCall(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  innerFnName + "|" + methodName,
					Line:         int(memberNode.StartPosition().Row) + 1,
					DispatchKind: dispatchKindUsingForChainCall,
				})
				continue
			}
			// V1.4: cross-contract chained shape `<obj>.<innerFn>().<method>(...)`.
			// Inner expression is a member call on a state-var / parameter
			// receiver. Resolver follows the receiver's contract type to
			// look up the inner function's declaration, then uses that
			// function's return type as the V1.3-style chain receiver.
			if receiverObj, innerFnName, methodName, ok := matchCrossContractChain(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  receiverObj + "|" + innerFnName + "|" + methodName,
					Line:         int(memberNode.StartPosition().Row) + 1,
					DispatchKind: dispatchKindUsingForCrossChainCall,
				})
				continue
			}
			// V1.5: depth-2 chained shape `<innerFn1>().<innerFn2>().<method>(...)`.
			// Each chain link is a plain function call (function-position
			// identifier at the innermost level). Resolver walks two
			// levels of funcReturnTypes to reach the binding type.
			if innerFn1, innerFn2, methodName, ok := matchDeepChainedMethodCall(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  innerFn1 + "|" + innerFn2 + "|" + methodName,
					Line:         int(memberNode.StartPosition().Row) + 1,
					DispatchKind: dispatchKindUsingForDeepChainCall,
				})
				continue
			}
			// V1.6: deep cross-contract chained shape
			// `<obj>.<innerFn1>().<innerFn2>().<method>(...)`. Combines
			// V1.4 (receiver-typed inner method) with V1.5 (depth-2
			// chain). Resolver walks: receiverObj → receiverType →
			// innerFn1 in receiverType → returnType1 → innerFn2 in
			// returnType1 → returnType2 → library binding.
			if recvObj, innerFn1, innerFn2, methodName, ok := matchDeepCrossContractChain(&memberNode, v.src); ok {
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeCalls,
					TargetQName:  recvObj + "|" + innerFn1 + "|" + innerFn2 + "|" + methodName,
					Line:         int(memberNode.StartPosition().Row) + 1,
					DispatchKind: dispatchKindUsingForDeepCrossChainCall,
				})
				continue
			}
		}
	}
}

// matchStateVarMethodCall tests whether a member_expression fits the
// state-variable method-call shape `<identifier>.<identifier>` AND its
// parent context is a call_expression (i.e. it's actually being called,
// not just member-accessed for a property read).
//
// Returns (receiverName, methodName, true) on match. Rejects chained
// shapes (`foo().bar`, `IFoo(x).bar`), member receivers (`a.b.c`), and
// pure property reads (`obj.field` outside a call) — those are V1.1
// follow-up or not in scope.
func matchStateVarMethodCall(member *sitter.Node, src []byte) (string, string, bool) {
	if member == nil {
		return "", "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", false
	}
	object := member.ChildByFieldName("object")
	innerObj := unwrapExpression(object)
	if innerObj == nil || innerObj.Kind() != "identifier" {
		return "", "", false
	}
	// Must be the function-position of a call_expression. The member
	// node is wrapped in an expression node, which is itself a child of
	// the call_expression's `function` field.
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", "", false
	}
	return innerObj.Utf8Text(src), property.Utf8Text(src), true
}

// matchDeepChainedMethodCall — W-C W6 V1.5 (2026-05-12). Tests whether
// a member_expression fits the depth-2 chained shape
// `<innerFn1>().<innerFn2>().<method>(...)`. Each link in the chain is
// a plain function call whose function-position is an identifier; the
// chain unwinds left-to-right by resolving each function's return type.
//
// Returns (innerFn1, innerFn2, method, true) on match.
//
// AST shape:
//
//	call_expression                                  ← outer .method(...)
//	  function: expression
//	    member_expression                            ← outer
//	      object: expression
//	        call_expression                          ← middle .innerFn2(...)
//	          function: expression
//	            member_expression                    ← middle
//	              object: expression
//	                call_expression                  ← inner .innerFn1(...)
//	                  function: expression
//	                    identifier (innerFn1)        ← V1.5 captures this
//	              property: identifier (innerFn2)
//	      property: identifier (method)
//
// Rejects:
//   - `obj.foo().bar().baz()` — innermost identifier is preceded by
//     `obj.` (member_expression). That's V1.6+ (deep cross-contract).
//   - depth >= 3 (`f().g().h().i()`) — outer's chain has one more
//     layer wrapping than V1.5 handles. V1.6+.
//
// Disambiguation from V1.4 (`<obj>.<innerFn>().<method>`): V1.4 has
// inner member_expression.object = identifier (state-var / param); V1.5
// has inner member_expression.object = call_expression (further chain).
// Caller dispatches in order (V1.4 first, then V1.5) so V1.4's predicate
// claims the call when shapes overlap on simpler chains.
func matchDeepChainedMethodCall(member *sitter.Node, src []byte) (string, string, string, bool) {
	if member == nil {
		return "", "", "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", "", false
	}
	object := member.ChildByFieldName("object")
	middleCall := unwrapExpression(object)
	if middleCall == nil || middleCall.Kind() != "call_expression" {
		return "", "", "", false
	}
	middleFn := middleCall.ChildByFieldName("function")
	middleMember := unwrapExpression(middleFn)
	if middleMember == nil || middleMember.Kind() != "member_expression" {
		return "", "", "", false
	}
	middleProperty := middleMember.ChildByFieldName("property")
	if middleProperty == nil || middleProperty.Kind() != "identifier" {
		return "", "", "", false
	}
	middleObject := middleMember.ChildByFieldName("object")
	innerCall := unwrapExpression(middleObject)
	if innerCall == nil || innerCall.Kind() != "call_expression" {
		return "", "", "", false
	}
	innerFn := innerCall.ChildByFieldName("function")
	innerIdent := unwrapExpression(innerFn)
	if innerIdent == nil || innerIdent.Kind() != "identifier" {
		return "", "", "", false
	}
	// Outer must itself be the function-position of a call_expression.
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", "", "", false
	}
	return innerIdent.Utf8Text(src), middleProperty.Utf8Text(src), property.Utf8Text(src), true
}

// matchDeepCrossContractChain — W-C W6 V1.6 (2026-05-12). Tests
// whether a member_expression fits the deep cross-contract chained
// shape `<obj>.<innerFn1>().<innerFn2>().<method>(...)`. Two-link chain
// originating from a state-var / parameter receiver.
//
// Returns (receiverObj, innerFn1, innerFn2, method, true) on match.
//
// AST shape:
//
//	call_expression                                  ← outer .method(...)
//	  function: expression
//	    member_expression                            ← outer
//	      object: expression
//	        call_expression                          ← middle .innerFn2(...)
//	          function: expression
//	            member_expression                    ← middle
//	              object: expression
//	                call_expression                  ← inner .innerFn1(...)
//	                  function: expression
//	                    member_expression            ← inner
//	                      object: expression
//	                        identifier (obj)         ← V1.6 captures
//	                      property: identifier (innerFn1)
//	              property: identifier (innerFn2)
//	      property: identifier (method)
//
// Disambiguation:
//   - V1.4 (`obj.foo().bar()`): outer's chain stops one link shorter —
//     middle.object is identifier, not call_expression.
//   - V1.5 (`foo().bar().baz()`): innermost function-position is
//     identifier, not member_expression on an identifier.
//   - V1.7+ (`obj.foo().bar().baz().qux()`): even one link deeper —
//     outer's object is yet another call_expression wrapping the V1.6
//     pattern.
//
// Caller dispatch order (state-var → V1.3 → V1.4 → V1.5 → V1.6) ensures
// V1.6 only fires on shapes the simpler predicates rejected.
func matchDeepCrossContractChain(member *sitter.Node, src []byte) (string, string, string, string, bool) {
	if member == nil {
		return "", "", "", "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", "", "", false
	}
	object := member.ChildByFieldName("object")
	middleCall := unwrapExpression(object)
	if middleCall == nil || middleCall.Kind() != "call_expression" {
		return "", "", "", "", false
	}
	middleFn := middleCall.ChildByFieldName("function")
	middleMember := unwrapExpression(middleFn)
	if middleMember == nil || middleMember.Kind() != "member_expression" {
		return "", "", "", "", false
	}
	middleProperty := middleMember.ChildByFieldName("property")
	if middleProperty == nil || middleProperty.Kind() != "identifier" {
		return "", "", "", "", false
	}
	middleObject := middleMember.ChildByFieldName("object")
	innerCall := unwrapExpression(middleObject)
	if innerCall == nil || innerCall.Kind() != "call_expression" {
		return "", "", "", "", false
	}
	innerFn := innerCall.ChildByFieldName("function")
	innerMember := unwrapExpression(innerFn)
	if innerMember == nil || innerMember.Kind() != "member_expression" {
		return "", "", "", "", false
	}
	innerObj := innerMember.ChildByFieldName("object")
	innerObjIdent := unwrapExpression(innerObj)
	if innerObjIdent == nil || innerObjIdent.Kind() != "identifier" {
		return "", "", "", "", false
	}
	innerProperty := innerMember.ChildByFieldName("property")
	if innerProperty == nil || innerProperty.Kind() != "identifier" {
		return "", "", "", "", false
	}
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", "", "", "", false
	}
	return innerObjIdent.Utf8Text(src),
		innerProperty.Utf8Text(src),
		middleProperty.Utf8Text(src),
		property.Utf8Text(src),
		true
}

// matchCrossContractChain — W-C W6 V1.4 (2026-05-12). Tests whether a
// member_expression fits the cross-contract chained shape
// `<obj>.<innerFn>().<method>` where the outer expression is invoking
// `<method>` on the return value of a method call on a state-var /
// parameter receiver.
//
// Returns (receiverObjName, innerFnName, methodName, true) on match.
//
// AST shape (verified via probe):
//
//	call_expression                                ← outer .method(...)
//	  function: expression
//	    member_expression                          ← outer
//	      object: expression
//	        call_expression                        ← inner .innerFn(...)
//	          function: expression
//	            member_expression                  ← inner
//	              object: expression
//	                identifier (receiverObjName)   ← state var / param
//	              property: identifier (innerFnName)
//	      property: identifier (methodName)
//
// Rejects:
//   - `factory().bar()` — inner function-position is identifier, not
//     member_expression (handled by matchChainedMethodCall, V1.3).
//   - `obj.field.bar()` — inner is just member access without call
//     (V1.x property-chain support).
//   - Deeper chains like `obj.foo().baz().bar()` — outer's object is
//     itself a call_expression whose function is a member_expression on
//     another call_expression. V1.5+ recursive chains.
//
// Edge case: the receiverObjName might also match an interface variable
// (`IFoo iface; iface.fn()`) — the resolver re-checks against
// stateVarTypes / paramTypes, where the typeName recorded by V1.0/V1.1
// would be "IFoo". The resolver step 3 maps that to the interface's
// declared methods.
func matchCrossContractChain(member *sitter.Node, src []byte) (string, string, string, bool) {
	if member == nil {
		return "", "", "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", "", false
	}
	object := member.ChildByFieldName("object")
	innerCall := unwrapExpression(object)
	if innerCall == nil || innerCall.Kind() != "call_expression" {
		return "", "", "", false
	}
	innerFn := innerCall.ChildByFieldName("function")
	innerMember := unwrapExpression(innerFn)
	if innerMember == nil || innerMember.Kind() != "member_expression" {
		return "", "", "", false
	}
	innerObj := innerMember.ChildByFieldName("object")
	innerObjIdent := unwrapExpression(innerObj)
	if innerObjIdent == nil || innerObjIdent.Kind() != "identifier" {
		return "", "", "", false
	}
	innerProperty := innerMember.ChildByFieldName("property")
	if innerProperty == nil || innerProperty.Kind() != "identifier" {
		return "", "", "", false
	}
	// Outer must be a call_expression (the chained `.method()` is itself
	// invoked).
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", "", "", false
	}
	return innerObjIdent.Utf8Text(src), innerProperty.Utf8Text(src), property.Utf8Text(src), true
}

// matchChainedMethodCall — W-C W6 V1.3 (2026-05-12). Tests whether a
// member_expression fits the chained-call shape `<fn>(...).<method>`
// where the inner expression is a *plain function call* (bare
// identifier in the function position), not a type cast.
//
// Returns (innerFnName, methodName, true) on match.
//
// Shape (verified via AST dump):
//
//	call_expression                       ← outer .method(...)
//	  function: expression
//	    member_expression
//	      object: expression
//	        call_expression               ← inner fn() call
//	          function: expression
//	            identifier (innerFnName)  ← what V1.3 captures
//	      property: identifier (methodName)
//
// Rejects shapes already covered or out of scope for V1.3:
//   - `IFoo(addr).bar()` (W3 interface dispatch — inner identifier is
//     a TYPE name; V3's matchInterfaceDispatch handles this).
//   - `obj.foo().bar()` (member-receiver chain — inner function-position
//     is itself a member_expression, not a bare identifier). V1.4+.
//   - `obj.field.bar()` (pure property access chain). V1.4+.
//
// V1.3 vs W3 disambiguation: the runUsingForCalls walker calls this
// AFTER matchStateVarMethodCall has rejected. To avoid emitting both a
// W3 EdgeInvokes and a V1.3 EdgeCalls for the same site, callers should
// also verify the resolved inner identifier maps to a *function* (not
// an interface) — that's Pass 2's job via funcByQName, so the parser
// emits the chained PendingRef unconditionally and the resolver drops
// when no funcID matches.
func matchChainedMethodCall(member *sitter.Node, src []byte) (string, string, bool) {
	if member == nil {
		return "", "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", false
	}
	object := member.ChildByFieldName("object")
	innerCall := unwrapExpression(object)
	if innerCall == nil || innerCall.Kind() != "call_expression" {
		return "", "", false
	}
	innerFn := innerCall.ChildByFieldName("function")
	innerIdent := unwrapExpression(innerFn)
	if innerIdent == nil || innerIdent.Kind() != "identifier" {
		return "", "", false
	}
	// Outer must be a call_expression (the chained `.method()` is itself
	// invoked, not just read as a property).
	parent := member.Parent()
	if parent != nil && parent.Kind() == "expression" {
		parent = parent.Parent()
	}
	if parent == nil || parent.Kind() != "call_expression" {
		return "", "", false
	}
	return innerIdent.Utf8Text(src), property.Utf8Text(src), true
}

// dispatchKindUsingFor tags PendingRefs originating from W6 V0 using-for
// binding detection. String literal matches the existing idiom (W1
// "inherit", W2 "override"/"override_explicit", W3 "interface_dispatch").
const dispatchKindUsingFor = "using_for"

// dispatchKindUsingForTypeBind (V1.0) carries the bound-type information
// for binding-map construction. Does not produce a graph edge — Pass 2
// reads these to populate (contractID, typeName) → libraryName map.
const dispatchKindUsingForTypeBind = "using_for_typebind"

// dispatchKindUsingForCall (V1.0) tags PendingRefs that resolve to
// EdgeCalls (caller function → library function) once the binding map
// has been built. TargetQName encodes `<receiverName>|<methodName>`.
const dispatchKindUsingForCall = "using_for_call"

// dispatchKindUsingForChainCall (V1.3) tags PendingRefs for chained
// call dispatch: `<fn>().<method>(...)`. TargetQName encodes
// `<innerFnName>|<methodName>` — same shape as using_for_call but the
// receiver lookup goes through funcReturnTypes instead of stateVarTypes
// / paramTypes (Pass 2 splits the resolver paths by DispatchKind).
const dispatchKindUsingForChainCall = "using_for_chain_call"

// dispatchKindUsingForCrossChainCall (V1.4) tags PendingRefs for
// cross-contract chained dispatch: `<obj>.<innerFn>().<method>(...)`.
// TargetQName encodes `<receiverObjName>|<innerFnName>|<methodName>`
// (three parts, '|'-separated). Pass 2 splits the chain across
// stateVarTypes / paramTypes (obj→type) + byName (type→contract) +
// funcByQName (contract.innerFn→funcID) + funcReturnTypes (funcID→
// returnType) + bindings (callerContractID, returnType→library).
const dispatchKindUsingForCrossChainCall = "using_for_cross_chain_call"

// dispatchKindUsingForDeepChainCall (V1.5) tags PendingRefs for depth-2
// chained dispatch: `<innerFn1>().<innerFn2>().<method>(...)`.
// TargetQName encodes `<innerFn1>|<innerFn2>|<method>` (three parts).
// Resolver walks two levels of funcReturnTypes (innerFn1 → returnType1
// → resolve innerFn2 in returnType1's namespace → returnType2 → binding
// lookup on returnType2) before reaching the library function.
const dispatchKindUsingForDeepChainCall = "using_for_deep_chain_call"

// dispatchKindUsingForDeepCrossChainCall (V1.6) tags PendingRefs for
// the deep cross-contract chained dispatch
// `<obj>.<innerFn1>().<innerFn2>().<method>(...)`. TargetQName encodes
// `<obj>|<innerFn1>|<innerFn2>|<method>` (four parts). Resolver
// combines V1.4 (receiver type lookup) with V1.5 (depth-2 return
// chain) — 8-step total chain.
const dispatchKindUsingForDeepCrossChainCall = "using_for_deep_cross_chain_call"
