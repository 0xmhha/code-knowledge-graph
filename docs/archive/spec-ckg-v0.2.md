# CKG v0.2 Foundation Spec

> **ARCHIVED 2026-07-18 — superseded.** This May foundation spec mixes shipped
> work (parser migration, concurrency Stage 1, incremental cache Phase 1) with
> superseded plans and still-pending items:
> - **PostgreSQL roadmap → superseded by `docs/adr/0003-deprecate-postgres-backend.md`.**
> - **Schema 1.0/1.1 references → now 1.23** (`internal/buildpipe/cache.go`; history in `docs/SCHEMA.md`).
> - **Still-pending:** concurrency Stage 2 (SSA `--deep` / `is_potential_race`) and
>   incremental cache Phase 2 (reverse-reference index) — carried in `docs/CONTINUITY.md`.
>
> Authoritative now: `docs/SCHEMA.md`, `docs/INCREMENTAL.md`, `docs/ARCHITECTURE-DETAILED.md`.

> 적용 범위: smacker → upstream tree-sitter 마이그레이션, Go 동시성 분석,
> PostgreSQL 스토리지 백엔드, 파일 단위 incremental 캐시.
> v0 prototype 다음 단계의 기반 작업 4종을 묶는다.

## 0. Overview

### Goals
1. **파서 의존성 건전화**: 정체된 `smacker/go-tree-sitter`(pseudo-version) 의존성 제거.
2. **Go 코드 정확도 차별화**: 고루틴/채널/뮤텍스를 그래프 1급 시민으로 추출.
3. **스토리지 확장성**: 단일 사용자 SQLite는 유지하되, 팀/RAG 시나리오용 PostgreSQL 백엔드 옵션 추가.
4. **재실행 비용 절감**: 변경된 파일만 재파싱하는 incremental build 도입.

### Non-Goals
- 새 언어 추가 (Rust/Python 등) — v0.3+에서 평가.
- LLM 기반 시맨틱 추출 (graphify 방식) — v0.2 범위 외.
- LSP/IDE 통합 — v0.4+에서 평가.
- Neo4j 직접 채택 — Cypher 표현력은 PostgreSQL + Apache AGE로 대체.

### Guiding Principles
- **Thin core, fat extensions**: 파싱 라이브러리는 그대로 쓰고, 시맨틱 분석은 항상 *위*에 패스로 추가. fork 금지.
- **단일 바이너리 정체성 유지**: SQLite가 default. PostgreSQL은 `--db postgres://...` opt-in.
- **결정론 우선**: 모든 추출 결과는 동일 입력 → 동일 그래프. 휴리스틱은 `confidence: INFERRED` 라벨로 명시.
- **점진적 강화**: 각 항목은 빠른 Phase 1 → 정밀 Phase 2 단계로 분리. Phase 1만으로도 단독 가치를 가짐.

### Cross-Item Dependencies
```
Item 1 (parser migration) ──┬─► Item 2 (concurrency)        : tree-sitter 신ABI는 무관, 독립 진행 가능
                            └─► Item 5 (incremental cache)  : Tree.Edit() API 활용 가능 (Phase 2+)

Item 4 (storage)            : 다른 항목과 독립. Store interface 추상화만 선행.

Item 5 (cache)              : Item 1/2의 schema_version 변경 시 캐시 무효화 (캐시 키에 schema_version 포함).
```

권장 진행 순서: **Item 1 + Item 5(Phase 1)** 동시 → **Item 2(Stage 1) + Item 4(Phase 1)** 동시 → 정밀 단계.

---

## 1. Tree-sitter Parser Migration

### Motivation
- 현 의존성: `github.com/smacker/go-tree-sitter v0.0.0-20240827...` (pseudo-version, 사실상 정체).
- 영향 면적: TS/JS/Solidity 3개 grammar 기반 파서 = grammar 파서 100%.
- Solidity는 이미 `internal/parse/solidity/binding/binding.go`에서 자체 binding 우회로를 가짐 (smacker가 solidity 서브패키지를 안 줌). upstream 전환 시 정리 기회.
- 공식 `github.com/tree-sitter/go-tree-sitter`는 0.25 ABI, ABI 검증·incremental parsing(`Tree.Edit()`)·`LookaheadIterator`·progress callback·Windows 지원을 제공.

