package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Sol W-C W8 V6 (2026-05-19) — cross-contract function pointer
// call detection.
//
// W8 V5 marked HasFunctionPointerCall on bare-identifier callees
// that matched a function-typed state variable declared on the
// SAME contract. V6 covers the cross-contract shape:
//
//	contract Hub {
//	    function(uint256) external returns (uint256) onAction;
//	    ...
//	}
//	contract Caller {
//	    Hub h;
//	    function trigger(uint256 x) external {
//	        h.onAction(x); // <- V6 detects this
//	    }
//	}
//
// The walker emits one PendingRef per `<receiver>.<method>(...)`
// shape (matchStateVarMethodCall covers it) tagged with a new
// dispatch kind. Pass 2 looks up the receiver's declared type
// (state-var / param / local), checks whether
// (contractName, fieldName) is in the cross-file function-typed
// NodeField set, and marks HasFunctionPointerCall on the source
// callable when it matches.
//
// V6 limitations:
//   - Chained / cast receivers (`getHub().onAction(x)`,
//     `Hub(addr).onAction(x)`) drop — matchStateVarMethodCall
//     requires a bare-identifier receiver.
//   - Function-typed state vars inherited through `is`-clauses are
//     visible to the byContract index only after Pass 2a populates
//     parents, so V6 sees direct-contract-typed fields but not the
//     inheritance-walk shape. Deferred to V7+ where the inheritance
//     index can be threaded in.

const dispatchKindCrossContractFnPointerCall = "cross_contract_fn_pointer_call"

func (v *declVisitor) runCrossContractFnPointerCall() {
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
			receiverName, methodName, ok := matchStateVarMethodCall(&memberNode, v.src)
			if !ok {
				continue
			}
			fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
			if !fnOK {
				continue
			}
			v.pending = append(v.pending, parse.PendingRef{
				SrcID:        parse.MakeID(fnQ, "sol", fnStart),
				EdgeType:     types.EdgeInvokes, // routed by DispatchKind; never emitted
				TargetQName:  receiverName + "|" + methodName,
				Line:         int(memberNode.StartPosition().Row) + 1,
				ByteOffset:   int(memberNode.StartByte()),
				DispatchKind: dispatchKindCrossContractFnPointerCall,
			})
		}
	}
}
