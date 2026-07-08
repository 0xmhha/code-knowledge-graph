# [포인터] `ckg_node_id` 은퇴 · `canonical_id` 단일화 — CKG 작업분

- 상태: Proposed
- 작성일: 2026-07-08
- **마스터 문서**: `code-knowledge-system/docs/retire-ckg-node-id.md` (전체 배경·판정·세 repo 체크리스트)
- 관련 ADR: `docs/adr/0001-canonical-symbol-id.md`

## 요지

ckv/cks가 공유하던 `ckg_node_id`(위치 해시)는 죽은 필드로 판정되어 은퇴하고, ADR-0001의 `canonical_id`로 단일화한다. **CKG 코드는 사실상 변경 없음** — 이 은퇴는 ckv/cks 공유 표면에서만 일어난다. (자세한 근거·판정은 마스터 문서.)

## CKG 체크리스트

- [ ] **코드 변경 없음** — `nodes.id`(위치 해시 PK)는 edges FK·traversal 백본이므로 유지. "ckg_node_id"라는 이름은 ckg에 존재하지 않음(외부 이름).
- [ ] (선택·독립) `internal/parse/golang/declarations.go:376` `goCanonicalID` 커버리지 확대 — 빈 `canonical_id` 축소로 다운스트림 조인 적중률↑. **이 은퇴의 필수 아님.**
- [ ] `canonical_id`는 ckg가 생산·소유하는 값이므로, 스키마/생성 로직 변경 시 다운스트림(ckv 정렬)에 영향 있음을 인지.

## 주의

CKG 입장에서 이 문서는 "우리는 이미 canonical_id를 생산 중이고, 다운스트림이 ckg_node_id를 버린다"는 **인지용 포인터**다. ADR-0001을 새 ADR로 supersede할 필요는 없음(결정 변경이 아니라 미완 이관의 마감).