### Design

#### Phase 1a: TypeScript/JavaScript 마이그레이션
- 파일: `internal/parse/typescript/parser.go`, `internal/parse/typescript/declarations.go`, `internal/parse/typescript/queries.go`
- 변경:
  - import 경로 교체:
    - `github.com/smacker/go-tree-sitter` → `github.com/tree-sitter/go-tree-sitter`
    - `github.com/smacker/go-tree-sitter/typescript/typescript` → `github.com/tree-sitter/tree-sitter-typescript/bindings/go`
    - `github.com/smacker/go-tree-sitter/javascript` → `github.com/tree-sitter/tree-sitter-javascript/bindings/go`
  - `Language` 생성 패턴 변경: `sitter.NewLanguage(unsafe.Pointer(...))` → upstream의 `NewLanguage()` 시그니처에 맞춰 조정.
  - Query API 차이 흡수: `sitter.NewQuery(query, lang)` 반환 타입/에러 형태 변경 가능성.
  - `QueryCursor.Matches()` 호출 시그니처 확인 — upstream은 `(query, root)` 순서가 다를 수 있음.
- 검증: `internal/parse/typescript/*_test.go` 전수 통과 + 기존 testdata 그래프와 노드/엣지 1:1 매칭.

#### Phase 1b: Solidity 마이그레이션 + binding 정리
- 파일: `internal/parse/solidity/parser.go`, `internal/parse/solidity/binding/binding.go`
- 결정 포인트: `tree-sitter-solidity` grammar는 third-party (JoranHonig/tree-sitter-solidity 등). 다음 옵션 비교:
  1. **자체 binding 유지**: 현 패턴 그대로, import만 upstream으로 교체. 가장 안전.
  2. **third-party binding으로 위임**: 외부 패키지가 Go binding을 제공하면 그걸로 교체. 의존성 1개 추가, 유지보수 위임.
  3. **purego 동적 로딩**: upstream의 `purego` 경로 활용, 컴파일 타임 의존성 제거. 빌드 단순화 / 런타임 비용.
- 권장: **(1) 자체 binding 유지**. 변경 면적 최소, 리스크 최저. (2)/(3)은 v0.3+에서 재평가.

#### Phase 1c: Incremental parsing 인프라
- 신규: `internal/parse/incremental.go` (또는 기존 dispatch에 통합)
- 기능: `Parser.Parse(content, oldTree)` 형태로 이전 파스 결과를 재사용. file 캐시(Item 5)와 결합 시 *변경 범위 외* 노드는 AST 재계산 없이 재사용.
- Phase 5 이후 작업 — Item 5 Phase 1 완료 후 진행.

### Files Affected
```
go.mod, go.sum                               (의존성 교체)
internal/parse/typescript/parser.go          (import + API)
internal/parse/typescript/declarations.go    (import + API)
internal/parse/typescript/queries.go         (Query 객체 생성)
internal/parse/typescript/*_test.go          (테스트)
internal/parse/solidity/parser.go            (import + API)
internal/parse/solidity/binding/binding.go   (import + API)
internal/parse/solidity/declarations.go      (import + API)
internal/parse/solidity/*_test.go            (테스트)
```

### Acceptance Criteria
- [x] `go.mod`에서 `smacker/go-tree-sitter` 제거. (A1+A2 atomic, 2026-04-29)
- [x] 기존 `parse/typescript`, `parse/solidity` 테스트 100% 통과.
- [x] `testdata/` 기준 그래프 빌드 결과가 마이그레이션 전후 동일 (노드/엣지 ID + count 일치).
- [x] `ckg build` end-to-end 정상 동작.

### Risks
- **R1.1**: Query DSL 미세 변화로 silent miss-extraction. *완화*: golden 테스트 추가, snapshot 비교.
- **R1.2**: solidity grammar 호환성. *완화*: Phase 1b를 별도 PR로 분리, 1a 먼저 머지.
- **R1.3**: ABI 0.25 grammar와 0.20 grammar 혼재 시 런타임 panic. *완화*: 모든 grammar를 같은 ABI 라인으로 통일.

