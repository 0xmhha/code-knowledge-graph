package typescript

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TS W-B W1 — heritage detection (extends / implements).
//
// Spec: docs/design/ts-async-await-and-interface.md §3.1, §4.1.
// Dispatch index: docs/DISPATCH-WITHIN-LANG-SEMANTICS.md §2 Phase 5 W-B W1.
//
// Scope: three tree-sitter shapes (verified 2026-05-11 against
// tree-sitter-typescript v0.23.2):
//
//	class Derived extends Base implements IBase {}
//	class Multi   extends Base implements IFoo, IBar, IBaz {}
//	interface IChild extends IBase {}              // single-parent
//	interface IUnion extends IFoo, IBar {}         // multi-parent
//
// → EdgeExtends:    Derived → Base, IChild → IBase, IUnion → IFoo / IBar
// → EdgeImplements: Derived → IBase, Multi → IFoo / IBar / IBaz
//
// Per §5.0 decisions (2026-05-11):
//   - Same-file resolution → ConfExtracted.
//   - Cross-file resolution → ConfInferred (Pass 2 PendingRef path).
//   - Unresolved (parent name matches no node) → drop. graph.Validate
//     rejects dangling edges, and the TS parser already drops unresolved
//     `calls` pending refs — same idiom.
//   - Multiple `implements` / `extends` targets each emit a separate edge
//     in source order (Q4 in §5.0).
//   - Declaration merging V0: each `interface X { ... }` re-declaration
//     becomes its own NodeInterface (the existing parser already does
//     this — heritage edges anchor on whichever interface node the
//     resolver picks first, see Q5 in §5.0).
//   - Edge direction: child → parent ("child inherits from parent"),
//     matching Sol inheritance.go and the existing track-c convention.
//
// Tree-sitter shapes (probe-confirmed):
//
//	class_declaration
//	  name: type_identifier (@class_name)
//	  class_heritage
//	    extends_clause
//	      identifier | member_expression | (identifier + type_arguments)
//	    implements_clause
//	      (type_identifier | nested_type_identifier | generic_type)+
//
//	interface_declaration
//	  name: type_identifier (@iface_name)
//	  extends_type_clause
//	    (type_identifier | nested_type_identifier | generic_type)+
//
// Target name extraction: we take the *trailing* identifier (rightmost
// segment for `Ns.Foo` namespaces, the bare type_identifier for generics).
// This matches the existing TS parser's name-based resolution — qualified
// imports are out of scope for V0, same way `calls` resolves via byName.

// runHeritage walks the parse tree and queues one PendingRef per
// (child, parent) heritage edge. Provisional EdgeType is set at emit
// time based on the *clause* kind:
//
//   - extends_clause / extends_type_clause → EdgeExtends
//   - implements_clause → EdgeImplements
//
// Unlike Sol — where parent kind decides extends-vs-implements — TS keeps
// the distinction lexically (the `extends` / `implements` keywords are
// disjoint in the AST). So we can fix the EdgeType at Pass 1 without
// needing Pass 2 to reclassify.
func (v *declVisitor) runHeritage() {
	v.walkHeritage(v.root)
}

// walkHeritage recurses the tree looking for class_declaration /
// interface_declaration nodes and dispatches to per-shape extractors.
// We hand-roll the recursion (rather than a tree-sitter query) because
// each declaration has a *pair* of distinct heritage children — query
// captures can't easily express "for this parent, walk both clauses
// and emit one PendingRef per target name" without re-doing the
// structural decomposition we'd otherwise just write directly.
func (v *declVisitor) walkHeritage(n *sitter.Node) {
	if n == nil {
		return
	}
	switch n.Kind() {
	case "class_declaration":
		v.emitClassHeritage(n)
	case "interface_declaration":
		v.emitInterfaceHeritage(n)
	}
	for i := uint(0); i < uint(n.ChildCount()); i++ {
		v.walkHeritage(n.Child(i))
	}
}

// emitClassHeritage handles `class Foo extends Bar implements Baz, Qux`.
// Walks the class_heritage child to find extends_clause / implements_clause
// siblings; emits one PendingRef per parent identifier discovered.
func (v *declVisitor) emitClassHeritage(class *sitter.Node) {
	nameNode := class.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	childName := nameNode.Utf8Text(v.src)
	srcID := makeID(childName, "ts", int(nameNode.StartByte()))

	heritage := findChildOfKind(class, "class_heritage")
	if heritage == nil {
		return
	}
	for i := uint(0); i < uint(heritage.ChildCount()); i++ {
		c := heritage.Child(i)
		switch c.Kind() {
		case "extends_clause":
			// extends_clause carries a single value field — but the
			// grammar varies: a bare identifier, a member_expression
			// (Ns.Base), or an identifier followed by type_arguments
			// (Map<string>). The first non-keyword child is the value.
			if target := extractExtendsClauseTarget(c, v.src); target != "" {
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        srcID,
					EdgeType:     types.EdgeExtends,
					TargetQName:  target,
					Line:         int(c.StartPosition().Row) + 1,
					DispatchKind: dispatchKindHeritage,
				})
			}
		case "implements_clause":
			for _, target := range extractTypeListTargets(c, v.src) {
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        srcID,
					EdgeType:     types.EdgeImplements,
					TargetQName:  target,
					Line:         int(c.StartPosition().Row) + 1,
					DispatchKind: dispatchKindHeritage,
				})
			}
		}
	}
}

