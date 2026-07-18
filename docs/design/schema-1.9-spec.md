# CKG Schema 1.9 — Design Spec (cross-language interop expansion)

> 다음 schema bump의 design plan. schema 1.8 (Hunk-graph H1-H4 + §11.3
> hybrid)가 main에 안착한 시점에서 가장 큰 미커버 dimension은
> **cross-language interop edges**. 이 문서는 `docs/design/hunk-graph.md`
> 패턴 (사용자 §11 결정 → H 시리즈 dispatch) 따라 작성된 plan이며,
> §6의 8개 결정 항목에 사용자 답변을 받은 다음 W 시리즈 구현으로
> 진입한다.
>
> **Status** (updated 2026-07-18): **W1–W3 LANDED**, W4 PENDING.
> - W1 (TS HTTP server, reuses `NodeEndpoint`), W2 (`http_calls` — TS+Go HTTP
>   client), W3a/b/c (proto parser + `grpc_listens_on`/`grpc_calls`) are all
>   emitted (`internal/parse/{typescript,proto,golang}`, `internal/link/http_match.go`).
> - The "next schema bump = 1.9" framing is **superseded** — schema is now
>   **1.23** (`internal/buildpipe/cache.go`; history in `docs/SCHEMA.md`).
> - **W4 message-queue / pub-sub (Kafka/NATS/RabbitMQ/SQS + `Topic` node +
>   `publishes_to`/`consumes_from` edges) is NOT implemented** — no `NodeTopic`
>   / `EdgePublishesTo` in `pkg/types/enums.go`. This is the sole remaining
>   backlog item from this spec; tracked in `docs/CONTINUITY.md`.

---

## §0. Cold start (이 spec 처음 읽는 경우)

- **무엇**: schema 1.8 → 1.9 — Go ↔ TS ↔ Solidity 사이 cross-language
  edges를 확장. 현재는 `binds_to` (Sol↔TS, INFERRED) + Go
  `listens_on`/`handles_message`/`rpc_calls` (HTTP/JSON-RPC MVP)만
  emit하고 TS/Sol 쪽 endpoint detection은 비어 있음.
- **왜**: 멀티 언어 monorepo (web frontend + Go backend + Solidity
  contract)에서 *"이 TS API 호출이 Go 어디로 도착하는가?"* 같은 가장
  자연스러운 질의를 그래프 traversal로 답할 수 없음. 6 graph axis 중
  G5 Distributed가 자체 정의 (E3 Endpoint/MessageType + 3 edge types)
  대비 약 30~40% 수준.
- **어떻게**: 4 stage (W1 TS HTTP server, W2 HTTP client matching,
  W3 gRPC, W4 message queue / pub-sub). 각 stage마다 새 edge type
  1~3개 추가, 기존 Endpoint/MessageType 노드 재사용. PageRank/Leiden
  exclusion + cache invalidation 규칙은 schema 1.8과 동일 패턴.
- **선행**: 없음. schema 1.8 위에 append-only. 단 W3 (gRPC)는
  `.proto` parser가 새로 필요해 사이즈가 큼.

---

## §1. 왜 지금 — 현재 schema 1.8의 cross-language 한계

### 1.1 현재 emit되는 cross-language edges

| Edge | Source | Target | Confidence | 검출 패턴 |
|------|--------|--------|-----------|---------|
| `binds_to` | TS Variable/Class | Solidity Contract | INFERRED | `internal/link/xlang.go` name + ABI heuristic |
| `listens_on` | Go Function/Method | Endpoint | EXTRACTED \| INFERRED | `http.HandleFunc` / `(*ServeMux).HandleFunc` + 문자열 리터럴 route |
| `handles_message` | Go Method | MessageType | EXTRACTED | JSON-RPC `func (T) M(args A, reply *R) error` 시그니처 매칭 |
| `rpc_calls` | Go Function | MessageType | INFERRED | `client.Call("Service.Method", ...)` 문자열 인자 |

### 1.2 검출 안 되는 것 (사용자가 가장 자주 묻는 질의)

- **TS HTTP server → Endpoint**: Express/Koa/Fastify/Hono 모두 미검출.
  TS 6,846 노드 중 endpoint emit = 0. 현재 self-graph (CKG 자체)에는
  Next.js viewer가 있는데 `/api/*` route handlers의 Endpoint 노드가
  없음.
- **TS HTTP client → Go server matching**: `fetch('/api/users')` 또는
  `axios.get('/api/...')`이 어느 backend handler로 도착하는지 graph
  traversal 불가. 노드 양쪽에 존재해도 connecting edge 없음.
- **Go HTTP client (`http.Client.Get`)**: caller → 외부 Endpoint
  matching 누락. 같은 monorepo의 다른 서비스 endpoint 호출 시 graph
  분리됨.
- **gRPC client/server**: `pb.RegisterFooServer(s, &impl{})` /
  `stub.RpcMethod(ctx, req)` 모두 미검출. `distributed.go` 주석에
  명시적으로 "deferred" 표기됨.
- **Message queue topics**: Kafka / NATS / RabbitMQ / AWS SQS의
  publisher/consumer pair. 비동기 통신은 graph traversal에서 끊김.
- **WebSocket / SSE handlers**: long-lived connection 기반 endpoint.

### 1.3 영향

CKG의 G5 Distributed axis가 자체 spec 대비 30~40% 수준. 가장 큰
실제 사용자 질의 — *"이 TS 함수 호출 → 결국 어느 Go 함수가 실행되는가?"*
— 가 graph traversal로 답이 안 됨. 사용자는 `search_text` →
파일 grep → 수동 매칭으로 fallback 중.

---

## §2. 후보 방향 3가지 (SESSION-HANDOFF-2026-05-10 §10.A에서 enumerate)

### A. Cross-language interop edges (본 spec의 추천)

