# Self-Graph Baseline (P0-1)

> **측정 시점**: 2026-05-06, lenient validate 도입 직후 (HEAD 미커밋)
> **명령**: `./bin/ckg build --src=. --out=/tmp/ckg-self --no-cache --lang=go`
> **목적**: T1-A/T1-B/T1-C/P0-2/P0-3/P0-4/P0-5/P1-1 적용 전 oracle.

## 헤드라인

- **Total**: 11,415 nodes / 52,527 edges (Go production + tests)
- **Empty/null violations**: 0 across nodes(id/name/qname/file_path), edges(src/dst/confidence), blobs(source) ✅
- **Lenient validate dropped**: 7 dangling `listens_on` (root cause 미규명, 별도 fix backlog)
- **Build time**: ~4s

## Node breakdown

| Type | Count | 비고 |
|---|---|---|
| CallSite | 5,488 | G3 |
| IfStmt | 1,921 | G3 |
| ReturnStmt | 1,182 | G3 |
| Function | 520 | G1/G2 |
| Import | 504 | G1 |
| LoopStmt | 462 | G3 |
| Field | 291 | G2 |
| Method | 240 | G1/G2 |
| Variable | 224 | G2 |
| Commit | 151 | G6 |
| File | 135 | G1 |
| Constant | 113 | G2 |
| Struct | 77 | G2 |
| SwitchStmt | 44 | G3 |
| Package | 37 | G1 |
| **Interface** | **7** | G2 — 모든 인터페이스 검출 ✅ |
| Goroutine | (낮음, 테스트만) | G4 |
| Channel | (낮음, 테스트만) | G4 |

## Edge breakdown

| Type | Count | 비고 |
|---|---|---|
| changed_in | 39,367 | G6 |
| contains | 9,234 | G1 |
| calls | 1,674 | G3 |
| defines | 1,489 | G1 |
| imports | 624 | G1 |
| blame | 135 | G6 |
| recvs_from | 2 | G4 (test fixture만, production 0) |
| spawns | 2 | G4 (test fixture만, production 0) |
| **implements** | **0** | ⚠️ G2 — 7개 인터페이스 모두 미연결 |
| listens_on | 0 | (7건 lenient drop) |
| handles_message | 0 | — |
| rpc_calls | 0 | — |
| accessed_under_lock | 0 | (production에 sync 없음) |
| reads_field / writes_field | 0 | — |

## 핵심 발견

### Critical gap 1: `implements` edges = 0
- Interface 7개 검출됨 (parse.Parser / audit.store / eval.LLMClient / persist.TopicTreeInput / StoreReader / StoreWriter / Store)
- 구현체(sqliteStore / pgStore / goParser / tsParser / solParser / cliClient / apiClient ...)는 emit되지만 **`implements` edge 자체가 한 건도 emit되지 않음**.
- 사용자가 언급한 "인터페이스 기반의 코드를 실제 실행 코드와도 연결" 기능의 *정량 증거*: V0에서 **interface satisfaction 분석 미구현**.
- Go의 implicit interface satisfaction은 정적 분석이 까다로움 → P3 follow-up.

### Critical gap 2: G4 (concurrency) edges가 production에 0
- spawns / sends_to / recvs_from 모두 *test fixture에서만* emit (production code = 0)
- T1-A에서 Pass 1 parsing + DB writer를 channel/goroutine 기반으로 추가하면 *자기 분석 시 G4 edges가 emit*되는지 검증 가능.
- 검증 후 G4 detector 자체의 정확도 측정 가능.

### Lenient validate dropped (root cause backlog)
- `listens_on: 7` dangling. server.go의 `s.mux.HandleFunc(...)` 7건이 출처로 추정.
- `idForFunc`의 fset/offset 매핑 또는 `resolveHTTPHandlerArg` typed mode resolution 둘 중 하나에 버그 (Task #10 별도 추적).

## 후속 작업 비교 시 사용할 기준값

| 측정값 | Baseline | 변경 후 기대 |
|---|---|---|
| Total nodes | 11,415 | ≥ 11,415 (decrease 시 regression) |
| Total edges | 52,527 | ≥ 52,527 |
| Empty value violations | 0 | 0 (반드시 유지) |
| Dangling drops | 7 listens_on | 0 또는 ≤7 (Task #10 fix 시) |
| spawns / sends_to / recvs_from in production | 0 | > 0 (T1-A 후) |
| implements | 0 | 0 (이번 사이클 변경 안 함, P3 follow-up) |
| MCP smart context BM25 algorithm | `1/(rank+1)` rank reciprocal | real Okapi BM25 |
| eval.smartContext == mcp.buildContext | false | true |

**End of baseline. Source of truth: `/tmp/ckg-self/graph.db` 파일 (재생성 가능).**
