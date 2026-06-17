package typescript

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// declVisitor walks tree-sitter query matches and emits Pass 1 nodes/edges.
type declVisitor struct {
	rel     string
	src     []byte
	lang    *sitter.Language
	root    *sitter.Node
	fileID  string
	nodes   []types.Node
	edges   []types.Edge
	pending []parse.PendingRef
	// endpointIDs deduplicates NodeEndpoint emissions inside a single file
	// (schema 1.9 W1). Key = endpoint qname (`http:METHOD /route`),
	// value = node ID. Initialised lazily by runDistributed.
	endpointIDs map[string]string
	// httpClientPlaceholderIDs maps an HTTP-client target qname to the
	// AMBIGUOUS placeholder Endpoint ID emitted in http_client.go.
	// Separate from endpointIDs because placeholder Endpoints use
	// Language="external" — they live in a distinct ID space until the
	// link pass (internal/link/http_match.go) resolves them (W2,
	// schema 1.9 §6.3 (B), §6.9).
	httpClientPlaceholderIDs map[string]string
	// grpcClientStubsTS maps a local stub variable name to the gRPC
	// service name it was bound to via `new XxxClient(...)` /
	// `createPromiseClient(Svc, ...)` / `createClient(Svc, ...)`.
	// Populated by collectGRPCClientStubs (W3c, schema 1.9 §3.4).
	// Flat per-file scope — V0 doesn't model nested-scope shadowing.
	grpcClientStubsTS map[string]string
	// grpcClientPlaceholderIDsTS maps a gRPC-client target qname
	// (`grpc:Service.Method`) to the AMBIGUOUS placeholder Endpoint
	// emitted in grpc_client.go. Distinct ID space (Language="external")
	// — same shape as the Go-side grpcClientPlaceholderIDs so a future
	// cross-language linker pass can reuse the same suffix matcher
	// (schema 1.9 §3.4, §6.5).
	grpcClientPlaceholderIDsTS map[string]string
	// tsGRPCImportPresent caches whether this file imports any known
	// gRPC client library. Pattern A (`new <Svc>Client(host)`) emits
	// gRPC stubs only when this is true — without the gating, common
	// non-gRPC clients (RedisClient/PrismaClient/HttpClient/ApolloClient
	// /S3Client/MongoClient/KafkaClient/ApiClient) would all produce
	// AMBIGUOUS placeholders. W3c review Important #1 (2026-05-11).
	// Set once in runGRPCClients() via fileHasGRPCImport().
	tsGRPCImportPresent bool
}

func newDeclVisitor(rel string, src []byte, lang *sitter.Language, root *sitter.Node) *declVisitor {
	v := &declVisitor{rel: rel, src: src, lang: lang, root: root}
	fileQ := "file:" + rel
	v.fileID = makeID(fileQ, "ts", 0)
	v.nodes = append(v.nodes, types.Node{
		ID: v.fileID, Type: types.NodeFile, Name: rel, QualifiedName: fileQ,
		FilePath: rel, StartLine: 1, EndLine: 1,
		Language: "ts", Confidence: types.ConfExtracted,
	})
	return v
}

func (v *declVisitor) visit() {
	v.runQuery(queryClass, types.NodeClass)
	v.runQuery(queryInterface, types.NodeInterface)
	v.runQuery(queryFunction, types.NodeFunction)
	v.runQuery(queryMethod, types.NodeMethod)
	v.runQuery(queryTypeAlias, types.NodeTypeAlias)
	v.runQuery(queryEnum, types.NodeEnum)
	v.runQuery(queryDecorator, types.NodeDecorator)
	v.runImports()
	// W-B W1 (schema 1.10 slots, 2026-05-11): emit extends / implements
	// PendingRefs from class_heritage / extends_type_clause subtrees.
	// Must run after queryClass / queryInterface so the SrcID anchors
	// (computed independently here from the same name-identifier
	// startByte) match the actual Class / Interface node IDs in v.nodes.
	// See heritage.go for the full shape spec.
	v.runHeritage()
	// Walk function/method bodies for statement-level nodes (IfStmt,
	// LoopStmt, SwitchStmt, ReturnStmt, CallSite) + emit cross-file
	// PendingRefs anchored on each CallSite. Runs after the declaration
	// queries so the Function/Method byte intervals (collected via
	// collectFnIntervalsFromTree) are populated for the enclosing-fn
	// lookup. Replaces the earlier P3 runBodyCalls — see
	// internal/parse/typescript/statements.go for the full schema.
	v.runBodyStatements()
	// W-B W2 (schema 1.10 slots, 2026-05-11): emit NodeAwaitPoint +
	// EdgeAwaits for every await_expression that lives inside a
	// Function/Method interval. Runs after runBodyStatements so any
	// CallSite/IfStmt/LoopStmt that might co-locate with an await is
	// already in v.nodes (ordering doesn't affect the await emit itself
	// — it anchors on intervals from v.nodes — but keeping the
	// statement-level passes contiguous makes the order easier to read).
	// See async.go for the full shape spec.
	v.runAsync()
	// W1 (schema 1.9): emit HTTP server Endpoint nodes + listens_on edges
	// for Express / Koa / Fastify / Hono / Next.js App Router patterns.
	// Runs after the declaration queries so Function/Method nodes are
	// already in v.nodes — listens_on resolution walks v.nodes to find
	// the handler.
	v.runDistributed()
	// W2 (schema 1.9): emit HTTP client placeholder Endpoints + http_calls
	// edges for fetch / axios / useSWR / useQuery patterns. The link pass
	// (internal/link/http_match.go) reconciles placeholders against real
	// server-side Endpoints from the same monorepo.
	v.runHTTPClients()
	// W3c (schema 1.9 §3.4): emit gRPC-web / Connect-web client
	// placeholder Endpoints + grpc_calls edges. AST-only — all edges
	// are INFERRED per §6.5 (c) because tree-sitter parses don't carry
	// typesInfo. Placeholder Endpoints share the qname shape
	// `grpc:Service.Method` with W3a/W3b so a future linker pass can
	// rewire by suffix match.
	v.runGRPCClients()
}

