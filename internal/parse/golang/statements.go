package golang

import (
	"fmt"
	"go/ast"
	"go/token"
	gotypes "go/types"

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
			// Pending edge: CallSite -(calls|invokes)-> callee — resolved in Pass 2.
			// Track C P1b: classify dispatch kind so non-static dispatches
			// surface as `invokes` with a populated dispatch_kind metadata
			// column instead of being conflated with static `calls`.
			v.pending = append(v.pending, v.parsePendingFromCall(id, s))
			// Track C P1b: for dispatch kinds whose target is unresolvable
			// (closure literal, func value, method value) — Resolve would
			// drop the PendingRef because TargetQName has no matching
			// Function/Method node — we emit a direct self-loop on the
			// parent function so the edge isn't lost. Self-loop semantics
			// mirror timeout_path / cancellation_path: marker edges that
			// say "this function performs <kind> dispatch" without naming
			// a runtime target. interface_method dispatch resolves cleanly
			// to a Method node so we leave it in the PendingRef path.
			v.maybeEmitInvokesSelfLoop(parentID, s)
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
			// W-A §3.3 fix (P2 #8): named-function goroutines like
			// `go x.method()` previously emitted no calls/invokes edge,
			// so the cross-function lock propagator skipped the body of
			// x.method entirely (it could only see anonymous goroutine
			// literals via the intra-fn parent attribution path). Emit
			// a PendingRef here so Pass 2 Resolve materialises a calls
			// (or invokes, for interface dispatch) edge from the parent
			// function to the goroutine's target. Anonymous `go func(){}()`
			// is skipped — the FuncLit body has no resolvable target
			// qname and the existing parent-attribution path already
			// covers field touches inside it.
			if s.Call != nil {
				if _, isFuncLit := s.Call.Fun.(*ast.FuncLit); !isFuncLit {
					v.pending = append(v.pending, v.parsePendingFromCall(parentID, s.Call))
				}
			}
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
// The result is consumed in Pass 2 (Resolve) to materialize a `calls` (static)
// or `invokes` (non-static dispatch) edge.
//
// Track C P1b: classifies dispatch kind via types.Info when available and
// stamps the result on PendingRef.DispatchKind. The five outcomes:
//
//	dispatch_kind == ""                  → static call (EdgeCalls)
//	dispatch_kind == "interface_method"  → callee.Recv() is *types.Interface
//	dispatch_kind == "func_value"        → CallExpr.Fun is *ast.Ident bound to
//	                                       a var/param of *types.Signature type
//	dispatch_kind == "method_value"      → CallExpr.Fun is *ast.SelectorExpr
//	                                       whose Sel is a Field of *types.Signature
//	dispatch_kind == "closure"           → CallExpr.Fun is an *ast.FuncLit
//
// AST-only fallback (typesInfo == nil): only the closure case is detectable
// without type info; everything else falls back to EdgeCalls. Real graphs
// run the typed path, so the AST-only mode is mainly a test convenience.
func (v *declVisitor) parsePendingFromCall(srcID string, c *ast.CallExpr) parse.PendingRef {
	target := exprName(c.Fun)
	pos := v.fset.Position(c.Pos())
	pr := parse.PendingRef{
		SrcID:       srcID,
		EdgeType:    types.EdgeCalls,
		TargetQName: target,
		HintFile:    pos.Filename,
		Line:        pos.Line,
	}
	// Builtins (len/cap/make/append/new/delete/...) have no graph node.
	// Leaving the bare name ("len") lets Pass 2's bare-name fallback bind it
	// to an unrelated method that happens to share the name (e.g. a type with
	// a method literally named len) — a false cross-subsystem call edge. Clear
	// the target so Resolve drops it instead of guessing.
	if v.isBuiltinCall(c.Fun) {
		pr.TargetQName = ""
		return pr
	}
	dispatchKind := v.classifyCallDispatch(c)
	switch dispatchKind {
	case "interface_method":
		// Interface dispatch resolves to the interface's Method node. Qualify
		// it to "pkg.Interface.Method" (via go/types) so Pass 2 matches the
		// exact interface method rather than bare-name binding to a same-named
		// concrete method on an unrelated type.
		pr.EdgeType = types.EdgeInvokes
		pr.DispatchKind = dispatchKind
		if q := v.qualifiedStaticTarget(c.Fun); q != "" {
			pr.TargetQName = q
		}
	case "closure", "func_value", "method_value":
		// Target is unresolvable (closure literal / runtime func value /
		// stored callback). The direct self-loop emission in
		// maybeEmitInvokesSelfLoop covers this case. Leave the PendingRef
		// here as a `calls` so it stays out of the way (Resolve will
		// drop it because the TargetQName won't match).
	default:
		// Static dispatch (concrete method / package function). exprName
		// captured only the bare callee name (e.g. "Size"), which Pass 2
		// resolved by bare-name suffix match with last-write-wins — so
		// `valSet.Size()` could bind to an unrelated `pathdb.Database.Size`
		// in another package. When go/types is available, replace the bare
		// name with the callee's fully-qualified qname so it matches the
		// exact definition node. Falls back to the bare name (AST-only
		// builds, or callees we can't qualify) so behaviour is unchanged
		// when type info is absent.
		if q := v.qualifiedStaticTarget(c.Fun); q != "" {
			pr.TargetQName = q
		}
	}
	return pr
}

// isBuiltinCall reports whether c.Fun is a Go builtin (len, cap, make, append,
// new, delete, copy, close, panic, recover, print, println). Builtins are
// called as a plain identifier that resolves to a *types.Builtin object.
// Requires type info; without it we cannot distinguish a builtin from a
// same-named user function, so we conservatively return false.
func (v *declVisitor) isBuiltinCall(fun ast.Expr) bool {
	if v.typesInfo == nil {
		return false
	}
	id, ok := fun.(*ast.Ident)
	if !ok {
		return false
	}
	_, isBuiltin := v.typesInfo.ObjectOf(id).(*gotypes.Builtin)
	return isBuiltin
}

// qualifiedStaticTarget builds the fully-qualified callee qname for a
// statically-dispatched call using go/types, in the same form the callee's
// definition node carries (see visitFuncDecl): "pkg.RecvType.Method" for a
// concrete method and "pkg.Func" for a package function. Returns "" when type
// info is unavailable or the callee is not a concrete *types.Func with a
// package, so the caller keeps the bare-name fallback.
func (v *declVisitor) qualifiedStaticTarget(fun ast.Expr) string {
	if v.typesInfo == nil {
		return ""
	}
	var obj gotypes.Object
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		obj = v.typesInfo.ObjectOf(f.Sel)
	case *ast.Ident:
		obj = v.typesInfo.ObjectOf(f)
	default:
		return ""
	}
	fn, ok := obj.(*gotypes.Func)
	if !ok || fn.Pkg() == nil {
		return ""
	}
	sig, ok := fn.Type().(*gotypes.Signature)
	if !ok {
		return ""
	}
	pkg := fn.Pkg().Name()
	if recv := sig.Recv(); recv != nil {
		rt := recv.Type()
		if ptr, ok := rt.(*gotypes.Pointer); ok {
			rt = ptr.Elem()
		}
		named, ok := rt.(*gotypes.Named)
		if !ok || named.Obj() == nil {
			return ""
		}
		return pkg + "." + named.Obj().Name() + "." + fn.Name()
	}
	return pkg + "." + fn.Name()
}

