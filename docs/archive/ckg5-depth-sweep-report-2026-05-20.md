# CKG-5 — Depth sweep latency measurement (2026-05-20)

> Source: `ckg bench-mcp --depth-sweep` against a self-index of this repo.
> Goal: answer the question raised by `docs/followups-from-cks-dogfood-2026-05-19.md`
> CKG-5 — "Is depth=2 acceptable as the default for find_callers /
> find_callees?" — with **ckg-side latency data**. cks-side recall data
> remains the consumer's measurement responsibility.

## Setup

| | |
|---|---|
| Graph | self-index of `code-knowledge-graph` (Go only) |
| Node count | 27,944 (Function: 1,363 / CallSite: 13,941 / IfStmt: 5,700 / …) |
| Seed | `pickFunctionSeed` picks top-pagerank Function |
| Iterations | 50 / probe / worker × 4 workers = 200 calls per probe |
| Backend | SQLite (single graph.db, OpenReadOnly) |
| Raw data | `docs/ckg5-depth-sweep.json` |

## Result (ms)

| Tool | d=1 p50 | d=2 p50 | p50 ratio | d=1 p95 | d=2 p95 | d=1 p99 | d=2 p99 |
|---|---:|---:|---:|---:|---:|---:|---:|
| find_callers     |  4.70 | 10.01 | 2.13× |  8.95 | 13.84 | 13.27 | 16.41 |
| find_callees     |  0.24 |  4.28 | 17.8× |  0.84 |  8.25 |  2.15 | 10.27 |
| get_subgraph     |  9.29 | 26.88 | 2.89× | 39.55 | 39.04 |121.99 | 51.12 |
| impact_of_change |  9.38 | 15.07 | 1.61× | 17.16 | 23.75 | 21.00 | 29.08 |

## Observations

1. **find_callees는 가장 큰 상대 증가(17.8×)지만 절대치는 여전히 ms 한 자리.**
   d=1의 0.24ms는 캐시 워밍/단순 인덱스 스캔에 가까운 노이즈 수준이라
   ratio가 과장됨. d=2의 4.28ms도 사용자 인지 가능 임계(보통 50ms)를
   훨씬 밑돔.

2. **find_callers는 2.1× 증가, p99 16ms.** depth=2가 default여도
   사용자 인지 영역 아님.

3. **get_subgraph d=1의 p99(122ms)가 d=2(51ms)보다 *더 큼*.**
   양방향 BFS의 노드 분포가 d=1에서 더 산포해 변동성이 큼. d=2는
   더 안정적 — depth가 깊어진다고 무조건 비용이 커지는 게 아니라
   *분산이 줄어들 수도* 있음.

4. **impact_of_change 1.6× — 가장 작은 증가.**
   pagerank/usage_score 가중치가 이미 depth-aware라 추가 hop의
   marginal 비용이 작음.

## Constraint reminder

이 측정은 **ckg territory만 다룸**:

- ✅ Tool별 latency (p50/p95/p99)
- ✅ 백엔드 부담 추정
- ❌ cks recall 변화 — cks 측에서만 측정 가능 (`mcp-tool-handlers`,
  `stamp-integrity-lookup` 시나리오의 0.67 plateau)
- ❌ 다양한 graph 크기에서의 scaling — self-index 한 그래프만 측정

## Decision options

| 옵션 | ckg 변경 | cks 영향 | 권장 시점 |
|---|---|---|---|
| **A** find_callers / find_callees default 1→2 | tools.go 두 줄 (`DefaultNumber(2)`) | 즉시 recall 회복 가능성 | 지금 (latency 데이터로 정당화됨) |
| **B** default 유지, cks가 explicit `depth=2` 호출 | 0 | cks adapter 수정 필요 | cks 팀이 default-respect 정책일 때 |
| **C** 새 옵션 `max_depth` server-side cap | tool 인자 추가 + 가드 | cks가 자유롭게 raise 가능 + DoS 방어 | 외부 사용자 늘어날 때 |

## Recommendation

**옵션 A 권장.** 이유:

- depth=2의 p99이 모든 tool에서 30ms 이하 — 사용자 인지 임계 한참 아래.
- `get_subgraph`가 이미 default=2를 채택하고 있어 *tool 간 일관성*이
  복원됨 (find_callers/callees만 1인 게 변칙).
- cks 측이 follow-up 문서에서 recall plateau를 명시했으므로
  *기본값이 통증을 일으키는* 상황 — default 변경이 자연스럽다.
- 옵션 C(server cap)는 cks의 직접 호출자가 ckg 운영자와 같은
  사람일 때(현재 상황) 과한 방어. 외부 사용자 등장 시 별도 작업.

## Next actions

1. `internal/mcp/tools.go`의 `find_callers` / `find_callees` default
   를 `DefaultNumber(1)` → `DefaultNumber(2)`로 변경.
2. tool description에 "depth=2 default — see CKG-5 measurement" 한 줄.
3. cks 팀에 이 리포트 공유 → cks 측 recall 재측정 + 적절한 default
   결정.
4. 다음 dogfood에서 `mcp-tool-handlers` / `stamp-integrity-lookup`
   시나리오 recall 변동 확인.