---

## 2. Go Concurrency Analysis (Goroutine/Channel/Mutex)

### Motivation
- 현 CKG는 Go 코드의 *제어 흐름은 무시*. 그러나 Go 프로젝트에서 goroutine/채널/뮤텍스는 데이터 흐름과 동작 제어의 핵심.
- 이미 `pkg/types/`에 `Goroutine`, `Channel` 노드 + `spawns/sends_to/recvs_from` 엣지가 정의돼 있어 *추출 로직만 추가*하면 됨.
- 차별화 포인트: graphify(tree-sitter only)는 채널 방향성·뮤텍스 페어링을 추출 못 함. CKG가 `go/types` + `go/ssa`로 풀면 압도적 우위.

### Design

#### Stage 1: AST 휴리스틱 (Phase 1 default-on)
신규 파일: `internal/parse/golang/concurrency.go`

| 패스 | 입력 AST | 추출 결과 | 신뢰도 |
|------|----------|-----------|--------|
| Goroutine spawn | `*ast.GoStmt` | `Goroutine` 노드 + `spawns(caller_func, target_func)` edge | EXTRACTED |
| Channel decl | `*ast.ChanType`, `make(chan T, ...)` 호출 | `Channel` 노드 (방향성·elem 타입 포함) | EXTRACTED |
| Channel send | `*ast.SendStmt` (`ch <- v`) | `sends_to(func, channel)` edge | EXTRACTED |
| Channel recv | `*ast.UnaryExpr{Op:ARROW}` (`<-ch`) | `recvs_from(func, channel)` edge | EXTRACTED |
| Select branch | `*ast.SelectStmt` | 각 case를 채널과 연결 | EXTRACTED |
| Mutex Lock | `mu.Lock()` (selector + types.Info로 `*sync.Mutex`/`*sync.RWMutex` 확인) | `acquires_lock(func, mutex)` edge | EXTRACTED |
| Mutex Unlock | `mu.Unlock()`, `defer mu.Unlock()` | `releases_lock(func, mutex)` edge | EXTRACTED |
| Critical section | Lock~Unlock 사이 변수 접근 (`*ast.Ident` 해석) | `accessed_under_lock(field, mutex)` edge | INFERRED (Stage 1) → EXTRACTED (Stage 2) |

핵심 구현 포인트:
- `types.Info.ObjectOf(ident)`로 같은 변수임을 *타입 시스템*에서 확인. 이름 매칭만으로는 false positive.
- `defer mu.Unlock()` 패턴은 Go 표준 라이브러리 90%+에서 발견 — 이걸 우선 처리하면 가성비 최고.
- channel 방향성은 *타입*에서 결정: `chan<- T`(send-only), `<-chan T`(recv-only), `chan T`(양방향).

#### Stage 2: SSA 기반 정밀 분석 (Phase 2, opt-in)
- 진입: `ckg build --deep` 또는 `--analyze concurrency`
- 의존: `golang.org/x/tools/go/ssa`, `golang.org/x/tools/go/callgraph` (이미 `tools` 모듈에 포함, 추가 의존 없음)
- 추출 강화:
  - cross-function lock chain (`f`가 lock된 상태에서 `g`를 호출 → `g`도 critical section 내)
  - early-return 후 명시 unlock 패턴 (defer 없는 경우)
  - lock 없이 접근되는 공유 변수 자동 플래깅 (`is_potential_race: true` 노드 속성)
- 비용: 대형 모노레포에서 빌드 시간 2~5배. 따라서 default off.

### Schema 추가 (`pkg/types/`)

신규 노드 타입 (또는 속성 압축):
```go
// option A: 신규 NodeType
NodeMutex NodeType = "Mutex"  // sync.Mutex, sync.RWMutex 인스턴스 (필드/지역변수)

// option B: 기존 Variable/Field에 속성으로 압축
type Node struct {
    // ...
    IsLock         bool   `json:"is_lock,omitempty"`
    LockKind       string `json:"lock_kind,omitempty"` // "mutex" | "rwmutex"
}
```
**권장**: option A (신규 `Mutex` 타입). 시각화에서 별도 색/모양으로 구분 가능, 쿼리 단순.

