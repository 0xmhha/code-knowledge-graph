# CKS Spec ↔ CKG 구현 Gap 분석

> **목적**: CKS deep-dive spec(`04-cks-deep-dive.md`, 1,312줄, stablenet-ai-agent project)과 현재 CKG 구현의 정합성 검증.
> 사용자 의도(*"분석 프로젝트 경로 → 코드 + git diff → graph DB → LLM이 어디를 수정할지 정확히 찾도록 지원"*)가 어디까지 충족되는지 판정.
>
> **참조**:
> - spec: `/Users/0xtopaz/work/github/onlyhyde/study/projects/stablenet-ai-agent/claudedocs/04-cks-deep-dive.md`
> - 구현: `docs/CODE-STRUCTURE.md`, `docs/analysis/{GO-PROJECT-BUILD-FLOW,TS-SOL-BUILD-FLOW,MCP-QUERY-FLOW,EVAL-FLOW}.md`
>
> **마지막 갱신**: 2026-05-05
> **결론 한 줄**: CKG는 CKS spec의 약 **25%** 수준만 구현. spec이 가장 강조한 **Layer 3 Retrieval Orchestrator(Pager)**와 **Layer 2 Working Memory**가 통째로 빠져있어 사용자 핵심 의도가 실현되지 않음.

---

## 목차

