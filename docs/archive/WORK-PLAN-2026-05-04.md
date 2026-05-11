# CKG Work Plan — post-V0

> NEXT-SESSION.md (T32-T38 핸드오프) 후속. V0 완료 시점부터 v0.2 spec + 사용자
> 정의 완성도 gap + viewer 운영성 개선까지 통합한 작업 리스트.

| Field | Value |
|---|---|
| Last update | 2026-05-04 (refresh 7 — G6 v4 + C1 + C2 + real-corpus parity ✅) |
| V0 status | **38/38 완료** (CP-1~CP-7 도달, T38 DoD 검증 포함) |
| HEAD | `3514a7a` (`docs: refresh handoff — G6 v4 + C1 + C2 completed`) |
| Working dir | `/Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph` |
| Working tree expectation | clean |
| Subagent workflow | `/superpowers:subagent-driven-development` (impl → review → fix loop) |

---

## 1. Quick Start (cold-read, 5분)

```bash
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph
git log --oneline -5
go test ./...                                                   # 18 패키지 PASS
go build -o bin/ckg ./cmd/ckg
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth   # cold build
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth   # partial (short-circuit)
./bin/ckg audit --src=testdata/synthetic --graph=/tmp/ckg-synth # exit 0 = parity
./bin/ckg serve --graph=/tmp/ckg-synth --port=8080 --open       # viewer
```

이어서: 본 문서 §4 Wave 9 진입 포인트 참조.

핵심 문서:
- **본 문서** — 작업 리스트 + 운영 패턴
- `docs/spec-ckg-v0.2.md` (497 lines) — v0.2 foundation spec (smacker 마이그레이션 + 동시성 + PG + incremental)
- `docs/SCHEMA.md`, `docs/ARCHITECTURE.md`, `docs/EVAL.md` — 사용자용 reference
- `docs/STUDY-GUIDE.md` — 외부 개념 (Leiden / MCP / tree-sitter)

---

## 2. 누적 결과물 + 검증 가능 동작

### 누적 결과물

- 단일 Go 바이너리 `ckg` (멀티 OS, modernc/sqlite CGO-free + pgx/v5)
- 7 subcommand: `build` / `serve` / `mcp` / `export-static` / `export-postgres` / `eval` / `audit`
- 33 node types × 30 edge types (`pkg/types/enums.go`)
- 파서 3종: Go (`go/packages`) / TS-JS (tree-sitter/go-tree-sitter v0.25) / Solidity (vendored v1.2.11)
- xlang: Sol↔TS `binds_to` (name+ABI heuristic, INFERRED)
- Next.js 3D viewer (`web/viewer-next/`, react-force-graph-3d, 6-graph filter UI, embedded via `go:embed`)
- 6 MCP tools (find_symbol/callers/callees/get_subgraph/search_text/get_context_for_task)
- Eval framework: 4 baselines (α raw / β graph-dump / γ granular / δ smart 1-shot) × YAML tasks
- CI: `[ubuntu, macos, windows] × [amd64, arm64]` matrix + Playwright smoke
- Docs: SCHEMA / ARCHITECTURE / EVAL / README quick start / INCREMENTAL
- **Storage backends**: SQLite (default) / PostgreSQL (`--db postgres://...`)
- **Incremental cache**: cold + short-circuit + partial (G6 v4 correctness fix ✅, C1 reverse-ref invalidation ✅)
- **Logging**: `--verbose` / `--log-file` / `CKG_LOG_LEVEL=debug`
- go-stablenet parity: cold vs partial diff = **0** (214,343 nodes / 652,892 edges)

### 검증 가능 동작

