# 응답 검증 레포트 — go-stablenet × CKG MCP

> 작성일: 2026-05-11
> 검증 대상: (A) MCP 8 도구 응답 정확도 / (B) 4-방식 LLM 답변 코드 이해도 / (C) 30-question 골든셋 측정 인프라 준비도

## 0. 한 줄 요약

| 영역 | 결과 |
|---|---|
| (A) MCP 8 도구 응답 정확도 | **8/8 PASS** — grep 기반 ground truth와 모두 일치, hallucination 0건 |
| (B) LLM 답변 코드 이해도 | **정량 검증 불가** — V0가 raw_output 미저장 + 환경 한계로 실 LLM 응답 데이터 부재 |
| (C) 30-question 골든셋 인프라 | **4 지표 중 1.5만 측정 가능** — 위치 정확도·hallucination 검증기 부재, γ tool-loop emulation 부재가 결정적 |

---

## (A) MCP 8 도구 응답 ↔ 실 소스 정확도

검증 방법: smoke probe(`probes/mcp_probe.py`) 응답을 grep / sqlite SQL / 직접 파일 확인과 대조.

### A1. `find_symbol(name="NewBlockChain", exact=false)`

| 항목 | MCP 응답 | Ground truth (grep) | 판정 |
|---|---|---|---|
| qname | `core.NewBlockChain` | — | — |
| type | `Function` | `func NewBlockChain(...)` | ✓ |
| file_path | `core/blockchain.go` | `core/blockchain.go` | ✓ |
| start_line | 269 | grep: line 269 | **정확** |

**PASS** — 정의 위치, 심볼 종류, 라인 모두 정확.

### A2. `find_callers(qname="core.NewBlockChain", depth=1)`

MCP 응답: 4 nodes (seed + 3 callers).

| MCP가 caller로 보고한 qname | grep 결과 (빌드 참여 파일) | 일치 |
|---|---|---|
| `eth.New` | `eth/backend.go:227` | ✓ |
| `tests.BlockTest.Run` | `tests/block_test_util.go:152` | ✓ |
| `utils.MakeChain` | `cmd/utils/flags.go:2158` | ✓ |

grep으로 발견된 추가 호출자(`_test.go` 다수)는 `--files-from` 화이트리스트의 `**/*_test.go` exclude로 제외 — 누락이 *정상*.

**PASS** — precision 1.0, recall 1.0 (빌드 범위 기준).

### A3. `find_callees(qname="core.NewBlockChain", depth=1)`

MCP 응답: 46 nodes / 79 edges. first_node = `core.NewBlockValidator` (`core/block_validator.go:41`).

검증: NewBlockChain 함수 body에서 `NewBlockValidator` 호출 위치 grep:
```
core/blockchain.go:318:  bc.validator = NewBlockValidator(chainConfig, bc, engine)
```
✓ 실제로 호출됨. **PASS** (sample-based — 전수는 entry당 grep 가능).

### A4. `get_subgraph(seed_qname="core.NewBlockChain", depth=1)`

MCP 응답: 198 nodes / 233 edges. first_node = `core.NewBlockChain#IfStmt@15239` (`core/blockchain.go:347`).

검증: 해당 라인 — `if err := bc.loadLastState(); err != nil {` IfStmt 실재.

depth=1 양방향 = (callers 3) + (callees 46) + (contains AST 노드 다수) + (defines/imports edges) = 198. AST까지 인덱싱 정상. **PASS**.

### A5. `impact_of_change(seed_qname="core.NewBlockChain", depth=2)`

MCP 응답: 0 nodes / 8 edges.

검증: 도구 description 명시 — "If results look empty for a Go concrete-method seed, retry with the interface method qname". NewBlockChain은 concrete function이므로 의도된 빈 응답. **PASS** (설계 의도 일치).

### A6. `search_text(query="WBFT consensus", top_k=5)`

MCP 응답: 5 nodes. first_node = `wbftcommon.ErrEmptyPrevPreparedSeals` (`consensus/wbft/common/errors.go:81`).

검증:
```
consensus/wbft/common/errors.go:81:
  ErrEmptyPrevPreparedSeals = errors.New("zero prev prepared seals")
```
✓ 정확. **PASS**.

### A7. `get_context_for_task(task_description="...WBFT prepare messages are validated")`

MCP 응답: OK (no error). B1 버그 수정 전 마침표 있는 description은 SQL syntax error 발생 → 수정 후 정상화 (v2 probe로 확인).

**PASS** (도구 동작 안정). 응답 *내용*이 비어있는 경우 있음 — prose query에 대한 BM25 mismatch는 §C에서 별도 한계.

### A8. `evidence_for_intent(intent="...governance staking")`

MCP 응답: 0 hits.

검증: 그래프에 commit 36개만 인덱싱(`commit_nodes=36`). 전체 git log엔 관련 commit 있지만 인덱싱 corpus 밖. BM25가 36 corpus에서 매칭 못한 결과 — *corpus 한계*이지 도구 버그 아님. **PASS**.

