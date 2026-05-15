# 실행 전략 — CKG 작업을 CKS 맥락에서 효과적으로 진행

> 작성일: 2026-05-11
> 참조: stablenet-ai-agent의 `EXECUTION-GUIDE.md` §3.2 (S0 acceptance), `04-cks-deep-dive.md` §7·§11·§12
> 본 문서: HANDOFF.md 10 task를 상위 프로젝트 맥락에 맞춰 재정렬·우선순위 재산정한 실행 전략.
> HANDOFF.md = *무엇을* / 본 문서 = *어떻게·언제·왜*

## 1. 상위 프로젝트에서의 CKG 위치

### 1.1 CKG는 CKS의 *최하층 부품*

`04-cks-deep-dive.md`가 정의하는 CKS 4-layer 구조:

```
Layer 4 — Query API (MCP / HTTP / CLI)
Layer 3 — Retrieval Orchestrator ("Pager") + Playbook
Layer 2 — Working Memory Store
Layer 1 — Storage Backends                ← CKG는 여기
            ├ Graph DB (6 graphs)         ← 본 repo의 주 산출물
            ├ Vector DB                   (CKV — 별도 repo, 미생성)
            ├ BM25 / Full-text
            ├ AST cache
            └ File + Blob store
```

### 1.2 현재의 임시 책임 분담

S0/S1 사이 과도기에 CKG는 *Layer 1만이 아니라 Layer 4 capability 일부*까지 노출:

| Capability | CKS 본래 위치 | 현재 CKG 구현 | S1 이후 이관 대상 |
|---|---|---|---|
| `find_symbol` | Layer 4 low-level | `internal/mcp/tools.go` | 유지 (light, raw 그래프 접근) |
| `find_callers` | Layer 4 light | `internal/mcp/tools.go` | 유지 |
| `find_callees` | Layer 4 light | `internal/mcp/tools.go` | 유지 |
| `get_subgraph` | Layer 4 low-level (graph_query 변형) | `internal/mcp/tools.go` | internal-only로 강등 가능 |
| `impact_of_change` | Layer 4 medium | `internal/mcp/impact.go` | 유지 |
| `search_text` | Layer 4 low-level (bm25_search) | `internal/mcp/tools.go` | internal-only로 강등 |
| `get_context_for_task` | **Layer 3 Orchestrator** | `pkg/smartctx` | **CKS 이관** — Playbook + CKV fusion 들어가야 |
| `evidence_for_intent` | Layer 3 Orchestrator | `pkg/evidence` | **CKS 이관** — sanitize Step 8.5 적용 필요 |

→ 즉 *상위 2개 도구*(`get_context_for_task`, `evidence_for_intent`)는 *CKG에 남으면 안 되는* 기능. S1 진입 시 CKS로 옮김.

### 1.3 CKG가 *책임지는* KPI (§11)

CKS deep dive §11이 정의한 10개 KPI 중 **CKG가 책임지는 것**:

| KPI | CKG 책임 | 현재 측정 상태 |
|---|---|---|
| **Citation accuracy** (file:line 실재율) | ★ 직접 | **우리 §A에서 1차 확인 — 100%** |
| **Symbol lookup precision** | ★ 직접 | 부분 측정 (V0 채점기 한계) |
| **Cross-language link recall** | ★ 직접 (xlang_calls edge) | **미측정** (Go 단일 언어 검증만) |
| **Incremental index time** | ★ 직접 | **미측정** (`--no-cache` vs 증분 비교 없음) |
| Average tokens per query | 간접 (Layer 1 응답 크기) | 부분 측정 |
| Query latency p95 | 직접 | `bench-mcp-stdio` 결과 있음 |
| Index freshness lag | 직접 | 미측정 |
| Token budget hit rate | 간접 (Layer 3 책임) | n/a |
| Semantic search relevance@10 | 부분 (BM25만, CKV 부재) | 미측정 |
| Working memory hit rate | n/a (Layer 2 책임) | n/a |

**관찰**: 우리가 측정한 것은 **Citation accuracy 1건 + Query latency**. 7개 KPI는 미측정이며 *현재의 30-question 골든셋 디자인으로는 모두 측정 가능하지 않음*.

## 2. HANDOFF 10 task 재분류 — CKS 맥락 기준

