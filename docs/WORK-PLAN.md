# CKG Work Plan — post-V0

> NEXT-SESSION.md (T32-T38 핸드오프) 후속. V0 완료 시점부터 v0.2 spec + 사용자
> 정의 완성도 gap + viewer 운영성 개선까지 통합한 작업 리스트.

| Field | Value |
|---|---|
| Last update | 2026-04-28 |
| V0 status | **38/38 완료** (CP-1~CP-7 도달, T38 DoD 검증 포함) |
| Working dir | `/Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph` |
| Working tree expectation | clean (untracked: `.playwright-mcp/`, `ckg-*.png` — gitignored) |
| Subagent workflow | `/superpowers:subagent-driven-development` (impl → review → fix loop) |

---

## 1. Quick Start (cold-read, 5분)

```bash
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph
git log --oneline -5
go test ./...                                                  # 17 패키지 PASS
go build -o bin/ckg ./cmd/ckg
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth  # smoke
./bin/ckg serve --graph=/tmp/ckg-synth --port=8787 --open      # viewer
```

이어서: 본 문서 §3 Wave 1 부터 순차 진행.

핵심 문서:
- **본 문서** — 작업 리스트 + 운영 패턴
- `docs/spec-ckg-v0-prototype.md` (1,442 lines) — V0 결정 근거
- `docs/spec-ckg-v0.2.md` (497 lines) — v0.2 foundation spec (smacker 마이그레이션 + 동시성 + PG + incremental)
- `docs/plan-ckg-v0-prototype.md` (8,983 lines) — V0 구현 plan (T1-T38 verbatim)
- `docs/SCHEMA.md`, `docs/ARCHITECTURE.md`, `docs/EVAL.md` — 사용자용 reference
- `docs/STUDY-GUIDE.md` — 외부 개념 (Leiden / MCP / tree-sitter)

---

## 2. 누적 결과물 + 검증 가능 동작

### 누적 결과물

- 단일 Go 바이너리 `ckg` (멀티 OS, modernc/sqlite + tree-sitter via CGO)
- 5 subcommand: `build` / `serve` / `mcp` / `export-static` / `eval`
- 29 node types × 22 edge types (`pkg/types/enums.go`)
- 파서 3종: Go (`go/packages`) / TS-JS (smacker tree-sitter) / Solidity (vendored tree-sitter v1.2.11)
- xlang: Sol↔TS `binds_to` (name+ABI heuristic, INFERRED)
- Next.js 3D viewer (`web/viewer-next/`, react-force-graph-3d, embedded via `go:embed`)
- 6 MCP tools (find_symbol/callers/callees/get_subgraph/search_text/get_context_for_task)
- Eval framework: 4 baselines (α raw / β graph-dump / γ granular / δ smart 1-shot) × YAML tasks
- CI: `[ubuntu, macos, windows] × [amd64, arm64]` matrix + Playwright smoke
- Docs: SCHEMA / ARCHITECTURE / EVAL / README quick start

### 검증 가능 동작

```bash
make build
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth
./bin/ckg serve --graph=/tmp/ckg-synth --port=8787 --open
./bin/ckg mcp --graph=/tmp/ckg-synth
./bin/ckg export-static --graph=/tmp/ckg-synth --out=/tmp/ckg-static
ANTHROPIC_API_KEY=… ./bin/ckg eval --tasks='eval/tasks/synthetic-*.yaml' \
  --graph=/tmp/ckg-synth --baselines=alpha,beta,gamma,delta --out=eval/results
```

---

## 3. 작업 그룹 (post-V0)

### Group A — v0.2.0 Foundation (spec roadmap 1차)

| ID | 작업 | 의존 | 추정 |
|---|---|---|---|
| A1 | Item 1 Phase 1a: TS/JS tree-sitter smacker → upstream 마이그레이션 | — | M |
| A2 | Item 1 Phase 1b: Solidity 마이그레이션 + binding 정리 | A1 (병렬 가능) | M |
| A3 | Item 4 Phase 1: file-level SHA256 캐시 + manifest schema v2 + 변경 파일만 재파싱 | — | L |
| A4 | Item 3 Storage abstraction: `Store` interface 추출 (SQLite를 구현체로 정리) | — | M |
| A5 | Schema bump 1.0→1.1 + concurrency edge 자리 예약 (`acquires_lock` 등) | — | S |

### Group B — v0.2.1 Concurrency + PG export

| ID | 작업 | 의존 | 추정 |
|---|---|---|---|
| B1 | Item 2 Stage 1: Go AST 휴리스틱 (Goroutine/Channel/Mutex, types.Info) | A5 | L |
| B2 | Item 3 Phase 1: `ckg export-postgres --dsn ... --source ...` 명령 | A4 | M |
| B3 | Item 1 Phase 1c: incremental parsing 인프라 (Tree.Edit() API) | A1, A3 | M |

### Group C — v0.2.2 Incremental Pass 2 + PG primary

| ID | 작업 | 의존 | 추정 |
|---|---|---|---|
| C1 | Item 4 Phase 2: reverse-reference invalidation | A3 | L |
| C2 | Item 3 Phase 2: `ckg build --db postgres://...` direct PG 빌드 | B2 | L |

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
| E3 | 6 graphs G5 Distributed: `listens_on`, `handles_message`, `rpc_calls` | #3 | L |
| E4 | 6 graphs G6 Temporal: `git log --follow` 기반 `changed_in`, `blame` | #3 | M |
| E5 | viewer에 6-graph 그룹 필터 UI (G1~G6별 토글) | #4 | M |
| E6 | edge type schema vs viewer EDGE_STYLE desync 수정 | #4 | S |