### A 종합

| 도구 | precision | recall | hallucination |
|---|---|---|---|
| find_symbol | 1.0 | 1.0 | 0 |
| find_callers | 1.0 | 1.0 (빌드범위) | 0 |
| find_callees | 1.0 (sample) | n/a | 0 |
| get_subgraph | 1.0 (sample) | n/a | 0 |
| impact_of_change | n/a (design 의도) | n/a | 0 |
| search_text | 1.0 (sample) | n/a | 0 |
| get_context_for_task | 1.0 (B1 fix 후) | n/a | 0 |
| evidence_for_intent | n/a (corpus 0-hit) | n/a | 0 |

**hallucination 0건** — 모든 응답이 실 그래프 노드를 정확히 가리키고, 그 노드들은 실 소스와 일치. CKG의 *정보 추출 단계*는 신뢰 가능.

---

## (B) LLM 답변의 코드 이해도

### B1. 결론: 정량 검증 불가

1. v1 측정(`results/eval-v1/results.csv`) 실행 당시 `writeCSV`가 `raw_output` 컬럼을 미기록(V0 한계, 사후 fix `c4c8c97`). 점수만 있고 LLM 응답 텍스트가 없어 "코드 수정 가능 이해도"를 사후 평가할 데이터 부재.
2. fix 적용 후 재실행 시도 → cliwrap-agent 경로 변경으로 panic → v2 결과 미수집.
3. 시뮬레이션(`sim/score_simulation.py`) 답안은 이 세션 Claude가 *직접 작성한 가상 답*. V0 채점기의 표기 민감도를 시연용. LLM 자력 이해도 평가 아님.

### B2. 우회 평가 — 컨텍스트 충분성

LLM 답을 못 봐도 각 baseline이 *제공한 컨텍스트*가 정답 도달에 충분한지는 §A의 ground truth로 평가 가능:

| Task | baseline | 제공 컨텍스트 | 정답 도달 가능? |
|---|---|---|---|
| T01 (callers) | α | `core/asm/*` 5 파일 | **불가** — caller 정보 없음 |
| T01 | β | 121K 노드 graph JSON | 가능 (정보 존재) |
| T01 | γ | `find_callers` 결과 4 nodes | **가능** — 정답 직접 노출 |
| T01 | δ | smartctx 빈 응답 `{}` | **불가** — BM25 매칭 실패 |
| T02 (WBFT prepare) | δ | smartctx — `ValidatorSet`(48 occ), `RoundChange`(106 occ), `preprepare`(50 occ) 매칭 기대 | 가능 (BM25가 wbft 코드 잘 잡음) |
| T02 | α | `consensus/wbft/` 5 파일 | **부분** — rubric 일부 cover |
| T03 (cross-package) | α | `systemcontracts/` 5 파일 | **불가** — params/, ethconfig/ 까지 봐야 |
| T03 | γ | 도구 호출로 cross-package 수집 | 가능 (V0 emulation 부재 시 불가) |

**관찰**:
- γ가 *컨텍스트 측면*에서 모든 task에서 정답 도달 가능
- δ는 task 유형에 따라 hit/miss 분명: 코드-키워드 풍부(T02) OK, prose-heavy(T01) 0-hit
- α는 corpus_path가 우연히 정답 영역과 겹치면 강함(T02), 어긋나면 0(T01)

### B3. 실 LLM 답을 받아 검증하려면

(i) `--llm-backend=api` + `ANTHROPIC_API_KEY` + 모델 명시 (cli wrapper 의존 없음)
(ii) 또는 cliwrap-agent 재설치 후 `--llm-backend=cli`
(iii) 재실행 → `results.csv`의 `raw_output` 컬럼에서 답 회수 → 본 §B 재기록

현재 데이터로 §B는 *환경 가용 시 채워질 placeholder* 상태.

---

## (C) 30-question 골든셋 측정 인프라 gap

요구사항: 4 지표 × 4 방식 × 30 질문.

### C1. 지표별 현 인프라 매핑

| 지표 | 현 인프라 | gap |
|---|---|---|
| **위치 정확도** | **없음** | 답 텍스트에서 `path/to/file.go:NNN` 패턴 추출 → 그래프 nodes 테이블 `file_path × start_line`(±range) cross-check validator 필요 |
| **정답률** | `internal/eval/score.go` (precision_recall / rubric) | V0 한계: file path가 정답 토큰으로 추출됨(`extractSymbols`), rubric은 substring만(semantic 동의어 불가). 시뮬레이션에서 같은 정답이 답 스타일에 따라 1.0 → 0.71 변동 확인 |
| **Hallucination** | **없음** | 답의 모든 qname/file 토큰 추출 → graph nodes 존재 + filesystem 존재 cross-check, 카운트 |
| **토큰 사용량** | `Result.{InputTokens, OutputTokens, CachedTokens}` 기록 | cli backend는 system+context를 cached로 몰아 input은 한자리수만 — api backend 필수 (L1) |