S0(CKG) 영역에서 처리할 것 vs S1(CKS) 단계에서 다시 봐야 할 것:

| ID | 제목 | HANDOFF 우선 | CKS 맥락 우선 | 머무를 곳 | 이유 |
|---|---|---|---|---|---|
| T-03 | 위치 정확도 validator | P0 | **★ P0** | S0 | §11 Citation accuracy의 *측정 기구* — CKG가 가장 책임 큰 KPI |
| T-04 | Hallucination validator | P0 | **★ P0** | S0 | T-03과 동일 KPI의 반대 표현 |
| T-02 | extractSymbols 정규화 | P0 | **P0** | S0 | Symbol lookup precision 측정 정확도. 단 LLM-judge로 대체 시 S1로 이관 가능 |
| **T-14** | CKG public 표면 정리 (`pkg/mcphandlers/`) | — (신규) | **★ P0** | S0 | **CKS S1 진입의 차단 요소** — 핸들러를 import 가능하게. §10 |
| T-09 | FTS sigil bypass | P1 | **P1** | S0 | Layer 1 BM25 강건성 — CKG 책임 |
| T-07 | dumpFiles _test.go | P1 | **P1** | S0 | 측정 V0 신뢰성 — *현 측정 인프라가 CKG에 있으므로* S0에서 마무리 |
| T-08 | dumpFiles 무작위화 | P1 | **P1** | S0 | T-07과 동시 |
| **T-15** | task ↔ known-issues JSONL 동기화 도구 | — (신규) | P1 | S0 | Q2 결정의 결과 — 양쪽 저장 규약 유지 |
| T-01 | γ tool-loop emulation | P0 | **P2 (S1로 이관)** | **S1 (CKS)** | tool-loop은 CKS 측정 인프라가 가짐. CKG는 `runner.go:106`에 명시 코멘트만 |
| T-10 | smartctx prose 강건성 | P2 | **★ P0 (S1에서)** | **S1 (CKS)** | `get_context_for_task`는 CKS Layer 3로 이관. CKG `pkg/smartctx`는 *deprecate-friendly stable* 유지 |
| T-05 | LLM backend 가동 | P0 | **P0** | 환경 | **확정**: api backend (`ANTHROPIC_API_KEY`) |
| T-06 | 27 task 추가 | P1 | **P1** | S0/S1 공용 | **확정**: YAML + JSONL 양쪽 (§6.2 규약) |

### 2.1 핵심 권고: 작업 *경계*를 분명히

- **CKG에 머무를 task만 손댄다 (S0)**: T-02, T-03, T-04, T-07, T-08, T-09
- **S1 이관 후보는 *기능 강화 금지***: T-01, T-10
  - T-01: 지금 emulation 추가하지 말고, `runner.go:106`에 *명시적 TODO 코멘트*만 강화. CKS S1에서 진짜 tool-loop 구현
  - T-10: 지금 smartctx 강건화 작업 금지. CKS S1에서 *Playbook + CKV fusion*과 함께 재설계

> *이유*: CKG에서 Layer 3 기능을 보강하면 S1 시작 시 CKS Repo로 옮길 때 **rewrite + 검증 재실행**. 작업 두 번. 차라리 S0에서 CKG의 Layer 1 정확도와 KPI 측정 기구에 집중.

## 3. EXECUTION-GUIDE.md S0 Acceptance와 매핑

`EXECUTION-GUIDE.md §3.2`의 S0 acceptance 3개 항목을 우리 검증 자료와 매핑:

| S0 Acceptance | 현재 상태 | 남은 작업 |
|---|---|---|
| Go-stablenet 코드베이스 indexing 완료, indexing 시간 + 결과 reasonable | **✅ PASS** — 121K nodes / 399K edges, 빌드 ~45s. `BUILD_SOURCE_FILES.md` 781 파일 화이트리스트 사용 | indexing 시간 *trend* 측정 (Incremental index time KPI) — T-신규 |
| 임의 함수에 대해 `find_callers` 호출 시 **직접·간접 caller 모두 반환** | **부분 PASS** — 직접 (depth=1) 정확 확인. **간접 (depth>1) 미검증** | depth=2, depth=3 회귀 test 추가 — T-신규 |
| `impact_of_change` 결정적 (동일 입력 → 동일 출력) | **PASS (추정)** — SQL 기반 deterministic, 다만 *결정성 회귀 test 부재* | 결정성 단위 test 추가 — T-신규 |