**범위**: Go ↔ TS ↔ Solidity 사이 HTTP/gRPC/queue/contract 호출.
**가치**: 사용자의 #1 unanswered 질의 직접 해결. G5 axis를 50%대로 끌어올림.
**비용**: 언어별 detection 패턴 × 호환 frameworks (Express/Koa/Fastify/...).
  MVP는 dominant pattern만, INFERRED edge 허용.

### B. Build-system / configuration edges

**범위**: `go.mod`, `package.json`, `Cargo.toml`, `Dockerfile`, `*.proto`,
  Helm charts, `docker-compose.yml` 사이 dependency graph.
**가치**: deployment / migration 추적 (이 서비스 deploy하면 어떤
  upstream이 영향받는가?). DevOps 질의에는 강력.
**비용**: parser 다수 추가. 그래프 traversal 의미가 코드와 분리됨
  (build artifact는 별도 dimension).

### C. Runtime / telemetry edges

**범위**: 프로덕션 traces (OpenTelemetry / Jaeger) 입력 → observed
  call graph. static analysis가 놓치는 dynamic dispatch / 외부 API
  호출 캡처.
**가치**: 가장 정확. dynamic 패턴 (reflection, ifc dispatch, message
  queue) 100% 커버.
**비용**: external data dependency. trace ingestion pipeline 신규
  구축. CKG의 "static analysis only" 약속 변경.

### 추천 결정 근거

A 권장. 이유:

- B/C는 새 dimension이지만 **A의 부분집합 질의가 가장 자주 깨진다**
  (사용자 fallback 발생률 #1).
- A는 schema 1.8 위에 append-only — 기존 Endpoint/MessageType 노드를
  재사용하고 새 edge types만 추가하면 됨.
- A의 W1~W4 stage는 각각 1-2주 단위로 ship 가능. B/C는 모놀리식.
- B는 W2에 부분 흡수 가능 (e.g. `.proto` parser가 W3 gRPC를 위해 필요).
- C는 schema 2.0 candidate — runtime data는 새 storage tier가 필요할
  것이라 별도 spec.

---

## §3. 추천 방향: Cross-language interop expansion

기존 G5 정의 (Endpoint / MessageType + 3 edges) 위에 새 detection 추가.
노드 타입은 기존 재사용 + 1~2개 추가 (e.g. `Topic` for pub/sub),
edge types 새로 4~6개 추가.

### 3.1 Stage 분할

| Stage | Scope | 새 edges | 새 nodes | 사이즈 | 의존성 |
|-------|-------|---------|---------|-------|--------|
| **W1** | TS HTTP server (Express/Koa/Fastify/Hono/Next.js route) | `ts_listens_on` (Endpoint 재사용) | — | M (~6h) | 없음 |
| **W2** | HTTP client matching (TS fetch/axios + Go http.Client) | `http_calls` (Func → Endpoint) | — | M-L (~8h) | W1 (matching target) |
| **W3** | gRPC client/server (Go + TS) + `.proto` schema | `grpc_listens_on` / `grpc_calls` | (MessageType 재사용) | L (~16h, parser 신규) | 없음 (병렬 가능) |
| **W4** | Message queue (Kafka/NATS/RabbitMQ) pub/sub | `publishes_to` / `consumes_from` | `Topic` (신규) | M (~8h) | 없음 |

### 3.2 W1: HTTP server endpoint detection (TS + Go W1.5 follow-up)

본 stage는 두 phase로 land됨:
- **W1** (commit `da502f4` + `ee1a17b`): TS 측 detection.
- **W1.5** (commit `c21ea61`): Go HTTP endpoint qname을 `(method, path)`
  쌍으로 끌어올려 TS와 cross-language 매칭 가능하게 정합.

**대상 patterns** (V0 — dominant만):

- **TS Express/Koa**: `app.get('/users', handler)` / `router.post(...)`
- **TS Fastify**: `fastify.route({ method, url, handler })` / `fastify.get(...)`
- **TS Hono**: `app.get('/api', c => ...)` (fluent API)
- **TS Next.js App Router**: `app/api/users/route.ts`의 `export async function GET/POST/...`
- **TS Next.js Pages Router**: `pages/api/*.ts`의 default export
- **Go stdlib (W1.5)**: `http.HandleFunc(pattern, h)` / `http.Handle(pattern, h)`
  / `(*ServeMux).HandleFunc(pattern, h)` / `(*ServeMux).Handle(pattern, h)`.
  Pattern은 두 형태:
  - **legacy** (`"/users"`): method 미지정 → `METHOD="*"` (net/http가 모든
    verb를 단일 핸들러로 dispatch하는 의미를 그대로 표현).
  - **Go 1.22+ method-prefixed** (`"GET /users"`, `"DELETE /admin/{id}"`):
    `splitGo122Pattern` 헬퍼가 leading uppercase token + 단일 공백 + `/`
    prefix를 검출해 (method, route) 분리. 매치 실패는 conservative
    fallback으로 `("*", original)` — false positive로 route를 망가뜨릴
    위험 회피. Edge case 락-인은 `distributed_internal_test.go::TestSplitGo122Pattern`
    (13개 sub-test).

**emit 규칙**:

- Endpoint 노드 (재사용): `qualified_name = 'http:METHOD /route'`,
  e.g. `http:GET /api/users`. Method 누락 또는 wildcard → `http:* /api/users`.
- `listens_on` edge (§6.2 (B) 결정 — 새 edge type 없이 기존 재사용):
  handler Function/Method → Endpoint. Endpoint 노드의 `language` 필드로
  emit 언어 식별 (`go` / `ts`). Confidence: 문자열 리터럴 route +
  인식된 framework 패턴 → EXTRACTED. computed route / unknown framework
  → INFERRED.
- Dedup: 같은 `(method, route)` 쌍은 한 Endpoint 노드. 같은 path의
  GET / POST / DELETE는 각각 다른 노드.

**검증 fixture**:
- TS: `internal/parse/typescript/testdata/distributed/` (Express / Fastify
  / Hono / Next.js 4종).
- Go: `internal/parse/golang/testdata/distributed/http_handlers.go`
  (legacy + Go 1.22 method-prefixed 둘 다 포함).

### 3.3 W2: HTTP client → server matching

**대상 patterns**:

- **TS**: `fetch('/api/users')`, `axios.get('/api/users', {...})`,
  `axios.post`, `useSWR('/api/...', fetcher)`, `useQuery({ url: '/...' })`
- **Go**: `http.Get(url)`, `http.Post(url, ...)`, `(*http.Client).Do(req)`,
  Request URL이 string-literal일 때만 (computed URL은 INFERRED 또는 drop)

**emit 규칙**:

- `http_calls` edge (신규): caller Function → Endpoint.
- Target Endpoint 매칭은 **2-단계 cascade** (§6.9 결정):
  1. **Specific verb 우선**: client의 method가 알려진 경우
     (`fetch('/x', {method: 'POST'})`, `axios.post(...)`) `http:POST /x`로
     먼저 lookup.
  2. **Wildcard fallback**: 1단계 miss 시 같은 path의 `http:* /x` 로
     cascade. method 모르는 client (`fetch('/x')` default GET, dynamic
     method)는 곧바로 wildcard부터 lookup.
  - net/http (Go 1.22 ServeMux) 실제 라우터 동작과 정합: 같은 path에
    `"GET /x"` (specific) + `"/x"` (wildcard) 공존 시 GET 요청은
    specific으로, 다른 method는 wildcard로 fall-through.
- 매칭 fail 시: placeholder Endpoint with `AMBIGUOUS` confidence
  (§6.3 (B) 결정). monorepo 외부 API audit 가능하게 surface 유지.
- **Path matching은 exact-match** — W2 dispatch 시점 결정 (2026-05-11):
  - 옵션 (a) exact-match — 사용자가 monorepo path convention을 신뢰할 수
    있다고 가정. false-negative 가능 (e.g. multi-segment prefix mismatch).
  - 옵션 (b) suffix-match — 다른 서비스의 동일 path suffix (e.g. 두 microservice가
    모두 `/api/users` 노출) 가 false-positive로 cross-link. graph noise 큼.
  - 옵션 (c) hybrid (exact 우선 + suffix fallback) — 직관 위반 + audit 시
    원인 추적이 어려움.
  - **결정 (a) exact-match.** 이유: V0에서는 false-positive보다 false-negative가
    훨씬 안전. monorepo 외부 API는 §6.3 placeholder로 surface되므로 사용자가
    누락된 매칭을 발견할 수 있음. suffix-match는 graph 의미를 의심하게 만들고
    PageRank를 왜곡. 추후 단계에서 suffix fallback이 필요해지면 같은 placeholder
    위에 incrementally 추가 가능.

**의존성**: W1 + W1.5가 Endpoint를 정확한 `(method, path)` qname으로
emit해야 cascade lookup이 의미 있음. backend가 다른 monorepo / 외부
API일 경우 placeholder Endpoint 노드 필요.

### 3.4 W3: gRPC client/server + `.proto` schema

**대상 patterns**:

- **Go server**: `pb.RegisterFooServer(s, &impl{})` →
  generated `FooServer` interface implementation
- **Go client**: `stub.RpcMethod(ctx, req)` where `stub` is
  `pb.NewFooClient(conn)` return
- **TS gRPC-web client**: `grpcClient.unary(...)` patterns
- **`.proto` parser**: 새 언어 입력. service / message / rpc 정의를
  CKG 노드로 변환. MessageType 노드 재사용 + Service 신규 (or
  Interface 재사용으로 fold).

**emit 규칙**:

- `grpc_listens_on`: Method → Endpoint (qname: `grpc:Service.Method`)
- `grpc_calls`: caller Function → Endpoint (suffix-match on `Service.Method`)
- `.proto` Message → MessageType node (qname: `proto:pkg.Message`)
- `defines` edge (기존 재사용): Service → Method, Message → Field

**파서 위치**: `internal/parse/proto/` 신규. tree-sitter-proto 또는
구현 별도 검토 (§6 결정).

**W3a status (2026-05-11 land)**: `.proto` parser shipped. Hand-rolled
recursive-descent lexer + visitor in `internal/parse/proto/` (decls.go,
lexer.go, parser.go, visitor.go). Cross-namespace nested-type resolution
via `byBareName` fallback (W3a review I1) is the key resolver for both
own-pass `uses_type` edges and the W3b Go-side suffix matcher.

**W3b status (2026-05-11 land)**: Go gRPC server/client detection shipped.
`internal/parse/golang/grpc.go` runs as a sub-pass of `emitDistributedDecls`
(after HTTP/JSON-RPC, before context-paths). Two detectors:

- `pb.RegisterXXXServer(s, &impl{})` → for each exported method on the
  impl receiver, emits one `grpc_listens_on` edge to a Endpoint named
  `grpc:Service.Method` (language="go", sub_kind="grpc"). typesInfo path
  EXTRACTED; AST fallback INFERRED.
- `stub := pb.NewXXXClient(conn)` then `stub.RpcMethod(ctx, req)` →
  emits one `grpc_calls` edge per call site. typesInfo path verifies the
  receiver's underlying type is *Interface (false-positive guard against
  user-defined `FakeClient struct{}`); AST fallback uses a per-function
  stub-variable map. Misses emit AMBIGUOUS placeholder Endpoints
  (language="external") mirroring the W2 http_calls pattern.

V0 limitations (W3b):

- Cross-file server↔client resolution lives in placeholders — when a Go
  server in `a.go` registers a service that a Go client in `b.go` calls,
  the client edge points at a language="external" placeholder Endpoint
  even though the same-qname real Endpoint exists in the graph. Per-file
  endpointNodeIDs dedup is the cause; a future linker pass can rewire
  by qname suffix-match. Same-file pairs DO resolve to the real Endpoint.
