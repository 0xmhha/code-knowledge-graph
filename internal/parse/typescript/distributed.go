// Package typescript — distributed.go implements W1 of schema 1.9 (CKS G5
// Distributed cross-language interop expansion): TypeScript HTTP server
// endpoint detection. Mirrors the Go parser's distributed.go semantics so
// graph traversal can answer cross-language queries
//
//	TS Function ← listens_on → Endpoint ← listens_on ← Go Method
//
// using the SAME Endpoint qname format (`http:METHOD /route`) and the SAME
// edge type (`listens_on`). §6.2 of docs/design/schema-1.9-spec.md elected
// option (B) — reuse `listens_on`, distinguish languages via the Endpoint
// node's `language` field (here always "ts").
//
// V0 detection patterns (string-literal routes only — variables / concat
// are flagged INFERRED with a "<computed>" sentinel):
//
//  1. Express / Koa:    `app.get('/path', handler)`,
//     `router.post('/path', ...)`, etc.
//  2. Fastify:          `fastify.get('/path', ...)`,
//     `fastify.route({ method, url, handler })`
//  3. Hono:             `app.get('/path', c => ...)` (fluent API)
//  4. Next.js App Router: file path `app/api/.../route.ts` with
//     `export async function GET/POST/PUT/DELETE/PATCH/HEAD/OPTIONS`.
//
// Out of scope (deferred):
//   - Pages Router (`pages/api/*.ts` default export).
//   - Computed routes beyond INFERRED placeholder (no const-fold).
//   - Per-framework guard: receiver-type confirmation requires a TS LSP
//     server which CKG doesn't embed. Detection is name-based ("call to
//     a method whose name is an HTTP verb") — false positives possible on
//     unrelated APIs that happen to use the same verb names.
package typescript

import (
	"path/filepath"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// httpVerbs is the set of HTTP method names treated as route-registration
// selectors on Express/Koa/Fastify/Hono-style fluent APIs. Lowercase only —
// the framework conventions all use lowercase method names. The detector
// uppercases for the Endpoint qname.
var httpVerbs = map[string]string{
	"get":     "GET",
	"post":    "POST",
	"put":     "PUT",
	"delete":  "DELETE",
	"patch":   "PATCH",
	"head":    "HEAD",
	"options": "OPTIONS",
	"all":     "*", // express: register for every method
}

// runDistributed is the W1 entry point. Called once per file from
// declarations.go:visit() after the declaration queries populated v.nodes
// (we need Function/Method intervals to anchor listens_on edges).
//
// Order of operations:
//  1. Next.js file-path heuristic: app/.../route.ts → emit Endpoint per
//     exported HTTP-method function.
//  2. AST scan for framework call patterns (Express / Fastify / Hono).
func (v *declVisitor) runDistributed() {
	v.endpointIDs = map[string]string{}
	v.nextjsExportEndpoints()
	v.frameworkCallEndpoints()
}

// nextjsExportEndpoints handles Next.js App Router file-based routing.
// Detection: file path matches `<root>/app/<segments>/route.ts(x)` (or
// `.js`/`.jsx`). Each top-level `export async function GET|POST|...` (and
// non-async variant) inside such a file yields one Endpoint + listens_on
// edge from the function node to that Endpoint.
//
// The route string is derived from the file path:
//
//	app/api/users/route.ts            → /api/users
//	app/api/users/[id]/route.ts       → /api/users/[id]
//	app/dashboard/route.ts            → /dashboard
//	app/route.ts                      → /
//
// File path normalisation: locate the path segment "app" and take everything
// after it up to the "route.<ext>" filename. Any framework-flavoured
// `(group)` route groups in path segments are preserved (Next.js docs:
// route groups don't affect the URL).
func (v *declVisitor) nextjsExportEndpoints() {
	route, ok := nextjsRouteFromPath(v.rel)
	if !ok {
		return
	}
	// Walk the syntax tree looking for `export_statement` with an inner
	// `function_declaration` whose name is one of the HTTP method
	// constants Next.js routes dispatch on.
	v.walkForNextjsExports(v.root, route)
}

// nextjsRouteFromPath converts a Next.js App Router file path to its route
// string. Returns ("", false) when the path doesn't look like an app-router
// route file. Match shape: the path contains a directory segment exactly
// named "app", and the basename (without extension) is "route".
func nextjsRouteFromPath(rel string) (string, bool) {
	// Normalise to forward slashes (Windows-tolerant).
	p := filepath.ToSlash(rel)
	base := strings.ToLower(filepath.Base(p))
	ext := filepath.Ext(base)
	switch ext {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
	default:
		return "", false
	}
	if strings.TrimSuffix(base, ext) != "route" {
		return "", false
	}
	parts := strings.Split(p, "/")
	// Find the LAST occurrence of "app" so deeper nesting (e.g. an
	// `apps/web/src/app/...` monorepo layout) still resolves the route
	// from the innermost `app/` segment.
	appIdx := -1
	for i := range parts {
		if parts[i] == "app" {
			appIdx = i
		}
	}
	if appIdx < 0 || appIdx == len(parts)-1 {
		return "", false
	}
	// Segments between "app/" and the "route.ext" basename form the route.
	segs := parts[appIdx+1 : len(parts)-1]
	if len(segs) == 0 {
		return "/", true
	}
	return "/" + strings.Join(segs, "/"), true
}

// walkForNextjsExports recursively scans the tree-sitter parse tree
// looking for exported HTTP-method functions and emits the corresponding
// Endpoint + listens_on edge.
func (v *declVisitor) walkForNextjsExports(n *sitter.Node, route string) {
	if n == nil {
		return
	}
	if n.Kind() == "export_statement" {
		// Look for an inner function_declaration. Tree-sitter exposes the
		// exported declaration as a child labelled `declaration`, but for
		// robustness we scan children directly.
		count := int(n.ChildCount())
		for i := 0; i < count; i++ {
			child := n.Child(uint(i))
			if child == nil {
				continue
			}
			if child.Kind() == "function_declaration" {
				v.maybeEmitNextjsRoute(child, route)
			}
		}
	}
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		v.walkForNextjsExports(n.Child(uint(i)), route)
	}
}

