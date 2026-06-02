package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Sol W8 V0 (2026-05-18) — contract-type cast dispatch detection.
//
// Pattern: `MyContract(addr).method()` where MyContract is a concrete
// contract (not an interface). This is the concrete-type sibling of
// W3 interface dispatch (`IFoo(addr).bar()`).
//
// The AST shape is identical between W3 and W8 — both are
// `(call_expression
//    (member_expression
//      (expression (call_expression function: (identifier @type)))
//      property: (identifier @method)))`
//
// The two walkers do not collide because their resolvers look up
// disjoint name indices (W3: byName[NodeInterface]; W8:
// byName[NodeContract]). A given TypeName cannot be both an
// Interface and a Contract in the same project, so at most one
// resolver emits an edge per matched call site.
//
// V0 receiver semantics: same as W3 — runtime address determines
// the real target, so Confidence is fixed at ConfAmbiguous regardless
// of file boundary. DispatchKind = "contract_cast" lets downstream
// consumers (viewer, llmSafeStoreReader filter) distinguish from
// interface dispatch.
//
// Out of V0:
//   - Chained casts `A(B(addr).field()).method()` — already excluded
//     by matchInterfaceDispatch's predicate (object is identifier,
//     not call_expression result).
//   - Address-typed cast `address(addr).balance` — that's a builtin
//     accessor, not a method call.

// dispatchKindContractCast tags PendingRefs from runContractCastDispatch
// so the Pass 2 dispatch switch routes them to resolveContractCastRef.
const dispatchKindContractCast = "contract_cast"

// runContractCastDispatch walks every member_expression matching the
// `TypeName(args).method` shape (re-using matchInterfaceDispatch's
// predicate) and queues a PendingRef tagged dispatchKindContractCast.
// Pass 2 then attempts to resolve TypeName against byName[NodeContract].
//
// Anchored to the enclosing function via nearestFunctionQnameAndStart
// for SrcID consistency with the function node hash (same idiom as
// runDispatch and runLowLevelCalls).
func (v *declVisitor) runContractCastDispatch() {
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
				continue
			}
			v.pending = append(v.pending, parse.PendingRef{
				SrcID:        parse.MakeID(fnQ, "sol", fnStart),
				EdgeType:     types.EdgeInvokes,
				TargetQName:  typeName + "." + methodName,
				Line:         int(memberNode.StartPosition().Row) + 1,
				ByteOffset:   int(memberNode.StartByte()),
				DispatchKind: dispatchKindContractCast,
			})
		}
	}
}
