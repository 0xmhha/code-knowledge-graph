package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

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
			FilePath: v.rel, StartLine: int(nameNode.StartPosition().Row) + 1,
			EndLine:   int(nameNode.EndPosition().Row) + 1,
			StartByte: startByte, EndByte: endByte,
			Language: "sol", Confidence: types.ConfExtracted,
			SubKind: subKind,
		})
		v.edges = append(v.edges, types.Edge{
			Src: v.fileID, Dst: id, Type: types.EdgeDefines,
			Count: 1, Confidence: types.ConfExtracted,
		})

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
