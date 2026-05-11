// Package typescript — grpc_client.go implements W3c of schema 1.9
// (CKS G5 Distributed): TypeScript gRPC-web / Connect-web client
// detection. Companion to internal/parse/golang/grpc.go (W3b, Go server +
// client) — the two together let graph traversal answer
// "TS Func → grpc_calls → Endpoint ← grpc_listens_on ← Go Method" in a
// monorepo.
//
// Detection patterns (V0 — AST-only, all INFERRED per §6.5 (c)):
//
//  1. Generated client class instantiation + method call
//
//     const client = new UserServiceClient(host)
//     client.getUser(req, callback)
//
//     The local stub variable is tracked; each subsequent
//     `<stub>.method(...)` emits one grpc_calls edge to a placeholder
//     Endpoint with qname `grpc:UserService.GetUser`.
//
//  2. grpc-web unary descriptor call
//
//     grpc.unary(EchoService.Echo, { request, host, onEnd })
//
//     The first argument is a member_expression `Service.Method`;
//     emits one grpc_calls edge.
//
//  3. Connect-web / Connect-ES promise client
//
//     const client = createPromiseClient(GreetService, transport)
//     const client = createClient(GreetService, transport)
//     await client.sayHello(req)
//
//     The factory call's first argument is the service identifier; the
//     return value is the stub. Subsequent `<stub>.method(...)` emits
//     one grpc_calls edge per call site.
//
// Each detection emits an AMBIGUOUS placeholder Endpoint (Language
// "external") and an `grpc_calls` edge (INFERRED) from the enclosing
// TS Function to the placeholder. The placeholder lives in a distinct
// ID space from the real server-side Endpoints emitted by Go W3b's
// `pb.RegisterXXXServer` pass, so cross-language resolution stays in
// placeholder form until a future linker pass merges by qname suffix
// (mirroring the §6.5 V0 limitation already documented for Go).
//
// Per §6.5 (c) — typesInfo is unavailable in tree-sitter parses, so
// every TS gRPC call edge is INFERRED.
//
// Pattern A (`new <Svc>Client(host)`) is gated on a file-scoped
// import-path heuristic: at least one import from `grpc-web`,
// `@improbable-eng/grpc-web`, `@bufbuild/connect-web`, `@connectrpc/connect`,
// `nice-grpc`, or any path matching `*grpc*` must be present, otherwise
// the suffix `*Client` is far too common (RedisClient / PrismaClient /
// ApolloClient / HttpClient / S3Client / KafkaClient / MongoClient /
// ElasticsearchClient / ApiClient) to be treated as a gRPC stub. This
// was raised by W3c code-review as Important #1 (2026-05-11) — the prior
// implementation matched `*Client` unconditionally and would have
// produced AMBIGUOUS placeholders for every non-gRPC client in a real
// monorepo.
//
// Patterns B (`createPromiseClient` / `createClient`) and C
// (`grpc.unary(Service.Method, ...)`) carry distinctive function names
// and are NOT gated — their signal-to-noise ratio is high enough that
// false positives in real code are vanishingly rare.
//
// Out of scope (deferred):
//   - nice-grpc, twirp, ts-proto generated clients — same shape as
//     pattern 1; can fold in incrementally.
//   - Streaming methods (server-streaming, client-streaming, bidi) —
//     emitted identically to unary in V0; stream semantics are not
//     modelled.
//   - Method-name camelCase ↔ proto PascalCase mismatch — V0 emits the
//     observed JS method name (camelCase). Linker pass can normalise
//     against proto Method nodes later.
package typescript

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// runGRPCClients is the W3c entry point. Called from declVisitor.visit()
// after runHTTPClients(). Two passes:
//
//	(1) collectGRPCClientStubs — walk the tree once to register every
//	    `const client = new ServiceClient(...)` /
//	    `const client = createPromiseClient(Service, ...)` /
//	    `const client = createClient(Service, ...)` binding into the
//	    function-scoped grpcClientStubsTS map.
//	(2) walkForGRPCClientCalls — walk again, emitting grpc_calls edges
//	    for (a) `<stub>.method(...)` where stub is in the map, and
//	    (b) `grpc.unary(Service.Method, ...)` descriptor-shaped calls.
//
// The two-pass shape mirrors the Go W3b pattern (scanFuncBodyForGRPCStubs
// → maybeEmitGRPCClientCall) so per-function stub-name scoping is
// deterministic regardless of AST traversal order.
func (v *declVisitor) runGRPCClients() {
	intervals := collectFnIntervalsFromTree(v)
	v.grpcClientStubsTS = map[string]string{}
	// Pattern A gating signal — file-scoped scan for gRPC library imports.
	// See module header for the rationale (W3c review Important #1, 2026-05-11).
	v.tsGRPCImportPresent = fileHasGRPCImport(v.root, v.src)
	v.collectGRPCClientStubs(v.root, intervals)
	v.walkForGRPCClientCalls(v.root, intervals)
}