```bash
make build
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth
./bin/ckg serve --graph=/tmp/ckg-synth --port=8080 --open
./bin/ckg serve --graph=/tmp/ckg-synth --no-viewer --port=8788   # API-only
./bin/ckg mcp --graph=/tmp/ckg-synth
./bin/ckg export-static --graph=/tmp/ckg-synth --out=/tmp/ckg-static
./bin/ckg audit --src=testdata/synthetic --graph=/tmp/ckg-synth   # exit 0 = parity
ANTHROPIC_API_KEY=… ./bin/ckg eval --tasks='eval/tasks/synthetic-*.yaml' \
  --graph=/tmp/ckg-synth --baselines=alpha,beta,gamma,delta --out=eval/results

# PostgreSQL backend (선택)
./bin/ckg build --src=testdata/synthetic --db=postgres://user:pass@localhost/ckg
./bin/ckg serve --db=postgres://user:pass@localhost/ckg --port=8080
```

---

## 3. 작업 그룹 (post-V0)

### Group A — v0.2.0 Foundation (spec roadmap 1차)

| ID | 작업 | 의존 | 추정 |
|---|---|---|---|
| A1 | Item 1 Phase 1a: TS/JS tree-sitter smacker → upstream 마이그레이션 | — | M ✅ (atomic with A2, see below) |
| A2 | Item 1 Phase 1b: Solidity 마이그레이션 + binding 정리 | A1 (atomic 병합) | M ✅ (vendored grammar v1.2.11 ABI 14 호환, byte-level golden parity 확보) |
| A3 | Item 4 Phase 1: file-level SHA256 캐시 + manifest schema v2 + 변경 파일만 재파싱 | — | L ✅ (cache_key + manifest v2 + ON DELETE CASCADE + --no-cache/--rebuild-metrics; cold→warm 40s→1s on go-stablenet-latest 2142 files) |
| A4 | Item 3 Storage abstraction: `Store` interface 추출 (SQLite를 구현체로 정리) | — | M ✅ (ISP split: StoreReader / StoreWriter / Store; struct → unexported sqliteStore) |
| A5 | Schema bump 1.0→1.1 + concurrency edge 자리 예약 (`acquires_lock` 등) | — | S ✅ (NodeMutex + 3 lock edges 슬롯 예약, viewer styling 추가, 1.0 graph 하위 호환 검증) |

### Group B — v0.2.1 Concurrency + PG export

| ID | 작업 | 의존 | 추정 |
|---|---|---|---|
| B1 | Item 2 Stage 1: Go AST 휴리스틱 (Goroutine/Channel/Mutex, types.Info) | A5 | L ✅ (Mutex/Channel emission, acquires_lock/releases_lock 781/834 on go-stable-code; accessed_under_lock Phase 4 deferred → B1-FOLLOWUP. **Channel flow 강화 2026-05-04**: sends_to/recvs_from → Channel 노드 직접 연결, goroutine body 추적, eb5e9bb+8784ac9) |
| B2 | Item 3 Phase 1: `ckg export-postgres --dsn ... --source ...` 명령 | A4 | M ✅ (jackc/pgx/v5 COPY 프로토콜, 전 필드 export, DSNHost URL+kv 양식, 테스트 4개, 13317f7) |
| B3 | Item 1 Phase 1c: incremental parsing 인프라 (Tree.Edit() API) | A1, A3 | M |

### Group C — v0.2.2 Incremental Pass 2 + PG primary ✅ 완료

| ID | 작업 | 의존 | 추정 |
|---|---|---|---|
| C1 | Item 4 Phase 2: reverse-reference invalidation | A3 ✅ | L ✅ **95dc3c2** — `ReverseDepsForFiles` StoreReader (SQLite+PG), incremental step 1.5, LIKE suffix match. real-corpus diff=0 확인. |
| C2 | Item 3 Phase 2: `ckg build --db postgres://...` direct PG 빌드 | B2 ✅ | L ✅ **6d01112** — pgxpool full Store implementation (~1160 lines), `--db` flag on build+serve. |

### Group D — v0.3.0 (별도 spec 필요)

| ID | 작업 | 의존 | 추정 |
|---|---|---|---|
| D1 | Item 2 Stage 2: SSA 정밀 동시성 (`--deep` opt-in) | B1 | XL |
| D2 | Item 3 Phase 3: pgvector + Apache AGE 통합 | C2 | XL |