사용자 조건 4가지 (이전 분석 세션에서 사용자 제시):
1. 빌드 시 포함되는 모든 파일이 누락 없이 코드 그래프 DB화
2. 누락 없음을 audit으로 검증 가능
3. CKS deep-dive 6 graph 지원 (Structural / Semantic / Execution / Concurrency / Distributed / Temporal)
4. viewer + CLI evaluation 가능

### Group F — viewer 운영성 (사용자 추가 질문 대응)

| ID | 작업 | 추정 |
|---|---|---|
| F1 | `CKG_DEV_VIEWER_DIR` env: dev hot reload (viewer 변경 시 ckg 재빌드 불요) | S |
| F2 | `ckg serve --no-viewer` 옵션: API only (operator의 reverse-proxy 패턴) | S |
| F3 | README에 production-split 패턴 (export-static + 정적 호스팅) 1차 권장 명시 | S |

### Group G — Tech debt / 위생

| ID | 작업 | 상태 |
|---|---|---|
| G1 | `.gitignore`에 `.playwright-mcp/` + `ckg-*.png` 추가 | ✅ 본 commit |
| G2 | repo root의 `ckg-*.png` 디버그 스크린샷 5개 삭제 | ✅ 본 commit |
| G3 | NEXT-SESSION.md 정리 → 본 문서로 대체 | ✅ 본 commit |

추정: XS=5분, S=15-30분, M=1-2시간, L=반나절, XL=하루+

---

## 4. 진행 순서 (Wave)

```
Wave 1 (즉시): G1 + G2 + G3 + E6                    [housekeeping + edge desync]
Wave 2 (1-2일): E1 + E2                             [사용자 조건 #1, #2]
Wave 3 (3-5일): A4 + A5 + A3                        [v0.2.0 Storage + Schema + Cache]
Wave 4 (3-5일): A1 + A2                             [smacker 제거]
Wave 5 (1주+): B1 + E3 + E4 + E5                    [Concurrency + 6 graph 완전성]
Wave 6 (별도): B2/B3/C1/C2/D1/D2                    [PG / SSA / pgvector]
Wave 7 (병렬): F1 + F2 + F3                         [viewer 운영성]
```

각 wave 종료 후 §6 검증 명령 모두 그린이어야 다음 wave 시작.

### Wave간 의존성 그래프

```
A1 ──► A2 (병렬 가능)
A1 ──► B3 (incremental parsing)
A3 ──► C1 (Pass 2 invalidation)
A4 ──► B2 ──► C2 ──► D2
A5 ──► B1 ──► D1
E1 ──► E2 (audit으로 누락 측정 후 측정 기반 수정)
E3, E4 ──► E5 (graph 데이터 있어야 viewer filter 의미)
F1, F2, F3 — 모두 독립
```

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
go test ./...
go test -tags e2e ./...

# 2. Web side (viewer 변경 있을 때)
make viewer

# 3. Binary smoke
make build
./bin/ckg --help

# 4. End-to-end smoke
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth
ls /tmp/ckg-synth/        # graph.db + manifest.json
./bin/ckg export-static --graph=/tmp/ckg-synth --out=/tmp/ckg-static
ls /tmp/ckg-static/

# 5. Eval (Wave 5 이후 새 graph 추출 항목 추가 시)
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
    github.com/mark3labs/mcp-go v0.49.0
    github.com/smacker/go-tree-sitter v0.0.0-20240827...   // A1/A2에서 제거 예정
    github.com/spf13/cobra v1.10.2
    golang.org/x/tools v0.44.0
    gopkg.in/yaml.v3 v3.0.1
    modernc.org/sqlite v1.49.1
)
```

### Web (`web/viewer-next/package.json`)

Next.js 14 + react-force-graph-2d/3d + zustand + lit-html 잔재 정리 중.

### Vendored

- `internal/parse/solidity/binding/` — JoranHonig/tree-sitter-solidity v1.2.11
  (smacker ABI 14 호환. A2에서 upstream 0.25 ABI로 정리 결정 필요.)

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
- [ ] `ckg audit --src=… --graph=…` 동작
- [ ] testdata/synthetic 빌드 후 audit zero-diff 확인
- [ ] Go production path가 `go/packages.Load(./...)` 사용
- [ ] build constraint 무시 case 회귀 테스트 통과
- [ ] `go.work` 또는 generated 파일 inclusion 회귀 테스트

### Wave 3 (A4 + A5 + A3)
- [ ] `persist.Store` interface 추출, SQLite는 구현체
- [ ] schema_version 1.0 → 1.1, 새 edge 자리 (`acquires_lock` 등) 정의
- [ ] file-level SHA256 캐시 동작 (재빌드 시 변경 없는 파일 0건 재파싱)
- [ ] `--no-cache` 플래그로 강제 전체 재빌드 가능

### Wave 4 (A1 + A2)
- [ ] `go.mod`에서 `smacker/go-tree-sitter` 제거
- [ ] golden 테스트로 마이그레이션 전후 그래프 동일성 확인 (노드/엣지 1:1)
- [ ] `ckg build` end-to-end 정상

### Wave 5 (B1 + E3 + E4 + E5)
- [ ] Goroutine/Channel/Mutex 노드 + 엣지 추출 (Stage 1)
- [ ] G5 Distributed edge 추출 (Go HTTP handler / gRPC service)
- [ ] G6 Temporal edge 추출 (`changed_in`, `blame`)
- [ ] viewer에 6-graph 그룹 토글 UI

---

## 9. References

### Spec / Plan / Study

- `docs/spec-ckg-v0-prototype.md` — V0 design (1,442 lines)
- `docs/spec-ckg-v0.2.md` — v0.2 foundation spec (497 lines)
- `docs/plan-ckg-v0-prototype.md` — V0 implementation plan (8,983 lines)
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
