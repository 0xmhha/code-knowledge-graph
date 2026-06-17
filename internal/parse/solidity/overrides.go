package solidity

import (
	"strconv"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// nearestEnclosingBlockEndLine walks up from n to the nearest
// enclosing `block_statement` or `function_body` and returns its
// end-line (1-based). Used by emitLocalVarBinding (V2.0) to record
// each local's scope range. Returns 0 when no enclosing block is
// found (defensive — every valid variable_declaration in a function
// body sits inside one).
func nearestEnclosingBlockEndLine(n *sitter.Node) int {
	for cur := n.Parent(); cur != nil; cur = cur.Parent() {
		switch cur.Kind() {
		case "function_body", "block_statement":
			return int(cur.EndPosition().Row) + 1
		}
	}
	return 0
}

// nearestEnclosingBlockEndByte walks up from n to the nearest
// enclosing `block_statement` or `function_body` and returns its
// end byte offset. W-C W6 V2.15 (2026-05-15): mirrors
// nearestEnclosingBlockEndLine but at byte precision so the
// resolver can disambiguate same-line shadows. Returns 0 when no
// enclosing block is found.
func nearestEnclosingBlockEndByte(n *sitter.Node) int {
	for cur := n.Parent(); cur != nil; cur = cur.Parent() {
		switch cur.Kind() {
		case "function_body", "block_statement":
			return int(cur.EndByte())
		}
	}
	return 0
}

// Sol W2 — virtual / override modifier detection and EdgeOverrides emit.
//
// Spec: docs/design/solidity-inheritance-and-interface-dispatch.md §3.3, §4.2
// Dispatch index: docs/DISPATCH-WITHIN-LANG-SEMANTICS.md §2 Phase 5 W-C W2.
//
// Scope: Solidity function declarations carry two related modifiers that
// shape dynamic dispatch:
//
//	function foo() public virtual returns (uint) { ... }            // base
//	function foo() public override returns (uint) { ... }           // child
//	function foo() public virtual override returns (uint) { ... }   // middle of chain
//	function foo() public override(A, B) returns (uint) { ... }     // explicit parents
//
// Per §5.0 decisions (2026-05-11):
//   - SubKind values: "function" (plain) / "virtual" / "override" /
//     "virtual_override". `function_definition` keyword in tree-sitter
//     grammar exposes `virtual` (sym_virtual, named) and `override_specifier`
//     (sym_override_specifier, named) as children.
//   - EdgeOverrides direction: child.method -> parent.method (Q4).
//   - Confidence: same-file resolution -> ConfExtracted; cross-file ->
//     ConfInferred (Q9 / §2.2). Unresolved parents -> drop.
//   - Multiple inheritance: `override(A, B)` produces one EdgeOverrides per
//     listed parent. Bare `override` (no list) resolves against the union
//     of inherited contracts/interfaces in Pass 2 (one edge per parent that
//     declares a same-name virtual function).
//
// W2 piggybacks on W1's EdgeExtends / EdgeImplements emission. The Pass 2
// resolver consults the already-resolved inheritance graph to walk a child
// contract's parents when looking for the function being overridden. This
// keeps W2 strictly additive — no changes to W1 edge counts or shapes.
//
// Out of scope for W2 (separate dispatches):
//   - `super.foo()` body-walk emit. The spec (§3.3) describes super-call
//     handling as an EdgeCalls/EdgeInvokes emission, not EdgeOverrides.
//     W2 covers the *declaration-time* override relationship; super calls
//     are a runtime invocation pattern that belongs with W3 (interface
//     dispatch) since both share the resolver path for inheritance-aware
//     name lookup. Kept out of W2 to keep this dispatch atomic.
//   - `using For` library extension (W6).
//   - Interface dispatch `IFoo(addr).bar()` (W3).

// runFunctionDecl replaces the generic runDecl(queryFunction, NodeFunction)
// path so we can stamp SubKind and queue EdgeOverrides PendingRefs in the
// same pass. Behaviour for plain functions (no virtual/override modifier)
// is identical to runDecl — same node ID, same `defines` edge — so existing
// callers (ABI collection, mapping writes, emits, modifier_invocation,
// runHasModifier) remain wired against the same Function nodes.
func (v *declVisitor) runFunctionDecl() {
	query, qErr := sitter.NewQuery(v.lang, queryFunction)
	if qErr != nil {
		return
	}
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
		var nameNode *sitter.Node
		var declNode *sitter.Node
		for _, c := range m.Captures {
			switch names[c.Index] {
			case "name":
				n := c.Node
				nameNode = &n
			case "decl":
				n := c.Node
				declNode = &n
			}
		}
		if nameNode == nil || declNode == nil {
			continue
		}

		// Build the function node identical to the generic runDecl path so
		// SrcID hashes line up with existing pending-ref emitters. The only
		// added field is SubKind (W2). When the function has no
		// virtual/override modifier, SubKind defaults to "function" — making
		// the Sol Function SubKind taxonomy explicit (mirrors W4's contract
		// SubKind: plain contracts get SubKind="contract", not "").
		ident := nameNode.Utf8Text(v.src)
		startByte := int(nameNode.StartByte())
		endByte := int(nameNode.EndByte())
		qname := ident
		if cn := nearestContractName(nameNode, v.src); cn != "" {
			qname = cn + "." + ident
		}
		id := parse.MakeID(qname, "sol", startByte)

		isVirtual, override := scanFunctionModifiers(declNode, v.src)
		subKind := functionSubKind(isVirtual, override.present)

		v.nodes = append(v.nodes, types.Node{
			ID: id, Type: types.NodeFunction, Name: ident, QualifiedName: qname,
			// canonical id (ADR-0001): no import path in Solidity, so the
			// relative file path is the qualifier and the parameter-type
			// signature separates overloads — <relpath>:<Contract>.<func>(<types>).
			// The file path also separates same-named contracts across
			// version dirs (e.g. contracts/v1 vs contracts/v2).
			CanonicalID: v.rel + ":" + qname + funcParamSignature(declNode, v.src),
			FilePath:    v.rel, StartLine: int(nameNode.StartPosition().Row) + 1,
			EndLine:   int(nameNode.EndPosition().Row) + 1,
			StartByte: startByte, EndByte: endByte,
			Language: "sol", Confidence: types.ConfExtracted,
			SubKind: subKind,
		})
		v.edges = append(v.edges, types.Edge{
			Src: v.fileID, Dst: id, Type: types.EdgeDefines,
			Count: 1, Confidence: types.ConfExtracted,
		})

		// W-C W6 V1.1 (2026-05-12): emit parameter name→type PendingRefs
		// so Pass 2 can index (funcID, paramName) → typeName for
		// parameter-receiver using-for dispatch resolution. function_definition
		// holds `parameter` nodes as direct named children (verified via
		// node-types.json — they sit alongside override_specifier /
		// modifier_invocation / state_mutability). Each parameter carries a
		// `name` field (optional — anonymous parameters drop) and a
		// `type` field (required, type_name).
		emitParameterMetaPending(v, id, declNode)
		// W-C W6 V1.3 (2026-05-12): emit function return-type PendingRef
		// so Pass 2 can resolve `<localCall>().<method>(...)` chained
		// dispatch by looking up the inner function's return type. V0
		// captures only the first declared return type (multi-return
		// tuples drop their tail — V1.4+).
		emitFunctionReturnMetaPending(v, id, declNode)
		// W-C W6 V1.19 (2026-05-12): emit name→type PendingRefs for
		// named return parameters (`function f() returns (uint256 x)`).
		// Solidity treats them as function-scope variables — they share
		// the parameter namespace, so dispatchKindUsingForParamType is
		// the right channel.
		emitNamedReturnParamMetaPending(v, id, declNode)
		// W-C W6 V1.15 (2026-05-12): emit local-variable name→type
		// PendingRefs for every `variable_declaration_statement` in the
		// function body (single-var form only — tuple destructuring is
		// V1.16+). Pass 2 indexes these into (funcID, varName) → typeName
		// for local-var receiver dispatch resolution.
		emitLocalVarMetaPending(v, id, declNode)

		if !override.present {
			continue
		}
		// Emit one PendingRef per override target. When the user wrote
		// `override(A, B)`, every listed parent gets a queued edge. When
		// they wrote bare `override` (no list), we queue a single ref with
		// an empty TargetQName — the resolver expands this against all of
		// the enclosing contract's known parents in Pass 2.
		fnLine := int(nameNode.StartPosition().Row) + 1
		if len(override.explicitParents) == 0 {
			v.pending = append(v.pending, parse.PendingRef{
				SrcID:        id,
				EdgeType:     types.EdgeOverrides,
				TargetQName:  ident,
				Line:         fnLine,
				DispatchKind: dispatchKindOverride,
			})
			continue
		}
		for _, parent := range override.explicitParents {
			v.pending = append(v.pending, parse.PendingRef{
				SrcID:    id,
				EdgeType: types.EdgeOverrides,
				// TargetQName carries "Parent.method" so the resolver can
				// scope its lookup directly to the named parent contract
				// (rather than scanning every parent of the enclosing
				// contract). This keeps explicit-list semantics distinct
				// from the bare-override case above.
				TargetQName:  parent + "." + ident,
				Line:         fnLine,
				DispatchKind: dispatchKindOverrideExplicit,
			})
		}
	}
}

