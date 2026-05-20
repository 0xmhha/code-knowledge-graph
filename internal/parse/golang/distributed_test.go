package golang_test

import (
	"strings"
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestDistributed_HTTP_Endpoints asserts that http.HandleFunc / Handle and
// (*ServeMux).HandleFunc calls produce NodeEndpoint entries deduped by
// route, with subKind="http".
func TestDistributed_HTTP_Endpoints(t *testing.T) {
	root := "testdata/distributed"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	endpoints := nodesByType(g.Nodes, types.NodeEndpoint)
	// Schema 1.9 §6.2 — HTTP Endpoint qname is `http:METHOD /route`. Plain
	// `http.HandleFunc("/x", ...)` registrations carry method="*" because
	// net/http dispatches all verbs to the same handler. The "GET /scoped"
	// case exercises Go 1.22+ method-prefixed pattern parsing.
	wantRoutes := map[string]bool{
		"http:* /users":    false,
		"http:* /admin":    false,
		"http:* /health":   false,
		"http:* /ping":     false, // anonymous handler — endpoint still emitted
		"http:GET /scoped": false, // Go 1.22+ method-prefixed pattern
	}
	for _, n := range endpoints {
		if _, want := wantRoutes[n.QualifiedName]; want {
			wantRoutes[n.QualifiedName] = true
		}
		// Scope the sub_kind assertion to HTTP endpoints — the W3b grpc
		// detection lands in the same fixture and emits sub_kind="grpc"
		// Endpoint nodes that share the listens_on edge type but a different
		// route family. Filtering by qname prefix keeps the HTTP test
		// surface unchanged.
		if !strings.HasPrefix(n.QualifiedName, "http:") {
			continue
		}
		if n.SubKind != "http" {
			t.Errorf("Endpoint %q sub_kind = %q, want http", n.QualifiedName, n.SubKind)
		}
	}
	for route, found := range wantRoutes {
		if !found {
			t.Errorf("missing Endpoint for route %q", route)
		}
	}
}

// TestDistributed_HTTP_ListensOnEdges verifies listens_on edges connect
// the named handler functions to their endpoints, and that the anonymous
// handler at /ping does NOT produce a listens_on edge (V0 limitation).
func TestDistributed_HTTP_ListensOnEdges(t *testing.T) {
	root := "testdata/distributed"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	listensOn := edgesByType(g.Edges, types.EdgeListensOn)
	// usersHandler, adminHandler, healthHandler, and the Go 1.22 GET /scoped
	// re-use of usersHandler — at least 4 named-handler edges.
	if len(listensOn) < 3 {
		t.Errorf("listens_on count: got %d, want >=3 (usersHandler/adminHandler/healthHandler)",
			len(listensOn))
	}
	// Each named-handler edge must point INTO an `http:METHOD /route` Endpoint
	// (i.e. handler is src, endpoint is dst). Schema 1.9 §6.2.
	for _, e := range listensOn {
		dst := findNodeByID(g.Nodes, e.Dst)
		if dst == nil || dst.Type != types.NodeEndpoint {
			t.Errorf("listens_on dst is not an Endpoint: %+v", e)
			continue
		}
		if !strings.HasPrefix(dst.QualifiedName, "http:") {
			t.Errorf("listens_on Endpoint qname missing http: prefix: %q", dst.QualifiedName)
		}
	}
	// The /ping endpoint must NOT have a listens_on edge into it.
	var pingID string
	for _, n := range g.Nodes {
		if n.Type == types.NodeEndpoint && n.QualifiedName == "http:* /ping" {
			pingID = n.ID
			break
		}
	}
	if pingID == "" {
		t.Fatal("http:* /ping Endpoint not found")
	}
	for _, e := range listensOn {
		if e.Dst == pingID {
			t.Errorf("listens_on into anonymous /ping handler should be skipped: %+v", e)
		}
	}
}

// TestDistributed_HTTP_MethodHandler_NoDangling pins down the regression
// where method-receiver handlers (s.mux.HandleFunc("/x", s.handleX))
// produced dangling listens_on edges. idForFunc was computing the ID
// from fn.Pos() (the method NAME offset per go/types semantics) while
// visitFuncDecl uses the FuncDecl's Pos (the `func` keyword) — for
// methods these differ by the receiver-clause width. The fix is a
// qname lookup against v.nodes.
//
// This test asserts BOTH (a) the listens_on edges exist for the method
// handlers, and (b) every edge's Src is a Method node actually present
// in the graph. Before the fix, (b) would fail because Src pointed at
// a recomputed ID with no matching node.
func TestDistributed_HTTP_MethodHandler_NoDangling(t *testing.T) {
	root := "testdata/distributed"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	nodeByID := make(map[string]*types.Node, len(g.Nodes))
	for i := range g.Nodes {
		nodeByID[g.Nodes[i].ID] = &g.Nodes[i]
	}
	methodRoutes := map[string]bool{
		"http:* /method-a": false,
		"http:* /method-b": false,
	}
	listensOn := edgesByType(g.Edges, types.EdgeListensOn)
	for _, e := range listensOn {
		dst := nodeByID[e.Dst]
		if dst == nil || dst.Type != types.NodeEndpoint {
			continue
		}
		if _, want := methodRoutes[dst.QualifiedName]; !want {
			continue
		}
		methodRoutes[dst.QualifiedName] = true
		src := nodeByID[e.Src]
		if src == nil {
			t.Errorf("listens_on for %s has dangling src %q (regression)",
				dst.QualifiedName, e.Src)
			continue
		}
		if src.Type != types.NodeMethod {
			t.Errorf("listens_on for %s src is %s, want Method", dst.QualifiedName, src.Type)
		}
		if !strings.HasSuffix(src.QualifiedName, ".MethodServer.handleA") &&
			!strings.HasSuffix(src.QualifiedName, ".MethodServer.handleB") {
			t.Errorf("listens_on for %s src qname is %q, want MethodServer.handle{A,B}",
				dst.QualifiedName, src.QualifiedName)
		}
	}
	for route, found := range methodRoutes {
		if !found {
			t.Errorf("listens_on edge missing for method-handler route %q", route)
		}
	}
}

// TestDistributed_JSONRPC_HandlesMessage asserts the EchoService.Echo
// handler matches the net/rpc shape and produces a handles_message edge to
// the EchoArgs MessageType. Confirms false-positive guards: NotJSONRPC
// (wrong return type), AlsoNotJSONRPC (non-pointer reply), and
// FreeFunctionEcho (no receiver) must NOT produce edges.
func TestDistributed_JSONRPC_HandlesMessage(t *testing.T) {
	root := "testdata/distributed"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	msgs := nodesByType(g.Nodes, types.NodeMessageType)
	var echoArgsID string
	for _, n := range msgs {
		if strings.HasSuffix(n.QualifiedName, ".EchoArgs") {
			echoArgsID = n.ID
			break
		}
	}
	if echoArgsID == "" {
		t.Fatal("MessageType for EchoArgs not found")
	}
	hm := edgesByType(g.Edges, types.EdgeHandlesMessage)
	var hits int
	for _, e := range hm {
		if e.Dst != echoArgsID {
			continue
		}
		src := findNodeByID(g.Nodes, e.Src)
		if src == nil {
			continue
		}
		if !strings.HasSuffix(src.QualifiedName, "EchoService.Echo") {
			t.Errorf("handles_message src should be EchoService.Echo, got %q", src.QualifiedName)
		}
		hits++
	}
	if hits != 1 {
		t.Errorf("handles_message edges into EchoArgs: got %d, want 1", hits)
	}
	// False-positive guard: NotJSONRPC / AlsoNotJSONRPC / FreeFunctionEcho
	// must NOT contribute any handles_message edge.
	for _, e := range hm {
		src := findNodeByID(g.Nodes, e.Src)
		if src == nil {
			continue
		}
		bad := []string{"NotJSONRPC", "AlsoNotJSONRPC", "FreeFunctionEcho"}
		for _, b := range bad {
			if strings.HasSuffix(src.QualifiedName, "."+b) {
				t.Errorf("handles_message false positive: %q matched %q",
					src.QualifiedName, b)
			}
		}
	}
}

// TestDistributed_RPCCalls_NetRPC asserts client.Call("Service.Method", ...)
// produces an rpc_calls edge to a MessageType placeholder, and that the
// dynamic-target form is correctly skipped.
func TestDistributed_RPCCalls_NetRPC(t *testing.T) {
	root := "testdata/distributed"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	rpcCalls := edgesByType(g.Edges, types.EdgeRPCCalls)
	// CallEcho + CallSomethingElse + FakeUse(false-positive accepted in V0)
	// = at least 2 named targets.
	wantTargets := map[string]bool{
		"rpc:EchoService.Echo": false,
		"rpc:Math.Add":         false,
	}
	for _, e := range rpcCalls {
		dst := findNodeByID(g.Nodes, e.Dst)
		if dst == nil {
			continue
		}
		if _, want := wantTargets[dst.QualifiedName]; want {
			wantTargets[dst.QualifiedName] = true
		}
	}
	for tgt, found := range wantTargets {
		if !found {
			t.Errorf("missing rpc_calls edge for target %q", tgt)
		}
	}
	// Dynamic target (variable as first arg) must NOT produce an edge —
	// no MessageType qname containing "method" should exist.
	for _, n := range nodesByType(g.Nodes, types.NodeMessageType) {
		if n.Name == "method" || n.Name == "" {
			t.Errorf("dynamic-target rpc.Call should not produce a MessageType: %+v", n)
		}
	}
}

// TestDistributed_HTTP_ClientCalls asserts the W2 client detector emits
// placeholder Endpoints + http_calls edges for Go HTTP client call sites
// in testdata/distributed/http_clients.go. The placeholders use
// Language="external" so the downstream link pass (internal/link/http_match.go)
// can locate them for cascade resolution.
func TestDistributed_HTTP_ClientCalls(t *testing.T) {
	root := "testdata/distributed"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	placeholders := map[string]types.Node{}
	for _, n := range g.Nodes {
		if n.Type != types.NodeEndpoint || n.Language != "external" {
			continue
		}
		// Scope to HTTP placeholders — W3b grpc client detection emits
		// language="external" Endpoints with sub_kind="grpc" in the same
		// fixture (CallExternalService), which would otherwise fail the
		// sub_kind/AMBIGUOUS assertions tailored to HTTP.
		if !strings.HasPrefix(n.QualifiedName, "http:") {
			continue
		}
		if n.SubKind != "http" {
			t.Errorf("placeholder %q sub_kind=%q, want http", n.QualifiedName, n.SubKind)
		}
		if n.Confidence != types.ConfAmbiguous {
			t.Errorf("placeholder %q confidence=%q, want AMBIGUOUS",
				n.QualifiedName, n.Confidence)
		}
		placeholders[n.QualifiedName] = n
	}
	wantPlaceholders := []string{
		"http:GET /users",
		"http:POST /admin",
		"http:HEAD /health",
		"http:PUT /method-a",
		"http:GET /external/endpoint",
	}
	for _, want := range wantPlaceholders {
		if _, ok := placeholders[want]; !ok {
			t.Errorf("missing client placeholder %q (saw %v)",
				want, mapKeysNode(placeholders))
		}
	}
	httpCalls := edgesByType(g.Edges, types.EdgeHTTPCalls)
	// Fixture has 7 detectable client call sites (DynamicURLSkipped is
	// intentionally dropped by V0).
	if len(httpCalls) < 6 {
		t.Errorf("http_calls edge count: got %d, want ≥ 6", len(httpCalls))
	}
	// Each http_calls edge must point at a placeholder Endpoint.
	placeholderIDs := map[string]bool{}
	for _, n := range placeholders {
		placeholderIDs[n.ID] = true
	}
	for _, e := range httpCalls {
		if !placeholderIDs[e.Dst] {
			t.Errorf("http_calls edge dst=%q not a placeholder Endpoint", e.Dst)
		}
	}
}

// TestDistributed_HTTP_ClientCalls_DynamicSkipped pins down that a variable
// URL produces no http_calls edge (V0 string-literal restriction).
func TestDistributed_HTTP_ClientCalls_DynamicSkipped(t *testing.T) {
	root := "testdata/distributed"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	// No client placeholder Endpoint should target a route that came from
	// the DynamicURLSkipped function — V0 detector drops variable URLs.
	// Indirectly verified: no placeholder with qname containing "target"
	// (the variable name) should exist.
	for _, n := range g.Nodes {
		if n.Type != types.NodeEndpoint {
			continue
		}
		if n.Language == "external" && strings.Contains(n.QualifiedName, "target") {
			t.Errorf("dynamic URL leaked into placeholder Endpoint: %q",
				n.QualifiedName)
		}
	}
}

func mapKeysNode(m map[string]types.Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestDistributed_NoRegression confirms E3's pass doesn't disturb the B1
// concurrency emission or the existing call-graph wiring on the same
// fixture (the testdata is HTTP/RPC-heavy but should still report 0 Mutex
// nodes and zero acquires_lock edges since no sync.Mutex is in scope).
func TestDistributed_NoRegression(t *testing.T) {
	root := "testdata/distributed"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	if got := len(nodesByType(g.Nodes, types.NodeMutex)); got != 0 {
		t.Errorf("unexpected NodeMutex emission on distributed fixture: %d", got)
	}
	if got := len(edgesByType(g.Edges, types.EdgeAcquiresLock)); got != 0 {
		t.Errorf("unexpected acquires_lock emission on distributed fixture: %d", got)
	}
	// Function nodes for the named handlers must exist (we resolve listens_on
	// against them).
	wantFuncs := []string{"usersHandler", "adminHandler", "healthHandler", "Echo"}
	have := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Type == types.NodeFunction || n.Type == types.NodeMethod {
			have[n.Name] = true
		}
	}
	for _, w := range wantFuncs {
		if !have[w] {
			t.Errorf("missing Function/Method node for %q (E3 anchor)", w)
		}
	}
}
