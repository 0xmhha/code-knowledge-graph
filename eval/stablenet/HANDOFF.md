# Handoff — go-stablenet 검증의 미해결 결함

> 작성일: 2026-05-11
> 직전 세션 마무리 상태에서 다음 세션으로 인계되는 작업 목록.
> 본 문서만 읽어도 각 task를 단독으로 시작 가능하도록 self-contained 작성.

## 0. 지난 세션 한 줄 요약

- CKG MCP 8 도구가 go-stablenet 그래프(121K nodes / 399K edges)에 대해 hallucination 0건으로 정확 응답 — *정보 추출 단계*는 신뢰.
- 4-방식 측정 인프라(α/β/γ/δ) *구동은* OK, *측정 의도와 실 동작 사이에 다수 결함* — 30-question 골든셋 측정 시작 전에 해소 필요.
- 본 세션에서 해결된 결함: **B1**(smartctx FTS punctuation), **B2**(find_symbol description), **L2**(raw_output column).
- 본 세션에서 발견·미해결 결함: **B1 인접**(sigil bypass), **L3·L4·L5·L6**, smartctx prose 강건성, 30-question 인프라 5건.

## 1. Task 일람

| ID | 제목 | 우선 | 카테고리 | 머무를 곳 | 의존 |
|---|---|---|---|---|---|
| T-01 | γ baseline tool-loop emulation 구현 | P0 → S1 이관 | CKS 측정 인프라 | **S1 (CKS)** | — |
| T-02 | extractSymbols 채점기 정규화 | **P0** | CKG core | S0 (CKG) | — |
| T-03 | 위치 정확도(file:line) validator 구현 | **P0** | 측정 인프라 | S0 (CKG) | — |
| T-04 | Hallucination validator 구현 | **P0** | 측정 인프라 | S0 (CKG) | — |
| T-05 | LLM backend 가동 (**api 확정**) | **P0** | 환경 | 환경 | — |
| T-06 | 27 task YAML + JSONL 양쪽 작성 | P1 | 콘텐츠 | S0/S1 공용 | T-04, T-15 |
| T-07 | dumpFiles의 _test.go 제외 (L5) | P1 | CKG core | S0 (CKG) | — |
| T-08 | dumpFiles 표본 추출 무작위화 (L6) | P1 | CKG core | S0 (CKG) | T-07 |
| T-09 | rewriteFTSQuery sigil bypass 완화 (B1 인접) | P1 | CKG core | S0 (CKG) | — |
| T-10 | smartctx prose-query 강건성 강화 | P2 → S1 이관 | CKS Layer 3 | **S1 (CKS)** | — |
| T-11 | Incremental index time KPI 측정 | P1 | CKG core | S0 (CKG) | — |
| T-12 | find_callers depth>1 회귀 test | **P0** | CKG core | S0 (CKG) | — |
| T-13 | impact_of_change 결정성 회귀 test | **P0** | CKG core | S0 (CKG) | — |
| **T-14** | CKG public 표면 정리 (`pkg/mcphandlers/`) | **P0** | CKG core | S0 (CKG) | — |
| **T-15** | task YAML ↔ known-issues JSONL 동기화 도구 | P1 | 검증 자료 | S0 (CKG) | — |

P0 = 30-question 측정 시작 전 필수
P1 = 측정 신뢰성 향상
P2 = 측정 정밀도 향상

### 결정 사항 (2026-05-11 사용자 확정)

- **Q1 LLM backend** = api (`ANTHROPIC_API_KEY`)
- **Q2 27 task 저장** = `eval/stablenet/tasks/` + `stablenet-ai-agent/benchmark/known-issues.jsonl` 둘 다
- **Q3 T-01 γ emulation** = CKG는 손대지 않음, S1 (CKS) 이관
- **Q4 T-10 smartctx prose** = CKG는 손대지 않음, S1 (CKS Layer 3) 이관
- **추가**: import-based 아키텍처 — CKS·CKV가 CKG를 `go.mod` import. 코드 이동 X
- 상세: `EXECUTION_STRATEGY.md` §6, §10

---

## T-01. γ baseline tool-loop emulation 구현

**카테고리**: CKG core (V1 작업)
**우선**: P0 — γ가 30-question 측정의 핵심 baseline인데 V0는 fiction