→ **추가 task 3개 발견**:
- **T-11**: indexing 시간 trend KPI 측정 (`Incremental index time`)
- **T-12**: `find_callers` depth>1 회귀 test (간접 caller 검증)
- **T-13**: `impact_of_change` 결정성 회귀 test

## 4. 권장 작업 순서 — 4 phase

### Phase 1 — S0 closeout (1주 ~ 1.5주, P0 작업)
*목표*: EXECUTION-GUIDE S0 acceptance 완전 충족 + KPI 측정 기구 마련 + CKS import 표면 정리.

순서:
1. **T-05** LLM backend 가동 (api backend, `ANTHROPIC_API_KEY` 확인 — 30분)
2. **T-14** CKG public package 표면 정리 — `pkg/mcphandlers/` 신설, MCP 도구 핸들러 export (2~3일). **CKS S1 진입 전 차단 요소**
3. **T-12** `find_callers` depth>1 회귀 test (1일)
4. **T-13** `impact_of_change` 결정성 회귀 test (반나절)
5. **T-03** 위치 정확도 validator (3일)
6. **T-04** Hallucination validator (T-02 토큰 추출 공유, 3일)
7. **T-02** `extractSymbols` 정규화 + 단위 test (2일)

Phase 1 종료 조건:
- S0 acceptance 3 항목 모두 자동 회귀 test로 보장
- Citation accuracy / Hallucination 측정기가 임의 LLM 응답에 동작 (T-03·T-04)
- `pkg/mcphandlers`가 CKS에서 import 가능 (T-14 dry-run: 빈 CKS repo에서 `go mod tidy` + 1 도구 register 성공)
- 5개 task로 1차 KPI baseline 측정 가능

### Phase 2 — KPI baseline 1차 측정 (3일)

순서:
1. 기존 3 task + 빠르게 큐레이션한 추가 2 task (총 5 task)로 4 방식 측정 실행
2. raw_output 회수 → 위치 정확도 / hallucination / 정답률 / 토큰 산출
3. 측정 보고서 1차 작성 (이것이 EXECUTION-GUIDE §5.1 regression scoring harness의 시드)

Phase 2 종료 조건:
- 5 task × 4 방식 = 20 측정점 raw_output 보존
- 4 지표 모두 baseline 값 산출 (제대로 측정 불가한 지표는 *왜*를 기록)

### Phase 3 — S0 인프라 결함 정리 (1주, P1 작업)

순서:
1. **T-07** dumpFiles _test.go 제외 + 회귀 test
2. **T-08** dumpFiles 무작위화 (deterministic seed)
3. **T-09** FTS sigil bypass 완화 + 회귀 test
4. **T-11** Incremental index time KPI 측정 인프라

Phase 3 종료 조건:
- α/β 두 baseline 측정 신뢰성 ↑
- 5 KPI 측정 가능 (Citation + Symbol precision + Latency + Index time + Cross-lang은 별도)

### Phase 4 — 30 task 콘텐츠 확장 + S1 진입 (병행)

순서:
1. **T-06** 27 task YAML 추가 작성 — `eval/stablenet/tasks/T*.yaml`
2. **T-15** YAML → JSONL 동기화 도구로 `stablenet-ai-agent/benchmark/known-issues.jsonl` 생성 (§6.2 규약)
3. 30 task × 4 방식 = 120 측정점 → `Report.md` 산출
4. *동시에* S1 (CKS) 진입 준비:
   - CKS repo (`/Users/.../code-knowledge-system`) `go mod init github.com/0xmhha/code-knowledge-system`
   - CKG를 `require github.com/0xmhha/code-knowledge-graph` import
   - CKV를 `require github.com/0xmhha/code-knowledge-vector` import
   - T-01·T-10은 CKS의 S1 plan에 task로 등록 (HANDOFF.md에 명시된 위치 그대로)

Phase 4 종료 조건:
- 30-question 골든셋 측정 완료, Report.md 산출
- CKS repo `go.mod`가 CKG·CKV 둘 다 import 성공 (smoke build)
- CKS S1 plan에 T-01·T-10이 task로 포함

