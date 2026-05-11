# CKG Schema 1.9 — Design Spec (cross-language interop expansion)

> 다음 schema bump의 design plan. schema 1.8 (Hunk-graph H1-H4 + §11.3
> hybrid)가 main에 안착한 시점에서 가장 큰 미커버 dimension은
> **cross-language interop edges**. 이 문서는 `docs/design/hunk-graph.md`
> 패턴 (사용자 §11 결정 → H 시리즈 dispatch) 따라 작성된 plan이며,
> §6의 8개 결정 항목에 사용자 답변을 받은 다음 W 시리즈 구현으로
> 진입한다.
>
> **Status**: design draft 2026-05-11. 코드 변경 없음.
> **Audience**: 다음 세션 / 사용자와 §6 결정 합의 후 W1 first commit
> 받는 subagent.

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

### 3.2 W1: TS HTTP server endpoint detection

**대상 patterns** (V0 — dominant만):

- **Express/Koa**: `app.get('/users', handler)` / `router.post(...)`
- **Fastify**: `fastify.route({ method, url, handler })` / `fastify.get(...)`
- **Hono**: `app.get('/api', c => ...)` (fluent API)
- **Next.js App Router**: `app/api/users/route.ts`의 `export async function GET/POST/...`
- **Next.js Pages Router**: `pages/api/*.ts`의 default export

**emit 규칙**:

- Endpoint 노드 (재사용): `qualified_name = 'http:METHOD route'`,
  e.g. `http:GET /api/users`. Method 누락 시 `http:* /api/users`.
- `ts_listens_on` edge (신규): TS handler Function/Method → Endpoint.
  Confidence: 문자열 리터럴 route + 인식된 framework 패턴 → EXTRACTED.
  computed route / unknown framework → INFERRED.
- 같은 route 중복 emit 시 dedup (qname 기반).

**검증 fixture**: `internal/parse/typescript/testdata/distributed/`
신규. Express/Fastify/Hono/Next.js 4종 minimal handler.

### 3.3 W2: HTTP client → server matching

**대상 patterns**:

- **TS**: `fetch('/api/users')`, `axios.get('/api/users', {...})`,
  `axios.post`, `useSWR('/api/...', fetcher)`, `useQuery({ url: '/...' })`
- **Go**: `http.Get(url)`, `http.Post(url, ...)`, `(*http.Client).Do(req)`,
  Request URL이 string-literal일 때만 (computed URL은 INFERRED 또는 drop)

**emit 규칙**:

- `http_calls` edge (신규): caller Function → Endpoint.
- Target Endpoint 매칭은 **suffix-match** 기반 (e.g. `/api/users` →
  `http:GET /api/users` 또는 `http:* /api/users`). Method 매칭은
  optional second-pass.
- 매칭 fail 시: dropped (V0) 또는 placeholder Endpoint with
  AMBIGUOUS confidence (decision §6).

**의존성**: W1이 Endpoint를 emit해야 매칭이 의미 있음. 단 backend가
다른 monorepo / 외부 API일 경우 placeholder Endpoint 노드 필요.

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
| `http_calls` | W2 | Func (any lang) → Endpoint | suffix-match resolution |
| `grpc_listens_on` | W3 | Method → Endpoint | gRPC service method |
| `grpc_calls` | W3 | Func → Endpoint | gRPC client call |
| `publishes_to` | W4 | Func → Topic | pub/sub producer |
| `consumes_from` | W4 | Func → Topic | pub/sub consumer |

총 35 → **41** edge types.

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

1. ~~사용자 §6 결정 8개 답변~~ → **§6.1~§6.3 확정 2026-05-11** (§6.4~§6.8
   은 W2~W4 진입 시점에 다시 확인).
2. **W1 first commit dispatch** — `internal/parse/typescript/distributed.go`
   신규 + 4 fixture + unit test. Subagent에 본 spec §3.2 + §6.2 (B) + §7.0
   Go regression guard + §7 W1 acceptance criteria 핸드오프.
   - **Go regression이 깨지면 즉시 중단 + 사용자 보고** — 자동 재시도
     금지.
3. **W1 land 후 viewer 검증** — self-graph build → Endpoint 노드 등장 +
  G5 카운트 증가 확인.
4. **W2 dispatch** — W1 base 위에 client matching. W1과 마찬가지로
   §7.0 Go regression guard 우선 검증.
5. W3 / W4 진행은 W1+W2 사이즈 + 사용자 추가 결정 (§6.4~§6.6) 따라 분기.

**End of design draft.**
