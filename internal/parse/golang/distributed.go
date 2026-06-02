package golang

import (
	"go/ast"
	"go/token"
	gotypes "go/types"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// distributed.go implements E3 (CKS deep-dive § 4.1, "Graph 5: Distributed
// Interaction") MVP for Go: HTTP route handlers, JSON-RPC handler signatures,
// and `client.Call("Service.Method", ...)` style RPC dispatch. Three new
// edge kinds (listens_on / handles_message / rpc_calls) attach to two new
// node kinds (Endpoint / MessageType).
//
// Detection (best-effort, V0 MVP):
//   - listens_on: `http.HandleFunc` / `http.Handle` / `(*ServeMux).HandleFunc`
//     / `(*ServeMux).Handle` calls with a string-literal route. Routes that
//     are computed at runtime (variable / concatenation) are skipped — runtime
//     trace integration is the right hammer for those.
//   - handles_message: a method whose signature matches the JSON-RPC handler
//     shape `func (T) Method(args A, reply *R) error`. The arg type A becomes
//     a NodeMessageType, and a handles_message edge connects method → A.
//   - rpc_calls: a `client.Call("Service.Method", ...)` invocation where the
//     first argument is a "Service.Method" string literal. Resolution by
//     suffix-match against existing function/method qnames; on miss the edge
//     is dropped (V0 simplification — full AMBIGUOUS handling is deferred).
//
// Defer (B1-style follow-up):
//   - gRPC client calls (`stub.RpcMethod(ctx, req)`) need cross-package type
//     info to identify Method as part of an auto-generated `XClient` interface.
//   - gRPC server method registration (`pb.RegisterFooServer(s, &impl{})`).
//   - P2P broadcasters and consensus_flow (CKS § 4.1 deferred set).
//
// Receiver resolution: relies on typesInfo where available. AST-only mode
// emits with INFERRED confidence and broader matching (any `HandleFunc(...)`
// call with first-arg string literal is treated as a route handler).

// emitDistributedDecls is the per-file entry point for the E3 distributed
// pass. Walks every function body for HTTP handler registrations + RPC client
// calls, and every top-level function declaration for JSON-RPC handler
// signatures. Idempotent: duplicate Endpoint/MessageType nodes are deduped
// by qname via endpointNodeIDs / messageNodeIDs maps.
func (v *declVisitor) emitDistributedDecls(f *ast.File) {
	if v.endpointNodeIDs == nil {
		v.endpointNodeIDs = map[string]string{}
	}
	if v.messageNodeIDs == nil {
		v.messageNodeIDs = map[string]string{}
	}
	// W3b gRPC server pass — top-level scan for pb.RegisterXXXServer call
	// sites. Runs BEFORE the per-function body walk so the resulting real
	// Endpoint IDs (cached in endpointNodeIDs) are visible to the client
	// pass via upsertGRPCClientPlaceholder's same-file dedup.
	v.emitGRPCDecls(f)
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		v.maybeEmitJSONRPCHandler(fd)
		if fd.Body == nil {
			continue
		}
		funcID, funcQname := v.funcDeclIDQname(fd)
		// W3b gRPC client pre-pass — register stub variables for this
		// function body before the CallExpr walk fires. Without this,
		// maybeEmitGRPCClientCall would miss the `client.RpcMethod(...)`
		// edges because the stub var isn't bound yet at visit time.
		v.scanFuncBodyForGRPCStubs(fd.Body)
		v.scanFuncBodyForDistributed(funcID, funcQname, fd.Body)
	}
}

// scanFuncBodyForDistributed walks a function body and dispatches each
// CallExpr to the HTTP handler / RPC client / HTTP-client / gRPC-client
// detectors.
func (v *declVisitor) scanFuncBodyForDistributed(parentFuncID, parentFuncQname string, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		v.maybeEmitHTTPListensOn(parentFuncID, parentFuncQname, call)
		v.maybeEmitRPCCall(parentFuncID, call)
		v.maybeEmitHTTPClientCall(parentFuncID, call)
		// W3b: gRPC client call detection. Mirrors the http_calls path —
		// emits one grpc_calls edge per stub method call, with AMBIGUOUS
		// placeholder fallback when the stub var type can't be resolved.
		v.maybeEmitGRPCClientCall(parentFuncID, call)
		return true
	})
}