## 5. 검증 Cadence (반복 사이클)

`EXECUTION-GUIDE.md §4`의 LLM 작업 요청 패턴을 그대로 따름:

| 단계 | 빈도 | 검증 방법 |
|---|---|---|
| Task 1건 완료 | 1일 1~3건 | 단위 test + 회귀 test (해당 task의 acceptance) |
| Phase 종료 | Phase당 1회 | KPI 측정 1회 실행 + 직전 측정 대비 trend |
| S0 → S1 전환 | 1회 | EXECUTION-GUIDE §3.2 S0 acceptance 3항목 + KPI baseline 보고 |
| S1 진입 | 1회 | CKS repo 초기화 (S1 plan 문서가 가이드) |

핵심 원칙 (EXECUTION-GUIDE §4.3):
- **Phase boundary에서만 사용자 검토** — task 단위 매번 보고 X
- **메인 컨텍스트는 plan/decision/검증만** — 실제 코딩은 subagent
- **한 task = 한 commit**, PR은 Phase 또는 logical group 단위

## 6. 결정 사항 (2026-05-11 확정)

| # | 결정 | 영향 |
|---|---|---|
| **Q1** | **api backend** (`ANTHROPIC_API_KEY`) — cli wrapper 의존 제거 | T-05 단순화. 토큰 분류 정확 — H1 가설 의미 있게 측정 가능 |
| **Q2** | **eval/stablenet/tasks/ + benchmark/known-issues.jsonl 둘 다** | CKG 자체 검증 YAML + agent 평가 JSON 분리 보관. 동기화 규약 필요 (§6.2) |
| **Q3** | **T-01 γ emulation → S1 (CKS) 이관**. CKG는 손대지 않음 | `runner.go:106`에 명시 코멘트만 강화. 진짜 tool-loop은 CKS에서 |
| **Q4** | **T-10 smartctx prose → S1 (CKS) 이관**. CKG는 손대지 않음 | `pkg/smartctx`는 *import 대상으로 stable* 유지. 강건성은 CKS Layer 3 Orchestrator에서 |
| **추가** | **import-based 아키텍처** — 코드 이동 X | CKS·CKV가 CKG를 `go.mod` import. CKG는 *외부 import 표면을 정돈*해야 함 (§10) |

### 6.1 CKS·CKV 프로젝트 현 상태

| Repo | 경로 | 상태 |
|---|---|---|
| **CKG** | `<workspace>/tools/code-knowledge-graph` | go.mod = `github.com/0xmhha/code-knowledge-graph`. 진행 중 |
| **CKV** | `<workspace>/tools/code-knowledge-vector` | **진행 중** — go.mod, sqlite-vec 의존성, cmd/internal/pkg 구조 + bin. 활발 개발 |
| **CKS** | `<workspace>/tools/code-knowledge-system` | **거의 빈 repo** — `.git`만 존재. S1 시작점. go.mod 미생성 |

### 6.2 27 task 양쪽 저장 규약 (Q2 결정에 따른 동기화)

| 저장 위치 | 포맷 | 용도 |
|---|---|---|
| `code-knowledge-graph/eval/stablenet/tasks/T*.yaml` | task YAML (현 스키마 — `internal/eval/task.go`) | CKG 자체 4-방식 baseline 측정. precision_recall / rubric |
| `stablenet-ai-agent/benchmark/known-issues.jsonl` | known-issue JSONL (EXECUTION-GUIDE §5.1 스키마 — `issue_id`, `pre_fix_commit`, `request`, `ground_truth`, `scoring`) | Coding Agent S2+ regression scoring |

동기화 규약:
- task ID 일치: `T01-newblockchain-callers.yaml` ↔ `issue_id: "T01-newblockchain-callers"`
- ground truth: YAML의 `expected.symbols` = JSONL의 `ground_truth.files_changed` (단, 표기 정규화 필요)
- 변경 시 양쪽 동시 수정 — `eval/stablenet/sync_tasks.py` 같은 도구로 검증 자동화 (T-15 신규)

### 6.3 Import 표면 정리에 따른 신규 task (§10에서 상세)

