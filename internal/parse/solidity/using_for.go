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
// contract / library / interface body and queues a PendingRef for the
// library reference. Container identifier comes from the same query
// capture so the SrcID lines up with the container's existing node ID.
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
		for _, c := range m.Captures {
			switch names[c.Index] {
			case "container":
				n := c.Node
				containerNode = &n
			case "lib":
				n := c.Node
				libNode = &n
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
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:        srcID,
			EdgeType:     types.EdgeUsesFor,
			TargetQName:  libName,
			Line:         int(libNode.StartPosition().Row) + 1,
			DispatchKind: dispatchKindUsingFor,
		})
	}
}

// dispatchKindUsingFor tags PendingRefs originating from W6 using-for
// detection so the resolver can route them through the dedicated path.
// String literal matches the existing idiom (W1 "inherit", W2
// "override"/"override_explicit", W3 "interface_dispatch").
const dispatchKindUsingFor = "using_for"