// fileHasGRPCImport reports whether the parse tree contains at least one
// import_statement whose source string matches a known gRPC library path.
// The set of recognised paths is small and dominant; nice-grpc and other
// vendor variants fall back to the generic `*grpc*` substring rule (so
// e.g. `nice-grpc-common`, `@grpc/grpc-js` also activate Pattern A).
//
// The check is intentionally string-based rather than parser-aware: every
// TS module-level import either spells out the dependency literally or
// re-exports through a barrel; the latter is rare enough in gRPC client
// code that we don't bother chasing. False negatives here surface as
// missing grpc_calls edges — auditable via the same path-trace UX as any
// other detector gap.
func fileHasGRPCImport(root *sitter.Node, src []byte) bool {
	if root == nil {
		return false
	}
	// Known dominant gRPC client libraries — match by exact equality on
	// the import source string.
	var exact = []string{
		"grpc-web",
		"@improbable-eng/grpc-web",
		"@bufbuild/connect-web",
		"@connectrpc/connect",
		"@connectrpc/connect-web",
	}
	return walkForGRPCImport(root, src, exact)
}

func walkForGRPCImport(n *sitter.Node, src []byte, exact []string) bool {
	if n == nil {
		return false
	}
	if n.Kind() == "import_statement" {
		// Source string lives in a child of kind "string"; payload is the
		// quoted literal (e.g. `"grpc-web"`). We strip surrounding quotes
		// and compare against the exact list + the generic `grpc` substring.
		count := int(n.ChildCount())
		for i := 0; i < count; i++ {
			child := n.Child(uint(i))
			if child == nil || child.Kind() != "string" {
				continue
			}
			raw := child.Utf8Text(src)
			raw = strings.Trim(raw, "\"'`")
			for _, e := range exact {
				if raw == e {
					return true
				}
			}
			// Generic fallback: any import whose path mentions "grpc"
			// (e.g. nice-grpc-common, @grpc/grpc-js, my-org/grpc-internal).
			if strings.Contains(raw, "grpc") {
				return true
			}
		}
	}
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		if walkForGRPCImport(n.Child(uint(i)), src, exact) {
			return true
		}
	}
	return false
}

// collectGRPCClientStubs walks the parse tree for variable_declarator nodes
// whose RHS is a `new <Svc>Client(...)` / `createPromiseClient(Svc, ...)` /
// `createClient(Svc, ...)` call, and records (stubName@fnID → serviceName)
// in v.grpcClientStubsTS so the second pass can resolve method calls.
//
// Per-function scoping: the map key embeds the enclosing-function ID, so a
// stub named `client` in function A and a same-named stub in function B —
// each bound to a different service — don't collide. Module-scope stubs
// use an empty fnID prefix; in practice they don't anchor edges anyway
// (module-scope calls drop in maybeEmitGRPCClientCallTS).
//
// Nested-scope shadowing (a stub `client` inside an `if` block within fn
// A re-bound to a different service) is not modelled — V0 retains the
// outermost binding inside one function. Documented limitation.
func (v *declVisitor) collectGRPCClientStubs(n *sitter.Node, intervals []fnInterval) {
	if n == nil {
		return
	}
	if n.Kind() == "variable_declarator" {
		v.maybeTrackGRPCClientStub(n, intervals)
	}
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		v.collectGRPCClientStubs(n.Child(uint(i)), intervals)
	}
}