신규 엣지 타입:
```go
EdgeAcquiresLock     EdgeType = "acquires_lock"
EdgeReleasesLock     EdgeType = "releases_lock"
EdgeAccessedUnderLock EdgeType = "accessed_under_lock"
```
* 기존 22개 → 25개로 확장.

신규 노드 속성:
```go
Function.IsConcurrentSafe *bool  // 모든 공유 접근이 lock 하에 있으면 true
Channel.Direction        string  // "send" | "recv" | "bidi"
Channel.ElemType         string  // 타입 이름
Channel.BufferSize       int     // 0=unbuffered
```

### Files Affected
```
internal/parse/golang/concurrency.go         (신규: Stage 1 AST 패스)
internal/parse/golang/concurrency_test.go    (신규)
internal/parse/golang/declarations.go        (수정: Mutex 타입 인식)
internal/parse/golang/parser.go              (수정: concurrency pass 호출)
internal/parse/golang/dataflow.go            (신규, Stage 2: SSA 패스, --deep 진입점)
internal/parse/golang/dataflow_test.go       (신규, Stage 2)
pkg/types/node.go                            (수정: Mutex NodeType 추가, Channel/Function 속성 추가)
pkg/types/edge.go                            (수정: 3종 엣지 추가)
internal/persist/schema.sql                  (수정: nodes/edges 테이블 컬럼 추가, schema_version bump)
internal/cluster/topic_tree.go               (선택: concurrency 토픽을 별도 community로)
docs/SCHEMA.md                               (수정: 새 노드/엣지 문서화)
testdata/concurrency/                        (신규: goroutine/channel/mutex 시나리오 fixture)
```

### Acceptance Criteria
**Stage 1 (필수)**:
- [ ] `go func() {...}()` 1회당 `Goroutine` 노드 1개 + `spawns` edge 1개.
- [ ] `make(chan int, 10)` → `Channel` 노드 (direction=bidi, elem=int, buffer=10).
- [ ] `defer mu.Unlock()` 패턴이 있는 함수의 모든 변수 접근에 `accessed_under_lock` edge.
- [ ] `testdata/concurrency/` fixture 빌드 결과의 노드/엣지 count가 expected와 일치.
- [ ] viewer에서 동시성 엣지를 별도 색상으로 토글 가능.

**Stage 2 (--deep)**:
- [ ] cross-function lock 전파 추적 (lock된 f가 g 호출 시 g 컨텍스트도 lock).
- [ ] race candidate (`is_potential_race`) 노드 추출 + 검증.

### Risks
- **R2.1**: AST 휴리스틱의 false positive (다른 변수 같은 이름). *완화*: `types.Info.ObjectOf` 필수 사용.
- **R2.2**: `sync.RWMutex.RLock()`은 read-only critical section — 일반 Lock과 다른 의미. *완화*: 별도 edge 종류(`acquires_rlock`) 또는 속성으로 구분.
- **R2.3**: 임베디드 mutex (`type S struct { sync.Mutex }; s.Lock()`) 추적. *완화*: `types.Info`로 메서드 receiver 해석.
- **R2.4**: SSA 빌드 비용으로 빌드 시간 폭증. *완화*: Stage 2 default off, `--deep` opt-in.

---

## 3. Storage Backend: PostgreSQL Option

### Motivation
- 현재 SQLite는 *단일 사용자 + 단일 머신* 시나리오에 최적. 다음 시나리오는 SQLite 한계:
  - 팀 단위 코드 검색 서버 (다중 사용자 동시 접근)
  - RAG 통합 (코드 임베딩 저장 + 벡터 유사도 검색)
  - 영속적 그래프 호스팅 (CI마다 빌드 → 항상-켜진 PG로 push)
- Neo4j는 라이선스(Enterprise 유료) + 운영 부담으로 제외.
- PostgreSQL은 무료(permissive license), pgvector·Apache AGE로 벡터·Cypher까지 한 백엔드에서 처리 가능.

### Design

#### Storage 추상화
신규: `internal/persist/store.go`