### Group E — 사용자 4 완성도 조건 gap

| ID | 작업 | 사용자 조건 | 추정 |
|---|---|---|---|
| E1 | `ckg audit` 명령: `go list -deps -json ./...` vs DB의 `SELECT DISTINCT file_path` set-diff | #1, #2 | M |
| E2 | Go file inclusion: production path를 `go/packages.Load(./...)` 기반으로 교체 | #1 | L |
| E3 | 6 graphs G5 Distributed: `listens_on`, `handles_message`, `rpc_calls` | #3 | L ✅ (NodeEndpoint + NodeMessageType slot adds; net/http HandleFunc/Handle + ServeMux + JSON-RPC handler signature + net/rpc client.Call detectors; schema 1.2→1.3; deferred: gRPC stubs, P2P broadcasters, consensus_flow) |
| E4 | 6 graphs G6 Temporal: `git log` 기반 `changed_in`, `blame` | #3 | M ✅ (NodeCommit slot add; single `git log --raw --no-renames` invocation per build → per-file commit list capped at 10; `changed_in` heuristic = 모든 symbol whose file_path matches × commits touching that file; `blame` = File node → most-recent commit (V0 file-level simplification, line-level deferred); schema 1.3→1.4; graceful skip when not git checkout; deferred: line-level blame, submodule traversal, correlated_with/observed_in/mentioned_in) |
| E5 | viewer에 6-graph 그룹 필터 UI (G1~G6별 토글) | #4 | M ✅ (`web/viewer-next/src/lib/edges.ts`에 GRAPH_GROUPS 단일 출처 정의 — G1~G6에 29개 non-hidden edge 매핑; `EdgeTypeFilters.tsx` 재작성 — collapsible group section + 3-state group toggle (all-on / partial / all-off, "all"/"some"/"none" 라벨); group toggle은 `setEdgeTypeWhitelistBulk` store 액션 호출; 접힘 상태는 `localStorage[ckg.edgeFiltersCollapsed]`에 JSON array of GraphID 영속; default-collapsed = G1+G2 (가장 다수, trace mode에서 부차적); 각 헤더에 dominant edge color dot + N/M count + tooltip; backend 변경 0줄) |
| E6 | edge type schema vs viewer EDGE_STYLE desync 수정 | #4 | S |

사용자 조건 4가지 (이전 분석 세션에서 사용자 제시):
1. 빌드 시 포함되는 모든 파일이 누락 없이 코드 그래프 DB화
2. 누락 없음을 audit으로 검증 가능
3. CKS deep-dive 6 graph 지원 (Structural / Semantic / Execution / Concurrency / Distributed / Temporal)
4. viewer + CLI evaluation 가능

### Group F — viewer 운영성 (사용자 추가 질문 대응) — Wave 7 ✅

| ID | 작업 | 추정 |
|---|---|---|
| F1 | `CKG_DEV_VIEWER_DIR` env: dev hot reload (viewer 변경 시 ckg 재빌드 불요) | S ✅ (`server.Options{DevViewerDir}` + os.DirFS swap; commit `1d42787`) |
| F2 | `ckg serve --no-viewer` 옵션: API only (operator의 reverse-proxy 패턴) | S ✅ (`Options{NoViewer}`; static mount skip; `--open` 자동 suppress; commit `1d42787`) |
| F3 | README에 production-split 패턴 (export-static + 정적 호스팅) 1차 권장 명시 | S ✅ (export-static + serve --no-viewer + reverse proxy + dev hot-reload 모두 명시; commit `412e622`) |

### Group G — Tech debt / 위생