// maybeTrackGRPCClientStub inspects one variable_declarator and, when the
// RHS is a recognised gRPC client constructor, records the binding. The
// declarator shape is:
//
//	variable_declarator
//	├── name: identifier "client"
//	└── value: new_expression / call_expression
//
// Recognised RHS shapes:
//
//	new UserServiceClient(host)              → svc="UserService"
//	new pkg.UserServiceClient(host)          → svc="UserService"
//	createPromiseClient(GreetService, ...)   → svc="GreetService"
//	createClient(GreetService, ...)          → svc="GreetService"
//
// Returns silently when nothing matches.
func (v *declVisitor) maybeTrackGRPCClientStub(decl *sitter.Node, intervals []fnInterval) {
	nameNode := decl.ChildByFieldName("name")
	valueNode := decl.ChildByFieldName("value")
	if nameNode == nil || valueNode == nil {
		return
	}
	if nameNode.Kind() != "identifier" {
		return
	}
	stubName := nameNode.Utf8Text(v.src)
	if stubName == "" || stubName == "_" {
		return
	}
	svc := extractGRPCServiceFromConstructor(valueNode, v.src)
	if svc == "" {
		return
	}
	// Pattern A gating — `new <Svc>Client(host)` is the noisiest signal
	// (RedisClient / PrismaClient / ApolloClient / HttpClient / S3Client /
	// MongoClient / ApiClient all match the `*Client` suffix). Require
	// a gRPC library import in the file before honouring the binding.
	// Pattern B (createPromiseClient / createClient) uses an explicit
	// factory name and is never gated. See module header + W3c review
	// Important #1 (2026-05-11).
	if valueNode.Kind() == "new_expression" && !v.tsGRPCImportPresent {
		return
	}
	fnID, _ := findEnclosingFn(intervals, int(decl.StartByte()))
	key := grpcStubKey(fnID, stubName)
	// First binding wins within a function scope — see
	// collectGRPCClientStubs for the V0 nested-shadowing limitation.
	if _, exists := v.grpcClientStubsTS[key]; exists {
		return
	}
	v.grpcClientStubsTS[key] = svc
}

// grpcStubKey returns the (fnID, stubName)-composite map key used by
// grpcClientStubsTS. Stub variable names are unique within a function
// scope, so embedding the enclosing-function ID into the key is enough
// to keep cross-function bindings separate without a real scope tree.
func grpcStubKey(fnID, stubName string) string {
	return fnID + "::" + stubName
}

// extractGRPCServiceFromConstructor handles both the `new XxxClient(host)`
// and `createPromiseClient(Service, transport)` / `createClient(Service,
// transport)` shapes. Returns "" when value isn't a recognised
// constructor call.
func extractGRPCServiceFromConstructor(value *sitter.Node, src []byte) string {
	switch value.Kind() {
	case "new_expression":
		return grpcServiceFromNewExpression(value, src)
	case "call_expression":
		return grpcServiceFromConnectFactory(value, src)
	}
	return ""
}

// grpcServiceFromNewExpression handles `new XxxClient(...)` /
// `new pkg.XxxClient(...)`. The constructor identifier is read via the
// "constructor" field; we accept either a bare identifier or a member
// expression and rely on the trailing-segment Client-suffix convention.
func grpcServiceFromNewExpression(newExpr *sitter.Node, src []byte) string {
	ctor := newExpr.ChildByFieldName("constructor")
	if ctor == nil {
		return ""
	}
	name := ""
	switch ctor.Kind() {
	case "identifier":
		name = ctor.Utf8Text(src)
	case "member_expression":
		prop := ctor.ChildByFieldName("property")
		if prop != nil {
			name = prop.Utf8Text(src)
		}
	}
	svc, ok := tsClientTypeServiceName(name)
	if !ok {
		return ""
	}
	return svc
}

// grpcServiceFromConnectFactory handles `createPromiseClient(Svc, ...)` /
// `createClient(Svc, ...)`. The factory name must be one of the known
// Connect-web / Connect-ES entry points, and the first argument must be
// an identifier (the service descriptor). Member-expression service
// idents (e.g. `pkg.GreetService`) are unwrapped to the trailing segment.
func grpcServiceFromConnectFactory(call *sitter.Node, src []byte) string {
	fn := call.ChildByFieldName("function")
	args := call.ChildByFieldName("arguments")
	if fn == nil || args == nil {
		return ""
	}
	if fn.Kind() != "identifier" {
		return ""
	}
	if !isConnectFactoryName(fn.Utf8Text(src)) {
		return ""
	}
	argList := namedArgs(args)
	if len(argList) == 0 {
		return ""
	}
	first := argList[0]
	switch first.Kind() {
	case "identifier":
		return first.Utf8Text(src)
	case "member_expression":
		prop := first.ChildByFieldName("property")
		if prop != nil {
			return prop.Utf8Text(src)
		}
	}
	return ""
}

