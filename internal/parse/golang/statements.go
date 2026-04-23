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
		case *ast.CallExpr:
			id := v.appendLogicBlockPos(parentID, parentQname, types.NodeCallSite, "", s.Pos(), s.End())
			// Pending edge: CallSite -calls-> callee — resolved in Pass 2.
			v.pending = append(v.pending, parsePendingFromCall(id, parentQname, s, v.fset))
		case *ast.GoStmt:
			id := v.appendLogicBlockPos(parentID, parentQname, types.NodeGoroutine, "", s.Pos(), s.End())
			v.edges = append(v.edges, types.Edge{
				Src: parentID, Dst: id, Type: types.EdgeSpawns, Count: 1,
				Confidence: types.ConfExtracted,
			})
		case *ast.SendStmt:
			id := v.appendLogicBlockPos(parentID, parentQname, types.NodeCallSite, "send", s.Pos(), s.End())
			v.edges = append(v.edges, types.Edge{
				Src: parentID, Dst: id, Type: types.EdgeSendsTo, Count: 1,
				Confidence: types.ConfExtracted,
			})
		case *ast.UnaryExpr:
			if s.Op == token.ARROW {
				id := v.appendLogicBlockPos(parentID, parentQname, types.NodeCallSite, "recv", s.Pos(), s.End())
				v.edges = append(v.edges, types.Edge{
					Src: parentID, Dst: id, Type: types.EdgeRecvsFrom, Count: 1,
					Confidence: types.ConfExtracted,
				})
			}
		}
		return true
	})
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
func parsePendingFromCall(srcID, parentQname string, c *ast.CallExpr, fset *token.FileSet) parse.PendingRef {
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