### 현 상태
`internal/eval/runner.go:106`에 명시 — "γ is intentionally NOT pre-called — emulating the multi-turn cost, we let the LLM ask in plain text." 즉 γ baseline에서 LLM이 plain text로 "I would call find_callers(...)" 라고 답해도 실제 도구 호출은 일어나지 않음. 그 응답 텍스트를 그대로 채점기에 통과 → 정답률 매우 낮음.

### 해결 위치
`internal/eval/runner.go:75~120` `runOne()` 함수의 γ 분기.

### 권장 접근
1. mcp-go 클라이언트를 in-process로 spawn (or 직접 store에 접근)
2. LLM 응답에서 tool_use block 파싱
3. tool 호출 → 결과를 다음 turn의 user message로 feedback
4. 최대 N turn (e.g. 8) 또는 LLM이 final answer를 줄 때까지 loop
5. 최종 답을 scoreTask에 통과

mcp-go의 client 인터페이스 (`server.AddTool` 반대 방향) 사용 가능 — Anthropic SDK도 tool_use response 지원.

### Acceptance criteria
- γ baseline이 T01에서 `find_callers(core.NewBlockChain)` 실 호출 → 응답 4 nodes를 받고 → 답에 정답 3개를 포함
- T01 γ score ≥ 0.7 (precision_recall threshold)
- 측정 결과 `num_tool_calls` 컬럼이 0이 아닌 실제 호출 수 반영

### 관련 자료
- `internal/eval/runner.go` (현 emulation 한계)
- `internal/eval/baseline.go::AllowedTools` (γ 허용 도구 목록)
- 본 세션 시뮬레이션 결과: `results/sim/score-simulation.json` (γ minimal=1.0 / verbose=0.71)

---

## T-02. extractSymbols 채점기 정규화

**카테고리**: CKG core
**우선**: P0 — 답 표기 디테일에 점수가 흔들려 baseline 비교 불가

### 현 상태
`internal/eval/score.go:142~152` `extractSymbols()`:
```go
if strings.Contains(tok, ".") && !strings.HasPrefix(tok, ".") && !strings.HasSuffix(tok, ".") {
    out = append(out, strings.Trim(tok, ".:;()"))
}
```
모든 "점 포함 토큰"을 정답 후보로 수집. 결과:
- 파일 경로(`eth/backend.go`) → 추출되어 정답 셋에 없으면 precision 깎임
- 받는 측 receiver 표기(`*eth.Ethereum.New`) → 정답 `eth.New`와 표기 차이로 recall 깎임
- 질문에 등장한 seed(`core.NewBlockChain`)도 답에 다시 적으면 정답으로 잡힘

본 세션 시뮬레이션 §8.1에서 동일 정답이 minimal=1.0 / verbose=0.71로 변동 직접 확인.

### 해결 위치
`internal/eval/score.go:142`

### 권장 접근
1. **파일 확장자 blacklist**: `.go`, `.ts`, `.sol`, `.py`, `.js`, `.md`, `.yaml`, `.json` 등으로 끝나는 토큰 제외
2. **Receiver normalization**: `*eth.Ethereum.New`, `(*eth.Ethereum).New` → `eth.New` (마지막 두 segment 추출)
3. **Seed 자동 제외**: scoreTask 호출 시 task description에서 등장한 qname을 답 토큰에서 빼기
4. (옵션) LLM-judge로 전환 — 답을 LLM에 다시 통과시켜 semantic match

### Acceptance criteria
- 시뮬레이션 verbose 답안(`eth.New (in eth/backend.go)` 같은)이 1.0 점수
- 동일 정답 표기 변형 10개 모두 ≥0.9 (회귀 test)
- 기존 `internal/eval/score_test.go` 통과 유지

### 관련 자료
- 본 세션 §8 분석
- `results/sim/score-simulation.json` (8 cases 시뮬레이션)

---

## T-03. 위치 정확도 validator 구현

**카테고리**: 측정 인프라 (신규)
**우선**: P0 — 30-question 요구사항 첫 번째 지표

### 현 상태
없음. 측정 항목 신규 추가.

### 해결 위치
신규 패키지 또는 `internal/eval/location_check.go`.

