package link_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/link"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestMatchHTTPClients_SpecificVerbHit confirms the §6.9 stage-1 path:
// client emits `http:POST /api/users`, server has the matching exact qname,
// matcher rewires the http_calls edge to the real Endpoint and drops the
// placeholder.
func TestMatchHTTPClients_SpecificVerbHit(t *testing.T) {
	clientFunc := node("client-fn", types.NodeFunction, "go", "pkg.Caller", types.ConfExtracted, "")
	placeholder := node("ph-1", types.NodeEndpoint, "external", "http:POST /api/users", types.ConfAmbiguous, "http")
	realEndpoint := node("real-1", types.NodeEndpoint, "go", "http:POST /api/users", types.ConfExtracted, "http")
	edges := []types.Edge{
		{Src: clientFunc.ID, Dst: placeholder.ID, Type: types.EdgeHTTPCalls, Confidence: types.ConfInferred, Count: 1},
		{Src: "file-1", Dst: placeholder.ID, Type: types.EdgeDefines, Confidence: types.ConfAmbiguous, Count: 1},
	}
	nodes, newEdges, result := link.MatchHTTPClients(
		[]types.Node{clientFunc, placeholder, realEndpoint},
		edges,
	)
	if result.Rewired != 1 || result.SpecificHits != 1 || result.WildcardHits != 0 {
		t.Errorf("counts: rewired=%d specific=%d wildcard=%d; want rewired=1 specific=1 wildcard=0",
			result.Rewired, result.SpecificHits, result.WildcardHits)
	}
	if result.PlaceholdersDropped != 1 {
		t.Errorf("placeholders_dropped=%d, want 1", result.PlaceholdersDropped)
	}
	// Placeholder node and its defines edge are gone.
	for _, n := range nodes {
		if n.ID == placeholder.ID {
			t.Errorf("placeholder node should have been dropped: %+v", n)
		}
	}
	// http_calls edge dst rewired.
	var found bool
	for _, e := range newEdges {
		if e.Type == types.EdgeHTTPCalls && e.Src == clientFunc.ID {
			if e.Dst != realEndpoint.ID {
				t.Errorf("http_calls dst=%q, want %q", e.Dst, realEndpoint.ID)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("http_calls edge missing after rewire")
	}
}

// TestMatchHTTPClients_WildcardCascade confirms stage-2: client emits
// `http:GET /x`, no matching specific verb exists, but `http:* /x` does —
// matcher uses the wildcard fallback.
func TestMatchHTTPClients_WildcardCascade(t *testing.T) {
	clientFunc := node("client-fn", types.NodeFunction, "ts", "Caller", types.ConfExtracted, "")
	placeholder := node("ph-1", types.NodeEndpoint, "external", "http:GET /x", types.ConfAmbiguous, "http")
	wildcardServer := node("real-1", types.NodeEndpoint, "go", "http:* /x", types.ConfExtracted, "http")
	edges := []types.Edge{
		{Src: clientFunc.ID, Dst: placeholder.ID, Type: types.EdgeHTTPCalls, Confidence: types.ConfInferred, Count: 1},
		{Src: "file-1", Dst: placeholder.ID, Type: types.EdgeDefines, Confidence: types.ConfAmbiguous, Count: 1},
	}
	_, newEdges, result := link.MatchHTTPClients(
		[]types.Node{clientFunc, placeholder, wildcardServer},
		edges,
	)
	if result.Rewired != 1 || result.SpecificHits != 0 || result.WildcardHits != 1 {
		t.Errorf("counts: rewired=%d specific=%d wildcard=%d; want rewired=1 specific=0 wildcard=1",
			result.Rewired, result.SpecificHits, result.WildcardHits)
	}
	for _, e := range newEdges {
		if e.Type == types.EdgeHTTPCalls && e.Src == clientFunc.ID && e.Dst != wildcardServer.ID {
			t.Errorf("http_calls dst=%q, want %q", e.Dst, wildcardServer.ID)
		}
	}
}

// TestMatchHTTPClients_SpecificBeforeWildcard confirms cascade order: when
// BOTH `http:GET /x` and `http:* /x` exist, the matcher picks the specific one.
func TestMatchHTTPClients_SpecificBeforeWildcard(t *testing.T) {
	clientFunc := node("client-fn", types.NodeFunction, "ts", "Caller", types.ConfExtracted, "")
	placeholder := node("ph-1", types.NodeEndpoint, "external", "http:GET /x", types.ConfAmbiguous, "http")
	specificServer := node("real-1", types.NodeEndpoint, "go", "http:GET /x", types.ConfExtracted, "http")
	wildcardServer := node("real-2", types.NodeEndpoint, "go", "http:* /x", types.ConfExtracted, "http")
	edges := []types.Edge{
		{Src: clientFunc.ID, Dst: placeholder.ID, Type: types.EdgeHTTPCalls, Confidence: types.ConfInferred, Count: 1},
		{Src: "file-1", Dst: placeholder.ID, Type: types.EdgeDefines, Confidence: types.ConfAmbiguous, Count: 1},
	}
	_, newEdges, result := link.MatchHTTPClients(
		[]types.Node{clientFunc, placeholder, specificServer, wildcardServer},
		edges,
	)
	if result.SpecificHits != 1 || result.WildcardHits != 0 {
		t.Errorf("cascade order broken: specific=%d wildcard=%d; want specific=1 wildcard=0",
			result.SpecificHits, result.WildcardHits)
	}
	for _, e := range newEdges {
		if e.Type == types.EdgeHTTPCalls && e.Src == clientFunc.ID && e.Dst != specificServer.ID {
			t.Errorf("http_calls dst=%q, want specific server %q", e.Dst, specificServer.ID)
		}
	}
}

// TestMatchHTTPClients_WildcardClient confirms that a client whose method is
// "*" (e.g. useSWR — verb unknown) skips stage 1 and matches stage 2 directly
// when a wildcard server exists.
func TestMatchHTTPClients_WildcardClient(t *testing.T) {
	clientFunc := node("client-fn", types.NodeFunction, "ts", "Caller", types.ConfExtracted, "")
	placeholder := node("ph-1", types.NodeEndpoint, "external", "http:* /list", types.ConfAmbiguous, "http")
	wildcardServer := node("real-1", types.NodeEndpoint, "go", "http:* /list", types.ConfExtracted, "http")
	specificServer := node("real-2", types.NodeEndpoint, "go", "http:GET /list", types.ConfExtracted, "http")
	edges := []types.Edge{
		{Src: clientFunc.ID, Dst: placeholder.ID, Type: types.EdgeHTTPCalls, Confidence: types.ConfInferred, Count: 1},
	}
	_, newEdges, result := link.MatchHTTPClients(
		[]types.Node{clientFunc, placeholder, wildcardServer, specificServer},
		edges,
	)
	// Client method="*" — matcher's specific-verb lookup is skipped (we
	// don't know which specific verb to look up). Stage 2 hits.
	if result.WildcardHits != 1 || result.SpecificHits != 0 {
		t.Errorf("counts: specific=%d wildcard=%d; want specific=0 wildcard=1",
			result.SpecificHits, result.WildcardHits)
	}
	for _, e := range newEdges {
		if e.Type == types.EdgeHTTPCalls && e.Src == clientFunc.ID && e.Dst != wildcardServer.ID {
			t.Errorf("http_calls dst=%q, want wildcard server %q",
				e.Dst, wildcardServer.ID)
		}
	}
}

// TestMatchHTTPClients_AmbiguousRetained confirms that an unmatched client
// keeps its placeholder Endpoint + http_calls edge (§6.3 (B)).
func TestMatchHTTPClients_AmbiguousRetained(t *testing.T) {
	clientFunc := node("client-fn", types.NodeFunction, "ts", "Caller", types.ConfExtracted, "")
	placeholder := node("ph-1", types.NodeEndpoint, "external", "http:GET /nowhere", types.ConfAmbiguous, "http")
	// Unrelated server — different path, must not match.
	unrelated := node("real-1", types.NodeEndpoint, "go", "http:GET /other", types.ConfExtracted, "http")
	edges := []types.Edge{
		{Src: clientFunc.ID, Dst: placeholder.ID, Type: types.EdgeHTTPCalls, Confidence: types.ConfInferred, Count: 1},
		{Src: "file-1", Dst: placeholder.ID, Type: types.EdgeDefines, Confidence: types.ConfAmbiguous, Count: 1},
	}
	nodes, newEdges, result := link.MatchHTTPClients(
		[]types.Node{clientFunc, placeholder, unrelated},
		edges,
	)
	if result.AmbiguousRetained != 1 || result.Rewired != 0 || result.PlaceholdersDropped != 0 {
		t.Errorf("counts: retained=%d rewired=%d dropped=%d; want retained=1 rewired=0 dropped=0",
			result.AmbiguousRetained, result.Rewired, result.PlaceholdersDropped)
	}
	// Placeholder is still in nodes.
	var foundPlaceholder bool
	for _, n := range nodes {
		if n.ID == placeholder.ID {
			foundPlaceholder = true
		}
	}
	if !foundPlaceholder {
		t.Errorf("AMBIGUOUS placeholder should have been retained")
	}
	// http_calls edge still points at the placeholder.
	for _, e := range newEdges {
		if e.Type == types.EdgeHTTPCalls && e.Dst != placeholder.ID {
			t.Errorf("http_calls dst=%q, want unchanged placeholder %q", e.Dst, placeholder.ID)
		}
	}
}

// TestMatchHTTPClients_ExactNotSuffix confirms the §3.3 exact-match decision:
// client `/api/users` MUST NOT match server `/foo/api/users` (different
// prefix, suffix would otherwise spuriously match).
func TestMatchHTTPClients_ExactNotSuffix(t *testing.T) {
	clientFunc := node("client-fn", types.NodeFunction, "ts", "Caller", types.ConfExtracted, "")
	placeholder := node("ph-1", types.NodeEndpoint, "external", "http:GET /api/users", types.ConfAmbiguous, "http")
	differentPrefix := node("real-1", types.NodeEndpoint, "go", "http:GET /foo/api/users", types.ConfExtracted, "http")
	edges := []types.Edge{
		{Src: clientFunc.ID, Dst: placeholder.ID, Type: types.EdgeHTTPCalls, Confidence: types.ConfInferred, Count: 1},
	}
	_, _, result := link.MatchHTTPClients(
		[]types.Node{clientFunc, placeholder, differentPrefix},
		edges,
	)
	if result.Rewired != 0 || result.AmbiguousRetained != 1 {
		t.Errorf("suffix-match false positive: rewired=%d retained=%d; want rewired=0 retained=1",
			result.Rewired, result.AmbiguousRetained)
	}
}

// TestMatchHTTPClients_TSClientToGoServer is the 4-permutation acceptance
// criterion (schema-1.9-spec §7 W2) for TS→Go.
func TestMatchHTTPClients_TSClientToGoServer(t *testing.T) {
	tsClient := node("ts-fn", types.NodeFunction, "ts", "Caller", types.ConfExtracted, "")
	tsPlaceholder := node("ph-1", types.NodeEndpoint, "external", "http:POST /api/users", types.ConfAmbiguous, "http")
	goServer := node("go-ep", types.NodeEndpoint, "go", "http:POST /api/users", types.ConfExtracted, "http")
	goHandler := node("go-fn", types.NodeMethod, "go", "pkg.handleCreateUser", types.ConfExtracted, "")
	edges := []types.Edge{
		{Src: tsClient.ID, Dst: tsPlaceholder.ID, Type: types.EdgeHTTPCalls, Confidence: types.ConfInferred, Count: 1},
		{Src: goHandler.ID, Dst: goServer.ID, Type: types.EdgeListensOn, Confidence: types.ConfExtracted, Count: 1},
	}
	_, newEdges, result := link.MatchHTTPClients(
		[]types.Node{tsClient, tsPlaceholder, goServer, goHandler},
		edges,
	)
	if result.Rewired != 1 {
		t.Fatalf("TS→Go: rewired=%d, want 1", result.Rewired)
	}
	// Confirm both listens_on (Go handler → Endpoint) and http_calls
	// (TS client → Endpoint) survive and converge on the same Endpoint.
	var hasListensOn, hasHTTPCalls bool
	for _, e := range newEdges {
		if e.Type == types.EdgeListensOn && e.Src == goHandler.ID && e.Dst == goServer.ID {
			hasListensOn = true
		}
		if e.Type == types.EdgeHTTPCalls && e.Src == tsClient.ID && e.Dst == goServer.ID {
			hasHTTPCalls = true
		}
	}
	if !hasListensOn || !hasHTTPCalls {
		t.Errorf("convergence broken: listens_on=%v http_calls=%v", hasListensOn, hasHTTPCalls)
	}
}

// TestMatchHTTPClients_GoClientToTSServer covers the reverse permutation.
func TestMatchHTTPClients_GoClientToTSServer(t *testing.T) {
	goClient := node("go-fn", types.NodeFunction, "go", "pkg.Caller", types.ConfExtracted, "")
	goPlaceholder := node("ph-1", types.NodeEndpoint, "external", "http:GET /api/health", types.ConfAmbiguous, "http")
	tsServer := node("ts-ep", types.NodeEndpoint, "ts", "http:GET /api/health", types.ConfExtracted, "http")
	tsHandler := node("ts-fn", types.NodeFunction, "ts", "GET", types.ConfExtracted, "")
	edges := []types.Edge{
		{Src: goClient.ID, Dst: goPlaceholder.ID, Type: types.EdgeHTTPCalls, Confidence: types.ConfInferred, Count: 1},
		{Src: tsHandler.ID, Dst: tsServer.ID, Type: types.EdgeListensOn, Confidence: types.ConfExtracted, Count: 1},
	}
	_, newEdges, result := link.MatchHTTPClients(
		[]types.Node{goClient, goPlaceholder, tsServer, tsHandler},
		edges,
	)
	if result.Rewired != 1 {
		t.Fatalf("Go→TS: rewired=%d, want 1", result.Rewired)
	}
	for _, e := range newEdges {
		if e.Type == types.EdgeHTTPCalls && e.Src == goClient.ID && e.Dst != tsServer.ID {
			t.Errorf("Go→TS dst=%q, want ts server %q", e.Dst, tsServer.ID)
		}
	}
}

// node is a terse constructor used by the test cases.
func node(id string, t types.NodeType, lang, qname string, conf types.Confidence, subKind string) types.Node {
	return types.Node{
		ID: id, Type: t, Language: lang, QualifiedName: qname,
		Confidence: conf, SubKind: subKind, Name: qname,
	}
}
