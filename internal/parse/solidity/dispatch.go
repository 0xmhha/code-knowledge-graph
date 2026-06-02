package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Sol W3 — Interface-typed dynamic dispatch (`IFoo(addr).bar(...)`).
//
// Spec: docs/design/solidity-inheritance-and-interface-dispatch.md §3.4, §4.3
// Dispatch index: docs/DISPATCH-WITHIN-LANG-SEMANTICS.md §2 Phase 5 W-C W3.
//
// Scope: the canonical Solidity idiom for calling a method on an
// externally-supplied contract address through a known interface type:
//
//	IERC20(tokenAddr).transfer(to, amount);   // basic dispatch
//	IERC20(addr).balanceOf(IBar(other).owner()); // nested / chained
//	helper.thing(IFoo(addr).foo());           // inner-only dispatch
//
// AST shape (verified via TestDispatchASTDump 2026-05-11):
//
//	call_expression                  ← the outer .bar(...) call
//	  .function = expression
//	    member_expression
//	      .object   = expression
//	        call_expression           ← the inner IFoo(addr) type cast
//	          .function = expression
//	            identifier "IFoo"     ← the interface identifier
//	          call_argument           ← the address being cast
//	      .property = identifier "bar"  ← the method being invoked
//	  call_argument(s)                ← arguments forwarded to bar(...)
//
// Per §5.0 Q5 (decided 2026-05-11):
//   - All resolved interface-dispatch edges land at ConfAmbiguous, *regardless*
//     of whether the resolution crossed a file boundary or any implementer
//     could be located. Rationale: the dispatch target is determined by the
//     runtime address, not the declared interface — graph-level resolution
//     can at best identify the *interface method* declaration, never the
//     concrete implementation. The `llmSafeStoreReader` wrapper already
//     filters AMBIGUOUS edges from LLM-facing surfaces (hunk-graph §11.3),
//     so this confidence label keeps the data lossless for human inspection
//     without contaminating model context.
//   - Unresolved type names (identifier does not match any known
//     NodeInterface) are dropped (V0 — same strict purge policy as W1/W2;
//     keeps the AMBIGUOUS bucket scoped to *real* interface dispatch only).
//
// Out of scope for W3 (separate dispatches):
//   - Plain contract-typed casts `MyContract(addr).foo()` — solc accepts
//     these but they are not interface dispatch; covered in a future
//     spec when contract-type tracking is added.
//   - `using For` library extension (W6).
//   - `super.foo()` body walk — declarative override edges (W2) cover the
//     declaration-time relationship; super-call resolution is a separate
//     dispatch that shares this detector's enclosing-function lookup.
//   - Low-level `addr.call(...)`, `delegatecall`, `staticcall` — out of
//     scope per spec §0.

// runDispatch walks every `member_expression` that fits the
// `Type(args).method` shape and queues a PendingRef tagged with
// dispatchKindInterfaceDispatch. The Pass 2 resolver only emits an
// EdgeInvokes when the leading identifier resolves to a NodeInterface
// in the indexed name table — plain identifiers, primitive type casts
// (`address(x)`), and `super.foo()` (object is identifier, not call) are
// all naturally filtered out by the AST-shape predicate without an
// explicit blocklist.
//
// We walk member_expression nodes (rather than call_expression) so that
// chained patterns like `IFoo(addr).getX().bar()` are not double-emitted:
// the inner `IFoo(addr).getX` *is* a Type(args).method pattern (queued as
// one ref), but the outer `.bar` is a method call on the *result* of
// `getX()` — object is `call_expression` whose function is `member_expression`,
// not `identifier` — so the predicate naturally rejects it. Verified
// against the chained_dispatch fixture.
func (v *declVisitor) runDispatch() {
	// Query every member_expression once. Filtering by AST shape happens
	// in Go because tree-sitter's predicate vocabulary cannot express
	// "object is a call_expression whose function is an identifier"
	// without nesting that the JoranHonig grammar's quantifier rules
	// reject (tested 2026-05-11 — `(call_expression function: (...))`
	// inside a member_expression's object field constraint fails to
	// compile against this grammar's S-expression schema).
	const query = `(member_expression) @member`
	q, qErr := sitter.NewQuery(v.lang, query)
	if qErr != nil {
		return
	}
	defer func() { q.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
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
			typeName, methodName, ok := matchInterfaceDispatch(&memberNode, v.src)
			if !ok {
				continue
			}
			fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
			if !fnOK {
				// Top-level free function / file-scoped expression — no
				// enclosing function to attribute the call to. V0 drops
				// these; real Sol code keeps dispatch inside functions.
				continue
			}
			v.pending = append(v.pending, parse.PendingRef{
				SrcID:        parse.MakeID(fnQ, "sol", fnStart),
				EdgeType:     types.EdgeInvokes,
				TargetQName:  typeName + "." + methodName,
				Line:         int(memberNode.StartPosition().Row) + 1,
				DispatchKind: dispatchKindInterfaceDispatch,
			})
		}
	}
}

// matchInterfaceDispatch tests whether a member_expression node fits the
// `TypeName(...).methodName` shape and returns (typeName, methodName,
// true) on match.
//
// Predicate (must satisfy all):
//  1. member.property is a plain `identifier` (the method name).
//  2. member.object descends through `expression` to a `call_expression`
//     (the type cast). The expression wrapper is the grammar's
//     left-recursion handler — every value position is wrapped in one.
//  3. The cast call's `function` field descends through `expression` to a
//     plain `identifier` (the type name). Rejects `lib.IFoo(addr).bar()`
//     (member_expression there → not interface dispatch in V0;
//     qualified-name support tracked alongside W1's known limitation).
//
// Returns ok=false for any shape mismatch — callers should not emit an
// edge in that case.
func matchInterfaceDispatch(member *sitter.Node, src []byte) (string, string, bool) {
	if member == nil {
		return "", "", false
	}
	property := member.ChildByFieldName("property")
	if property == nil || property.Kind() != "identifier" {
		return "", "", false
	}
	object := member.ChildByFieldName("object")
	// The grammar wraps every value-position child in an `expression`
	// node (it has no inline expression rule); unwrap one level so we
	// can pattern-match the inner call.
	innerCall := unwrapExpression(object)
	if innerCall == nil || innerCall.Kind() != "call_expression" {
		return "", "", false
	}
	callFn := innerCall.ChildByFieldName("function")
	innerIdent := unwrapExpression(callFn)
	if innerIdent == nil || innerIdent.Kind() != "identifier" {
		return "", "", false
	}
	// All three predicates passed. Pull the text of the identifiers —
	// these are guaranteed non-empty by the grammar (identifier rule
	// requires at least one character).
	return innerIdent.Utf8Text(src), property.Utf8Text(src), true
}

// unwrapExpression peels one layer of `expression` wrapper off a node.
// The JoranHonig grammar inserts an `expression` node between most
// value-position parents and their actual operand; matching code is
// cleaner if we strip that layer at the boundary.
//
// Returns the input unchanged when it is nil or not an `expression`
// (some grammar positions skip the wrapper for trivial leaves).
func unwrapExpression(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if n.Kind() != "expression" {
		return n
	}
	if n.NamedChildCount() == 0 {
		return n
	}
	c := n.NamedChild(0)
	return c
}

// dispatchKindInterfaceDispatch tags PendingRefs that originate from W3
// interface dispatch detection so resolveInterfaceDispatch in Pass 2 can
// identify and process them. String constant for consistency with W1
// (dispatchKindInherit) and W2 (dispatchKindOverride / dispatchKindOverrideExplicit).
const dispatchKindInterfaceDispatch = "interface_dispatch"