### 권장 접근
1. 답 텍스트에서 `path/to/file.ext:NNN` 또는 `path/to/file.ext#LNNN` 등 패턴 정규식 추출
2. 각 인용에 대해:
   - `store.GetNodesByFile(path)` 또는 SQL — 그래프에 해당 file 노드 존재?
   - 존재하면, 인용된 line이 *어떤 노드의 범위(start_line ~ end_line)에 포함*되는지 검사
3. score 4종 반환: total citations, file_exists, line_in_some_node, line_matches_expected_node
4. 채점 보고서에 columns 추가 (`location_precision`, `location_recall`)

### Acceptance criteria
- 답에 `eth/backend.go:227` 인용 → file 존재 + line 227이 `eth.New` 노드 범위에 포함되면 hit
- 30-question 골든셋에서 baseline 별로 file:line 인용 정밀도 보고

### 관련 자료
- `pkg/store` Reader 인터페이스 (`GetNodesByFile`, `ListNodesByFile` 등 추가 필요할 수 있음)
- 본 보고서 §C1

---

## T-04. Hallucination validator 구현

**카테고리**: 측정 인프라
**우선**: ~~P0~~ → **CLOSED (2026-05-23)**
**상태**: **production-ready** — V0→V5 11 cycles 누적, 4 baselines × 3 runs 측정 안정

### 진행 trajectory (요약)
| Cycle | Commit | Milestone |
|---|---|---|
| C18 V0 | `2f58ce7` | ValidateMentions API + 6 unit tests |
| C19 V1 | `bc6fe9c` | runner 통합 — 결과 CSV/Report에 metric |
| C20-C23 | `7955b35`+`e794504`+`e9ffced`+`dc87a96` | infra + paren/prose/brace/suffix |
| C24 4-axis | `1db6d2d`+`c368d9c`+`1754b6c`+`63e1d44` | β/γ/δ + multi-shot + prompt + filter |
| C27 A+B+D | `22539fc` | β fix + total-token + multi-lang |
| C29 cycle 6 | `524685a` | line-ref + Hangul separator |
| C31 cycle 7 | `0d1b2f2` | numeric + #/@/→ separator |
| C32 V3 | `49a6d26` | receiver-style heuristic |
| C34 B | `46693a6` | UserPromptBytes metric for H1 |
| C36-37 FTS | `2a4db90`+`8e8bf9b` | dotted-id + power-user gate (ckg core bug) |

상세: `docs/eval-trajectory.md`

### 최종 baseline (post-cycle 9, 2026-05-23)
| Baseline | Score (mean±std) | Halu rate | UserPromptBytes |
|---|---|---|---|
| α | 0.396±0.119 | 0.083 | 2,245 |
| β | 0.746±0.046 | **0.000** | 69,422 |
| γ | 0.688±0.037 | 0.122 | 157 |
| δ | **0.825±0.035** | **0.000** | 12,612 |

H1 (user-prompt-bytes savings): δ vs α **-461.8%** (δ가 α의 5.6x 사용; *cost-benefit trade-off 명시*)
H2 (score delta): δ-α **+0.429** (**PASS** ≥ 0)

### Acceptance criteria (최종 평가)
- LLM 가상 함수명 hallucination++ — **PASS**
- baseline별 hallucination 카운트 — **PASS** (4 baselines × 3 runs)
- false-positive 율 < 5% — **PASS** (β/δ 0.000)

### V5 미해결 (defer or close)
- file 경로 token validation — *cycle 6 line-ref blacklist*로 *부분 해소*
- fuzzy matching (NewBlockchain vs NewBlockChain) — *V3 receiver-style heuristic*과 *유사 영역*. *데이터 더 모은 후 결정*
- 남은 *real LLM noise*: `mux.HandleFunc`, `http.HandleFunc`, `http.HandlerFunc` (Go std-lib reaching) — *prompt engineering territory (C 옵션)*

---

## T-05. LLM backend 가동

**카테고리**: 환경
**우선**: ~~P0~~ → **CLOSED (2026-05-23)**
**상태**: **가동 검증 완료** — 9회 smoke run 모두 정상 종료 (cycle 1-9, 4 baselines × 3 runs each)

### 가동 setup
| Backend | Env | 비용 |
|---|---|---|
| api (권장) | `ANTHROPIC_API_KEY` set | Anthropic API |
| cli | `claude` on PATH + `CLIWRAP_AGENT` set | Claude 구독 |

