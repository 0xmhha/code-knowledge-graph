# CKG → CKV 협의 회신 — canonical_id join · 2026-06-29

> Tier 3 (dated snapshot). CKG 세션이 CKV의
> `code-knowledge-vector/docs/coordination-prompts-2026-06-29.md` §1(→ CKG)에
> 회신한 내용의 CKG 측 사본. 권위: `docs/adr/0001-canonical-symbol-id.md`(why),
> `docs/SCHEMA.md`(현재 schema **1.22**), 코드+git(현재 사실). CKV 문서에는
> §1-R로 동일 내용이 반영됐고, 그쪽의 `>= 1.16` 게이팅 오류도 함께 정정했다.

## 핵심 정정 (CKV가 반드시 반영해야 할 사실 오류)

CKV 문서가 canonical_id population 게이트를 **`schema >= 1.16`**으로 적었으나
**틀렸다.** 컬럼은 SQL schema 1.16에 추가되지만 *값*은 cache `SchemaVersion`
**>= 1.19**로 재빌드한 그래프에서만 채워진다 (`internal/buildpipe/cache.go`:
"pre-1.19 DBs carry it empty, so the cache-key flip forces a cold rebuild to
repopulate canonical_id graph-wide"). CKV의 PRAGMA 컬럼-존재 probe는 1.16~1.18
그래프를 통과시키지만 값은 NULL → 그 NULL을 join key로 쓰면 silent 실패.
**올바른 게이트**: 매니페스트 `schema_version >= 1.19`, 또는 가장 안전하게 현재
스키마(**1.22**)로 reindex된 그래프에만 정렬. 컬럼 probe는 필요조건일 뿐.

## Q1 — 스키마 / population
- 현재 cache `SchemaVersion` = **1.22** (`internal/buildpipe/cache.go`). canonical_id
  컬럼 존재 ✅ (`internal/persist/schema.sql`).
- **모든 노드에 채워지지 않음 — 의도된 설계.** 심볼 노드(Function/Method/Struct/
  Field/Constant/패키지레벨 const·var) = 100%. 비심볼 노드(CallSite·IfStmt·Loop·
  Return·Switch·AwaitPoint, git Commit·Hunk)는 의도적 공백(심볼 아님). "빈 비율"은
  비심볼 노드 비율이지 결손이 아니다.
- 심볼 내 잔여 비유일성 ~4%(모두 설명됨): minified vendored JS, Go `init`, 테스트
  스텁 타입, 생성된 `.pb.go`. Go Method 유일성 99.98%.

## Q2 — 안정성
- canonical_id는 해시도 positional도 아니다. 의미 기반: Go = `<importpath>.(*Recv).Method`
  (go/types 유래, `internal/parse/golang/declarations.go` `goCanonicalID`), sol/ts/proto =
  `<relpath>:<qname>` (Solidity는 오버로드용 `(paramTypes)` 추가).
- **rebuild·라인 이동에도 불변**(유일 케이스). **예외**: 같은 파일 내 동일 id 중복 시
  `@<line>` 접미사(refinement B3, `internal/buildpipe/language_runners.go`
  `lineQualifyDuplicateCanonicalIDs`) — 이 접미사만 위치 의존. positional인 것은 별개의
  **node ID** = `sha256(qname|lang|startByte)`.

## Q3 — join key 합의안 (B7)
- **canonical_id 그 자체가 join key.** CKG가 포맷 소유(ADR-0001), CKV는 PR #9에서 이미
  바이트 그대로 상속 → 별도 정규화 규칙 불필요. 비심볼 노드는 node ID로 폴백.
- **integration fixture 합의**: 고정 repo에서 chunk↔node당 동일 canonical_id 단언;
  fixture에 두 caveat(≥1.19 게이트, `@<line>` 중복) 케이스 포함.

## Q4 — BM25 corpus (D4)
- **소유권 확인 ✅.** CKG가 BM25 소유: `pkg/bm25`(Okapi+tokenizer) + FTS5 인덱스
  (`internal/persist/sqlite_fts.go`) + evidence/hunk corpus(`pkg/evidence`). ADR-003의
  "BM25=CKG / CKV=vector-only / CKS=RRF"와 일치.
- 노드는 이미 `Signature`·`DocComment` 보유(`pkg/types/node.go`). qname+signature+
  doc-comment를 FTS 토큰 corpus에 접는 D4 확장 실현 가능 — 현재 `search_tokens` 커버리지
  확인 후 확장이 CKG deliverable.

## 협의 결정 (CKG 측)
- **매칭률 ≥90% 실측: 동의.** 대상 = go-stablenet(검증 코퍼스). 측정 그래프는 현재
  스키마(1.22)로 `make eval-build-dbs LANG=auto` 재빌드한 graph.db(1.16 아님). CKG가
  경로/sha 공유 → CKV 정렬·측정.
- **Flow-corpus Phase 2 / control-flow: CKG 제공 가능.** 함수 내부 제어흐름을 이미
  노드(IfStmt/Loop/Switch/CallSite/Return/AwaitPoint) + 엣지(calls/invokes/awaits/
  writes_field/rpc_calls)로 보유. 약식 `pkg.Func` ↔ 노드 조인은 B7(canonical_id) 선행.

## 후속 (CKG action items)
1. go-stablenet을 현재 스키마(1.22)로 재빌드 → graph.db 경로/sha를 CKV에 공유(매칭률 측정용).
2. D4: `search_tokens`가 qname+signature+doc-comment를 덮는지 확인, 미달 시 확장.
3. integration fixture를 양측 합의로 추가(≥1.19 게이트 + `@<line>` 중복 케이스 포함).
