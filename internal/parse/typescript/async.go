package typescript

import (
	"fmt"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TS W-B W2 — async/await detection (schema 1.10 slots NodeAwaitPoint,
// EdgeAwaits).
//
// Spec: docs/design/ts-async-await-and-interface.md §2.1, §3.2, §4.2, §5.0.
// Dispatch index: docs/DISPATCH-WITHIN-LANG-SEMANTICS.md §2 Phase 5 W-B W2.
//
// Emits:
//
//	NodeAwaitPoint: one per `await_expression` whose start byte falls
//	                 inside a Function/Method interval. Top-level `await`
//	                 (no enclosing function) is dropped — V0 doesn't
//	                 model module-scope async.
//	EdgeAwaits:      Function/Method → AwaitPoint  ("parent contains
//	                 suspension point").
//
// Per §5.0 decisions (2026-05-11):
//
//   - Q1: NodeAwaitPoint is a first-class NodeType — schema 1.10 slot
//     `pkg/types/enums.go` index 34.
//   - Q2: async modifier reflected as Function/Method SubKind="async" —
//     set at the canonical declaration emit site (declarations.go::runQuery).
//     Mirror of Solidity W2's SubKind="virtual"/"override" idiom.
//   - Q3: EdgeAsyncCall skipped — callee join uses AwaitPoint.StartByte /
//     EndByte against the CallSite emitted by statements.go (positional
//     overlap, same idiom as `spawns` → goroutine body). One direction
//     only: Function → AwaitPoint.
//   - Q5 (declaration merging): each `await` is its own AwaitPoint
//     (line/byte-distinguished). No dedup.
//   - Multiple awaits in one function → separate AwaitPoint nodes per
//     call site.
//   - Naming: QualifiedName = `<parentQname>#AwaitPoint@<startByte>`
//     mirrors statements.go::emitStatementNode so cross-pass IDs collide
//     deterministically when they should and never otherwise.
//     Name = "await <callee>" when extractable, else "await".
//
// V0 limitations (carried forward from track-c §7):
//
//   - Arrow function await inside a named function: the AwaitPoint
//     anchors on the outer named function (intervals walker doesn't
//     descend into arrow bodies as separate intervals — they aren't
//     Function/Method declarations). Documented in
//     testdata/async/await_in_branch.ts.
//   - `for await ... of` loops emit only the LoopStmt; the implicit
//     await on each iteration is not surfaced as a separate AwaitPoint.

// runAsync walks the parse tree once and emits NodeAwaitPoint + EdgeAwaits
// for every await_expression that falls inside a known Function/Method
// interval.
//
// Ordering: must run after declarations.go::runQuery has populated
// v.nodes (collectFnIntervalsFromTree reads them). visit() schedules
// it after runBodyStatements so any new statement nodes are also
// already emitted — the await→CallSite join is positional (StartByte
// overlap), not relational, so the runAsync vs runBodyStatements
// order doesn't actually matter, but keeping them contiguous in
// source order makes the body-walk family easy to find.
func (v *declVisitor) runAsync() {
	intervals := collectFnIntervalsFromTree(v)
	if len(intervals) == 0 {
		return
	}
	qnameByID := make(map[string]string, len(v.nodes))
	for _, n := range v.nodes {
		qnameByID[n.ID] = n.QualifiedName
	}
	v.walkAwaits(v.root, intervals, qnameByID)
}

// walkAwaits is the recursive workhorse — mirrors statements.go::walkStatements
// in shape. Hand-rolled recursion (instead of a tree-sitter query) keeps
// the dynamic "is this inside a Function/Method interval" predicate easy
// to express; query captures would need a post-filter regardless.
func (v *declVisitor) walkAwaits(n *sitter.Node, intervals []fnInterval, qnameByID map[string]string) {
	if n == nil {
		return
	}
	if n.Kind() == "await_expression" {
		startByte := int(n.StartByte())
		if parentID, ok := findEnclosingFn(intervals, startByte); ok {
			v.emitAwaitPoint(n, parentID, qnameByID[parentID])
		}
	}
	for i := uint(0); i < uint(n.ChildCount()); i++ {
		v.walkAwaits(n.Child(i), intervals, qnameByID)
	}
}

// emitAwaitPoint appends one NodeAwaitPoint to v.nodes and the EdgeAwaits
// edge from its enclosing function. Name carries the best-effort callee
// identifier so search / viewer hover text is informative without needing
// a positional join.
func (v *declVisitor) emitAwaitPoint(awaitNode *sitter.Node, parentID, parentQname string) {
	startByte := int(awaitNode.StartByte())
	endByte := int(awaitNode.EndByte())
	startLine := int(awaitNode.StartPosition().Row) + 1
	endLine := int(awaitNode.EndPosition().Row) + 1
	qname := fmt.Sprintf("%s#AwaitPoint@%d", parentQname, startByte)
	id := makeID(qname, "ts", startByte)
	name := "await"
	if callee := extractAwaitCalleeName(awaitNode, v.src); callee != "" {
		name = "await " + callee
	}
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: types.NodeAwaitPoint, Name: name, QualifiedName: qname,
		FilePath: v.rel, StartLine: startLine, EndLine: endLine,
		StartByte: startByte, EndByte: endByte,
		Language: "ts", Confidence: types.ConfExtracted,
	})
	v.edges = append(v.edges, types.Edge{
		Src: parentID, Dst: id, Type: types.EdgeAwaits, Count: 1,
		Confidence: types.ConfExtracted,
	})
}

