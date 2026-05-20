package golang_test

import (
	"strings"
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestGRPC_ServerListensOn asserts that pb.RegisterXXXServer(s, &impl{})
// patterns produce one grpc_listens_on edge per exported method on the
// impl type, with sub_kind="grpc" Endpoint nodes named
// `grpc:Service.Method` and Language="go".
//
// Source: testdata/distributed/grpc_server.go.
func TestGRPC_ServerListensOn(t *testing.T) {
	root := "testdata/distributed"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	endpoints := map[string]types.Node{}
	for _, n := range g.Nodes {
		if n.Type != types.NodeEndpoint {
			continue
		}
		if !strings.HasPrefix(n.QualifiedName, "grpc:") {
			continue
		}
		endpoints[n.QualifiedName] = n
	}

	wantServerEndpoints := []string{
		"grpc:UserService.GetUser",
		"grpc:UserService.ListUsers",
		"grpc:EchoService.Echo",
	}
	for _, q := range wantServerEndpoints {
		n, ok := endpoints[q]
		if !ok {
			t.Errorf("missing server-side Endpoint %q", q)
			continue
		}
		if n.SubKind != "grpc" {
			t.Errorf("Endpoint %q sub_kind=%q, want grpc", q, n.SubKind)
		}
		if n.Language != "go" {
			t.Errorf("Endpoint %q language=%q, want go (server-side)", q, n.Language)
		}
	}

	listensOn := edgesByType(g.Edges, types.EdgeGRPCListensOn)
	hits := map[string]int{}
	for _, e := range listensOn {
		dst := findNodeByID(g.Nodes, e.Dst)
		if dst == nil {
			continue
		}
		hits[dst.QualifiedName]++
		src := findNodeByID(g.Nodes, e.Src)
		if src == nil {
			t.Errorf("grpc_listens_on for %s has dangling src %q",
				dst.QualifiedName, e.Src)
			continue
		}
		if src.Type != types.NodeMethod {
			t.Errorf("grpc_listens_on for %s src is %s, want Method",
				dst.QualifiedName, src.Type)
		}
	}
	for _, q := range wantServerEndpoints {
		if hits[q] == 0 {
			t.Errorf("grpc_listens_on edge missing for %q", q)
		}
	}
}

// TestGRPC_ClientCalls asserts that client.RpcMethod(ctx, req) emits one
// grpc_calls edge per call site, with the dst Endpoint either resolving
// to a same-file server-registered Endpoint (real, language="go") or to
// an AMBIGUOUS placeholder (language="external") when no server matches.
//
// Source: testdata/distributed/grpc_client.go.
func TestGRPC_ClientCalls(t *testing.T) {
	root := "testdata/distributed"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	clientCalls := edgesByType(g.Edges, types.EdgeGRPCCalls)
	if len(clientCalls) < 4 {
		t.Errorf("grpc_calls count: got %d, want ≥ 4 "+
			"(GetUser + ListUsers + Echo + ExternalService.DoSomething; "+
			"CallGetUserVarForm adds a 5th)",
			len(clientCalls))
	}

	dstByQName := map[string]*types.Node{}
	for _, e := range clientCalls {
		dst := findNodeByID(g.Nodes, e.Dst)
		if dst == nil {
			t.Errorf("grpc_calls dst dangling: %+v", e)
			continue
		}
		dstByQName[dst.QualifiedName] = dst
		src := findNodeByID(g.Nodes, e.Src)
		if src == nil {
			t.Errorf("grpc_calls src dangling: %+v", e)
			continue
		}
		if src.Type != types.NodeFunction && src.Type != types.NodeMethod {
			t.Errorf("grpc_calls src is %s, want Function/Method", src.Type)
		}
	}

	wantHits := []string{
		"grpc:UserService.GetUser",
		"grpc:UserService.ListUsers",
		"grpc:EchoService.Echo",
		"grpc:ExternalService.DoSomething",
	}
	for _, q := range wantHits {
		if _, ok := dstByQName[q]; !ok {
			t.Errorf("grpc_calls missing target %q", q)
		}
	}

	// Cross-file (server in grpc_server.go, client in grpc_client.go) →
	// the client emits an AMBIGUOUS placeholder Endpoint (language="external")
	// because endpointNodeIDs dedup is per-declVisitor / per-file.
	// V0 W3b limitation: cross-file resolution to the real Endpoint is
	// deferred to a future linker pass. Both languages (server "go" Endpoint
	// AND client "external" placeholder) coexist in the graph; users see
	// the call site as AMBIGUOUS until the linker rewires.
	//
	// In addition to the placeholder, the real server-side Endpoint
	// (`grpc:UserService.GetUser`, language="go") must ALSO exist — verifying
	// the server-pass detector emits it independent of the client side.
	var serverEndpoint *types.Node
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != types.NodeEndpoint {
			continue
		}
		if n.QualifiedName == "grpc:UserService.GetUser" && n.Language == "go" {
			serverEndpoint = n
			break
		}
	}
	if serverEndpoint == nil {
		t.Error("server-side grpc:UserService.GetUser (language=go) Endpoint missing — " +
			"server pass did not emit it independent of the client side")
	} else if serverEndpoint.Confidence != types.ConfExtracted {
		t.Errorf("server-side grpc:UserService.GetUser confidence=%q, want EXTRACTED",
			serverEndpoint.Confidence)
	}

	// ExternalService has no server registration in the fixture → the
	// edge must point at an AMBIGUOUS placeholder Endpoint
	// (language="external").
	if dst, ok := dstByQName["grpc:ExternalService.DoSomething"]; ok {
		if dst.Language != "external" {
			t.Errorf("grpc:ExternalService.DoSomething dst language=%q, "+
				"want external (no server-side registration)", dst.Language)
		}
		if dst.Confidence != types.ConfAmbiguous {
			t.Errorf("ExternalService placeholder confidence=%q, want AMBIGUOUS",
				dst.Confidence)
		}
	}
}

