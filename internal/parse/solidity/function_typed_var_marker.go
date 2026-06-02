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
	defer func() { query.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
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
	// W-C W8 V8 (2026-05-19): callers that PROPAGATE a function-
	// typed value (assign to state var or pass as argument) without
	// invoking it. Distinct from V4/V5/V6/V7 invocation paths.
	pointerPropagators := v.findFunctionPointerPropagators(fnByID, stateVarByContract)
	if len(affected) == 0 && len(pointerCallers) == 0 && len(pointerPropagators) == 0 {
		return
	}
	for i := range v.nodes {
		if affected[v.nodes[i].ID] {
			v.nodes[i].HasFunctionTypedVar = true
		}
		if pointerCallers[v.nodes[i].ID] {
			v.nodes[i].HasFunctionPointerCall = true
		}
		if pointerPropagators[v.nodes[i].ID] {
			v.nodes[i].HasFunctionPointerPropagation = true
		}
	}
}

// findFunctionPointerPropagators — W-C W8 V8 (2026-05-19). Walks
// every assignment_expression and call_expression in the file. For
// assignments, checks whether the RHS is a bare identifier matching
// a function-typed name in scope (fnByID for params/locals,
// stateVarByContract for state vars on the enclosing contract).
// For call_expressions, scans each call_argument for the same
// pattern. Returns the set of callable IDs that have at least one
// such propagation site.
//
// Invocations (`name(...)`) are NOT propagation — those are
// handled by findFunctionPointerCallers. Pure read access without
// assignment / pass-through (e.g. `(handler)` on its own) also
// stays out of scope; only RHS-of-assignment and call-argument
// positions count as propagation.
func (v *declVisitor) findFunctionPointerPropagators(
	fnByID map[string]map[string]bool,
	stateVarByContract map[string]map[string]bool,
) map[string]bool {
	if len(fnByID) == 0 && len(stateVarByContract) == 0 {
		return nil
	}
	out := map[string]bool{}

	// Helper closure: given an identifier node + the call_expression
	// node it sits inside, walk up to the enclosing function and
	// mark if the identifier matches a fn-typed name in scope.
	markIfFnTyped := func(idNode *sitter.Node) {
		if idNode == nil || idNode.Kind() != "identifier" {
			return
		}
		fnQ, fnStart, ok := nearestFunctionQnameAndStart(idNode, v.src)
		if !ok {
			return
		}
		fnID := parse.MakeID(fnQ, "sol", fnStart)
		name := idNode.Utf8Text(v.src)
		if pool, has := fnByID[fnID]; has && pool[name] {
			out[fnID] = true
			return
		}
		contract := nearestContractName(idNode, v.src)
		if contract != "" {
			if pool, has := stateVarByContract[contract]; has && pool[name] {
				out[fnID] = true
			}
		}
	}

	// Pass 1: assignments. RHS bare identifier of fn-typed name.
	const aq = `(assignment_expression) @assign`
	if query, qErr := sitter.NewQuery(v.lang, aq); qErr == nil {
		defer func() { query.Close() }()
		cur := sitter.NewQueryCursor()
		defer func() { cur.Close() }()
		matches := cur.Matches(query, v.root, v.src)
		names := query.CaptureNames()
		for {
			m := matches.Next()
			if m == nil {
				break
			}
			for _, c := range m.Captures {
				if names[c.Index] != "assign" {
					continue
				}
				assignNode := c.Node
				right := assignNode.ChildByFieldName("right")
				right = unwrapExpression(right)
				markIfFnTyped(right)
			}
		}
	}

	// Pass 2: call arguments.
	const cq = `(call_expression) @call`
	if query, qErr := sitter.NewQuery(v.lang, cq); qErr == nil {
		defer func() { query.Close() }()
		cur := sitter.NewQueryCursor()
		defer func() { cur.Close() }()
		matches := cur.Matches(query, v.root, v.src)
		names := query.CaptureNames()
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
				// Iterate `call_argument` children — each is an
				// expression wrapping the actual argument node.
				for i := uint(0); i < callNode.NamedChildCount(); i++ {
					child := callNode.NamedChild(i)
					if child == nil || child.Kind() != "call_argument" {
						continue
					}
					arg := unwrapExpression(child.NamedChild(0))
					markIfFnTyped(arg)
				}
			}
		}
	}

	// W-C W8 V10 (2026-05-19) — Pass 4: emit-statement propagation.
	// `emit MyEvent(handler)` passes a fn-typed value through an
	// event, which logs it to the chain — security-relevant
	// propagation that the assignment / argument / return passes
	// don't cover. We scan every identifier descendant of each
	// emit_statement and mark any that matches a fn-typed name.
	const eq = `(emit_statement) @emit`
	if query, qErr := sitter.NewQuery(v.lang, eq); qErr == nil {
		defer func() { query.Close() }()
		cur := sitter.NewQueryCursor()
		defer func() { cur.Close() }()
		matches := cur.Matches(query, v.root, v.src)
		names := query.CaptureNames()
		for {
			m := matches.Next()
			if m == nil {
				break
			}
			for _, c := range m.Captures {
				if names[c.Index] != "emit" {
					continue
				}
				emitNode := c.Node
				walkIdentifiers(&emitNode, markIfFnTyped)
			}
		}
	}

	// W-C W8 V9 (2026-05-19) — Pass 3: return-position propagation.
	// `return cb;` where cb is a fn-typed param/local/state-var
	// propagates the function pointer to the caller.
	const rq = `(return_statement) @ret`
	if query, qErr := sitter.NewQuery(v.lang, rq); qErr == nil {
		defer func() { query.Close() }()
		cur := sitter.NewQueryCursor()
		defer func() { cur.Close() }()
		matches := cur.Matches(query, v.root, v.src)
		names := query.CaptureNames()
		for {
			m := matches.Next()
			if m == nil {
				break
			}
			for _, c := range m.Captures {
				if names[c.Index] != "ret" {
					continue
				}
				retNode := c.Node
				// return_statement may wrap the expression in
				// expression / variable_declaration_tuple etc.;
				// scan all named children for a bare identifier.
				for i := uint(0); i < retNode.NamedChildCount(); i++ {
					child := retNode.NamedChild(i)
					if child == nil {
						continue
					}
					expr := unwrapExpression(child)
					markIfFnTyped(expr)
				}
			}
		}
	}
	return out
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
	defer func() { query.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
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

// walkIdentifiers recurses into a subtree and invokes fn for every
// identifier descendant. Used by the W8 V10 emit-statement walker
// so an identifier nested under nested call_arguments / expression
// wrappers is still inspected. The recursion bound is the AST
// depth of a single emit statement (typically <10) so cost stays
// trivial.
func walkIdentifiers(n *sitter.Node, fn func(*sitter.Node)) {
	if n == nil {
		return
	}
	if n.Kind() == "identifier" {
		fn(n)
		return
	}
	for i := uint(0); i < n.NamedChildCount(); i++ {
		walkIdentifiers(n.NamedChild(i), fn)
	}
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
//
// W-C W8 V12 (2026-05-19): also recurses into nested type_name
// wrappers so `function(uint256)[] handlers` lights up the same
// markers as the scalar form. tree-sitter-solidity v1.2.11 models
// the array form as an outer type_name whose first named child is
// the inner element type_name (the surrounding `[]` is punctuation,
// not a separate AST node).
func typeNameIsFunctionTyped(typeNode *sitter.Node) bool {
	if typeNode == nil {
		return false
	}
	for i := uint(0); i < typeNode.NamedChildCount(); i++ {
		child := typeNode.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "parameter", "return_parameter":
			return true
		case "type_name":
			// V12: nested type_name (array wrapper). Recurse
			// into the element type — if it's a function type,
			// the whole array is fn-typed.
			if typeNameIsFunctionTyped(child) {
				return true
			}
		}
	}
	return false
}
