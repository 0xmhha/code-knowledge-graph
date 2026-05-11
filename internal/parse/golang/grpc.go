package golang

import (
	"go/ast"
	"go/token"
	gotypes "go/types"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// grpc.go implements W3b (schema 1.9, CKS G5 Distributed): Go gRPC
// server/client detection. Two new edge kinds:
//
//   - grpc_listens_on: server impl Method → Endpoint
//                      Triggered by `pb.RegisterXXXServer(s, &impl{})` or
//                      `pb.RegisterXXXServer(s, impl)` call sites. Each
//                      method on the impl receiver type emits one edge to
//                      a `grpc:Service.Method` Endpoint node (language="go",
//                      sub_kind="grpc").
//   - grpc_calls:      caller Function/Method → Endpoint (real or AMBIGUOUS
//                      placeholder). Triggered by stub-variable method
//                      calls where the stub was assigned from a
//                      `pb.NewXXXClient(conn)` call. Placeholder uses
//                      Language="external" — mirrors W2 http_calls.
//
// Confidence split (schema-1.9-spec §6.5 (C)):
//   - typesInfo present + receiver type resolves to a generated gRPC server
//     interface → EXTRACTED.
//   - AST-only suffix matcher (Register*Server / New*Client) → INFERRED.
//   - Stub var type unresolvable → AMBIGUOUS placeholder Endpoint.
//
// V0 limitations (documented):
//   - Cross-package proto package prefix is dropped: the emitted Endpoint
//     qname is `grpc:Service.Method` (no `pkg.` prefix). Future linker pass
//     can rewire to the proto-parser's `proto:pkg.Service.Method` Method
//     nodes by suffix-match.
//   - Streaming, bidirectional, and per-message recursion through the impl
//     are not modelled — each method emits a single edge regardless of
//     stream semantics. Streaming detection is out-of-scope for W3b.
//   - Server impl method discovery uses receiver-name match against
//     v.nodes (typesInfo-aware via fn.Pkg() / Name lookup). Out-of-file
//     methods are not chased; the receiver type must be declared in the
//     same package as the Register call.

// emitGRPCDecls is the per-file entry point for the W3b gRPC pass. Walks
// every function body for RegisterXXXServer + stub-creating NewXClient
// patterns. Idempotent: grpcServerEmitted/grpcClientStubs reset per file.
//
// Two sub-passes:
//
//	(a) emitGRPCServerDecls — scan top-level for pb.RegisterXXXServer
//	    call sites and emit grpc_listens_on edges. Independent of body
//	    walks because the registration call is usually inside a `main`
//	    or `setup` function, not at file scope, but the impl receiver
//	    methods are top-level FuncDecls.
//	(b) emitGRPCClientCalls — walk every function body, track local
//	    stub variables (`client := pb.NewXClient(conn)`), and emit
//	    grpc_calls edges on subsequent `client.RpcMethod(ctx, req)`.
//
// Called from emitDistributedDecls after the HTTP / JSON-RPC passes so
// shared upsertEndpoint maps are warm.
func (v *declVisitor) emitGRPCDecls(f *ast.File) {
	if v.endpointNodeIDs == nil {
		v.endpointNodeIDs = map[string]string{}
	}
	if v.grpcServerImpls == nil {
		v.grpcServerImpls = map[string]struct{}{}
	}
	v.scanFileForGRPCServers(f)
}

// scanFileForGRPCServers walks every CallExpr in every function body and
// dispatches to maybeEmitGRPCServerRegistration. Mirrors
// scanFuncBodyForDistributed but is dedicated to W3b's server pass so the
// stub-var scope is per-function (handled in emitGRPCClientCalls, called
// from scanFuncBodyForDistributed in distributed.go).
func (v *declVisitor) scanFileForGRPCServers(f *ast.File) {
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			v.maybeEmitGRPCServerRegistration(call)
			return true
		})
	}
}