// runModifierOverride — W-C W7.3 V0 (2026-05-18). Walks modifier_definition
// nodes and emits EdgeOverrides PendingRefs when the override_specifier
// child is present. Mirrors the function-override emit path in
// emitFunctionDeclWithOverride: bare `override` becomes a
// dispatchKindOverride PendingRef (resolver walks the inheritance chain
// for any parent that declares the same modifier name); explicit
// `override(Parent1, Parent2)` becomes one dispatchKindOverrideExplicit
// PendingRef per listed parent.
//
// Re-uses scanFunctionModifiers and collectOverrideParents — both are
// AST-shape helpers that already work on any node with named-child
// modifiers (function_definition, modifier_definition share the
// override_specifier child shape per grammar v1.2.11 probe 2026-05-18).
//
// The Pass 2 W2 resolver (resolveOverridesRef) is NodeType-agnostic:
// it indexes funcByQName with both NodeFunction and NodeModifier
// (declarations.go assigns Modifier qnames the same `Container.name`
// shape as Function), so modifier-pair lookups land without changes
// to the resolver.
func (v *declVisitor) runModifierOverride() {
	const q = `(modifier_definition name: (identifier) @name) @decl`
	query, qErr := sitter.NewQuery(v.lang, q)
	if qErr != nil {
		return
	}
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
		var declNode *sitter.Node
		var nameNode *sitter.Node
		for _, c := range m.Captures {
			if names[c.Index] == "decl" {
				n := c.Node
				declNode = &n
			}
			if names[c.Index] == "name" {
				n := c.Node
				nameNode = &n
			}
		}
		if declNode == nil || nameNode == nil {
			continue
		}
		_, override := scanFunctionModifiers(declNode, v.src)
		if !override.present {
			continue
		}
		ident := nameNode.Utf8Text(v.src)
		qname := ident
		if cn := nearestContractName(nameNode, v.src); cn != "" {
			qname = cn + "." + ident
		}
		startByte := int(nameNode.StartByte())
		srcID := parse.MakeID(qname, "sol", startByte)
		line := int(nameNode.StartPosition().Row) + 1

		if len(override.explicitParents) == 0 {
			v.pending = append(v.pending, parse.PendingRef{
				SrcID:        srcID,
				EdgeType:     types.EdgeOverrides,
				TargetQName:  ident,
				Line:         line,
				DispatchKind: dispatchKindOverride,
			})
			continue
		}
		for _, parent := range override.explicitParents {
			v.pending = append(v.pending, parse.PendingRef{
				SrcID:        srcID,
				EdgeType:     types.EdgeOverrides,
				TargetQName:  parent + "." + ident,
				Line:         line,
				DispatchKind: dispatchKindOverrideExplicit,
			})
		}
	}
}