// TestGRPC_ConfidenceSplit pins down §6.5 (C) — typesInfo path emits
// EXTRACTED, AST-only suffix-matcher emits INFERRED. The fixture is
// loaded with packages.Load so typesInfo is available, exercising the
// EXTRACTED branch for both server (interface satisfaction) and client
// (interface underlying check).
func TestGRPC_ConfidenceSplit(t *testing.T) {
	root := "testdata/distributed"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	listensOn := edgesByType(g.Edges, types.EdgeGRPCListensOn)
	for _, e := range listensOn {
		// Server-side via typesInfo → EXTRACTED.
		if e.Confidence != types.ConfExtracted {
			dst := findNodeByID(g.Nodes, e.Dst)
			name := "<dangling>"
			if dst != nil {
				name = dst.QualifiedName
			}
			t.Errorf("grpc_listens_on edge → %s confidence=%q, "+
				"want EXTRACTED (typesInfo-aware server pass)",
				name, e.Confidence)
		}
	}
	clientCalls := edgesByType(g.Edges, types.EdgeGRPCCalls)
	for _, e := range clientCalls {
		dst := findNodeByID(g.Nodes, e.Dst)
		if dst == nil {
			continue
		}
		// External-service placeholder edges keep INFERRED confidence
		// (classifyGRPCClientCall fails for external because typesInfo
		// resolves the receiver to ExternalServiceClient which IS an
		// interface in our stubs → also EXTRACTED). We accept either
		// EXTRACTED or INFERRED on the edge, just not AMBIGUOUS (the
		// Endpoint can be AMBIGUOUS without the edge being so).
		if e.Confidence == types.ConfAmbiguous {
			t.Errorf("grpc_calls edge → %s confidence=AMBIGUOUS, "+
				"want EXTRACTED or INFERRED on the edge "+
				"(Endpoint may still be AMBIGUOUS placeholder)",
				dst.QualifiedName)
		}
	}
}

// TestGRPC_NoFalsePositive_FakeClient ensures user-defined types whose
// name matches the gRPC client convention (`<Svc>Client`) but whose
// underlying kind is NOT *Interface do not produce grpc_calls edges.
// The fixture's FakeClient (rpc_client.go) is a struct with a Call
// method — without the interface-underlying guard, the W3b detector
// would emit a `grpc:Fake.Call` edge.
func TestGRPC_NoFalsePositive_FakeClient(t *testing.T) {
	root := "testdata/distributed"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	for _, n := range g.Nodes {
		if n.Type != types.NodeEndpoint {
			continue
		}
		if strings.HasPrefix(n.QualifiedName, "grpc:Fake") {
			t.Errorf("FakeClient (non-interface) leaked into grpc Endpoint: %q",
				n.QualifiedName)
		}
	}
}

// TestGRPC_RegisterServerServiceName_Heuristic exercises the AST-level
// name-matching rules for RegisterXXXServer in isolation, ensuring the
// false-positive surface (`RegisterMux`, `Register`, etc.) doesn't
// accidentally match.
func TestGRPC_RegisterServerServiceName_Heuristic(t *testing.T) {
	root := "testdata/distributed"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	// The fixture registers UserService + EchoService. No edge should
	// have a dst named just "grpc:Server" or "grpc:.<anything>".
	for _, e := range edgesByType(g.Edges, types.EdgeGRPCListensOn) {
		dst := findNodeByID(g.Nodes, e.Dst)
		if dst == nil {
			continue
		}
		if dst.QualifiedName == "grpc:Server" ||
			dst.QualifiedName == "grpc:." ||
			!strings.HasPrefix(dst.QualifiedName, "grpc:") {
			t.Errorf("malformed gRPC Endpoint qname: %q", dst.QualifiedName)
		}
	}
}

// TestGRPC_TableDriven_NameExtraction covers the heuristic name extractors
// (registerServerServiceName, clientTypeServiceName, callTargetNewXClient)
// indirectly through the fixture surface. Direct unit tests on the
// unexported helpers would require an internal-test file; the table here
// validates the externally observable surface (which qnames show up in
// the graph) given controlled fixture call sites.
func TestGRPC_TableDriven_NameExtraction(t *testing.T) {
	root := "testdata/distributed"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	endpoints := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Type == types.NodeEndpoint && strings.HasPrefix(n.QualifiedName, "grpc:") {
			endpoints[n.QualifiedName] = true
		}
	}
	cases := []struct {
		name    string
		qname   string
		wantHit bool
		reason  string
	}{
		{"UserService.GetUser", "grpc:UserService.GetUser", true,
			"server registers UserService + client calls GetUser"},
		{"UserService.ListUsers", "grpc:UserService.ListUsers", true,
			"second method on same service"},
		{"EchoService.Echo", "grpc:EchoService.Echo", true,
			"second service in same file"},
		{"ExternalService.DoSomething", "grpc:ExternalService.DoSomething", true,
			"client-only — placeholder Endpoint expected"},
		{"Fake.Call (FakeClient struct)", "grpc:Fake.Call", false,
			"FakeClient is a struct, not an interface — must be filtered"},
		{"UserService.helperMethod (unexported)", "grpc:UserService.helperMethod", false,
			"unexported method must not emit grpc_listens_on"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := endpoints[tc.qname]
			if got != tc.wantHit {
				t.Errorf("endpoint %q presence = %v, want %v (reason: %s)",
					tc.qname, got, tc.wantHit, tc.reason)
			}
		})
	}
}