// maybeEmitInvokesSelfLoop emits a self-loop `invokes` edge on the parent
// function for dispatch kinds whose runtime target cannot be resolved at
// AST time. Dst == Src by design — self-loop semantics signal "this
// function performs <dispatch_kind> dispatch" without claiming a callee.
//
// The parentID passed by the caller is a CallSite ID (appendLogicBlockPos
// returns the call-site, not the enclosing function). We climb to the
// enclosing function via the same callSiteParent map Resolve uses — but
// inline at AST time, by deriving the parent function's qname from the
// call-site qname suffix construction "<parentQname>#<Kind>@<offset>".
//
// Implementation note: at this AST visitor call site we already know the
// enclosing function's ID (the v.declVisitor's currently-walked function),
// but the existing emitFunctionBodyPos signature accepts a parentID that
// happens to be the function ID (not the call-site ID — the call-site is
// minted inside the case body and assigned to local var `id`). So we use
// parentID directly. Reading emitFunctionBodyPos confirms parentID is the
// function ID (visitFuncDecl passes `id` from MakeID(funcQname, ...)).
func (v *declVisitor) maybeEmitInvokesSelfLoop(parentFuncID string, c *ast.CallExpr) {
	if c == nil || parentFuncID == "" {
		return
	}
	dispatchKind := v.classifyCallDispatch(c)
	switch dispatchKind {
	case "closure", "func_value", "method_value":
		// fall through
	default:
		return
	}
	pos := v.fset.Position(c.Pos())
	conf := types.ConfExtracted
	if v.typesInfo == nil {
		conf = types.ConfInferred
	}
	v.edges = append(v.edges, types.Edge{
		Src: parentFuncID, Dst: parentFuncID,
		Type:         types.EdgeInvokes,
		Line:         pos.Line,
		Count:        1,
		Confidence:   conf,
		FilePath:     v.relPath,
		DispatchKind: dispatchKind,
	})
}

