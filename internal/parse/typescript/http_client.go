// Package typescript — http_client.go implements W2 of schema 1.9 (CKS G5
// Distributed cross-language interop expansion): TypeScript HTTP client
// detection. Companion to distributed.go (server-side W1) — the two together
// let graph traversal answer "TS Func → http_calls → Endpoint ← listens_on
// ← Go Method" in a monorepo.
//
// Detection patterns (V0 — string-literal URLs only):
//
//  1. fetch('/api/x')              → method=GET (default)
//  2. fetch('/api/x', { method: 'POST' })
//     → method extracted from options object
//  3. axios.get('/api/x', ...)     → method=GET
//     axios.post / put / delete / patch / head    → same pattern
//  4. axios('/api/x', { method, url })            → method from options
//     axios({ method, url })                      → both from options
//     axios.request({ method, url })              → same
//  5. useSWR('/api/x', fetcher)                   → method=* (any) per
//     §6.9 "method unknown"
//  6. useQuery({ url: '/api/x' })  → method=* (any). queryKey['/api/x'] is
//     skipped — too ambiguous in V0.
//
// Each detection emits an AMBIGUOUS placeholder Endpoint node + an
// `http_calls` edge from the enclosing TS Function to the placeholder. The
// link pass (internal/link/http_match.go) then either rewires the edge to
// a real server-side Endpoint (cascade: specific verb → wildcard) or keeps
// the placeholder as an external-API marker (§6.3 (B), §6.9).
//
// Out of scope (deferred):
//   - ky.get/post, wretch, superagent — same shape as axios; can fold in once
//     V0 stabilises.
//   - Template-string URLs (`${base}/users`) — INFERRED with route placeholder.
//   - Dynamic methods (axios[verb]('/x')) — would need const-fold.
package typescript

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// axiosVerbs maps lowercase axios method names to their canonical HTTP
// uppercase form. Mirrors httpVerbs in distributed.go but scoped to the
// client-side surface (no "all" because axios.all is a different concept —
// Promise.all wrapper, not a wildcard verb).
var axiosVerbs = map[string]string{
	"get":     "GET",
	"post":    "POST",
	"put":     "PUT",
	"delete":  "DELETE",
	"patch":   "PATCH",
	"head":    "HEAD",
	"options": "OPTIONS",
}

// runHTTPClients is the W2 entry point. Called from runDistributed() after
// the W1 server-side pass. Walks the parse tree for call_expression nodes
// matching the supported client shapes and emits http_calls edges.
//
// Order matters: this MUST run after the declaration queries (so v.nodes
// has Function/Method intervals) AND after the W1 server pass (so any
// fetch/axios call inside a route handler still gets an http_calls edge
// alongside the handler's listens_on).
func (v *declVisitor) runHTTPClients() {
	intervals := collectFnIntervalsFromTree(v)
	v.walkForHTTPClientCalls(v.root, intervals)
}

// walkForHTTPClientCalls recursively scans the parse tree for client call
// patterns. We hand-roll the walk (rather than a tree-sitter query) for the
// same reason runBodyStatements does — the predicate "is this call inside
// any Function/Method interval, and what's its enclosing function ID?"
// depends on dynamic state that's painful in the query language.
func (v *declVisitor) walkForHTTPClientCalls(n *sitter.Node, intervals []fnInterval) {
	if n == nil {
		return
	}
	if n.Kind() == "call_expression" {
		v.maybeEmitHTTPClientCall(n, intervals)
	}
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		v.walkForHTTPClientCalls(n.Child(uint(i)), intervals)
	}
}

// maybeEmitHTTPClientCall inspects one call_expression and emits an
// http_calls edge when it matches a supported client pattern. The edge
// Src is the smallest-enclosing Function/Method ID; calls at module scope
// (outside any function — e.g. top-level `await fetch(...)`) are dropped
// because http_calls requires a Function source.
func (v *declVisitor) maybeEmitHTTPClientCall(call *sitter.Node, intervals []fnInterval) {
	method, rawURL, ok := classifyTSHTTPClientCall(call, v.src)
	if !ok || rawURL == "" {
		return
	}
	path := extractTSURLPath(rawURL)
	if path == "" {
		return
	}
	startByte := int(call.StartByte())
	parentID, hasParent := findEnclosingFn(intervals, startByte)
	if !hasParent {
		// Top-level / module-scope client calls — graph has nowhere to anchor
		// the edge. Drop in V0; deferred to a future pass that synthesises
		// a Module-scope Function placeholder.
		return
	}
	endpointID := v.upsertHTTPClientPlaceholderTS(method, path, call)
	startLine := int(call.StartPosition().Row) + 1
	v.edges = append(v.edges, types.Edge{
		Src: parentID, Dst: endpointID, Type: types.EdgeHTTPCalls,
		Line: startLine, Count: 1, Confidence: types.ConfInferred,
		FilePath: v.rel,
	})
}

