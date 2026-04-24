# CKG V0 — Next-Session Handoff (T32–T38)

| Field | Value |
|---|---|
| Date | 2026-04-24 |
| Working dir | `/Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph` |
| Branch / HEAD | `main` / `1469d60` (T31 complete) |
| Status | **31/38 tasks done (82%)** — Phases 1–8 complete (CP-1~CP-5 reached); Phases 9–10 remaining (T32–T38, CP-6+CP-7) |
| Working tree | clean (3 untracked: `docs/jira-tickets-*.md` — sourced externally, leave alone) |

---

## Quick start (cold-read, 5 분)

새 세션이 컨텍스트 없이 바로 시작 가능하도록:

```bash
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph
git log --oneline -5     # 마지막 5 커밋 확인
go test ./...            # 16 패키지 모두 PASS 확인
git status --short       # untracked 3개만 있어야 함
```

이어서 진행할 첫 작업: **T32 (Eval task YAML + baseline definitions)** — `/superpowers:subagent-driven-development` 스킬로 task-by-task + review 패턴 그대로.

핵심 문서:
- **Plan**: `docs/plan-ckg-v0-prototype.md` (8,983 lines, 38 tasks 전체)
- **Spec**: `docs/spec-ckg-v0-prototype.md` (1,442 lines, 결정 근거)
- **Study Guide**: `docs/STUDY-GUIDE.md` (Leiden / MCP / Tree-sitter 등 외부 개념)
- **본 문서**: 다음 세션의 작업 목록 + 운영 패턴

---

## 1. 진행 상황 요약

### Phase 진행 표

| Phase | Tasks | Status | Last Commit | Checkpoint |
|---|---|---|---|---|
| 1. Foundation | T1–T4 | ✅ 완료 | `dd37f71` | — |
| 2. Detect + Go parser | T5–T9 | ✅ 완료 | `f802608` | — |
| 3. Graph + cluster + score | T10–T14 | ✅ 완료 | `35c4c51` | — |
| 4. First vertical slice | T15–T17 | ✅ 완료 | `61f7e6f` | **CP-1** (`ckg build` Go-only) |
| 5. TS + Sol + xlang | T18–T21 | ✅ 완료 | `9e22ea1` | **CP-2** (3-language + binds_to) |
| 6. Viewer + `ckg serve` | T22–T26 | ✅ 완료 | `6429d15` | **CP-3** (3D viewer + Playwright) |
| 7. MCP server + `ckg mcp` | T27–T29 | ✅ 완료 | `e7ff3ba` | **CP-4** (6 tools via stdio JSON-RPC) |
| 8. Export-static | T30–T31 | ✅ 완료 | `1469d60` | **CP-5** (chunked JSON + auto-detect viewer) |
| **9. Eval framework** | **T32–T35** | **⏭️ 다음** | — | **CP-6** (4-baseline 측정) |
| **10. Corpus + CI + docs** | **T36–T38** | ⏭️ | — | **CP-7** (V0 DoD 완수) |

### 누적 결과물

- 단일 Go 바이너리 `ckg` (멀티 OS, modernc/sqlite + tree-sitter via CGO for parsers)
- 5 subcommand: `build` ✅ / `serve` ✅ / `mcp` ✅ / `export-static` ✅ / `eval` ⏭️
- 29 node types × 22 edge types
- Go (`go/packages`) + TS/JS (tree-sitter) + Solidity (vendored tree-sitter)
- xlang: Sol↔TS `binds_to` (name+ABI heuristic, INFERRED)
- 3D web viewer (3d-force-graph + lit-html, 2.9MB bundle)
- 6 MCP tools (find_symbol/callers/callees/get_subgraph/search_text/get_context_for_task)

### 검증 가능한 동작

```bash
make build
./bin/ckg build --src=internal/parse/golang/testdata/resolve --out=/tmp/ckg-smoke
./bin/ckg serve --graph=/tmp/ckg-smoke --port=8787 --open    # 3D viewer 브라우저 자동 오픈
./bin/ckg mcp --graph=/tmp/ckg-smoke                           # stdio MCP 서버 (Claude Code spawn 가능)
./bin/ckg export-static --graph=/tmp/ckg-smoke --out=/tmp/ckg-static  # 정적 호스팅 가능 디렉토리
```

---

