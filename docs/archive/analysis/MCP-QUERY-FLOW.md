# MCP Query Flow 검토

> **대상**: `ckg mcp --graph=<dir>` stdio 서버 — LLM 클라이언트(Claude Code 등)가 코드베이스 컨텍스트를 조회하는 6 도구의 동작 흐름
> **참조 파일**:
> - `cmd/ckg/mcp.go`
> - `internal/mcp/{server,tools,get_context}.go`
> - `internal/persist/sqlite.go` (Search, NeighborhoodByQname, SubgraphByQname, GetBlob 등)
>
> **선행 문서**: `docs/CODE-STRUCTURE.md` §8 (요약), `docs/ARCHITECTURE-DETAILED.md` §5
> **마지막 갱신**: 2026-05-05

> ⚠️ **Honest assessment**: MCP 도구 표면은 6개로 구성되어 있으나, **smart 도구(`get_context_for_task`)의 BM25 점수는 진짜 BM25가 아니라 rank reciprocal로 근사**되어 있고, `eval`에서 사용하는 δ 경로는 **실제 MCP 도구와 다른 simplification(SearchFTS만 호출)**을 사용합니다. 본 문서는 이 차이를 § 4와 § 6에서 분명히 합니다.

---

## 목차

1. [진입 (CLI Entry)](#1-진입-cli-entry)
2. [Server 구조](#2-server-구조)
3. [Granular 5 도구](#3-granular-5-도구)
4. [Smart 도구 — `get_context_for_task`](#4-smart-도구--get_context_for_task)
5. [응답 envelope 통일성](#5-응답-envelope-통일성)
6. [현재 한계 / Gap (Critical)](#6-현재-한계--gap-critical)
7. [근본 원인 후보 (디버깅 시 우선 검사)](#7-근본-원인-후보-디버깅-시-우선-검사)
8. [핵심 한 줄 요약](#8-핵심-한-줄-요약)

---

## 1. 진입 (CLI Entry)

`ckg mcp --graph=<dir>`:

```go
// cmd/ckg/mcp.go (요지)
store := persist.OpenReadOnly(filepath.Join(graphDir, "graph.db"))
mcp.Run(ctx, store)
```

- `OpenReadOnly`로 SQLite를 read-only 핸들로 열기 → 동시 build와 충돌 안 함
- store는 `persist.StoreReader` 인터페이스 (서명상 write 메서드 보이지 않음)
- `mcp.Run` 종료 조건: stdin EOF (= LLM client 종료)

PostgreSQL backend는 현재 `serve`/`build`에서만 옵트인. **mcp 서브커맨드는 아직 `--db` 플래그 미노출** (확인 필요시 `cmd/ckg/mcp.go` 직접 점검).

---

## 2. Server 구조

`internal/mcp/server.go`:

```go
func Run(ctx context.Context, store persist.StoreReader) error {
    s := server.NewMCPServer("ckg", "0.1.0")

    registerFindSymbol(s, store)
    registerFindCallers(s, store)
    registerFindCallees(s, store)
    registerGetSubgraph(s, store)
    registerSearchText(s, store)
    registerGetContextForTask(s, store)

    return server.ServeStdio(s)
}
```

- `mark3labs/mcp-go v0.49` SDK 사용
- transport: **stdio JSON-RPC 2.0** (HTTP/SSE 미지원)
- 6 tool은 모두 동일 store 인스턴스를 공유 → 멀티스레드 안전성은 SQLite 자체의 동시 read에 의존
- `textResult(payload)` = `mcp.NewToolResultStructured(payload, "")` — JSON 본문 + 빈 텍스트 보조

---

## 3. Granular 5 도구

### 3.1 `find_symbol`

| Field | Value |
|---|---|
| Input | `name`(req), `language`?, `exact`(default true), `include_blobs`(default false) |
| Backend | `store.FindSymbol(name, lang, exact)` |
| Algo | exact `qualified_name` 또는 `name` 매칭 (exact=false면 suffix-LIKE) |
| Output | `{ "nodes": [...] }` (attachBlobs 가공) |

### 3.2 `find_callers`

| Field | Value |
|---|---|
| Input | `qname`(req), `depth`(default 1), `include_blobs`(default false) |
| Backend | `store.NeighborhoodByQname(q, d, reverse=true, "calls", "invokes")` |
| Algo | qname → seed node ID → reverse BFS depth=d → edge type 필터 (calls, invokes만) |
| Output | `{ "nodes": [...], "edges": [...] }` |

`callEdgeTypes = ["calls", "invokes"]` — 의도적으로 `rpc_calls`는 제외 (RPC는 별도 의미론). `contains/defines`는 자동 제외되어 "파일이 caller로 나오는" 노이즈 방지.

### 3.3 `find_callees`

3.2와 동일한 backend, `reverse=false` (forward BFS).

### 3.4 `get_subgraph`

| Field | Value |
|---|---|
| Input | `seed_qname`(req), `depth`(default 2), `include_blobs`(default false) |
| Backend | `store.SubgraphByQname(q, d)` |
| Algo | seed → 양방향 BFS depth=d → 모든 edge type 포함 |
| Output | `{ "nodes": [...], "edges": [...] }` |

`get_subgraph`는 edge type 필터 **없음** → contains/defines/calls 모두 따라가서 큰 그래프 반환 가능. depth 컨트롤이 매우 중요.

### 3.5 `search_text`

| Field | Value |
|---|---|
| Input | `query`(req), `top_k`(default 10), `language`?, `include_blobs`(default false) |
| Backend | `store.Search(q, top_k)` (smart router) |
| Algo | ASCII 쿼리 → FTS5 BM25 + auto-prefix; CJK 쿼리 → LIKE substring fallback |
| Output | `{ "nodes": [...] }` |

`Search`는 `nodes_fts` 가상 테이블을 사용. 인덱스 대상 컬럼: `name`, `qualified_name`, `signature`, `doc_comment`. **소스코드 본문(blob) 자체는 FTS index에 들어가지 않음** — 식별자 검색은 강하지만 자유 텍스트 query는 약함.

### 3.6 응답 가공: `attachBlobs`

```go
func attachBlobs(store, nodes, include) []map[string]any {
    for each node:
        m := { id, type, name, qname, file, line, confidence, signature, usage_score }
        if include: m["source"] = store.GetBlob(id)   // 에러는 silent ignore
        out = append(out, m)
}
```

**Silent error swallow**: Package 노드는 source blob이 없어서 `sql.ErrNoRows`가 정상. 하지만 다른 종류의 에러도 동일하게 무시됨 — DB 손상 등도 silent.

---

## 4. Smart 도구 — `get_context_for_task`

### 4.1 인풋

| Field | Default | 의미 |
|---|---|---|
| `task_description` (required) | — | 자연어 task 설명 |
| `budget_tokens` | 8000 | 응답 token 상한 (chars/4 heuristic) |
| `language` | (any) | 미사용 (선언만 있고 buildContext에 전달 X) |
| `include_blobs` | true | 본문 inline 여부 |
| `max_bodies` | 5 | full source 노드 최대 개수 |

⚠️ `language` 파라미터는 **선언되어 있으나 사용되지 않음** (`buildContext`에 전달되지 않고 closure에서만 RX). 실제 필터 효과 없음.

### 4.2 알고리즘 (`buildContext`)

```
(a) Retrieve:
    cands := store.Search(query, 30)
    └─ 결과 0건 → not_found=true 반환

(b) Expand 1-hop:
    ids := [cand.ID for cand in cands]
    moreEdges := store.QueryEdgesForNodes(ids)
    expIDs := union(ids, moreEdges.Src ∪ moreEdges.Dst)
    expanded := store.NodesByIDs(expIDs)

(c) Score-fuse:
    bm25Rank[id] = 1 / (rank+1)        // ⚠️ 진짜 BM25가 아니라 rank reciprocal
    maxPR, maxUS := 1e-9 floor
    for each n in expanded:
        score = 0.5 * bm25Rank[n.ID]    // 0이면 1/inf 효과 = 0
              + 0.3 * (n.PageRank / maxPR)
              + 0.2 * (n.UsageScore / maxUS)
    sort desc

(d) Diversify:
    if len(rows) > 30: rows = rows[:30]
    └─ ⚠️ "diversify"라고 부르지만 실제론 단순 top-30 cap. 클러스터/중복 제거 없음.

(e) Pack within budget:
    tokens := estimateTokens(query)
    bodies := []
    summaries := []
    for i, r in rows:
        if i < maxBodies and includeBlobs:
            b := store.GetBlob(r.n.ID)
            if cost(b) + tokens > budget: break
            bodies.append({id, qname, source: b})
            tokens += cost
            continue
        if len(summaries) >= 15: continue
        cost := estimateTokens(signature + " " + doc_comment)
        if tokens + cost > budget: continue
        summaries.append({id, qname, signature, doc})
        tokens += cost
```

### 4.3 응답 envelope

```json
{
  "task_description": "<query>",
  "subgraph": {
    "nodes": [{ id, name, type, qname, score }, ...],
    "edges": [[src, dst, type], ...]   // ← compact triples (4-tuple 아님 — line 빠짐)
  },
  "bodies":   [{ id, qname, source }, ...],
  "summaries":[{ id, qname, signature, doc }, ...],
  "tokens_estimated": 7234,
  "trimmed": false,
  "not_found": false  // 결과 0건일 때만 true
}
```

---

## 5. 응답 envelope 통일성

| 도구 | nodes 필드 | edges 필드 |
|---|---|---|
| find_symbol | `nodes: []` (attachBlobs map) | (없음) |
| find_callers | `nodes: []` + `edges: []` (raw types.Edge) | line 포함 |
| find_callees | 동일 | 동일 |
| get_subgraph | 동일 | 동일 |
| search_text | `nodes: []` | (없음) |
| get_context_for_task | `subgraph.nodes: [{score 포함}]` | `subgraph.edges: [[src,dst,type]]` (line 누락) |

**일관성 부족**: 6 도구 중 5개는 `nodes: [...]` 평면 구조, smart 도구는 `subgraph.{nodes,edges}` + `bodies` + `summaries`로 다른 envelope. LLM client 측 파서 분기 필요.

---

## 6. 현재 한계 / Gap (Critical)

### 6.1 알고리즘 정확성 한계

| 항목 | 실제 동작 | 의도/spec | 영향 |
|---|---|---|---|
| **BM25 점수** | `1/(rank+1)` rank reciprocal | 진짜 BM25 score | 첫 결과가 항상 1.0 → 점수 분포 왜곡 |
| **Diversify** | top-30 단순 cap | per-cluster cap | 같은 파일 함수가 다 들어올 수 있음 |
| **language 필터** | smart 도구는 무시 | language별 격리 | 멀티-언어 corpus에서 노이즈 |
| **`Stale` 체크** | mcp 서버는 없음 | freshness 알림 | LLM이 stale graph로 답할 위험 |
| **에러 swallow** | `attachBlobs`/`smart bodies` | 명시 reporting | DB 이상 silent miss |

### 6.2 Eval과 MCP의 비대칭

`internal/eval/runner.go:smartContext`:

```go
func smartContext(store, query) (string, error) {
    hits, _ := store.SearchFTS(query, 10)   // ⚠️ 단순 FTS만
    return jsonString(hits), nil
}
```

vs MCP의 `buildContext` (50+ lines: retrieve → expand → score-fuse → diversify → pack).

⇒ **eval δ 베이스라인이 측정하는 "smart context"는 MCP가 실제로 LLM에게 주는 smart context가 아닙니다.** 같은 코드 경로를 공유하지 않음. 주석에 "Should be moved into a shared package in V1"이라 명시.

### 6.3 도구별 미세 한계

| 도구 | 미세 한계 |
|---|---|
| `find_symbol` | `language` 필터는 적용되지만 exact=true의 의미가 store 내부 구현에 의존 (suffix LIKE OR exact) |
| `find_callers/callees` | `callEdgeTypes` = `[calls, invokes]`만 — `rpc_calls` 의도적 제외 (별도 도구로 안 노출됨) |
| `get_subgraph` | edge type 필터 없음 → temporal/cluster 엣지까지 따라감 — `seed_qname=""` `depth=99`이면 전체 그래프 반환 가능 (eval β가 정확히 이 호출) |
| `search_text` | FTS index에 source body 미포함 → "deposit handling logic" 같은 자연어 query는 식별자에 그 단어가 들어있지 않으면 miss |
| `get_context_for_task` | `subgraph.edges`는 line 누락 → 정확한 호출 위치 추적 불가 |

### 6.4 Concurrency / Race

- `OpenReadOnly`로 read 동시성은 OK
- 그러나 **`ckg build`가 동일 graph.db에 동시 실행되면** SQLite 잠금 가능성 — mcp 서버 실행 중 build는 write contention 발생할 수 있음
- B3 (incremental parsing) / G6 v4가 partial-cache를 활성화한 후엔 더 위험 — 현재는 D4 fallback이라 cold만이라 wipe-replace로 race 발생 시점이 작지만 0은 아님

### 6.5 graph 연결 미설정 시 silent fail

- `--graph` 디렉토리 안에 `graph.db`가 없으면 `OpenReadOnly` 단계에서 에러 → mcp.Run 진입 전 종료
- 하지만 `--db postgres://...` 옵션이 mcp 서브커맨드에 노출되지 않음 → PG backend로 build한 후 mcp는 사용 불가
  - 대응: `cmd/ckg/mcp.go` 확장 필요 (현재 SQLite path만 받음)

---

## 7. 근본 원인 후보 (디버깅 시 우선 검사)

### R1. **`find_callers`가 0건을 반환한다**
원인 후보:
- (a) qname이 DB에 정확히 일치하지 않음 (Go의 `pkg.Type.Method` 형식 vs TS의 `ClassName.method` 형식)
- (b) caller 함수의 `calls` 엣지 자체가 emit되지 않은 언어 (TS/Sol — 위 TS-SOL 문서 §5 참조)
- (c) Pass 2 Resolve에서 미해소된 ref가 silent drop됨 (V0 simplification)

검증:
```sql
SELECT id, qualified_name FROM nodes WHERE name = 'methodName';
SELECT * FROM edges WHERE dst = '<found_id>' AND type IN ('calls','invokes');
```

대응: qname을 정확히 입력하거나 `find_symbol`로 먼저 ID 확인.

### R2. **`get_context_for_task` 결과가 빈약하거나 엉뚱하다**
원인 후보:
- (a) 자연어 query가 식별자/주석/시그니처에 들어있지 않음 → FTS5 miss → not_found=true
- (b) PageRank/usage_score가 모두 0이라 (작은 corpus) bm25 비중만 효과 → 첫 30개 검색 결과만 채택
- (c) `language` 파라미터 무시 → 의도하지 않은 언어 노드도 후보에 들어옴
- (d) max_bodies=5라 첫 5개에 적합한 자료가 없으면 summary로 떨어짐

검증:
```bash
# 직접 Search 호출 결과 보기
sqlite3 graph.db "SELECT name, qualified_name FROM nodes_fts(?) LIMIT 30" "<query>"
```

대응: query에 식별자 키워드 명시. budget_tokens/max_bodies 조정.

### R3. **`get_subgraph(seed='', depth=99)` 시 응답이 거대 / 크래시**
원인: empty seed qname이라 모든 노드를 seed로 잡고 depth=99로 BFS → 메모리/응답 크기 폭발.
검증: `len(nodes)` 측정. eval β baseline이 이 호출 → eval 자체가 LLM 응답 token 폭증 발생 가능.
대응: depth는 보수적으로(2~3), seed_qname 항상 명시.

### R4. **MCP server stdio가 hang하거나 응답 안 함**
원인 후보:
- (a) Claude Code 등 client가 JSON-RPC framing을 NDJSON 외 형식으로 보냄 (mcp-go v0.49는 NDJSON 기반)
- (b) `server.ServeStdio`는 stdin EOF까지 block — TTY에서 직접 실행 시 input이 닿지 않음
- (c) 매우 큰 응답 (`get_subgraph` 등)에서 stdout buffer overflow 가능성

검증: `ckg mcp --graph=... < /dev/null` → 즉시 EOF로 종료되면 정상.
대응: Claude Code 통합으로만 사용. 디버그시 `--log-file`로 JSON 로그 확인.

### R5. **stale graph로 잘못된 답변**
원인: mcp 서버는 빌드 시점의 graph.db만 보며 staleness 알림 미설정. 소스 변경 후 rebuild 누락하면 LLM이 옛 정보 참조.
검증: `sqlite3 graph.db "SELECT value FROM manifest WHERE key='build_timestamp'"` vs 소스 mtime.
대응: 빌드 후 mcp 서버 재시작. 또는 `serve` API의 staleness banner를 별도 호출로 사전 검증.

### R6. **`include_blobs=true`인데 source가 안 들어있다**
원인 후보:
- (a) 노드가 Package(blob 없음 — 정상)
- (b) blobs 테이블 자체가 비어있음 (build 시 `RebuildFTS` 실패 또는 InsertBlobs skip)
- (c) `attachBlobs`의 silent error swallow가 다른 에러 가림

검증: `sqlite3 graph.db "SELECT COUNT(*) FROM blobs"` → 0이면 build 단계 문제.
대응: `--no-cache` 재빌드.

### R7. **eval δ 결과가 mcp 도구와 다르다**
원인: § 6.2에서 설명한 비대칭. `eval δ`는 SearchFTS 10건 dump, MCP는 retrieve+expand+score-fuse+pack.
검증: 두 결과를 토큰/내용 비교.
대응: V1+ 영역. 단기적으론 결과 비교 시 인지하고 해석.

### R8. **PG backend로 build한 graph에 mcp 사용 불가**
원인: `cmd/ckg/mcp.go`가 `--graph=<dir>/graph.db`만 받음. PG DSN 옵션 미노출.
검증: `ckg mcp --help` → `--db` 플래그 없으면 확정.
대응: 코드 변경 필요 (B2/C2 활성화 후속).

---

## 8. 핵심 한 줄 요약

> **MCP는 6 도구로 제공되지만, smart 도구의 "BM25"는 rank reciprocal, "diversify"는 단순 cap이라 알고리즘 표면은 정직하지 않습니다.** Eval δ baseline은 MCP의 실제 smart context와 **다른 simplified 경로(SearchFTS 10건만)**를 사용하므로 H1/H2 가설 검증의 근거가 약합니다. PG backend는 build/serve에만 노출되었고 mcp 서브커맨드는 SQLite path 한정이며, language 필터는 smart 도구에서 무시됩니다.

---

## Appendix: 도구별 backend 메서드 매핑

| 도구 | StoreReader 메서드 | 추가 호출 |
|---|---|---|
| find_symbol | `FindSymbol(name, lang, exact)` | `GetBlob` (선택) |
| find_callers | `NeighborhoodByQname(q, d, reverse=true, edgeTypes...)` | `GetBlob` |
| find_callees | `NeighborhoodByQname(q, d, reverse=false, edgeTypes...)` | `GetBlob` |
| get_subgraph | `SubgraphByQname(q, d)` | `GetBlob` |
| search_text | `Search(q, top_k)` | `GetBlob` |
| get_context_for_task | `Search` + `QueryEdgesForNodes` + `NodesByIDs` + `GetBlob` | (5개 메서드 fan-out) |

각 도구당 평균 1~5회 SQLite query → 작은 corpus(<1M nodes)에서 <100ms 응답. 큰 corpus + 깊은 depth에선 BFS 비용 선형 증가.

**End of MCP query flow analysis.**