```go
package persist

type Store interface {
    Open(dsn string) error
    Close() error

    UpsertNodes(nodes []types.Node) error
    UpsertEdges(edges []types.Edge) error

    GetNode(id string) (*types.Node, error)
    FindByName(name string, lang string, exact bool) ([]types.Node, error)
    SearchText(query string) ([]types.Node, error)

    GetSubgraph(rootID string, depth int) (*types.Graph, error)
    GetCallers(qname string, depth int) ([]types.Node, error)
    GetCallees(qname string, depth int) ([]types.Node, error)

    SaveTopics(topics []types.Topic) error
    GetTopicsForNode(nodeID string) ([]types.Topic, error)

    Tx(fn func(tx Tx) error) error
    SchemaVersion() string
}
```
- 기존 `internal/persist/sqlite.go`는 `Store` 구현체로 정리.
- 신규 `internal/persist/postgres.go`가 PostgreSQL 구현체.
- 기존 호출부(`internal/server/`, `internal/mcp/`, `internal/buildpipe/`)는 `Store` 인터페이스만 의존하도록 리팩터링.

#### Phase 1: `ckg export-postgres`
- 진입: `ckg export-postgres --dsn postgres://user:pass@host/db --source ./graph.db`
- 동작: SQLite에서 읽고 PostgreSQL에 push (one-shot).
- 활용: 빌드는 로컬 SQLite로, 검색은 팀 PG 서버로.
- graphify의 Cypher/Neo4j export와 동일한 패턴.
- 의존: `github.com/jackc/pgx/v5`.

#### Phase 2: PostgreSQL을 primary store로 선택 가능
- 진입: `ckg build --db postgres://...` (default는 여전히 SQLite).
- DDL: `internal/persist/postgres-schema.sql` (FTS5 → tsvector + pg_trgm으로 변환).
- recursive CTE는 PG에서 더 빠름 (subgraph 쿼리 자연스럽게 매핑).

#### Phase 3: pgvector + Apache AGE 통합
- **pgvector**: 코드 임베딩(함수/클래스 단위)을 nodes 테이블의 추가 컬럼에 저장. `get_context_for_task`(δ-mode)가 *Leiden 클러스터 + 벡터 유사도 + 텍스트 BM25*를 한 쿼리로 결합.
  ```sql
  SELECT n.*, embedding <=> $1 AS distance
  FROM nodes n
  WHERE n.topic_id = ANY($cluster_ids)
  ORDER BY distance LIMIT 50;
  ```
- **Apache AGE**: 가변 길이 패턴 매칭이 필요해질 때 Cypher 사용.
  ```cypher
  MATCH (a:Function {qname: $qname})-[:calls*1..5]->(b:Function)
  WHERE b.is_potential_race = true
  RETURN b
  ```
- 둘 다 *옵션 확장*. 미설치 시 기본 SQL로 fallback.

### Files Affected
```
internal/persist/store.go              (신규: Store interface)
internal/persist/sqlite.go             (수정: Store 구현)
internal/persist/postgres.go           (신규)
internal/persist/postgres_test.go      (신규: testcontainers-go 활용)
internal/persist/postgres-schema.sql   (신규)
internal/persist/migrate.go            (신규: SQLite → PG 마이그레이션 로직)

cmd/ckg/export_postgres.go             (신규: Phase 1)
cmd/ckg/build.go                       (수정: --db 플래그 추가, Phase 2)
cmd/ckg/serve.go                       (수정: Store interface 사용)
cmd/ckg/mcp.go                         (수정: 동일)

internal/server/server.go              (수정: persist.Store 의존성)
internal/mcp/tools.go                  (수정: 동일)
internal/buildpipe/pipeline.go         (수정: 동일)

go.mod                                 (의존: jackc/pgx/v5, testcontainers/postgres-go optional)
docs/STORAGE.md                        (신규: SQLite vs PG 가이드)
```

### Acceptance Criteria
**Phase 1 (export only)**:
- [ ] `ckg export-postgres --dsn ... --source graph.db`로 동일한 노드/엣지가 PG에 적재.
- [ ] PG에서 동일한 `find_callers`/`find_callees` 쿼리가 SQLite와 동일 결과.

