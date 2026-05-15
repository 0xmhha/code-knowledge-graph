# CKG MCP Verification Report — go-stablenet-latest

> Jira ticket: 1.2 MCP 표준 서버(ckg mcp) — 6개 조회 도구 + 1.3 평가 연동(α/β/γ/δ)
> 검증 일자: 2026-05-11
> 대상 코드: `${STABLENET_SRC}` (go-stablenet-latest 체크아웃)
> CKG: `${CKG}` (기본값 `bin/ckg`)

## 1. 요약

| 검증 항목 | 결과 |
|---|---|
| 6개 필수 MCP 도구 등록·동작 | **PASS** (find_symbol / find_callers / find_callees / get_subgraph / search_text / get_context_for_task) |
| 추가 도구 동작 | PASS (impact_of_change, evidence_for_intent) |
| `ckg eval` 4-방식 인프라(α/β/γ/δ) | **PASS** — `--baselines alpha,beta,gamma,delta` 1:1 매핑 |
| 토큰 절감 가설(H1) 정량 측정 | **부분** — cli backend 토큰 분류 한계로 cli 모드에서는 H1 비교 신뢰도 낮음 |
| 결과 채점(score) 정확도 | **부분** — V0 채점기/extractSymbols 한계, raw output CSV 미저장 |

요구사항에 명시된 핵심 능력은 모두 구현·동작. 정량 평가의 측정 정확도는 cli backend 한계와 채점기 V0 한계가 남아있으며, 보고서 §5에 명시.

## 2. 검증 환경 준비

### 2.1 빌드 화이트리스트

`.claude/docs/BUILD_SOURCE_FILES.md`(분석일 2026-03-23, dev `bf17c9607`)의 781개 파일 / 160 패키지를 `ckg build --files-from` JSON으로 변환:

- 파서: `/tmp/ckg-stablenet-prep/build_filterlist.py` (정규식으로 마크다운 표 781행 추출 → 100% 매치)
- 출력: `/tmp/ckg-stablenet-prep/stablenet-files.json` (`{include: [...781 paths], exclude: ["**/*_test.go","**/testdata/**"]}`)

**문서 ↔ 디스크 drift**: 3개 파일이 문서에는 있지만 현재 디스크에 없음 (`core/stablenet_genesis.go`, `eth/protocols/eth/tracker.go`, `eth/protocols/snap/tracker.go`). 분석 이후 2026-05-11까지 약 2개월간 dev 브랜치에서 이동/삭제된 것으로 보임. ckg가 skip하므로 빌드에는 영향 없음.

### 2.2 그래프 빌드

```
ckg build --src=go-stablenet-latest --files-from=stablenet-files.json \
          --out=/tmp/ckg-stablenet --lang=go
```

| 지표 | 값 |
|---|---|
| 입력 파일 (allowlist 통과) | 778 (.go) |
| 출력 노드 | 121,088 |
| 출력 엣지 | 399,256 |
| Temporal G6 (git history) | 36 commits / 5,416 hunks / 188,836 changed_in edges |
| graph.db 크기 | 169 MB |
| 빌드 시간 | ~45 s |

### 2.3 무결성

`ckg audit --src=... --graph=/tmp/ckg-stablenet`:
- DB 1315 files vs `go/packages` 1259 files
- DB only (over-included): 59 — 대부분 git history hunk가 추가한 플랫폼별 변형 / 이미 삭제된 파일 (예: `core/stablenet_genesis.go`)
- Build only (missing from DB): 3 — `scripts/cmd/*` (BUILD_SOURCE_FILES.md 화이트리스트 밖). 정상

verdict: DRIFT — **의도된 차이.** allowlist 정책이 darwin/arm64 build set만 인덱싱한 결과.

## 3. MCP 도구 검증 (smoke)

`ckg mcp --graph=/tmp/ckg-stablenet` stdio 서버에 NDJSON JSON-RPC 직접 호출. probe 스크립트: `/tmp/ckg-stablenet-prep/mcp_probe.py`.

