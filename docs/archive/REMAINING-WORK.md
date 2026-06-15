> **ARCHIVED 2026-06-15.** Dated Tier-3 snapshot, superseded — most Tier A/B
> items landed in PR #12–#22 (concurrency_impact, lint sweep, temporal-depth,
> canonical symbol). Live work tracking: `docs/CONTINUITY.md` +
> `docs/CAPABILITY-AUDIT.md`. Kept for provenance; not authoritative.

# Remaining Work — 2026-06-02

> Priority queue for the next session. 완료 항목은 제거하되 어떤 작업이
> 어디서 land됐는지 짧게 기록한다. 상세 구현은 `git log` 참조.

---

## 0. Snapshot

- **Branch**: `main` (origin/main 동기화 완료)
- **HEAD**: `ae3d159 R1' ckg: concurrency_impact (G7) + LLM excision + go-stablenet smokes (#12)`
- **Schema**: 1.15 (`NodePolicy` + `NodeSecurityPattern` 슬롯 — P1 #4/#5)
- **Public surface (R1' C2)**:
  `pkg/store` · `pkg/mcphandlers` · `pkg/bm25` · `pkg/impact` · `pkg/evidence`
  · `pkg/smartctx` · `pkg/concurrency` (G7/S1, NEW) · `pkg/policy` · `pkg/security` · `pkg/types`
- **Binary 상태**: production binary LLM-free (`anthropic-sdk-go`, `cli-wrapper`
  + 13개 transitive 제거 — `nm` symbol grep 0건 검증됨)
- **Eval baseline (LLM-free)**: 14/14 R=1.00 P=0.93 F1=0.96 (`eval/baseline/retrieval.json`)
- **Eval Stage B (LLM-driven, 2026-05-29 측정)**: α=0.399 / β=0.441 / γ=0.364 / δ=0.335
  — δ가 β의 5% prompt로 76% score 달성. 측정 후 P0 #3 (CandidateLimit 30→100)
  + P2 #7 (2-hop expand) land됨 → **재측정 미실시**.
- **Evaluation graphs**: `$EVAL_DB_ROOT/stablenet-<short-sha>/` —
  `0bf2f4d1`, `319b84d1`, `940e9f28`, `98f05c2a` 4개 commit 스냅샷 + HEAD

---

## 1. R1' refactor — ckg 측 완료 (PR #12, 2026-06-02)

`coding-agent/docs/r1-refactor/{00-system-contract.md, 01-ckg-refactor.md,
plans/01-ckg-plan.md}` 명세에 따라 다음을 land:

| Plan Step | 결과 | 커밋 |
|---|---|---|
| Step 1 Baseline | (묵시적) | — |
| Step 2 G1 Score contract (runtime 가드) | ✅ | gsn_query_smoke 내 Score ∈ [0,1] + non-increasing assert |
| Step 3 G2 bm25 external import | ✅ | 기존 `example_external_test.go` |
| Step 4 G5 typed-calls | ✅ | 기존 `uses_type_test.go`가 cover (confirmed) |
| Step 5 G6 channel pair | ✅ | 기존 `concurrency_test.go`가 cover (confirmed) |
| Step 6 `pkg/concurrency.Analyze` | ✅ | G7/S1 본체 — 449 LOC + 5 테이블 테스트 |
| Step 7 MCP wrapper | ✅ | `RegisterConcurrencyImpact` (dev-only convenience) |
| Step 8 `ckg eval` 명령 제거 | ✅ | LLM-driven baseline 제거, eval-retrieval 보존 |
| Step 9-10 LLM clients + scorers 제거 | ✅ | 29 파일 / 5603 LOC |
| Step 11 `go mod tidy` | ✅ | anthropic + cli-wrapper + 13 transitive 드롭 |
| Step 12 go-stablenet smoke | ✅ | M2.a (실측 ground-truth cross-check) + M2.d (`NodePolicy=1 SecurityPattern=1`) |

**M2 Acceptance 4/4 완전 충족**:
- (a) frozen `pkg/` + `ConcurrencyImpact` 컴파일 + 비공실 (실측 PASS)
- (b) `internal/eval/retrieval` CI self-test 통과
- (c) `go.mod`에 `anthropic-sdk-go`/`cli-wrapper` 0건
- (d) `NodePolicy`/`NodeSecurityPattern` count > 0 (gated 실측 검증)

---

## 2. 잔여 작업 (우선순위 순)

### Tier A — spec-level 잔여 (R1' 00 §7)

| 작업 | 위치 | 추정 | 비고 |
|---|---|---|---|
| **dev-only MCP build tag** | `cmd/ckg/mcp.go` | ~30분 | `//go:build dev_mcp` 추가 + `root.go`에서 조건부 등록 (stub 분리). PR #12 commit body 명시: "Remaining: 00 §7 dev-only MCP build tag (spec-level)". |

### Tier B — linter / 위생 sweep (진행 중일 수 있음)

| 작업 | 위치 | 추정 | 비고 |
|---|---|---|---|
| **errcheck `defer ... .Close()` 정리** | persist/sqlite_*.go, buildpipe/* 등 | ~1h | `defer rows.Close()` → `defer func() { _ = rows.Close() }()` 패턴 |
| **modernize sweep 잔여** | distributed.go, declarations.go (Sol), topnodes_e2e_test.go | ~30분 | de Morgan, switch, 타입 추론 |
| **`.gitignore` root `/ckg` 패턴** | `.gitignore` | 1줄 | `make build-no-viewer` 산출물 재발 방지 |
| **viewer Playwright spec ↔ UI 표류 모니터링** | `web/viewer-next/tests/` | 상시 | 5-29 ~ 6-02 사이 누적된 4건 회귀를 2026-06-03 land. UI(`.canvas-legend.collapsed` → `.canvas-legend-trigger`, anchor가 NodeList 클릭에서 분리, FirstTimeOverlay 추가)가 spec 미반영 상태로 머무는 패턴. 신규 UI 변경 시 spec 동반 업데이트 강제할 방법 검토 (lint rule 또는 PR template). |

### Tier C — within-language semantics (R1' 영역 밖, hold/optional)

| ID | Language | Work | 추정 | Status |
|---|---|---|---|---|
| **W-B** | TypeScript | async/await + heritage | ~700 LOC | design resolved (`docs/design/ts-async-await-and-interface.md`); detector pending |
| **W-C** | Solidity | inheritance + interface dispatch + `using For` | ~1100-1200 LOC | design resolved (`docs/design/solidity-inheritance-and-interface-dispatch.md`); detector pending |
| **Stage B 재측정** | — | P0 #3 widening + P2 #7 2-hop 효과 정량 검증 | 수 시간 + API 비용 | 측정 환경 변동 (cli-wrapper 제거) — backend 재설계 필요 |

W-A는 P2 #8 (named-goroutine call edge) commit으로 land됨 (R1' refactor와 별개로 5-29 세션에 완료).

### Tier D — 운영 / 측정 인프라

| 작업 | 추정 | 비고 |
|---|---|---|
| **`make eval-stage-b-baseline-update`** | 5분 | 측정 baseline을 `eval/baseline/stage-b/`에 잠금. **PR #12 후 LLM 측정 인프라 (`ckg eval` 명령)는 제거됨** — Stage B는 이제 agent/session 레이어에서 측정해야 함 (`00 §5`). 즉 R1' 후 backend 재설계 필요. |
| **`ckg watch` partial-cache 정상화** | ~2-4h | `runIncremental` 부분 캐시 비활성 상태 (cold rebuild ~40s fallback). reverse-ref index 추가 필요. `00 §10` 명시. |
| **N=3 multi-shot 측정 기본값** | 30분 | LLM 비결정성 안정화. (Stage B 측정 인프라 재설계 시 함께) |

---

## 3. R1' refactor — 다른 repo 작업 (ckg 영역 밖)

ckg(`01`)는 PR #12로 closed. 나머지 4개 repo의 작업이 진행되어야 R1' 통합
완료. 의존 순서 (`coding-agent/docs/r1-refactor/00-system-contract.md §6`):

| Order | Doc | Repo | 작업 골자 | Status |
|---|---|---|---|---|
| 1 | `01-ckg-refactor` | code-knowledge-graph (this) | G7 + LLM excision + smokes | ✅ **완료 (PR #12)** |
| 2 | `02-ckv-refactor` | code-knowledge-vector | `pkg/embed/ollama` 승격 (bge-m3), glossary populate, judge 제거 | ⏳ pending |
| 3 | `03-cks-refactor` | code-knowledge-system | `pkg/ckv` + ckg in-process import, 서브프로세스 proxy 제거, C1 surface lock | ⏳ pending |
| 4 | `04-chainbench-refactor` | chainbench | tool 이름 정규화, `gstable` hardcoding 제거 | ⏳ pending |
| 5 | `05-coding-agent-refactor` | coding-agent | shim 제거, `.mcp.json` cks 직접 연결, 모델 ID 수정, stablenet-context skill 폐기 | ⏳ pending |

ckg는 row 1이므로 더 이상 ckg 영역에서 R1' 작업 없음. cks(`03`)가 row 1 land
를 in-process로 import할 차례.

---

## 4. ckg 단독 후속 검토 항목 (R1' 영역 밖)

다른 repo 작업이 진행되는 동안 ckg에서 자체 개선이 가능한 항목:

| 항목 | 시간 | 비고 |
|---|---|---|
| **W-B / W-C detector** | 각 4-6h | 위 Tier C 참조 |
| **`G4` const/var 노드 활용** | ~1-2h | 노드는 emit됨. 다음 단계는 R1' 00 §4의 "config-as-code domain values" 추출에 cks 측이 사용. ckg 측은 추가 작업 없음 (확인됨). |
| **PR breadcrumb 시간 슬라이싱 cks 통합** | — | `Reader.GetNodePRs(nodeID, cutoff)` 이미 wire됨. cks가 in-process 호출하는 패턴 land 시점 별도. |
| **Postgres 백엔드 검증** | ~1-2h | `internal/persist/postgres_*` 구현됨. 단 R1' contract 측면에선 옵션. CI smoke 또는 explicit pin 권고. |

---

## 5. Evaluation DB 관리 절차

```bash
# .env.local 필요 (gitignored):
#   STABLENET_SRC = <go-stablenet 절대경로>
#   EVAL_DB_ROOT  = <evaluation DB 저장 경로>

# HEAD graph 생성 (이미 존재하면 skip)
make eval-build-dbs

# 과거 커밋 graph 함께 생성
make eval-build-dbs AT_COMMITS="0bf2f4d1bfeb 319b84d113c5 940e9f281edb 98f05c2a0c16"

# 강제 재빌드
make eval-build-dbs FORCE=--force

# Smoke 실행 (R1' M2 검증)
CKG_GSN_GRAPH=$EVAL_DB_ROOT/stablenet-0bf2f4d1bfeb \
  go test ./pkg/concurrency/ -run GoStablenetSmoke -v
CKG_GSN_GRAPH=$EVAL_DB_ROOT/stablenet-0bf2f4d1bfeb \
  go test ./pkg/store/ -run GoStablenetQuerySmoke -v
CKG_GSN_SRC=$STABLENET_SRC \
  go test ./internal/buildpipe/ -run GoStablenetPolicySecurityBuild -v
```

`docs/HANDOFF-2026-05-29.md` §2 — 새 머신 부트스트랩 절차.

---

## 6. Cross-links

| Topic | Doc |
|---|---|
| 새 머신 부트스트랩 | `docs/HANDOFF-2026-05-29.md` |
| 청사진 + 코드 매핑 (구) | `docs/PROJECT-BLUEPRINT-ALIGNMENT.md` (P0/P1/P2/P3 — 청사진 진행 기록) |
| R1' contract (SSoT) | `coding-agent/docs/r1-refactor/00-system-contract.md` |
| R1' ckg refactor | `coding-agent/docs/r1-refactor/01-ckg-refactor.md` + `plans/01-ckg-plan.md` |
| Cold-start guide | `docs/CONTINUITY.md` |
| Project overview | `docs/PROJECT-OVERVIEW.md` |
| Capability audit | `docs/CAPABILITY-AUDIT.md` |
| Schema reference | `docs/SCHEMA.md` |
| Eval trajectory | `docs/eval-trajectory.md` |
| Within-lang semantics design | `docs/NEXT-CANDIDATES-WITHIN-LANG-SEMANTICS.md` + `docs/design/*.md` |
| Self-verification commands | `docs/SELF-VERIFICATION.md` |

---

## 7. 정리 메모 (이번 문서 갱신 시점)

- 9개 outdated 파일을 `docs/archive/`로 이동
  (PERF-BASELINE / SESSION-HANDOFF 2건 / NEXT-CANDIDATES-2026-05-10 /
  ckg5-depth-sweep 2건 / stale-git-lock-analysis / cks-dogfood-followups 2건).
- 이번 문서 기준 `docs/` 루트는 22개 활성 .md.
- `CODE-AUDIT-2026-05-29.md`, `STAGE-B-EVAL-RESULTS-2026-05-29.md`는
  gitignored — 측정 결과 일회성 보고서, archive 대상 아님.