| ID | 작업 | 상태 |
|---|---|---|
| G1 | `.gitignore`에 `.playwright-mcp/` + `ckg-*.png` 추가 | ✅ `df70804` |
| G2 | repo root의 `ckg-*.png` 디버그 스크린샷 5개 삭제 | ✅ `df70804` |
| G3 | NEXT-SESSION.md 정리 → 본 문서로 대체 | ✅ `df70804` |
| G4 | `pipeline.go` 596줄 → language_runners.go + staleness.go 분리 (pipeline.go 359줄, 모두 400줄 미만) | ✅ `c0ecd81` |
| G5 | `go.work` 회귀 smoke test (workspace root at srcRoot, 2 member modules) | ✅ TestGoFiles_GoWorkspace ([G5 commit]) — outside-srcRoot members 미지원 documented |
| G6 | A3 incremental 경로 재활성화. v1+v2 fail → v3 D4(cold fallback) → **v4 완료** (`6d01112`). Root cause H3: `NodesByFilePath` rowid 순 반환 ≠ AST 선언 순서 → `ORDER BY start_line ASC` 추가. `runIncremental` D4 dead code 활성화. C1(reverse-ref invalidation) 구현 후 real-corpus cold vs partial diff = 0 확인. | ✅ **6d01112 + 95dc3c2** — go-stablenet 214,343 nodes / 652,892 edges 완전 일치 |
| G7 | TS/Sol parser 패키지에 committed golden test 추가 (fixture parse → 안정 marshalling → checked-in golden 비교). 미래 tree-sitter 버전 bump 시 silent miss-extraction을 CI에서 즉시 감지. | ✅ TestGolden_TypeScript / TestGolden_Solidity (G7 commit) — `-update` flag로 golden 갱신 |
| G8 | B1 Phase 4: `accessed_under_lock` edges (Lock~Unlock 사이 변수 접근 추적). | ✅ go-stable-code 0 → 2916 edges; intra-function lexical heuristic (cross-function propagation은 D1 SSA territory) |
| G9 | B1 Mutex detection root cause: emitMutexNode와 emitFields가 **동일 qname + 동일 startByte** 사용 → MakeID 해시 충돌 → graph.Build의 last-writer-wins로 emitFields의 Field 노드가 emitMutexNode의 Mutex 노드를 silent overwrite. mutexNodeIDs map은 (이제 의미 없는) 그 ID 보유, Lock detection은 그 ID로 edge emit. Fix: emitMutexNode qname에 `#mutex` suffix 추가하여 disambiguate. go-stable-code 측정: Mutex nodes 8 → 170 (21× 증가), Field-targeting acquires_lock 157 → 1 (99.4% 감소, 남은 1건은 함수 local 엣지 케이스). | ✅ |

추정: XS=5분, S=15-30분, M=1-2시간, L=반나절, XL=하루+

---

## 4. 진행 순서 (Wave)

```
Wave 1 (즉시): G1 + G2 + G3 + E6                    ✅ 완료 [housekeeping + edge desync]
Wave 2 (1-2일): E1 + E2                             ✅ 완료 [사용자 조건 #1, #2]
Wave 3 (3-5일): A4 + A5 + A3                        ✅ 완료 [v0.2.0 Storage + Schema + Cache]
Wave 4 (3-5일): A1 + A2                             ✅ 완료 [smacker 제거]
Wave 5 (1주+): B1 + E3 + E4 + E5                    ✅ 완료 [Concurrency + 6 graph 완전성]
Wave 6: B2 + Logging + Channel flow                 ✅ 완료 [export-postgres + 구조화 로그 + sends_to/recvs_from]
Wave 7 (병렬): F1 + F2 + F3                         ✅ 완료 [viewer 운영성]
Wave 8: G6 v4 + C1 + C2                             ✅ 완료 [incremental correctness + PG primary + real-corpus diff=0]
Wave 9 (next): B3 + minor follow-ups                ← 진입 가능
```

각 wave 종료 후 §6 검증 명령 모두 그린이어야 다음 wave 시작.

### Wave간 의존성 그래프 (현재 상태)

```
A1 ──► A2 (병렬 가능)                               ✅
A1 ──► B3 (incremental parsing)                    ← next
A3 ──► C1 (Pass 2 invalidation)                    ✅
A4 ──► B2 ──► C2 ──► D2                            ✅ (C2까지), D2 미착수
A5 ──► B1 ──► D1                                   ✅ (B1까지), D1 미착수
E1 ──► E2                                          ✅
E3, E4 ──► E5                                      ✅
F1, F2, F3                                         ✅
G6 v4 ──► C1 ──► B3                                ✅ (B3 진입 가능)
```