// maybeEmitNextjsRoute emits an Endpoint + listens_on if the function
// declaration name is a recognised HTTP method (`GET`, `POST`, ...).
func (v *declVisitor) maybeEmitNextjsRoute(fnDecl *sitter.Node, route string) {
	nameNode := fnDecl.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := nameNode.Utf8Text(v.src)
	method, ok := nextjsHTTPMethod(name)
	if !ok {
		return
	}
	startByte := int(fnDecl.StartByte())
	endByte := int(fnDecl.EndByte())
	startLine := int(fnDecl.StartPosition().Row) + 1
	endLine := int(fnDecl.EndPosition().Row) + 1
	endpointID := v.upsertEndpointTS(method, route, startByte, endByte,
		startLine, endLine, types.ConfExtracted)
	// listens_on: the exported function node was already emitted by the
	// declaration query (queryFunction) keyed on the name's startByte.
	// Resolve by (name, startLine) match against v.nodes.
	nameLine := int(nameNode.StartPosition().Row) + 1
	handlerID := v.findFunctionByNameLine(name, nameLine)
	if handlerID == "" {
		return
	}
	v.edges = append(v.edges, types.Edge{
		Src: handlerID, Dst: endpointID, Type: types.EdgeListensOn,
		Line: startLine, Count: 1, Confidence: types.ConfExtracted,
		FilePath: v.rel,
	})
}

// nextjsHTTPMethod normalises a Next.js exported handler name to its
// canonical HTTP method string. Returns ("", false) for any name that
// isn't a recognised dispatch target.
func nextjsHTTPMethod(name string) (string, bool) {
	switch name {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS":
		return name, true
	}
	return "", false
}

// frameworkCallEndpoints walks the parse tree for `app.METHOD(...)` and
// `router.METHOD(...)` style fluent-API route registrations (Express /
// Koa / Fastify / Hono), plus `fastify.route({ method, url, handler })`.
func (v *declVisitor) frameworkCallEndpoints() {
	v.walkForFrameworkCalls(v.root)
}

// walkForFrameworkCalls recursively finds call_expression nodes whose
// callee shape matches a route-registration pattern. Recursion (rather
// than a tree-sitter query) keeps the framework-detection logic in Go
// where the conditional emission is easier to express than in the query
// language.
func (v *declVisitor) walkForFrameworkCalls(n *sitter.Node) {
	if n == nil {
		return
	}
	if n.Kind() == "call_expression" {
		v.maybeEmitFrameworkRoute(n)
	}
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		v.walkForFrameworkCalls(n.Child(uint(i)))
	}
}