**Phase 2 (primary)**:
- [ ] `ckg build --db postgres://...` 직접 빌드 정상.
- [ ] 모든 기존 테스트가 SQLite·PG 양쪽에서 통과 (testcontainers-go).
- [ ] viewer/MCP 모두 백엔드 무관하게 동작.

**Phase 3 (pgvector/AGE)**:
- [ ] pgvector 설치 시 `get_context_for_task`가 벡터 유사도 결합.
- [ ] AGE 설치 시 `--cypher "MATCH ..."` MCP 툴 추가.

### Risks
- **R3.1**: SQL 방언 차이로 쿼리 두 벌 유지 비용. *완화*: `database/sql` placeholder 표준화, 특수 쿼리만 백엔드별 분기.
- **R3.2**: PG 미설치 환경에서 테스트 환경 부담. *완화*: testcontainers-go로 도커 자동화, CI matrix.
- **R3.3**: 임베딩 생성 비용/모델 선택. *완화*: Phase 3에서 별도 결정. v0.2 범위는 PG 적재까지.
- **R3.4**: 다중 사용자 동시 빌드 시 race. *완화*: 빌드는 단일 writer 락(advisory lock) 보장.

---

## 4. File-Level Incremental Cache

### Motivation
- 현재는 빌드마다 전체 재파싱. 대형 레포에서 비효율.
- graphify는 SHA256 file hash로 변경 파일만 재처리 → CKG도 동일 패턴 도입.
- 추가 이득: incremental parsing(Item 1 Phase 1c)과 결합 시 IDE-grade 응답성.

### Design

#### 캐시 키 설계
```
cache_key = sha256(
    file_content +
    "|ckg:" + ckg_version +              // CKG 자체 변경 무효화
    "|parser:" + parser_version_for_lang + // grammar 또는 go-tree-sitter 버전
    "|schema:" + schema_version           // 추출 스키마 minor 버전
)
```
*이유: 코드 동일해도 추출 로직 변경 시 캐시는 무효. silent corruption 방지.*

#### manifest.json 스키마 v2
```json
{
  "ckg_version": "0.2.0",
  "schema_version": "1.1",
  "built_at": "2026-04-28T10:00:00Z",
  "files": [
    {
      "path": "internal/foo/bar.go",
      "language": "go",
      "sha256": "abc123...",
      "mtime": 1714291200,
      "parser_version": "go/types@1.25.5",
      "node_ids": ["n_001", "n_002"],
      "edge_ids": ["e_010", "e_011"],
      "referenced_by": ["internal/baz/qux.go"]
    }
  ]
}
```
- `node_ids` / `edge_ids`: 파일별 생성물 추적 → 변경 시 정확한 invalidation.
- `referenced_by`: **reverse-reference index**. 이 파일을 import/call하는 파일들. Pass 2 invalidation의 핵심 (Phase 2).

#### 빌드 흐름
```
1. (fast path)  mtime 미변경  → 캐시 히트, SHA256 스킵
2. (slow path)  mtime 변경    → SHA256 재계산
                              → manifest의 sha256과 동일? → 캐시 히트, manifest mtime만 갱신
                              → 다르면 → 변경 파일로 분류
3. 변경 파일 처리:
   - SQLite에서 해당 파일의 node_ids/edge_ids 삭제
   - 재파싱 → 노드/엣지 재삽입
   - manifest 갱신
4. Pass 2 (cross-file resolve):
   - Phase 1: 전체 재실행 (단순)
   - Phase 2: referenced_by 추적해 영향받는 파일들의 pending ref만 재처리
5. PageRank/Leiden:
   - 노드/엣지 변동량 ≥ 1% OR --rebuild-metrics → 재계산
   - 미만 → 기존 메트릭 유지
6. 빌드 통계 출력:
   "Cache: 487 hits, 13 misses, parsed 13 files, saved 0.3s"
```