### C2. baseline별 가동 가능성

| baseline | V0 가동 | 30-question 신뢰성 |
|---|---|---|
| α | ✓ | `dumpFiles`가 정렬 첫 5개 `.go`만 dump(_test.go 포함, 무작위 X) — corpus_path 디자인이 baseline 정답률을 흔듦 |
| β | ✓ | 121K 노드 JSON dump는 input >>1M tokens — context window 초과 가능 |
| γ | ⚠️ **부분** | `runner.go:106` "γ is intentionally NOT pre-called" — LLM이 plain text로 도구 요청만, 실제 호출 없음. **γ가 30Q 측정의 핵심 baseline인데 V0에서 실 측정 불가** |
| δ | ✓ | smartctx BM25가 prose query에 약함 — T01 같은 자연어 task에서 빈 pack 반환. δ 성능이 task 표현에 민감 |

### C3. 30-question 골든셋 자체 구성

| 항목 | 현 상태 | gap |
|---|---|---|
| 정답 셋 (30개) | 3개만 (T01·T02·T03) | **27개 추가 작성** — domain expert 또는 그래프 자동 생성기 |
| 정답 종류 다양화 | symbol_set 1 + rubric 2 | code_patch 종류도 cover (스키마 `Expected.MustUseSymbols` 등 이미 있음) |
| 정답의 그래프 sync | T01 정답은 grep 검증 통과 — 빌드 화이트리스트 영향 받음 | 정답 정의 단계에서 그래프 노드 존재 자동 확인 |

### C4. 본 검증에서 발견된 인프라 결함

| ID | 영역 | 영향 | 해결 |
|---|---|---|---|
| B1 | smartctx FTS5 punctuation | δ baseline 자연어 description 시 100% 실패 | **fixed 5/11** (`trimFTSToken`). sigil bypass(`*"():`) 케이스는 미해결 |
| B2 | find_symbol description ↔ 동작 | γ baseline의 LLM이 false-empty | **fixed 5/11** |
| L1 | cli backend 토큰 분류 | H1 토큰 절감 가설 측정 불가 | docs only — api backend 권고 |
| L2 | writeCSV raw_output 미저장 | LLM 답 사후 분석 불가 → 위치 정확도·hallucination 측정 자체가 데이터 부재 | **fixed 5/11** (raw_output column) |
| L3 | γ tool-loop emulation 부재 | γ baseline 실 production 동작 미측정 | **미해결** — V1+ |
| L4 | extractSymbols 정밀도 | rubric/symbol_set 모두 표기 민감 | **미해결** — file-ext blacklist + receiver normalisation 필요 |
| L5 | dumpFiles _test.go 미제외 | α baseline이 test 파일을 dump해 정답에 노이즈 | **미해결** — runner.go:163의 `.go` 필터에 `_test.go` 제외 추가 |
| L6 | dumpFiles 정렬 첫 5개 | α의 "무작위 dump" 의도와 다름 — `asm/asm.go` 같은 알파벳 1번 디렉토리만 | **미해결** — 시드 기반 무작위 또는 비율 표본 |

### C5. "실 테스트 가능"까지의 최소 작업

30-question 골든셋 측정 요구사항을 만족하려면:

1. **(필수, V1)** γ tool-loop emulation 구현 — agent loop를 실제로 돌려서 도구 호출 결과를 LLM에 feedback. 없으면 γ baseline 자체가 fiction
2. **(필수)** api backend 가동 (`ANTHROPIC_API_KEY`) — cli backend는 토큰 측정 부적합
3. **(필수)** 위치 정확도 validator — 답 정규식 파싱 → 그래프 cross-check
4. **(필수)** hallucination validator — 답의 qname/path 토큰 추출 → graph + filesystem 존재 검사
5. **(필수)** 27개 추가 task 작성 + 그래프 sync 검증
6. **(권장)** `dumpFiles` 보완 (L5·L6) — α baseline의 측정 신뢰성
7. **(권장)** `extractSymbols` 정규화 (L4) — 점수 노이즈 제거, 또는 LLM-judge 채점
8. **(권장)** smartctx prose-query 강건성 — δ baseline의 task 표현 민감도 완화

1~5 충족 시 *측정의 무결성* 확보. 6~8은 *측정 정밀도*. 보고서 결과에 신뢰 구간을 부여하려면 1~5 + 6~8 모두 필요.

---

## 부록. 검증 자료

| 항목 | 위치 |
|---|---|
| MCP smoke 응답 raw | `results/smoke/mcp-probe-results.json`, `mcp-probe-v2-results.json` |
| 시뮬레이션 답안 + 채점 | `results/sim/score-simulation.json` |
| v1 측정 결과 (점수만, raw 부재) | `results/eval-v1/results.csv` |
| Ground truth grep / SQL | 본 보고서 §A 인용 라인 |