기본 명령:
```bash
make eval-llm-smoke BASELINES=alpha,beta,gamma,delta N_RUNS=3
```

### 검증된 작동 (cycle 9 결과)
- 4 baselines × 3 runs = 12 calls, 평균 ~5-10분 (cli backend)
- API 529 Overloaded / SSL transient failures 정상 처리 (1/12 fail 시 11/12 계속)
- `eval/results/latest/{results.csv,report.md}` 정상 생성
- *Hallucination metric*, *UserPromptBytes*, *mean±std*, *Hypothesis check* 모두 정확

### Acceptance criteria (V0 → final)
- ~~`make eval-llm-smoke` 1회 정상 종료~~ → **9회 누적 검증 완료**
- ~~raw_output 컬럼 채워짐~~ → **확인**
- ~~hallucination_total/count/rate 컬럼~~ → **확인 + V3 receiver-style + UserPromptBytes 추가**
- ~~report.md Hallucination detail~~ → **확인 + post-filter warnings (Axis 4) 통합**

### 알려진 transient issues (작동 영향 없음)
- API 529 / SSL: 1/12 fail 발생 — Run() loop이 graceful skip
- cli backend token field anomaly: *β run 0 token=0 한 번 관찰 (transient)*; raw_output/score는 정상

### 관련 자료
- `internal/eval/llm_cli.go`, `internal/eval/llm.go` (api backend)
- 본 보고서 §B3

---

## T-06. 27개 task YAML 추가 작성

**카테고리**: 콘텐츠
**우선**: P1 — 30-question 골든셋 자체
**의존**: T-04 (정답의 그래프 sync 확인용)

### 현 상태
3개만 (`tasks/T01-newblockchain-callers.yaml`, `T02-wbft-prepare-validation.yaml`, `T03-systemcontracts-v2-upgrade.yaml`).

### 권장 접근
정답 종류 분포 (총 30개):
- `symbol_set` × 10 (find_callers 류, find_callees 류, type users 류)
- `rubric` × 15 (도메인 설명, 절차 설명, 변경 점검)
- `code_patch` × 5 (`Expected.MustUseSymbols`, `MustCall`, `MustNotBreakSignature` 활용)

도메인 분포 (go-stablenet 특화):
- WBFT consensus × 8 (prepare/commit/round-change/validator-set 등)
- system contracts × 6 (artifact embed, hard fork 등록, InjectContracts)
- core 블록체인 × 8 (state, txpool, snapshot 등)
- eth 핸들러 × 5
- 기타 × 3

각 task 작성 단계:
1. 질문 작성 (description, 마침표 회피 — B1 인접 미해결)
2. 정답을 그래프 조회로 확정 (SQL 또는 ckg query) — T-04 validator로 정답 sync 확인
3. corpus_path 선정 (α baseline 우연 우위 회피 — 정답이 corpus_path *바깥*에도 일부 분산되도록)
4. scoring.threshold 조정

### Acceptance criteria
- 30 task YAML 파일이 `eval/stablenet/tasks/` 하위에 존재
- 각 task의 정답 심볼이 그래프에 모두 존재 (T-04 validator로 자동 확인)
- task 종류 분포 위 비율과 ±2개 차이 이내

### 관련 자료
- 본 task 3개 (`tasks/`)를 템플릿으로
- `internal/eval/task.go::Expected` 스키마

---

## T-07. dumpFiles의 _test.go 제외 (L5)

**카테고리**: CKG core
**우선**: P1 — α baseline 신뢰성

### 현 상태
`internal/eval/runner.go:163~165`:
```go
ext := filepath.Ext(p)
if ext != ".go" && ext != ".ts" && ext != ".sol" {
    return nil
}
```
`_test.go`도 그대로 dump. 본 세션에서 T01 α 시뮬레이션 시 `asm/asm_test.go` 등이 dump에 포함된 것 확인.

### 해결 위치
`internal/eval/runner.go:163`

### 권장 접근
```go
ext := filepath.Ext(p)
if ext != ".go" && ext != ".ts" && ext != ".sol" {
    return nil
}
// V0가 testdata + _test.go를 dump하던 결함 (HANDOFF.md L5)
if strings.HasSuffix(p, "_test.go") || strings.Contains(p, "/testdata/") {
    return nil
}
```

