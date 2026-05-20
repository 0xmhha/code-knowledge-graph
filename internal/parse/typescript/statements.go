// Package typescript — statements.go emits Pass-1 statement-level nodes
// from inside TS/JS function bodies, mirroring the Go parser's
// statements.go (internal/parse/golang/statements.go). Five kinds:
//
//   - IfStmt     ← `if_statement`
//   - LoopStmt   ← `for_statement` / `for_in_statement` / `for_of_statement`
//     / `while_statement` / `do_statement`
//     (SubKind = "for" / "for-in" / "for-of" / "while" / "do")
//   - SwitchStmt ← `switch_statement`
//   - ReturnStmt ← `return_statement`
//   - CallSite   ← `call_expression`
//
// Each node attaches to its enclosing Function / Method via a `contains`
// edge — same shape Go's appendLogicBlockPos produces. CallSite nodes
// additionally serve as the SrcID for the cross-file PendingRef that the
// Pass-2 Resolve consumes, mirroring the Go pattern where a `Function ->
// contains -> CallSite -> calls -> Method` chain is the canonical
// representation. This replaces the pre-existing body_walk.go-emitted
// PendingRefs that were anchored on the enclosing Function — keeping
// the schema consistent across languages so viewer/api code can treat
// "what calls X" the same way regardless of source language.
//
// Out of scope (deferred — would mirror more of the Go pass):
//
//   - Goroutine / channel / mutex emit (no equivalent runtime in TS).
//   - timeout_path / cancellation_path self-loops (no AbortController
//     pattern detection yet — could land separately).
//   - dispatch_kind classification (closure/func_value/method_value) —
//     TS has no in-process type system; everything stays as static
//     `calls` until a TS LSP server is embedded.
package typescript

import (
	"fmt"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// stmtKind maps a tree-sitter node Kind() to the (CKG NodeType, SubKind)
// pair we emit. Returns ("", "") for kinds that don't correspond to a
// statement-level node we surface in the graph.
//
// for_in_statement subkind disambiguation: the tree-sitter TS grammar
// uses one node kind (`for_in_statement`) for both `for…in` and
// `for…of`, distinguished only by the `operator` field. Callers with
// access to the live *sitter.Node should use stmtKindNode below; the
// string-only stmtKind defaults to "for-in" for that ambiguous shape.
func stmtKind(kind string) (types.NodeType, string) {
	switch kind {
	case "if_statement":
		return types.NodeIfStmt, ""
	case "for_statement":
		return types.NodeLoopStmt, "for"
	case "for_in_statement":
		return types.NodeLoopStmt, "for-in"
	case "for_of_statement":
		return types.NodeLoopStmt, "for-of"
	case "while_statement":
		return types.NodeLoopStmt, "while"
	case "do_statement":
		return types.NodeLoopStmt, "do"
	case "switch_statement":
		return types.NodeSwitchStmt, ""
	case "return_statement":
		return types.NodeReturnStmt, ""
	case "call_expression":
		return types.NodeCallSite, ""
	}
	return "", ""
}

// stmtKindNode is the live-node version of stmtKind. Inspects the
// `operator` field on for_in_statement to distinguish `for…in` from
// `for…of` (the grammar conflates them under a single node kind).
func stmtKindNode(n *sitter.Node, src []byte) (types.NodeType, string) {
	nt, sub := stmtKind(n.Kind())
	if n.Kind() == "for_in_statement" {
		if op := n.ChildByFieldName("operator"); op != nil {
			text := op.Utf8Text(src)
			if text == "of" {
				return types.NodeLoopStmt, "for-of"
			}
		}
	}
	return nt, sub
}

// runBodyStatements is the unified body-walk pass. Replaces the prior
// runBodyCalls (which only handled call_expression and anchored pending
// refs on the enclosing function). Walks the parse tree once, emits a
// node + `contains` edge per statement kind whose start position falls
// inside a Function/Method interval, and for call_expression also enqueues
// a PendingRef anchored on the new CallSite.
//
// Per-CallSite Pending dedup: graphify-style by (parentFn, calleeName,
// line) so multi-arg calls that lexically repeat a callee on one line
// don't double-count. Different lines from one function to one callee
// remain distinct (each call site is a real invocation).
func (v *declVisitor) runBodyStatements() {
	intervals := collectFnIntervalsFromTree(v)
	if len(intervals) == 0 {
		return
	}
	qnameByID := make(map[string]string, len(v.nodes))
	for _, n := range v.nodes {
		qnameByID[n.ID] = n.QualifiedName
	}
	emittedCalls := make(map[string]struct{}, 256)
	v.walkStatements(v.root, intervals, qnameByID, emittedCalls)
}

// walkStatements is the recursive workhorse. We hand-roll the recursion
// (rather than using a tree-sitter query) because the predicate "is
// this node inside any Function/Method interval" depends on dynamic
// state that's painful to express in the query language, and because
// nested statements (e.g. a CallSite whose argument is itself an
// if-shaped expression in JSX) need their own emit pass — recursion is
// the natural fit.
func (v *declVisitor) walkStatements(n *sitter.Node, intervals []fnInterval,
	qnameByID map[string]string, emittedCalls map[string]struct{}) {
	if n == nil {
		return
	}
	if nt, subKind := stmtKindNode(n, v.src); nt != "" {
		startByte := int(n.StartByte())
		if parentID, ok := findEnclosingFn(intervals, startByte); ok {
			stmtID := v.emitStatementNode(nt, subKind, qnameByID[parentID], parentID, n)
			if nt == types.NodeCallSite {
				v.emitPendingFromCall(stmtID, parentID, n, emittedCalls)
			}
		}
	}
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		v.walkStatements(n.Child(uint(i)), intervals, qnameByID, emittedCalls)
	}
}

