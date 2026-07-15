# [포인터] `ckg_node_id` 은퇴 · `canonical_id` 단일화 — CKG 작업분

> **ARCHIVED 2026-07-15 — CKG 작업분 마감.** ckg는 `ckg_node_id` 참조 0건(외부 이름)이라 코드 변경 없음. 실제 통합은 ckv(완료 07-11)·cks(마감 07-12). 마스터: cks `docs/retire-ckg-node-id.md`. Provenance용 보존.

- 상태: **CKG 작업분 마감** (검증 2026-07-10) — 코드 변경 없음 확정
- 작성일: 2026-07-08
- **마스터 문서**: `code-knowledge-system/docs/retire-ckg-node-id.md` (전체 배경·판정·세 repo 체크리스트)
- 관련 ADR: `docs/adr/0001-canonical-symbol-id.md`

## 요지

ckv/cks가 공유하던 `ckg_node_id`(위치 해시)는 죽은 필드로 판정되어 은퇴하고, ADR-0001의 `canonical_id`로 단일화한다. **CKG 코드는 사실상 변경 없음** — 이 은퇴는 ckv/cks 공유 표면에서만 일어난다. (자세한 근거·판정은 마스터 문서.)

## CKG 체크리스트

- [x] **코드 변경 없음 — 검증 완료.** `grep -rn 'ckg_node_id\|CKGNodeID'` over ckg `.go` = **0건**.
  `nodes.id`는 PK로 유지(`internal/persist/schema.sql:7`, `MakeID = sha256(qname|lang|startByte)`
  `internal/parse/idgen.go:12`) — edges FK·traversal 백본. "ckg_node_id"는 ckv/cks가 붙인 외부 이름.
- [x] **(선택·독립) `goCanonicalID` 커버리지 확대 — 이번엔 하지 않음(의도적).** 정본 그래프 pr-77-2
  실측(2026-07-10): 빈 `canonical_id`는 거의 전부 **설계상 정상** — Method 608 중 607이 promoted/synthetic,
  Variable 74%가 함수-지역(B2), Constant 대부분 test/함수-지역. 실제 늘릴 진짜 심볼 극소수. 반면 `goCanonicalID`
  (`internal/parse/golang/declarations.go:376`) 변경은 canonical_id 값→schema bump→재빌드→**공표 정본 그래프
  sha(806e03fa) 무효화→CKV/coding-agent 협의 파급**. near-zero 이득 대비 비용 큼 → 독립 과제로 보류.
- [x] **다운스트림 영향 인지.** `canonical_id`는 ckg 생산·소유 값이라 스키마/생성 로직 변경 시 ckv 정렬에
  영향. 그래서 위 커버리지 작업을 정본 그래프 재빌드 없이 함부로 하지 않는다(협의 포인터 정합 유지).

## 주의

CKG 입장에서 이 문서는 "우리는 이미 canonical_id를 생산 중이고, 다운스트림이 ckg_node_id를 버린다"는 **인지용 포인터**다. ADR-0001을 새 ADR로 supersede할 필요는 없음(결정 변경이 아니라 미완 이관의 마감).

## Cross-repo 상태 (2026-07-10)

전수조사 결과 `ckg_node_id`/`CKGNodeID`는 **ckg 코드에 0건**(외부 이름) — 통합 대상은 ckv/cks 공유
표면(ckv `chunks.ckg_node_id`, cks `Hit.CKGNodeID`)이다. 따라서 실제 `canonical_id` 단일화 코드
작업은 **ckv·cks 소관**이며 각각 별도 세션에서 진행 중이다.

| repo | 작업 | 상태 |
|---|---|---|
| **ckg** (본 repo) | 코드 변경 없음 (nodes.id 유지, canonical_id 이미 유일 생산) | ✅ 마감 |
| **ckv** | `chunks.ckg_node_id` 컬럼·인덱스·필드 제거 → canonical_id 단일화 (생산자, 먼저) | 🔶 진행 중 (전용 세션) |
| **cks** | `Hit.CKGNodeID` 제거 → canonical_id 단일 조인 키 (소비자, ckv 다음) | 🔶 진행 중 (전용 세션) |

ckg는 위 두 세션에 대해 **의존성 없음** — 이 브랜치는 독립적으로 PR 가능(ckg 작업분 완료).
