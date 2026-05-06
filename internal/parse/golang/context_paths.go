package golang

import (
	"go/ast"
	gotypes "go/types"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// context_paths.go implements P2 of the dogfood plan (CKS deep-dive § 4.1
// "Graph 3: Control Flow"): detect Go `context.With*` constructor calls and
// emit timeout_path / cancellation_path edges anchored on the enclosing
// Function/Method.
//
// Edge shape (self-loop): every edge is `Src == Dst == enclosing func ID`,
// `Line` = call-site line. Multiple distinct sites in the same function
// produce multiple edges (different Line keeps them distinct under the
// graph.Build 4-tuple dedup `<src,dst,type,line>`).
//
// Detection mapping:
//   context.WithTimeout(parent, duration)   → timeout_path
//   context.WithDeadline(parent, time)      → timeout_path  (deadline ≡ timeout variant)
//   context.WithCancel(parent)              → cancellation_path
//   context.WithCancelCause(parent)         → cancellation_path  (Go 1.20+)
//
// Confidence rules:
//   - typesInfo present and the called func resolves to package "context":
//     EXTRACTED. Zero false positives in this branch — go/types confirms the
//     stdlib context package binding.
//   - typesInfo nil (AST-only mode): match `context.<Name>` selector by
//     string. INFERRED — a user-defined package aliased to `context` with a
//     same-named func would slip through. False-positive set is small
//     (renaming a package to `context` is unusual in real codebases).
//
// retry_path is intentionally NOT emitted (TODO in pkg/types/enums.go) —
// the heuristic for retry detection (loop + RPC call + error branch) is
// too noisy without a typed retry primitive to anchor on; deferred to a
// follow-up that targets specific backoff libraries / patterns.

// emitContextPaths is the per-file entry point for the P2 control-flow pass.
// Walks each top-level FuncDecl body looking for context.With* call sites
// and emits timeout_path / cancellation_path self-loop edges on the
// enclosing function. Idempotent: the function is called once per file by
// ParseFile after ast.Walk + emitDistributedDecls, so v.nodes already has
// the Function/Method node we anchor on.
//
// Anonymous function literals (`go func() { ctx, _ := context.WithTimeout(...); }()`)
// are NOT a separate emission target — V0 doesn't synthesise nodes for
// FuncLit, so the edge instead anchors on the FuncDecl that contains the
// literal. Acceptable trade-off: the funcdecl IS the unit a graph consumer
// asks about ("which functions create timeouts?"), and a per-funclit edge
// would have nowhere to attach.
func (v *declVisitor) emitContextPaths(f *ast.File) {
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		funcID, _ := v.funcDeclIDQname(fd)
		if funcID == "" {
			continue
		}
		v.scanFuncBodyForContextPaths(funcID, fd.Body)
	}
}

// scanFuncBodyForContextPaths walks a function body and, for every CallExpr
// matching a context.With* constructor, appends a self-loop edge of the
// appropriate type. Shape and confidence determined by classifyContextCall.
func (v *declVisitor) scanFuncBodyForContextPaths(funcID string, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		edgeType, conf := v.classifyContextCall(call)
		if edgeType == "" {
			return true
		}
		pos := v.fset.Position(call.Pos())
		v.edges = append(v.edges, types.Edge{
			Src: funcID, Dst: funcID, Type: edgeType,
			Line: pos.Line, Count: 1, Confidence: conf,
			FilePath: v.relPath,
		})
		return true
	})
}

// classifyContextCall returns the edge type and confidence for a context.With*
// call, or ("", "") when the call is not one of the recognised constructors.
//
// Selector-shape requirement: call.Fun must be `<X>.<Sel>` where Sel ∈
// {WithTimeout, WithDeadline, WithCancel, WithCancelCause}. The receiver `X`
// is matched two ways:
//   - typesInfo path: ObjectOf(Sel) resolves to a *types.Func whose
//     Pkg().Path() == "context". This is the only way to be sure — handles
//     dot-imports, renamed imports, and rejects user-defined `context`
//     packages that happen to expose the same function names.
//   - AST-only fallback: `X.(*ast.Ident).Name == "context"`. Trusts the
//     conventional import name. False positives possible (a `context`-named
//     user package with same function names) but emit INFERRED so consumers
//     can filter.
func (v *declVisitor) classifyContextCall(call *ast.CallExpr) (types.EdgeType, types.Confidence) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", ""
	}
	edgeType := contextEdgeTypeForName(sel.Sel.Name)
	if edgeType == "" {
		return "", ""
	}
	if v.typesInfo != nil {
		obj := v.typesInfo.ObjectOf(sel.Sel)
		if obj == nil {
			return "", ""
		}
		fn, ok := obj.(*gotypes.Func)
		if !ok || fn.Pkg() == nil {
			return "", ""
		}
		if fn.Pkg().Path() != "context" {
			return "", ""
		}
		return edgeType, types.ConfExtracted
	}
	// AST-only fallback: receiver must be the bare identifier "context".
	id, ok := sel.X.(*ast.Ident)
	if !ok || id.Name != "context" {
		return "", ""
	}
	return edgeType, types.ConfInferred
}

// contextEdgeTypeForName maps a context.With* function name to the edge
// type we emit. WithDeadline rolls into timeout_path because deadline is
// a wall-clock bound — semantically identical for graph queries that ask
// "which functions impose a time budget?".
func contextEdgeTypeForName(name string) types.EdgeType {
	switch name {
	case "WithTimeout", "WithDeadline":
		return types.EdgeTimeoutPath
	case "WithCancel", "WithCancelCause":
		return types.EdgeCancellationPath
	}
	return ""
}