### Acceptance criteria
- α dump 결과에 `_test.go` 0건
- `dumpFiles_test.go`에 회귀 케이스 추가

---

## T-08. dumpFiles 표본 추출 무작위화 (L6)

**카테고리**: CKG core
**우선**: P1
**의존**: T-07

### 현 상태
`internal/eval/runner.go:155~178` `dumpFiles()`가 `filepath.Walk` 결과를 *정렬된 순서대로* 처음 5개만 사용. T01 α는 `asm/asm.go, asm_test.go, compiler.go, compiler_test.go, lex_test.go` — 알파벳 1번 디렉토리만.

### 해결 위치
`internal/eval/runner.go:154~179`

### 권장 접근
1. 1차 Walk로 모든 후보 .go 파일 수집
2. seed 기반 deterministic shuffle (rand.New(rand.NewSource(taskID)))
3. 처음 5개 사용 — 디렉토리 분산 보장
4. 또는 패키지(directory) 단위로 stratified sampling

deterministic seed 핵심 — 동일 task의 α 결과는 재현 가능해야 (정량 비교의 전제).

### Acceptance criteria
- T01 α dump가 다양한 디렉토리(최소 3개 이상)의 파일을 포함
- 동일 task에서 두 번 dump 시 동일 결과 (seed deterministic)

---

## T-09. rewriteFTSQuery sigil bypass 완화 (B1 인접)

**카테고리**: CKG core
**우선**: P1
**해결 위치**: `internal/persist/sqlite.go:642~648`

### 현 상태
지난 세션 v2 probe에서 신규 발견:
```
fts search "Where does (NewBlockChain) get called, and why? Show callers!":
SQL logic error: fts5: syntax error near "does" (1)
```
원인: `rewriteFTSQuery`가 `*"():` 중 하나라도 있으면 power-user mode로 분기 → raw query 그대로 FTS5 전달. 자연어 prose에도 `(`, `)`는 흔히 등장.

### 권장 접근
1. power-user sigil을 좁힘 — `*` 또는 `"`만 (FTS5 prefix/phrase 의도 명시)
2. `()` 와 `:`는 자연어에 흔하므로 sigil에서 제외
3. 또는 power-user 분기 이전에 한 번 더 sanitize 통과

또는 `():`를 sigil로 유지하되, 자연어 path에서도 `trimFTSToken`을 거치게 분기 재설계.

### Acceptance criteria
- "Where does (NewBlockChain) get called?" 같은 자연어 입력에서 SQL error 발생 안 함
- 의도된 power-user 입력 `"core.New*"`, `"prefix:value"`는 그대로 패스
- 회귀 test: `pkg/persist/fts_test.go`에 자연어 prose 케이스 추가

### 관련 자료
- `internal/persist/sqlite.go::trimFTSToken` 주석
- 본 세션 §7.3

---

## T-10. smartctx prose-query 강건성 강화

**카테고리**: CKG core
**우선**: P2 — δ baseline의 task 표현 민감도 완화

### 현 상태
δ가 자연어 task description("List every Go function that DIRECTLY calls core.NewBlockChain") 입력 시 BM25 매칭 0 → 빈 pack 반환. 본 세션에서 T01 δ가 빈 응답으로 score 0인 것 확인.

### 해결 위치
`pkg/smartctx/smartctx.go::BuildContext`

### 권장 접근
1. **Query expansion**: prose 입력에서 `core.NewBlockChain` 같은 qname 추출 + 도메인 키워드(`callers`, `directly`)도 함께 BM25에 통과
2. **Fallback**: BM25 0-hit 시 substring fallback 또는 grep-like fallback (heavy)
3. **Heuristic**: task description의 첫 줄 또는 quoted identifiers 우선 가중치

### Acceptance criteria
- T01 δ context pack에 `core.NewBlockChain` 노드 + 직접 callers 노드 포함
- 회귀 test: `pkg/smartctx/buildcontext_test.go`에 prose query 케이스

> **2026-05-11 결정 반영**: T-10은 **S1 (CKS) 이관**. CKG에서는 손대지 않음. `pkg/smartctx`는 *stable* 유지하고, prose-query 강건성은 CKS Layer 3 Orchestrator(Playbook + CKV fusion)에서 해결.

