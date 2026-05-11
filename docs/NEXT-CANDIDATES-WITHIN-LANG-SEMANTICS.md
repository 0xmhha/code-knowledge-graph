# Next Candidates — Within-Language Semantic Gaps

> 다른 후보 인덱스(`docs/NEXT-CANDIDATES-2026-05-10.md`, schema 1.9 W 시리즈)와
> **독립된 dimension**: 언어별 내부 의미론(within-language semantics)의
> 미커버 영역을 묶어둔다. 진행중인 cross-language interop (schema 1.9 W1~W4)
> 와 충돌하지 않으므로 병렬 dispatch 가능.
>
> **작성**: 2026-05-11. **상태**: 설계 문서 3종 ready, 사용자 결정 대기.

---

## §0. 한 줄 요약

| ID | 언어 | 주제 | 설계 문서 | 사이즈 | 우선순위 |
|----|------|------|-----------|--------|---------|
| **W-A** | Go | Cross-function lock propagation (D1) | [`design/go-cross-function-lock-propagation.md`](design/go-cross-function-lock-propagation.md) | S (~200 LOC) | P1 |
| **W-B** | TypeScript | async/await + heritage (interface/extends/implements) | [`design/ts-async-await-and-interface.md`](design/ts-async-await-and-interface.md) | M (~700 LOC, schema bump) | **P0** |
| **W-C** | Solidity | Inheritance + interface dispatch | [`design/solidity-inheritance-and-interface-dispatch.md`](design/solidity-inheritance-and-interface-dispatch.md) | M (~900 LOC, schema bump) | **P0** |

---

## §1. 왜 이 dimension 인가

| 차원 | 기존 작업 | 본 인덱스 |
|------|----------|----------|
| Cross-language interop | schema 1.9 W1~W4 (진행중) | — |
| Hunk-graph / temporal | schema 1.8 (완료) | — |
| **Within-language semantics** | track-c-detector-gap 진단만 | **본 인덱스 — 실행 가능 plan 으로 격상** |

`track-c-detector-gap.md` 는 진단 문서로 P0~P3 우선순위를 enumerate 했으나
**plan 없음**. 본 인덱스의 3개 spec 은 진단을 받아 step-by-step 구현
계획으로 변환한 것.

---

## §2. 진행 권장 순서

### Phase 1 — 결정 합의 (사용자 dispatch)
각 spec 의 `§5 결정 필요 항목` 8~10 개에 답변. 평균 30~60분 / spec.
- W-A: 8 항목
- W-B: 8 항목
- W-C: 10 항목

병렬 진행 가능. 가장 가치 큰 W-C 부터 권장 (Solidity 의 90% 패턴 미캡처).

### Phase 2 — W4-style warm-up (가장 작은 PR)
schema 변경 없는 small wins 먼저:
- W-C 의 W4 (`abstract` / `library` SubKind, ~100 LOC)
- W-A 의 baseline 측정 (PR 아님, 측정만)

### Phase 3 — schema bump
- W-B + W-C 의 schema 1.10 bump 합병 PR (Q8 결정 후)
- enums.go 변경은 prompt cache 무효화 비용 — 1회 batch.

### Phase 4 — 본 구현
- W-A (의존성 없음)
- W-B W1+W2 (병렬)
- W-C W1, W2, W3 순차

### Phase 5 — 측정 + 핸드오프
- 각 spec 의 `§4 측정` 단계
- 새 `SESSION-HANDOFF-<date>.md` 의 §6 후보 등재

---

## §3. 다른 세션과의 충돌 회피

다음 파일은 schema 1.9 / perf 작업이 활성 — **건드리지 않음**:

| 파일 | 충돌 가능 작업 |
|------|---------------|
| `docs/design/schema-1.9-spec.md` | cross-language interop |
| `docs/design/hunk-graph.md` | hunk-graph H 시리즈 |
| `docs/SESSION-HANDOFF-2026-05-10.md` | 이전 세션 핸드오프 (frozen) |
| `docs/NEXT-CANDIDATES-2026-05-10.md` | 이전 후보 (frozen) |
| `pkg/types/enums.go` | (TS/Sol spec 의 schema bump 시 신중 — 동일 위치 동시 수정 위험) |

본 인덱스 + 3개 design doc 은 모두 **신규 파일** — 진행중인 작업과 직교.

---

## §4. 빠른 시작 (다음 세션 cold start)

```bash
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph
git status                                        # working tree clean 확인
git log --oneline -3                              # HEAD 확인 (a729cd2 이후)
go test ./... -count 1 2>&1 | grep -E '^(ok|FAIL)'  # 회귀 baseline

# 본 인덱스 확인
cat docs/NEXT-CANDIDATES-WITHIN-LANG-SEMANTICS.md

# 진행할 spec 선택 후 사용자에게 §5 결정 항목 dispatch
cat docs/design/<선택한-spec>.md
```

---

## §5. 메타: 본 문서들이 만들어진 맥락

- 2026-05-11 사용자 검토: 요구사항 5개(Go/TS/Sol 파싱 + 신뢰도 + 점수 +
  무결성 + graph.db) 진행 상황 점검 → 모두 완료 확인
- 후속 질문: "동시성 미구현 부분 상세" → Go 는 B1 Phase 1~4 완료
  (`enums.go` stale comment 가 오해 유발), TS/Sol 0 emission 확인
- 후속 지시: TS async/interface, Sol inheritance/interface 작업 필요 +
  설계 문서 작성
- 결과: 본 인덱스 + 3개 design doc. 진행중인 schema 1.9 와 직교 dimension.

---

## §6. 참조

- 기반 진단: `docs/design/track-c-detector-gap.md`
- 이전 핸드오프: `docs/SESSION-HANDOFF-2026-05-10.md`
- schema 정의: `pkg/types/enums.go`
- Spec V0.2: `docs/spec-ckg-v0.2.md`