### Wave 9 진입 포인트

| 작업 | 추정 | 우선순위 |
|---|---|---|
| **B3** Tree.Edit() incremental parsing (재파싱 비용 절감) | M | 1 |
| E2-FU `go.work` 회귀 테스트 | S | 2 |
| Wave1 DoD viewer dead-key 정리 (`reads/writes/…`) | S | 2 |
| E3-FU httprouter 패턴 + Ethereum RPC client.Call | S | 3 |
| E4-FU line-level blame | M | 3 |

---

## 5. 운영 패턴 (NEXT-SESSION.md에서 승계)

### 5.1 Subagent-driven 패턴

`/superpowers:subagent-driven-development` 스킬 그대로:
1. 각 task별로 fresh `general-purpose` subagent 디스패치 (impl + commit)
2. `superpowers:code-reviewer` subagent로 review
3. Critical/Important issue 있으면 fix subagent 디스패치 후 다음 task

작은 task (single-line fix, doc-only)는 main session에서 직접 처리하는 편이 효율적
— skill의 "When NOT to use: tightly coupled" 가이드 준수.

### 5.2 발견된 plan/spec 결함 패턴 (V0에서 수집)

| Source | 결함 | 수정 방식 |
|---|---|---|
| Plan T3 | Go 1.22 명시 vs modernc/sqlite v1.49.1 requires 1.25 | go.mod 1.25로 정렬 |
| Plan T22 | three@0.158 vs 3d-force-graph@1.80 peer >=0.179 | three 0.180 bump |
| Plan T29 | LSP-style framing vs mcp-go NDJSON | 실 라이브러리 동작 우선 |
| Spec v0.2 R1.1 | Tree-sitter Query DSL 미세 변화로 silent miss-extraction | golden 테스트 필수 |

**다음 세션 행동 가이드**: subagent에게 spec/plan을 verbatim 따르라고 하되,
**테스트 실패 또는 컴파일 에러 시 spec vs 실제 라이브러리 API 차이 의심**.
Subagent가 deviation을 자체 판단하면 review 단계에서 검증.

### 5.3 gopls 캐시 지연 false positive

매 task마다 새 패키지 추가 시 gopls가 IDE 진단으로 `BrokenImport` /
`UndeclaredName` / `MissingFieldOrMethod` / `unusedfunc` 경고를 표시함 (몇 분간).
**실제 `go build ./...` / `go test ./...` 는 그린**. 매번 실 build/test로 검증 후
false positive 무시.

### 5.4 Subagent 호출 비용

각 task 평균 2~3 subagent dispatch (impl + review + 가끔 fix).
한 dispatch ~50K~100K tokens. Wave별 견적:

| Wave | Tasks | 예상 dispatch |
|---|---|---|
| 1 | G/E6 | ~3 (E6만 subagent) |
| 2 | E1, E2 | ~6 |
| 3 | A4, A5, A3 | ~9 |
| 4 | A1, A2 | ~6 |
| 5 | B1, E3, E4, E5 | ~12 |

### 5.5 Commit 컨벤션 (HARD CONSTRAINTS)

- Conventional Commits, English subject
- **NO `Co-Authored-By` / `Generated with [Claude Code]` attribution** (사용자 글로벌 룰)
- Subject ≤ 70 chars 권장
- Body는 *why* 중심, verbose 지양

### 5.6 Viewer build 분리 (v0.2 spec § 5 권장)

현재 Makefile의 `build: viewer` 의존이 chunk hash desync 문제 유발 가능
(`docs/spec-ckg-v0.2.md` 외 본 문서 Group F). v0.2.0 외 별도 진행 가능:
- F1 (CKG_DEV_VIEWER_DIR env) — 단일 옵션 추가, viewer dev 사이클 분리
- F2 (`--no-viewer` 옵션) — operator의 reverse-proxy 패턴 지원