// funcDeclIDQname recomputes the (qname, id) pair declarations.go.visitFuncDecl
// minted for fd. Required because the distributed pass walks the file AFTER
// the main visitor populated v.nodes, but we don't keep a pos→id reverse map.
// Stays in sync with declarations.go.visitFuncDecl naming convention.
func (v *declVisitor) funcDeclIDQname(fd *ast.FuncDecl) (id, qname string) {
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		recvType := exprName(fd.Recv.List[0].Type)
		qname = v.pkgName + "." + recvType + "." + fd.Name.Name
	} else {
		qname = v.pkgName + "." + fd.Name.Name
	}
	_, startByte := v.pos(fd.Pos())
	return MakeID(qname, "go", startByte), qname
}

// maybeEmitHTTPListensOn checks whether call is one of the supported HTTP
// route registration patterns and, if so, emits a NodeEndpoint + listens_on
// edge from the resolved handler function/method to that endpoint.
//
// Supported patterns (V0 MVP):
//
//	http.HandleFunc("/path", handler)
//	http.HandleFunc("GET /path", handler)   // Go 1.22+ method-prefixed pattern
//	http.Handle("/path", handler)
//	mux.HandleFunc("/path", handler)        // mux: *http.ServeMux (or compatible)
//	mux.Handle("/path", handler)
//
// Routes that are not string literals are skipped — they require runtime
// trace integration to capture the actual path.
//
// Endpoint qname uses the cross-language format `http:METHOD /route` (schema
// 1.9 §6.2). When the route literal is a plain path (no method prefix),
// METHOD defaults to `*` — net/http's stdlib HandleFunc dispatches all HTTP
// methods to the same handler, so a single node represents the union.
func (v *declVisitor) maybeEmitHTTPListensOn(parentFuncID, parentFuncQname string, call *ast.CallExpr) {
	_, isHTTPSel, ok := httpHandleSelector(call)
	if !ok {
		return
	}
	// Need at least (path, handler) after the receiver.
	if len(call.Args) < 2 {
		return
	}
	pattern, ok := stringLiteral(call.Args[0])
	if !ok || pattern == "" {
		return // dynamic or empty route — skip (V0).
		// Empty: Go 1.22 stdlib panics on `http.HandleFunc("", h)` at runtime,
		// but a defensive guard here avoids emitting a malformed Endpoint
		// qname like `http:* ` (trailing space, empty path) if such code
		// somehow compiles.
	}
	// Best-effort confirm receiver is *http.ServeMux or http.Handler-like.
	// Without typesInfo or when receiver is `http`, accept INFERRED.
	conf := v.classifyHTTPHandlerCall(call, isHTTPSel)
	if conf == "" {
		return // user-defined HandleFunc on unrelated type — skip
	}

	httpMethod, route := splitGo122Pattern(pattern)
	endpointID := v.upsertHTTPEndpoint(httpMethod, route, call.Pos(), call.End())
	pos := v.fset.Position(call.Pos())
	handlerID := v.resolveHTTPHandlerArg(call.Args[1])
	if handlerID != "" {
		v.edges = append(v.edges, types.Edge{
			Src: handlerID, Dst: endpointID, Type: types.EdgeListensOn,
			Line: pos.Line, Count: 1, Confidence: conf,
			FilePath: v.relPath,
		})
		_ = parentFuncQname
		return
	}
	// Fallback: handler is in another file or unresolved — defer to Pass 2
	// via a synthetic pending ref. SrcID is the endpoint (we want edge
	// handler→endpoint, but PendingRef wires src→target; resolve.go below
	// is taught to swap src↔dst for listens_on so the final edge points
	// the right way).
	if name := handlerArgName(call.Args[1]); name != "" {
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:       endpointID,
			EdgeType:    types.EdgeListensOn,
			TargetQName: name,
			HintFile:    v.relPath,
			Line:        pos.Line,
		})
	}
	_ = parentFuncID
	_ = parentFuncQname
}

