# Eval (4-Baseline) 동작 검토

> **대상**: `ckg eval --tasks=<glob> --graph=<dir> --baselines=alpha,beta,gamma,delta` 실행 흐름 — α/β/γ/δ baseline의 LLM 호출 + scoring + report 생성
> **참조 파일**:
> - `cmd/ckg/eval.go`
> - `internal/eval/{runner,baseline,llm,llm_cli,task,score,report}.go`
> - `eval/tasks/synthetic-T01-find-callers.yaml`, `synthetic-T02-rubric-deposit.yaml`
>
> **선행 문서**: `docs/EVAL.md`(사용법), `docs/analysis/MCP-QUERY-FLOW.md` § 6.2 (smart context 비대칭)
> **마지막 갱신**: 2026-05-05

> ⚠️ **Honest assessment**: Eval은 **V0 simplification이 다수 존재**합니다. γ baseline은 사실상 단일 LLM 호출(tool loop 없음), δ는 MCP의 진짜 smart context를 호출하지 않고 단순 SearchFTS 10건 덤프, β는 `seed=""` `depth=99`로 graph 전체를 dump합니다. 가설 H1(δ ≤ 50% α tokens) / H2(δ score ≥ α score)는 **현재 구현에서 수치적으로 의미 있는 검증을 하기 어렵습니다**. 본 문서는 이를 § 5에서 분명히 합니다.

---

## 목차