## 2. 남은 작업 — Phase 9 (T32–T35) Eval Framework

**목표**: 4 baseline (α raw / β graph-dump / γ granular / δ smart 1-shot) × N task에 대해 token + 정답률 측정.

### T32 — Task YAML loader + 4 baseline 정의

- **파일**: `internal/eval/task.go` (Task struct + LoadTasks(glob) using yaml.v3) + `internal/eval/baseline.go` (Baseline enum + AllowedTools(b) + SystemPrompt(b))
- **테스트**: `internal/eval/task_test.go` (TestLoadTasks)
- **새 deps**: `gopkg.in/yaml.v3`
- **핵심**: AllowedTools maps baseline → MCP tool name set:
  - α (alpha): nil (no tools)
  - β (beta): `[get_subgraph]`
  - γ (gamma): `[find_symbol, find_callers, find_callees, get_subgraph, search_text]`
  - δ (delta): `[get_context_for_task]`
- **Plan**: `docs/plan-ckg-v0-prototype.md` Task 32

### T33 — LLM client (Anthropic) + token tracking

- **파일**: `internal/eval/llm.go` + `internal/eval/llm_test.go`
- **새 deps**: `github.com/anthropics/anthropic-sdk-go`
- **핵심**: `LLMClient.Complete(ctx, system, user, tools)` returns `LLMResult{OutputText, InputTokens, OutputTokens, CacheReadTokens, CacheCreateTokens}`
- **환경**: `ANTHROPIC_API_KEY` 환경변수 필요
- **테스트**: API key 없을 때 에러 반환 확인 (실제 호출 없이)
- **Plan**: Task 33

### T34 — Scoring (precision/recall + rubric)

- **파일**: `internal/eval/score.go` + `internal/eval/score_test.go`
- **핵심**: `PrecisionRecall(got, want []string) (p, r float64)` + `RubricCheck(output, rubric) (hits, total)` (60% word-overlap heuristic)
- **테스트**: 표준 P/R 케이스 + rubric word-overlap 케이스
- **Plan**: Task 34

### T35 — `ckg eval` runner + report (CP-6)

- **파일**: `internal/eval/{runner,report}.go` + `cmd/ckg/eval.go` (실 wiring)
- **핵심 deviations** (T35 반드시 주의):
  - V0 simplification: γ baseline은 *실제 multi-turn tool loop 안 함*. Plan은 in-process 단축 호출로 simulate (single-turn). 진짜 multi-turn은 V1+.
  - δ baseline은 in-process로 `mcp.buildContext` 호출 (avoid spawning ckg mcp).
- **출력**: `eval/results/results.csv` + `eval/results/report.md` (auto-generated)
- **report.md 핵심 내용**: H1 (δ vs α token 절감 %), H2 (δ vs α score regression check)
- **`ckg eval` flags**: `--tasks=GLOB --baselines=alpha,beta,gamma,delta --graph=DIR --out=DIR --llm=claude-sonnet-4-6`
- **Plan**: Task 35

---

## 3. 남은 작업 — Phase 10 (T36–T38) Corpus + CI + Docs

### T36 — Synthetic corpus + starter eval YAMLs

- **파일**: `testdata/synthetic/{go-backend,ts-frontend,sol-contract}/...` + `eval/tasks/synthetic-T01-find-callers.yaml` + `synthetic-T02-rubric-deposit.yaml`
- **합성 corpus 규모**: Go ~500 LOC (3 packages: `api/`, `service/`, `domain/`) + TS ~300 LOC + Sol ~150 LOC. Cross-lang link: `Vault.deposit` chain.
- **2개 task spec** (precision_recall + rubric 두 종류 cover)
- **Plan**: Task 36

### T37 — CI workflow

- **파일**: `.github/workflows/ci.yml`
- **핵심**: matrix `[ubuntu, macos, windows] × [amd64, arm64]` (excluding ubuntu-arm64 + windows-arm64)
- **단계**: `go vet` → `go test -race` → npm install + esbuild + viewer copy → `go build ./cmd/ckg` → smoke job (build + serve + Playwright)
- **Go version**: `1.25` (T3 fix에서 1.22→1.25 bump 완료, T22 fix에서 plan도 1.25로 정렬됨)
- **CGO**: 필요 (tree-sitter parsers). CI runner의 C toolchain 필요 — 모든 GitHub-hosted runners에 기본 포함됨.
- **Plan**: Task 37