// splitGo122Pattern parses Go 1.22+'s method-prefixed http.HandleFunc
// pattern syntax (`"GET /users"`) and returns (method, route). When the
// pattern has no method prefix, returns ("*", pattern) to indicate the
// handler accepts all HTTP methods — net/http stdlib semantics for the
// plain `"/path"` form.
//
// Only the leading uppercase token followed by a single space and a path
// starting with `/` is recognised as a method. Anything else falls through
// to the wildcard form (conservative: false positives here would mangle
// the route).
func splitGo122Pattern(pattern string) (method, route string) {
	sp := strings.IndexByte(pattern, ' ')
	if sp <= 0 || sp == len(pattern)-1 {
		return "*", pattern
	}
	head, tail := pattern[:sp], pattern[sp+1:]
	if !strings.HasPrefix(tail, "/") {
		return "*", pattern
	}
	for i := 0; i < len(head); i++ {
		c := head[i]
		if c < 'A' || c > 'Z' {
			return "*", pattern
		}
	}
	return head, tail
}

// httpHandleSelector returns (methodName, isHTTPPackageSelector, true) when
// call.Fun is a SelectorExpr whose Sel.Name is "HandleFunc" or "Handle".
// isHTTPPackageSelector is true when the receiver is the bare identifier
// "http" (i.e. `http.HandleFunc`); otherwise the receiver is some value
// (presumed to be a *http.ServeMux or compatible router).
func httpHandleSelector(call *ast.CallExpr) (string, bool, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false, false
	}
	switch sel.Sel.Name {
	case "HandleFunc", "Handle":
	default:
		return "", false, false
	}
	if id, ok := sel.X.(*ast.Ident); ok && id.Name == "http" {
		return sel.Sel.Name, true, true
	}
	return sel.Sel.Name, false, true
}

