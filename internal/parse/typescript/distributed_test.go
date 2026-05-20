package typescript_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestTSDistributed_FixtureMatrix is the W1 acceptance check (schema 1.9 spec
// §7 W1). Each fixture must yield at least one ts Endpoint + one listens_on
// edge. Routes declared twice in one file must collapse to one Endpoint.
// Computed routes (variable / concat) must be flagged INFERRED with a
// "<computed>" placeholder name so downstream UI can surface them distinctly.
func TestTSDistributed_FixtureMatrix(t *testing.T) {
	type want struct {
		file              string
		mustHaveEndpoints []string // qnames that MUST appear
		minListensOn      int
	}
	cases := []want{
		{
			file: "testdata/distributed/express.ts",
			mustHaveEndpoints: []string{
				"http:GET /users",
				"http:POST /users",
				"http:GET /admin",
				"http:DELETE /admin/:id",
			},
			minListensOn: 4,
		},
		{
			file: "testdata/distributed/fastify.ts",
			mustHaveEndpoints: []string{
				"http:GET /ping",
				"http:POST /items",
				"http:PUT /items/:id",
			},
			minListensOn: 3,
		},
		{
			file: "testdata/distributed/hono.ts",
			mustHaveEndpoints: []string{
				"http:GET /api/hello",
				"http:POST /api/hello",
				"http:GET /api/users/:id",
			},
			minListensOn: 3,
		},
		{
			file: "testdata/distributed/nextjs/app/api/users/route.ts",
			mustHaveEndpoints: []string{
				"http:GET /api/users",
				"http:POST /api/users",
			},
			minListensOn: 2,
		},
	}

	p := tsp.New(".")
	for _, c := range cases {
		t.Run(filepath.Base(c.file), func(t *testing.T) {
			src, err := os.ReadFile(c.file)
			if err != nil {
				t.Fatalf("read %s: %v", c.file, err)
			}
			r, err := p.ParseFile(c.file, src)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			// Build qname → node and count listens_on edges.
			byQname := map[string]types.Node{}
			for _, n := range r.Nodes {
				if n.Type != types.NodeEndpoint {
					continue
				}
				if n.Language != "ts" {
					t.Errorf("Endpoint %q has language=%q, want \"ts\"",
						n.QualifiedName, n.Language)
				}
				if n.SubKind != "http" {
					t.Errorf("Endpoint %q has sub_kind=%q, want \"http\"",
						n.QualifiedName, n.SubKind)
				}
				byQname[n.QualifiedName] = n
			}
			for _, qn := range c.mustHaveEndpoints {
				if _, ok := byQname[qn]; !ok {
					t.Errorf("missing Endpoint qname=%q in %s (saw %v)",
						qn, c.file, mapKeys(byQname))
				}
			}
			// Dedup invariant: same qname appears exactly once.
			if got, want := len(byQname), countDistinctEndpoints(r.Nodes); got != want {
				t.Errorf("Endpoint dedup leak: distinct qnames=%d, total nodes=%d", got, want)
			}
			// listens_on count.
			lo := 0
			endpointIDs := map[string]bool{}
			for _, n := range r.Nodes {
				if n.Type == types.NodeEndpoint {
					endpointIDs[n.ID] = true
				}
			}
			for _, e := range r.Edges {
				if e.Type == types.EdgeListensOn && endpointIDs[e.Dst] {
					lo++
				}
			}
			if lo < c.minListensOn {
				t.Errorf("listens_on count: got %d, want ≥ %d", lo, c.minListensOn)
			}
		})
	}
}

// TestTSDistributed_ComputedRoute_Inferred — the express.ts fixture declares
// `app.patch(dynRoute, ...)` where dynRoute is a variable. The detector must
// (a) still emit an Endpoint so the call site is visible, but (b) mark it
// INFERRED with a "<computed>" sentinel so consumers can filter it.
func TestTSDistributed_ComputedRoute_Inferred(t *testing.T) {
	src, err := os.ReadFile("testdata/distributed/express.ts")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	p := tsp.New(".")
	r, err := p.ParseFile("testdata/distributed/express.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	var hit *types.Node
	for i := range r.Nodes {
		n := &r.Nodes[i]
		if n.Type != types.NodeEndpoint {
			continue
		}
		if strings.Contains(n.QualifiedName, "<computed>") {
			hit = n
			break
		}
	}
	if hit == nil {
		t.Fatal("expected at least one Endpoint with <computed> route")
	}
	if hit.Confidence != types.ConfInferred {
		t.Errorf("computed-route Endpoint confidence=%q, want INFERRED", hit.Confidence)
	}
}

// TestTSDistributed_NextjsRouteFromPath — file-based routing means the
// Endpoint qname comes from the FILE PATH, not any literal in the source.
// Confirms the parser strips the `app/` prefix and `/route.ts` suffix.
func TestTSDistributed_NextjsRouteFromPath(t *testing.T) {
	path := "testdata/distributed/nextjs/app/api/users/route.ts"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	p := tsp.New(".")
	r, err := p.ParseFile(path, src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	seen := map[string]bool{}
	for _, n := range r.Nodes {
		if n.Type == types.NodeEndpoint {
			seen[n.QualifiedName] = true
		}
	}
	for _, want := range []string{"http:GET /api/users", "http:POST /api/users"} {
		if !seen[want] {
			t.Errorf("missing Next.js Endpoint %q (saw %v)", want, seen)
		}
	}
}

// TestTSDistributed_NamedFunctionExpression_Resolves — reviewer caught that
// named function expressions (`async function fooHandler(...) {...}`) used
// inline as a handler argument were collapsing to "<anonymous>" instead of
// resolving to their declared name. After the fix, only true anonymous arrow
// functions should produce "<anonymous>" Function nodes — every named
// function (whether top-level declaration or named function expression)
// must keep its identity for search / pagerank / impact downstream.
//
// The bound is "≤ arrow-handler count in the fixture": named function
// expressions must resolve (so they never contribute), but anonymous arrow
// handlers still synthesise. Identifier handlers (`app.get('/x', getUsers)`)
// resolve via declaration-pass IDs so they don't synthesise either.
func TestTSDistributed_NamedFunctionExpression_Resolves(t *testing.T) {
	cases := []struct {
		file             string
		wantAnonymousMax int // arrow-function handler count only
	}{
		{"testdata/distributed/express.ts", 2},                    // 2 arrow handlers (DELETE /admin/:id, PATCH dyn)
		{"testdata/distributed/fastify.ts", 0},                    // no arrow handlers
		{"testdata/distributed/hono.ts", 1},                       // 1 arrow handler (POST /api/hello)
		{"testdata/distributed/nextjs/app/api/users/route.ts", 0}, // file-based routing — no inline handlers
	}
	p := tsp.New(".")
	for _, c := range cases {
		t.Run(filepath.Base(c.file), func(t *testing.T) {
			src, err := os.ReadFile(c.file)
			if err != nil {
				t.Fatalf("read %s: %v", c.file, err)
			}
			r, err := p.ParseFile(c.file, src)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			anon := 0
			for _, n := range r.Nodes {
				if n.Type == types.NodeFunction && n.Name == "<anonymous>" {
					anon++
				}
			}
			if anon > c.wantAnonymousMax {
				t.Errorf("%s: %d <anonymous> Function nodes, want ≤ %d (arrow handlers only)",
					filepath.Base(c.file), anon, c.wantAnonymousMax)
			}
		})
	}
}

// helpers

func mapKeys(m map[string]types.Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func countDistinctEndpoints(nodes []types.Node) int {
	n := 0
	for _, x := range nodes {
		if x.Type == types.NodeEndpoint {
			n++
		}
	}
	return n
}