// classifyCallDispatch returns the dispatch_kind tag for c, or "" when c is
// a static call. Uses types.Info when available; falls back to AST-only
// closure detection otherwise (the only kind unambiguously visible at the
// syntactic layer).
//
// Order matters: closure literal beats SelectorExpr beats Ident, because a
// `func(){...}()` call's Fun is *ast.FuncLit (no Selector / no Ident path).
func (v *declVisitor) classifyCallDispatch(c *ast.CallExpr) string {
	if c == nil {
		return ""
	}
	// Closure literal call: `func() { ... }()`. Detectable at AST layer alone.
	if _, ok := c.Fun.(*ast.FuncLit); ok {
		return "closure"
	}
	if v.typesInfo == nil {
		return ""
	}
	switch fun := c.Fun.(type) {
	case *ast.SelectorExpr:
		// Try Selections first — gives precise interface-vs-concrete answer.
		if sel, ok := v.typesInfo.Selections[fun]; ok {
			recv := sel.Recv()
			if recv == nil {
				return ""
			}
			// Strip pointer to find the underlying type.
			if ptr, ok := recv.(*gotypes.Pointer); ok {
				recv = ptr.Elem()
			}
			if _, isIface := recv.Underlying().(*gotypes.Interface); isIface {
				return "interface_method"
			}
			// A field of function type whose Sel resolves to a *types.Var
			// (struct field) carrying *types.Signature is a "method_value"
			// dispatch (the field is a stored function pointer). Distinguish
			// from a real method by checking the Sel's object kind.
			if obj := v.typesInfo.ObjectOf(fun.Sel); obj != nil {
				if vobj, ok := obj.(*gotypes.Var); ok {
					if _, isSig := vobj.Type().Underlying().(*gotypes.Signature); isSig {
						return "method_value"
					}
				}
			}
			return ""
		}
		// No Selection: Sel might still resolve to a Var-of-Signature
		// (package-level func value referenced via package selector).
		if obj := v.typesInfo.ObjectOf(fun.Sel); obj != nil {
			if vobj, ok := obj.(*gotypes.Var); ok {
				if _, isSig := vobj.Type().Underlying().(*gotypes.Signature); isSig {
					return "func_value"
				}
			}
		}
	case *ast.Ident:
		// Bare identifier call: `cb(arg)` where cb is a func-typed var/param.
		// Distinguish from a regular function call by checking the object
		// kind — *types.Func means static dispatch, *types.Var of signature
		// type means func value.
		if obj := v.typesInfo.ObjectOf(fun); obj != nil {
			if vobj, ok := obj.(*gotypes.Var); ok {
				if _, isSig := vobj.Type().Underlying().(*gotypes.Signature); isSig {
					return "func_value"
				}
			}
		}
	}
	return ""
}