// classifyHTTPHandlerCall returns the confidence label for an HTTP handler
// call site, or "" when the receiver doesn't look like a real HTTP router
// (false-positive guard for user-defined HandleFunc methods).
//
// With typesInfo: confirms `http.HandleFunc` resolves to net/http and that
// non-`http.` receivers are *http.ServeMux. Returns EXTRACTED on hit.
//
// AST-only fallback: trust `http.HandleFunc` form (INFERRED). For other
// receivers, accept anything named HandleFunc/Handle on a value with 2+ args
// (broad but safe for V0 — the false-positive set is small).
func (v *declVisitor) classifyHTTPHandlerCall(call *ast.CallExpr, isHTTPSel bool) types.Confidence {
	if v.typesInfo == nil {
		if isHTTPSel {
			return types.ConfInferred
		}
		return types.ConfInferred
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	// Resolve the called function and check its package == net/http.
	if obj := v.typesInfo.ObjectOf(sel.Sel); obj != nil && obj.Pkg() != nil {
		if obj.Pkg().Path() == "net/http" {
			return types.ConfExtracted
		}
	}
	// Receiver-type check: *http.ServeMux (or anything from net/http).
	if t := v.typesInfo.TypeOf(sel.X); t != nil {
		if isNetHTTPType(t) {
			return types.ConfExtracted
		}
	}
	// Unknown receiver — most likely user-defined router (chi, gorilla/mux,
	// echo). gorilla/mux's HandleFunc has the same signature shape as
	// stdlib; emit INFERRED so consumers can distinguish.
	return types.ConfInferred
}

// isNetHTTPType returns true when t (after stripping pointer) is a Named
// type declared in package net/http. Used to validate that a call's receiver
// is an HTTP router.
func isNetHTTPType(t gotypes.Type) bool {
	if ptr, ok := t.(*gotypes.Pointer); ok {
		return isNetHTTPType(ptr.Elem())
	}
	named, ok := t.(*gotypes.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "net/http"
}

// resolveHTTPHandlerArg returns the function/method node ID for the handler
// argument expression, or "" when it can't be resolved within the current
// file. Cross-file resolution is handled by the caller via PendingRef.
//
// Supported argument shapes:
//   - Bare identifier:           `usersHandler`              → local scan / typesInfo
//   - Selector:                  `srv.HandleUsers`           → typesInfo lookup
//   - Type-conversion wrapper:   `http.HandlerFunc(handler)` → unwrap then recurse
//   - Function literal:          `func(w, r) {...}`          → "" (skip — V0
//     doesn't synthesise anonymous Function nodes)
func (v *declVisitor) resolveHTTPHandlerArg(arg ast.Expr) string {
	// Unwrap an explicit type-conversion call like `http.HandlerFunc(h)` —
	// idiomatic for `http.Handle` registration. The inner expression is the
	// real handler.
	if call, ok := arg.(*ast.CallExpr); ok && len(call.Args) == 1 {
		if isHandlerFuncConversion(call.Fun) {
			return v.resolveHTTPHandlerArg(call.Args[0])
		}
	}
	switch x := arg.(type) {
	case *ast.Ident:
		if v.typesInfo != nil {
			if obj := v.typesInfo.ObjectOf(x); obj != nil {
				if fn, ok := obj.(*gotypes.Func); ok {
					return v.idForFunc(fn)
				}
			}
		}
		// AST fallback: scan current file's nodes for a matching Function.
		return v.findFuncByName(x.Name)
	case *ast.SelectorExpr:
		if v.typesInfo != nil {
			if obj := v.typesInfo.ObjectOf(x.Sel); obj != nil {
				if fn, ok := obj.(*gotypes.Func); ok {
					return v.idForFunc(fn)
				}
			}
		}
		// AST fallback: try the bare method name in current file.
		return v.findFuncByName(x.Sel.Name)
	case *ast.FuncLit:
		// Anonymous handler — V0 doesn't synthesise nodes for these. Skip.
		return ""
	}
	return ""
}

// findFuncByName returns the ID of a Function/Method node in the current
// file whose Name matches name. Returns "" on no match. Used by the AST-only
// fallback path of resolveHTTPHandlerArg.
func (v *declVisitor) findFuncByName(name string) string {
	for i := range v.nodes {
		n := &v.nodes[i]
		if n.Type != types.NodeFunction && n.Type != types.NodeMethod {
			continue
		}
		if n.Name == name {
			return n.ID
		}
	}
	return ""
}

// handlerArgName extracts a name suitable for cross-file pending-ref lookup
// from a handler argument. Returns "" for anonymous functions or unsupported
// shapes. Unwraps `http.HandlerFunc(h)` type conversions.
func handlerArgName(arg ast.Expr) string {
	if call, ok := arg.(*ast.CallExpr); ok && len(call.Args) == 1 {
		if isHandlerFuncConversion(call.Fun) {
			return handlerArgName(call.Args[0])
		}
	}
	switch x := arg.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return ""
}

// isHandlerFuncConversion returns true when fun is `http.HandlerFunc` (the
// standard adapter for converting a plain function into an http.Handler).
// Matched purely by syntax; users redeclaring an `http.HandlerFunc` in a
// different package would slip through, but the cost of a false unwrap is
// just attempting to resolve the inner expression as a handler — at worst
// no edge is emitted.
func isHandlerFuncConversion(fun ast.Expr) bool {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "http" && sel.Sel.Name == "HandlerFunc"
}

// idForFunc returns the existing Function/Method node ID for fn by
// qname lookup against v.nodes. Returns "" when fn is nil, has no
// package, or no matching node exists in the current file's nodes
// (cross-file or stdlib calls fall through to the PendingRef path).
//
// Earlier this function recomputed an ID via MakeID(qname, "go", offset)
// where offset came from v.fset.Position(fn.Pos()).Offset. That offset
// is the position of the function NAME (per go/types semantics), but
// visitFuncDecl in declarations.go uses the *ast.FuncDecl's Pos —
// the position of the `func` keyword. For methods these differ by
// the receiver-clause width (~17 bytes for "func (s *Server) "), so
// the recomputed ID never matched the emitted Method node, producing
// dangling listens_on edges (every server.go HandleFunc call).
//
// Looking up by qname against v.nodes — populated by the ast.Walk that
// runs before emitDistributedDecls — sidesteps the offset mismatch
// entirely. qname is unique within a package (Go forbids same-name
// methods on the same receiver), so this lookup is unambiguous for
// any handler defined in the same file as its registration.
func (v *declVisitor) idForFunc(fn *gotypes.Func) string {
	if fn == nil {
		return ""
	}
	pkg := fn.Pkg()
	if pkg == nil {
		return ""
	}
	pkgName := pkg.Name()
	qname := pkgName + "." + fn.Name()
	if sig, ok := fn.Type().(*gotypes.Signature); ok && sig.Recv() != nil {
		recvName := receiverTypeName(sig.Recv().Type())
		if recvName != "" {
			qname = pkgName + "." + recvName + "." + fn.Name()
		}
	}
	for i := range v.nodes {
		n := &v.nodes[i]
		if n.Type != types.NodeFunction && n.Type != types.NodeMethod {
			continue
		}
		if n.QualifiedName == qname {
			return n.ID
		}
	}
	return ""
}

// receiverTypeName extracts the bare type name from a method receiver type,
// stripping pointer wrappers. Returns "" when the type isn't a Named type.
func receiverTypeName(t gotypes.Type) string {
	if ptr, ok := t.(*gotypes.Pointer); ok {
		return receiverTypeName(ptr.Elem())
	}
	if named, ok := t.(*gotypes.Named); ok {
		obj := named.Obj()
		if obj != nil {
			return obj.Name()
		}
	}
	return ""
}

// stringLiteral returns the content of a *ast.BasicLit string node and true,
// or ("", false) when e is not a string literal.
func stringLiteral(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	return strings.Trim(lit.Value, "\"`"), true
}

// upsertHTTPEndpoint emits a NodeEndpoint for an HTTP (method, route) pair
// if not already emitted in this file, and returns its node ID. Uses
// endpointNodeIDs keyed by the full `http:METHOD /route` qname so that
// `GET /users` and `POST /users` are distinct endpoints (schema 1.9 §6.2 —
// cross-language qname format shared with the TS parser).
//
// The Name field stores just the route (without the method), matching the
// TS parser's convention and keeping the existing viewer/search behaviour
// (route lookup) intact.
func (v *declVisitor) upsertHTTPEndpoint(method, route string, startPos, endPos token.Pos) string {
	qname := "http:" + method + " " + route
	if id, ok := v.endpointNodeIDs[qname]; ok {
		return id
	}
	startLn, startBy := v.pos(startPos)
	endLn, endBy := v.pos(endPos)
	id := MakeID(qname, "go", startBy)
	v.endpointNodeIDs[qname] = id
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: types.NodeEndpoint,
		Name: route, QualifiedName: qname,
		FilePath: v.relPath, StartLine: startLn, EndLine: endLn,
		StartByte: startBy, EndByte: endBy,
		Language: "go", Confidence: types.ConfExtracted, SubKind: "http",
	})
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1,
		Confidence: types.ConfExtracted,
	})
	return id
}