#### Phase 분리
| Phase | 범위 | 가치 | 구현 비용 |
|-------|------|------|-----------|
| 1 | 파일 단위 SHA256 + 변경 파일만 재파싱 + Pass 2 전체 재실행 | **90%** (graphify 파리티) | 낮음 |
| 2 | reverse-reference 인덱스 + Pass 2 부분 invalidation | 모노레포에서 추가 50% 단축 | 중간 |
| 3 (선택) | 함수 단위 body_hash, IDE/LSP용 sub-second update | LSP 통합 시점에 | 높음 |

**v0.2는 Phase 1 + Phase 2 완료를 목표.** Phase 3는 v0.4+에서 평가.

### Files Affected
```
internal/persist/manifest.go              (신규 또는 확장: schema v2)
internal/persist/manifest_test.go         (신규)
internal/buildpipe/incremental.go         (신규: 변경 감지 + invalidation)
internal/buildpipe/incremental_test.go    (신규)
internal/buildpipe/pipeline.go            (수정: incremental path 분기)
internal/persist/sqlite.go                (수정: 노드/엣지 부분 삭제 메서드)
internal/persist/store.go                 (수정: 부분 삭제 인터페이스)
cmd/ckg/build.go                          (수정: --no-cache, --rebuild-metrics 플래그)
docs/INCREMENTAL.md                       (신규: 캐시 동작 설명)
```

### Acceptance Criteria
**Phase 1**:
- [ ] 빌드 후 동일 입력 재빌드 시 0개 파일 재파싱 (전부 캐시 히트).
- [ ] 1개 파일 수정 후 재빌드 시 1개 파일만 재파싱.
- [ ] mtime만 변경되고 내용은 동일한 경우(git checkout 시나리오) 재파싱 안 함.
- [ ] CKG 버전 bump 시 전체 캐시 무효화.
- [ ] `--no-cache` 플래그로 강제 전체 재빌드 가능.

**Phase 2**:
- [ ] A.go가 B.go 함수를 호출, B.go만 변경 시 A.go의 엣지가 정확히 갱신.
- [ ] reverse-reference 인덱스가 manifest에 정확히 기록.

### Risks
- **R4.1**: mtime 비신뢰 (git checkout으로 거꾸로 갈 때). *완화*: mtime 변경 시에도 SHA256까지 가야 캐시 히트 결정.
- **R4.2**: schema_version bump 누락 시 silent corruption. *완화*: 스키마 변경 PR 체크리스트에 schema_version 갱신 필수 항목 추가.
- **R4.3**: reverse-reference 인덱스 누락 시 stale 엣지. *완화*: Pass 2가 변경 파일의 *직접 참조*뿐 아니라 *역참조*까지 봐야 함을 강제하는 통합 테스트.
- **R4.4**: 캐시 파일(manifest.json) 손상 시 빌드 실패. *완화*: 손상 감지 시 `--no-cache` 자동 fallback + 경고.

---

## 5. Roadmap & Phasing

### Release Plan

**v0.2.0 — Foundation (parser + cache)**
- Item 1 Phase 1a + 1b (smacker 제거 완료)
- Item 5 Phase 1 (file-level cache)
- Item 4 Storage abstraction (`Store` interface) — 구현체는 SQLite만, PG는 v0.2.1
- Schema bump: 1.0 → 1.1 (concurrency 엣지 정의 자리만 예약, 추출은 v0.2.1)

**v0.2.1 — Concurrency (Stage 1) + PG Export**
- Item 2 Stage 1 (AST 휴리스틱 동시성)
- Item 4 Phase 1 (`ckg export-postgres`)
- Item 1 Phase 1c (incremental parsing 인프라)

**v0.2.2 — Incremental Pass 2 + PG Primary**
- Item 5 Phase 2 (reverse-reference invalidation)
- Item 4 Phase 2 (`ckg build --db postgres://...`)

**v0.3.0 (별도 spec) — Concurrency Stage 2 + RAG**
- Item 2 Stage 2 (SSA, `--deep`)
- Item 4 Phase 3 (pgvector + Apache AGE)

### Cross-Cutting Tasks
- 빌드 호환성: 각 phase가 독립 PR로 머지 가능, downstream 호환 유지.
- 문서: `docs/SCHEMA.md`, `docs/STORAGE.md`, `docs/INCREMENTAL.md` 동기화.
- 평가: `ckg eval` 베이스라인이 새 동시성 엣지를 인지하는지 검증 (rubric 갱신).