- Proto-package prefix is dropped in the emitted Endpoint qname:
  `grpc:Service.Method` instead of `grpc:pkg.Service.Method`. The proto
  parser emits `proto:pkg.Service.Method` Method nodes; cross-language
  matching between the two surfaces is left to the same future linker.
- Streaming, bidirectional, and per-message recursion are out-of-scope.

**W3c status (2026-05-11 land)**: TS gRPC-web / Connect-web client
detection shipped. `internal/parse/typescript/grpc_client.go` runs as the
final sub-pass of `declVisitor.visit()` (after `runHTTPClients`). Two
patterns:

- `new <Svc>Client(host)` then `client.method(req, cb)` (grpc-web
  Improbable Engineering / Google grpc-web). The stub variable is
  scoped per enclosing function ID — `client` in fn A and `client` in
  fn B bound to different services don't collide.
- `createPromiseClient(Svc, transport)` / `createClient(Svc, transport)`
  (Buf Connect-web / Connect-ES) then `await client.method(req)`. Same
  per-function scope key.
- `grpc.unary(Service.Method, { request, host, onEnd })` — service +
  method extracted from the first argument's MemberExpression.

Each match emits an `grpc_calls` edge from the enclosing Function to an
AMBIGUOUS placeholder Endpoint (language="external", sub_kind="grpc",
qname `grpc:Service.Method`). Per §6.5 (c), every TS gRPC edge is
INFERRED — tree-sitter parses don't carry typesInfo, so the EXTRACTED
lane is reserved for the Go-side W3b. Same-file placeholder Endpoints
dedup by qname; cross-file / cross-language linking stays in placeholder
form until a future linker pass.

V0 limitations (W3c, additive to W3b):

- Method-name camelCase ↔ proto PascalCase mismatch is observed (TS
  emits `grpc:UserService.getUser` while proto declares `GetUser`). The
  future linker pass that rewires placeholders to real Endpoints will
  normalise. Method-name comparison should be case-insensitive on first
  letter at least.
- Nested-scope stub shadowing within one function (a `client` inside an
  `if` block re-bound) retains the outermost binding — V0 doesn't model
  block scopes. Documented; rare in real codebases.
- Streaming method types (server-streaming, client-streaming, bidi) emit
  identically to unary. Stream semantics are not surfaced in V0.

### 3.5 W4: Message queue pub/sub

**대상 patterns**:

- **Kafka**: `kafka.NewProducer.Produce(&kafka.Message{Topic: "x", ...})`
  / `consumer.Subscribe(["x"], ...)`
- **NATS**: `nc.Publish("subject", msg)` / `nc.Subscribe("subject", ...)`
- **RabbitMQ**: `ch.PublishWithContext(...)` / `ch.Consume(...)`
- **AWS SQS / SNS / EventBridge**: SDK call patterns
- **TS equivalents**: `@nestjs/microservices`, `kafkajs`, `amqplib`

**emit 규칙**:

- `Topic` node (신규): `qualified_name = 'topic:<name>'`. Topic 이름
  string-literal로 추출.
- `publishes_to` edge: producer Function → Topic
- `consumes_from` edge: consumer Function → Topic
- Dynamic topic (variable) → AMBIGUOUS confidence

### 3.6 어떻게 graph traversal로 답하는가 (질의 예시)

| 질의 | Traversal |
|------|----------|
| "이 TS 함수가 어느 Go handler 호출?" | TS Func → `http_calls` → Endpoint ← `listens_on` ← Go Method |
| "이 Endpoint를 누가 호출하나?" | Endpoint ← `http_calls` ← Function (any language) |
| "이 Kafka topic의 producer/consumer 목록?" | Topic ← `publishes_to`/`consumes_from` ← Function |
| "이 gRPC method의 client + server pair?" | Endpoint ← `grpc_listens_on`/`grpc_calls` |

---

## §4. Schema 변경 (예상)

### 4.1 새 NodeType

- `Topic` (W4): pub/sub topic. `qualified_name = 'topic:<name>'`,
  `sub_kind = 'kafka' | 'nats' | 'rabbitmq' | 'sqs' | ...`.

총 34 → **35** node types.

### 4.2 새 EdgeType

| Edge | Stage | Src → Dst | Notes |
|------|-------|----------|-------|
| `ts_listens_on` | W1 | TS Func → Endpoint | Go `listens_on`의 TS counterpart |
| `http_calls` | W2 | Func (any lang) → Endpoint | exact-match resolution (§3.3 결정) |
| `grpc_listens_on` | W3 | Method → Endpoint | gRPC service method |
| `grpc_calls` | W3 | Func → Endpoint | gRPC client call |
| `publishes_to` | W4 | Func → Topic | pub/sub producer |
| `consumes_from` | W4 | Func → Topic | pub/sub consumer |

총 35 → **41** edge types. W2 land 시점 (2026-05-11) 35 → **36** (http_calls 1개
append; ts_listens_on은 §6.2 (B) 결정으로 listens_on 재사용 — 미신설).
**W3b land 시점 (2026-05-11) 36 → 38** (`grpc_listens_on` + `grpc_calls` 2개
append; both at positions 37 + 38 in `AllEdgeTypes()` — TestAllEdgeTypes_Stable
locks the new order).

### 4.3 SchemaVersion bump

- `internal/buildpipe/cache.go`: `SchemaVersion = "1.9"`
- `internal/persist/sqlite.go`: 새 migration 단계 추가
  (W1 land 후 1.8 → 1.9). 새 컬럼은 없고 enum 값만 추가되므로
  ALTER 불필요 — `pkg/types/enums.go` append-only로 충분.
- 모든 stage가 같은 1.9 안에 들어가도 되는가? 아니면 stage마다 bump
  (1.9 / 1.10 / 1.11 / 1.12)? → §6 decision.