// upsertMessageType emits a NodeMessageType for qname if not already present
// in this file's emission set, returning the node ID. Used for both gRPC/
// JSON-RPC request types and unresolved Service.Method placeholders.
func (v *declVisitor) upsertMessageType(qname, name, subKind string, startPos, endPos token.Pos) string {
	if id, ok := v.messageNodeIDs[qname]; ok {
		return id
	}
	startLn, startBy := v.pos(startPos)
	endLn, endBy := v.pos(endPos)
	id := MakeID(qname, "go", startBy)
	v.messageNodeIDs[qname] = id
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: types.NodeMessageType,
		Name: name, QualifiedName: qname,
		FilePath: v.relPath, StartLine: startLn, EndLine: endLn,
		StartByte: startBy, EndByte: endBy,
		Language: "go", Confidence: types.ConfExtracted, SubKind: subKind,
	})
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1,
		Confidence: types.ConfExtracted,
	})
	return id
}

// maybeEmitJSONRPCHandler checks whether fd matches the Go net/rpc handler
// signature `func (T) Method(args A, reply *R) error` and, if so, emits a
// NodeMessageType for A and a handles_message edge from the method to A.
//
// Net/rpc dispatches on this exact shape (https://pkg.go.dev/net/rpc).
// The detection is purely structural — it doesn't require the method to be
// registered via `rpc.Register` (which would require cross-function tracking).
// False positives are possible (any 2-arg pointer-second-arg error-returning
// method matches); kept INFERRED to surface the uncertainty.
func (v *declVisitor) maybeEmitJSONRPCHandler(fd *ast.FuncDecl) {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return // free functions can't be JSON-RPC handlers
	}
	if !isJSONRPCSignature(fd) {
		return
	}
	argTypeName := exprName(fd.Type.Params.List[0].Type)
	if argTypeName == "" {
		return
	}
	argPkg, argName := splitSelectorName(fd.Type.Params.List[0].Type)
	var qname string
	if argPkg != "" {
		qname = argPkg + "." + argName
	} else {
		// Local type — qualify with the current package.
		qname = v.pkgName + "." + argName
	}
	methodID, _ := v.funcDeclIDQname(fd)
	msgID := v.upsertMessageType(qname, argName, "rpc_request",
		fd.Type.Params.List[0].Pos(), fd.Type.Params.List[0].End())
	pos := v.fset.Position(fd.Pos())
	v.edges = append(v.edges, types.Edge{
		Src: methodID, Dst: msgID, Type: types.EdgeHandlesMessage,
		Line: pos.Line, Count: 1, Confidence: types.ConfInferred,
		FilePath: v.relPath,
	})
}