// isConnectFactoryName checks whether name is a known Connect-web factory
// entry point. Both `@bufbuild/connect-web` (createPromiseClient) and
// `@connectrpc/connect` (createClient) ship with these exact names; we
// match by symbol rather than import path because TS users often re-export
// through barrel modules.
func isConnectFactoryName(name string) bool {
	switch name {
	case "createPromiseClient", "createClient", "createCallbackClient":
		return true
	}
	return false
}

// tsClientTypeServiceName mirrors the Go-side clientTypeServiceName but
// operates on tree-sitter-extracted identifier text. Returns
// ("UserService", true) for "UserServiceClient", ("Echo", true) for
// "EchoClient". "Client" alone, lower-case prefix, or non-Client suffix
// returns ("", false).
func tsClientTypeServiceName(typeName string) (string, bool) {
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

// walkForGRPCClientCalls recursively scans for call_expression nodes and
// dispatches to maybeEmitGRPCClientCallTS. Hand-rolled walk for the same
// reason as walkForHTTPClientCalls — the "enclosing-fn ID" predicate
// depends on dynamic state.
func (v *declVisitor) walkForGRPCClientCalls(n *sitter.Node, intervals []fnInterval) {
	if n == nil {
		return
	}
	if n.Kind() == "call_expression" {
		v.maybeEmitGRPCClientCallTS(n, intervals)
	}
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		v.walkForGRPCClientCalls(n.Child(uint(i)), intervals)
	}
}

// maybeEmitGRPCClientCallTS classifies one call_expression and emits an
// `grpc_calls` edge when it matches a supported client pattern. The edge
// Src is the smallest-enclosing Function/Method ID; module-scope calls
// are dropped (same policy as W2 maybeEmitHTTPClientCall).
//
// Two patterns:
//
//  1. `<stub>.method(...)` where stub ∈ grpcClientStubsTS — service name
//     comes from the map, method name from the SelectorExpr's property.
//  2. `grpc.unary(Service.Method, ...)` — service + method both come
//     from the first argument's SelectorExpr.
func (v *declVisitor) maybeEmitGRPCClientCallTS(call *sitter.Node, intervals []fnInterval) {
	startByte := int(call.StartByte())
	parentID, hasParent := findEnclosingFn(intervals, startByte)
	svc, method, ok := v.classifyTSGRPCClientCall(call, parentID)
	if !ok {
		return
	}
	if !hasParent {
		// Module-scope client calls — graph has nowhere to anchor the
		// edge. Drop in V0; mirrors the W2 http_calls policy.
		return
	}
	endpointID := v.upsertGRPCClientPlaceholderTS(svc, method, call)
	startLine := int(call.StartPosition().Row) + 1
	v.edges = append(v.edges, types.Edge{
		Src: parentID, Dst: endpointID, Type: types.EdgeGRPCCalls,
		Line: startLine, Count: 1, Confidence: types.ConfInferred,
		FilePath: v.rel,
	})
}