1. [진입 (CLI Entry)](#1-진입-cli-entry)
2. [실행 Flow](#2-실행-flow)
3. [4 Baseline 상세 동작](#3-4-baseline-상세-동작)
4. [Scoring](#4-scoring)
5. [현재 한계 / Gap (Critical)](#5-현재-한계--gap-critical)
6. [근본 원인 후보 (디버깅 시 우선 검사)](#6-근본-원인-후보-디버깅-시-우선-검사)
7. [측정값의 해석상 위험](#7-측정값의-해석상-위험)
8. [핵심 한 줄 요약](#8-핵심-한-줄-요약)

---

## 1. 진입 (CLI Entry)

```bash
ckg eval --tasks='eval/tasks/synthetic-*.yaml' \
         --graph=/tmp/ckg-synth \
         --baselines=alpha,beta,gamma,delta \
         --out=eval/results
```

`cmd/ckg/eval.go`의 핵심 스텝:

1. `eval.LoadTasks(glob)` → YAML 파일 디코드 → `[]Task`
2. `selectLLMBackend(llmBackend, model, claudeBinary)` → `cli|api`
   - 기본값: `--llm-backend=cli` (Claude Code CLI binary 호출)
   - `--llm-backend=api` 시 `ANTHROPIC_API_KEY` 필요
3. `eval.Run(ctx, tasks, baselines, graphDir, llm, outDir)`

### 1.1 Task YAML schema

```yaml
id: T01
corpus: synthetic
corpus_path: testdata/synthetic
description: |
  List all functions in this codebase that ultimately call Vault.deposit.
expected_kind: symbol_set      # symbol_set | code_patch | rubric
expected:
  symbols:
    - "service.Vault.Deposit"
    - "VaultService.depositFn"
    - "api.Handler.HandleDeposit"
scoring:
  type: precision_recall       # precision_recall | rubric
  threshold:
    precision: 0.7
    recall: 0.7
```

eval/tasks/ 안의 task는 현재 **2개만 존재** (synthetic-T01, synthetic-T02). real-corpus task는 없음.

---

## 2. 실행 Flow

### 2.1 `eval.Run` 메인 루프

```go
func Run(ctx, tasks, baselines, graphDir, llm, outDir) ([]Result, error) {
    defer llm.Close()
    store := persist.OpenReadOnly(graphDir + "/graph.db")
    stale := isStale(store, graphDir)         // git rev-parse 비교

    var results []Result
    for each task t:
        for each baseline b:
            res := runOne(ctx, llm, store, t, b, stale)
            // 에러 발생 시 stderr 경고 후 skip — 결과에서 빠짐
            results = append(results, res)

    expected := len(tasks) * len(baselines)
    dropped := expected - len(results)
    if dropped > 0:
        stderr.Print("X/Y pairs failed; H1/H2 may be biased")

    writeCSV(outDir/results.csv)
    WriteReport(outDir/report.md)
}
```

⚠️ **Drop된 pair는 csv에 들어가지 않음**. 가설 검증 모집단이 비대칭이 됨.

### 2.2 `runOne` per (task, baseline)

```go
func runOne(ctx, llm, store, t, b, stale) (Result, error) {
    start := time.Now()
    system := SystemPrompt(b)            // baseline별 정해진 string
    user := t.Description

    // baseline별 user prompt 가공 (아래 §3 참조)
    if b == BaselineAlpha:
        user += dumpFiles(t.CorpusPath, 5, 4000)
    if b == BaselineBeta:
        user += jsonString(store.SubgraphByQname("", 99))
    if b == BaselineDelta:
        user += smartContext(store, t.Description)  // ← MCP와 다른 simplified 경로
    // β/γ/δ 모두 LLM은 single-shot — tool round-trip 없음

    out := llm.Complete(ctx, system, user)

    score, calls := scoreTask(t, out.OutputText)
    return Result{...}
}
```

### 2.3 LLM backend

| Backend | Constructor | 사용 |
|---|---|---|
| `cli` (default) | `NewCLIClient(opts)` | Claude Code CLI binary spawn (cli-wrapper Manager) |
| `api` | `NewAPIClient(model)` | Anthropic Messages API (no tool, single-shot) |

`api` backend의 `Complete`:
```go
msg, _ := c.Messages.New(ctx, MessageNewParams{
    Model:     l.model,
    MaxTokens: 4096,
    System:    [{Text: system}],
    Messages:  [NewUserMessage(NewTextBlock(user))],
})
```

⚠️ **Tool 정의 미전달**: API 호출에서 `Tools: ...` 파라미터가 없음 → LLM은 tool_use 블록을 만들 수 없음 → **β/γ baseline의 "tool 사용"은 모두 시뮬레이션**.

---

## 3. 4 Baseline 상세 동작

### 3.1 α (alpha) — raw file dump

| Field | Value |
|---|---|
| Tools | 없음 |
| System prompt | "Raw source files are appended below the task description." |
| User prompt 가공 | `t.Description + dumpFiles(CorpusPath, count=5, perFileLimit=4000)` |

`dumpFiles` 동작:
```
filepath.Walk(CorpusPath):
   skip if dir
   if count <= 0: SkipAll
   if ext not in {.go, .ts, .sol}: skip
   read file, truncate to perFileLimit (4000 bytes)
   append "=== <path> ===\n<content>"
   count--
```

⚠️ **첫 5개 파일만**: walk 순서(lexicographic) 기반 → 항상 같은 파일들이 dump됨. task description과 무관. **α의 tokensum은 corpus 크기와 무관하게 거의 일정**.

### 3.2 β (beta) — get_subgraph 1번

| Field | Value |
|---|---|
| Tools | (시뮬레이션) get_subgraph |
| System prompt | "Call get_subgraph once to retrieve the entire graph, then answer." |
| User prompt 가공 | `t.Description + jsonString(store.SubgraphByQname("", 99))` |

`SubgraphByQname("", 99)`:
- seed qname이 `""`라 빈 문자열로 노드 lookup → 결과 0 또는 모든 노드 (구현 상세에 따라 다름)
- depth=99로 BFS → 사실상 **모든 노드/엣지 dump**
- 응답이 거대 (큰 corpus면 수백 MB JSON) → token 카운트 폭증

→ **β의 token cost가 이론상 가장 큼**. 작은 synthetic corpus에선 OK, real corpus에선 LLM context window를 넘김.

### 3.3 γ (gamma) — 5 granular tools

| Field | Value |
|---|---|
| Tools | (선언만) find_symbol, find_callers, find_callees, get_subgraph, search_text |
| System prompt | "Use find_symbol/find_callers/find_callees/get_subgraph/search_text as needed" |
| User prompt 가공 | **없음** (raw description만 전달) |

⚠️ **γ는 tool 호출 없이 LLM이 자체 지식으로 답함**. 코드 주석에 명시:
> "γ is intentionally NOT pre-called — emulating the multi-turn cost, we let the LLM ask in plain text. (Real tool-loop emulation arrives V1+.)"

→ γ가 보고하는 score는 LLM이 query를 자기 prior로 답한 것. corpus의 실제 코드를 못 봄.

### 3.4 δ (delta) — get_context_for_task ★

| Field | Value |
|---|---|
| Tools | (시뮬레이션) get_context_for_task |
| System prompt | "Call get_context_for_task ONCE with the user's task description, then answer." |
| User prompt 가공 | `t.Description + smartContext(store, t.Description)` |

⚠️ `smartContext` 함수 (`runner.go:195-203`):
```go
func smartContext(store, query) (string, error) {
    hits, _ := store.SearchFTS(query, 10)   // ⚠️ FTS 10건만
    return jsonString(hits), nil
}
```

vs `internal/mcp/get_context.go:buildContext`:
```
(a) Search 30건 retrieve
(b) QueryEdgesForNodes 1-hop expand
(c) score-fuse (BM25 + PR + usage)
(d) diversify
(e) pack within budget
→ {subgraph, bodies, summaries, ...}
```

→ **eval δ가 측정하는 것은 MCP의 smart context가 아닙니다**. 단순 FTS 10건 dump. 코드 주석에 admit:
> "Should be moved into a shared package in V1."

---

## 4. Scoring

### 4.1 `scoreTask` dispatcher

```go
switch t.Scoring.Type:
case "precision_recall":
    got := extractSymbols(output)
    p, r := PrecisionRecall(got, t.Expected.Symbols)
    return (p + r) / 2, 0          // ⚠️ NumToolCalls는 항상 0
case "rubric":
    hits, total := RubricCheck(output, t.Expected.Rubric)
    return hits/total, 0
default:
    return 0, 0                     // ⚠️ "code_patch" 미구현 — 무조건 0점
```

### 4.2 `extractSymbols`

```go
for each token in FieldsFunc(s, by space/comma/newline/backtick/quote):
    if Contains(tok, ".") and !HasPrefix(".") and !HasSuffix("."):
        out = append(out, Trim(tok, ".:;()"))
```

⚠️ "Crude but adequate for V0":
- "Mr.Foo" 같은 영문 자연어도 symbol로 추출
- 도메인 (`example.com`) 추출
- 패키지 경로 (`github.com/foo`) 도 추출
- 진짜 식별자 (`pkg.Func`)와 구분 안 됨

→ precision_recall scoring은 false positive 풍부.

### 4.3 `RubricCheck`

```go
const rubricMatchThreshold = 0.6
const stopWordMinLen = 4

for each rubric item:
    eligible := words.filter(len >= 4)
    match := words.filter(strings.Contains(low_output, w))
    if match/eligible >= 0.6: hits++
```

⚠️ 단순 substring 매칭 + 짧은 단어(<4자) 무시. 의미적 정합성은 보지 않음.

---

## 5. 현재 한계 / Gap (Critical)

### 5.1 가설 H1/H2의 실험 설계 문제

`docs/EVAL.md`의 가설:
- **H1**: δ ≤ 50% of α tokens
- **H2**: δ score ≥ α score (no regression)

**현재 구현으로 검증 시 문제**:

| 가설 | 문제 |
|---|---|
| H1 (token) | α는 단순 5개 파일 4000B truncate → corpus와 무관 거의 고정. δ는 SearchFTS 10건 → query 따라 변동. 둘 다 corpus의 진짜 정보량을 반영하지 않음 |
| H2 (score) | γ는 tool 사용 안 하고 LLM 단독 답변 → corpus 무관. δ가 γ보다 잘하는지 확인할 baseline이 약함 |
| 모집단 | (task, baseline) pair 중 LLM 호출 실패는 silent drop → 가설별 N이 다를 수 있음 |
| Variance | 단발 실행, multiple seed 미지원 → variance 측정 불가 |

### 5.2 LLM tool round-trip 부재

`api` backend가 `Tools` 파라미터 미전달 → LLM이 실제로 도구를 호출할 수 없음. 모든 baseline의 "tool 사용"은:
- α: file dump (LLM은 그냥 text 받음)
- β: subgraph dump (동일)
- γ: 아무것도 안 줌 — LLM은 지식으로만 답
- δ: SearchFTS 10건 dump (동일)

→ **실제 production MCP 사용 시나리오와 eval 측정은 수치적으로 다른 게임**.

### 5.3 δ ≠ MCP smart context

§ 3.4에서 보았듯, `eval.smartContext`는 MCP의 `buildContext`와 다른 함수. 따라서 eval δ에서 좋은 스코어가 나와도 production MCP에서 그대로 재현된다고 단언할 수 없음. 반대로 production δ가 eval δ보다 더 풍부한 컨텍스트(subgraph + bodies + summaries)를 제공.

### 5.4 `code_patch` scoring 미구현

`Task.Scoring.Type == "code_patch"`이거나 그 외 임의 타입이면 `scoreTask`는 `0, 0` 반환. eval/tasks/*.yaml 안에 code_patch 타입이 있다면 무조건 0점. 현재 task가 2개뿐(둘 다 precision_recall/rubric)이라 표면화되지 않음.

### 5.5 `dumpFiles` 결정성 + 무관성

α의 file dump가 task description과 무관 → α가 답할 수 있는 문제는 corpus 첫 5개 파일이 task와 우연히 관련 있을 때만. 실질적으로 **α는 무지성 답변 baseline에 가까움**.

### 5.6 isStale의 둔감함

```go
func isStale(store, graphDir) bool {
    m, _ := store.GetManifest()
    if m.StalenessMethod != "git": return false
    out, _ := exec.Command("git", "-C", m.SrcRoot, "rev-parse", "HEAD")
    return strings.TrimSpace(out) != m.SrcCommit
}
```

- git checkout이 아니면 항상 false (stale 모름)
- uncommitted 변경은 감지 못 함 (HEAD만 비교)
- StalenessMethod가 "git"이 아닌 빌드는 항상 false

### 5.7 Drop된 pair의 silent bias

LLM 에러로 실패한 pair가 csv에서 빠짐 → α가 100% 성공 / δ가 50%만 성공이면 평균 비교가 의미 잃음. stderr에 "X/Y pairs failed" 경고만 출력.

---

## 6. 근본 원인 후보 (디버깅 시 우선 검사)

### R1. **`ckg eval` 실행 시 ANTHROPIC_API_KEY 에러**
원인: 기본 backend가 `cli`(Claude Code binary)인데 `--llm-backend=api`로 지정했거나 docs가 그렇게 안내. API 키가 환경에 없으면 `ErrNoAPIKey`.
검증: `echo $ANTHROPIC_API_KEY`, `ckg eval --help | grep llm-backend`.
대응: `--llm-backend=api` 사용 시에만 키 필요. cli 백엔드면 Claude Code binary path를 `--llm-claude-binary`로 또는 PATH에 둠.

### R2. **β baseline이 OOM/context overflow를 일으킨다**
원인: `SubgraphByQname("", 99)`로 사실상 전체 그래프 JSON dump → 큰 corpus에서 수십~수백 MB.
검증: synthetic이 아닌 실제 corpus로 eval 시 LLM API에서 max context 에러.
대응: 코드 변경 — β baseline에 명시적 seed/depth 적용 필요. 현재는 V0 한계.

### R3. **δ score가 기대보다 낮다**
원인 후보:
- (a) `eval.smartContext`가 MCP buildContext가 아니라 SearchFTS 10건만 → context 빈약
- (b) FTS index에 source body 미포함 → 자연어 query miss
- (c) task description의 식별자가 corpus와 정확히 매칭 안 됨

검증: `sqlite3 graph.db "SELECT * FROM nodes_fts WHERE nodes_fts MATCH ? LIMIT 10" "<query>"` 직접 실행 후 결과 빈약 여부 확인.
대응: query에 식별자 키워드 명시. 또는 코드 변경으로 `eval.smartContext`를 `mcp.buildContext`와 통일 (V1+).

### R4. **γ score가 의외로 높다 / 낮다**
원인: γ는 LLM이 도구 없이 답하는 baseline. 즉 γ score는 LLM의 prior 능력 반영 — 코드 그래프 효과 측정 불가.
검증: 같은 task로 corpus 변경해도 γ score가 비슷하면 confirmed.
대응: γ를 의미 있는 baseline으로 만들려면 real tool loop 구현 (V1+).

### R5. **report.md에 H1/H2 결과가 이상하다**
원인 후보:
- (a) baseline별 N이 다름 (drop된 pair) → 평균 비교 왜곡
- (b) precision_recall이 `extractSymbols`의 noisy 추출 → 실제 정확도와 다름
- (c) rubric의 substring 매칭이 false positive

검증: results.csv를 직접 열고 task별/baseline별 N과 score 분포 확인.
대응: 신뢰도 낮음 — 가설 검증 결과는 indicative하게만 해석.

### R6. **`ckg eval` 실행이 매우 느리다**
원인 후보:
- (a) cli backend라면 매 호출마다 Claude Code 프로세스 spawn (cli-wrapper Manager가 재사용한다 해도 first call cost 큼)
- (b) tasks × baselines = N개 LLM 호출 직렬 실행 (병렬화 없음)
- (c) β baseline의 큰 JSON serialization

검증: `time ckg eval ...` + `--verbose`로 단계별 시간.
대응: V0 한계 — 작은 task suite/baseline subset부터 검증.

### R7. **`code_patch` 타입 task가 무조건 0점**
원인: `scoreTask`에 `code_patch` case 없음.
검증: `grep -n "case " internal/eval/runner.go` → precision_recall / rubric 외 없음.
대응: V1+ 구현 필요. 현재는 task YAML에서 `code_patch` 사용 금지.

### R8. **graph.db 누락 시 eval이 어떻게 실패하는가**
원인: `persist.OpenReadOnly`가 없으면 즉시 에러 → eval 시작 안 함.
검증: empty graphDir로 실행하면 stderr에 명확한 메시지.
대응: 정상 동작.

### R9. **isStale이 false인데 실은 stale**
원인: § 5.6 — git diff/uncommitted는 무시. 또는 StalenessMethod가 "git" 아니면 항상 false.
검증: 소스 변경 후 commit 안 한 채 eval → stale=false 보고.
대응: rebuild 강제하거나 stale 판정 로직 강화 (V0 한계).

---

## 7. 측정값의 해석상 위험

| Field in Result | 신뢰도 | 비고 |
|---|---|---|
| `InputTokens` / `OutputTokens` | 🟢 높음 | API 응답 그대로 |
| `CachedTokens` | 🟢 높음 | API 응답 |
| `Score` (precision_recall) | 🟡 중간 | extractSymbols 노이즈 |
| `Score` (rubric) | 🟡 중간 | substring 매칭, 의미 평가 X |
| `Score` (code_patch) | 🔴 가짜 | 항상 0 |
| `LatencyMS` | 🟢 높음 | wall clock |
| `NumToolCalls` | 🔴 항상 0 | V0 plumbing 미완 |
| `Stale` | 🟡 낮음 | git HEAD-only |
| `RawOutput` | 🟢 높음 | LLM raw text |

⇒ **Token / latency는 신뢰**, **score / tool call count는 indicative**.

---

## 8. 핵심 한 줄 요약

> **Eval은 "tool 사용 효과" 측정 도구로 의도되었으나, 실제로는 LLM single-shot completion + baseline별 prompt 가공만 수행합니다.** γ는 도구 없이 LLM 단독, δ는 MCP의 진짜 smart context가 아닌 SearchFTS 10건 dump, β는 graph 전체 dump (실제 corpus엔 부적절), α는 corpus 첫 5개 파일 truncate. **가설 H1/H2의 수치는 indicative하게만 해석**해야 하며, score는 noisy substring 기반이라 절대값보다 baseline 간 상대 비교에만 의미가 있습니다. NumToolCalls가 항상 0인 점도 "tool loop 미구현"의 직접 증거입니다.

---

## Appendix: 측정 신뢰도를 높이는 방향 (V1+ 후보)

| 영역 | 개선 방향 |
|---|---|
| **Tool loop** | API backend에 `Tools: [{name, schema}]` 전달 + tool_use round-trip 처리 |
| **δ 통일** | `eval.smartContext` → `mcp.buildContext` shared package 추출 |
| **α improve** | dumpFiles를 task-relevant 파일 선택으로 변경 (e.g. `Search` 후 dump) |
| **β improve** | seed/depth 명시 / 또는 큰 그래프엔 cluster 단위 sampling |
| **score** | precision_recall은 식별자 인지 토큰화 (Go `pkg.Type.Method` AST 파싱) |
| **rubric** | semantic embedding 비교 (cosine) 또는 LLM-as-judge |
| **code_patch** | scoring 구현 (must_use_symbols/must_call check, signature preservation) |
| **N replicas** | (task, baseline) 당 N회 실행으로 variance 측정 |
| **stale** | git diff + manifest mtime 비교 (현재 HEAD only) |
| **drop policy** | 실패 pair를 csv에 NaN/marker로 기록 (silent drop 금지) |

**End of eval flow analysis.** 본 문서는 eval 결과 해석 시 어디까지 신뢰하고 어디부터 indicative로 봐야 할지를 분명히 합니다.