### T38 — Docs + V0 DoD verification (CP-7)

- **파일**: `docs/SCHEMA.md` (29 nodes / 22 edges 표) + `docs/ARCHITECTURE.md` (5 subcommand + pipeline overview) + `docs/EVAL.md` (baseline + scoring 사용법) + `README.md` (Quick Start 5분 안)
- **마지막 단계**: V0 DoD 체크리스트 수동 검증 (build synthetic + real / serve / mcp / eval / coverage ≥ 80%)
- **Plan**: Task 38

---

## 4. 운영 패턴 — 다음 세션이 알아야 할 것

### 4.1 Subagent-driven 패턴 사용

이번 세션은 `/superpowers:subagent-driven-development` 스킬로 진행:
1. 각 task별로 fresh `general-purpose` subagent 디스패치
2. Subagent가 plan 읽고 TDD로 구현 + commit
3. `superpowers:code-reviewer` subagent로 review
4. Critical/Important issue 있으면 fix subagent 디스패치 후 다음 task

작업 컨벤션 + 메시지 포맷이 일관되어 있어 다음 세션도 그대로 따라 가는 게 효율적.

### 4.2 발견된 plan 결함 패턴

Plan은 ~9000 lines라 verbatim copy 시 작은 결함이 종종 발견됨. 지금까지 발견 + 수정한 것:

| Task | 결함 | 수정 방식 |
|---|---|---|
| T3 | Plan: Go 1.22 / 실제: modernc.org/sqlite v1.49.1 requires Go 1.25 | Plan + go.mod 모두 1.25로 정렬 (`38c31d0` fix) |
| T7 | `lookupStartByte` O(N²) on field/method emission | parent ID를 caller가 직접 전달 (`a225de7` fix) |
| T9 | `Resolve` doc 주장 vs 실제 동작 모순 (AMBIGUOUS edge 감안 vs drop) | Doc + LoadAndResolve 둘 다 정정 (`f802608` fix) |
| T12 | Plan formula `strings.Count(qname, "/")` ≠ test 예상 levels | Test golden을 formula에 맞춰 정정 + 명시적 doc note |
| T18 | `queryExport` 정의됐지만 wire 안 됨 (runQuery `@name` capture filter) | Drop with TODO comment (`cb45666` fix) |
| T19 | `binding.go` v1.2.13 vs 실제 v1.2.11 inconsistency + dead `queryStateVar` | Sync version + drop dead const (`3f473fc` fix) |
| T22 | `three@0.158`이 `3d-force-graph@1.80`의 peer 요구(>=0.179)와 불일치 | three 0.180으로 bump + lockfile commit + esbuild CVE patch (`89e2343` fix) |
| T25 | `SELECT n.*`이 nullable column에서 scanNodes 깨뜨림 | `SELECT nodeColumns` (T28에서 동일 패턴 재발견) |
| T28 | mcp-go v0.49.0의 API: `mcp.Default(x)` 없음, typed variants만 | `mcp.DefaultBool/Number/String` 사용 |
| T29 | Plan의 LSP-style `Content-Length` framing ≠ mcp-go의 NDJSON | Test에서 NDJSON 적용 + plan에 note 추가 (`482c57d` fix) |

**다음 세션 행동 가이드**: subagent에게 plan을 verbatim 따르라고 하되, **테스트 실패 또는 컴파일 에러 시 plan vs 실제 라이브러리 API 차이 의심**. Subagent가 deviation을 자체 판단하면 review 단계에서 검증.

### 4.3 gopls 캐시 지연 false positive

매 task마다 새 패키지 추가 시 gopls가 IDE 진단으로 `BrokenImport` / `UndeclaredName` / `MissingFieldOrMethod` 경고를 표시함 (몇 분간). **실제 `go build ./...` / `go test ./...` 는 그린**. 매번 실 build/test로 검증한 후 false positive 무시.

발생 패턴:
- 새 패키지 추가 후 첫 5~10분
- 한 패키지에 새 메서드 추가 후 다른 패키지가 그것을 호출할 때
- 파일 삭제 후 그 파일의 심볼 참조 (gopls가 stale view 보유)

### 4.4 Subagent 호출 비용 관리

각 task는 평균 2~3 subagent dispatch (impl + review + 가끔 fix). 한 dispatch가 ~50K~100K tokens.