// extractAwaitCalleeName best-effort extracts the awaited expression's
// callee name for the AwaitPoint's display Name. Shapes handled:
//
//	await foo()        → "foo"
//	await obj.foo()    → "foo"
//	await foo          → "foo"  (bare identifier — Promise variable)
//
// Returns "" for shapes V0 doesn't model (`await (expr)`, `await new Foo()`,
// `await import(...)`, computed-property calls). The AwaitPoint is still
// emitted in those cases with Name="await" — only the display label
// degrades, not the suspension-point detection itself.
func extractAwaitCalleeName(awaitNode *sitter.Node, src []byte) string {
	for i := uint(0); i < uint(awaitNode.ChildCount()); i++ {
		c := awaitNode.Child(i)
		switch c.Kind() {
		case "await":
			continue
		case "call_expression":
			return extractCalleeName(c, src)
		case "identifier":
			return c.Utf8Text(src)
		case "member_expression":
			if prop := c.ChildByFieldName("property"); prop != nil {
				return prop.Utf8Text(src)
			}
		}
	}
	return ""
}

// isAsyncFunctionLike returns true if the function-like declaration that
// owns this name capture carries the `async` modifier. Walks the parent
// chain until it hits the nearest function_declaration / method_definition
// / function_expression / arrow_function (or a generator variant), then
// scans that node's direct children for the `async` keyword.
//
// Tree-sitter-typescript grammar quirks:
//
//   - `async` appears as a direct child of the function-like node before
//     the `function` keyword / property name, so a single linear scan of
//     direct children is sufficient (no recursion needed).
//   - Generator forms (`async function* foo`) carry both `async` and `*`
//     siblings; we treat generators as async-eligible (async generators
//     are legal TS — `for await (const x of gen())` consumes them).
//   - For arrow functions assigned to a variable
//     (`const f = async () => {}`), the parser captures Function from the
//     variable declarator's name. We still need to climb past
//     variable_declarator/lexical_declaration to find the arrow_function;
//     the parent loop does this transparently.
func isAsyncFunctionLike(nameNode *sitter.Node) bool {
	if nameNode == nil {
		return false
	}
	for cur := nameNode.Parent(); cur != nil; cur = cur.Parent() {
		switch cur.Kind() {
		case "function_declaration", "method_definition",
			"function_expression", "arrow_function",
			"generator_function_declaration", "generator_function":
			for i := uint(0); i < uint(cur.ChildCount()); i++ {
				if cur.Child(i).Kind() == "async" {
					return true
				}
			}
			return false
		}
	}
	return false
}
