# CKG 남은 작업 — 문서 vs 코드 대조 (2026-07-15)

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
- **CKG에 남은 실제 코드 작업은 B2(Stage B eval harness 확장) 하나뿐**이고, 그것도 eval·LLM
  의존 서피스 확장(필수 블로커 아님).

## A. 문서 오류 (stale) — ✅ 이번에 확인/정정

| # | 07-11 문서 주장 | 코드 실제 (근거) | 판정 |
|---|---|---|---|
| A1 | **B1** Korean/CJK graceful degradation **미테스트** (P1) | ✅ `TestSearchFTS_KoreanInput_GracefulEmpty` + `TestSearchFTS_KoreanMixed_ExtractsAsciiToken` (`internal/persist/search_mode_test.go:203,236`), 랜딩 `75aeb60` (07-11 문서보다 먼저). 통과 확인 | **완료** |
| A2 | **B3** `internal/mcp`→`pkg/mcphandlers` shim 이전 (T-14b) 열림 | ✅ `85f6705` "remove internal/mcp handler duplication, single-source pkg/mcphandlers (T-14b)". 현재 `internal/mcp/` = `server.go`+`bench.go`만, 중복 핸들러 0 | **완료** |
| A3 | (07-11이 이미 정정) awaits/overrides 방출 | ✅ `internal/parse/typescript/declarations.go`(awaits), `internal/parse/solidity/resolve.go`(overrides) | 완료 확인 |

## B. 실제 미완료 (코드로 확인된 열린 항목)

| # | 항목 | 근거 | 성격 | 우선순위 |
|---|---|---|---|---|
| B1 | **Stage B eval harness 확장** — ckv fixture 12개의 ckg-side mirror(`ckg-NEW-5/8`) + 14-task×4-baseline 측정 | `eval/stablenet/CKS-INTEGRATION-2026-05-23.md:27,62`. harness(`make eval-stage-b`, `Makefile:219`) + 일반 stable-net task 30개(`eval/stablenet/tasks/T01–T30`)는 존재. 단 **ckv-mirror 12개 태스크 미작성**(`grep ckv|mirror tasks/` = 0) + 런타임 측정(LLM 의존) 미실시 | eval 서피스 확장 (YAML 저작 + LLM 측정) | P1 (블로커 아님) |

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

1. **B1(Stage B harness 확장)** — 유일한 코드성 잔여. eval·LLM 필요 → 우선순위·리소스 확인 후 착수.
2. C1/D1–D3는 defer 또는 타 세션 대기.

**CKG 필수/블로킹 작업 없음** — 코드 상태 클린. B1은 eval 서피스 확장(선택적).