// maybeEmitFrameworkRoute checks one call_expression against the known
// framework patterns and emits an Endpoint + listens_on when matched.
//
// Patterns:
//
//	app.get('/path', handler)         → ("GET", "/path", handler)
//	router.post('/path', handler)     → ("POST", "/path", handler)
//	fastify.route({ method, url, handler })  → object literal extraction
func (v *declVisitor) maybeEmitFrameworkRoute(call *sitter.Node) {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Kind() != "member_expression" {
		return
	}
	prop := fn.ChildByFieldName("property")
	if prop == nil {
		return
	}
	propName := prop.Utf8Text(v.src)
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return
	}

	if propName == "route" {
		v.maybeEmitFastifyRoute(call, args)
		return
	}

	method, isHTTP := httpVerbs[propName]
	if !isHTTP {
		return
	}
	// W2 false-positive guard (schema 1.9): exclude obvious client-side
	// receivers from the server-side detector. `axios.put('/x', ...)` and
	// similar client APIs share the same shape as `router.put('/x', handler)`,
	// so without this guard a single axios call would emit BOTH a (spurious)
	// real Endpoint via W1 AND a placeholder Endpoint via W2. The client
	// detector (http_client.go) owns the call site exclusively.
	if obj := fn.ChildByFieldName("object"); obj != nil {
		objName := obj.Utf8Text(v.src)
		if strings.HasPrefix(objName, "axios") {
			return
		}
	}
	// Need at least (route, handler).
	argList := namedArgs(args)
	if len(argList) < 2 {
		return
	}
	route, conf := routeLiteral(argList[0], v.src)
	if route == "" {
		// No usable string — skip entirely. (We could emit a fully-opaque
		// "<computed>" Endpoint, but with no route hint the node is noise.)
		return
	}
	startByte := int(call.StartByte())
	endByte := int(call.EndByte())
	startLine := int(call.StartPosition().Row) + 1
	endLine := int(call.EndPosition().Row) + 1
	endpointID := v.upsertEndpointTS(method, route, startByte, endByte,
		startLine, endLine, conf)
	v.emitListensOnFromHandlerArg(argList[1], endpointID, startLine, conf)
}

// maybeEmitFastifyRoute handles `fastify.route({ method, url, handler })`
// where method/url/handler are properties of a single object-literal arg.
// All three must be string-literal (method/url) / identifier-or-function
// (handler) for the call to be classified EXTRACTED.
func (v *declVisitor) maybeEmitFastifyRoute(call, args *sitter.Node) {
	argList := namedArgs(args)
	if len(argList) < 1 {
		return
	}
	obj := argList[0]
	if obj.Kind() != "object" {
		return
	}
	props := objectPairs(obj, v.src)
	methodLit, hasMethod := props["method"]
	urlLit, hasURL := props["url"]
	handler, hasHandler := props["handler"]
	if !hasMethod || !hasURL {
		return
	}
	method, mConf := stringLitOrComputed(methodLit, v.src)
	url, uConf := stringLitOrComputed(urlLit, v.src)
	if url == "" {
		return
	}
	conf := mergeConfidence(mConf, uConf)
	startByte := int(call.StartByte())
	endByte := int(call.EndByte())
	startLine := int(call.StartPosition().Row) + 1
	endLine := int(call.EndPosition().Row) + 1
	methodStr := strings.ToUpper(method)
	if methodStr == "" {
		methodStr = "*"
	}
	endpointID := v.upsertEndpointTS(methodStr, url, startByte, endByte,
		startLine, endLine, conf)
	if hasHandler {
		v.emitListensOnFromHandlerArg(handler, endpointID, startLine, conf)
	}
}