**남은 7 tasks × ~2.5 dispatch = ~18 subagent calls** 예상.

T35 (eval runner)와 T37 (CI workflow)이 가장 무거울 가능성 — multi-file integration. Pre-emptive 추천:
- T35는 plan 그대로 따르되 LLM 호출은 mock으로 대체 가능 (실제 Anthropic API 키는 사용자가 별도 설정).
- T37은 plan에 verbatim YAML 있으니 거의 paste-and-tune.

### 4.5 Commit 컨벤션 (HARD CONSTRAINTS)

- Conventional Commits, English subject
- **NO `Co-Authored-By` / `Generated with [Claude Code]` attribution** (사용자 글로벌 룰)
- Subject ≤ 70 chars 권장
- Body 자유롭지만 verbose 지양

각 task는 plan에 verbatim commit 메시지가 있음 — 그대로 사용.

---

## 5. 환경 / 의존성 현재 상태

### Go module (`go.mod`)

```
module github.com/0xmhha/code-knowledge-graph
go 1.25.5

require (
    github.com/anthropics/anthropic-sdk-go  // T33에서 추가 예정
    github.com/mark3labs/mcp-go v0.49.0
    github.com/smacker/go-tree-sitter v0.0.0-20240827094217-dd81d9e9be82
    github.com/spf13/cobra v1.10.2
    golang.org/x/tools v0.44.0
    gopkg.in/yaml.v3                       // T32에서 추가 예정
    modernc.org/sqlite v1.49.1
)
```

### Web (`web/viewer/package.json`)

```json
{
  "dependencies": {
    "3d-force-graph": "^1.73.0",
    "lit-html": "^3.1.0",
    "three": "^0.180.0"           // ← 0.158 → 0.180 (T22 fix)
  },
  "devDependencies": {
    "esbuild": "^0.25.0",          // ← 0.21 → 0.25 (CVE patch)
    "@playwright/test": "^1.45.0"
  }
}
```

### Vendored

- `internal/parse/solidity/binding/` — JoranHonig/tree-sitter-solidity v1.2.11 (MIT, ABI 14 — 최신 v1.2.13는 ABI 15라서 smacker 호환 안 됨)

### Build artifacts (gitignored)

- `bin/ckg` — `make build` or `go build -o bin/ckg ./cmd/ckg`
- `web/viewer/dist/` — `cd web/viewer && node esbuild.config.js`
- `web/viewer/node_modules/` — `npm install`
- `internal/server/web_assets/` — committed (embed.FS source), regenerated by `make viewer`

---

## 6. 검증 명령 (Phase 경계마다)

각 phase 완료 후 다음을 모두 실행해서 그린이어야 함:

```bash
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph

# 1. Go side
go vet ./...
go test ./...
go test -tags e2e ./...      # e2e (T17, T29)도 그린

# 2. Web side (해당 phase에 viewer 변경 있으면)
cd web/viewer && node esbuild.config.js && cd ../..
make viewer                  # 자동으로 internal/server/web_assets/ 갱신

# 3. Binary smoke
go build -o bin/ckg ./cmd/ckg
./bin/ckg --help             # 5 subcommands listed

# 4. End-to-end smoke (T31 이후)
./bin/ckg build --src=internal/parse/golang/testdata/resolve --out=/tmp/ckg-smoke
ls /tmp/ckg-smoke/           # graph.db + manifest.json
./bin/ckg export-static --graph=/tmp/ckg-smoke --out=/tmp/ckg-static
ls /tmp/ckg-static/          # index.html + assets/ + nodes/ + edges/ + ...

# 5. Working tree clean (untracked jira docs는 OK)
git status --short
```

---

## 7. V0 Definition of Done 체크리스트 (T38에서 검증)

`docs/spec-ckg-v0-prototype.md` §14 참조:

- [ ] `ckg build --src=testdata/synthetic --out=/tmp/ckg-synth` 무에러 종료 *(T36 후)*
- [ ] `ckg build --src=$STABLENET_PATH --out=/tmp/ckg-real` 무에러 종료, < 10분 *(STABLENET_PATH = `/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest`)*
- [ ] `ckg serve --graph=/tmp/ckg-real`로 viewer 정상 로드, L0~L4 fold/unfold 동작
- [ ] `ckg mcp --graph=/tmp/ckg-real`이 Claude Code에서 spawn 후 6개 도구 정상 응답
- [ ] `ckg eval --baselines=alpha,beta,gamma,delta` 합성 + 실 corpus 모두 완주 *(T35 후)*
- [ ] Eval 보고서 자동 생성: H1/H2/H3 결과 명시
- [ ] CI 통과: 3 OS × 2 arch × test/build/smoke *(T37 후)*
- [ ] 모든 unit test 80%+ coverage
- [ ] `docs/SCHEMA.md`, `docs/ARCHITECTURE.md`, `docs/EVAL.md` 작성 *(T38)*
- [ ] README.md에 Quick Start 5분 안 *(T38)*

---

## 8. 다음 세션 시작 권고 시퀀스

```
1. 새 Claude Code 세션 시작 (cwd: 위 working dir)

2. 첫 user message:
   "docs/NEXT-SESSION.md 읽고 T32부터 subagent-driven으로 진행해.
    Commit은 영어 + conventional + co-author 제거."

3. 모델이 본 문서 read → 지난 진행 컨텍스트 회복 → T32 dispatch
   (실패 시 docs/plan-ckg-v0-prototype.md Task 32 직접 read)

4. Task by task:
   - dispatch impl subagent (general-purpose, task-specific prompt)
   - dispatch code-reviewer subagent (superpowers:code-reviewer)
   - 필요시 fix subagent
   - 다음 task

5. Phase 9 종료 시 CP-6 도달 → Phase 10 시작
6. T38 도달 시 V0 DoD 수동 검증
```

### Subagent prompt 템플릿

지난 세션에서 사용한 패턴:

```
Implement **Task NN (...)** from /Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph/docs/plan-ckg-v0-prototype.md.

⚠️ [task-specific cautions if applicable]

## Working directory
/Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph (HEAD `...`).

## Commit conventions
- Commit verbatim from plan Step N: `...`
- No Co-Authored-By / Claude attribution.

## Files
[list]

## Steps
[TDD steps from plan]

## Key technical points / Watch-outs
[plan defects to anticipate, library API gotchas]

## What to NOT do
[scope guards]

## Report (under N words)
[what to verify in subagent's report]
```

Code reviewer prompt 템플릿:

```
Review **Task NN (...)** in the CKG repo.

## What was implemented
[from impl subagent's report]

## Plan / requirements
docs/plan-ckg-v0-prototype.md `## Task NN: ...`. Plan provides verbatim source for ...

## Diff to review
- BASE: `<prev HEAD>`
- HEAD: `<new HEAD>`

## Review focus
[specific items per task]

## Report
- Strengths
- Issues — Critical / Important / Minor (file:line + suggested fix)
- Assessment — READY / MINOR FIXES NEEDED / BLOCKING

End with the Fact-based Answer structure.
```

---

## 9. References

### Spec / Plan / Study

- `docs/spec-ckg-v0-prototype.md` — V0 design (1,442 lines)
- `docs/plan-ckg-v0-prototype.md` — implementation plan with verbatim code (8,983 lines)
- `docs/STUDY-GUIDE.md` — Leiden / MCP / Tree-sitter / 3D layout 학습 자료 (321 lines)
- `docs/NEXT-SESSION.md` — 본 문서

### 외부 리소스

- mcp-go: https://github.com/mark3labs/mcp-go (v0.49.0 — NDJSON stdio framing)
- 3d-force-graph: https://github.com/vasturiano/3d-force-graph
- tree-sitter-solidity: https://github.com/JoranHonig/tree-sitter-solidity (MIT, vendored v1.2.11)
- Anthropic SDK Go: https://github.com/anthropics/anthropic-sdk-go (T33 추가 예정)
- Leiden algorithm reference (Java): https://github.com/CWTSLeiden/networkanalysis

### 기존 stablenet-ai-agent 프로젝트와의 관계

CKG는 stablenet-ai-agent의 CKS(Code Knowledge System) 구현 검증용 standalone prototype:
- 검증 대상 corpus: `/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest` (1,300 files / 371K LOC)
- CKS 통합은 V1+ 영역 (Vector DB, hybrid retrieval 등)
- 가설 검증 (token 효율 + 정확도) 결과로 CKS plan 영향 가능

---

**End of handoff. T32부터 시작하면 됨. 행운.**