// emitParameterMetaPending walks the named children of a function_definition
// and queues one PendingRef per `parameter` node carrying the (paramName,
// typeName) binding. Pass 2 indexes these into (funcID, paramName) →
// typeName for parameter-receiver using-for dispatch resolution (W-C W6
// V1.1). Anonymous parameters (no name field) are skipped — their type is
// still in the AST but no caller can address them by identifier, so the
// using-for receiver lookup never has a key for them.
//
// TargetQName encoding mirrors the state-var path: `paramName|typeName`.
// SrcID = function's node ID so Pass 2 can resolve the meta refs against
// the same funcID space used by containerIDByFuncID.
// funcParamSignature returns the parenthesised parameter-type list of a
// function_definition node, e.g. "(address,uint256)" or "()". It feeds the
// canonical id (ADR-0001) so Solidity overloads — which differ only by
// parameter types — get distinct ids. Parameter NAMES are intentionally
// ignored (anonymous parameters still contribute their type), matching how the
// Solidity ABI computes a function selector.
func funcParamSignature(declNode *sitter.Node, src []byte) string {
	if declNode == nil {
		return "()"
	}
	var paramTypes []string
	for i := uint(0); i < uint(declNode.NamedChildCount()); i++ {
		child := declNode.NamedChild(i)
		if child == nil || child.Kind() != "parameter" {
			continue
		}
		typeNode := child.ChildByFieldName("type")
		if typeNode == nil {
			continue
		}
		if t := extractTypeNameText(typeNode, src); t != "" {
			paramTypes = append(paramTypes, t)
		}
	}
	return "(" + strings.Join(paramTypes, ",") + ")"
}