### 4.4 Hash-stable IDs

기존 enums.go 패턴 따라 **append만** — 기존 indexable position 불변.
W1~W4의 새 EdgeType은 `EdgeModifies` 뒤에 순차 append.

---

## §5. 영향 받는 컴포넌트

### 5.1 코드 변경 위치

- `pkg/types/enums.go`: NodeType + EdgeType append.
- `internal/buildpipe/cache.go`: SchemaVersion bump.
- `internal/parse/typescript/distributed.go` 신규: W1 + W2 TS 패턴.
- `internal/parse/golang/distributed.go` 확장: W2 Go HTTP client,
  W3 gRPC patterns, W4 message queue.
- `internal/parse/proto/` 신규 디렉토리: W3 `.proto` parser.
- `internal/link/xlang.go` 확장: cross-language matching (`http_calls`
  suffix resolution 등).
- `internal/buildpipe/pipeline.go`: language_runners.go에 proto
  runner 추가 (W3).
- `internal/persist/sqlite.go`: schema migration 1.8 → 1.9 (필요시).
- `web/viewer-next/src/lib/edges.ts`: GRAPH_GROUPS 갱신 (새 edges는
  G5에 합류).
- `web/viewer-next/src/components/EdgeTypeFilters.tsx`: 새 edge types
  pill 추가.

### 5.2 검증 / 회귀

- W1: TS testdata 4 fixtures (Express/Fastify/Hono/Next.js).
  unit test `typescript/distributed_test.go`.
- W2: Cross-language matching test. testdata에 Go server + TS client
  쌍 fixtures. Integration test `internal/link/xlang_test.go` 확장.
- W3: `.proto` parser 회귀, gRPC fixture 양쪽.
- W4: 각 broker별 fixture (4 patterns).
- `pkg/evidence` H3 / H4 회귀는 영향 없음 (new edges는 retrieval
  외부).
- bench-server: edge type 수 증가로 `/api/edges/counts` payload 약간
  증가. p99 영향 nil 예상.

### 5.3 문서 동기화

W1 land 시:
- `docs/SCHEMA.md`: 1.8 → 1.9, 새 node + edges 추가.
- `docs/INCREMENTAL.md`: SchemaVersion 1.8 → 1.9.
- `docs/design/schema-1.9-spec.md` (본 spec): "implemented" 마킹.

---

## §6. §11.x 형식 결정 항목 (사용자 답변 — 2026-05-11 confirmed §6.1~§6.3)

hunk-graph.md의 §11 8개 결정 패턴 따라 사용자 합의 받음. W1 시작 전
필수인 §6.1~§6.3은 답변 확정. §6.4~§6.8은 W2~W4 진입 시점에 다시 확인.

### §6.1 Stage 단위 schema bump — **(A) 1.9 한 번** ✅

- (A) 1.9 한 번 — W1~W4 모두 1.9 안에 점진 append.
- (B) Stage마다 bump (1.9 / 1.10 / 1.11 / 1.12) — incremental cache
  invalidation을 stage마다 발생시킴.
- **확정 (2026-05-11): (A) 1.9 한 번. 단 사용자 추가 조건 — "schema
  변경이 있으면 추가 작업"으로 인식.** 즉 W1 land 시점에 SchemaVersion
  1.8 → 1.9 bump (`internal/buildpipe/cache.go`)는 schema-changing
  작업으로 다루며 cache invalidation을 명시. 이후 W2~W4 stage는 new
  edge type append만으로 1.9 유지 (enum append-only는 cache-key 변화
  없으나 *새 detection 결과가 그래프에 새 edges 등장* → 사용자 의도상
  schema 변경에 준한 검증 필요).

### §6.2 W1 — `ts_listens_on` 별도 edge type vs `listens_on` 재사용 — **(B) 재사용** ✅

- (A) 별도 `ts_listens_on` — 언어 식별이 쉬워 viewer pill 분리 가능.
- (B) 동일 `listens_on` — Go와 같은 semantics, viewer 필터 단순.
- **확정 (2026-05-11): (B) Go와 동일 `listens_on` 재사용.** Endpoint
  노드의 `language` 필드 (현재 Go emit은 `language='go'`)로 언어 구분.
- **사용자 추가 제약 (load-bearing — §7.0 참조)**: *"TS를 위한 작업으로
  인하여, Golang을 위한 작업이 절대 깨져서는 안 된다. 작업마다 테스트
  검토가 필요."* — Go regression guard를 모든 W stage acceptance criteria의
  P0 항목으로 격상.

**Endpoint qname 규칙 (protocol별)** — §6.2 (B) 결정의 구체화 (W1.5
2026-05-11 확정):

| protocol | qname 포맷                | 예시                          | 비고                                                                       |
|----------|---------------------------|-------------------------------|----------------------------------------------------------------------------|
| http     | `http:METHOD /route`      | `http:GET /api/users`         | (method, path) 쌍이 unique key (REST semantics). Go stdlib `HandleFunc`에서 verb 미지정 시 `METHOD=*` (모든 verb 수신). Go 1.22+ `"GET /users"` pattern은 그대로 split. |
| rpc      | `rpc:Service.Method`      | `rpc:UserService.Get`         | net/rpc Service.Method 단독 (이미 method가 식별자에 포함됨).               |
| grpc     | `grpc:pkg.Service.Method` | `grpc:user.UserService.Get`   | proto 정의 따름 (W3에서 land).                                             |
| ws       | `ws:/route[#msg]`         | `ws:/chat#text-message`       | path + optional message-type (W4 또는 이후 stage).                         |
| graphql  | `graphql:opType.opName`   | `graphql:query.userById`      | (operation-type, name) (이후 stage).                                       |

공통 패턴: `{sub_kind}:{protocol-specific-identifier}` — protocol-specific
identifier가 각 protocol에서 unique key. `language` 필드는 emit한 parser
식별용 (cross-language 매칭에는 무관 — 동일 qname이면 동일 Endpoint).