func (v *declVisitor) runQuery(q string, nt types.NodeType) {
	query, qErr := sitter.NewQuery(v.lang, q)
	if qErr != nil {
		return
	}
	defer func() { query.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		for _, c := range m.Captures {
			if names[c.Index] != "name" {
				continue
			}
			node := c.Node
			ident := node.Utf8Text(v.src)
			startByte := int(node.StartByte())
			endByte := int(node.EndByte())
			startLine := int(node.StartPosition().Row) + 1
			endLine := int(node.EndPosition().Row) + 1
			qname := ident
			if nt == types.NodeMethod {
				if className := nearestClassName(&node, v.src); className != "" {
					qname = className + "." + ident
				}
			}
			// W-B W2 (schema 1.10): tag async function-likes via SubKind so
			// graph consumers can distinguish suspension-bearing callables
			// without reaching back into the body walk. Limited to
			// NodeFunction / NodeMethod — class/interface/etc. have no
			// async modifier. Arrow functions don't appear here (they
			// aren't captured by queryFunction); their await points still
			// get emitted in runAsync, but the SubKind on the arrow's
			// surrounding declaration is unaffected by the inner arrow's
			// asyncness — same idiom as Go's mutex SubKind on a wrapping
			// struct vs the field that holds the lock.
			subKind := ""
			if nt == types.NodeFunction || nt == types.NodeMethod {
				if isAsyncFunctionLike(&node) {
					subKind = "async"
				}
			}
			id := makeID(qname, "ts", startByte)
			v.nodes = append(v.nodes, types.Node{
				ID: id, Type: nt, Name: ident, QualifiedName: qname,
				// canonical id (ADR-0001): no import path in TS, so the
				// relative file path is the qualifier — <relpath>:<qname>.
				CanonicalID: v.rel + ":" + qname,
				FilePath:    v.rel, StartLine: startLine, EndLine: endLine,
				StartByte: startByte, EndByte: endByte,
				Language: "ts", Confidence: types.ConfExtracted, SubKind: subKind,
			})
			v.edges = append(v.edges, types.Edge{
				Src: v.fileID, Dst: id, Type: types.EdgeDefines,
				Count: 1, Confidence: types.ConfExtracted,
			})
		}
	}
}

func (v *declVisitor) runImports() {
	query, qErr := sitter.NewQuery(v.lang, queryImport)
	if qErr != nil {
		return
	}
	defer func() { query.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		for _, c := range m.Captures {
			if names[c.Index] != "path" {
				continue
			}
			node := c.Node
			path := trimQuotes(node.Utf8Text(v.src))
			qname := "import:" + path
			startByte := int(node.StartByte())
			endByte := int(node.EndByte())
			id := makeID(qname, "ts", startByte)
			v.nodes = append(v.nodes, types.Node{
				ID: id, Type: types.NodeImport, Name: path, QualifiedName: qname,
				FilePath: v.rel, StartLine: int(node.StartPosition().Row) + 1,
				EndLine:   int(node.EndPosition().Row) + 1,
				StartByte: startByte, EndByte: endByte,
				Language: "ts", Confidence: types.ConfExtracted,
			})
			v.edges = append(v.edges, types.Edge{
				Src: v.fileID, Dst: id, Type: types.EdgeImports,
				Count: 1, Confidence: types.ConfExtracted,
			})
		}
	}
}

// nearestClassName walks the parent chain looking for an enclosing
// class_declaration and returns its name (empty if none).
func nearestClassName(n *sitter.Node, src []byte) string {
	for cur := n; cur != nil; cur = cur.Parent() {
		if cur.Kind() == "class_declaration" {
			id := cur.ChildByFieldName("name")
			if id != nil {
				return id.Utf8Text(src)
			}
		}
	}
	return ""
}

func trimQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'' || s[0] == '`') {
		return s[1 : len(s)-1]
	}
	return s
}

// makeID is a thin wrapper over the shared parse.MakeID, kept local for
// ergonomic call sites within this package.
func makeID(qname, lang string, startByte int) string {
	return parse.MakeID(qname, lang, startByte)
}