| ID | 제목 | 우선 | 위치 |
|---|---|---|---|
| **T-14** | CKG public package 표면 정리 — CKS가 import할 stable API 결정 | **P0** | CKG |
| **T-15** | task ↔ known-issues JSONL 동기화 도구 | P1 | CKG (검증 자료) |

## 7. CKG 측면 *해도 되는 것 / 하지 말 것* 정리

### 7.1 해야 할 것

- Layer 1 (Storage Backends)의 정확성 — 6 graph types의 edge 정확도, citation accuracy 100%
- Light/medium capability 안정성 — `find_symbol`, `find_callers`, `find_callees`, `impact_of_change`, `get_definition`
- KPI 측정 기구 — Citation accuracy, Symbol precision, Incremental index time, Cross-lang recall, Query latency
- §11 KPI의 *7개 미측정* 항목을 *측정 가능하게* 만들기

### 7.2 *지금* 손대지 말 것

- Layer 3 (Retrieval Orchestrator) — Playbook + token budget manager. `pkg/smartctx` 강화 금지
- Layer 4 sanitize Step 8.5 — CKS의 단일 진입점 원칙(§7 Architectural Rule). CKG에서 sanitize 도입 시 책임 분산
- Working Memory (Layer 2) — `remember_fact`, `record_decision`, `recall_session`. CKS 책임
- CKV 통합 — vector retrieval은 별도 repo
- BM25 fusion 알고리즘 변경 — CKS Layer 3에서 다시 짤 것

이 분리를 지키면 S1 진입 시 **rewrite 없이 import만** 가능. 안 지키면 S1 진입 시 CKG 일부 코드를 통째로 옮기거나 폐기.

## 8. 다음 액션 (이 문서를 본 직후)

1. ~~사용자 합의~~ — 확정됨 (§6)
2. **Phase 1 시작**:
   - T-05: `export ANTHROPIC_API_KEY=...` 확인 → `ckg-e val --llm-backend=api` 1 task smoke
   - T-14: 외부 import 표면 정리 (CKS S1 진입 차단 요소)
   - T-12·T-13: S0 acceptance closeout
   - T-03·T-04: KPI 측정기 (Citation accuracy / Hallucination)
   - T-02: extractSymbols 정규화
3. Phase 1 종료 시 §4 cadence에 따라 KPI baseline 1차 보고

## 10. Import-based 아키텍처 함의 (Q3·Q4·Q5 결정의 결과)

### 10.1 핵심 원칙

> **코드를 옮기지 않고 import한다** — CKS·CKV가 CKG를 `go.mod`로 import해서 *그 위에 layer를 얹는다*.

이는 단순한 import 그 이상의 의미:
- CKG는 *stable public API*를 책임진다 (semver — V1+에서 breaking change 신중)
- CKS는 CKG의 public 패키지만 사용 — `internal/*`는 import 불가 (Go 규칙)
- CKG의 `pkg/smartctx`, `pkg/evidence`는 *CKS가 직접 import해서 호출하거나 override 가능한 형태*로 유지

### 10.2 현재 CKG의 import 가능성 분석

| 패키지 | CKS 사용 의도 | 현 상태 | 조치 |
|---|---|---|---|
| `pkg/store` | Layer 1 그래프 접근 — *가장 핵심* | public, stable | 유지 |
| `pkg/types` | 노드/엣지 타입 | public, stable | 유지 |
| `pkg/bm25` | Layer 1 BM25 토크나이저 | public | 유지 |
| `pkg/impact` | Layer 4 medium capability | public | 유지 (interface 검토) |
| `pkg/smartctx` | Layer 3에서 *대체될 임시 구현* | public | **유지하되 deprecate 표시** — CKS Orchestrator가 superset 제공 시 import만 |
| `pkg/evidence` | Layer 3에서 *대체될 임시 구현* | public | smartctx와 동일 운명 |
| `internal/mcp/tools.go` | CKS MCP 서버가 *동일 도구 핸들러 재사용* | **internal — import 불가** | **T-14: 일부를 `pkg/mcphandlers` 같은 곳으로 export 이전** |
| `internal/persist` | sqlite 구현체 — Layer 1 부분 | internal | 유지 (CKS는 `pkg/store.Reader` 인터페이스만 의존) |
| `internal/buildpipe` | 빌드 파이프라인 | internal | 유지 (CKS는 `ckg build` CLI 호출) |