---

## 6. Decision Log

| ID | 결정 | 이유 | 대안 (기각) |
|----|------|------|------------|
| D1 | smacker → upstream 마이그레이션 | pseudo-version 의존 위험, ABI 검증, incremental API | smacker 유지 (정체된 의존성 누적) |
| D2 | tree-sitter는 fork하지 않고 upstream 그대로 사용 | thin core, fat extensions 원칙. fork는 장기 유지비 폭증 | go-tree-sitter fork에 동시성 분석 추가 (잘못된 레이어) |
| D3 | 동시성 분석은 `internal/parse/golang/concurrency.go`로 분리 | Go 코드는 `go/types` 의존이라 tree-sitter와 무관, 책임 분리 | tree-sitter 패스에 통합 (타입 정보 부재로 정확도 저하) |
| D4 | Stage 1은 AST + types.Info, Stage 2는 SSA opt-in | 90%는 휴리스틱이 잡고, SSA는 비용이 큼 | 처음부터 SSA only (default-on은 빌드 시간 부담) |
| D5 | PostgreSQL은 *옵션 백엔드*, default는 SQLite | 단일 바이너리 정체성 유지 | PG primary 강제 (배포 복잡도 증가) |
| D6 | Neo4j는 채택하지 않음, Apache AGE로 Cypher 호환 | Neo4j Enterprise 비용, 운영 부담 | Neo4j 직접 채택 (라이선스/운영) |
| D7 | DuckDB/Kuzu는 v0.2 범위 외 | PG가 RAG/팀 시나리오까지 한 번에 커버 | DuckDB primary (OLAP 강점은 있으나 다중 사용자 약함) |
| D8 | 캐시는 파일 단위 우선, 함수 단위는 v0.4+ | 90% 가치를 20% 비용으로. cross-file invalidation이 함수 단위의 이득을 잠식 | 함수 단위부터 시작 (Pass 2 복잡도 폭증) |
| D9 | 캐시 키에 ckg/parser/schema 버전 모두 포함 | silent corruption 방지 | content hash만 (스키마 변경 시 stale data) |
| D10 | reverse-reference 인덱스는 *처음부터* manifest에 포함 | Phase 2 진입 시 리팩터링 비용 회피 | Phase 1 단순화 후 Phase 2에서 추가 (마이그레이션 부담) |

---

## 7. Open Questions

1. **Solidity grammar 출처**: JoranHonig/tree-sitter-solidity? 다른 fork? Phase 1b 진입 시 결정.
2. **Mutex Node 표현**: 신규 NodeType vs Variable 속성 압축? Stage 1 구현 시 fixture로 양쪽 시각화 비교 후 결정.
3. **임베딩 모델**: pgvector 도입 시 어떤 임베딩 모델? (OpenAI text-embedding-3-small vs sentence-transformers vs Voyage AI). v0.3 spec에서 별도 결정.
4. **PG 마이그레이션 전략**: SQLite ↔ PG 양방향 마이그레이션? 또는 PG는 항상 export 결과만? v0.2.2 Phase 2 진입 시 결정.
5. **schema_version 정책**: semantic versioning(1.0 → 1.1)? 또는 단조 증가 정수? `internal/persist/schema.sql` 헤더에 명시 필요.

---

## References
- `docs/ARCHITECTURE.md` — 아키텍처 개요
- `docs/SCHEMA.md` — 노드/엣지 스키마 (v0.2.0에서 1.1로 bump 예정)
- `pkg/types/node.go`, `pkg/types/edge.go` — 타입 정의
- `internal/parse/golang/parser.go` — Go 파서 (concurrency pass 추가 대상)
- `internal/persist/sqlite.go` — 현 스토리지 (Store interface로 추상화 대상)
- 외부:
  - tree-sitter/go-tree-sitter (upstream)
  - jackc/pgx/v5 (PostgreSQL driver)
  - pgvector/pgvector (벡터 확장)
  - apache/age (Cypher on PG)
  - golang.org/x/tools/go/{ssa,cfg,callgraph} (Stage 2 동시성)