| 도구 | 호출 인자 | 결과 |
|---|---|---|
| `initialize` | protocolVersion=2024-11-05 | OK |
| `tools/list` | — | **8 tools** (요구사항 6 + impact_of_change + evidence_for_intent) |
| `find_symbol` | `name="NewBlockChain"`, `exact=false` | 1 node → seed=`core.NewBlockChain` |
| `find_callers` | qname=core.NewBlockChain, depth=1 | 4 nodes / 3 edges |
| `find_callees` | qname=core.NewBlockChain, depth=1 | 46 nodes / 79 edges |
| `get_subgraph` | seed_qname=core.NewBlockChain, depth=1 | 198 nodes / 233 edges |
| `impact_of_change` | seed_qname=core.NewBlockChain, depth=2 | 0 nodes / 8 edges (도구 description의 권고대로 interface qname 재시도 필요) |
| `search_text` | query="WBFT consensus", top_k=5 | 5 nodes |
| `get_context_for_task` | task_description="...WBFT prepare..." (no period) | OK |
| `evidence_for_intent` | intent="...governance staking..." | OK (0 hits) |

전체: **10/10 OK**.

### 3.1 발견된 CKG 버그

#### B1. `get_context_for_task` FTS5 query에 마침표 escape 누락

task_description 끝에 `.`이 있으면 smartctx 토크나이저가 `validated.*` 같은 토큰을 생성, FTS5가 `syntax error near "."` 발생:

```
fts search "Investigate* OR how OR WBFT* OR ... OR validated.*":
SQL logic error: fts5: syntax error near "." (1)
```

재현: `tools/call` `get_context_for_task` with `{"task_description":"... validated."}`.

**위치**: `pkg/smartctx/smartctx.go` BuildContext의 토크나이저 단계.
**영향**: 자연어 task description(보통 마침표로 끝남)에 그대로 호출하면 100% 실패. δ baseline도 동일 영향.
**워크어라운드**: description 끝의 punctuation 제거. 영구 해결은 토크나이저에서 non-alnum 제거.

#### B2. `find_symbol`의 도구 description ↔ 동작 불일치

도구 설명: `"Find symbols by name or qualified name."`
실제 동작: `FindSymbol(name, lang, exact)`는 항상 `qualified_name`을 매칭. `exact=true`면 정확 일치, `exact=false`면 접미 일치. 즉 단일 이름("NewBlockChain")으로 `exact=true` 호출 시 0 결과.

**영향**: LLM 에이전트가 description만 보고 `exact=true`로 호출하면 false-empty. 권고 — description을 "by qualified name; pass exact=false for suffix matching on a bare symbol name"으로 명확화.

## 4. 4-방식 평가 (`ckg eval --baselines alpha,beta,gamma,delta`)

### 4.1 Task 설계

| ID | 유형 | 검증 가설 | corpus_path |
|---|---|---|---|
| T01 | `symbol_set` (precision_recall) | γ(find_callers) 우위 | `.../core` |
| T02 | `rubric` | δ(get_context_for_task) 도메인 이해 우위 | `.../consensus/wbft` |
| T03 | `rubric` cross-package | α(좁은 raw dump) 한계 노출 | `.../systemcontracts` |

전체 task YAML: `/tmp/ckg-stablenet-prep/tasks/*.yaml`.

### 4.2 측정 결과

12 측정점 (3 tasks × 4 baselines). 전체 raw CSV: `/tmp/ckg-stablenet-prep/eval-results-full/results.csv`.

#### 4.2.1 점수 매트릭스

| task \\ baseline | α (file dump) | β (subgraph dump) | γ (find_*+search) | δ (smart context) |
|---|---|---|---|---|
| T01 callers (P/R) | 0.000 | 0.000 | 0.000 | 0.000 |
| T02 WBFT prepare (rubric) | **1.000** | 0.600 | 0.400 | 0.600 |
| T03 syscontract v2 (rubric) | 0.800 | 0.800 | 0.800 | 0.800 |
| **avg** | **0.600** | 0.467 | 0.400 | 0.467 |

#### 4.2.2 토큰·레이턴시

| baseline | avg input_tokens | avg output_tokens | avg cached_tokens | avg latency_ms |
|---|---|---|---|---|
| α | 9 | 4,191 | 251,628 | 66,154 |
| β | 9 | 2,748 | 136,120 | 38,881 |
| γ | 10 | 3,337 | 300,085 | 68,141 |
| δ | 11 | 4,734 | 334,824 | 80,996 |

#### 4.2.3 H1 / H2

- **H1** δ vs α input-token 절감: **-22.2%** (target ≥ 50%) → **FAIL** (단, L1 한계로 cli 백엔드에서 H1은 신뢰도 낮음 — input_tokens가 한 자리수에 머무름)
- **H2** δ score - α score: **-0.133** (target ≥ 0) → **FAIL**