// isJSONRPCSignature returns true when fd matches `func (T) Name(A, *R) error`.
// Specifically: 2 params, second param is a pointer type, exactly 1 result,
// and that result is the predeclared `error` type (best-effort name-match
// when typesInfo is unavailable).
func isJSONRPCSignature(fd *ast.FuncDecl) bool {
	t := fd.Type
	if t.Params == nil || t.Results == nil {
		return false
	}
	if paramCount(t.Params) != 2 || paramCount(t.Results) != 1 {
		return false
	}
	// Second param must be a pointer type.
	second := nthParamType(t.Params, 1)
	if _, ok := second.(*ast.StarExpr); !ok {
		return false
	}
	// Result type must be `error`.
	resType := nthParamType(t.Results, 0)
	resID, ok := resType.(*ast.Ident)
	if !ok || resID.Name != "error" {
		return false
	}
	return true
}

// paramCount counts individual fields in a FieldList, accounting for shared
// types like `func(a, b int)` where one Field has 2 Names.
func paramCount(fl *ast.FieldList) int {
	if fl == nil {
		return 0
	}
	n := 0
	for _, f := range fl.List {
		if len(f.Names) == 0 {
			n++
		} else {
			n += len(f.Names)
		}
	}
	return n
}

// nthParamType returns the type expression of the i-th parameter (0-indexed),
// expanding shared-type fields as if they were one Field per Name. Returns
// nil when i is out of range.
func nthParamType(fl *ast.FieldList, i int) ast.Expr {
	if fl == nil {
		return nil
	}
	idx := 0
	for _, f := range fl.List {
		count := len(f.Names)
		if count == 0 {
			count = 1
		}
		if i < idx+count {
			return f.Type
		}
		idx += count
	}
	return nil
}

// splitSelectorName returns (pkg, name) for `pkg.Name` SelectorExpr or for
// `*pkg.Name` StarExpr. Returns ("", name) for bare identifiers and ("", "")
// for unsupported shapes.
func splitSelectorName(e ast.Expr) (string, string) {
	if star, ok := e.(*ast.StarExpr); ok {
		return splitSelectorName(star.X)
	}
	if sel, ok := e.(*ast.SelectorExpr); ok {
		if id, ok := sel.X.(*ast.Ident); ok {
			return id.Name, sel.Sel.Name
		}
	}
	if id, ok := e.(*ast.Ident); ok {
		return "", id.Name
	}
	return "", ""
}

// maybeEmitRPCCall detects `client.Call("Service.Method", args, reply)` —
// the shape used by Go's net/rpc client. When the first argument is a
// "Service.Method" string literal, emits an rpc_calls edge to either an
// existing function/method node (resolved by Pass 2 via PendingRef) or a
// MessageType placeholder when no match is found.
//
// V0 simplification: gRPC stub calls (`stub.RpcMethod(ctx, req)`) are NOT
// detected here — they require recognising the auto-generated `XClient`
// interface, which needs cross-package types.Info chasing. Documented in
// the WORK-PLAN deferred set.
func (v *declVisitor) maybeEmitRPCCall(parentFuncID string, call *ast.CallExpr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Call" {
		return
	}
	if len(call.Args) < 1 {
		return
	}
	target, ok := stringLiteral(call.Args[0])
	if !ok {
		return
	}
	// Heuristic: "Service.Method" form — exactly one dot, both halves
	// non-empty, first char uppercase (RPC convention). Rules out generic
	// .Call usages on unrelated types.
	dot := strings.Index(target, ".")
	if dot <= 0 || dot == len(target)-1 {
		return
	}
	svc, method := target[:dot], target[dot+1:]
	if !isExportedIdent(svc) || !isExportedIdent(method) {
		return
	}
	// Emit a placeholder MessageType for the Service.Method target so the
	// edge has somewhere to land even when the server-side method isn't in
	// the loaded source set.
	pos := v.fset.Position(call.Pos())
	qname := "rpc:" + target
	msgID := v.upsertMessageType(qname, target, "rpc_method", call.Pos(), call.End())
	v.edges = append(v.edges, types.Edge{
		Src: parentFuncID, Dst: msgID, Type: types.EdgeRPCCalls,
		Line: pos.Line, Count: 1, Confidence: types.ConfInferred,
		FilePath: v.relPath,
	})
}

