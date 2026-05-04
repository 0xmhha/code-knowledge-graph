package golang

import (
	"fmt"
	"go/ast"
	"go/token"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// emitFunctionBodyPos walks a function/method body and emits Pass-1 logic
// blocks (5 kinds), CallSite nodes, Goroutines, and channel send/recv edges.
// Cross-file call resolution is left to Pass 2 (T9).
//
// parentID must be the ID already minted for the enclosing function/method
// node — we accept it from the caller so we don't have to re-derive the
// parent's start byte offset here.
func (v *declVisitor) emitFunctionBodyPos(parentQname, parentID string, body *ast.BlockStmt) {
	if body == nil {
		return
	}
	// Reset channel variable scope for this function — chanVarIDs is function-scoped.
	v.chanVarIDs = make(map[string]string)
	// assignedMakeChan tracks token.Pos of make(chan ...) calls that were already
	// handled by the AssignStmt path, so the subsequent CallExpr event on the
	// same expression doesn't emit a duplicate Channel node.
	assignedMakeChan := map[token.Pos]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		switch s := n.(type) {
		case *ast.IfStmt:
			v.appendLogicBlockPos(parentID, parentQname, types.NodeIfStmt, "", s.Pos(), s.End())
		case *ast.ForStmt:
			v.appendLogicBlockPos(parentID, parentQname, types.NodeLoopStmt, "for", s.Pos(), s.End())
		case *ast.RangeStmt:
			v.appendLogicBlockPos(parentID, parentQname, types.NodeLoopStmt, "range", s.Pos(), s.End())
		case *ast.SwitchStmt:
			v.appendLogicBlockPos(parentID, parentQname, types.NodeSwitchStmt, "", s.Pos(), s.End())
		case *ast.TypeSwitchStmt:
			v.appendLogicBlockPos(parentID, parentQname, types.NodeSwitchStmt, "type", s.Pos(), s.End())
		case *ast.ReturnStmt:
			v.appendLogicBlockPos(parentID, parentQname, types.NodeReturnStmt, "", s.Pos(), s.End())
		case *ast.AssignStmt:
			// Capture `ch := make(chan T, n)` before the generic CallExpr case fires.
			// Maps LHS variable name → Channel node ID so sends_to/recvs_from edges
			// can point at the actual Channel node rather than an anonymous CallSite.
			// Note: channel parameters (e.g. `out chan<- int`) are not captured here;
			// those fall back to CallSite as destination (best-effort only).
			if len(s.Lhs) == 1 && len(s.Rhs) == 1 {
				if isMakeChan(s.Rhs[0]) {
					call := s.Rhs[0].(*ast.CallExpr)
					if chanID := v.emitChannelFromMake(parentID, call); chanID != "" {
						if lhsIdent, ok := s.Lhs[0].(*ast.Ident); ok {
							v.chanVarIDs[lhsIdent.Name] = chanID
						}
						assignedMakeChan[call.Pos()] = true
					}
				}
			}
		case *ast.CallExpr:
			id := v.appendLogicBlockPos(parentID, parentQname, types.NodeCallSite, "", s.Pos(), s.End())
			// Pending edge: CallSite -calls-> callee — resolved in Pass 2.
			v.pending = append(v.pending, parsePendingFromCall(id, s, v.fset))
			// Concurrency phase 2: lock/unlock edges. Receiver resolution
			// uses types.Info when available; falls back to AST-only INFERRED
			// matching otherwise. No-op for non-mutex calls.
			v.maybeEmitLockEdge(parentID, s)
			// Concurrency phase 3: emit Channel node for make(chan ...) calls
			// that were NOT already handled by the AssignStmt path above
			// (prevents duplicate Channel nodes for `ch := make(chan T, n)`).
			if isMakeChan(s) && !assignedMakeChan[s.Pos()] {
				v.emitChannelFromMake(parentID, s)
			}
		case *ast.GoStmt:
			goroutineID := v.appendLogicBlockPos(parentID, parentQname, types.NodeGoroutine, "", s.Pos(), s.End())
			v.edges = append(v.edges, types.Edge{
				Src: parentID, Dst: goroutineID, Type: types.EdgeSpawns, Count: 1,
				Confidence: types.ConfExtracted,
			})
			// Emit sends_to/recvs_from from goroutine body to known channels.
			v.emitGoroutineChannelEdges(goroutineID, s.Call)
			return false // goroutine body handled by emitGoroutineChannelEdges; prevent double-walk
		case *ast.SendStmt:
			chanName := channelVarName(s.Chan)
			if chanName != "" {
				if chanID, ok := v.chanVarIDs[chanName]; ok {
					v.edges = append(v.edges, types.Edge{
						Src: parentID, Dst: chanID, Type: types.EdgeSendsTo,
						Count: 1, Confidence: types.ConfExtracted,
					})
					break
				}
			}
			// Fallback: channel not in chanVarIDs (parameter, return value, field, etc.)
			id := v.appendLogicBlockPos(parentID, parentQname, types.NodeCallSite, "send", s.Pos(), s.End())
			v.edges = append(v.edges, types.Edge{
				Src: parentID, Dst: id, Type: types.EdgeSendsTo,
				Count: 1, Confidence: types.ConfExtracted,
			})
		case *ast.UnaryExpr:
			if s.Op == token.ARROW {
				chanName := channelVarName(s.X)
				if chanName != "" {
					if chanID, ok := v.chanVarIDs[chanName]; ok {
						v.edges = append(v.edges, types.Edge{
							Src: parentID, Dst: chanID, Type: types.EdgeRecvsFrom,
							Count: 1, Confidence: types.ConfExtracted,
						})
						break
					}
				}
				id := v.appendLogicBlockPos(parentID, parentQname, types.NodeCallSite, "recv", s.Pos(), s.End())
				v.edges = append(v.edges, types.Edge{
					Src: parentID, Dst: id, Type: types.EdgeRecvsFrom,
					Count: 1, Confidence: types.ConfExtracted,
				})
			}
		}
		return true
	})
}