0. [요약 진단](#0-요약-진단)
1. [Layer 1 — Storage Backends Gap](#1-layer-1--storage-backends-spec-§4-gap-매트릭스)
2. [Layer 2 — Working Memory](#2-layer-2--working-memory-spec-§5--통째로-미구현)
3. [Layer 3 — Retrieval Orchestrator](#3-layer-3--retrieval-orchestrator-pager--통째로-미구현)
4. [Layer 4 — Query API Gap](#4-layer-4--query-api-spec-§7-gap)
5. [Bootstrap Pipeline (spec §12)](#5-spec-§12-bootstrap-pipeline-5-phase-구현도)
6. [Runtime Evidence (spec §13)](#6-spec-§13-runtime-evidence-phase-2~3--통째로-미구현)
7. [Cross-Language Linking (spec §8)](#7-cross-language-linking-spec-§8-gap)
8. [Sanitize / Prompt-Injection (spec §6.2 Step 8.5)](#8-sanitize--prompt-injection-spec-§62-step-85--통째로-미구현)
9. [사용자 핵심 목적 대비 진단](#9-종합--사용자-핵심-목적-대비-진단)
10. [우선순위별 권장 작업 (P0 → P3)](#10-우선순위별-권장-작업-p0--p3)
11. [한 줄 결론](#11-한-줄-결론)

---

## 0. 요약 진단

CKS spec은 **4-Layer 아키텍처**를 정의:

```
Layer 4: Query API           (13 capabilities, 5 MCP tool groups)
Layer 3: Retrieval Orchestrator  ← ★ "가장 중요한 계층" (spec §6)
         (Task classify → Playbook → Backend select → RRF → Sanitize → Pack)
Layer 2: Working Memory      (run_id별 facts/decisions/Q&A cache, writeback)
Layer 1: Storage Backends    (Graph 6axis + Vector + BM25 + AST + File 5종)
```

### CKG 현 위치

| spec Layer | CKG 구현 상태 | 핵심 격차 |
|---|---|---|
| Layer 1 (Storage) | 🟡 **40%** — Graph DB(SQLite) + AST 일부 + File blob | **Vector DB 없음**, **BM25 없음** (FTS5는 별개) |
| Layer 2 (Working Memory) | 🔴 **0%** — 통째로 미구현 | run_id, facts, decisions, Q&A cache, recall_session 모두 없음 |
| Layer 3 (Retrieval Orchestrator) | 🔴 **5%** — score-fuse만 존재 | **Pager 자체가 없음**: task_type 분류, playbook, RRF, sanitize, citation enforcement, token budget 관리 모두 부재 |
| Layer 4 (Query API) | 🟡 **35%** — 6 MCP tools | 13 capability 중 6개만, 5 tool group 분리 X, mTLS/envelope 계약 없음 |

---

## 1. Layer 1 — Storage Backends (spec §4) Gap 매트릭스

### 1.1 6 Graph Axis 노드/엣지 spec 충실도

CKG의 `pkg/types/enums.go` (33 nodes × 30 edges)를 spec과 1:1 비교:

| Graph | spec edges | CKG 구현 | 누락된 edge |
|---|---|---|---|
| **G1 Structural** | contains / defined_in / imports / **configures** | contains / defines / imports / exports | `configures` (config key→func), ConfigKey 노드 ❌ |
| **G2 Semantic** | references / implements / extends / **overrides** / **tests** / **state_mutation** / reads / writes / emits / **consumes** / **handles** | references / implements / extends / uses_type / instantiates / reads_field / writes_field / reads_mapping / writes_mapping / emits_event / has_modifier / has_decorator | **`overrides`, `tests`, `state_mutation`, `consumes`, `handles` 5종 누락**. Test 노드 자체 ❌ |
| **G3 Execution** | calls / **returns** / **branches** / **modifies** / **timeout_path** / **retry_path** / **cancellation_path** | calls / invokes | **timeout/retry/cancellation/returns/branches 5종 누락** (bug fix playbook 핵심 자산) |
| **G4 Concurrency** | spawns / sends_to / receives_from / locks / unlocks / **waits_for** / **shares_state_with** | spawns / sends_to / recvs_from / acquires_lock / releases_lock / accessed_under_lock | **`waits_for`(WaitGroup/blocking), `shares_state_with`(goroutine↔goroutine) 2종 누락** |
| **G5 Distributed** | listens_on / handles_message / **xlang_calls** / rpc_calls / **p2p_broadcasts** / **consensus_flow** | listens_on / handles_message / rpc_calls / binds_to | **`p2p_broadcasts`, `consensus_flow` 누락**. binds_to는 spec의 xlang_calls를 약간 다른 의미로 emit |
| **G6 Temporal** | changed_in / blame / **correlated_with** / **observed_in** / **mentioned_in** | changed_in / blame | **`correlated_with`(incident↔log↔code), `observed_in`(런타임 실증), `mentioned_in`(issue/PR/ADR) 3종 누락** |

**총 30 spec edges 중 16개만 emit ≈ 53% coverage**. 결정적으로 **bug-fix playbook의 핵심 신호인 `timeout_path`/`retry_path`/`cancellation_path`가 모두 빠져있어**, spec §6.3 Bug Fix Playbook의 step 3(blame) 외 step들이 실제로는 동작하지 않음.

### 1.2 Storage backend 5종 (spec §4.2~4.6)

| Backend | spec 의도 | CKG 현 구현 | 격차 |
|---|---|---|---|
| **Graph DB** | Neo4j/AGE/KuzuDB 후보 | SQLite (modernc) + nodes/edges 테이블 | 🟢 OK (단순 단일-DB로 풀고 있음) |
| **Vector DB** | Qdrant/LanceDB/sqlite-vec, 코드 임베딩 | **❌ 없음** | semantic_search/similar_functions 모두 불가 |
| **BM25** | tantivy/bleve, 정확 토큰 매칭 | FTS5(SQLite 내장)로 흉내 | 부분: spec의 "regex 매칭", "TODO/FIXME 위치" 같은 grep-수준 정확성 미보장. BM25 score는 `1/(rank+1)` placeholder (MCP 분석 §6.1 참조) |
| **AST Cache** | tree-sitter 직렬화 + 버전별 캐시 | (직렬화 안 함, 매 빌드마다 reparse) | 🟡 cache_key가 있으나 AST 자체는 derived 후 버려짐 |
| **File/Blob** | git blob 활용 | `blobs` 테이블에 노드 source slice 저장 | 🟢 OK |

→ **Vector + BM25 부재가 critical**. spec §4.2는 "정확한 심볼명을 모를 때 첫 단계는 Vector"라고 명시. Bug fix/feature add playbook의 step 1(anchor)과 step 2(similar)가 모두 Vector를 전제로 함.

### 1.3 3수준 파서 파이프라인 (spec §4.7)

spec은 **수준 1 + 수준 2 + 수준 3** 통합을 명시:

| 수준 | spec | Go | TS/JS | Solidity |
|---|---|---|---|---|
| **수준 1** (tree-sitter AST) | 모든 언어 | (Go는 go/parser 사용) | ✅ | ✅ |
| **수준 2** (compiler/LSP) | gopls/go/packages, tsc, solc, slither | ✅ types.Info | ❌ tsc 미통합 | ❌ solc/slither 미통합 |
| **수준 2 동시성** | go/ssa + go vet | ❌ (D1 미구현) | ❌ | ❌ |
| **수준 3** (커스텀 분석기) | consensus handler / state machine / config↔code / timeout/retry | ❌ (E3에서 net/rpc 시그니처만) | ❌ | ❌ |

→ TS/Sol은 사실상 **수준 1만**. spec의 "Semantic +50%, Execution +50%, Concurrency +40%" 보강(수준 2)과 "Distributed +60%"(수준 3)이 모두 빠져있음. 이게 `docs/analysis/TS-SOL-BUILD-FLOW.md` §5에서 보고한 "TS/Sol은 declarations + imports만"의 근본 원인.

---

## 2. Layer 2 — Working Memory (spec §5) → **통째로 미구현**

spec §5는 다음을 명시:

```
{
  "run_id": "abc-123",
  "qa_cache": [...],          // query 중복 방지
  "facts": [...],              // remember_fact로 LLM 누적
  "decisions": [...],          // record_decision으로 결정 기록
  "touched_files": [...],
  "plan_state": { current_step, completed_steps, next_step }
}
```

**현 CKG에 대응하는 코드 0줄**:

| 항목 | 검증 명령 | 결과 |
|---|---|---|
| `run_id` 개념 | `grep -r "run_id" internal/` | 매치 없음 |
| `remember_fact` MCP tool | `grep -r "remember_fact" internal/mcp/` | 매치 없음 |
| `record_decision` MCP tool | `grep -r "record_decision" internal/mcp/` | 매치 없음 |
| `recall_session` MCP tool | `grep -r "recall_session" internal/mcp/` | 매치 없음 |
| `state-store/{run_id}/working_memory.json` | 없음 | — |

**영향**: spec이 강조한 **"명시적 writeback이 LLM의 메타인지를 강제한다"**(§5.4)는 효과를 전혀 얻을 수 없음. 동일 session에서 같은 query를 여러 번 던져도 캐시 hit이 없고, Resume(UC-D2)도 불가능. 사용자가 *"여러 번 같은 코드를 다시 분석해야 한다"*고 느끼는 원인 중 하나.

---

## 3. Layer 3 — Retrieval Orchestrator (Pager) → **통째로 미구현**

> spec §6 첫 줄: **"이 컴포넌트가 빠진 게 기존 초안의 가장 큰 결함이었다."**
> 현재 CKG는 정확히 그 결함을 가진 상태.

### 3.1 spec §6.2의 13단계 알고리즘 vs CKG의 `buildContext`

| spec step | spec 동작 | CKG 구현 | gap |
|---|---|---|---|
| **0. Task Type Classification** | bug_fix/feature_add/refactor/perf_optimization/concurrency_safety/io_reliability/security_review/architecture_explain 8종 분류 | ❌ 없음 | 모든 query를 동일 알고리즘으로 처리 |
| **0.5. Playbook Selection** | task_type → backends/hop_depth/budget_ratio 사전 정의 | ❌ 없음 | playbook 4종(§6.3) 자체가 미존재 |
| **1. Intent Classification** | symbol_lookup/impact/semantic/hybrid/structural/definition | ❌ 없음 | — |
| **2. Working Memory Lookup** | run_id별 cached lookup | ❌ 없음 (Layer 2 자체가 없음) | — |
| **3. Backend Selection (playbook 기반)** | bug_fix → [BM25, Graph(2-hop), Git(blame)] 등 | ❌ 단일 경로 (FTS5 → 1-hop) | hop_depth 동적 조정 X |
| **4. Freshness Check** | git rev-parse 비교 + on-demand reindex | 🟡 server.Staleness 존재하지만 mcp는 호출 안 함, eval만 git rev-parse | 부분 |
| **5. Parallel Backend Queries** | 여러 backend 병렬 + max_hops 제한 | ❌ 단일 backend (FTS5만) | 병렬화 X |
| **6. Result Fusion (RRF)** | Reciprocal Rank Fusion 검증 알고리즘 | ❌ score = 0.5·BM25(rank reciprocal)+0.3·PR+0.2·usage 단순 가중합 | RRF 아님 |
| **7. Reranking** | 선택적 reranker | ❌ 없음 | — |
| **8. Evidence Enrichment** | playbook별 추가 정보(recent_commits/concurrency_context/similar_functions...) | ❌ 없음 | Evidence Pack 형식 미존재 |
| **8.5. Sanitize Evidence** | <untrusted-*> sentinel + sanitization_rules.yaml + audit log + fail-closed | ❌ 없음 | prompt-injection 방어 0 |
| **9. Token Budget Allocation** | diversity quota(50/30/20), compression, citation 외 산정 | 🟡 단순 chars/4 + max_bodies cap만 | diversity quota X |
| **10. Citation Enforcement** | file:line 없는 결과 반환 금지 (hallucination 방지의 핵심) | ❌ 없음 | LLM이 잘못된 file:line 만들 수 있음 |
| **11. Writeback** | sanitized pack을 working_memory에 저장 | ❌ 없음 | — |

**13단계 중 실제 구현된 step ≈ 1.5단계**. spec §6.2의 의사코드 대부분이 누락.

### 3.2 Evidence Pack (spec §6.1) vs CKG 응답

spec의 Evidence Pack 16개 필드 vs CKG의 `get_context_for_task` 응답:

| Evidence Pack 필드 | CKG 응답 |
|---|---|
| `task_type` | ❌ |
| `goal` | ❌ |
| `candidate_files` | 🟡 nodes에 file_path만 있음 |
| `candidate_symbols` (kind, role) | 🟡 nodes에 type 있으나 role(bug_location/caller) ❌ |
| `relevant_edges` (with risk annotation) | 🟡 raw edges만, risk 주석 ❌ |
| `recent_commits` | ❌ |
| `existing_tests` | ❌ |
| `concurrency_context` (shared_state, locks, goroutines_involved) | ❌ |
| `constraints` | ❌ |
| `risks` | ❌ |
| `context_snippets` (with rank) | 🟡 bodies로 일부 |
| `tokens_used` | ✅ tokens_estimated |
| `metadata.freshness` | ❌ |
| `metadata.cache_hits` | ❌ |
| `metadata.warnings` | ❌ |
| `metadata.sanitize_report` | ❌ |

→ **사용자 의도("어디를 고쳐야 하는지 정확히 찾는다")의 핵심 정보(`risks`, `concurrency_context`, `recent_commits`, `existing_tests`)가 모두 빠져있음**. 이게 LLM이 *"어디를 고쳐야 하는지 자신 없게 답하는"* 직접 원인일 가능성이 높음.

### 3.3 Retrieval Playbooks 4종 (spec §6.3)

| Playbook | spec | CKG |
|---|---|---|
| Bug Fix (anchor→locate→blame→impact→tests→context) | 6 step yaml 정의 | ❌ |
| Feature Add (entry→similar→domain→boundary→tests) | 5 step | ❌ |
| Concurrency Safety (shared_state→concurrency→timeout→context→tests) | 5 step | ❌ |
| Architecture Explain (structure→entry_points→key_types→flow) | 4 step | ❌ |

→ Playbook 자체가 없으므로 사용자가 "버그 수정용 컨텍스트가 필요"라고 해도 CKG는 **모든 task에 대해 동일한 single-shot FTS top-30** 결과만 반환.

---

## 4. Layer 4 — Query API (spec §7) Gap

### 4.1 13 capability vs CKG 6 tools

| spec capability | CKG MCP tool | 상태 |
|---|---|---|
| `get_context_for_task` (heavy) | ✅ get_context_for_task | spec과 다른 알고리즘 (eval 분석 §6.2 참조) |
| `find_callers` (light) | ✅ find_callers | OK |
| `impact_of_change` (medium) | ❌ | "어떤 파일 변경 시 영향 범위" — playbook 핵심 |
| `semantic_search` (light) | 🟡 search_text (FTS5 only, vector 없음) | semantic 아닌 lexical |
| `get_file_slice` (light) | ❌ (HTTP API의 /api/blob과 비슷하나 line range X) | "정밀 라인 범위 조회" 누락 |
| `cross_lang_refs` (medium) | ❌ | binds_to edge는 있으나 query 표면 없음 |
| `get_definition` (light) | 🟡 find_symbol로 흉내 | OK |
| `list_tests_for` (light) | ❌ | Test 노드 자체 미정의 |
| `get_freshness` (light) | ❌ | server.Staleness는 viewer banner용, MCP에 없음 |
| `request_refresh` (light) | ❌ | — |
| `remember_fact` (stateful) | ❌ | Working Memory 자체가 없음 |
| `record_decision` (stateful) | ❌ | — |
| `recall_session` (stateful) | ❌ | — |

→ **13 capability 중 5개 OK, 3개 부분, 5개 누락**. 가장 큰 미스는 **`impact_of_change`, `list_tests_for`, `recall_session`** 셋 — 사용자가 *"수정 영향 범위"*를 묻는 본질적 query에 답할 수 없음.

### 4.2 5 MCP Tool Group (spec §7.4)

| spec group | CKG |
|---|---|
| `cks.index.*` (find_symbol, get_definition, get_file_slice, bm25_search, get_file_ast) | ❌ namespace 분리 X |
| `cks.graph.*` (find_callers, impact_of_change, cross_lang_refs, list_tests_for) | ❌ |
| `cks.context.*` (get_context_for_task, semantic_search) | ❌ |
| `cks.memory.*` (remember_fact, record_decision, recall_session) | ❌ |
| `cks.ops.*` (get_freshness, request_refresh, graph_query) | ❌ |

→ 6 도구가 모두 평면 namespace로 등록. 권한 분리 / 비용 추적 / 역할별 필터링 모두 불가.

### 4.3 CKS↔Tier B Contract (spec §7.5)

| spec 요건 | CKG |
|---|---|
| mTLS verified caller (cert SAN) | ❌ stdio 평문 |
| `manifest_ref` envelope | ❌ |
| `budget_tokens`/`budget_hops` envelope 명시 | 🟡 budget_tokens은 있음, budget_hops X |
| Response class (heavy/medium/light/stateful) | ❌ |
| 6 CKS 특수 error code (FreshnessStale/BudgetExceeded/CitationNotFound/SanitizeFailed/IndexUnavailable/PolicyError) | ❌ 일반 Go error |
| `metadata.sanitize_report` | ❌ |

---

## 5. spec §12 Bootstrap Pipeline 5-Phase 구현도

spec은 `cks bootstrap --repo /path --languages go,solidity,typescript,shell --depth full`을 명시:

| Phase | spec | CKG `ckg build` |
|---|---|---|
| **A. Detection & Parsing** | 수준1 + 수준2 + 수준3 통합 | 🟡 Go만 수준1+2, TS/Sol은 수준1만 |
| **B. Graph Construction** (6 graph 순차) | Structural→Semantic→Execution→Concurrency→Distributed→Temporal | 🟡 cold path에서 순차 emit, 단 emit 종류는 §1.1대로 16/30 |
| **C. Pattern Detection** (커스텀 분석기) | consensus handler / state machine / state transitions / config↔code | ❌ E3에서 stdlib HTTP/RPC만 |
| **D. Index Materialization** (Graph + Vector + BM25 + AST + Freshness) | 4 backend 동시 초기화 | 🟡 Graph만, Vector ❌, BM25는 FTS5로 흉내 |
| **E. Systemization Report** (구조요약/진입점카탈로그/동시성패턴/cross-lang map/coverage) | JSON 리포트 | ❌ ckg는 manifest.json만 (count + Files[]), 위 내용 없음 |

→ Phase A+B만 부분 구현, **C+D+E 사실상 미구현**.

### spec §12.2 Code Systemizer

> "Bootstrap 완료 후, 6가지 그래프의 정보를 종합하여 **Project Knowledge Base**를 생성한다. 이 결과물은 (1) CKS Working Memory 초기 데이터, (2) Retrieval Playbook 선택 근거, (3) UC-B4 Onboarding Walkthrough 소스."

→ CKG에는 **이 단계 자체가 없음**. 빌드 후 LLM이 프로젝트를 처음부터 이해해야 함. 이게 *"동일 프로젝트를 분석할 때마다 LLM이 처음부터 헤매는"* 사용자 체감의 직접 원인.

---

## 6. spec §13 Runtime Evidence (Phase 2~3) — 통째로 미구현

| 입력 | spec | CKG |
|---|---|---|
| `go test -race` 결과 → `verified_race` | required | ❌ |
| `go test -coverprofile` → tests edge 강화 | required | ❌ |
| benchmark → `hot_path: true` | required | ❌ |
| structured log → `correlated_with` | required | ❌ |
| OpenTelemetry trace → Execution Graph 실증 | required | ❌ |
| consensus message log → Distributed Graph 보강 | required | ❌ |
| goroutine dump → Concurrency Graph 보강 | required | ❌ |

→ Phase 2~3 영역으로 spec도 후순위로 두긴 했으나, 이로 인해 *"정적 그래프가 실제 런타임과 일치하는지"*를 검증할 수단이 없음.

---

## 7. Cross-Language Linking (spec §8) Gap

| spec 항목 | CKG |
|---|---|
| Go ↔ Solidity ABI: contract artifact 파싱 + ABI selector + abigen 패턴 인식 | 🟡 contract 이름 + TS class 이름 매칭만. **ABI selector 미사용**, abigen 미인식. ParamTypes는 nil placeholder |
| JS/TS ↔ Go RPC: web3.eth.call/provider.send → Go RPC handler | ❌ |
| 동적 생성 테스트 incremental update (수초 latency) | ❌ |

**현재 binds_to edge의 정확도는 매우 낮음** — 이름이 우연히 같은 모든 클래스가 후보가 됨. 실제 ABI selector 매칭이 없으므로 false positive/negative 모두 가능.

---

## 8. Sanitize / Prompt-Injection 방어 (spec §6.2 Step 8.5) — 통째로 미구현

spec은 다음을 의무로 명시:

- `<untrusted-{origin}>` XML sentinel + markdown fence
- 6개 baseline 패턴 (pi-imperative-001 ~ pi-base64-006)
- `policies/sanitization_rules.yaml#cks_evidence_pack`
- ECDSA P-256 detached `.sig`
- Hot reload + fsnotify watch
- Fail-closed 정책

**CKG에는 이 영역 전체가 없음**. LLM이 받는 응답에 코드 주석/commit message가 그대로 포함되며, 거기에 prompt-injection 시도가 있어도 차단 불가.

---

## 9. 종합 — 사용자 핵심 목적 대비 진단

> 사용자 의도: **"분석 프로젝트 경로 → 코드 + git diff → graph DB → LLM이 어디를 수정할지 정확히 찾도록 지원"**

이를 4가지 sub-goal로 분해해서 실측:

| Sub-goal | 충족도 | 근거 |
|---|---|---|
| (1) 프로젝트 경로 → graph DB 자동 구축 | 🟡 60% | Go는 audit PARITY ✅. TS/Sol은 declarations/imports만 (분석 §5). Solidity 함수 호출, JS RPC, Sol 상속 모두 미추출 |
| (2) git history diff 반영 | 🟡 50% | `changed_in`/`blame` file-level만. line-level blame, `correlated_with`/`observed_in`/`mentioned_in` 모두 미구현. Bug-fix playbook의 step 3(blame)이 작동은 하나 라인 정밀도 부족 |
| (3) "어디를 수정해야 하는지" 정확 추천 | 🔴 15% | Layer 3(Retrieval Orchestrator) 전체 부재. task-type 분류, playbook, evidence enrichment(risks/concurrency_context/recent_commits/existing_tests) 모두 없음. LLM은 단순 FTS top-30만 받음 |
| (4) LLM이 코드 분석/수정 시 정확한 file:line 인용 | 🔴 10% | Citation enforcement 없음. file:line은 응답 envelope에 포함되긴 하나 **존재성 검증/필수화 메커니즘 없음** → LLM hallucination 가능 |

**가장 critical한 결손**: **Layer 3 Retrieval Orchestrator (Pager) 전체**. spec이 가장 강조한 *"이 계층이 없으면 LLM이 매 질문마다 토큰 폭발한다"*는 결함이 정확히 발생.

---

## 10. 우선순위별 권장 작업 (P0 → P3)

> 사용자가 *"동작 정상이지 않다"*고 보고한 이슈의 근본 원인 해결 순서.

### P0 — 사용자 핵심 목적 직접 차단 요인

1. **Citation Enforcement 도입** (1주 작업, 가장 ROI 큼)
   - `mcp.buildContext` 출력에 `file:line` 필수화
   - 노드 `file_path`/`start_line`이 빈 결과는 응답에서 제외
   - 효과: hallucination 즉각 감소
2. **Task-type classifier + 4 Playbook 도입** (2~3주)
   - bug_fix / feature_add / concurrency_safety / architecture_explain 4종
   - intent → backends + hop_depth + budget_ratio 매핑 (spec §6.3 yaml 그대로 구현)
   - 효과: "수정 위치 정확도" 핵심 개선
3. **Evidence Pack v2** (1~2주)
   - `recent_commits` (이미 있는 G6 데이터 활용)
   - `existing_tests` (Test 노드 신설 필요)
   - `risks` 주석 (concurrency edges 활용)
   - `concurrency_context` (G4 데이터 활용)
   - 효과: LLM이 받는 정보의 깊이 즉각 향상

### P1 — Spec 핵심 미구현

4. **Working Memory store** (`run_id` + Q&A cache + facts/decisions)
   - SQLite/JSON 단일 파일로 구현 가능 (spec §5.3 *"개발/단일 user 환경: SQLite 또는 JSON"*)
   - `remember_fact` / `record_decision` / `recall_session` 3 MCP tool 추가
5. **Vector DB 통합** (`sqlite-vec` 권장 — CGO-free 정합)
   - function/class/contract chunk 임베딩 (`bge-small-en-v1.5` 로컬)
   - `semantic_search` 진짜 구현
6. **`impact_of_change`, `list_tests_for`, `get_freshness`, `request_refresh` 4 MCP tool 추가**

### P2 — Graph 정밀도

7. **Solidity body 분석**: `function_definition` 안의 `call_expression` query 추가 → `calls` edge emit
8. **TS body walk**: tree-sitter `call_expression` query → `calls` + cross-file resolve
9. **G3 timeout/retry/cancellation_path**: Go의 `context.WithTimeout/WithCancel` 패턴 detector
10. **Sol 상속 (`is X`)**: `inheritance_specifier` query → `extends`/`implements` edge
11. **xlang ABI selector 매칭**: solc artifact JSON 읽고 selector hash로 매칭

### P3 — Spec 후순위 (Phase 2~3)

12. Sanitize sentinel + baseline 6 rule
13. Runtime evidence integration (race/coverage/log)
14. 5 MCP Tool Group namespace 분리
15. CKS↔Tier B Contract (mTLS, envelope, error codes, response class)

---

## 11. 한 줄 결론

> **현재 CKG는 CKS spec의 Layer 1 일부(Graph DB)와 Layer 4 일부(6 tools)만 구현된 상태입니다. spec이 "가장 중요하다"고 강조한 Layer 3 Retrieval Orchestrator(Pager)와 Layer 2 Working Memory가 통째로 빠져있어, 사용자가 의도한 "LLM이 정확히 어디를 수정해야 하는지 찾도록 돕는다"는 핵심 가치가 실현되지 않습니다. 사용자가 보고한 빈번한 에러와 누락은 V0 simplification(개별 코드 버그)이 아니라 spec 핵심 계층의 미구현이 직접 원인일 가능성이 높습니다.**

---

## Appendix A: spec coverage 스코어카드

| 영역 | spec 분량 | CKG 구현률 |
|---|---|---|
| §2 CPU 비유 + 4-Layer 구조 | 도입 | (개념 채택만) |
| §4.1 6 Graph Axis (30 edges) | 핵심 데이터 모델 | 🟡 53% (16/30) |
| §4.2 Vector DB | required | 🔴 0% |
| §4.3 BM25 | required | 🟡 30% (FTS5 흉내) |
| §4.4 AST Cache | required | 🟡 30% (직렬화 X) |
| §4.5 File/Blob | required | 🟢 100% |
| §4.6 Multi-Lang Indexer + Freshness | required | 🟡 50% |
| §4.7 3-Level Parser Pipeline | required | 🟡 35% (Go만 L2, TS/Sol L1) |
| §5 Working Memory | required | 🔴 0% |
| §6 Retrieval Orchestrator (Pager) | ★ 핵심 | 🔴 5% |
| §6.1 Evidence Pack 16 fields | required | 🔴 15% (4/16) |
| §6.2 13-step 알고리즘 | required | 🔴 12% (1.5/13) |
| §6.3 4 Playbook | required | 🔴 0% |
| §7.1 13 Capability | required | 🟡 35% (5+3/13) |
| §7.4 5 MCP Tool Group | required | 🔴 0% |
| §7.5 Tier B Contract | required | 🔴 5% |
| §8 Cross-Language Linking | required | 🟡 30% (Sol→TS name only) |
| §12 Bootstrap 5-Phase | required | 🟡 50% (A+B만) |
| §12.2 Code Systemizer | required | 🔴 0% |
| §13 Runtime Evidence (Phase 2~3) | optional later | 🔴 0% |
| §6.2 Step 8.5 Sanitize | required | 🔴 0% |

**가중 평균 추정 (P0~P2 항목 기준)**: ≈ **25%**

---

## Appendix B: 다음 세션이 본 분석을 활용하는 방법

1. **이슈 보고가 들어오면 §9의 4 sub-goal 매트릭스에 매핑**
   - "TS 코드 분석이 빈약" → sub-goal (1) 60%, root cause: TS 파서 declarations만
   - "수정 위치 추천이 부정확" → sub-goal (3) 15%, root cause: Layer 3 부재
   - "동일 query 반복" → Layer 2 부재
2. **fix하려는 issue가 P0/P1/P2/P3 중 어디에 속하는지 우선 분류**
   - P0이면 즉시 작업
   - P1+은 spec V1+ 영역 — 사용자 합의 필요
3. **개별 버그 fix가 spec 미구현을 우회하려는 것인지 확인**
   - "LLM이 잘못된 file:line 답한다" → P0-1(Citation Enforcement)로 root fix
   - 개별 응답 후처리로 막지 말 것
4. **신규 작업이 spec 어느 §에 대응하는지 PR 제목/본문에 명시**
   - `feat(mcp): add citation enforcement (CKS spec §6.2 step 10)`
   - `feat(workmem): add remember_fact tool (CKS spec §5.4 + §7.1)`

---

**End of CKS spec compliance analysis.** 본 문서는 CKG가 CKS spec의 어디까지 와 있고 어디가 비어있는지의 단일 권위 reference. 신규 작업 우선순위 결정·외부 reviewer 공유·spec V1 작업 계획 수립의 근거로 사용.