func emitParameterMetaPending(v *declVisitor, funcID string, declNode *sitter.Node) {
	if declNode == nil {
		return
	}
	for i := uint(0); i < uint(declNode.NamedChildCount()); i++ {
		child := declNode.NamedChild(i)
		if child == nil || child.Kind() != "parameter" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue // anonymous parameter — no addressable receiver
		}
		typeNode := child.ChildByFieldName("type")
		if typeNode == nil {
			continue // shouldn't happen — type is required per grammar
		}
		paramName := nameNode.Utf8Text(v.src)
		typeName := extractTypeNameText(typeNode, v.src)
		if paramName == "" || typeName == "" {
			continue
		}
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:        funcID,
			EdgeType:     types.EdgeUsesFor, // routed by DispatchKind; not emitted
			TargetQName:  paramName + "|" + typeName,
			Line:         int(nameNode.StartPosition().Row) + 1,
			DispatchKind: dispatchKindUsingForParamType,
		})
	}
}

// dispatchKindUsingForParamType (W-C W6 V1.1) tags PendingRefs carrying
// function parameter (name, type) bindings for the parameter-receiver
// dispatch path. Resolver sweeps these into paramTypes index and emits
// no graph edge for them — pure side-channel.
const dispatchKindUsingForParamType = "using_for_param_type"

// emitFunctionReturnMetaPending — W-C W6 V1.3 (2026-05-12). Pulls the
// function's first declared return type out of the `return_type` field
// and queues a side-channel PendingRef that Pass 2 sweeps into the
// funcReturnTypes index. Empty when the function has no return clause,
// when the return clause has no parameter children, or when the type
// can't be normalised (extraction-failed shapes).
//
// V1.3 V0 scope: first return type only. Multi-return tuples
// (`returns (uint256, address)`) drop the tail — `foo().add(x)` chain
// dispatch typically targets the first return slot anyway. Multi-return
// handling (named return params, tuple destructuring) is V1.4+.
//
// TargetQName encoding: bare `<typeName>` (no `|` delimiter — there's
// only one piece of information to carry). SrcID = funcID so Pass 2
// can join against containerIDByFuncID for chained-call resolution.
func emitFunctionReturnMetaPending(v *declVisitor, funcID string, declNode *sitter.Node) {
	if declNode == nil {
		return
	}
	returnDef := declNode.ChildByFieldName("return_type")
	if returnDef == nil {
		return
	}
	// return_type_definition's children are `parameter` nodes (the
	// grammar reuses `parameter` for return slots — confirmed via
	// node-types.json). Take the first named parameter and pull its
	// type field, matching the receiver-type idiom used elsewhere.
	for i := uint(0); i < uint(returnDef.NamedChildCount()); i++ {
		child := returnDef.NamedChild(i)
		if child == nil || child.Kind() != "parameter" {
			continue
		}
		typeNode := child.ChildByFieldName("type")
		if typeNode == nil {
			continue
		}
		typeName := extractTypeNameText(typeNode, v.src)
		if typeName == "" {
			return
		}
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:        funcID,
			EdgeType:     types.EdgeUsesFor, // unused — routed by DispatchKind
			TargetQName:  typeName,
			Line:         int(returnDef.StartPosition().Row) + 1,
			DispatchKind: dispatchKindUsingForFnReturn,
		})
		return // first return slot only (V0)
	}
}