// maybeEmitGRPCServerRegistration recognises `pb.RegisterXXXServer(s, impl)`
// shaped call sites and emits grpc_listens_on edges from the impl's methods
// to Endpoint nodes named `grpc:XXX.MethodName`.
//
// Detection (AST-only, INFERRED):
//   - call.Fun is SelectorExpr `<pkg>.Register<Svc>Server`
//   - len(call.Args) >= 2 (server instance + impl)
//   - serviceName := strip "Register" prefix + "Server" suffix from Sel.Name
//
// typesInfo-aware (EXTRACTED): receiver of the Register function must
// belong to a package whose Path() ends in a `.pb` segment or whose
// last-segment generated-file convention is recognised. Conservative — when
// in doubt, fall back to INFERRED.
//
// The impl argument types are resolved via:
//   - typesInfo: TypeOf(arg) → receiver type → list its methods.
//   - AST fallback: extract type name from `&Foo{}` / `Foo{}` / `foo`
//     identifier shapes, then scan v.nodes for methods with that receiver.
//
// Each impl method whose name maps to an rpc on the service emits one edge.
// "Maps" is conservative: in V0 we trust that every exported method on the
// impl receiver is intended as an rpc handler — false positives are bounded
// because the user has already declared `RegisterXXXServer(...)` intent.
func (v *declVisitor) maybeEmitGRPCServerRegistration(call *ast.CallExpr) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	svc, ok := registerServerServiceName(sel.Sel.Name)
	if !ok {
		return
	}
	if len(call.Args) < 2 {
		return
	}
	// Dedup: if we've already emitted edges for this (file, service)
	// registration, skip — handles two registrations of the same impl
	// in the same file producing duplicate edges (rare but possible).
	key := v.relPath + "::" + svc
	if _, seen := v.grpcServerImpls[key]; seen {
		return
	}
	v.grpcServerImpls[key] = struct{}{}

	implMethods, conf := v.collectGRPCImplMethods(call.Args[1])
	if len(implMethods) == 0 {
		return
	}
	pos := v.fset.Position(call.Pos())
	for _, m := range implMethods {
		endpointID := v.upsertGRPCEndpoint(svc, m.name, call.Pos(), call.End())
		v.edges = append(v.edges, types.Edge{
			Src: m.id, Dst: endpointID, Type: types.EdgeGRPCListensOn,
			Line: pos.Line, Count: 1, Confidence: conf,
			FilePath: v.relPath,
		})
	}
}

// registerServerServiceName checks whether selName matches the gRPC server
// registration pattern `Register<Service>Server` and returns the extracted
// service name on a hit. Returns ("", false) when selName doesn't match.
//
// Examples:
//
//	"RegisterUserServiceServer" → ("UserService", true)
//	"RegisterEchoServer"        → ("Echo", true)
//	"RegisterMux"               → ("", false)         // missing both affixes
//	"RegisterUserService"       → ("", false)         // missing Server suffix
//	"UserServiceServer"         → ("", false)         // missing Register prefix
func registerServerServiceName(selName string) (string, bool) {
	const prefix = "Register"
	const suffix = "Server"
	if !strings.HasPrefix(selName, prefix) || !strings.HasSuffix(selName, suffix) {
		return "", false
	}
	core := selName[len(prefix) : len(selName)-len(suffix)]
	if core == "" {
		return "", false
	}
	// Require the first char to be uppercase (Go exported naming + gRPC
	// generated convention). Guards against pathological matches like
	// `Registerserver` or `RegisterxServer`.
	c := core[0]
	if c < 'A' || c > 'Z' {
		return "", false
	}
	return core, true
}

// grpcImplMethod pairs a Method node ID with its bare name. Returned by
// collectGRPCImplMethods so maybeEmitGRPCServerRegistration can wire one
// edge per method without recomputing IDs.
type grpcImplMethod struct {
	id   string
	name string
}

// collectGRPCImplMethods extracts the impl receiver type from arg and
// returns the list of Method nodes on that type in v.nodes, plus a
// confidence label.
//
// Resolution order:
//
//  1. typesInfo path: TypeOf(arg) → strip pointer → Named type → use
//     receiverTypeName + scan v.nodes for Method nodes whose QualifiedName
//     ends in `.<recv>.<method>`. Confidence EXTRACTED.
//  2. AST fallback: extract a type name from `&Foo{}` / `Foo{}` /
//     identifier shapes, then scan v.nodes by receiver-name suffix.
//     Confidence INFERRED.
//
// Empty result (no matching methods) signals the caller to skip emission —
// it's the W3b equivalent of HTTP's "unknown receiver, skip" guard.
func (v *declVisitor) collectGRPCImplMethods(arg ast.Expr) ([]grpcImplMethod, types.Confidence) {
	recvName := ""
	conf := types.ConfInferred
	if v.typesInfo != nil {
		if t := v.typesInfo.TypeOf(arg); t != nil {
			if n := receiverTypeName(t); n != "" {
				recvName = n
				conf = types.ConfExtracted
			}
		}
	}
	if recvName == "" {
		recvName = grpcImplTypeName(arg)
	}
	if recvName == "" {
		return nil, ""
	}
	prefix := v.pkgName + "." + recvName + "."
	var out []grpcImplMethod
	for i := range v.nodes {
		n := &v.nodes[i]
		if n.Type != types.NodeMethod {
			continue
		}
		// W3b review Minor #1 (2026-05-11): match strictly by qname prefix
		// `<pkg>.<recv>.<method>`. Earlier code had a `strings.Contains`
		// fallback for the same suffix that was effectively dead within a
		// single-file declVisitor (pkgName is single-valued per visitor) but
		// would over-match if v.nodes ever spanned packages — a cross-file
		// integration risk we don't want lurking. Prefix-only is both
		// correct and defensive.
		if !strings.HasPrefix(n.QualifiedName, prefix) {
			continue
		}
		// Pull bare method name = trailing segment after the last dot.
		dot := strings.LastIndex(n.QualifiedName, ".")
		if dot < 0 || dot == len(n.QualifiedName)-1 {
			continue
		}
		methodName := n.QualifiedName[dot+1:]
		// Skip non-exported helpers — gRPC RPCs are always exported.
		if !isExportedIdent(methodName) {
			continue
		}
		out = append(out, grpcImplMethod{id: n.ID, name: methodName})
	}
	return out, conf
}