// isExportedIdent returns true when s is a non-empty Go identifier whose
// first rune is uppercase ASCII (the conservative net/rpc convention).
func isExportedIdent(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	if c < 'A' || c > 'Z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_'
		if !ok {
			return false
		}
	}
	return true
}

// maybeEmitHTTPClientCall detects Go HTTP client call sites and emits an
// http_calls edge (schema 1.9 W2) from the enclosing function to a
// placeholder Endpoint. The link pass (internal/link/http_match.go) walks
// these edges after all per-language parsers run and either rewires the
// edge Dst to a matching real Endpoint (cascade: specific verb → wildcard)
// or leaves the placeholder in place as an AMBIGUOUS external-API marker
// (schema-1.9-spec §6.3 (B), §6.9).
//
// Supported patterns (V0 — string-literal URLs only):
//
//	http.Get(url)                             → method=GET
//	http.Post(url, contentType, body)         → method=POST
//	http.PostForm(url, values)                → method=POST
//	http.Head(url)                            → method=HEAD
//	(*http.Client).Get/Post/PostForm/Head     → same methods, receiver-based
//	http.NewRequest(method, url, body)        → method = string-literal arg
//	http.NewRequestWithContext(ctx, m, u, b)  → method = string-literal arg
//
// Computed URLs (variable, concat, fmt.Sprintf) are skipped — the placeholder
// would have no useful qname. Path is extracted from the URL literal: if the
// literal is an absolute URL (`https://api.example.com/foo`), the host portion
// is stripped and the placeholder qname uses just `/foo`; if it's already a
// path (`/api/users`), it's used verbatim. This matches the route literal
// convention used by the server-side detector.
func (v *declVisitor) maybeEmitHTTPClientCall(parentFuncID string, call *ast.CallExpr) {
	if parentFuncID == "" {
		return
	}
	method, urlArg, ok := classifyHTTPClientCall(call)
	if !ok {
		return
	}
	rawURL, ok := stringLiteral(urlArg)
	if !ok || rawURL == "" {
		return // dynamic URL — schema-1.9-spec §3.3 V0 skips computed URLs.
	}
	path := extractURLPath(rawURL)
	if path == "" {
		return
	}
	endpointID := v.upsertHTTPClientPlaceholder(method, path, call.Pos(), call.End())
	pos := v.fset.Position(call.Pos())
	v.edges = append(v.edges, types.Edge{
		Src: parentFuncID, Dst: endpointID, Type: types.EdgeHTTPCalls,
		Line: pos.Line, Count: 1, Confidence: types.ConfInferred,
		FilePath: v.relPath,
	})
}