// emitInterfaceHeritage handles `interface IChild extends IBase, IFoo`.
// The grammar uses extends_type_clause (note the different node kind from
// class extends_clause — TS grammar quirk) which holds a list of type
// references identical in shape to implements_clause.
func (v *declVisitor) emitInterfaceHeritage(iface *sitter.Node) {
	nameNode := iface.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	childName := nameNode.Utf8Text(v.src)
	srcID := makeID(childName, "ts", int(nameNode.StartByte()))

	clause := findChildOfKind(iface, "extends_type_clause")
	if clause == nil {
		return
	}
	for _, target := range extractTypeListTargets(clause, v.src) {
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:        srcID,
			EdgeType:     types.EdgeExtends,
			TargetQName:  target,
			Line:         int(clause.StartPosition().Row) + 1,
			DispatchKind: dispatchKindHeritage,
		})
	}
}

// extractExtendsClauseTarget pulls the trailing identifier out of a
// class extends_clause. The clause's first non-keyword child is the
// value expression; we accept three shapes:
//
//	identifier         → "Base"             (bare)
//	member_expression  → "Ns.Base"          (last segment "Base")
//	identifier + type_arguments → "Map<T>"  (bare identifier wins)
//
// Returns "" when the value node is something we don't know how to
// name-resolve (e.g. a call expression like `extends mixin(Base)`).
// V0 doesn't attempt to model mixin patterns.
func extractExtendsClauseTarget(clause *sitter.Node, src []byte) string {
	for i := uint(0); i < uint(clause.ChildCount()); i++ {
		c := clause.Child(i)
		switch c.Kind() {
		case "extends":
			continue
		case "identifier", "type_identifier":
			return c.Utf8Text(src)
		case "member_expression":
			return trailingName(c, src)
		case "nested_type_identifier":
			return trailingName(c, src)
		case "generic_type":
			if id := findChildOfKind(c, "type_identifier"); id != nil {
				return id.Utf8Text(src)
			}
		case "type_arguments", ",":
			continue
		default:
			// Unknown shape — try a generic trailing-name fallback.
			if name := trailingName(c, src); name != "" {
				return name
			}
		}
	}
	return ""
}

// extractTypeListTargets walks a clause that holds a comma-separated
// list of type references (implements_clause, extends_type_clause) and
// returns each parent's trailing identifier. Shapes encountered:
//
//	type_identifier         (IFoo)
//	nested_type_identifier  (Ns.IFoo)        → "IFoo"
//	generic_type            (IBox<number>)   → "IBox"
//
// Skips the keyword child (`implements` / `extends`), commas, and any
// shape we can't name-extract (logs nothing — V0 silent drop, same
// pattern as the rest of the TS parser).
func extractTypeListTargets(clause *sitter.Node, src []byte) []string {
	var out []string
	for i := uint(0); i < uint(clause.ChildCount()); i++ {
		c := clause.Child(i)
		switch c.Kind() {
		case "implements", "extends", ",":
			continue
		case "type_identifier", "identifier":
			out = append(out, c.Utf8Text(src))
		case "nested_type_identifier":
			if name := trailingName(c, src); name != "" {
				out = append(out, name)
			}
		case "generic_type":
			if id := findChildOfKind(c, "type_identifier"); id != nil {
				out = append(out, id.Utf8Text(src))
			} else if name := trailingName(c, src); name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}

// findChildOfKind returns the first direct child whose Kind() matches,
// or nil. Cheap linear scan — typical class_declaration has < 6 children.
func findChildOfKind(n *sitter.Node, kind string) *sitter.Node {
	if n == nil {
		return nil
	}
	for i := uint(0); i < uint(n.ChildCount()); i++ {
		c := n.Child(i)
		if c.Kind() == kind {
			return c
		}
	}
	return nil
}

// trailingName returns the rightmost identifier-like text under n. Used
// to flatten `Ns.Base` and similar namespaced references down to the
// final segment. Walks named children in reverse looking for the first
// identifier / type_identifier / property_identifier.
func trailingName(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	for i := int(n.ChildCount()) - 1; i >= 0; i-- {
		c := n.Child(uint(i))
		switch c.Kind() {
		case "identifier", "type_identifier", "property_identifier":
			return c.Utf8Text(src)
		}
	}
	return ""
}

// dispatchKindHeritage tags PendingRefs produced by W-B W1 so the
// Pass 2 resolver can route them through the heritage-specific path
// (cross-file → ConfInferred, unresolved → drop). String literal
// matches the existing convention (golang/grpc.go "rpc"/"grpc",
// solidity/inheritance.go "inherit").
const dispatchKindHeritage = "heritage"
