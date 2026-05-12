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
			receiverName, methodName, ok := matchStateVarMethodCall(&memberNode, v.src)
			if !ok {
				continue
			}
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
