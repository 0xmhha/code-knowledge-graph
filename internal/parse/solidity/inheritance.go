package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Sol W1 — Inheritance + interface implementation detection.
//
// Spec: docs/design/solidity-inheritance-and-interface-dispatch.md §3.1, §4.1
// Dispatch index: docs/DISPATCH-WITHIN-LANG-SEMANTICS.md §2 Phase 5 W-C W1.
//
// Scope: the `is`-clause on contract / interface declarations:
//
//	contract Child is Parent { ... }              → EdgeExtends    (Child → Parent)
//	contract Impl  is IERC20 { ... }              → EdgeImplements (Impl  → IERC20)
//	contract Multi is Base, IFoo, IBar { ... }    → 3 edges, one per parent
//	interface IB   is IA { ... }                  → EdgeExtends    (IB → IA)
//
// Per §5.0 decisions (2026-05-11):
//   - Same-file resolution → ConfExtracted.
//   - Cross-file resolution → ConfInferred (Pass 2 PendingRef).
//   - Unresolved (parent qname matches no node) → drop (V0 — strict purge,
//     graph.Validate rejects dangling edges).
//   - Multiple inheritance is order-preserving (one edge per parent in
//     source order); C3 linearisation is out of scope for V0.
//   - Edge direction: child → parent ("child inherits from parent"),
//     consistent with Go implements.go and TS heritage semantics.
//
// Parent type classification (EdgeExtends vs EdgeImplements) requires
// knowing whether `Parent` is a contract or an interface. We always emit
// a PendingRef in Pass 1 with a *provisional* EdgeType (EdgeExtends by
// default) and let Pass 2 (resolveInheritance) reclassify based on the
// resolved target's NodeType. This keeps the Pass 1 / Pass 2 split clean
// — Pass 1 only knows local file structure; cross-file type discovery
// belongs to Pass 2.
//
// Out of scope for W1 (separate dispatches): virtual/override (W2),
// interface dispatch `IFoo(addr).bar()` (W3), `using For` (W6).

// runInheritance walks every `is`-clause match and queues a PendingRef
// per (child, parent) pair. The query (queryInheritance) matches both
// contract_declaration and interface_declaration parents so the same
// emit path covers both forms.
//
// Pass 1 cannot finalise the edge type — `Parent` may be a contract
// (EdgeExtends) or interface (EdgeImplements), and that distinction can
// only be made once the cross-file node table is built in Pass 2. We
// stash a provisional EdgeType (EdgeExtends) on the PendingRef and
// reclassify in resolveInheritance.
//
// We use `DispatchKind` as a side-channel marker — set to "inherit" so
// the resolver knows which PendingRefs originated here. This avoids
// adding a new field to PendingRef just for one detector.
func (v *declVisitor) runInheritance() {
	query, qErr := sitter.NewQuery(v.lang, queryInheritance)
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
		var childNode *sitter.Node
		var parentNode *sitter.Node
		for _, c := range m.Captures {
			switch names[c.Index] {
			case "child":
				n := c.Node
				childNode = &n
			case "parent":
				n := c.Node
				parentNode = &n
			}
		}
		if childNode == nil || parentNode == nil {
			continue
		}
		// SrcID must match the contract / interface node ID emitted by
		// runContractDecl / runLibraryDecl / runInterfaceDecl, which all
		// hash (name, "sol", name-node startByte). emitContractLikeNode
		// (abstract_library.go) uses the *name identifier* StartByte, so
		// we reuse childNode.StartByte() here — childNode is the same
		// identifier node captured as `@name` in those decls.
		childName := childNode.Utf8Text(v.src)
		childStart := int(childNode.StartByte())
		srcID := parse.MakeID(childName, "sol", childStart)
		parentName := parentNode.Utf8Text(v.src)
		v.pending = append(v.pending, parse.PendingRef{
			SrcID: srcID,
			// Provisional — resolveInheritance reclassifies to
			// EdgeImplements when the resolved parent is an Interface.
			EdgeType:     types.EdgeExtends,
			TargetQName:  parentName,
			Line:         int(parentNode.StartPosition().Row) + 1,
			DispatchKind: dispatchKindInherit,
		})
	}
}

// dispatchKindInherit tags PendingRefs that originate from W1 inheritance
// detection so resolveInheritance can identify and process them in Pass 2.
// String constant (rather than a typed enum) for consistency with existing
// DispatchKind usage in golang/grpc.go ("rpc", "grpc"), which uses bare
// string literals.
const dispatchKindInherit = "inherit"
