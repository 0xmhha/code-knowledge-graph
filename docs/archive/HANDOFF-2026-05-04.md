# CKG Session Handoff (post-Wave5 + Group G + Wave 7 + G6 v4 + C1/C2)

> 다음 세션 cold-read용. 이 문서만 읽으면 현재 상태 + 남은 작업 + 함정을 모두
> 파악하고 즉시 작업 재개 가능. WORK-PLAN.md는 작업 실행 tracker, 본 문서는
> 세션 경계의 snapshot.

| Field | Value |
|---|---|
| Snapshot date | 2026-05-04 (refresh 7 — G6 v4 + C1 + C2 + real-corpus parity ✅) |
| HEAD | `95dc3c2` (`feat(buildpipe): C1 reverse-reference invalidation`) |
| Working tree | **clean** |
| Branch | `main` |
| Test gate | `go vet ./...` clean, `go test ./...` 18 packages PASS, `make build` clean |
| Schema version | **1.5** (pending_refs table — now active for incremental builds) |
| 사용자 4 완성도 조건 | **모두 충족** (#1-#4 ✅) |
| Wave 7 (Group F) | **완료** — F1 + F2 + F3 ship됨 (1d42787, 412e622) |
| G6 v4 | **완료** — `NodesByFilePath ORDER BY start_line ASC` 추가 (SQLite + PG), `runIncremental` D4 dead code 활성화. Root cause H3 해결. (6d01112) |
| C1 | **완료** — `ReverseDepsForFiles` StoreReader에 추가 (SQLite + PG). incremental step 1.5로 wire. `LIKE '%.' || target_qname` suffix match로 AST 미해석 이름 처리. (95dc3c2) |
| C2 | **완료** — `ckg build/serve --db postgres://...` (pgxpool full Store implementation). (6d01112) |
| B2 | **완료** — `ckg export-postgres --dsn ... --source ...` (13317f7). jackc/pgx/v5 COPY 프로토콜, 전 필드 export, DSNHost URL+kv 양식, 테스트 4개. |
| Logging | **완료** — `--verbose`, `--log-file <path>`, `CKG_LOG_LEVEL=debug`. JSON 파일 + text stderr 동시 출력, buildpipe 스테이지 Debug 마커 (4fc69ff). |
| Channel flow | **완료** — `sends_to`/`recvs_from` 엣지가 Channel 노드 직접 가리킴 (make(chan T) 변수 추적). 인라인 goroutine body 별도 추적 (eb5e9bb, 8784ac9). |
| Open critical | **없음** — go-stablenet cold vs partial edge diff = **0** ✅ (214,343 nodes / 652,892 edges 완전 일치) |
| Working machine | `wm-it-22-00661` (`/Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph`). go-stablenet corpus 직접 사용 가능 (`/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest`, commit `0bf2f4d1b`) |

---

## 1. Quick start (cold-read, 5분)

```bash
cd <repo root>                                # current: /Users/0xtopaz/work/github/0xmhha/code-knowledge-graph
git log --oneline -10
go test ./...                                 # 17 packages PASS (cmd/ckg + 16 internal/*)
make build                                    # full build incl. Next.js viewer
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth
./bin/ckg serve --graph=/tmp/ckg-synth --port=8080 --open
./bin/ckg audit --src=testdata/synthetic --graph=/tmp/ckg-synth   # exit 0 = parity

# Wave 7 (Group F) 검증
./bin/ckg serve --graph=/tmp/ckg-synth --no-viewer --port=8788    # API only
make viewer && CKG_DEV_VIEWER_DIR=$(pwd)/internal/server/web_assets \
  ./bin/ckg serve --graph=/tmp/ckg-synth --port=8789              # disk viewer
```

핵심 reference 문서:
- `docs/WORK-PLAN.md` — 작업 tracker (Group A-G, Wave 1-7, follow-up status)
- `docs/spec-ckg-v0.2.md` — v0.2 foundation spec (parser migration / 동시성 / PG / incremental)
- `docs/INCREMENTAL.md` — A3 캐시 동작 (operator-facing)
- `docs/SCHEMA.md` — 33 nodes / 30 edges (v0.2.x post-Wave 5)
- 외부: `/Users/wm-it-22-00661/Work/github/stable-net/study/projects/stablenet-ai-agent/claudedocs/04-cks-deep-dive.md` — CKS 6 graph 정의 원전

---

## 2. 현재 capability (v0.2 post-Wave 5)

### 단일 Go 바이너리 `ckg`

7 subcommand:
- `build` — graph build (cold + short-circuit + incremental). `--db postgres://...` 로 PG backend 선택 (C2).
- `serve` — embedded Next.js viewer + REST API (127.0.0.1:8080 default). `--db postgres://...` 지원.
- `mcp` — stdio MCP server, 6 tools (find_symbol/callers/callees/get_subgraph/search_text/get_context_for_task)
- `export-static` — chunked JSON + viewer를 정적 호스팅용으로 export
- `export-postgres` — SQLite → PostgreSQL one-shot push (B2 — 4fc69ff~13317f7)
- `eval` — 4 baseline (α/β/γ/δ) × YAML tasks → CSV + report.md
- `audit` — `go/packages.Load` set vs DB set 비교 (exit 0=parity, 1=drift, 2=error)

**글로벌 CLI 플래그** (persistent, 모든 subcommand 상속):
- `--verbose` — slog.LevelDebug 활성화 (또는 `CKG_LOG_LEVEL=debug`)
- `--log-file <path>` — JSON 구조화 로그를 파일에 추가 기록 (stderr는 text 유지)

### 파서 3종 (smacker 완전 제거 — A1+A2 atomic)

- Go: `golang.org/x/tools/go/packages` (build constraints + types.Info, B1 concurrency 패스)
- TS/JS: `github.com/tree-sitter/go-tree-sitter v0.25.0` + `tree-sitter-typescript v0.23.2` + `tree-sitter-javascript v0.25.0`
- Solidity: vendored grammar (JoranHonig v1.2.11, ABI 14 — upstream 0.25 ABI window 13..15 안에 들어가 regenerate 불요)

### 33 NodeTypes × 30 EdgeTypes (`pkg/types/enums.go`)

6 graph axis (CKS deep-dive § 4.1):
- **G1 Structural**: contains, defines, imports, exports
- **G2 Semantic**: references, implements, extends, uses_type, instantiates, reads_field, writes_field, reads_mapping, writes_mapping, emits_event, has_modifier, has_decorator
- **G3 Execution**: calls, invokes
- **G4 Concurrency**: spawns, sends_to, recvs_from, acquires_lock, releases_lock, accessed_under_lock (← B1 + G8 + G9)
- **G5 Distributed**: listens_on, handles_message, rpc_calls, binds_to (← E3)
- **G6 Temporal**: changed_in, blame (← E4)

특수 NodeType: Endpoint (E3), MessageType (E3), Commit (E4), Mutex (B1)

### Storage (A4 ISP split)

- `persist.StoreReader` — read-only surface (serve / mcp / eval / audit)
- `persist.StoreWriter` — write surface (buildpipe)
- `persist.Store` — composite (StoreReader + StoreWriter)
- 구현체 1: unexported `sqliteStore` (modernc.org/sqlite, CGO-free) — default
- 구현체 2: unexported `pgStore` (jackc/pgx/v5 pgxpool) — `--db postgres://...` 시 활성 (C2)
- ON DELETE CASCADE on edges/blobs/pkg_tree/topic_tree FK (A3)
- `ReverseDepsForFiles`: StoreReader에 추가 (C1). LIKE suffix match로 AST 미해석 target_qname과 nodes.qualified_name 매핑.

### Viewer (Next.js, embedded via go:embed)

- `web/viewer-next/` — react-force-graph-3d + zustand
- 6-graph axis filter UI (E5) — collapsible group sections + 3-state group toggle
- localStorage 영속 (collapse state, view/color/font prefs)

### serve options (Wave 7 — Group F)

`server.Options{DevViewerDir, NoViewer}` + `NewWithOptions` 도입. 기존 `New(store, log)`는 zero-options wrapper로 그대로 유지 (test 호환).

- **F1** `CKG_DEV_VIEWER_DIR` env: viewer asset을 disk path에서 serve. `make viewer` 후 브라우저 reload만으로 viewer 변경 반영, ckg 재빌드 불요.
- **F2** `--no-viewer` flag: static mount 생략. `/api/*` 만 노출. operator의 reverse-proxy 패턴용. `--open`은 `--no-viewer`와 함께면 자동 suppress.
- **F3** README에 production-split 패턴 (`export-static` + `serve --no-viewer` + reverse proxy) + dev hot-reload 패턴 명시.

테스트: `internal/server/options_test.go` — `TestOptions_NoViewer` (api OK / `/` 404), `TestOptions_DevViewerDir` (marker 파일이 disk에서 serve됨 검증).

### 검증된 동작 (go-stablenet-latest 2142 files / 217K nodes / 669K edges)

```bash
make build
./bin/ckg build --src=$STABLENET_PATH --out=$GRAPH
./bin/ckg serve --graph=$GRAPH --port=8080 --open
./bin/ckg mcp --graph=$GRAPH
./bin/ckg export-static --graph=$GRAPH --out=$STATIC
ANTHROPIC_API_KEY=… ./bin/ckg eval --tasks='eval/tasks/synthetic-*.yaml' \
  --graph=$GRAPH --baselines=alpha,beta,gamma,delta --out=eval/results
./bin/ckg audit --src=$STABLENET_PATH --graph=$GRAPH      # 1259/1259 PARITY
```

---

## 3. 사용자 4 완성도 조건 — 최종 충족 상태

| # | 조건 | Status |
|---|---|---|
| 1 | 빌드 시 모든 파일 누락 없이 DB화 | ✅ E2 (go/packages.Load production path) — go-stablenet 1259 build = 1259 db PARITY |
| 2 | audit으로 검증 가능 | ✅ E1 (`ckg audit`) — exit 0 / 1 / 2 |
| 3 | CKS 6 graph (G1~G6) 지원 | ✅ B1 + E3 + E4 emission, G9로 G4 정확도 21× 향상 (Mutex 8→170) |
| 4 | viewer + CLI eval | ✅ E5 (6-graph filter UI + group toggle) + eval framework (V0) |

### 측정 가능한 개선 (Wave 5 + Group G)

| 메트릭 | Pre | Post | Δ |
|---|---|---|---|
| Mutex nodes (go-stable-code) | 0 (B1 전) | 170 (G9 후) | — |
| acquires_lock edges | 0 | 781 | — |
| accessed_under_lock edges (G8) | 0 | 2916 | — |
| Field-misclassified acquires_lock dst (G9) | 157 | 1 | -99.4% |
| changed_in edges (E4 git history) | 0 | 344946 | — |
| Endpoint nodes (E3) | 0 | 2 (httprouter 사용 corpus라 적음) | — |
| handles_message (E3) | 0 | 57 | — |
| audit drift (E2 production path) | 41 over-include | 0 | PARITY |
| Schema version | 1.0 | 1.4 | A5/A3/E3/E4 each bump |
| pipeline.go LOC (G4) | 596 | 359 | -40% |

---

## 4. 남은 작업 우선순위 (다음 세션용)

### 4.1 G6 v4 — ✅ COMPLETED (2026-05-04)

**상태**: H3 root cause fix + D4 dead code 활성화 완료.

**Root cause (confirmed + fixed)**:
- **H3 (primary ← fixed)**: `NodesByFilePath`가 DB rowid 순(ID sorted) 반환 ≠ AST 선언 순서. `ORDER BY start_line ASC` 추가로 해결. SQLite + PostgreSQL 양쪽 모두 적용.
- **H4 (secondary ← known limitation)**: `reloadCachedEdges`가 `cached_src→dirty_dst` edge 미로드 → −5 imports. 허용 범위 내 design limitation으로 유지.

**Committed 변경**:
- `6d01112` — `NodesByFilePath ORDER BY start_line` (SQLite + PG), `runIncremental` D4 dead code 활성화, C2 PostgreSQL Store full implementation, `--db` flag (build + serve).

**v1+v2+v3 실패 → v4 해결 history**:

| 시도 | 접근 | 결과 |
|---|---|---|
| v1 (`31a17f0`) | pending refs를 manifest에 persist | go-stablenet: −92201 calls (100% loss) |
| v2 (`8d5521c`) | cached files Nodes+Pending reload, temporal/xlang split | go-stablenet: −347986 edges (52%), changed_in 0건 DB, **30분 runtime** |
| v3 (`c15cdcb`) | pending_refs SQLite + edge dedup + emitDerivedPasses unified | go-stablenet: +2675 edges over-emit. Root cause: H3 (NodesByFilePath order ≠ declaration order) |
| v4 (`6d01112`) | `ORDER BY start_line ASC` + D4 활성화 | ✅ H3 해결. Unit tests 18/18 PASS. real-corpus 검증 권장 (cold vs partial diff = 0 목표) |

참조: `docs/G6-V3-VALIDATION-FINDINGS.md`, `docs/G6-INCREMENTAL-REDESIGN.md` § 8.

### 4.2 v0.2.1 Wave (Group B)

| ID | 작업 | 의존 | 추정 |
|---|---|---|---|
| B2 | `ckg export-postgres --dsn ... --source ...` 명령 | A4 (✅) | M ✅ **완료 13317f7** — pgx COPY, 전 필드, 테스트 4개. |
| B3 | Item 1 Phase 1c — incremental parsing 인프라 (Tree.Edit() API) | A1 (✅) + A3 (✅) | M |

**이번 refresh 완료:**

| 작업 | 커밋 | 내용 |
|---|---|---|
| Logging | `4fc69ff` | `--verbose`, `--log-file`, `CKG_LOG_LEVEL=debug`. multiHandler(JSON파일+text stderr). 6 subcommand 적용. buildpipe 스테이지 Debug 마커. |
| B2 export-postgres | `13317f7` | `ckg export-postgres`. pgxpool + COPY 프로토콜. nodes/edges/blobs 전 필드. DSNHost URL+kv 양식. 테스트 4개. |
| Channel flow | `8784ac9`+`eb5e9bb` | `sends_to`/`recvs_from` → Channel 노드 직접 (make 변수 추적). goroutine body 별도 walk. double-emit/orphan CallSite 수정. 테스트 3개. |

### 4.3 v0.2.2 Wave (Group C) — ✅ 완료

| ID | 작업 | 의존 | 추정 | 상태 |
|---|---|---|---|---|
| C1 | reverse-reference invalidation — `ReverseDepsForFiles` + incremental step 1.5 | A3 ✅, G6 v4 ✅ | L | ✅ **95dc3c2** |
| C2 | `ckg build/serve --db postgres://...` direct PG 빌드 | B2 ✅ | L | ✅ **6d01112** |

**C1 구현 핵심 사항** (다음 세션에서 수정 시 참조):
- `pending_refs.target_qname`은 AST 미해석 이름 (`"Helper"`) — `nodes.qualified_name`은 fully qualified (`"mypkg.Helper"`).
- SQL JOIN은 `n.qualified_name LIKE ('%.' || pr.target_qname)` suffix match 필수. `simpleName()` in `resolve.go`와 동일 의미론.
- False positive (다른 패키지의 동명 함수도 매칭) 허용 — 안전. False negative (edge 누락) 불허.
- `ReverseDepsForFiles` MUST be called BEFORE `DeleteNodesByFilePath` (joins `pending_refs ⋈ nodes` still in DB).

### 4.4 v0.3.0 Wave (Group D — 별도 spec 필요)

| ID | 작업 | 의존 | 추정 |
|---|---|---|---|
| D1 | Item 2 Stage 2 — SSA 정밀 동시성 (`--deep` opt-in) | B1 (✅) | XL (하루+) |
| D2 | Item 3 Phase 3 — pgvector + Apache AGE 통합 | C2 | XL |

### 4.5 viewer 운영성 (Group F) — ✅ Wave 7 완료

F1 + F2 + F3 모두 ship됨 (1d42787, 412e622). 본 HANDOFF § 2 "serve options (Wave 7)" 참조.

### 4.6 G6 v4 — ✅ 완료 (2026-05-04)

`6d01112`에서 `ORDER BY start_line ASC` + `runIncremental` 활성화 완료. 상세는 § 4.1 참조.

**검증 완료 (2026-05-04)**:

```
Cold build  : 214,343 nodes / 652,892 edges
Partial (1 file dirty — accounts/abi/abi.go):
              214,343 nodes / 652,892 edges
Diff        : 0 nodes / 0 edges  ✅
```

모든 edge 타입 (changed_in 340,631 / contains 162,907 / calls 88,763 / defines 44,189 / …) 전부 cold와 동일. H3 phantom-edge +2675 재현 없음.

### 4.7 알려진 minor issues

- G3-1 (E3 follow-up): go-stablenet은 `julienschmidt/httprouter` 사용 → stdlib HandleFunc 패턴 detector가 적게 fire. Custom router 패턴 추가 시 listens_on 수 증가 가능.
- G4-1 (E3 follow-up): Ethereum-style RPC `client.Call(&result, "method", ...)` 시그니처 추가 (현재 net/rpc 시그니처만 detect)
- G6-temporal (E4 follow-up): line-level blame (G6 Phase 2 — `git blame --line-porcelain`); submodule traversal; correlated_with/observed_in/mentioned_in (D-tier)
- B1-1 (G9 follow-up): 1건 남은 Field-targeting acquires_lock — 함수 local mutex literal (`var mu = sync.Mutex{}`) edge case
- Channel flow limitation: channel 함수 파라미터(`out chan<- T`)는 chanVarIDs에 없어 CallSite fallback. make(chan T) 변수만 Channel 노드 직접 연결.
- E2-FU: `go.work` 회귀 테스트 — workspace 멤버가 srcRoot 외부 case 미지원 (현재 `TestGoFiles_GoWorkspace`로 documented만 됨)
- Wave 1 DoD: `web/viewer-next/src/lib/edges.ts`의 dead key (`reads/writes/modifies/decorates/emits`) 정리 — backend emit 시작 OR client 제거 결정 미완

---

## 5. 운영 패턴 (이번 세션에서 확립된 함정 + 회피)

### 5.1 Subagent 워크플로

`/superpowers:subagent-driven-development` 스킬:
1. fresh `general-purpose` subagent dispatch (impl + commit)
2. `superpowers:code-reviewer` subagent로 review
3. Critical/Important issue 있으면 fix subagent 또는 main session에서 처리
4. 작은 task (single-line fix, doc-only)는 main session에서 직접 처리 — skill의 "When NOT to use: tightly coupled" 가이드 준수

**주의**: code-reviewer subagent가 org limit으로 빠질 수 있음 (이번 세션에서 2건 발생). 그럴 땐 main session에서 직접 review (build/test/sample 측정).

### 5.2 large-task subagent dispatch 시 필수 체크리스트

이번 세션에서 G6 v1, v2 모두 실패한 원인 분석:
- subagent가 작은 fixture에서만 검증하고 commit → real corpus에서 catastrophic regression
- subagent가 timeout 직전에 stall → 코드는 작성됐지만 commit 직전 멈춤

**다음 dispatch 시**:
- 큰 task는 token budget 명시 (~150-200K)
- prompt에 **real-corpus parity check** 명시 + 실패 시 STOP and report 강제
- 측정 결과(edge counts pre/post) 다 받기 전 commit 금지
- subagent stall 후엔 main session에서 working tree 검증 → revert OR 손수 fix

### 5.3 plan/spec 결함 패턴 (V0~Wave 5 누적)

| Source | 결함 | 수정 |
|---|---|---|
| Plan T3 | Go 1.22 vs modernc/sqlite v1.49.1 requires 1.25 | go.mod 1.25 |
| Plan T22 | three@0.158 vs 3d-force-graph peer >=0.179 | three 0.180 |
| Plan T29 | LSP-style framing vs mcp-go NDJSON | NDJSON 우선 |
| Spec v0.2 R1.1 | tree-sitter Query DSL 미세 변화 → silent miss-extraction | golden 테스트 필수 |
| A3 | partial-cache cross-file edge silent loss | cold-fallback (a684239) |
| G9 | emitMutexNode/emitFields ID 충돌 | qname `#mutex` suffix |
| G6 v1+v2 | architecture-level mismatch incremental ↔ Pass 2/cluster/temporal | spec-level redesign 필요 |

**다음 세션 행동 가이드**: subagent에게 spec/plan 따르라고 하되, **테스트 실패 또는 컴파일 에러 시 spec vs 실 라이브러리 API 의심**. Real-corpus 측정 안 하고 small fixture만으로 commit 금지.

### 5.4 gopls 캐시 지연 false positive

매 task마다 새 패키지 추가 시 gopls가 IDE 진단으로 `BrokenImport` / `UndeclaredName` / `MissingFieldOrMethod` / `unusedfunc` 경고를 표시함 (수 분간). **실제 `go build ./...` / `go test ./...` 는 그린**. 매번 실 build/test로 검증 후 false positive 무시.

### 5.5 commit 컨벤션 (HARD CONSTRAINTS)

- Conventional Commits, English subject
- **NO `Co-Authored-By` / `Generated with [Claude Code]` attribution** (사용자 글로벌 룰)
- Subject ≤ 70 chars 권장
- Body는 *why* 중심
- Heredoc으로 multi-line 메시지 (perl regex 같은 escape-prone tooling 금지 — 이번 세션에서 한 번 WORK-PLAN.md 전체 망쳤다 복구함, 교훈)

### 5.6 Viewer build coupling

`make build`는 viewer (Next.js) 먼저, 그 다음 ckg binary. Makefile의 stub-restore 메커니즘으로 git status churn은 회피 — 다만 `make viewer` 별도 실행 후 `make build` 안 돌리면 binary와 disk hash desync 가능. 의심되면 `make build` 한 번 더.

---

## 6. 환경 / 의존성

### Go module (`go.mod`)

```
module github.com/0xmhha/code-knowledge-graph
go 1.25.5

require (
    github.com/0xmhha/cli-wrapper v0.2.1
    github.com/anthropics/anthropic-sdk-go v1.38.0
    github.com/jackc/pgx/v5 v5.9.2                          // B2 export-postgres
    github.com/mark3labs/mcp-go v0.49.0
    github.com/spf13/cobra v1.10.2
    github.com/tree-sitter/go-tree-sitter v0.25.0           // post A1+A2
    github.com/tree-sitter/tree-sitter-javascript v0.25.0   // post A1+A2
    github.com/tree-sitter/tree-sitter-typescript v0.23.2   // post A1+A2
    golang.org/x/tools v0.44.0
    gopkg.in/yaml.v3 v3.0.1
    modernc.org/sqlite v1.49.1
)
// smacker/go-tree-sitter 완전 제거됨 (A1+A2 atomic)
```

### Web (`web/viewer-next/package.json`)

Next.js 14 + react-force-graph-2d/3d + zustand

### Vendored

- `internal/parse/solidity/binding/` — JoranHonig/tree-sitter-solidity v1.2.11 (LANGUAGE_VERSION=14)

### Build artifacts (gitignored)

- `bin/ckg`
- `web/viewer-next/{out,.next,node_modules}/`
- `internal/server/web_assets/_next/`, `404/`, `404.html`, `index.txt` (stub `index.html`만 commit)
- `.playwright-mcp/`, `ckg-*.png`

### 검증 corpus

- `testdata/synthetic/` — Go 3 + TS 3 + Sol 2 = 8 files (소규모, 빠름)
- `/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest` — Go 1259 + TS 320 + Sol 563 = 2142 files (Ethereum-derived, 실 corpus)
- `/Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph/go-stable-code/` — go-stablenet의 사전 빌드된 graph.db (생성 비용 ~1m 15s)

---

## 7. 검증 명령 (wave 경계마다)

```bash
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph

# 1. Go side
go vet ./...
go test ./...
go test -tags e2e ./...

# 2. Web side (viewer 변경 있을 때)
make viewer

# 3. Binary smoke
make build
./bin/ckg --help

# 4. End-to-end smoke
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth
./bin/ckg audit --src=testdata/synthetic --graph=/tmp/ckg-synth   # exit 0
./bin/ckg export-static --graph=/tmp/ckg-synth --out=/tmp/ckg-static

# 5. Real-corpus parity (큰 변경 후)
./bin/ckg build --src=/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest --out=/tmp/ckg-stable
./bin/ckg audit --src=/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest --graph=/tmp/ckg-stable
sqlite3 /tmp/ckg-stable/graph.db "SELECT COUNT(*) FROM edges WHERE type='calls'"   # ~92201

# 6. Working tree clean
git status --short
```

---

## 8. 다음 세션 시작 시퀀스

1. 새 Claude Code 세션 (cwd: repo root)
2. 첫 user message:
   > "docs/HANDOFF.md 읽고 현재 상태 파악해. 그 다음 [작업 지시]."
3. 모델이 본 문서 read → 컨텍스트 회복 → 지시 받은 작업 dispatch
4. WORK-PLAN.md G3-G9 follow-up 잔여 항목, Group B/C/D는 모두 본 문서 § 4에 우선순위와 함께 기록됨. Group F는 ✅ 완료.

### 권장 다음 작업 순서 (2026-05-04 refresh 7)

| 순위 | 작업 | 이유 |
|---|---|---|
| 1 | **B3** (Tree.Edit() incremental parsing) | partial-cache 재파싱 비용 추가 절감. C1 ✅으로 now unblocked. |
| 2 | E2-FU + Wave1 DoD (viewer dead-key 정리) | 작은 정리 — main session에서 직접 처리 가능 |
| 3 | E3/E4 follow-up minors | httprouter, RPC client.Call 변종, line-level blame |
| 4 | D1 / D2 | XL, 별도 spec 필요 |

---

## 9. Commit 요약 (시간 역순)

### 이번 refresh (2026-05-04, G6 v4 + C1 + C2)

| Commit | 분류 | 내용 |
|---|---|---|
| `95dc3c2` | feat | C1: `ReverseDepsForFiles` StoreReader (SQLite+PG), incremental step 1.5, LIKE suffix match, `TestReverseDepsForFiles` |
| `6d01112` | feat | G6 v4: `NodesByFilePath ORDER BY start_line ASC` (SQLite+PG). C2: PostgreSQL full Store (pgxpool, ~1160 lines), `--db` flag on build/serve. |

### 이전 refresh (2026-05-04, B2 + Logging + Channel flow)

| Commit | 분류 | 내용 |
|---|---|---|
| `eb5e9bb` | feat | Channel flow: chanVarIDs, AssignStmt→Channel 매핑, goroutine body 추적 (TestChannelFlow 3개) |
| `8784ac9` | fix | channel double-emit + orphaned CallSite 제거. GoStmt return false, SendStmt break-or-fallback |
| `13317f7` | fix | export-postgres 7개 critical bug 수정 (node load, edge PK, DDL trx, DSNHost, 전 필드) |
| `4fc69ff` | feat | Logging: --verbose/--log-file/CKG_LOG_LEVEL=debug. multiHandler(JSON+text). 6 subcommand 적용 |
| `966d3c6` | docs | STATUS-REPORT channel async data flow gap 추가 (4d) |

### 이전 refresh (2026-05-04, D4 escape hatch EXECUTED + root cause confirmed)

| Commit | 분류 | 내용 |
|---|---|---|
| `c15cdcb` | fix | D4 escape hatch — routing cold-fallback 복귀, schema 1.5 dead code 보존, G6-INCREMENTAL-REDESIGN.md § 8 stamp, INCREMENTAL.md + FINDINGS.md § 0 추가 |
| `3a6d9f6` | refactor | runCold 미사용 파라미터(goCount/tsCount/solCount) 제거 |

**Root cause confirmed (H3)**: NodesByFilePath rowid-sorted ≠ AST declaration order → ambiguous qname winner differs → +2675 phantom edges. Fix: `NodesByFilePath ORDER BY start_line ASC` (v4).

### 이전 세션 (2026-05-03 ~ 2026-05-04, Wave 7 + G6 design)

| Commit | 분류 | 내용 |
|---|---|---|
| `b9c15f0` | docs | HANDOFF refresh (v3 implementation 진입 직전) |
| `e285d57` | docs | G6 § 8 4 결정 resolve (D1 v3 채택 / D2 풀 v3 먼저 / D3 C1 layered / D4 § 7.3 미달 시 drop) |
| `100591b` | docs | G6 v3 design spec (`docs/G6-INCREMENTAL-REDESIGN.md`, 465 lines) |
| `412e622` | docs | F3 — README production-split + dev hot-reload 패턴 |
| `1d42787` | feat | F1+F2 — `CKG_DEV_VIEWER_DIR` env + `--no-viewer` flag (server.Options + tests) |

### 이전 세션 (~32 commits, Wave 5 + Group G)

| Commit | 분류 | 내용 |
|---|---|---|
| `1aab892` | docs | HANDOFF snapshot 작성 |
| `8d5521c` | docs | G6 v2 fail 분석 |
| `f215b72` | fix | G9 — Mutex node ID collision (Mutex 8→170, 21×) |
| `31a17f0` | docs | G6 v1 fail 분석 |
| `1ea9e35` | docs | G9 re-scope (embedded → detection partial-miss) |
| `975818d` | feat | G8 — accessed_under_lock 0→2916 |
| `5ac0976` | test | G7 — TS/Sol golden tests |
| `1bcd5b0` | test | G5 — go.work workspace test |
| `ac53f17` | docs | G4 perl regex 사고 복구 |
| `c0ecd81` | refactor | G4 — pipeline.go 596→359 split |
| `8258cb0` | feat | E5 — viewer 6-graph filter UI |
| `95aa3ac` | feat | E4 — G6 Temporal (Commit + git log) |
| `f93390b` | feat | E3 — G5 Distributed (HTTP/RPC) |
| `ea8d776` | feat | B1 — concurrency Stage 1 (Mutex/Channel/lock edges) |
| `e44b9ba` | docs | G7 follow-up 등재 |
| `40c5c5f` | refactor | A1+A2 review follow-ups (cap shadow, substring → exact match) |
| `7448817` | refactor | A1+A2 atomic — smacker → upstream tree-sitter |
| `a684239` | fix | A3 partial-cache → cold fallback (correctness) |
| `145e8da` | feat | A3 — file-level SHA256 incremental cache |
| `8183626` | docs | A5 NodeMutex insert clarification |
| `b501201` | feat | A5 — NodeMutex + 3 lock edges + schema 1.0→1.1 |
| `b1cb4c3` | refactor | A4 — ExportChunked → StoreReader |
| `4df3adf` | refactor | A4 — Store interface ISP split |
| `239e519` | docs | E2 follow-ups 등재 |
| `8dd08d5` | feat | E2 — Go production path → go/packages.Load (1300→1259 PARITY) |
| `0b61b3a` | docs | E1 follow-ups 등재 |
| `a126ee8` | feat | E1 — `ckg audit` (41 file drift 측정) |
| `ecabc9a` | fix | E6 — viewer EDGE_STYLE color de-dup |
| `43c39dc` | fix | E6 — viewer EDGE_STYLE align with backend 22-edge schema |
| `df70804` | docs | NEXT-SESSION → WORK-PLAN 교체 + 기본 housekeeping |

(이전 세션 기반 commit은 `38fe64e..e9c03f1` 범위 — 그 이전은 V0 era)

---

**End of handoff.** 본 문서 + WORK-PLAN.md 로 다음 세션이 즉시 작업 재개 가능.