**Go HTTP qname 끌어올림 (W1.5, 2026-05-11)** — 직전 W1 TS commit (`da502f4`)
이 `http:METHOD /route` 포맷을 emit한 반면 기존 Go는 `http:/route`만 emit
하고 method를 버렸음 (`internal/parse/golang/distributed.go`의 `_ = method`).
W1.5에서 Go HTTP emit을 위 규칙에 맞춰 끌어올림 — TS·Go가 같은 (method, path)
쌍에 대해 동일 Endpoint qname을 생성하므로 cross-language matching이
자연스럽게 성립한다. 사용자 옵션 D 결정.

### §6.3 W2 — HTTP client matching 실패 처리 — **(B) AMBIGUOUS placeholder** ✅

- (A) Drop (V0 기존 패턴 — `rpc_calls` matching fail 처리와 동일).
- (B) Placeholder Endpoint with `AMBIGUOUS` confidence (`§11.3` 패턴).
- (C) `external:` prefix Endpoint (e.g. `http:GET external:/api/x`)
  with INFERRED.
- **확정 (2026-05-11): (B) AMBIGUOUS placeholder.** H3 retrieval
  boundary의 `llmSafeStoreReader` wrapper가 이미 AMBIGUOUS를 LLM에서
  숨기므로 자연 정합. 사용자 surface (Recovery 패널 또는 별도
  "External APIs" 패널 신설 — W2 시점에 결정) 에서 unmatched calls를
  명시적으로 노출해 monorepo 외부 API 의존성 audit 가능.

### §6.4 W3 — `.proto` parser 선택

- (A) `tree-sitter-proto` (community grammar — 검토 필요한 maintenance
  상태).
- (B) `google.golang.org/protobuf/types/descriptorpb` (pre-compiled
  `.pb.go`만 사용, `.proto` 자체는 안 읽음).
- (C) Hand-rolled minimal parser (Service / Message / Field 만 추출).
- 추천: **(C)** — `.proto`의 의미 있는 부분은 매우 단순 (service block,
  message block, field에 type + number). Maintenance가 가장 작고,
  tree-sitter dependency 추가 회피.

### §6.5 W3 — gRPC 식별 confidence

- (A) Strict: `pb.RegisterXXXServer` 호출 시점 + 인터페이스 매칭 type
  info 확보 시에만 EXTRACTED.
- (B) AST-only: function 시그니처가 `(context.Context, *Request) (*Response, error)`이면 INFERRED grpc handler로 후보.
- (C) Both with split confidence.
- 추천: **(C)** — typesInfo 있으면 EXTRACTED, 없으면 INFERRED. 기존
  Go parser의 strict_validate 패턴과 일관.

### §6.6 W4 — Topic 이름 매칭 (constants vs literals)

- (A) String literal만 — variable / concat 무시.
- (B) Variable + 1-level const-fold 추적 (Go: `const TopicX = "..."`,
  TS: `const TOPIC_X = '...'`).
- (C) Full data-flow analysis (out of scope V0).
- 추천: **(B)** — 큰 codebase에서 topic 이름은 종종 const로 빠짐.
  1-level const-fold는 cheap.

### §6.7 Viewer integration

- (A) 새 edges는 G5 axis로 합류, 기본 off (현재 G5 패턴과 일관).
- (B) 새 axis G7 신설 (Cross-language)? — 의미는 G5 distributed의
  subset이라 axis 신설 과잉.
- (C) G5에 sub-grouping (Endpoint / Queue / Contract).
- 추천: **(A)** — 사용자가 *"distributed"* 멘탈 모델로 묶음. axis 신설
  시 NodeTypeFilters + EdgeFilters 양쪽에 새 group 추가하는 cost가 큼.

### §6.8 PageRank / Leiden treatment

- (A) Topic 노드 제외 (Hunk / Commit과 동일 패턴).
- (B) 모두 포함 — distribution 분석 시 hub 노드로 의미 있음.
- 추천: **(A)** — Topic은 in-degree가 높아 pagerank 왜곡. 별도
  distribution chart로 surface하는 게 정확.

### §6.9 W2 — HTTP wildcard 매칭 cascade — **(A) specific 우선 + wildcard cascade** ✅

`http:GET /api/users` (specific) 와 `http:* /api/users` (wildcard) 가 같은
graph에 공존할 때 client `fetch('/api/users')` (method=GET default) 또는
`axios.post('/api/users')` 가 어느 Endpoint로 매칭되는가?

- (A) **Specific verb 우선, fail 시 wildcard cascade**: client method가
  알려졌으면 `http:METHOD /path` 먼저 lookup → miss 시 `http:* /path`로
  fall-through. method 모르는 client는 곧바로 wildcard.
- (B) Wildcard가 모든 method에 매칭: specific endpoint 무시.
- (C) 둘 다 매칭 — edge 2개 emit.

- **확정 (2026-05-11): (A) specific 우선 + wildcard cascade.**
- 근거:
  1. net/http (Go 1.22 ServeMux) 실제 라우터 동작과 정합. graph가
     라우터 의미를 반영해야 사용자 직관과 일치.
  2. W2 매칭 로직 단순: lookup table 2회 (`http:METHOD /x` → fail이면
     `http:* /x`). edge type은 그대로 `http_calls` 한 종류.
  3. 옵션 (B)는 specific endpoint를 무시 — 직관 위반. 옵션 (C)는
     graph noise + traversal ambiguous.
- 구현 위치: W2 dispatch 시 client matching pass (`internal/link/xlang.go`
  확장 또는 `internal/parse/typescript/http_client.go` 신규).

---

## §7. Acceptance criteria per stage

### §7.0 Go regression guard (모든 stage 공통 — P0, 사용자 명시 §6.2)