---

## 6. 검증 명령 (Wave 경계마다)

```bash
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph

# 1. Go side
go vet ./...
go test ./...         # 18 패키지 PASS

# 2. Binary smoke
go build -o bin/ckg ./cmd/ckg
./bin/ckg --help

# 3. End-to-end smoke (synthetic)
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth   # warm (short-circuit)
./bin/ckg audit --src=testdata/synthetic --graph=/tmp/ckg-synth # exit 0
./bin/ckg export-static --graph=/tmp/ckg-synth --out=/tmp/ckg-static

# 4. Real-corpus parity (큰 변경 후 필수)
STABLENET=/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest
./bin/ckg build --src=$STABLENET --out=/tmp/ckg-stable --no-cache
sqlite3 /tmp/ckg-stable/graph.db "SELECT COUNT(*) FROM edges"  # baseline: 652892
# 파일 1개 수정 → partial build
./bin/ckg build --src=$STABLENET --out=/tmp/ckg-stable
sqlite3 /tmp/ckg-stable/graph.db "SELECT COUNT(*) FROM edges"  # target: same

# 5. Eval
ANTHROPIC_API_KEY=… ./bin/ckg eval --tasks='eval/tasks/synthetic-*.yaml' \
  --graph=/tmp/ckg-synth --baselines=alpha,beta,gamma,delta --out=eval/results

# 6. Working tree clean
git status --short
```

---

## 7. 환경 / 의존성 현재 상태

### Go module (`go.mod`)

```
module github.com/0xmhha/code-knowledge-graph
go 1.25.5

require (
    github.com/0xmhha/cli-wrapper v0.2.1
    github.com/anthropics/anthropic-sdk-go v1.38.0
    github.com/jackc/pgx/v5 v5.9.2                          // B2 + C2 (PG export + PG store)
    github.com/mark3labs/mcp-go v0.49.0
    github.com/spf13/cobra v1.10.2
    github.com/tree-sitter/go-tree-sitter v0.25.0           // A1+A2 (smacker 대체)
    github.com/tree-sitter/tree-sitter-javascript v0.25.0   // A1+A2
    github.com/tree-sitter/tree-sitter-typescript v0.23.2   // A1+A2
    golang.org/x/tools v0.44.0
    gopkg.in/yaml.v3 v3.0.1
    modernc.org/sqlite v1.49.1
)
```

### Web (`web/viewer-next/package.json`)

Next.js 14 + react-force-graph-2d/3d + zustand + lit-html 잔재 정리 중.

### Vendored

- `internal/parse/solidity/binding/` — JoranHonig/tree-sitter-solidity v1.2.11
  (LANGUAGE_VERSION=14, upstream go-tree-sitter v0.25 ABI window 13..15 안에 들어가
  regenerate 없이 그대로 사용. cgo bridge는 어떤 Go binding과도 무관하게 동작.)

### Build artifacts (gitignored)

- `bin/ckg` — `make build`
- `web/viewer-next/{out,.next,node_modules}/`
- `internal/server/web_assets/_next/`, `404/`, `404.html`, `index.txt`
  (stub `index.html`만 commit)

---

## 8. Definition of Done — Wave별

### Wave 1 (G + E6)
- [x] `.gitignore`에 .playwright-mcp/ + ckg-*.png 등재
- [x] 디버그 스크린샷 5개 제거
- [x] `docs/NEXT-SESSION.md` 제거 (본 문서로 대체)
- [ ] viewer EdgeTypeFilters의 모든 토글이 실 schema와 매칭
- [ ] `lib/edges.ts`의 dead key (`reads/writes/modifies/decorates/emits`) 제거 OR
      backend가 emit 시작하도록 정렬 결정 commit