// dispatchKindUsingForFnReturn (W-C W6 V1.3) carries the function's
// first declared return type for chained-call dispatch resolution.
// Resolver sweeps these into the funcReturnTypes index — no graph edge.
const dispatchKindUsingForFnReturn = "using_for_fn_return"

// emitNamedReturnParamMetaPending — W-C W6 V1.19 (2026-05-12). Walks
// every parameter child of the function's `return_type` and queues a
// paramType PendingRef whenever the parameter has both a name and a
// type. Solidity treats named return parameters as function-scope
// variables initialised to zero — they share the parameter namespace
// for identifier resolution, so emitting them through dispatchKind-
// UsingForParamType (V1.1) lets lookupReceiverType pick them up via
// paramTypes without any resolver changes.
//
// Distinct from emitFunctionReturnMetaPending (V1.3, dispatchKind-
// UsingForFnReturn) — that captures the first-slot type for chained-
// call resolution. V1.19 captures every named slot's (name, type)
// pair for receiver-identifier dispatch. The two side-channels
// coexist: same return-type walk, different PendingRef payloads.
//
// Anonymous return slots (no name field) skip silently — there's no
// addressable receiver to resolve.
func emitNamedReturnParamMetaPending(v *declVisitor, funcID string, declNode *sitter.Node) {
	if declNode == nil {
		return
	}
	returnDef := declNode.ChildByFieldName("return_type")
	if returnDef == nil {
		return
	}
	for i := uint(0); i < uint(returnDef.NamedChildCount()); i++ {
		child := returnDef.NamedChild(i)
		if child == nil || child.Kind() != "parameter" {
			continue
		}
		nameNode := child.ChildByFieldName("name")
		if nameNode == nil {
			continue // anonymous return slot — no addressable receiver
		}
		typeNode := child.ChildByFieldName("type")
		if typeNode == nil {
			continue
		}
		paramName := nameNode.Utf8Text(v.src)
		typeName := extractTypeNameText(typeNode, v.src)
		if paramName == "" || typeName == "" {
			continue
		}
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:        funcID,
			EdgeType:     types.EdgeUsesFor, // unused — routed by DispatchKind
			TargetQName:  paramName + "|" + typeName,
			Line:         int(nameNode.StartPosition().Row) + 1,
			DispatchKind: dispatchKindUsingForParamType,
		})
	}
}

// emitLocalVarMetaPending — W-C W6 V1.15/V1.16 (2026-05-12). Walks every
// `variable_declaration_statement` reachable from a function's body and
// queues one PendingRef per LHS-typed declaration carrying
// (varName, typeName). Pass 2 indexes these into (funcID, varName) →
// typeName for local-var receiver dispatch resolution.
//
// Scope:
//   - V1.15: Single-var form `Type x = expr;`. variable_declaration_statement
//     wraps one `variable_declaration` child.
//   - V1.16: Tuple-destructuring form `(Ta a, Tb b) = foo();`.
//     variable_declaration_statement wraps one `variable_declaration_tuple`
//     child whose own children are mixed `variable_declaration` (typed
//     slot) and `identifier` (pre-declared slot — V1.17+ scope, dropped
//     here since the type isn't on the LHS).
//   - All blocks descended: variable_declaration_statement inside
//     if / for / while bodies are captured too. V0 scope treats locals
//     as function-scoped (no block-scope shadowing — first-declaration
//     wins by map-overwrite order, which matches typical Solidity style
//     where shadowing in nested blocks is rare).
//
// TargetQName encoding: `varName|typeName` (mirrors V1.1 parameter form).
// SrcID = function's node ID so Pass 2 joins against the same funcID
// space used by containerIDByFuncID / paramTypes.
//
// Tree-sitter shape (verified via node-types.json v1.2.13):
//
//	variable_declaration_statement
//	  value: expression (optional, RHS)
//	  children:
//	    variable_declaration                ← single-var (V1.15)
//	      location: 'calldata' | 'memory' | 'storage' (optional)
//	      name: identifier
//	      type: type_name
//	    | variable_declaration_tuple        ← tuple form (V1.16)
//	        children:
//	          identifier                    ← pre-declared slot (V1.17+, drop)
//	          | variable_declaration        ← typed slot (V1.16 scope)
func emitLocalVarMetaPending(v *declVisitor, funcID string, declNode *sitter.Node) {
	if declNode == nil {
		return
	}
	body := declNode.ChildByFieldName("body")
	if body == nil {
		return
	}
	collectLocalVarMetaPending(v, funcID, body)
}