// classifyTSGRPCClientCall returns (service, method, true) when the call
// matches one of the W3c patterns. Returns ("", "", false) on miss.
// parentFnID scopes the stub-name lookup; module-scope calls (parentFnID
// "") can still match Pattern 2 (grpc.unary) but never Pattern 1 (stub
// var) because stubs are recorded under their enclosing function ID.
func (v *declVisitor) classifyTSGRPCClientCall(call *sitter.Node, parentFnID string) (string, string, bool) {
	fn := call.ChildByFieldName("function")
	if fn == nil {
		return "", "", false
	}
	if fn.Kind() != "member_expression" {
		return "", "", false
	}
	obj := fn.ChildByFieldName("object")
	prop := fn.ChildByFieldName("property")
	if obj == nil || prop == nil {
		return "", "", false
	}
	methodName := prop.Utf8Text(v.src)
	if methodName == "" {
		return "", "", false
	}

	// Pattern 2: grpc.unary(Service.Method, ...). The receiver is the
	// identifier `grpc` and the method is `unary` / `invoke` /
	// `serverStreaming` / `bidiStreaming`. The first argument carries
	// Service + Method as a MemberExpression.
	if obj.Kind() == "identifier" {
		objName := obj.Utf8Text(v.src)
		if objName == "grpc" && isGRPCWebOperation(methodName) {
			args := call.ChildByFieldName("arguments")
			if args == nil {
				return "", "", false
			}
			argList := namedArgs(args)
			if len(argList) == 0 {
				return "", "", false
			}
			svc, m, ok := serviceMethodFromDescriptor(argList[0], v.src)
			if !ok {
				return "", "", false
			}
			return svc, m, true
		}
	}

	// Pattern 1: <stub>.method(...) where stub ∈ grpcClientStubsTS.
	// Scoped by enclosing function ID — `client` in fn A and `client`
	// in fn B bound to different services won't collide.
	if obj.Kind() == "identifier" {
		stubName := obj.Utf8Text(v.src)
		key := grpcStubKey(parentFnID, stubName)
		if svc, ok := v.grpcClientStubsTS[key]; ok {
			return svc, methodName, true
		}
	}
	return "", "", false
}

// isGRPCWebOperation returns true for the grpc-web client entry points
// that take a `Service.Method` descriptor as their first argument.
// `unary` is the dominant V0 case; the streaming variants share shape.
func isGRPCWebOperation(name string) bool {
	switch name {
	case "unary", "invoke", "serverStreaming", "bidiStreaming", "client":
		return true
	}
	return false
}

// serviceMethodFromDescriptor extracts (Service, Method) from a
// `Service.Method` member-expression argument. Returns ("", "", false)
// when the argument isn't a member expression.
//
// Handles both:
//
//	Service.Method                 → ("Service", "Method", true)
//	pkg.Service.Method (nested)    → ("Service", "Method", true)
//	                                 (outer object is itself a member_expression)
func serviceMethodFromDescriptor(arg *sitter.Node, src []byte) (string, string, bool) {
	if arg == nil || arg.Kind() != "member_expression" {
		return "", "", false
	}
	obj := arg.ChildByFieldName("object")
	prop := arg.ChildByFieldName("property")
	if obj == nil || prop == nil {
		return "", "", false
	}
	method := prop.Utf8Text(src)
	if method == "" {
		return "", "", false
	}
	// Outer object may itself be a member expression for nested
	// namespaces (`pkg.Service.Method`). Take the trailing segment as
	// the service name.
	svc := ""
	switch obj.Kind() {
	case "identifier":
		svc = obj.Utf8Text(src)
	case "member_expression":
		innerProp := obj.ChildByFieldName("property")
		if innerProp != nil {
			svc = innerProp.Utf8Text(src)
		}
	}
	if svc == "" {
		return "", "", false
	}
	return svc, method, true
}

// upsertGRPCClientPlaceholderTS emits one AMBIGUOUS placeholder Endpoint
// per (service, method) tuple inside this file. Distinct ID space from
// real server-side Endpoints (Language="external"). Same shape as the Go
// W3b upsertGRPCClientPlaceholder so a future cross-language linker pass
// can reuse the same qname suffix matcher.
func (v *declVisitor) upsertGRPCClientPlaceholderTS(service, method string, call *sitter.Node) string {
	if v.grpcClientPlaceholderIDsTS == nil {
		v.grpcClientPlaceholderIDsTS = map[string]string{}
	}
	qname := "grpc:" + service + "." + method
	if id, ok := v.grpcClientPlaceholderIDsTS[qname]; ok {
		return id
	}
	startByte := int(call.StartByte())
	endByte := int(call.EndByte())
	startLine := int(call.StartPosition().Row) + 1
	endLine := int(call.EndPosition().Row) + 1
	id := makeID(qname, "external", startByte)
	v.grpcClientPlaceholderIDsTS[qname] = id
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: types.NodeEndpoint,
		Name: service + "." + method, QualifiedName: qname,
		FilePath: v.rel, StartLine: startLine, EndLine: endLine,
		StartByte: startByte, EndByte: endByte,
		Language: "external", Confidence: types.ConfAmbiguous, SubKind: "grpc",
	})
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1,
		Confidence: types.ConfAmbiguous,
	})
	return id
}