**Load-bearing constraint**: TS / `.proto` / message-queue 신규 작업으로
인해 기존 Go 동작이 깨져서는 안 된다. 모든 W stage commit 전 다음 확인
필수:

- [ ] `go test ./... -count 1` 전 패키지 PASS — Go parser / distributed.go
  / pkg/evidence / internal/server / internal/mcp 등 23/23 ok.
- [ ] `go vet ./...` clean.
- [ ] **Go-only fixture 그래프 비교**: 작업 전 baseline build 산출물
  (`/tmp/ckg-go-baseline`)과 작업 후 build 산출물의 노드/엣지 count
  diff = 0 (또는 의도된 변화만, e.g. Go HTTP client patterns은 W2 시점
  에 의도된 증분만 허용).
  ```bash
  # baseline (작업 전):
  ./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-go-baseline --no-cache --lang=go
  sqlite3 /tmp/ckg-go-baseline/graph.db "SELECT type, COUNT(*) FROM edges GROUP BY type" > /tmp/baseline.txt

  # 작업 후:
  ./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-go-after --no-cache --lang=go
  sqlite3 /tmp/ckg-go-after/graph.db "SELECT type, COUNT(*) FROM edges GROUP BY type" > /tmp/after.txt
  diff /tmp/baseline.txt /tmp/after.txt   # 의도된 변화만 (W1은 0)
  ```
- [ ] `internal/parse/golang/distributed_test.go` (E3 Go HTTP/JSON-RPC
  핸들러 검증)가 변경 없이 PASS — TS 작업이 Go distributed pass의
  공유 헬퍼 (Endpoint dedup, messageNodeIDs 등)를 수정해야 한다면
  명시적 diff 표기.
- [ ] go-stablenet self-graph 또는 동등 corpus build 시 Go 노드/엣지
  카운트가 baseline ± 0 (TS-only 추가는 Go count에 영향 0이어야 함).

### §7.W stage별 criteria

### W1 (TS HTTP server)

- [ ] **§7.0 Go regression guard 통과** (가장 먼저 — TS 작업이 Go
  parser / shared helper에 영향 주지 않음 확인).
- [ ] testdata 4 fixtures (Express / Fastify / Hono / Next.js) 모두
  parse → Endpoint node 적어도 1개 + `listens_on` edge 적어도 1개
  (Endpoint의 `language='ts'` 명시).
- [ ] 같은 route 중복 dedup.
- [ ] Computed route는 INFERRED + 라벨에 "<computed>" 표기.
- [ ] CKG self-graph (Next.js viewer 포함) build 후 `/api/edges/counts`
  G5 카운트가 W1 land 전 대비 비례 증가.
- [ ] go test ./internal/parse/typescript/... PASS.
- [ ] `pkg/types/enums.go` 변경 없음 (§6.2 (B) — `listens_on` 재사용).
- [ ] `internal/buildpipe/cache.go` SchemaVersion `"1.8"` → `"1.9"`
  bump (§6.1 schema-changing 작업 인식).
- [ ] `internal/persist/sqlite.go::Migrate` 갱신 (1.8 → 1.9 stub —
  새 column 없으나 version 인식만 추가).

### W2 (HTTP client matching)

- [ ] Fixture: Go server + TS client + Go client / TS server 4가지
  permutation 모두 graph상에서 Endpoint 경유 traversal로 reachable.
- [ ] 매칭 실패는 §6.3 결정 따라 처리 (drop / AMBIGUOUS / external:).
- [ ] suffix-match가 false-positive로 다른 Endpoint에 cross-link
  발생하지 않음 (검증: identical name 다른 path 케이스).
- [ ] go test ./internal/link/... PASS.

### W3 (gRPC + `.proto`)

- [ ] `.proto` minimal parser가 service / message / field 추출.
- [ ] `pb.RegisterXXXServer` 패턴 인식, registered methods가
  Endpoint 노드로 등장.
- [ ] gRPC client `stub.M(...)` 호출 → matched Endpoint 또는
  AMBIGUOUS placeholder.
- [ ] testdata에 minimal .proto 1개 + Go server + Go client + TS client.

### W4 (Message queue)

- [ ] Kafka / NATS / RabbitMQ / AWS SQS 각 1개 fixture, Topic 노드
  생성 + publish/consume edges.
- [ ] Constant-fold (Go const, TS const declaration) 1-level 처리.
- [ ] Dynamic topic은 AMBIGUOUS.

---

## §8. Risks / known limitations

- **Detection scope 폭주**: 각 언어의 HTTP framework가 매우 다양함.
  V0는 dominant 3-4개만, long tail은 INFERRED fallback 또는 미검출.
  사용자가 framework X를 쓰면 *"왜 endpoints가 안 보이지?"* 호소
  나올 수 있음 → docs/SCHEMA.md에 supported list 명시.
- **Suffix-match false positive**: 다른 monorepo의 동일 path
  (`/api/users` 두 서비스 모두) — placeholder Endpoint 충돌. mitigation:
  Endpoint qname에 `service:` prefix optional (e.g. `http:GET auth-service:/api/users`).
- **gRPC parser maintenance**: hand-rolled parser는 .proto3 syntax
  edge case (oneof, map<>, options) 일부 누락 가능. 사용자 PR
  workflow로 점진 보강.
- **Computed routes 보편화**: Next.js dynamic routes (`[id]`)는 route
  pattern으로 emit 가능하나 query 매칭이 어려움. INFERRED + route
  template 보관.
- **Message queue dynamic topic**: 대형 codebase에서 변수 기반 topic
  이름이 흔함. const-fold만으로는 부족. SSA-level data flow는 V0
  out of scope.

---

## §9. References

- 직전 schema spec: `docs/design/hunk-graph.md` (§11 패턴 원본).
- 직전 hand-off: `docs/SESSION-HANDOFF-2026-05-10.md` §10.A
  ("schema 1.9 design 권장 시작점").