---

## T-11. Incremental index time KPI 측정 인프라

**카테고리**: CKG core (신규)
**우선**: P1 — `04-cks-deep-dive.md` §11 KPI 'Incremental index time' 측정

### 현 상태
없음. `--no-cache` 풀빌드 시간만 측정 가능.

### 해결 위치
신규 — `internal/buildpipe/incremental_bench.go` 또는 `cmd/ckg/bench_index.go`.

### 권장 접근
1. 그래프 상태 A 생성 → 1 파일 수정 → 그래프 상태 B 빌드 (캐시 활용)
2. A → B 빌드 시간 = incremental time
3. 다양한 수정 패턴 측정: 단일 함수 변경 / 새 파일 추가 / 파일 삭제 / 대량 변경
4. CSV 출력 — 시간 trend 추적

### Acceptance criteria
- `ckg bench-index --src=... --baseline-graph=... --modify=...` 명령 작동
- p50 / p95 incremental time 보고
- 풀빌드 대비 incremental 절감율

---

## T-12. find_callers depth>1 회귀 test

**카테고리**: CKG core (신규)
**우선**: P0 — `EXECUTION-GUIDE.md §3.2` S0 acceptance "직접·간접 caller 모두 반환"

### 현 상태
지난 세션 §A2에서 depth=1만 검증 (3 callers 정확). depth=2 이상의 *간접 caller*는 미검증.

### 해결 위치
신규 — `internal/persist/sqlite_callers_test.go` 또는 `internal/mcp/tools_callers_test.go`.

### 권장 접근
1. seed `core.NewBlockChain`에 대해 grep으로 depth=2, depth=3 ground truth 수집
   - depth=1 callers = {eth.New, tests.BlockTest.Run, utils.MakeChain}
   - depth=2 = 위 3개 함수를 호출하는 함수들 (grep)
2. `find_callers(qname, depth=2)` 결과 ↔ ground truth 비교
3. 회귀 test 추가 — depth=1, 2, 3 각각 precision/recall

### Acceptance criteria
- depth=1: precision 1.0 / recall 1.0 (빌드 화이트리스트 기준)
- depth=2: recall ≥ 0.9 (간접 caller 누락 0건 목표)
- depth=3: 회귀 보호용 — 결과의 결정성 확인

---

## T-13. impact_of_change 결정성 회귀 test

**카테고리**: CKG core (신규)
**우선**: P0 — `EXECUTION-GUIDE.md §3.2` S0 acceptance "결정적"

### 현 상태
SQL 기반이라 deterministic으로 추정. 자동 회귀 test 부재.

### 해결 위치
신규 — `internal/mcp/impact_determinism_test.go`.

### 권장 접근
1. 동일 seed_qname + depth로 100회 반복 호출
2. 결과 (nodes·edges 셋) 모두 동일하면 PASS
3. 노드 순서 차이는 무시 (set 비교) — 그래프 순회 순서가 비결정적일 수 있음
4. 동일 그래프 DB에 대한 결정성 + 다른 그래프 DB에서도 같은 의미적 결과 (회귀 보호)

### Acceptance criteria
- 100회 호출 결과 100% 일치
- CI에 추가, 매 PR마다 자동 실행

---

## T-14. CKG public package 표면 정리

**카테고리**: CKG core (신규)
**우선**: **P0** — **CKS S1 진입의 차단 요소**

