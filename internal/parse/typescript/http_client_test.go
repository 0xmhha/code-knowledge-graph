package typescript_test

import (
	"os"
	"testing"

	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestTSHTTPClient_DetectsKnownPatterns asserts the W2 client detector emits
// AMBIGUOUS placeholder Endpoints + http_calls edges for the canonical
// patterns in testdata/distributed/clients.ts.
//
// Verifies (per schema-1.9-spec §6.3 (B), §6.9):
//
//   - Each pattern yields one placeholder Endpoint with Language="external",
//     Confidence=AMBIGUOUS, SubKind="http".
//   - Each call site emits one http_calls edge from the enclosing Function
//     to the placeholder.
//   - Same (method, path) tuple dedups to one placeholder Endpoint.
//   - Absolute URLs are stripped to just the path portion.
func TestTSHTTPClient_DetectsKnownPatterns(t *testing.T) {
	path := "testdata/distributed/clients.ts"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	p := tsp.New(".")
	r, err := p.ParseFile(path, src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Collect placeholder Endpoints.
	placeholders := map[string]types.Node{}
	for _, n := range r.Nodes {
		if n.Type != types.NodeEndpoint {
			continue
		}
		if n.Language != "external" {
			continue
		}
		if n.SubKind != "http" {
			t.Errorf("placeholder Endpoint %q sub_kind=%q, want http",
				n.QualifiedName, n.SubKind)
		}
		if n.Confidence != types.ConfAmbiguous {
			t.Errorf("placeholder Endpoint %q confidence=%q, want AMBIGUOUS",
				n.QualifiedName, n.Confidence)
		}
		placeholders[n.QualifiedName] = n
	}

	wantQnames := []string{
		"http:GET /api/users",
		"http:POST /api/users",
		"http:PUT /api/users/:id",
		"http:DELETE /api/users/:id",
		"http:GET /api/users/all",
		"http:* /api/list",
		"http:* /api/query",
		"http:GET /external/foo",
	}
	for _, want := range wantQnames {
		if _, ok := placeholders[want]; !ok {
			t.Errorf("missing placeholder Endpoint %q (saw %v)", want, keysOfNodeMap(placeholders))
		}
	}

	// Count http_calls edges. Each call site should produce one.
	httpCallsCount := 0
	for _, e := range r.Edges {
		if e.Type == types.EdgeHTTPCalls {
			httpCallsCount++
		}
	}
	// Fixture has 9 client call sites; each emits its own http_calls edge
	// even when multiple sites dedup to the same placeholder Endpoint.
	if httpCallsCount < 9 {
		t.Errorf("http_calls edge count: got %d, want ≥ 9", httpCallsCount)
	}

	// Each http_calls edge must point at a placeholder Endpoint.
	placeholderIDs := map[string]bool{}
	for _, n := range placeholders {
		placeholderIDs[n.ID] = true
	}
	for _, e := range r.Edges {
		if e.Type != types.EdgeHTTPCalls {
			continue
		}
		if !placeholderIDs[e.Dst] {
			t.Errorf("http_calls edge dst=%q not a placeholder Endpoint", e.Dst)
		}
	}
}

// TestTSHTTPClient_NoMatchOnNonClient confirms that arbitrary member calls
// don't emit http_calls — `obj.get('x')` should not trigger axios-style
// detection unless the receiver name starts with "axios".
func TestTSHTTPClient_NoMatchOnNonClient(t *testing.T) {
	src := []byte(`
		import { Map } from 'immutable';
		export function example(m: any): unknown {
		  return m.get('/some/key');
		}
	`)
	p := tsp.New(".")
	r, err := p.ParseFile("nonclient.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, e := range r.Edges {
		if e.Type == types.EdgeHTTPCalls {
			t.Errorf("unexpected http_calls edge on non-client member call: %+v", e)
		}
	}
	for _, n := range r.Nodes {
		if n.Type == types.NodeEndpoint && n.Language == "external" {
			t.Errorf("unexpected placeholder Endpoint on non-client call: %+v", n)
		}
	}
}

func keysOfNodeMap(m map[string]types.Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