#### 4.2.4 결과 해석 — *task 디자인의 영향 정확히 관찰됨*

각 baseline의 차이를 의미 있게 드러내려는 의도였지만, 실제 결과는 §1 insight가 예측한 함정을 그대로 보여줌:

1. **T01 (symbol_set) — 전부 score 0**: L2 한계(extractSymbols, raw_output 미저장)로 LLM이 어떻게 답했는지 사후 확인 불가. γ는 24.8s 동안 동작했고 output 863 tokens이라 분명 답을 시도했지만 정답 매칭 안 됨. 정답 표기와 LLM 출력 표기의 차이가 의심됨 — 채점기 V0 한계 직접 증거.

2. **T02 (WBFT prepare) — α가 최고점 (1.0)**: corpus_path를 `consensus/wbft`로 좁게 잡은 결과 α가 무작위 dump 5 파일로도 5개 rubric 키워드를 충분히 커버. 도리어 γ가 0.4로 가장 낮음 — γ는 LLM이 자유 텍스트로 도구를 요청하지만 V0 emulation에서 실 호출이 없음(§L3 한계). 즉 *task의 corpus_path 폭이 baseline 비교를 결정짓는다*는 §1 insight의 실증.

3. **T03 (syscontract upgrade) — 4 baseline 동률 0.8**: rubric 항목이 코드베이스 어디서든 흔히 등장하는 단어("hard fork", "go:embed", "params" 등)로 표현돼 substring 채점에서 모두 4/5 hit. rubric을 더 특이적으로 잡지 않으면 baseline 차별화가 안 됨.

**핵심**: 4-방식 평가 *인프라*는 동작하며 12개 측정점을 일관되게 산출한다. 다만 **이 task 셋으로는 baseline 우열을 판단할 수 없다** — task 재설계가 의미 있는 비교의 전제조건.

## 5. 측정 한계 및 보고

### L1. cli backend 토큰 분류

`--llm-backend=cli`는 `claude` CLI를 spawn. cli-wrapper는 system + 컨텍스트를 `cache_read_tokens`로 분류, 사용자 메시지만 `input_tokens`로 기록. 그래서 α/β/γ/δ 4 baseline 모두 input_tokens 한 자리수 → H1(δ vs α input token 절감) 가설을 cli 모드로는 측정 불가.

**권고**: H1/H2 정량 비교는 `--llm-backend=api`(ANTHROPIC_API_KEY 필요)로 수행.

### L2. eval V0 채점기 한계

- `RawOutput`이 `Result` 구조엔 있지만 `writeCSV`(`internal/eval/runner.go:226`)가 빼고 9 컬럼만 기록 → 사후 분석 시 LLM 응답 자체를 볼 수 없음.
- `extractSymbols`(`internal/eval/score.go:142`)는 응답에서 모든 "pkg.Func" 점 포함 토큰을 수집. 따라서 응답에 질문 대상 자체(`core.NewBlockChain`)가 등장하면 그것도 답으로 잡혀 precision 하락. LLM이 `*eth.Ethereum.New`처럼 receiver를 붙이면 expected `eth.New`와 표기 다름 → match miss.
- rubric scoring(`score.go:42`)은 단순 substring(≥0.6 토큰 매치). semantic 동의어를 잡지 못함.

**권고**: 고신뢰 평가가 필요하면 (a) writeCSV에 raw_output 추가하고 (b) extractSymbols를 정규화(strip receiver 등) 또는 LLM-judge 채점기로 확장.

### L3. γ baseline의 tool-loop emulation 부재

`runner.go:106`: "γ is intentionally NOT pre-called — emulating the multi-turn cost, we let the LLM ask in plain text." 즉 V0에서 γ는 LLM이 plain text로 도구를 요청하고 실 호출이 일어나지 않음. γ의 실 production 동작(다중 턴 tool loop)을 측정한 게 아님.

**권고**: 진짜 tool-loop emulation은 V1+ 작업 후보.

## 6. 결론

요구사항 1.2에 명시된 **6개 조회 도구는 모두 등록·동작**하며, go-stablenet 코드베이스(778 파일 / 121K 노드 / 399K 엣지)에 대해 의미 있는 결과를 반환한다. 1.3의 4-방식 평가 인프라도 별도 구현되어 있어 task YAML만 작성하면 즉시 가동 가능하다.