// emitListensOnFromHandlerArg attempts to resolve the handler argument to
// a Function/Method node and emit the listens_on edge. Three resolved
// shapes:
//
//   - identifier:                     `usersHandler`
//   - member_expression (selector):   `obj.handleUsers`
//   - function expression / arrow:    inline handler — we emit a synthetic
//     Function node anchored on the inline expression so listens_on still
//     has somewhere to land. (V0 inline-anchoring; the synthetic node has
//     Name="<anonymous>" and Confidence=INFERRED.)
func (v *declVisitor) emitListensOnFromHandlerArg(arg *sitter.Node, endpointID string, line int, conf types.Confidence) {
	if arg == nil {
		return
	}
	switch arg.Kind() {
	case "identifier", "property_identifier":
		name := arg.Utf8Text(v.src)
		handlerID := v.findFunctionByName(name)
		if handlerID == "" {
			return
		}
		v.edges = append(v.edges, types.Edge{
			Src: handlerID, Dst: endpointID, Type: types.EdgeListensOn,
			Line: line, Count: 1, Confidence: conf,
			FilePath: v.rel,
		})
	case "member_expression":
		prop := arg.ChildByFieldName("property")
		if prop == nil {
			return
		}
		name := prop.Utf8Text(v.src)
		handlerID := v.findFunctionByName(name)
		if handlerID == "" {
			return
		}
		v.edges = append(v.edges, types.Edge{
			Src: handlerID, Dst: endpointID, Type: types.EdgeListensOn,
			Line: line, Count: 1, Confidence: conf,
			FilePath: v.rel,
		})
	case "arrow_function", "function_expression":
		// function_expression can be named (`function fooHandler(...) {...}`)
		// — tree-sitter exposes that via ChildByFieldName("name"). Lookup
		// order:
		//   1) Try declaration-pass's top-level Function table — if the name
		//      matches a top-level decl, hang listens_on off that ID directly.
		//   2) Otherwise synthesise a Function node using the ACTUAL name
		//      (the declaration-pass doesn't capture inline named function
		//      expressions, so a previous fix that only resolved-or-fell-through
		//      left them as "<anonymous>" — broken identity for search /
		//      pagerank / impact). Inline-synthesised nodes carry INFERRED
		//      Confidence to mark "name known, no top-level decl".
		// Arrow functions skip the name lookup entirely (no name field).
		handlerName := ""
		if arg.Kind() == "function_expression" {
			if nameNode := arg.ChildByFieldName("name"); nameNode != nil {
				handlerName = nameNode.Utf8Text(v.src)
				if handlerID := v.findFunctionByName(handlerName); handlerID != "" {
					v.edges = append(v.edges, types.Edge{
						Src: handlerID, Dst: endpointID, Type: types.EdgeListensOn,
						Line: line, Count: 1, Confidence: conf,
						FilePath: v.rel,
					})
					return
				}
			}
		}
		// Synthesise a Function node so listens_on has a concrete src.
		// Name = actual identifier when known (inline named function
		// expression), else "<anonymous>". Truly anonymous arrow handlers
		// keep the "<anonymous>" sentinel for downstream filtering.
		synthName := handlerName
		if synthName == "" {
			synthName = "<anonymous>"
		}
		startByte := int(arg.StartByte())
		endByte := int(arg.EndByte())
		startLine := int(arg.StartPosition().Row) + 1
		endLine := int(arg.EndPosition().Row) + 1
		qname := synthName + "@" + v.rel + ":" + itoa(startLine)
		id := makeID(qname, "ts", startByte)
		v.nodes = append(v.nodes, types.Node{
			ID: id, Type: types.NodeFunction, Name: synthName,
			QualifiedName: qname,
			FilePath:      v.rel, StartLine: startLine, EndLine: endLine,
			StartByte: startByte, EndByte: endByte,
			Language: "ts", Confidence: types.ConfInferred,
		})
		v.edges = append(v.edges, types.Edge{
			Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1,
			Confidence: types.ConfInferred,
		})
		v.edges = append(v.edges, types.Edge{
			Src: id, Dst: endpointID, Type: types.EdgeListensOn,
			Line: line, Count: 1, Confidence: mergeConfidence(conf, types.ConfInferred),
			FilePath: v.rel,
		})
	}
}

// upsertEndpointTS emits one Endpoint node per (method, route) combination,
// deduplicating same-file repeats. Returns the node ID for callers to use
// as the listens_on edge destination.
//
// route is the literal path string (or "<computed>" sentinel). If the route
// is a computed sentinel, the qname uses that sentinel and Confidence is
// caller-controlled (typically INFERRED).
func (v *declVisitor) upsertEndpointTS(method, route string, startByte, endByte, startLine, endLine int, conf types.Confidence) string {
	qname := "http:" + method + " " + route
	if id, ok := v.endpointIDs[qname]; ok {
		return id
	}
	id := makeID(qname, "ts", startByte)
	v.endpointIDs[qname] = id
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: types.NodeEndpoint,
		Name: route, QualifiedName: qname,
		FilePath: v.rel, StartLine: startLine, EndLine: endLine,
		StartByte: startByte, EndByte: endByte,
		Language: "ts", Confidence: conf, SubKind: "http",
	})
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1,
		Confidence: types.ConfExtracted,
	})
	return id
}

