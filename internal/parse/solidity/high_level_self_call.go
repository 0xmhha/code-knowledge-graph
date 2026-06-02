// Sol W-C W10 V19 (2026-05-21) — high-level self-call marker.
//
// V14-V18 cover the *low-level* self-call surface:
// `payable(this).call(...)`, `address(this).delegatecall(...)`,
// `payable(this).transfer(...)`, and the modern
// `.call{value:x, gas:y}(...)` chained syntax. Those routes pass
// through the V8 cast walker which keys on low-level method names.
//
// V19 closes the parallel *high-level* surface: typed self-calls
// through Sol's message-call boundary. Three shapes:
//
//	this.foo()                            // bare-this dispatch
//	MyContract(address(this)).foo()       // contract-type cast wrap
//	IFoo(address(this)).bar()             // interface-type cast wrap
//
// All three carry the same security implication as the low-level
// shapes — the EVM still performs a fresh message call against the
// same address, so the callee can re-enter the caller. The marker
// fires on the enclosing callable so security tooling can scan
// "every callable that allows typed re-entry into its own dispatch"
// independently of the low-level surface.
//
// The walker mirrors runExternalCallCastMarker's structure but
// inverts the method filter (admit user-defined methods, exclude
// low-level / value-transfer methods which the V18 walker already
// covers) and uses isSelfRef — a recursive cast-unwrap helper — to
// recognise self even when wrapped in contract / interface casts
// nested inside address() / payable() casts.

package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
)

func (v *declVisitor) runHighLevelSelfCallMarker() {
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
	highLevelSelfAffected := map[string]bool{}
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
			// V19 covers high-level dispatch only — the V8/V18 walker
			// already marks the low-level / value-transfer surface as
			// HasSelfReentrantCall, and double-marking would muddle the
			// "low-level vs high-level" axis security tooling relies on.
			if isLowLevelMethod(methodName) || isValueTransferMethod(methodName) {
				continue
			}
			parent := member.Parent()
			if parent != nil && parent.Kind() == "expression" {
				parent = parent.Parent()
			}
			// W-C W10 V22 (2026-05-21): high-level dispatch also
			// admits the `.foo{value: x, gas: y}()` options shape —
			// `this.target{value: 0}()` parses with the same
			// struct_expression wrapper V18 traversed for the
			// low-level path. Without this hop a typed self-call
			// with explicit value / gas options silently loses
			// HasHighLevelSelfCall while the equivalent
			// `this.target()` (no options) gets it. The shape is
			// common in payable workflows where the caller passes
			// ETH along with the dispatch.
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
			if !isSelfRef(inner, v.src) {
				continue
			}
			fnQ, fnStart, ok := nearestFunctionQnameAndStart(&member, v.src)
			if !ok {
				continue
			}
			id := parse.MakeID(fnQ, "sol", fnStart)
			highLevelSelfAffected[id] = true
		}
	}
	if len(highLevelSelfAffected) == 0 {
		return
	}
	for i := range v.nodes {
		if highLevelSelfAffected[v.nodes[i].ID] {
			v.nodes[i].HasHighLevelSelfCall = true
		}
	}
}

// isSelfRef reports whether n syntactically refers to `this`,
// directly or through one or more cast wrappers. Recognises:
//
//	identifier "this"                                  // bare
//	address(<isSelfRef>)                               // address cast
//	payable(<isSelfRef>)                               // payable cast
//	<TypeName>(<isSelfRef>)                            // contract /
//	                                                   //   interface
//	                                                   //   cast
//
// The recursion unwinds nested casts (e.g.
// `IFoo(address(this))`) without committing to a specific outermost
// cast shape — the criterion is "does the cast chain bottom out at
// `this`".
//
// Tree-sitter caveat: Solidity's grammar parses user-defined
// identifier-shaped casts (`MyContract(...)`, `IFoo(...)`) as
// `call_expression`, not `type_cast_expression`. We accept that
// shape conditionally — single call_argument + leading identifier
// whose first letter is upper-case (the Sol convention for type
// names). The upper-case filter trims the most obvious false
// positives (e.g. `requireOwner(this).foo()` where the leading
// identifier is a helper, not a type) while still catching every
// genuine contract / interface cast that follows the convention.
// A genuinely lower-case-named type is theoretically possible but
// rare in production; we accept that residual false negative as a
// reasonable trade for the false-positive reduction.
func isSelfRef(n *sitter.Node, src []byte) bool {
	if n == nil {
		return false
	}
	if n.Kind() == "identifier" && n.Utf8Text(src) == "this" {
		return true
	}
	if n.Kind() == "payable_conversion_expression" || n.Kind() == "type_cast_expression" {
		// Find the cast's first call_argument and recurse into the
		// expression it wraps. Mirrors isSelfCast's traversal but
		// returns *the recursive result* rather than a flat identifier
		// check.
		for i := uint(0); i < n.NamedChildCount(); i++ {
			child := n.NamedChild(i)
			if child == nil || child.Kind() != "call_argument" {
				continue
			}
			arg := unwrapExpression(child.NamedChild(0))
			return isSelfRef(arg, src)
		}
		return false
	}
	if n.Kind() == "call_expression" {
		// Sol grammar parses `TypeName(arg)` and `funcName(arg)`
		// identically. Use the leading-identifier upper-case
		// convention to filter most false positives; a single
		// call_argument keeps the false-positive cone narrow.
		if n.NamedChildCount() < 2 {
			return false
		}
		funcExpr := unwrapExpression(n.NamedChild(0))
		if funcExpr == nil || funcExpr.Kind() != "identifier" {
			return false
		}
		funcName := funcExpr.Utf8Text(src)
		if funcName == "" || funcName[0] < 'A' || funcName[0] > 'Z' {
			return false
		}
		// Walk the call_argument children — there must be exactly one
		// for a cast shape. Multi-arg calls (e.g. `Foo(this, other)`)
		// fall through as non-self-ref.
		var arg *sitter.Node
		argCount := 0
		for i := uint(0); i < n.NamedChildCount(); i++ {
			ch := n.NamedChild(i)
			if ch == nil || ch.Kind() != "call_argument" {
				continue
			}
			argCount++
			if argCount > 1 {
				return false
			}
			arg = unwrapExpression(ch.NamedChild(0))
		}
		if argCount != 1 {
			return false
		}
		return isSelfRef(arg, src)
	}
	return false
}