// collectLocalVarMetaPending recursively descends every named child of n.
// On `variable_declaration_statement`: handles both single-var (V1.15)
// and tuple-destructuring (V1.16) forms by routing each typed slot
// through emitLocalVarBinding. On `try_statement`: emits the returns
// clause's named parameter slots (V1.20) so receivers bound in the
// success block are addressable. Other nodes recurse normally so
// nested blocks (if / for / while / try bodies) reach their statements.
func collectLocalVarMetaPending(v *declVisitor, funcID string, n *sitter.Node) {
	if n == nil {
		return
	}
	if n.Kind() == "variable_declaration_statement" {
		for i := uint(0); i < uint(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			if child == nil {
				continue
			}
			switch child.Kind() {
			case "variable_declaration":
				// V1.15 single-var form: `Type x = expr;`.
				emitLocalVarBinding(v, funcID, child)
			case "variable_declaration_tuple":
				// V1.16 tuple form: `(Ta a, Tb b) = foo();`. Iterate the
				// tuple's children — typed slots emit, pre-declared
				// identifier slots drop (no LHS type info — V1.17+ would
				// need funcReturnTypes multi-slot inference).
				for j := uint(0); j < uint(child.NamedChildCount()); j++ {
					slot := child.NamedChild(j)
					if slot == nil || slot.Kind() != "variable_declaration" {
						continue
					}
					emitLocalVarBinding(v, funcID, slot)
				}
			}
		}
		// Don't recurse — no nested declaration statements inside the
		// statement node itself.
		return
	}
	if n.Kind() == "try_statement" {
		// W6 V1.20: `try foo() returns (Ta a, Tb b) { ... }` — the
		// returns clause named-parameter slots are exposed as direct
		// `parameter` children of try_statement (distinct from
		// function_definition's `return_type` field — different AST
		// shape). Each slot is bound for the duration of the success
		// block (try_statement's `body` field). V2.0: scopeEndLine =
		// success-block end so use sites outside the block don't
		// pick up these names. V2.15: scopeEndByte mirrors at byte
		// precision for same-line shadow disambiguation.
		scopeEnd, scopeEndB := 0, 0
		if body := n.ChildByFieldName("body"); body != nil {
			scopeEnd = int(body.EndPosition().Row) + 1
			scopeEndB = int(body.EndByte())
		}
		for i := uint(0); i < uint(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			if child == nil || child.Kind() != "parameter" {
				continue
			}
			emitTryReturnsBinding(v, funcID, child, scopeEnd, scopeEndB)
		}
		// Fall through to recurse — try_statement's body
		// (block_statement) and catch_clause bodies still contain
		// statements that must be visited.
	}
	if n.Kind() == "catch_clause" {
		// W6 V1.21: `catch Type(Ta a, Tb b) { ... }` — the catch's
		// named parameter slots are exposed as direct `parameter`
		// children of catch_clause (alongside an optional identifier
		// for the catch type name like "Error" / "Panic"). V2.0:
		// scopeEndLine = catch body end. V2.15: scopeEndByte mirrors.
		scopeEnd, scopeEndB := 0, 0
		if body := n.ChildByFieldName("body"); body != nil {
			scopeEnd = int(body.EndPosition().Row) + 1
			scopeEndB = int(body.EndByte())
		}
		for i := uint(0); i < uint(n.NamedChildCount()); i++ {
			child := n.NamedChild(i)
			if child == nil || child.Kind() != "parameter" {
				continue
			}
			emitTryReturnsBinding(v, funcID, child, scopeEnd, scopeEndB)
		}
		// Fall through to recurse — catch_clause body still contains
		// statements that need visiting.
	}
	for i := uint(0); i < uint(n.NamedChildCount()); i++ {
		collectLocalVarMetaPending(v, funcID, n.NamedChild(i))
	}
}