// findFunctionByName returns the ID of the first Function/Method node in
// the current file's nodes whose Name matches name. Returns "" on miss.
func (v *declVisitor) findFunctionByName(name string) string {
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

// findFunctionByNameLine is like findFunctionByName but additionally
// constrains by the function's StartLine (the line the IDENTIFIER appears
// on). Used by the Next.js export pass where multiple exported functions
// might share a common name across files but inside one file each name is
// unique.
func (v *declVisitor) findFunctionByNameLine(name string, line int) string {
	for i := range v.nodes {
		n := &v.nodes[i]
		if n.Type != types.NodeFunction && n.Type != types.NodeMethod {
			continue
		}
		if n.Name == name && n.StartLine == line {
			return n.ID
		}
	}
	// Fallback: name-only.
	return v.findFunctionByName(name)
}

// namedArgs returns the non-trivial children of an `arguments` node,
// filtering out punctuation tokens like `(`, `)`, and `,`.
func namedArgs(args *sitter.Node) []*sitter.Node {
	if args == nil {
		return nil
	}
	out := []*sitter.Node{}
	count := int(args.ChildCount())
	for i := 0; i < count; i++ {
		c := args.Child(uint(i))
		if c == nil {
			continue
		}
		if !c.IsNamed() {
			continue
		}
		out = append(out, c)
	}
	return out
}

// objectPairs flattens an `object` node into a {key → value-node} map,
// using each `pair`'s `key` and `value` field children. Computed keys
// (string-literal keys) and shorthand identifier values are tolerated;
// non-identifier-shaped keys are skipped (no use case in V0).
func objectPairs(obj *sitter.Node, src []byte) map[string]*sitter.Node {
	out := map[string]*sitter.Node{}
	if obj == nil {
		return out
	}
	count := int(obj.ChildCount())
	for i := 0; i < count; i++ {
		c := obj.Child(uint(i))
		if c == nil || c.Kind() != "pair" {
			continue
		}
		key := c.ChildByFieldName("key")
		val := c.ChildByFieldName("value")
		if key == nil || val == nil {
			continue
		}
		var name string
		switch key.Kind() {
		case "property_identifier", "identifier":
			name = key.Utf8Text(src)
		case "string":
			name = stringFragmentText(key, src)
		default:
			continue
		}
		if name == "" {
			continue
		}
		out[name] = val
	}
	return out
}

// routeLiteral inspects an argument node and returns either the literal
// route string + EXTRACTED, or ("<computed>", INFERRED) when the argument
// isn't a plain string literal. The (qname, computed) pair is used by
// callers to flag dynamic-route Endpoints distinctly.
//
// Returns ("", "") when the argument is neither a string nor any other
// detectable expression (caller may drop the emission entirely).
func routeLiteral(arg *sitter.Node, src []byte) (string, types.Confidence) {
	if arg == nil {
		return "", ""
	}
	switch arg.Kind() {
	case "string":
		s := stringFragmentText(arg, src)
		if s == "" {
			return "<computed>", types.ConfInferred
		}
		return s, types.ConfExtracted
	case "template_string":
		// V0: any template_string treated as <computed>. Could harden by
		// detecting templates without ${...} substitutions, but those are
		// rare in route registrations.
		return "<computed>", types.ConfInferred
	}
	return "<computed>", types.ConfInferred
}

// stringLitOrComputed is like routeLiteral but tailored for arbitrary
// string-or-computed fields (method, url) inside an object literal.
// Returns ("", ConfInferred) when the node isn't a string at all so the
// caller can decide whether to skip emission entirely.
func stringLitOrComputed(n *sitter.Node, src []byte) (string, types.Confidence) {
	if n == nil {
		return "", types.ConfInferred
	}
	if n.Kind() == "string" {
		s := stringFragmentText(n, src)
		if s == "" {
			return "", types.ConfInferred
		}
		return s, types.ConfExtracted
	}
	return "", types.ConfInferred
}

// stringFragmentText returns the textual content of a tree-sitter `string`
// node, stripped of its surrounding quote tokens. Returns "" for empty
// strings, template strings with substitutions (handled by caller via
// node-Kind check), or pathological shapes.
func stringFragmentText(n *sitter.Node, src []byte) string {
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		c := n.Child(uint(i))
		if c == nil {
			continue
		}
		if c.Kind() == "string_fragment" {
			return c.Utf8Text(src)
		}
	}
	// Fallback: strip outer quotes from the entire node text.
	text := n.Utf8Text(src)
	if len(text) >= 2 {
		first := text[0]
		last := text[len(text)-1]
		if (first == '"' || first == '\'' || first == '`') && first == last {
			return text[1 : len(text)-1]
		}
	}
	return text
}

// mergeConfidence returns the LOWER of two confidence labels (EXTRACTED >
// INFERRED > AMBIGUOUS). Used to combine per-field confidences inside the
// Fastify object-literal path where either method or url might be computed.
func mergeConfidence(a, b types.Confidence) types.Confidence {
	rank := func(c types.Confidence) int {
		switch c {
		case types.ConfExtracted:
			return 2
		case types.ConfInferred:
			return 1
		case types.ConfAmbiguous:
			return 0
		}
		return 2 // default to optimistic when unset
	}
	if rank(a) <= rank(b) {
		if a == "" {
			return b
		}
		return a
	}
	if b == "" {
		return a
	}
	return b
}
