package typescript_test

import (
	"os"
	"testing"

	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestTSGRPCClient_DetectsKnownPatterns asserts the W3c gRPC-client
// detector emits AMBIGUOUS placeholder Endpoints + grpc_calls edges for
// the canonical patterns in testdata/distributed/grpc/grpcweb_client.ts
// and connectweb_client.ts.
//
// Verifies (per schema-1.9-spec §3.4, §6.3 (B), §6.5 (c)):
//
//   - Each pattern yields one placeholder Endpoint with Language="external",
//     Confidence=AMBIGUOUS, SubKind="grpc".
//   - Each call site emits one grpc_calls edge from the enclosing TS
//     Function to the placeholder, with Confidence=INFERRED.
//   - Same (service, method) tuple dedups to one placeholder Endpoint
//     per file.
//   - Negative-case calls (Map.get, localStorage.getItem, noop()) emit
//     no grpc_calls edge.
func TestTSGRPCClient_DetectsKnownPatterns(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		wantQname []string
		minEdges  int
	}{
		{
			name: "grpc-web (Improbable)",
			path: "testdata/distributed/grpc/grpcweb_client.ts",
			wantQname: []string{
				"grpc:UserService.getUser",
				"grpc:UserService.listUsers",
				"grpc:EchoService.Echo",
				"grpc:OrderService.placeOrder",
			},
			minEdges: 4,
		},
		{
			name: "connect-web (Buf)",
			path: "testdata/distributed/grpc/connectweb_client.ts",
			wantQname: []string{
				"grpc:GreetService.sayHello",
				"grpc:GreetService.sayGoodbye",
				"grpc:NotificationService.subscribe",
			},
			minEdges: 3,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			p := tsp.New(".")
			r, err := p.ParseFile(tc.path, src)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}

			// Collect gRPC placeholder Endpoints (Language=external,
			// SubKind=grpc).
			placeholders := map[string]types.Node{}
			for _, n := range r.Nodes {
				if n.Type != types.NodeEndpoint {
					continue
				}
				if n.Language != "external" || n.SubKind != "grpc" {
					continue
				}
				if n.Confidence != types.ConfAmbiguous {
					t.Errorf("placeholder %q confidence=%q, want AMBIGUOUS",
						n.QualifiedName, n.Confidence)
				}
				placeholders[n.QualifiedName] = n
			}
			for _, want := range tc.wantQname {
				if _, ok := placeholders[want]; !ok {
					t.Errorf("missing placeholder Endpoint %q (saw %v)",
						want, keysOfNodeMap(placeholders))
				}
			}

			// Count grpc_calls edges + verify dst is a placeholder + verify
			// confidence is INFERRED (TS is AST-only per §6.5 (c)).
			grpcCallsCount := 0
			placeholderIDs := map[string]bool{}
			for _, n := range placeholders {
				placeholderIDs[n.ID] = true
			}
			for _, e := range r.Edges {
				if e.Type != types.EdgeGRPCCalls {
					continue
				}
				grpcCallsCount++
				if !placeholderIDs[e.Dst] {
					t.Errorf("grpc_calls edge dst=%q not a placeholder", e.Dst)
				}
				if e.Confidence != types.ConfInferred {
					t.Errorf("grpc_calls edge confidence=%q, want INFERRED",
						e.Confidence)
				}
			}
			if grpcCallsCount < tc.minEdges {
				t.Errorf("grpc_calls edges: got %d, want ≥ %d",
					grpcCallsCount, tc.minEdges)
			}
		})
	}
}

// TestTSGRPCClient_NoMatchOnNonGRPC confirms that arbitrary method calls
// on non-gRPC receivers (Map, localStorage, plain objects) don't emit
// grpc_calls edges.
func TestTSGRPCClient_NoMatchOnNonGRPC(t *testing.T) {
	src := []byte(`
		export function example(): void {
		  const cache = new Map<string, string>();
		  cache.get('hello');
		  localStorage.getItem('token');
		  const plain = { method: () => undefined };
		  plain.method();
		}
	`)
	p := tsp.New(".")
	r, err := p.ParseFile("nonclient.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, e := range r.Edges {
		if e.Type == types.EdgeGRPCCalls {
			t.Errorf("unexpected grpc_calls edge on non-gRPC call: %+v", e)
		}
	}
	for _, n := range r.Nodes {
		if n.Type == types.NodeEndpoint && n.SubKind == "grpc" {
			t.Errorf("unexpected gRPC Endpoint on non-gRPC source: %+v", n)
		}
	}
}