// emitTryReturnsBinding emits one localVar PendingRef for a try_
// statement's returns-clause `parameter` slot (V1.20). Same encoding
// as emitLocalVarBinding so lookupReceiverType picks it up via
// localVarTypes. Anonymous slot (no name field) skips silently —
// nothing addressable.
//
// W-C W6 V2.0 (2026-05-12): scopeEndLine encoded into TargetQName
// as the third part so Pass 2 can build a per-decl line range for
// scope-aware lookup. Callers pass the end line of the binding's
// effective scope (try_statement.body or catch_clause.body end).
//
// W-C W6 V2.15 (2026-05-15): TargetQName extended to 5-part encoding
// — `varName|typeName|scopeEndLine|declStartByte|scopeEndByte` — so
// Pass 2 can byte-disambiguate same-line shadow scopes. PendingRef
// .ByteOffset carries the decl's name-position byte; callers pass
// scopeEndByte from the try/catch body's EndByte.
func emitTryReturnsBinding(v *declVisitor, funcID string, p *sitter.Node, scopeEndLine, scopeEndByte int) {
	if p == nil {
		return
	}
	nameNode := p.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	typeNode := p.ChildByFieldName("type")
	if typeNode == nil {
		return
	}
	varName := nameNode.Utf8Text(v.src)
	typeName := extractTypeNameText(typeNode, v.src)
	if varName == "" || typeName == "" {
		return
	}
	declStartByte := int(nameNode.StartByte())
	v.pending = append(v.pending, parse.PendingRef{
		SrcID:    funcID,
		EdgeType: types.EdgeUsesFor, // unused — routed by DispatchKind
		TargetQName: varName + "|" + typeName + "|" +
			strconv.Itoa(scopeEndLine) + "|" +
			strconv.Itoa(declStartByte) + "|" +
			strconv.Itoa(scopeEndByte),
		Line:         int(nameNode.StartPosition().Row) + 1,
		ByteOffset:   declStartByte,
		DispatchKind: dispatchKindUsingForLocalVar,
	})
}

// emitLocalVarBinding emits one localVar PendingRef for a single
// `variable_declaration` node (used by both V1.15 single-var and V1.16
// tuple-slot paths). Drops silently when name / type fields are missing
// or extraction returns empty — Pass 2 won't see the slot.
//
// W-C W6 V2.0 (2026-05-12): scope-end line is determined by walking
// from `decl` up the parent chain to the nearest enclosing
// block_statement / function_body and recorded in TargetQName's third
// slot (encoded as decimal). Pass 2 uses (declLine, scopeEndLine) +
// use-site line to do narrowest-scope-wins lookup.
//
// W-C W6 V2.15 (2026-05-15): byte-precision scope range added as the
// 4th and 5th encoded slots (declStartByte, scopeEndByte). PendingRef
// .ByteOffset carries the decl's name-position byte. Pass 2 falls back
// to V2.0 line-only behavior when the byte slots are zero (defensive
// — every valid local-var emit populates them via tree-sitter ranges).
func emitLocalVarBinding(v *declVisitor, funcID string, decl *sitter.Node) {
	if decl == nil {
		return
	}
	nameNode := decl.ChildByFieldName("name")
	typeNode := decl.ChildByFieldName("type")
	if nameNode == nil || typeNode == nil {
		return
	}
	varName := nameNode.Utf8Text(v.src)
	typeName := extractTypeNameText(typeNode, v.src)
	if varName == "" || typeName == "" {
		return
	}
	scopeEndLine := nearestEnclosingBlockEndLine(decl)
	scopeEndByte := nearestEnclosingBlockEndByte(decl)
	declStartByte := int(nameNode.StartByte())
	v.pending = append(v.pending, parse.PendingRef{
		SrcID:    funcID,
		EdgeType: types.EdgeUsesFor, // unused — routed by DispatchKind
		TargetQName: varName + "|" + typeName + "|" +
			strconv.Itoa(scopeEndLine) + "|" +
			strconv.Itoa(declStartByte) + "|" +
			strconv.Itoa(scopeEndByte),
		Line:         int(nameNode.StartPosition().Row) + 1,
		ByteOffset:   declStartByte,
		DispatchKind: dispatchKindUsingForLocalVar,
	})
}

