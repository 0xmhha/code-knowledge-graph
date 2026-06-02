package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
)

// Sol W-C W8 V1 (2026-05-18) — HasLowLevelCall and HasValueTransfer
// presence markers on callables.
//
// Complement to W7.1 V0 EdgeInvokes, which only fires when the
// receiver of a `.call` family invocation is a state-var / parameter
// of Contract or Interface type. Address-typed receivers (the common
// proxy / forwarder pattern), `address(x)` cast wrappers, and
// chained receivers all produce no edge in V0 but the markers light
// up so security tooling can run "which functions perform any
// low-level call shape" queries without dropping into AST analysis.
//
// V1 scope:
//   - HasLowLevelCall:   any `.call` / `.delegatecall` / `.staticcall`
//                        on any receiver shape. The W7.1 walker still
//                        emits edges for resolvable receivers; this
//                        marker is a superset signal.
//   - HasValueTransfer:  any `.send` / `.transfer`. Sol semantics:
//                        ETH transfer with limited gas, not method
//                        dispatch — distinct from .call family even
//                        though the AST shape is identical.
//
// The walker is shape-only (no receiver resolution), so it triggers
// on any member_expression whose property text matches one of the
// six method names AND whose parent unwraps to a call_expression.
// Yul-internal builtins (assembly delegatecall, sstore, ...) are
// out of scope here — they live under W10's HasAssembly marker, and
// future W10 V1+ work surfaces them separately.

func isValueTransferMethod(name string) bool {
	switch name {
	case "send", "transfer":
		return true
	}
	return false
}

// runLowLevelCallMarker walks every member_expression with a
// call-expression parent and property text in the low-level or
// value-transfer set. Resolves to the enclosing callable via
// nearestFunctionQnameAndStart and flips the corresponding marker
// on the matching node post-Pass-1.
//
// Same idiom as runAssemblyMarker (W10 V0): post-Pass-1 in-place
// mutation, so the four function-emit paths (runFunctionDecl /
// runConstructorDecl / runFallbackReceiveDecl / runDecl-for-modifier)
// don't need touching.
func (v *declVisitor) runLowLevelCallMarker() {
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

	hasLowLevel := map[string]bool{}
	hasValue := map[string]bool{}

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
			property := memberNode.ChildByFieldName("property")
			if property == nil || property.Kind() != "identifier" {
				continue
			}
			method := property.Utf8Text(v.src)
			isLow := isLowLevelMethod(method)
			isVal := isValueTransferMethod(method)
			if !isLow && !isVal {
				continue
			}
			// Must be the function position of a call_expression. The
			// member node is wrapped in an expression node, which is
			// itself a child of the call_expression's `function` field.
			parent := memberNode.Parent()
			if parent != nil && parent.Kind() == "expression" {
				parent = parent.Parent()
			}
			if parent == nil || parent.Kind() != "call_expression" {
				continue
			}
			fnQ, fnStart, ok := nearestFunctionQnameAndStart(&memberNode, v.src)
			if !ok {
				continue
			}
			id := parse.MakeID(fnQ, "sol", fnStart)
			if isLow {
				hasLowLevel[id] = true
			}
			if isVal {
				hasValue[id] = true
			}
		}
	}

	if len(hasLowLevel) == 0 && len(hasValue) == 0 {
		return
	}
	for i := range v.nodes {
		if hasLowLevel[v.nodes[i].ID] {
			v.nodes[i].HasLowLevelCall = true
		}
		if hasValue[v.nodes[i].ID] {
			v.nodes[i].HasValueTransfer = true
		}
	}
}