// isMakeChan returns true when expr is a `make(chan T, ...)` call expression.
func isMakeChan(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || ident.Name != "make" {
		return false
	}
	if len(call.Args) < 1 {
		return false
	}
	_, ok = call.Args[0].(*ast.ChanType)
	return ok
}

// channelVarName returns the simple name of the channel operand if it is a
// plain identifier (e.g. ch in "ch <- v" or "<-ch"). Returns "" for complex
// expressions like field selectors or index expressions.
func channelVarName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// appendLogicBlockPos creates a logic-block (or CallSite/Goroutine) node and
// a contains-edge from the enclosing parent. Returns the new node's ID.
func (v *declVisitor) appendLogicBlockPos(parentID, parentQname string, t types.NodeType, subKind string, startPos, endPos token.Pos) string {
	startLn, startBy := v.pos(startPos)
	endLn, endBy := v.pos(endPos)
	qname := fmt.Sprintf("%s#%s@%d", parentQname, t, startBy)
	id := MakeID(qname, "go", startBy)
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: t, Name: string(t), QualifiedName: qname,
		FilePath: v.relPath, StartLine: startLn, EndLine: endLn,
		StartByte: startBy, EndByte: endBy,
		Language: "go", Confidence: types.ConfExtracted, SubKind: subKind,
	})
	v.edges = append(v.edges, types.Edge{
		Src: parentID, Dst: id, Type: types.EdgeContains, Count: 1, Confidence: types.ConfExtracted,
	})
	return id
}

// parsePendingFromCall extracts a best-effort callee qname from a *ast.CallExpr.
// The result is consumed in Pass 2 (Resolve) to materialize a `calls` edge.
func parsePendingFromCall(srcID string, c *ast.CallExpr, fset *token.FileSet) parse.PendingRef {
	target := exprName(c.Fun)
	pos := fset.Position(c.Pos())
	return parse.PendingRef{
		SrcID:       srcID,
		EdgeType:    types.EdgeCalls,
		TargetQName: target,
		HintFile:    pos.Filename,
		Line:        pos.Line,
	}
}