// dispatchKindUsingForLocalVar (W-C W6 V1.15) tags PendingRefs carrying
// function-local variable (name, type) bindings for the local-var
// receiver dispatch path. Resolver sweeps these into localVarTypes
// index — pure side-channel, no graph edge.
const dispatchKindUsingForLocalVar = "using_for_local_var_type"

// overrideInfo carries the parsed result of an override_specifier.
//
//   - present=false when the function has no `override` modifier.
//   - present=true, explicitParents=nil when the user wrote bare `override`.
//   - present=true, explicitParents=[A, B] for `override(A, B)`.
type overrideInfo struct {
	present         bool
	explicitParents []string
}

// scanFunctionModifiers walks the named children of a function_definition
// node, looking for the two modifier kinds W2 cares about:
//
//   - `virtual` (sym_virtual, named) — the leaf keyword token.
//   - `override_specifier` (sym_override_specifier, named) — either a bare
//     `override` keyword or `override ( UserDefinedType, ... )`.
//
// The grammar splits all function modifiers into siblings of the
// function_definition (return_type_definition, modifier_invocation,
// visibility, etc.), so a single shallow walk is sufficient — virtual /
// override never appear nested under another modifier.
func scanFunctionModifiers(decl *sitter.Node, src []byte) (bool, overrideInfo) {
	var isVirtual bool
	var override overrideInfo
	if decl == nil {
		return false, override
	}
	for i := uint(0); i < decl.NamedChildCount(); i++ {
		c := decl.NamedChild(i)
		switch c.Kind() {
		case "virtual":
			isVirtual = true
		case "override_specifier":
			override.present = true
			override.explicitParents = collectOverrideParents(c, src)
		}
	}
	return isVirtual, override
}

// collectOverrideParents extracts the parent identifiers from an
// override_specifier of the form `override(A, B, C)`. Bare `override`
// returns nil (length-0).
//
// The grammar wraps each parent in a `user_defined_type` whose first
// identifier child is the parent's leaf name. Qualified names like
// `lib.Foo` would carry the leading identifier (`lib`) here, which is a
// known V0 limitation — same as W1's parent resolution (inheritance.go
// uses the leading identifier of user_defined_type). Real codebases rarely
// use qualified parents in override lists; flagged for follow-up.
func collectOverrideParents(spec *sitter.Node, src []byte) []string {
	if spec == nil {
		return nil
	}
	var out []string
	for i := uint(0); i < spec.NamedChildCount(); i++ {
		c := spec.NamedChild(i)
		if c.Kind() != "user_defined_type" {
			continue
		}
		// First identifier inside user_defined_type is the parent name.
		// Mirrors the parent-extraction idiom in queryInheritance.
		for j := uint(0); j < c.NamedChildCount(); j++ {
			id := c.NamedChild(j)
			if id.Kind() == "identifier" {
				out = append(out, id.Utf8Text(src))
				break
			}
		}
	}
	return out
}

// functionSubKind maps (virtual, override) → SubKind string per §5.0
// decisions. The four-way enumeration captures every combination Solidity
// allows on a function_definition modifier list.
//
// Plain functions get SubKind="function" (explicit value, no empty
// string) for symmetry with W4's contract SubKind.
func functionSubKind(isVirtual, hasOverride bool) string {
	switch {
	case isVirtual && hasOverride:
		return "virtual_override"
	case isVirtual:
		return "virtual"
	case hasOverride:
		return "override"
	default:
		return "function"
	}
}

// DispatchKind tag constants for W2 PendingRefs. Two distinct kinds so the
// Pass 2 resolver can branch:
//
//   - dispatchKindOverride: bare `override` — resolver expands against
//     every direct parent of the enclosing contract that declares a
//     same-name virtual function.
//   - dispatchKindOverrideExplicit: `override(A, B)` — TargetQName already
//     carries `Parent.method`, resolver looks up directly.
//
// String constants (not a typed enum) for consistency with existing
// DispatchKind usage in golang/grpc.go ("rpc", "grpc") and W1's
// dispatchKindInherit.
const (
	dispatchKindOverride         = "override"
	dispatchKindOverrideExplicit = "override_explicit"
)