### 10.3 T-14 — CKG public package 표면 정리

**문제**: 현재 MCP 도구 핸들러(`registerFindSymbol`, `registerFindCallers` 등)는 `internal/mcp/tools.go`에 있어 CKS가 import해서 자체 MCP 서버에 등록할 수 없음.

**해결안 (3가지 검토)**:
1. **(권장)** 핸들러 함수를 `pkg/mcphandlers/`로 이전 — store reader만 받는 pure function 형태로 export. CKS MCP 서버는 import해서 자기 `*server.MCPServer`에 register
2. CKG의 `pkg/store.Reader` 인터페이스만 export 유지, CKS가 MCP 핸들러를 *처음부터* 다시 짬 (코드 중복)
3. CKG가 `cks-compat` 빌드 태그를 제공해 internal 일부를 internal-test로 export — 임시 방편

**Acceptance criteria (T-14)**:
- CKS의 `cmd/cks-mcp/main.go`에서 `import "github.com/0xmhha/code-knowledge-graph/pkg/mcphandlers"`로 `RegisterFindSymbol(server, store)` 같은 함수 호출 가능
- 기존 `ckg-mcp` 바이너리 동작 변경 없음 (회귀 test)
- `internal/mcp/server.go`는 `pkg/mcphandlers` 호출만 남기고 등록 로직 이전

### 10.4 CKS·CKV 관점에서 본 작업 분배

| 작업 | CKG에서 | CKS에서 | CKV에서 |
|---|---|---|---|
| `find_callers`, `find_callees`, `impact_of_change` 핸들러 | **구현 (T-14로 import 가능화)** | import해서 자기 MCP에 등록 + sanitize 적용 | n/a |
| `get_context_for_task` Orchestrator | smartctx *임시 구현* 유지 (deprecate) | **CKS Layer 3에서 Playbook + CKV fusion으로 재구현 (T-10)** | embedding query 응답 |
| BM25 tokenize / fts5 query | `pkg/bm25` 유지 | import해서 fusion 입력으로 | n/a |
| γ tool-loop emulation | **하지 않음** | CKS 측정 인프라에 emulation (T-01) | n/a |
| Vector retrieval | n/a | CKV import + 1차 검색 | **CKV 자체 구현 (별도 work)** |
| Sanitize Step 8.5 | **하지 않음** | CKS Layer 4 단일 진입점 | n/a |

### 10.5 Repo 경계 위반 alarm

다음 패턴이 보이면 *경계 위반* — code review에서 반려:
- CKG에 `import "github.com/0xmhha/code-knowledge-system/..."` (역방향 의존)
- CKG에 sanitize 룰셋 또는 redact 로직 추가
- CKG에 Working Memory (`remember_fact`, `record_decision`) 추가
- CKG에 vector store 의존성 추가
- CKG에 Playbook YAML 또는 token budget manager 추가

## 9. 참조

| 문서 | 본 전략에서의 역할 |
|---|---|
| `stablenet-ai-agent/claudedocs/EXECUTION-GUIDE.md` §3.2 | S0 acceptance 기준 (3 항목) |
| `stablenet-ai-agent/claudedocs/EXECUTION-GUIDE.md` §5.1 | regression scoring harness — 30-question 골든셋의 *공식 스키마* |
| `stablenet-ai-agent/claudedocs/04-cks-deep-dive.md` §7 | CKG가 노출하는 capability의 *향후 위치* |
| `stablenet-ai-agent/claudedocs/04-cks-deep-dive.md` §11 | CKG가 책임지는 KPI 10종 |
| `stablenet-ai-agent/claudedocs/04-cks-deep-dive.md` §12 | Bootstrap Indexer 5-Phase pipeline (CKG의 본래 범위) |
| `eval/stablenet/HANDOFF.md` | 본 전략이 재분류한 원본 10 task |
| `eval/stablenet/VERIFICATION_REPORT.md` | 본 전략의 근거가 되는 검증 결과 |
| `eval/stablenet/RESPONSE_VALIDATION_REPORT.md` | KPI 1차 측정 결과 (Citation accuracy 100%) |