// classifyTSHTTPClientCall inspects a call_expression node and returns
// (HTTP method, URL string, true) when the call matches one of the
// supported client patterns. Returns ("", "", false) on miss.
//
// Method default is "*" (wildcard) when the call shape doesn't carry an
// explicit method literal — e.g. `fetch(url)` without an options object,
// `useSWR(url, fetcher)`, `useQuery({ url })`. The link pass cascade
// (§6.9) treats "*" as "lookup any method" so the matching still works.
func classifyTSHTTPClientCall(call *sitter.Node, src []byte) (method, url string, ok bool) {
	fn := call.ChildByFieldName("function")
	args := call.ChildByFieldName("arguments")
	if fn == nil || args == nil {
		return "", "", false
	}
	argList := namedArgs(args)
	if len(argList) == 0 {
		return "", "", false
	}
	// Unwrap TypeScript `as_expression` / `type_assertion` wrappers so that
	// `useQuery({ url } as any)` resolves to the inner object literal. These
	// only appear on TS sources; JS sources skip the unwrap silently because
	// the node kind never matches.
	for i, arg := range argList {
		argList[i] = unwrapTSCast(arg)
	}

	switch fn.Kind() {
	case "identifier":
		// fetch(...) / useSWR(...) / useQuery(...) / axios(...)
		name := fn.Utf8Text(src)
		return classifyTSClientByName(name, argList, src)
	case "member_expression":
		// axios.get(...) / axios.post(...) / axios.request(...)
		obj := fn.ChildByFieldName("object")
		prop := fn.ChildByFieldName("property")
		if obj == nil || prop == nil {
			return "", "", false
		}
		objName := obj.Utf8Text(src)
		propName := prop.Utf8Text(src)
		// Restrict member-expression handling to axios* to avoid false-positive
		// .get on arbitrary objects (e.g. Map.get(key), URLSearchParams.get).
		// V0 narrows to the common case; broader matching is a follow-up.
		if !strings.HasPrefix(objName, "axios") {
			return "", "", false
		}
		if propName == "request" {
			return classifyAxiosOptionsCall(argList, src)
		}
		verb, isVerb := axiosVerbs[propName]
		if !isVerb {
			return "", "", false
		}
		// axios.get('/x', config) — first arg is the URL string.
		urlStr, ok := stringLiteralArg(argList[0], src)
		if !ok || urlStr == "" {
			return "", "", false
		}
		return verb, urlStr, true
	}
	return "", "", false
}

// classifyTSClientByName handles call_expression nodes whose callee is a
// bare identifier — fetch / useSWR / useQuery / axios (function form).
func classifyTSClientByName(name string, argList []*sitter.Node, src []byte) (method, url string, ok bool) {
	switch name {
	case "fetch":
		// fetch(url) → GET
		// fetch(url, { method: 'POST' }) → POST
		urlStr, ok := stringLiteralArg(argList[0], src)
		if !ok || urlStr == "" {
			return "", "", false
		}
		method := "GET"
		if len(argList) >= 2 && argList[1].Kind() == "object" {
			if m, _ := objectMethodLiteral(argList[1], src); m != "" {
				method = strings.ToUpper(m)
			}
		}
		return method, urlStr, true
	case "useSWR":
		// useSWR(key, fetcher, options) — first arg may be a string URL or
		// a tuple. V0 handles string only; method is wildcard since SWR
		// doesn't specify verb (the fetcher does).
		urlStr, ok := stringLiteralArg(argList[0], src)
		if !ok || urlStr == "" {
			return "", "", false
		}
		return "*", urlStr, true
	case "useQuery":
		// useQuery({ url: '/x' }) or useQuery({ queryKey: ['/x'], ... }).
		// V0 handles only the `url` property since queryKey is overloaded
		// (a cache key, not necessarily a URL).
		if argList[0].Kind() != "object" {
			return "", "", false
		}
		props := objectPairs(argList[0], src)
		urlNode, hasURL := props["url"]
		if !hasURL {
			return "", "", false
		}
		urlStr, conf := stringLitOrComputed(urlNode, src)
		if urlStr == "" || conf != types.ConfExtracted {
			return "", "", false
		}
		return "*", urlStr, true
	case "axios":
		// axios(config) — config has {method, url}.
		// axios(url, config) — first arg URL, second config (rarely used).
		if argList[0].Kind() == "object" {
			return classifyAxiosOptionsCall(argList, src)
		}
		urlStr, ok := stringLiteralArg(argList[0], src)
		if !ok || urlStr == "" {
			return "", "", false
		}
		method := "GET"
		if len(argList) >= 2 && argList[1].Kind() == "object" {
			if m, _ := objectMethodLiteral(argList[1], src); m != "" {
				method = strings.ToUpper(m)
			}
		}
		return method, urlStr, true
	}
	return "", "", false
}

