# Remaining Work — 2026-05-29

> Priority queue for the next session. Completed items are removed;
> see `git log` for implementation details.

## 0. Snapshot

- **Branch**: `main` (8 commits ahead of origin)
- **Schema**: 1.13 (`search_tokens` camelCase pre-split column)
- **Retrieval baseline**: 14/14 R=1.00 P=0.93 F1=0.96 (eval/baseline/retrieval.json)
- **Stage B 결과 (2026-05-29)**: α=0.399 / β=0.441 / γ=0.364 / δ=0.335 — δ가 β의 5% prompt로 76% score 달성. 상세 보고서는 `make eval-stage-b` 실행 후 로컬에 생성됨 (gitignored)
- **6-axis emission**: 40 edge types live; `awaits` (W-B) and `overrides` (W-C) slot-reserved only
- **Public surface**: pkg/store + pkg/mcphandlers + pkg/types + pkg/bm25 + pkg/smartctx + pkg/evidence + pkg/impact + pkg/hunkmodifies
- **Golden set**: 30 task YAMLs in `eval/stablenet/tasks/` (symbol_set=12, rubric=15, code_patch=3)
- **Evaluation graphs**: `~/Work/github/tools/go-stable-code/stablenet-{HEAD, 319b84d, 98f05c2a, 0bf2f4d1}/`
- **LLM backend**: cli-wrapper v0.4.6 (local replace via go.mod)

## 1. ckg standalone verification — 완료

Stage B 측정으로 ckg 핵심 가치 명제가 정량 입증됨:

> **δ baseline이 β 대비 1/19 크기 prompt로 76% score + 더 높은 cite precision 달성**

cks 통합은 여전히 hold 상태. 단독 검증은 끝났으나 추가 개선 여지 있음.

## 2. Next actions — δ/γ 성능 개선

Stage B에서 드러난 약점을 좁혀 "γ/δ를 압도적으로" 만든다.

### Tier 1 — 세션 마무리

| 작업 | 시간 | 비고 |
|---|---|---|
| **세션 결과 커밋** | 5분 | Stage B 보고서 + go.mod replace + 본 문서 |
| **origin/main push** | 즉시 | 8 commits ahead 동기화 |
| **`make eval-stage-b-baseline-update`** | 5분 | 측정 결과를 `eval/baseline/stage-b/`에 baseline으로 잠금 |

### Tier 2 — γ/δ 성능 개선 (Stage B 결과 기반)

| 작업 | 현재 → 목표 | 시간 | 접근 |
|---|---|---|---|
| **γ score 개선** | 0.364 → 0.4+ | ~3-4h | system prompt 개선, tool 선택 가이드 강화 |
| **γ latency 단축** | 137s → 100s 이하 | ~2-3h | parallel tool calls 지원 또는 `gammaMaxTurns` 8 → 5 |
| **δ score 개선** | 0.335 → β(0.441) 근접 | ~4-6h | `smartContext` 후보 30 → 100 확대, 2-hop 옵션, task type별 packing 전략 |
| **δ cite precision 유지하며 score↑** | 0.817 유지하며 score+ | 위와 함께 | `MaxBodies` 5 → 10, signature/doc 비율 조정 |

### Tier 3 — 측정 인프라

| 작업 | 시간 | 비고 |
|---|---|---|
| **N=3 multi-shot default** | 30분 + 측정시간×3 | LLM 비결정성 안정화. `STAGE_B_NRUNS` 기본값 변경 |
| **rate limit 회피 batch + delay** | 1-2h | task별 sleep, 또는 batch당 quota 추적. 11시간 연속 호출에서 rate limit 걸린 경험 반영 |
| **β 큰 코퍼스 적응형 cap** | 1-2h | `betaSeedTopK`를 코퍼스 크기에 따라 동적 조정 |
| **EV1 Phase 3 CI gate** | 30분 | `docs/todo-cks-dogfood-followups-2026-05-20.md §F` snippet 적용 |

## 3. Within-language semantics (paused — hold)

cks 통합 필요성이 확인되기 전까지 보류.

| ID | Language | Work | Estimate | Status |
|---|---|---|---|---|
| **W-A** | Go | cross-function lock propagation (Stage B DFS) | ~300-400 LOC | `--lock-propagation` flag wired; propagator partial |
| **W-B** | TypeScript | async/await + heritage | ~700 LOC | design resolved; detector pending |
| **W-C** | Solidity | inheritance + interface dispatch + `using For` | ~1100-1200 LOC | design resolved; detector pending |

Resume order: W-B → W-C (TS detector가 작고, Sol detector가 W-B 헬퍼 재사용).

## 4. Deferred — cks integration (hold)

ckg 기능 검증 완료 후 진행 (사용자 명시).

| ID | 설명 | Re-trigger |
|---|---|---|
| **B-2** cks `go.mod` ckg version bump | cks repo, separate session | ckg push 완료 후 |
| **E2** cks workaround removal PR | cks repo, separate session | cks 통합 단계 진입 시 |
| **viewer uncommitted files** | `internal/server/web_assets/index.html` + `web/viewer-next/README.md` — viewer 작업 세션의 잔여 | viewer 세션 재개 시 |

## 5. Evaluation DB 관리

```bash
# HEAD graph 생성 (이미 존재하면 skip)
make eval-build-dbs

# 과거 커밋 graph도 함께 생성
make eval-build-dbs AT_COMMITS="319b84d 0bf2f4d 98f05c2a"

# 강제 재빌드
make eval-build-dbs FORCE=--force

# Stage B 평가 실행 (cli-wrapper 백엔드)
CLIWRAP_AGENT=~/.gvm/.../cliwrap-agent make eval-stage-b STAGE_B_BASELINES=alpha,beta,gamma,delta
```

Graph DB 저장 위치: `~/Work/github/tools/go-stable-code/stablenet-<short-sha>/`

## 6. Cross-links

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