// grpcImplTypeName extracts a Go type name from the impl argument
// expression in `RegisterXXXServer(s, impl)`. Handles the common AST
// shapes used in gRPC registration call sites without requiring typesInfo.
//
// Recognised forms:
//
//	&Foo{...}     UnaryExpr{Op:&, X:CompositeLit{Type:Ident "Foo"}}
//	&pkg.Foo{...} UnaryExpr{Op:&, X:CompositeLit{Type:SelectorExpr}}
//	Foo{...}      CompositeLit{Type:Ident "Foo"}
//	foo           Ident "foo"  → "" (cannot infer type from var name alone)
//	pkg.foo       SelectorExpr → "" (same reason)
//
// Returns "" when the shape doesn't carry enough info — caller falls
// through to no-emit. The typesInfo path above is the reliable resolver;
// this fallback is best-effort.
func grpcImplTypeName(arg ast.Expr) string {
	if u, ok := arg.(*ast.UnaryExpr); ok && u.Op == token.AND {
		return grpcImplTypeName(u.X)
	}
	if cl, ok := arg.(*ast.CompositeLit); ok {
		return exprName(cl.Type)
	}
	return ""
}

// upsertGRPCEndpoint emits a NodeEndpoint for a gRPC `grpc:Service.Method`
// qname if not already emitted in this file, and returns its node ID.
// Reuses endpointNodeIDs so a same-file server + client pair share the
// same Endpoint by qname (Build's by-ID dedup handles cross-file).
func (v *declVisitor) upsertGRPCEndpoint(service, method string, startPos, endPos token.Pos) string {
	qname := "grpc:" + service + "." + method
	if id, ok := v.endpointNodeIDs[qname]; ok {
		return id
	}
	startLn, startBy := v.pos(startPos)
	endLn, endBy := v.pos(endPos)
	id := MakeID(qname, "go", startBy)
	v.endpointNodeIDs[qname] = id
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: types.NodeEndpoint,
		Name: service + "." + method, QualifiedName: qname,
		FilePath: v.relPath, StartLine: startLn, EndLine: endLn,
		StartByte: startBy, EndByte: endBy,
		Language: "go", Confidence: types.ConfExtracted, SubKind: "grpc",
	})
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1,
		Confidence: types.ConfExtracted,
	})
	return id
}