### Wave 2 (E1 + E2)
- [x] `ckg audit --src=… --graph=…` 동작
- [x] testdata/synthetic 빌드 후 audit zero-diff 확인 (3/3 parity, exit 0)
- [x] Go production path가 `go/packages.Load(./...)` 사용
- [x] build constraint 무시 case 회귀 테스트 통과
- [ ] `go.work` 또는 generated 파일 inclusion 회귀 테스트
      <!-- TODO(E2-followup): go.work 시나리오는 별도 fixture가 필요. 현재
           detect.GoFiles는 go.mod 단위로 packages.Load("./...")를 호출하므로
           workspace 멤버가 srcRoot 외부에 있는 케이스는 미지원 — go.work를
           쓰는 대형 모노레포에서 회귀 테스트 추가 필요. -->

E1 측정 결과 (go-stablenet-latest 대상, 2026-04-28):
- build set (go/packages, Tests=true): 1259 files
- DB set (detect.Walk): 1300 files
- in_build_only: 0 (현재 production은 누락 없음)
- in_db_only: 41 (build-tag mismatch + `//go:build ignore` 도구 + cross-OS shim 과다 포함)
- E2 목표: in_db_only를 0으로 수렴시키되, in_build_only도 계속 0 유지

E2 검증 결과 (go-stablenet-latest 대상, 2026-04-28):
- build set: 1259 / db set: 1259 / in_both: 1259
- in_build_only: 0 / in_db_only: 0 → **PARITY** (exit 0)
- testdata/synthetic 동시 검증: 3/3 PARITY 유지

### Wave 3 (A4 + A5 + A3)
- [x] `persist.Store` interface 추출, SQLite는 구현체
- [x] schema_version 1.0 → 1.1, 새 edge 자리 (`acquires_lock` 등) 정의
- [x] file-level SHA256 캐시 동작 (재빌드 시 변경 없는 파일 0건 재파싱)
- [x] `--no-cache` 플래그로 강제 전체 재빌드 가능

A3 측정 결과 (go-stablenet-latest, 2026-04-29):
- cold rebuild: 40.4s (1259 .go + 320 .ts + 563 .sol = 2142 files)
- warm rebuild: 0.99s (full cache hit, manifest timestamp refresh only)
- 1-file modify: ~0.2s (synthetic) — "Cache: 7 hits, 1 misses, 0 removed; parsed 1 files"
- schema bump 1.1 → 1.2 (FK ON DELETE CASCADE for cache invalidation cascading)

### Wave 4 (A1 + A2)
- [x] `go.mod`에서 `smacker/go-tree-sitter` 제거 (atomic with A1)
- [x] golden 테스트로 마이그레이션 전후 그래프 동일성 확인 (노드/엣지 1:1)
- [x] `ckg build` end-to-end 정상

A1+A2 측정 결과 (2026-04-29):
- testdata/synthetic 7-pair diff (go/ts/sol nodes.txt + nodes.csv + edges.txt): 모두 0 lines
- go-stablenet-latest: pre/post 모두 nodes=216200 / edges=317614
- 244 duplicate C symbol blocker 해소 (smacker + tree-sitter/go-tree-sitter 동시 import 시 발생하던 ts_parser_new 등 중복 정의)
- Solidity vendored grammar (JoranHonig v1.2.11, LANGUAGE_VERSION=14)는 upstream
  go-tree-sitter v0.25 ABI window (13..15) 안에 들어가 regenerate 불필요

### Wave 5 (B1 + E3 + E4 + E5)
- [x] Goroutine/Channel/Mutex 노드 + 엣지 추출 (Stage 1)
- [x] G5 Distributed edge 추출 (Go HTTP handler / JSON-RPC handler / net/rpc client)
- [x] G6 Temporal edge 추출 (`changed_in`, `blame`)
- [x] viewer에 6-graph 그룹 토글 UI

