# CKG 남은 작업 — 문서 vs 코드 대조 (2026-07-11)

> Tier 3 (dated snapshot). 상태 문서(`CONTINUITY`, `CAPABILITY-AUDIT`,
> coordination)를 **코드+git에 대조**해 실제 미완료·오류만 추린 목록. "완료로
> 보이지만 문서가 stale"인 것과 "실제 열린 항목"을 구분한다. 근거는 `file:line`으로
> 인용 — 다음 세션이 재검증 없이 신뢰할 수 있게. Ground truth = code + git.
> **Supersedes `REMAINING-WORK-2026-07-10.md`** (rename). 그 스냅샷의 B1(미push)·
> B3(slot)이 이번 대조로 해소/정정됐다.

## 요약

- 2026-07-10 스냅샷의 **열린 항목은 사실상 전부 완료**다: B1(미push 커밋)은 push됐고
  (#53), coordination Q1/Q2/Q6도 #53에서 구현됐다.
- **신규 발견**: `awaits`(W-B)·`overrides`(W-C)는 "미방출 slot"이 아니라 **이미 방출**된다
  (2026-05-11/05-18 랜딩). 세 문서가 이를 "deferred"로 잘못 표기 → 이번에 정정.
- CKG에 남은 **실제 코드 작업은 소수이고 전부 P1 이하 선택/외부**다.

## A. 문서 오류 (stale) — ✅ 정정 완료 (2026-07-11)

| # | 문서 위치 | 문서 주장 | 코드 실제 (근거) | 조치 |
|---|---|---|---|---|
| A1 | `CAPABILITY-AUDIT.md:80` | `awaits` **not emitted (❌ slot)** | ✅ 방출. `emitAwaitPoint`(`internal/parse/typescript/async.go:120`) wired at `declarations.go:92`; 랜딩 `0866ef0`(05-11). 빌드 그래프에 `awaits` 엣지 실측 | ❌ slot → **✅ emitted** |
| A2 | `CAPABILITY-AUDIT.md:81` | `overrides` **not emitted (❌ slot)** | ✅ 방출. `internal/parse/solidity/overrides.go:200` + `resolve.go:1173`; 랜딩 `be80e3d`(05-18). 빌드 그래프에 `overrides` 엣지 실측 | ❌ slot → **✅ emitted** |
| A3 | `CONTINUITY.md:85` | "Deferred slots: awaits/overrides detectors" | A1/A2로 무효 | queue에서 제거 |
| A4 | `coordination-…-07-10.md:12-17` | 판정표 Q1 🔴 / Q2 🟡 / Q6 🟡 | 전부 #53에서 완료(§"후속 … ✅") | 표에 "현재=완료" 열 추가 |

## B. 실제 미완료 (코드로 확인된 열린 항목)

| # | 항목 | 근거 | 성격 | 우선순위 |
|---|---|---|---|---|
| B1 | Korean/CJK 쿼리 graceful degradation 미테스트 | `CAPABILITY-AUDIT.md:114` (ckg-NEW-1). `Search()`가 non-ASCII를 `SearchSubstr` LIKE 경로로 라우팅하나 혼합 입력 미검증 | test-only (~80 LOC, 동작 변경 없음, panic 발견 시만) | P1 |
| B2 | Stage B harness (12 ckv-fixture 태스크) | `CAPABILITY-AUDIT.md:158` (ckg-NEW-5/8). YAML 저작 + harness 배선 후 14-task×4-baseline 측정 | eval·LLM 의존 | P0(eval 서피스) |
| B3 | `internal/mcp` → `pkg/mcphandlers` shim 이전 (T-14b) | `CAPABILITY-AUDIT.md:169` | 정리(cleanup) | 낮음 |

## C. 선택·설계상 defer (버그 아님)

| # | 항목 | 근거 | 판단 |
|---|---|---|---|
| C1 | `canonical_id` 커버리지 확대 (`goCanonicalID`, `internal/parse/golang/declarations.go:376`) | `retire-ckg-node-id.md` 체크리스트; #53 결정 | **defer 유지** — 빈 canonical_id는 대부분 by-design(promoted/synthetic 메서드, function-local var/const). 건드리면 스키마 bump→published 그래프 re-sha→CKV/coding-agent 파급, 이득 거의 0 |

## D. 외부 세션 소관 (CKG 아님 — 추적만)

| # | 항목 | 소관 | 트리거 |
|---|---|---|---|
| D1 | pr-77-2 정본 그래프 재정렬 + 매칭률 실측 | CKV | CKG가 `graph_digest` 공표 완료(#53) → `ckgalign.ReadCoords`가 자동 소비, `CheckAlignment()` 강화 |
| D2 | "~23% recall" 측정 출처 회신 (D-5) | coding-agent | — |
| D3 | `ckg_node_id` 은퇴 마스터 플랜 | cks (`cks docs/retire-ckg-node-id.md`) | CKG 측은 코드 변경 불요(닫힘) |

## E. 검증된 정상 (오류 아님 — 참고)

- **schema 1.23**: `internal/buildpipe/cache.go` = `docs/SCHEMA.md` 일치.
- **graph_digest 공표**: `internal/buildpipe/graph_digest.go`; 정본 pr-77-2 digest
  `4be26516…`(2회 cold 재빌드 동일). manifest(json + in-db row) 양쪽 기록.
- **cold rebuild 원자성**: `graph.db.building` 빌드 후 `os.Rename` (`pipeline.go`).
- `origin/main...HEAD = 0 0` (미push 없음).
- CI green: 2026-07-11 기준 govulncheck(Go 1.25.12)·smoke·audit·lint 전부 통과
  (PR #48/#49/#51/#52/#54 머지).

## 권장 실행 순서

1. **B1(Korean/CJK 테스트)** — 원하면 착수(P1, test-only, 리스크 낮음).
2. **B2(Stage B harness)** — eval·LLM 필요; 우선순위 재확인 후 착수.
3. **B3(shim 이전)** — 정리 여유 시.
4. C1/D1–D3는 defer 또는 타 세션 대기.