// classifyHTTPClientCall inspects call's callee shape and returns
// (HTTP method, URL-argument expression, true) when the call matches one of
// the supported HTTP client patterns. AST-only — no typesInfo required, which
// means receiver-type confirmation is best-effort (name + arg-count heuristics).
//
// The four shapes:
//
//   - http.Verb(...)              — receiver Ident "http", Sel one of Get/Post/PostForm/Head
//   - http.NewRequest{,WithContext}(method, url, ...) — first string-literal arg is method
//   - <recv>.Verb(...)            — receiver value (presumed *http.Client or compatible),
//     Sel one of Get/Post/PostForm/Head; matched leniently
//     to support common wrappers (chi.Client, retryablehttp.Client).
//   - <recv>.Do(req)              — receiver value, Sel "Do"; SKIPPED in V0 because the
//     request object's method/url require flow analysis.
//     Documented limitation.
func classifyHTTPClientCall(call *ast.CallExpr) (method string, urlArg ast.Expr, ok bool) {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return "", nil, false
	}
	name := sel.Sel.Name
	// http.NewRequest / NewRequestWithContext — method is a string-literal arg.
	if id, isPkg := sel.X.(*ast.Ident); isPkg && id.Name == "http" {
		switch name {
		case "NewRequest":
			// http.NewRequest(method, url, body) — 3 args.
			if len(call.Args) < 3 {
				return "", nil, false
			}
			methodStr, ok := stringLiteral(call.Args[0])
			if !ok || methodStr == "" {
				return "", nil, false
			}
			return strings.ToUpper(methodStr), call.Args[1], true
		case "NewRequestWithContext":
			// http.NewRequestWithContext(ctx, method, url, body) — 4 args.
			if len(call.Args) < 4 {
				return "", nil, false
			}
			methodStr, ok := stringLiteral(call.Args[1])
			if !ok || methodStr == "" {
				return "", nil, false
			}
			return strings.ToUpper(methodStr), call.Args[2], true
		case "Get", "Head":
			// http.Get(url) / http.Head(url) — 1 arg.
			if len(call.Args) < 1 {
				return "", nil, false
			}
			return strings.ToUpper(name), call.Args[0], true
		case "Post":
			// http.Post(url, contentType, body) — 3 args.
			if len(call.Args) < 3 {
				return "", nil, false
			}
			return "POST", call.Args[0], true
		case "PostForm":
			// http.PostForm(url, data) — 2 args.
			if len(call.Args) < 2 {
				return "", nil, false
			}
			return "POST", call.Args[0], true
		}
		return "", nil, false
	}
	// Receiver-based: <client>.Verb(...) — same method names.
	// AST-only mode can't confirm the receiver is *http.Client, so we accept
	// any value-receiver call to these names when the URL is the first arg.
	// False positives possible but tractable: user code defining a Get/Post
	// method on a non-HTTP type would emit an http_calls edge. The downstream
	// link pass either matches an Endpoint (suggesting it really was HTTP) or
	// keeps an AMBIGUOUS placeholder (low cost — surfaces as "unknown
	// external API" in viewer).
	switch name {
	case "Get", "Head":
		if len(call.Args) < 1 {
			return "", nil, false
		}
		return strings.ToUpper(name), call.Args[0], true
	case "Post":
		if len(call.Args) < 3 {
			return "", nil, false
		}
		return "POST", call.Args[0], true
	case "PostForm":
		if len(call.Args) < 2 {
			return "", nil, false
		}
		return "POST", call.Args[0], true
	}
	return "", nil, false
}

// extractURLPath strips an absolute URL's scheme + host, returning only the
// path portion. Paths starting with `/` are returned verbatim. Empty / pure-
// scheme inputs return "" so the caller can skip emission.
//
// Examples:
//
//	"/api/users"                  → "/api/users"
//	"https://api.example.com/foo" → "/foo"
//	"http://localhost:8080/x"     → "/x"
//	"api.example.com/foo"         → ""   (no leading slash, no scheme — ambiguous)
//	""                            → ""
func extractURLPath(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "/") {
		return raw
	}
	// scheme://host[:port]/path
	i := strings.Index(raw, "://")
	if i < 0 {
		return ""
	}
	rest := raw[i+3:]
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		// host-only URL with no path — root.
		return "/"
	}
	return rest[slash:]
}

// upsertHTTPClientPlaceholder emits an AMBIGUOUS placeholder Endpoint for an
// http_calls edge target. The placeholder uses Language="external" (sentinel
// for "client-side intent; resolution pending") so it doesn't collide with
// real server-side Endpoint IDs that include "go"/"ts" in their hash input.
//
// The link pass (internal/link/http_match.go) replaces these placeholders
// with real Endpoint IDs when a server-side handler is detected on a matching
// (METHOD, path) — or keeps them as-is when the call targets an external API.
func (v *declVisitor) upsertHTTPClientPlaceholder(method, path string, startPos, endPos token.Pos) string {
	qname := "http:" + method + " " + path
	// Distinct ID space from real Endpoints: use language="external" so the
	// placeholder ID never collides with a real `go`/`ts` Endpoint with the
	// same qname (which would otherwise dedup via graph.Build's by-ID merge).
	// Track via a separate map so we don't dedup against real-Endpoint IDs in
	// the same file.
	if v.httpClientPlaceholderIDs == nil {
		v.httpClientPlaceholderIDs = map[string]string{}
	}
	if id, ok := v.httpClientPlaceholderIDs[qname]; ok {
		return id
	}
	startLn, startBy := v.pos(startPos)
	endLn, endBy := v.pos(endPos)
	id := MakeID(qname, "external", startBy)
	v.httpClientPlaceholderIDs[qname] = id
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: types.NodeEndpoint,
		Name: path, QualifiedName: qname,
		FilePath: v.relPath, StartLine: startLn, EndLine: endLn,
		StartByte: startBy, EndByte: endBy,
		Language: "external", Confidence: types.ConfAmbiguous, SubKind: "http",
	})
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1,
		Confidence: types.ConfAmbiguous,
	})
	return id
}