다만 본 검증에서 다음을 함께 확인했다:
- CKG 자체 버그 2건 (FTS punctuation, find_symbol description) — 사용 시 워크어라운드 필요
- eval V0의 측정 정확도 한계 3건 — 정량 비교에 사용할 때는 보완 필요

요구사항 충족도 평가:
- **구현 측면**: PASS — 모든 요구 도구가 동작
- **정량 검증 측면**: 부분 PASS — cli 백엔드 측정 한계로 H1/H2 정량 비교는 api 백엔드로 재실행 필요

## 7. 버그 수정 회귀 검증 (2026-05-11 추가)

§3.1의 B1·B2 두 버그에 대해 CKG 측 수정이 반영됐고(`bin/ckg` 5/11 15:53 재빌드본), 동일 그래프 DB에 대해 회귀 probe(`mcp_probe_v2.py`)로 재검증.

### 7.1 코드 변경 확인

| 버그 | 변경 파일 | 변경 내용 |
|---|---|---|
| B1 | `internal/persist/sqlite.go` | `trimFTSToken()` 추가. `rewriteFTSQuery`가 각 토큰의 leading/trailing non-alnum을 제거한 뒤 `*` prefix 부여. 주석에 "Reported 2026-05-11 in go-stablenet VERIFICATION_REPORT §3.1 B1" 명시 |
| B2 | `internal/mcp/tools.go` | `find_symbol` description을 "by qualified_name. With exact=true (default), the input must match qualified_name exactly … With exact=false, the input is treated as a suffix"로 재작성. 주석에 "Description rewritten 2026-05-11 (VERIFICATION_REPORT §3.1 B2)" 명시 |

### 7.2 회귀 결과

| 케이스 | 입력 | 이전 결과 | 재검증 결과 |
|---|---|---|---|
| B1a (보고된 케이스) | `"...validated."` | `syntax error near "."` | **OK** ✓ |
| B1b (인접 케이스 — *신규 발견*) | `"Where does (NewBlockChain) get called, and why? Show callers!"` | (미테스트) | `syntax error near "does"` |
| B2a | `name="core.NewBlockChain", exact=true` | 0 (false-empty) | **1 node** ✓ |
| B2b | `name="NewBlockChain", exact=false` | 0 (false-empty) | **1 node** ✓ |
| B2c | `name="NewBlockChain", exact=true` | 0 | 0 (의도된 동작, 문서 명시) ✓ |

**판정**:
- **B1 (보고된 케이스): PASS** — trailing punctuation은 처리됨
- **B2: PASS** — description 권고대로 명확화됨

### 7.3 신규 발견 (B1 인접 케이스)

`rewriteFTSQuery`(`sqlite.go:642`)는 입력에 `*"():` 중 하나라도 있으면 **power-user 모드로 분기**해 raw query 그대로 FTS5에 전달 — 코멘트에 의도적 boundary로 명시. 그러나 자연어 task description에도 `(`나 `)`가 흔히 등장(예: "Where does (NewBlockChain) get called")해 false power-user 분류 위험. trimFTSToken을 거치지 않으므로 punctuation 노출 가능.

**권고 (참고용)**: power-user 분기 조건을 `*"` (또는 explicit FTS5 키워드)로 좁히거나, 자연어 path에서 입력을 사전 sanitize 후 sigil 검사. 본 검증 범위에서는 후속 결정 사항으로 남김.

## 8. 현 세션 직접 시뮬레이션 — T01 score 0의 원인 분리 (2026-05-11 추가)

cli backend가 사용 불가능(cliwrap-agent 경로 변경)한 상황에서, 이 세션의 LLM(Claude)이 직접 T01의 4 baseline에 답하고 **eval V0와 동일한 extractSymbols / PrecisionRecall 채점기**를 Python으로 포팅(`/tmp/ckg-stablenet-prep/sim/score_simulation.py`)해 적용. 입력 context는 실제 MCP 호출(`collect_inputs.py`)·`dumpFiles` 재현으로 확보.

### 8.1 점수 매트릭스 (T01)

각 baseline에 대해 두 가지 답변 스타일(`minimal` = qname만, `verbose` = 자연어 + file path 포함)로 채점:

| baseline | minimal | verbose | 비고 |
|---|---|---|---|
| α | 0.0000 | 0.0000 | input(asm/* 5 파일)에 NewBlockChain 미등장 → 답 불가 |
| β | **1.0000** | 0.7143 | 정답 3/3 알지만 verbose에 file path 끼면 precision 0.43 |
| γ | **1.0000** | 0.7143 | β와 동일 — 답은 안다 |
| δ | 0.0000 | 0.0000 | smartctx가 task description 매칭 못해 빈 context |

### 8.2 원본 eval score 0의 원인 분리

원래 v1 eval에서 T01 모든 baseline이 0.0이었던 원인을 시뮬레이션 결과와 교차해 분리:

| baseline | 원본 score | 시뮬레이션이 밝힌 원인 |
|---|---|---|
| α | 0 | 입력 정보 부재 — `dumpFiles`가 `corpus_path=core`에서 *정렬된 처음 5개 파일* = `asm/asm.go, asm/asm_test.go, asm/compiler.go, asm/compiler_test.go, asm/lex_test.go`만 dump. NewBlockChain 미등장 |
| β | 0 | LLM이 verbose 답을 했고 precision threshold 0.7을 verbose=0.71로 *간신히 통과* — 실제 cli 실행에선 file path 노이즈가 더 많아 threshold 미달했을 가능성 |
| γ | 0 | **V0 한계** — `runner.go:106` 명시: "γ is intentionally NOT pre-called". LLM이 plain text로 도구를 요청하지만 실제 호출은 없음. 즉 callers를 모름 → 정답 미응답 |
| δ | 0 | smartctx가 prose 형식 task description으로 BM25 검색 시 0-hit (구조적 키워드 우위) → 빈 pack 반환 → LLM 답 불가 |

### 8.3 핵심 결론

V0 eval로 baseline 우열을 정량 비교할 수 없는 이유가 *4가지 다른 메커니즘*으로 분산되어 있음:

1. **α**: 의도적 한계 — file dump의 정보 밀도가 낮음 (5 파일 × 4 KB)
2. **β/γ**: *답을 알면서도* 채점기가 답 표기 디테일(file path 노이즈)에 과민
3. **γ**: tool-loop 미에뮬레이션 — γ의 실 production 동작이 V0에서 측정되지 않음
4. **δ**: smartctx와 prose query의 도메인 불일치

§5의 L2(raw_output column) 추가 fix로 *답 표기 노이즈*는 사후 분석 가능해졌지만, *채점 함수 자체*는 그대로. 의미 있는 baseline 비교를 위해서는:
- (a) `extractSymbols`에 file extension blacklist (`.go`, `.ts`, `.sol` 등 제외) 또는
- (b) LLM-judge 채점기 + 동일 정답에 대해 표기 변형 robust
- (c) γ baseline에 실 tool-loop emulation

위 세 가지가 함께 적용될 때 H1/H2 가설이 의미 있게 측정 가능. 본 검증 범위에서는 한계 명문화에 그침.

### 8.4 산출물

- `/tmp/ckg-stablenet-prep/sim/score_simulation.py` — V0 채점기 Python 포트 + 8 시뮬레이션 케이스
- `/tmp/ckg-stablenet-prep/sim/score-simulation.json` — 케이스별 raw 결과
- `/tmp/ckg-stablenet-prep/sim/{alpha,beta,gamma,delta}-input.json` — 각 baseline의 실 input context (β/δ 빈 응답 포함)

---

## 부록 A. 산출물 파일

| 경로 | 내용 |
|---|---|
| `/tmp/ckg-stablenet-prep/build_filterlist.py` | BUILD_SOURCE_FILES.md → JSON 변환 파서 |
| `/tmp/ckg-stablenet-prep/stablenet-files.json` | 781-entry 화이트리스트 |
| `/tmp/ckg-stablenet/graph.db` | 121K-node 그래프 (169 MB) |
| `/tmp/ckg-stablenet-prep/mcp_probe.py` | MCP smoke probe 스크립트 (v1, 초기 검증) |
| `/tmp/ckg-stablenet-prep/mcp-probe-results.json` | smoke 결과 (10/10 OK) |
| `/tmp/ckg-stablenet-prep/mcp_probe_v2.py` | B1·B2 회귀 검증 probe |
| `/tmp/ckg-stablenet-prep/mcp-probe-v2-results.json` | 회귀 검증 결과 (B1·B2 PASS, B1 인접 케이스 신규 발견) |
| `/tmp/ckg-stablenet-prep/mcp-smoke.json` | bench-mcp-stdio 응답 (4 probes) |
| `/tmp/ckg-stablenet-prep/tasks/*.yaml` | 3개 task YAML |
| `/tmp/ckg-stablenet-prep/eval-results-full/` | ckg eval 결과 (CSV + report.md) |
