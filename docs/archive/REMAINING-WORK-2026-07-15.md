# CKG 남은 작업 — 문서 vs 코드 대조 (2026-07-15)

> **ARCHIVED 2026-07-15 — 잔여 없음.** CKG 코드 작업이 모두 완료되어(B1/B2/B3, A1/A2) 이 추적 문서의 목적이 끝났다. 남은 선택/타세션 항목(C1 canonical_id 커버리지 defer, D1 CKV 재정렬, D2 coding-agent D-5, D3 ckv/cks 은퇴)은 `docs/CONTINUITY.md`에 승계. Provenance용 보존.

> Tier 3 (dated snapshot). 상태 문서를 **코드+git에 대조**해 실제 미완료·오류만 추린
> 목록. 근거는 `file:line`/commit으로 인용 — 다음 세션이 재검증 없이 신뢰할 수 있게.
> Ground truth = code + git. **Supersedes `REMAINING-WORK-2026-07-11.md`**: 그 스냅샷의
> B1(Korean/CJK 테스트)·B3(shim 이전)이 이미 코드에 랜딩돼 있었음을 이번 대조로 확인
> (문서가 stale이었음). #55(index-project.sh) 신규 반영.

## 요약

- **07-11 스냅샷의 B1·B3는 사실 이미 완료였다** — 두 항목의 랜딩 커밋이 07-11 문서보다
  먼저 존재(`git merge-base --is-ancestor` 확인). 즉 07-11 문서가 완료 항목을 open으로 잘못 표기.
- **신규**: #55 `scripts/index-project.sh`의 `MAIN_PKG` 모드가 정본 그래프 빌드의 **동적·정밀
  스코핑**을 제공 → 기존 정적 필터(`eval/stablenet/stablenet-files-with-tests.json`)를 대체.
- **B2(Stage B / ckg-NEW-5)도 완료됨(#57, 2026-07-15)** — ckv fixture 미러(`eval/ckv-mirror`,
  12/12) + go-stablenet 실전 코퍼스 키워드 eval(`eval/stablenet-keyword`, 8/8). → **CKG에 남은
  코드 작업 없음.**

## A. 문서 오류 (stale) — ✅ 이번에 확인/정정

| # | 07-11 문서 주장 | 코드 실제 (근거) | 판정 |
|---|---|---|---|
| A1 | **B1** Korean/CJK graceful degradation **미테스트** (P1) | ✅ `TestSearchFTS_KoreanInput_GracefulEmpty` + `TestSearchFTS_KoreanMixed_ExtractsAsciiToken` (`internal/persist/search_mode_test.go:203,236`), 랜딩 `75aeb60` (07-11 문서보다 먼저). 통과 확인 | **완료** |
| A2 | **B3** `internal/mcp`→`pkg/mcphandlers` shim 이전 (T-14b) 열림 | ✅ `85f6705` "remove internal/mcp handler duplication, single-source pkg/mcphandlers (T-14b)". 현재 `internal/mcp/` = `server.go`+`bench.go`만, 중복 핸들러 0 | **완료** |
| A3 | (07-11이 이미 정정) awaits/overrides 방출 | ✅ `internal/parse/typescript/declarations.go`(awaits), `internal/parse/solidity/resolve.go`(overrides) | 완료 확인 |

## B. 실제 미완료 (코드로 확인된 열린 항목)

**없음** — 07-15 스냅샷의 유일한 열린 항목이던 B2(Stage B / ckg-NEW-5)가 #57로 완료됨.

| # | 항목 | 근거 | 판정 |
|---|---|---|---|
| ~~B1~~ | ~~Stage B eval harness 확장 (ckv fixture mirror + 측정)~~ | LLM-free `eval-retrieval` 기반 2단계로 구현: `eval/ckv-mirror`(12 fixture, `make eval-ckv-mirror`, 12/12) + `eval/stablenet-keyword`(8 fixture, `make eval-stablenet-keyword GRAPH=<dir>`, 8/8). #57 | ✅ **완료 (2026-07-15)** |

## C. 선택·설계상 defer (버그 아님)

| # | 항목 | 근거 | 판단 |
|---|---|---|---|
| C1 | `canonical_id` 커버리지 확대 (`goCanonicalID`, `internal/parse/golang/declarations.go:376`) | `retire-ckg-node-id.md`; #53 결정 | **defer 유지** — 빈 canonical_id는 대부분 by-design(promoted/synthetic 메서드, function-local var/const). 건드리면 스키마 bump→published 그래프 re-digest→CKV/coding-agent 파급, 이득 거의 0 |

## D. 외부 세션 소관 (CKG 아님 — 추적만)

| # | 항목 | 소관 | 상태 |
|---|---|---|---|
| D1 | pr-77-2 정본 그래프 재정렬 + graph_digest end-to-end 실증 | CKV | CKV `ckg_node_id` 제거 **코드 완료(07-11)**; CKG가 `graph_digest`(`4be26516…`) 공표 완료 → `ReadCoords` 자동 소비. 실증만 남음 |
| D2 | "~23% recall" 측정 출처 회신 (D-5) | coding-agent | 대기 |
| D3 | `ckg_node_id` 은퇴 (ckv/cks 코드) | ckv/cks | ckv **완료(07-11)** · cks **코드 마감(07-12)** (각 repo retire 문서). CKG 측 변경 불요(닫힘) |

## E. 검증된 정상 (오류 아님 — 참고)

- **schema 1.23**: `internal/buildpipe/cache.go` = `docs/SCHEMA.md` 일치.
- **graph_digest 공표**: `internal/buildpipe/graph_digest.go`; 정본 pr-77-2 digest `4be26516…`
  (2회 cold 재빌드 동일). manifest json + in-db row 양쪽 기록.
- **cold rebuild 원자성**: `graph.db.building` → `os.Rename` (`pipeline.go`).
- **재현 빌드 표준화(#55)**: `scripts/index-project.sh` — `MAIN_PKG`로 바이너리 도달 코드만
  스코핑(`go list -deps`), ADR-0002 staged composition 기반. 동일 입력→동일 그래프.
- `origin/main...HEAD = 0 0` (미push 없음). vet/gofmt/test green.

## 권장 실행 순서

**CKG 코드 잔여 없음** — B2(#57)까지 완료. 남은 항목은 전부 선택(C1) 또는 타 세션(D1–D3):
1. C1(`canonical_id` 커버리지) — defer 유지(by-design + published 그래프 파급).
2. D1(CKV pr-77-2 재정렬·매칭률), D2(coding-agent D-5), D3(ckv/cks 은퇴 코드) — 타 세션 대기/추적.

**CKG 필수/블로킹 작업 없음** — 코드 상태 클린. B1은 eval 서피스 확장(선택적).
