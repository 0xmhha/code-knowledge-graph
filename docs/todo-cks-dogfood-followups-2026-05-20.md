# 작업 투두 — 2026-05-20 정리

> 출처: 2026-05-19 cks dogfood-eval 결과를 반영한 ckg 후속 작업 정리.
> 본 문서는 진행 추적용이며, 항목별 상세 컨텍스트는
> `docs/followups-from-cks-dogfood-2026-05-19.md` 참조.

## 진행 상태

- [x] **A1** `git push origin main` — 사용자가 수동 처리 완료 (2026-05-20)

## B. cks dogfood 후속 (우선순위 順)

| # | 항목 | 영향 | 표면 | 상태 |
|---|---|---|---|---|
| CKG-1 | `SearchFTS`에 BM25 score/rank 반환 | High | 작음 | ✅ `2f89b17` |
| CKG-2 | `SearchFTS`에 native filter (language) | High | 중간 | ✅ `570e5ec` |
| CKG-4 | Symbol lookup `kinds []SymbolKind` 다중 처리 | Mid | 중간 | ✅ `d34a2eb` |
| CKG-3 | Cross-snapshot 정책 결정 | High | 가변 | ⬜ |
| CKG-5 | Traversal depth=2 측정 | Mid | 측정 | ⬜ |
| CKG-6 | `pkg/store.Reader` 공개 surface 정리 | Mid | 작음 | ⬜ |
| CKG-7 | `persist.Manifest` 일부 노출 | Low | 작음 | ⬜ |

### CKG-1 세부 — `SearchFTS` 점수 반환 ✅ `2f89b17`

**구현 요약:**
- `internal/persist/search_hit.go` — `SearchHit{Node, Score, RawScore}` 타입 + `normalizeSearchHits` 헬퍼
- `StoreReader.SearchFTS` 시그니처 `[]SearchHit` 로 변경
- SQLite: `-bm25(nodes_fts)` (sign-flip), `ORDER BY raw_score DESC`
- PG: `ts_rank(search_vector, plainto_tsquery)`, `ORDER BY raw_score DESC`
- `Search()` 시그니처 유지 — 내부 `nodesFromHits` 어댑터로 호환
- `llmSafeStoreReader.SearchFTS` — AMBIGUOUS 필터 후 점수 보존
- 회귀 테스트 3종: ScoreMonotonic / ScoreRangeNormalized / SingleHitScoreOne

**다운스트림 액션 (E2 후속):** cks `internal/ckgclient/real.go:149-155` 가짜 점수 제거 가능.


**현황:**
- `StoreReader.SearchFTS(q, limit) ([]types.Node, error)` — `internal/persist/store_interface.go:81`
- 구현 3곳: `sqlite.go:634`, `postgres_store.go:926`, `mcp/h3_filter.go:89` (LLM-safe wrapper)
- 호출자(non-test): `mcp/h3_filter.go:90` 1곳
- 테스트 mock 1곳: `postgres_exporter_test.go:65`

**다운스트림 통증:**
- cks `internal/ckgclient/real.go:149-155`이 `1 - i/(N+1)` 가짜 점수 생성
- 강한 단일 매치와 약한 5개 매치를 구분 불가 → cks 측 `fix: rerank by max(hit.Score)` 워크어라운드

**Acceptance:**
- 점수가 호출자에서 의미 있게 비교 가능 (단조성 보장)
- 기존 `SearchFTS` 호출자 무파괴 또는 명시적 마이그레이션 경로
- sqlite + postgres 양쪽 동작 일치
- 회귀 테스트: 동일 쿼리에서 강한 매치가 약한 매치보다 높은 점수

### CKG-2 세부 — native filter pushdown ✅ `570e5ec`

**구현 요약:**
- `SearchFTSOptions{Language string}` 추가 (`internal/persist/search_hit.go`)
- `StoreReader.SearchFTS(q, limit, opts)` 시그니처 확장
- SQLite/PostgreSQL 모두 동적 SQL — `opts.Language != ""` 일 때만 WHERE 추가
- `Search()` 어댑터는 `SearchFTSOptions{}` 전달, 기존 호출자 7곳 무영향
- path glob은 client-side 유지 (CKG-2 follow-up 문서 권장)
- 회귀 테스트 3종: LanguagePushdown / LanguageEmptyMatchesAll / LanguageNoMatch

**다운스트림 액션 (E2 후속):** cks `internal/ckgclient/real.go`의 `FilterOverfetchRatio=3` over-fetch + client-side language filter 제거 가능. path glob은 그대로 유지.


### CKG-4 세부 — multi-kind symbol lookup ✅ `d34a2eb`

**구현 요약:**
- `FindSymbolOptions{Language, Kinds []types.NodeType}` 추가
- `FindSymbol(name, exact, opts)`로 시그니처 재배치 — `lang`을 Options로 이동
- SQLite: 기존 `placeholders(n)` 헬퍼 재사용해 `type IN (?, ?, ...)` 조립
- PostgreSQL: `$N` placeholder 인라인 생성
- 빈 `Kinds` → 원래 WHERE plan 유지 (planner cost regression 없음)
- 회귀 테스트 4종: KindsSingle / KindsMultiple / KindsEmptyMatchesAll / KindsNoMatch

**다운스트림 액션 (E2 후속):** cks Stage 2 `arch_explain` 의도에서 N round-trip → 1 query로 단축 가능. dedupe는 Citation key 기준 클라이언트 측 유지.


### CKG-3 / 5 / 6 / 7

- **CKG-3**: cross-snapshot 로드맵 결정 먼저 → 문서화 or 구현
- **CKG-5**: depth=2 측정 (recall vs latency)
- **CKG-6**: `pkg/store.Reader`가 공식 surface면 internal alias 위로 끌어올림
- **CKG-7**: `Manifest` 핵심 필드만 `pkg/store`로 재노출

## C. W-C 회귀 가드 시리즈

- [ ] **C1** 다음 invariant 후보 발굴 (현재 V14~V16까지 누적)

## D. 잔여 정리

- [ ] **D1** viewer ESLint 4 warnings 정리 (`App.tsx:216`, `GraphCanvas.tsx:219`, `TicketIndex.tsx:144`, `usePersistedState.ts:89`)
- [ ] **D2** gofmt drift 재발 방지 (pre-commit hook 또는 CI gate)
- [ ] **D3** Makefile에 `fmt` 타겟 추가 검토

## E. 크로스 레포 동조

- [ ] **E1** ckv 리포 companion `followups-from-cks-dogfood-2026-05-19.md` 작성
- [ ] **E2** cks 측 워크어라운드 제거 PR (CKG-1, CKG-2 구현 후)