E3 측정 결과 (go-stablenet-latest, 2026-04-29):
- Endpoint: 2 (metrics/exp 모듈의 `http.Handle` 호출 — corpus는 대부분 `httprouter` 사용으로 stdlib HandleFunc/Handle 직접 사용 거의 없음)
- MessageType: 48 (JSON-RPC 시그니처 일치하는 `func (T) M(args A, reply *R) error` 메서드의 args 타입)
- listens_on: 0 (Endpoint은 잡았으나 핸들러가 함수 반환값/computed value라 anchor할 named function 없음 — V0 한계로 문서화)
- handles_message: 57 (모두 INFERRED — 시그니처 패턴만으로 판정, false positive 가능성 surfaces)
- rpc_calls: 0 (corpus는 Ethereum-style `client.Call(&result, "method", args...)` 형태 — net/rpc의 첫 인자가 메서드 문자열인 패턴과 다름)
- schema_version: 1.2 → 1.3 (manifest 검증 완료)
- 향후 보강 후보: httprouter / gorilla/mux / chi router 패턴 추가, Ethereum RPC client.Call(&result, method, ...) 시그니처 추가

E4 측정 결과 (go-stablenet-latest, 2026-04-29):
- Commit: 34 (corpus는 main repo가 비교적 얕은 history; default cap = 10 commits/file이라 distinct commit 수가 자연 수렴)
- changed_in: 344727 (= 평균 ~163 nodes/file × ~2.1 commits/file × 1259 Go files + ts/sol equivalents — file-level heuristic 특성상 high cardinality)
- blame: 1319 (File node → 최근 commit. corpus의 File 노드 2142개 중 git log이 본 1319개만 매칭 — 차이 823개는 submodule(`.gitmodules` 존재)에 속한 파일들로, V0는 main repo의 `git log -- .` 만 fetch하므로 silently skip — 문서화된 한계)
- schema_version: 1.3 → 1.4 (manifest 검증 완료)
- temporal pass 단독 시간: ~0.4s (single `git log --raw` invocation, in-memory parse + edge emission)
- cold build total: 21:06:03 → 21:06:58 = ~55s (pre-E4 baseline ~40s; +15s overhead은 추가 666332 - 322,...주: 다음 build의 ~344K edge insert가 dominate, git log 자체는 1초 미만)
- warm rebuild: 0.1s short-circuit (E4 edges 동결 — 모든 cache hit 시 manifest timestamp만 갱신, temporal 재실행 없음)
- 향후 보강 후보: line-level blame (G6 Phase 2 — `git blame --line-porcelain`), submodule history 트래버스, correlated_with/observed_in/mentioned_in (incident/runtime/PR 통합 D-tier)

---

## 9. References

### Spec / Plan / Study

- `docs/spec-ckg-v0.2.md` — v0.2 foundation spec (497 lines)
- `docs/STUDY-GUIDE.md` — Leiden / MCP / tree-sitter / 3D layout
- 외부 참조: CKS deep-dive
  `/Users/wm-it-22-00661/Work/github/stable-net/study/projects/stablenet-ai-agent/claudedocs/04-cks-deep-dive.md`
  (6 graph 정의의 원전, Group E의 #3 출처)

### 외부 리소스

- mcp-go: https://github.com/mark3labs/mcp-go (v0.49.0 — NDJSON stdio framing)
- 3d-force-graph: https://github.com/vasturiano/3d-force-graph
- react-force-graph-2d/3d: https://github.com/vasturiano/react-force-graph
- tree-sitter upstream: https://github.com/tree-sitter/go-tree-sitter (A1/A2 대상)
- tree-sitter-solidity: https://github.com/JoranHonig/tree-sitter-solidity (vendored v1.2.11)
- pgvector: https://github.com/pgvector/pgvector (D2)
- Apache AGE: https://age.apache.org/ (D2)
- golang.org/x/tools/go/{ssa,callgraph} (D1)

### 기존 stablenet-ai-agent 프로젝트와의 관계

CKG는 stablenet-ai-agent의 CKS 구현 검증용 standalone prototype.
- 검증 대상 corpus: `/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest`
- CKS 통합은 V1+ 영역 (Vector DB, hybrid retrieval)
- v0.2 spec은 CKS 통합 전 기반 보강 (smacker 제거 + 동시성 + PG + incremental)

---

**End of work plan. Wave 1부터 진행. 각 wave 종료 후 §6 검증 → 다음 wave.**