- 현재 G5 구현: `internal/parse/golang/distributed.go` (Go HTTP/JSON-RPC).
- 현재 cross-lang: `internal/link/xlang.go` (Sol↔TS binds_to).
- Schema 변경 패턴: `docs/SCHEMA.md` §"Schema bumps history".
- Append-only enum 패턴: `pkg/types/enums.go` 주석
  (`TestAllNodeTypes_Stable` 보장).

---

## §10. 다음 단계

1. ~~사용자 §6 결정 8개 답변~~ → **§6.1~§6.3 + §6.9 확정 2026-05-11**
   (§6.4~§6.8은 W2~W4 진입 시점에 다시 확인).
2. ~~**W1 first commit dispatch**~~ → **land 2026-05-11 (commit `da502f4` +
   follow-up `ee1a17b`)** — TS HTTP server endpoint detection.
3. ~~**W1.5 Go HTTP qname 끌어올림**~~ → **land 2026-05-11 (commit `c21ea61`)**
   — Go HTTP Endpoint qname을 `http:METHOD /route` 포맷으로 통일 (§6.2 보완
   표 참조). TS·Go가 동일 `(method, path)` 쌍에 동일 qname을 생성해 W2
   dispatch matching의 사전 조건을 충족.
4. ~~**W1.5 review 후속 처리**~~ → **land 2026-05-11** — code-reviewer가
   raise한 Important 2건 + Minor 2건 정리: `splitGo122Pattern` 13-case
   internal unit test 추가, §3.2 Go server 패턴 catalog 보강, §3.3 W2
   wildcard cascade 매칭 규칙 명시 (§6.9 결정), empty-pattern early-return
   guard 추가.
5. **W1 land 후 viewer 검증** — self-graph build → Endpoint 노드 등장 +
   G5 카운트 증가 확인.
6. ~~**W2 dispatch**~~ → **backend land 2026-05-11** — W1 + W1.5 base 위에
   client matching. §6.9 cascade 규칙 따라 specific verb 우선 lookup +
   wildcard fallback. §3.3 exact-match path matching 결정 land. §7.0 Go
   regression guard 통과 (synthetic baseline: 8 edge types, 동일 count;
   `http_calls`는 새 edge type append).
   - `pkg/types/enums.go`: `EdgeHTTPCalls` 추가 (35 → 36).
   - `internal/parse/golang/distributed.go`: HTTP client detection
     (`http.Get/Post/PostForm/Head`, `(*http.Client).Get/Post/...`,
     `http.NewRequest{,WithContext}`) — string-literal URL만, dynamic 스킵.
   - `internal/parse/typescript/http_client.go` (신규): `fetch`,
     `axios.METHOD`, `axios({method, url})`, `axios.request`, `useSWR`,
     `useQuery({url})` 검출 + axios* 식별자 W1 false-positive 가드.
   - `internal/link/http_match.go` (신규): §6.9 2-stage cascade matching
     + AMBIGUOUS placeholder retention + 매칭 시 placeholder drop.
   - `internal/buildpipe/pipeline.go::emitDerivedPasses`: `MatchHTTPClients`
     pass wire — xlang 이후, temporal 이전.
   - testdata: `internal/parse/golang/testdata/distributed/http_clients.go`
     + `internal/parse/typescript/testdata/distributed/clients.ts` (TS→TS
     자체 매칭으로 4 permutation 중 2개 cover; TS→Go·Go→TS는 link unit
     test로 cover).
   - **Viewer 통합은 별도 step에서 land** — 새 edge type은 자동 G5에
     합산되어 viewer 코드 변경 없음 (`internal/server/web_assets/`
     edge-counts 자동 반영).
7. **W2 viewer 통합** — 별도 step. 사용자 audit UX (`EdgeTypeFilters`,
   "External APIs" 패널 가능) 미land.
8. ~~**W3a `.proto` parser dispatch**~~ → **land 2026-05-11** — hand-rolled
   recursive-descent parser in `internal/parse/proto/`. Service / message /
   field / enum / oneof / map detection. Review fixes I1 (cross-namespace
   nested type resolve via `byBareName` fallback) + I2 (proto2 group
   handling) + I3 (label preservation) land.
9. ~~**W3b Go gRPC server/client dispatch**~~ → **land 2026-05-11** —
   `internal/parse/golang/grpc.go` + fixtures in
   `testdata/distributed/grpc_{server,client}.go` + `grpc.proto` + a
   hand-rolled `grpcstubs/` package so typesInfo path resolves under
   `packages.Load`. Two new edge types appended (36 → 38):
   `EdgeGRPCListensOn`, `EdgeGRPCCalls`. §7.0 Go regression guard PASS
   (synthetic baseline diff = 0; gRPC edges = 0 on the synthetic fixture
   because no `pb.RegisterXXXServer` patterns exist there). §6.5 (C)
   confidence split implemented — typesInfo path EXTRACTED, AST fallback
   INFERRED, unresolved targets AMBIGUOUS placeholders. Cross-file
   server↔client resolution to real Endpoint deferred to a future
   linker pass.
10. ~~**W3c TS gRPC-web client dispatch**~~ → **land 2026-05-11** —
    `internal/parse/typescript/grpc_client.go` + fixtures in
    `testdata/distributed/grpc/{grpcweb,connectweb}_client.ts`. No new
    edge types (reuses `grpc_calls` from W3b). Per §6.5 (c) all TS gRPC
    edges are INFERRED (typesInfo unavailable in tree-sitter). Per
    §6.3 (B) unmatched clients keep AMBIGUOUS placeholder Endpoints
    (language="external"). Per-function stub-name scoping handles the
    common case of `client` re-bound across functions. §7.0 Go
    regression guard PASS (synthetic baseline diff = 0). W3 series
    complete.
11. W4 (message queue) 진행은 W3 완료 후 사용자 추가 결정 (§6.4~§6.6)
    따라 분기.

**End of design draft.**