// emitStatementNode appends one statement-level node to v.nodes and the
// `contains` edge from its enclosing function to it. Returns the new
// node's ID so the caller can use it as the SrcID for any cross-file
// PendingRef.
//
// Naming convention mirrors Go: QualifiedName is
// `<parentQname>#<NodeType>@<startByte>` so multiple statements of the
// same kind in one function get distinct IDs.
func (v *declVisitor) emitStatementNode(nt types.NodeType, subKind, parentQname, parentID string,
	n *sitter.Node) string {
	startByte := int(n.StartByte())
	endByte := int(n.EndByte())
	startLine := int(n.StartPosition().Row) + 1
	endLine := int(n.EndPosition().Row) + 1
	qname := fmt.Sprintf("%s#%s@%d", parentQname, nt, startByte)
	id := makeID(qname, "ts", startByte)
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: nt, Name: string(nt), QualifiedName: qname,
		FilePath: v.rel, StartLine: startLine, EndLine: endLine,
		StartByte: startByte, EndByte: endByte,
		Language: "ts", Confidence: types.ConfExtracted, SubKind: subKind,
	})
	v.edges = append(v.edges, types.Edge{
		Src: parentID, Dst: id, Type: types.EdgeContains, Count: 1,
		Confidence: types.ConfExtracted,
	})
	return id
}

// emitPendingFromCall enqueues a Pass-2 PendingRef for a call_expression
// node. The src is the CallSite we just emitted (matching Go's pattern),
// not the enclosing function.
//
// Two callee shapes the matching tree-sitter query would catch (we
// re-implement here so the walk + query don't drift):
//
//   - bare identifier: `foo()`     → callee = "foo"
//   - selector expr:   `obj.foo()` → callee = "foo" (the property)
//
// parentFn is unused for the SrcID but we keep it as a parameter so the
// dedup key spans (caller, callee, line) — graphify-style.
func (v *declVisitor) emitPendingFromCall(stmtID, parentFn string, n *sitter.Node,
	emittedCalls map[string]struct{}) {
	calleeName := extractCalleeName(n, v.src)
	if calleeName == "" {
		return
	}
	line := int(n.StartPosition().Row) + 1
	key := parentFn + "|" + calleeName + "|" + itoa(line)
	if _, dup := emittedCalls[key]; dup {
		return
	}
	emittedCalls[key] = struct{}{}
	v.pending = append(v.pending, parse.PendingRef{
		SrcID:       stmtID,
		TargetQName: calleeName,
		EdgeType:    types.EdgeCalls,
		Line:        line,
	})
}

// extractCalleeName returns the callee identifier text for a call_expression
// node. Mirrors the queryCallExpression tree-sitter query in queries.go but
// runs structurally, so we don't pay the per-file query-compilation cost
// twice (the body_walk pass would otherwise compile the same query that
// runBodyStatements does).
//
// Returns "" when the function expression isn't an identifier or member
// expression (e.g. immediately-invoked function expressions, calls on
// computed properties).
func extractCalleeName(call *sitter.Node, src []byte) string {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return ""
	}
	switch fn.Kind() {
	case "identifier":
		return fn.Utf8Text(src)
	case "member_expression":
		prop := fn.ChildByFieldName("property")
		if prop != nil {
			return prop.Utf8Text(src)
		}
	}
	return ""
}