// classifyAxiosOptionsCall extracts (method, url) from a single object-literal
// argument with `method` and `url` keys — used by `axios({...})` and
// `axios.request({...})`. Returns ("", "", false) when url is missing or
// computed; method defaults to "*" when absent or computed.
func classifyAxiosOptionsCall(argList []*sitter.Node, src []byte) (string, string, bool) {
	if len(argList) == 0 || argList[0].Kind() != "object" {
		return "", "", false
	}
	props := objectPairs(argList[0], src)
	urlNode, hasURL := props["url"]
	if !hasURL {
		return "", "", false
	}
	urlStr, urlConf := stringLitOrComputed(urlNode, src)
	if urlStr == "" || urlConf != types.ConfExtracted {
		return "", "", false
	}
	method := "*"
	if methodNode, hasMethod := props["method"]; hasMethod {
		if mStr, mConf := stringLitOrComputed(methodNode, src); mStr != "" && mConf == types.ConfExtracted {
			method = strings.ToUpper(mStr)
		}
	}
	return method, urlStr, true
}

// objectMethodLiteral extracts a string-literal `method` property from an
// object node. Returns ("", false) when missing, non-string, or computed.
func objectMethodLiteral(obj *sitter.Node, src []byte) (string, bool) {
	props := objectPairs(obj, src)
	methodNode, ok := props["method"]
	if !ok {
		return "", false
	}
	s, conf := stringLitOrComputed(methodNode, src)
	if s == "" || conf != types.ConfExtracted {
		return "", false
	}
	return s, true
}

// stringLiteralArg returns the literal text of an argument node when it's a
// plain string literal, ("", false) otherwise. Helper to keep the
// classification functions terse.
func stringLiteralArg(arg *sitter.Node, src []byte) (string, bool) {
	if arg == nil {
		return "", false
	}
	if arg.Kind() != "string" {
		return "", false
	}
	s := stringFragmentText(arg, src)
	return s, s != ""
}

// unwrapTSCast peels off TypeScript type-assertion wrappers (`as_expression`
// and `type_assertion`) so the classification logic sees the underlying
// value node (commonly an object literal). Returns the input unchanged for
// non-cast nodes.
func unwrapTSCast(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	for {
		switch n.Kind() {
		case "as_expression", "satisfies_expression", "type_assertion":
			// Find the first named child — that's the underlying value.
			count := int(n.ChildCount())
			var inner *sitter.Node
			for i := 0; i < count; i++ {
				c := n.Child(uint(i))
				if c == nil || !c.IsNamed() {
					continue
				}
				if c.Kind() == "type_identifier" || c.Kind() == "predefined_type" ||
					c.Kind() == "type_annotation" || c.Kind() == "literal_type" {
					continue
				}
				inner = c
				break
			}
			if inner == nil || inner == n {
				return n
			}
			n = inner
		default:
			return n
		}
	}
}

// extractTSURLPath strips an absolute URL's scheme + host, returning only
// the path. Mirrors the Go-side extractURLPath helper.
//
// Examples:
//
//	"/api/users"                  → "/api/users"
//	"https://api.example.com/foo" → "/foo"
//	"http://localhost:8080/x"     → "/x"
//	"api.example.com/x"           → ""   (no leading slash, no scheme)
func extractTSURLPath(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "/") {
		return raw
	}
	i := strings.Index(raw, "://")
	if i < 0 {
		return ""
	}
	rest := raw[i+3:]
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return "/"
	}
	return rest[slash:]
}

// upsertHTTPClientPlaceholderTS emits one AMBIGUOUS placeholder Endpoint
// per (method, path) tuple inside this file. Distinct ID space from real
// server-side Endpoints (Language="external" rather than "ts") so the
// link pass can locate placeholders by language sentinel.
func (v *declVisitor) upsertHTTPClientPlaceholderTS(method, path string, call *sitter.Node) string {
	if v.httpClientPlaceholderIDs == nil {
		v.httpClientPlaceholderIDs = map[string]string{}
	}
	qname := "http:" + method + " " + path
	if id, ok := v.httpClientPlaceholderIDs[qname]; ok {
		return id
	}
	startByte := int(call.StartByte())
	endByte := int(call.EndByte())
	startLine := int(call.StartPosition().Row) + 1
	endLine := int(call.EndPosition().Row) + 1
	id := makeID(qname, "external", startByte)
	v.httpClientPlaceholderIDs[qname] = id
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: types.NodeEndpoint,
		Name: path, QualifiedName: qname,
		FilePath: v.rel, StartLine: startLine, EndLine: endLine,
		StartByte: startByte, EndByte: endByte,
		Language: "external", Confidence: types.ConfAmbiguous, SubKind: "http",
	})
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1,
		Confidence: types.ConfAmbiguous,
	})
	return id
}