### 현 상태
MCP 도구 핸들러가 `internal/mcp/tools.go`에 있음. Go 규칙상 *external repo가 internal/* 패키지를 import 불가. CKS가 CKG MCP 핸들러를 재사용하려면 export 이전 필요.

### 해결 위치
신규 패키지 — `pkg/mcphandlers/`.

### 권장 접근
1. `internal/mcp/tools.go` `registerFindSymbol` 등 8개 도구 등록 함수를 `pkg/mcphandlers/`로 이전
2. 시그니처: `func RegisterFindSymbol(s *server.MCPServer, store persist.StoreReader)` 등
3. `internal/mcp/server.go::Run`은 `pkg/mcphandlers`의 Register* 함수만 호출
4. CKS S1에서 다음 형태로 사용 가능:
```go
import (
    "github.com/0xmhha/code-knowledge-graph/pkg/mcphandlers"
    "github.com/0xmhha/code-knowledge-graph/pkg/store"
)
// cks-mcp main
s := server.NewMCPServer(...)
mcphandlers.RegisterFindSymbol(s, store)
mcphandlers.RegisterFindCallers(s, store)
// CKS 추가 도구 (sanitize wrapped)
mcphandlers.RegisterImpactOfChange(s, store, mcphandlers.WithSanitize(...))
```

### Acceptance criteria
- 기존 `ckg-mcp` 바이너리 행동 변경 없음 (`probes/mcp_probe_v2.py` 통과)
- 빈 CKS repo에서 다음 smoke 가능:
  ```bash
  cd /Users/.../code-knowledge-system
  go mod init github.com/0xmhha/code-knowledge-system
  echo 'require github.com/0xmhha/code-knowledge-graph v0.0.0' >> go.mod
  go mod tidy  # replace directive로 로컬 경로 지정
  # 5줄짜리 main.go로 RegisterFindSymbol 호출 → build 성공
  ```
- `internal/mcp/server.go` 코드 ≤30 lines (Run + helper)

### 관련 자료
- `EXECUTION_STRATEGY.md §10` import-based 아키텍처 분석
- `internal/mcp/server.go::Run` 현 구현

---

## T-15. task YAML ↔ known-issues JSONL 동기화 도구

**카테고리**: 검증 자료 (신규)
**우선**: P1
**의존**: T-06 (콘텐츠 작성과 동시)

### 현 상태
Q2 결정으로 두 위치에 같은 task 저장:
- `eval/stablenet/tasks/T*.yaml` (CKG 자체 4-방식 측정)
- `stablenet-ai-agent/benchmark/known-issues.jsonl` (Coding Agent regression scoring)

수동 동기화는 drift 위험.

### 해결 위치
신규 — `eval/stablenet/sync_tasks.py`.

### 권장 접근
1. `eval/stablenet/tasks/*.yaml` 읽기
2. 각 task의 `id`, `description`, `expected.symbols`(or rubric)을 `EXECUTION-GUIDE.md §5.1` JSONL 스키마로 변환:
```json
{
  "issue_id": "T01-newblockchain-callers",
  "pre_fix_commit": "<latest go-stablenet HEAD>",
  "request": "<YAML description>",
  "ground_truth": {
    "files_changed": [<expected symbols의 file_path>],
    "fix_summary": "<YAML 요약>",
    "fix_diff_path": null   // Coding Agent용 — task 정의에선 비워둠
  },
  "scoring": {
    "file_recall_target": 0.8,
    "behavior_match_target": "n/a"
  }
}
```
3. 결과를 `stablenet-ai-agent/benchmark/known-issues.jsonl`에 append (idempotent — issue_id 중복 시 update)
4. 역방향 (JSONL → YAML)도 구현 — Coding Agent 측 추가 시 CKG에 sync

### Acceptance criteria
- `python3 sync_tasks.py --check`로 drift 감지 (exit code !=0)
- `python3 sync_tasks.py --apply`로 동기화 자동 수행
- 30 task 셋이 양쪽에서 동일 (issue_id × content hash)

### 관련 자료
- `stablenet-ai-agent/claudedocs/EXECUTION-GUIDE.md §5.1` known-issue 스키마
- `internal/eval/task.go::Task` 현 YAML 스키마

---

## 2. 권장 작업 순서 (2026-05-11 결정 반영)

```
  Phase 1 — S0 closeout
  ┌──────────────────────────────────────────┐
  │ T-05 LLM backend (api 확정)              │  30분
  └──────────────┬───────────────────────────┘
                 │
                 ▼
  ┌──────────────────────────────────────────┐
  │ T-14 ★ pkg/mcphandlers 표면 정리         │  S1 진입 차단 요소
  │ T-12 find_callers depth>1 회귀 test      │
  │ T-13 impact_of_change 결정성 test        │
  │ T-03 위치 정확도 validator                │
  │ T-04 Hallucination validator              │
  │ T-02 extractSymbols 정규화               │
  └──────────────┬───────────────────────────┘
                 │
                 ▼
  Phase 2 — KPI baseline (5 task × 4 방식)
  ┌──────────────────────────────────────────┐
  │ 측정 실행 → raw_output 분석 → baseline   │
  └──────────────┬───────────────────────────┘
                 │
                 ▼
  Phase 3 — 인프라 결함 정리
  ┌──────────────────────────────────────────┐
  │ T-07 dumpFiles _test.go 제외             │
  │ T-08 dumpFiles 무작위화 (T-07)           │
  │ T-09 FTS sigil bypass 완화               │
  │ T-11 Incremental index time KPI          │
  └──────────────┬───────────────────────────┘
                 │
                 ▼
  Phase 4 — 30 task 확장 + S1 진입
  ┌──────────────────────────────────────────┐
  │ T-06 27 task YAML 작성                   │
  │ T-15 YAML ↔ JSONL 동기화 도구            │
  │ 30 task × 4 방식 → Report.md             │
  │ CKS repo `go mod init` + import smoke    │
  │ S1 plan에 T-01·T-10 등록 (CKS 측)        │
  └──────────────────────────────────────────┘

  [CKG에서 다루지 않음 — S1 (CKS) 이관]
  - T-01 γ tool-loop emulation
  - T-10 smartctx prose-query 강건성
```

**최단 경로 (12 task 완료, T-01·T-10 제외)**: 2~3주. T-14가 S1 진입의 결정적 차단 요소 — 우선 완료.

## 3. 다음 세션 시작 가이드

### 3.1 컨텍스트 빠르게 잡기

```bash
cd "$CKG_REPO"   # code-knowledge-graph 체크아웃 루트

# 직전 세션 보고서 + 검증
cat eval/stablenet/VERIFICATION_REPORT.md          # 전체 검증 결과 (§1~§8)
cat eval/stablenet/RESPONSE_VALIDATION_REPORT.md   # 응답 정확도 + 인프라 gap
cat eval/stablenet/HANDOFF.md                       # 본 문서

# 그래프 DB 위치 (지난 세션 산출물)
ls /tmp/ckg-stablenet/                              # graph.db, manifest.json
```

만약 `/tmp/ckg-stablenet/`이 정리됐다면 재빌드:
```bash
$CKG build --src=/Users/.../go-stablenet-latest \
           --files-from=eval/stablenet/stablenet-files.json \
           --out=/tmp/ckg-stablenet --lang=go
```

### 3.2 첫 작업 — T-05 (api backend 확인) → T-14 (import 표면)

확정 사항: api backend 사용.
```bash
echo $ANTHROPIC_API_KEY     # 설정되어 있어야
$CKG eval --llm-backend=api --llm=claude-sonnet-4-6 \
  --graph=/tmp/ckg-stablenet \
  --tasks='eval/stablenet/tasks/T01-newblockchain-callers.yaml' \
  --baselines=alpha --out=/tmp/smoke
# raw_output 컬럼 있는지 results.csv head 확인
```

이후 **T-14 (CKG public 표면 정리)** 진행 — CKS S1 진입의 차단 요소이므로 다른 P0보다 우선.

### 3.3 진행 추적

각 task 완료 시 본 문서 §1 표의 상태 컬럼 추가하거나 별도 `STATUS.md` 작성 권고.

## 4. 참고 자료

| 파일 | 내용 |
|---|---|
| `eval/stablenet/VERIFICATION_REPORT.md` | 8 sections, 직전 세션 종합 |
| `eval/stablenet/RESPONSE_VALIDATION_REPORT.md` | 응답 정확도 + 인프라 gap |
| `eval/stablenet/README.md` | 디렉토리 구조 + 재현 명령 |
| `eval/stablenet/results/eval-v1/` | v1 측정 결과 (점수만, raw 부재) |
| `eval/stablenet/results/sim/score-simulation.json` | 채점기 표기 민감도 실증 8 cases |
| `internal/eval/runner.go` | runOne, dumpFiles, writeCSV |
| `internal/eval/score.go` | extractSymbols, RubricCheck, PrecisionRecall |
| `internal/eval/baseline.go` | α/β/γ/δ AllowedTools + SystemPrompt |
| `pkg/smartctx/smartctx.go` | BuildContext (δ baseline 핵심) |
| `internal/persist/sqlite.go` | rewriteFTSQuery, trimFTSToken |