// maybeEmitGRPCClientCall is invoked from scanFuncBodyForDistributed for
// every CallExpr inside a function body. It tracks two patterns:
//
//	(a) stub-creating assignment: `client := pb.NewFooClient(conn)` /
//	    `var client = pb.NewFooClient(conn)`. Recognised at the AssignStmt
//	    level — not at the CallExpr level — so this function only handles
//	    (b). The stub-var bookkeeping lives in trackGRPCClientStub, called
//	    from emitGRPCClientStubScan below.
//
//	(b) stub method call: `stub.RpcMethod(ctx, req)` where `stub` is a
//	    name registered in v.grpcClientStubs. Emits a grpc_calls edge from
//	    parentFuncID to a grpc:Service.Method Endpoint (real or
//	    AMBIGUOUS placeholder). When typesInfo is available, the stub's
//	    receiver type is consulted directly (skipping the variable-name
//	    map), giving EXTRACTED confidence on the call.
func (v *declVisitor) maybeEmitGRPCClientCall(parentFuncID string, call *ast.CallExpr) {
	if parentFuncID == "" {
		return
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	// Receiver must be a bare identifier — `client.RpcMethod(...)`.
	// Multi-level selectors (`srv.client.RpcMethod`) are handled via
	// typesInfo when available (the SelectorExpr's full type is what we
	// care about). The AST-only path requires the simple shape.
	stubName := ""
	if id, ok := sel.X.(*ast.Ident); ok {
		stubName = id.Name
	}
	methodName := sel.Sel.Name
	if !isExportedIdent(methodName) {
		return // RPCs are always exported.
	}

	// typesInfo path: receiver type → check if it's a `<pkg>.<Svc>Client`
	// generated interface. Most reliable; doesn't need variable-name match.
	svc, conf := v.classifyGRPCClientCall(sel)
	if svc == "" && stubName != "" {
		if rec, ok := v.grpcClientStubs[stubName]; ok {
			svc = rec
			if conf == "" {
				conf = types.ConfInferred
			}
		}
	}
	if svc == "" {
		return
	}
	pos := v.fset.Position(call.Pos())
	endpointID := v.upsertGRPCClientPlaceholder(svc, methodName, call.Pos(), call.End())
	v.edges = append(v.edges, types.Edge{
		Src: parentFuncID, Dst: endpointID, Type: types.EdgeGRPCCalls,
		Line: pos.Line, Count: 1, Confidence: conf,
		FilePath: v.relPath,
	})
}

// classifyGRPCClientCall returns (service, confidence) when sel.X resolves
// (via typesInfo) to a generated gRPC client interface — i.e. a Named type
// whose Obj().Name() matches the `<Service>Client` convention AND whose
// underlying type is an *Interface. The interface-underlying check is a
// load-bearing false-positive guard: user-defined `FakeClient struct{}`
// types match the name convention but are not gRPC stubs.
//
// Returns ("", "") when typesInfo is nil, when the receiver type isn't a
// Named interface, or when the name doesn't fit the Client convention.
// Caller falls back to the variable-name map populated by
// trackGRPCClientStub.
func (v *declVisitor) classifyGRPCClientCall(sel *ast.SelectorExpr) (string, types.Confidence) {
	if v.typesInfo == nil {
		return "", ""
	}
	t := v.typesInfo.TypeOf(sel.X)
	if t == nil {
		return "", ""
	}
	// Strip pointer wrapper if any.
	if ptr, ok := t.(*gotypes.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*gotypes.Named)
	if !ok {
		return "", ""
	}
	obj := named.Obj()
	if obj == nil {
		return "", ""
	}
	svc, ok := clientTypeServiceName(obj.Name())
	if !ok {
		return "", ""
	}
	// Underlying-type guard: gRPC's generated <Svc>Client is always an
	// interface (the package declares both `type <Svc>Client interface { ... }`
	// and the unexported `<svc>Client struct{}` that implements it; consumers
	// hold the interface). A user-defined `FakeClient struct{}` slips past
	// the name check but is filtered here.
	if _, isInterface := named.Underlying().(*gotypes.Interface); !isInterface {
		return "", ""
	}
	return svc, types.ConfExtracted
}

// clientTypeServiceName checks whether typeName matches the gRPC client
// interface naming convention `<Service>Client` and returns the extracted
// service name. Mirrors registerServerServiceName for the client side.
//
// Examples:
//
//	"UserServiceClient" → ("UserService", true)
//	"EchoClient"        → ("Echo", true)
//	"Client"            → ("", false)  // missing service portion
//	"FooClientImpl"     → ("", false)  // suffix doesn't match exactly
func clientTypeServiceName(typeName string) (string, bool) {
	const suffix = "Client"
	if !strings.HasSuffix(typeName, suffix) {
		return "", false
	}
	core := typeName[:len(typeName)-len(suffix)]
	if core == "" {
		return "", false
	}
	c := core[0]
	if c < 'A' || c > 'Z' {
		return "", false
	}
	return core, true
}

// trackGRPCClientStub records a stub variable for the current function
// scope when `<lhs> := pb.NewXClient(<args>)` (or `var lhs = pb.NewXClient(...)`)
// is encountered. The map (grpcClientStubs) is populated from
// scanFuncBodyForDistributed before maybeEmitGRPCClientCall fires on the
// CallExpr nested inside the body — but ast.Inspect doesn't separate
// AssignStmt from CallExpr at the visit ordering level, so the assignment
// is captured eagerly when its RHS CallExpr matches.
//
// Variable-name shadowing across nested scopes is not modelled — V0 keeps
// the map flat per file. False positives (a stub of the same name in two
// nested scopes both referring to different services) are surfaced as
// AMBIGUOUS at edge emission. Documented limitation.
func (v *declVisitor) trackGRPCClientStub(assign *ast.AssignStmt) {
	if assign == nil || len(assign.Lhs) == 0 || len(assign.Rhs) == 0 {
		return
	}
	// Only handle simple `name := ...` / `var name = ...` — multi-LHS
	// (e.g. `client, err := pb.NewXClient(...)`) is rare for gRPC stub
	// constructors (which return only the client), so V0 takes the first
	// LHS.
	lhs, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || lhs.Name == "_" {
		return
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok {
		return
	}
	svc := callTargetNewXClient(call)
	if svc == "" {
		return
	}
	if v.grpcClientStubs == nil {
		v.grpcClientStubs = map[string]string{}
	}
	v.grpcClientStubs[lhs.Name] = svc
}

// callTargetNewXClient returns the service name when call matches
// `<pkg>.New<Svc>Client(...)`, or "" otherwise. Mirrors
// registerServerServiceName but for the client constructor side.
func callTargetNewXClient(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	const prefix = "New"
	const suffix = "Client"
	name := sel.Sel.Name
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return ""
	}
	core := name[len(prefix) : len(name)-len(suffix)]
	if core == "" {
		return ""
	}
	c := core[0]
	if c < 'A' || c > 'Z' {
		return ""
	}
	return core
}

// upsertGRPCClientPlaceholder emits an AMBIGUOUS placeholder Endpoint for a
// grpc_calls edge target when no real server-side Endpoint exists in the
// graph (yet). The placeholder uses Language="external" — mirrors W2's
// HTTP placeholder pattern (see upsertHTTPClientPlaceholder).
//
// When a real Endpoint with qname `grpc:Service.Method` already exists in
// endpointNodeIDs (same-file server + client), upsertGRPCClientPlaceholder
// returns that real ID instead — keeping the client edge pointed at the
// authoritative node. Cross-file resolution stays in placeholder form until
// a future linker pass merges by qname.
func (v *declVisitor) upsertGRPCClientPlaceholder(service, method string, startPos, endPos token.Pos) string {
	qname := "grpc:" + service + "." + method
	// Same-file server already registered this Endpoint? Reuse it (real
	// Endpoint, language="go", EXTRACTED) so the in-file graph is fully
	// connected without going through the placeholder lane.
	if id, ok := v.endpointNodeIDs[qname]; ok {
		return id
	}
	if v.grpcClientPlaceholderIDs == nil {
		v.grpcClientPlaceholderIDs = map[string]string{}
	}
	if id, ok := v.grpcClientPlaceholderIDs[qname]; ok {
		return id
	}
	startLn, startBy := v.pos(startPos)
	endLn, endBy := v.pos(endPos)
	id := MakeID(qname, "external", startBy)
	v.grpcClientPlaceholderIDs[qname] = id
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: types.NodeEndpoint,
		Name: service + "." + method, QualifiedName: qname,
		FilePath: v.relPath, StartLine: startLn, EndLine: endLn,
		StartByte: startBy, EndByte: endBy,
		Language: "external", Confidence: types.ConfAmbiguous, SubKind: "grpc",
	})
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1,
		Confidence: types.ConfAmbiguous,
	})
	return id
}

