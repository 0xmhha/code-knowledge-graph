# CKG 남은 작업 — 문서 vs 코드 대조 (2026-07-10)

> Tier 3 (dated snapshot). 상태 문서(`CONTINUITY`, `CAPABILITY-AUDIT`,
> coordination)를 **코드+git에 대조**해 실제 미완료·오류만 추린 목록. "완료로
> 보이지만 문서가 stale"인 것과 "실제 열린 항목"을 구분한다. 근거는 `file:line`으로
> 인용 — 다음 세션이 재검증 없이 신뢰할 수 있게. Ground truth = code + git.

## 요약

이번 대조의 핵심 발견: **`CAPABILITY-AUDIT.md`의 P0 3건은 이미 구현 완료** —
문서만 stale이다(방치하면 다음 작업자가 중복 구현). 실제 CKG 액션은 소수다.

## A. 문서 오류 (stale) — ✅ 정정 완료 (2026-07-10)

문서가 "미구현/P0"라 주장했으나 코드엔 이미 존재. A1–A5 모두 정정함:
`CAPABILITY-AUDIT.md` §3.1 표 + §3.2 P0 3행 → Done, §4.1 baseline/결론 갱신;
`CONTINUITY.md` §4 dependency map + §5 queue 현행화. (아래는 정정 근거 기록.)

| # | 문서 위치 | 문서 주장 | 코드 실제 (근거) | 조치 |
|---|---|---|---|---|
| A1 | `CAPABILITY-AUDIT.md:111` | MCP `search_text` AND/OR mode **미노출 (P0)** | ✅ 구현됨. `mode="or"\|"and"` — `pkg/mcphandlers/handlers.go:176,185`; AND 필터 `internal/persist/sqlite_fts.go:79-82` `filterHitsByAllTokens` (postgres도 `postgres_store.go:1131`) | P0 → **Done** 표기 |
| A2 | `CAPABILITY-AUDIT.md:112` | `pkg/store` AND/OR API 없음 **(P0)** | ✅ 노출됨. `SearchFTSOptions{Mode}` (`pkg/store/store.go:53-55` → `persist.SearchFTSOptions`), `Reader.SearchWithOpts` | P0 → **Done** |
| A3 | `CAPABILITY-AUDIT.md:113` | multi-keyword AND/OR fixtures **absent (P0)** | ✅ 존재. `eval/retrieval/R06`(or)/`R07`(three-token-and)/`R10`(strict-go)/`R14` | P0 → **Done** |
| A4 | `CAPABILITY-AUDIT.md:134` | keyword search 100% precision **"not yet met"** | A1–A3 근거가 무효 → 결론도 stale | 재평가 문구 수정 |
| A5 | `CONTINUITY.md:69-93` | Next-action 큐(ckg-NEW-5/8/9 등 오래된 P0) | 상당수 완료·무관, 현 상태와 괴리 | 큐에서 완료분 제거·현행화 |

## B. 실제 미완료 (코드로 확인된 열린 항목)

| # | 항목 | 근거 | 성격 | 우선순위 |
|---|---|---|---|---|
| B1 | **로컬 미push 커밋 `92b937e`** (retire-ckg-node-id 포인터) | `git rev-list HEAD...origin/main` = ahead 1 | 즉시 push | **높음** |
| B2 | `canonical_id` 커버리지 확대 (`goCanonicalID`, `internal/parse/golang/declarations.go:376`) | `retire-ckg-node-id.md` 체크리스트 | 선택·독립 (은퇴 필수 아님; 빈 canonical_id 축소로 조인 적중률↑) | 선택 |
| B3 | slot-only 미방출 `awaits`(W-B) / `overrides`(W-C) | `CAPABILITY-AUDIT.md:80-81` (❌ slot) | 예약 슬롯, 별도 detector Phase | defer |

## C. 외부 세션 소관 (CKG 아님 — 추적만)

| # | 항목 | 소관 |
|---|---|---|
| C1 | pr-77-2 정본 그래프에 재정렬 + 매칭률 실측 | CKV |
| C2 | "~23% recall" 측정 출처 회신 (D-5) | coding-agent |
| C3 | `ckg_node_id` 은퇴 마스터 플랜 진행 | cks (마스터: `cks docs/retire-ckg-node-id.md`) |

## D. 검증된 정상 (오류 아님 — 참고)

- **schema 1.23**: `internal/buildpipe/cache.go` = `docs/SCHEMA.md` 일치.
- ADR-0002(결정적 빌드, `isTestVariantPkg` ×3) / ADR-0003(Postgres deprecate 표기) / 통합 계약 테스트(`canonical_integration_test.go`) / 필터(`eval/stablenet/stablenet-files-with-tests.json`) 전부 온존.
- `go build ./...` green.
- 세 repo(ckg/ckv/cks) canonical_id 중심 정합 — cks `e456698`가 join을 contract.Hit까지 관통.

## 권장 실행 순서

1. **B1** — 미push 커밋 push (즉시, 리스크 0).
2. **A1–A5** — `CAPABILITY-AUDIT.md` P0 3건 Done 처리 + `CONTINUITY.md` 큐 현행화
   (가장 시급: stale P0가 중복 구현을 유발할 위험).
3. **B2** — 원하면 canonical_id 커버리지 확대 (선택, 다운스트림 조인 이득).
4. B3/C1–C3는 defer 또는 타 세션 대기.
