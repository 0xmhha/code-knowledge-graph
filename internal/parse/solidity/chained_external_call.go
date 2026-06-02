package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Sol W-C W10 V6 (2026-05-19) — chained-call receiver shape for
// HasExternalCall.
//
// V4 marked HasExternalCall via Pass 2 receiver-type lookup for
// bare-identifier receivers (`target.call(...)`); V5 caught Sol
// cast wrappers (`address(t).call(...)`). V6 covers chained
// shapes:
//
//	getTarget().call(data)
//	loadHandler().delegatecall(data)
//
// Detection re-uses matchChainedMethodCall (W6 V1.3) which matches
// `<innerFn>().<method>(...)`. When `<method>` is a low-level
// method and the inner function's first return type is
// `address` / `address payable`, the marker fires. The funcReturn
// Types index built for W6 V1.3 chained dispatch is the lookup
// source — no new index needed.
//
// V6 limitations remaining:
//   - Multi-return chained shape (`getThings()` returning a tuple)
//     consults only the first return slot, matching W6 V1.3.
//   - Deep chains (`a().b().call(...)`) consult b's return type
//     only when b is registered via funcReturnTypes; otherwise the
//     receiver drops at the type lookup.

const dispatchKindChainedExternalCall = "chained_external_call"

// dispatchKindDeepChainedExternalCall (W-C W10 V7, 2026-05-19)
// tags depth-2 chained shape `a().b().call(...)` — TargetQName
// encodes `<innerFn1>|<innerFn2>|<method>`.
const dispatchKindDeepChainedExternalCall = "deep_chained_external_call"

func (v *declVisitor) runChainedExternalCall() {
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
			// V7: try depth-2 (`a().b().call(...)`) first because
			// V6's single-level predicate would otherwise reject
			// the outer member (its `object` is a call_expression
			// whose function is a member_expression, not an
			// identifier — so single-level already drops).
			if fn1, fn2, methodName, ok := matchDeepChainedMethodCall(&memberNode, v.src); ok {
				if !isLowLevelMethod(methodName) {
					continue
				}
				fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
				if !fnOK {
					continue
				}
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        parse.MakeID(fnQ, "sol", fnStart),
					EdgeType:     types.EdgeInvokes,
					TargetQName:  fn1 + "|" + fn2 + "|" + methodName,
					Line:         int(memberNode.StartPosition().Row) + 1,
					ByteOffset:   int(memberNode.StartByte()),
					DispatchKind: dispatchKindDeepChainedExternalCall,
				})
				continue
			}
			// V6: single-level chained shape.
			innerFnName, methodName, ok := matchChainedMethodCall(&memberNode, v.src)
			if !ok {
				continue
			}
			if !isLowLevelMethod(methodName) {
				continue
			}
			fnQ, fnStart, fnOK := nearestFunctionQnameAndStart(&memberNode, v.src)
			if !fnOK {
				continue
			}
			v.pending = append(v.pending, parse.PendingRef{
				SrcID:        parse.MakeID(fnQ, "sol", fnStart),
				EdgeType:     types.EdgeInvokes, // routed by DispatchKind; no edge emitted
				TargetQName:  innerFnName + "|" + methodName,
				Line:         int(memberNode.StartPosition().Row) + 1,
				ByteOffset:   int(memberNode.StartByte()),
				DispatchKind: dispatchKindChainedExternalCall,
			})
		}
	}
}

// matchChainedMethodCall already lives in using_for.go; this
// walker re-uses it so the chained-call AST predicate stays in
// one place.
var _ = matchChainedMethodCall // documents the dependency for readers

// sitter import retained for future chained-shape expansions.
var _ sitter.Node