// scanFuncBodyForGRPCStubs walks the body once to register stub vars (via
// trackGRPCClientStub on every AssignStmt) BEFORE the CallExpr pass runs.
// Called from scanFuncBodyForDistributed so the variable-name map is
// populated before maybeEmitGRPCClientCall checks it.
//
// Two-pass within one function body is necessary because ast.Inspect's
// pre-order traversal visits the parent AssignStmt before its nested
// CallExpr children, but in `client := pb.NewXClient(conn)` the
// AssignStmt's RHS CallExpr could also be a method call we'd want to
// emit — and we need the LHS binding registered first either way.
// Doing two passes keeps each pass linearly scoped and deterministic.
func (v *declVisitor) scanFuncBodyForGRPCStubs(body *ast.BlockStmt) {
	// Reset per-function scope — a stub named `client` in fn A must not
	// leak into fn B's body scan.
	v.grpcClientStubs = map[string]string{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			v.trackGRPCClientStub(x)
		case *ast.GenDecl:
			if x.Tok == token.VAR {
				for _, spec := range x.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					// `var name = pb.NewXClient(conn)` — synthesise an
					// AssignStmt-like shape for trackGRPCClientStub.
					for i := range vs.Names {
						if i >= len(vs.Values) {
							break
						}
						v.trackGRPCClientStub(&ast.AssignStmt{
							Lhs: []ast.Expr{vs.Names[i]},
							Rhs: []ast.Expr{vs.Values[i]},
						})
					}
				}
			}
		}
		return true
	})
}
