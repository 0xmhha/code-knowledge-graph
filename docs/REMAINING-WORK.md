# Remaining Work — 2026-05-27

> Priority queue for the next session. Completed items are removed;
> see `git log` for implementation details.

## 0. Snapshot

- **Branch**: `main`
- **Schema**: 1.13 (`search_tokens` camelCase pre-split column)
- **Retrieval baseline**: 14/14 R=1.00 P=0.93 F1=0.96 (eval/baseline/retrieval.json)
- **Eval LLM baseline**: cycle 9, α=0.396 / β=0.746 / γ=0.688 / δ=0.825, halu β/δ 0.000
- **6-axis emission**: 40 edge types live; `awaits` (W-B) and `overrides` (W-C) slot-reserved only
- **Public surface**: pkg/store + pkg/mcphandlers + pkg/types + pkg/bm25 + pkg/smartctx + pkg/evidence + pkg/impact + pkg/hunkmodifies
- **Golden set**: 30 task YAMLs in `eval/stablenet/tasks/` (symbol_set=12, rubric=15, code_patch=3)
- **Stage B harness**: `make eval-stage-b` ready (requires LLM backend)

## 1. Next actions — ckg standalone verification

Stage B evaluation을 실행하여 ckg 단독 성능 baseline을 확보한다.
cks 통합은 ckg 기능 검증 완료 후 진행 (hold).

| ID | Work | Estimate | Notes |
|---|---|---|---|
| **Stage B 첫 실행** | `make eval-stage-b` — 30 task × 4 baselines × go-stablenet graph | ~1-2 h (LLM 호출 시간 포함) | ANTHROPIC_API_KEY 또는 CLIWRAP_AGENT 필요. 결과 → `eval/baseline/stage-b/` 커밋 |
| **EV1 Phase 3** | CI workflow에 eval gate 추가 | ~30 min | `docs/todo-cks-dogfood-followups-2026-05-20.md §F` snippet 적용 |

## 2. Within-language semantics (paused)

ckg 단독 검증 + cks 통합 이후에 필요성이 확인되면 재개.

| ID | Language | Work | Estimate | Status |
|---|---|---|---|---|
| **W-A** | Go | cross-function lock propagation (Stage B DFS) | ~300-400 LOC | `--lock-propagation` flag wired; propagator partial |
| **W-B** | TypeScript | async/await + heritage | ~700 LOC | design resolved; detector pending |
| **W-C** | Solidity | inheritance + interface dispatch + `using For` | ~1100-1200 LOC | design resolved; detector pending |

Resume order: W-B → W-C (TS detector가 작고, Sol detector가 W-B 헬퍼 재사용).

## 3. Deferred — external dependencies

| ID | Why deferred | Re-trigger |
|---|---|---|
| **B-2** cks `go.mod` ckg version bump | cks repo, separate session | ckg 검증 완료 후 |
| **E2** cks workaround removal PR | cks repo, separate session | cks 통합 단계 진입 시 |
| **viewer uncommitted files** | `internal/server/web_assets/index.html` + `web/viewer-next/README.md` — viewer 작업 세션의 잔여 | viewer 세션 재개 시 |

## 4. Cross-links

| Topic | Doc |
|---|---|
| Cold-start guide | `docs/CONTINUITY.md` |
| Project overview | `docs/PROJECT-OVERVIEW.md` |
| Capability audit | `docs/CAPABILITY-AUDIT.md` |
| Schema reference | `docs/SCHEMA.md` |
| HANDOFF tracker | `eval/stablenet/HANDOFF.md` |
| CKS integration plan | `eval/stablenet/CKS-INTEGRATION-2026-05-23.md` |
| Eval trajectory | `docs/eval-trajectory.md` |
| Within-lang semantics design | `docs/NEXT-CANDIDATES-WITHIN-LANG-SEMANTICS.md` + `docs/design/*.md` |