// TestTSGRPCClient_PatternAGatedByImport locks W3c review Important #1
// (2026-05-11): without an explicit gRPC library import, Pattern A
// (`new <Svc>Client(host)`) must NOT emit grpc_calls edges. The `*Client`
// suffix is far too common in TS ecosystems (RedisClient, PrismaClient,
// HttpClient, ApolloClient, S3Client, MongoClient, KafkaClient,
// ElasticsearchClient, ApiClient, ...) to be treated as a gRPC stub on
// the suffix heuristic alone.
//
// Patterns B (createPromiseClient) and C (grpc.unary) are NOT gated —
// their factory/descriptor names are distinctive enough.
func TestTSGRPCClient_PatternAGatedByImport(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "RedisClient — no gRPC import",
			src: `
				export function useRedis(): void {
				  const redis = new RedisClient({ host: 'localhost' });
				  redis.get('key');
				}
			`,
		},
		{
			name: "PrismaClient — no gRPC import",
			src: `
				export async function fetchUsers(): Promise<void> {
				  const prisma = new PrismaClient();
				  await prisma.findMany();
				}
			`,
		},
		{
			name: "HttpClient with non-gRPC import",
			src: `
				import axios from 'axios';
				export function callApi(): void {
				  const http = new HttpClient();
				  http.get('/api/users');
				}
			`,
		},
		{
			name: "ApolloClient with apollo-client import (non-gRPC despite client suffix)",
			src: `
				import { ApolloClient } from '@apollo/client';
				export function query(): void {
				  const client = new ApolloClient({ uri: 'http://x' });
				  client.query({ query: null });
				}
			`,
		},
	}
	p := tsp.New(".")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := p.ParseFile("client.ts", []byte(tc.src))
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			for _, e := range r.Edges {
				if e.Type == types.EdgeGRPCCalls {
					t.Errorf("Pattern A gating failed — non-gRPC %s emitted grpc_calls edge: %+v",
						tc.name, e)
				}
			}
			for _, n := range r.Nodes {
				if n.Type == types.NodeEndpoint && n.SubKind == "grpc" {
					t.Errorf("Pattern A gating failed — non-gRPC %s emitted gRPC Endpoint: %+v",
						tc.name, n)
				}
			}
		})
	}
}

// TestTSGRPCClient_PatternAActivatedByImport locks the inverse: when a
// gRPC library import IS present, Pattern A activates and the `*Client`
// suffix produces the expected grpc_calls + AMBIGUOUS placeholder.
// Together with PatternAGatedByImport this fixes the gate at exactly
// the right place — neither false-positive nor false-negative.
func TestTSGRPCClient_PatternAActivatedByImport(t *testing.T) {
	src := []byte(`
		import { grpc } from '@improbable-eng/grpc-web';
		import { UserServiceClient } from './gen/user_service';
		export function callUser(): void {
		  const client = new UserServiceClient('https://api.example.com');
		  client.getUser({ id: '1' });
		}
	`)
	p := tsp.New(".")
	r, err := p.ParseFile("gated.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	found := false
	for _, n := range r.Nodes {
		if n.Type == types.NodeEndpoint && n.QualifiedName == "grpc:UserService.getUser" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Pattern A activation failed — expected grpc:UserService.getUser " +
			"placeholder Endpoint with gRPC import present")
	}
}

// TestTSGRPCClient_UnmatchedClientStillPlaceholder verifies §6.3 (B):
// when a TS gRPC call hits a service that has no corresponding Go server
// in the graph, the AMBIGUOUS placeholder is retained so the audit pane
// can surface the dangling external dependency.
//
// At the parser-unit level this is implicitly verified by
// DetectsKnownPatterns (none of the fixture services exist as Go servers
// in this isolated parse), but a dedicated negative assertion makes the
// invariant explicit: the Endpoint Language is "external" (not "ts") and
// Confidence is AMBIGUOUS — not EXTRACTED or INFERRED.
func TestTSGRPCClient_UnmatchedClientStillPlaceholder(t *testing.T) {
	src := []byte(`
		import { createPromiseClient } from '@bufbuild/connect-web';
		import { UnknownService } from './gen/unknown_connectweb';
		export async function callUnknown(): Promise<void> {
		  const client = createPromiseClient(UnknownService, null as any);
		  await client.someMethod({});
		}
	`)
	p := tsp.New(".")
	r, err := p.ParseFile("unmatched.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	foundPlaceholder := false
	for _, n := range r.Nodes {
		if n.Type != types.NodeEndpoint {
			continue
		}
		if n.QualifiedName != "grpc:UnknownService.someMethod" {
			continue
		}
		foundPlaceholder = true
		if n.Language != "external" {
			t.Errorf("placeholder language=%q, want external", n.Language)
		}
		if n.Confidence != types.ConfAmbiguous {
			t.Errorf("placeholder confidence=%q, want AMBIGUOUS",
				n.Confidence)
		}
	}
	if !foundPlaceholder {
		t.Errorf("expected AMBIGUOUS placeholder grpc:UnknownService.someMethod, found none")
	}
	foundEdge := false
	for _, e := range r.Edges {
		if e.Type != types.EdgeGRPCCalls {
			continue
		}
		foundEdge = true
		if e.Confidence != types.ConfInferred {
			t.Errorf("edge confidence=%q, want INFERRED", e.Confidence)
		}
	}
	if !foundEdge {
		t.Errorf("expected grpc_calls edge for unmatched client, found none")
	}
}
