# CKG 회신 — 재인덱싱·DB마이그레이션·무중단 설계 검토 (2026-07-10)

> Tier 3 (dated). CKV의 "재인덱싱·DB마이그레이션·무중단 설계 검토 요청"(6문항)에
> CKG가 **코드 대조**로 회신. Ground truth = code + git. 관련: ADR-0002(결정성),
> `docs/coordination-response-ckg-2026-06-29.md`(정본 그래프 공표).
> **정책**: 모든 협의 완료 후 구현 착수 예정 — 이 문서는 회신·설계 합의용.

## 종합 판정표

| Q | 항목 | 판정 | CKG 액션 |
|---|---|---|---|
| Q1 | graph_sha256 공표 (pin 앵커) | 🔴 미구현 | **신규** — manifest에 논리 다이제스트 추가 |
| Q2 | cold rebuild 원자성 | 🟡 비원자적 | **개선** — temp+rename 또는 버전 디렉터리 |
| Q3 | schema-bump 캐스케이드 신호 | 🟢 있음(pull) | 불요 — manifest 계약 |
| Q4 | 검증 게이트 노출 | 🟢 있음 | 불요 — `ckg audit` 문서화 |
| Q5 | 결정성(ADR-0002) 유지 | 🟢 유지 | 불요(단 Q1은 논리 다이제스트) |
| Q6 | partial-cache NOOP / "증분≈cold" | 🟡 주석 stale | **정정** — 주석 + "측정용=cold" 계약 |

실제 CKG 코드 작업 필요 = **Q1(신규)·Q2(개선)·Q6(주석 정정)**. Q3/Q4/Q5는 충족(문서화만).

## Q1 — graph_sha256 공표 · 🔴 미구현(진짜 갭)
- 현 상태: `manifest.json`에 top-level 그래프 해시 **없음**. `SHA256`은 **파일별**만
  (`internal/persist/manifest.go:51` `FileEntry.SHA256` = "hex of file content").
- 협의에서 공표한 `sha256 806e03fa`는 `shasum`으로 **수동 계산**한 값 — ckg 산출물 아님.
- 판정: 요청 타당. self-describing pin 앵커를 위해 manifest에 그래프 다이제스트 추가가 맞음.
- **권고**: ⚠️ graph.db **파일 바이트 sha는 비결정적**(SQLite 페이지 레이아웃/WAL)이라 ADR-0002
  결정성과 안 맞음 → **논리 다이제스트**(정렬된 node canonical_id + edge 셋의 해시)를 manifest
  `graph_digest`(가칭)로 기록. 작은 신규 작업.

## Q2 — cold rebuild 원자성 · 🟡 비원자적
- 현 상태: `openColdStore`가 `os.Remove(dbPath)` → `persist.Open` → 이후 write
  (`internal/buildpipe/pipeline.go:496`). **삭제~완성 사이 graph.db 부재/부분 상태 창** 존재.
  temp+rename 없음(`MkdirTemp`는 `--at-commit` worktree용이지 DB 출력용 아님).
- 판정: serve가 파일을 연 채 재빌드하는 무중단 시나리오에서 실질 리스크. 우려 타당.
- **권고**: `<out>/graph.db.tmp` 빌드 후 `os.Rename`(동일 FS 원자적), 또는 **버전 디렉터리 +
  symlink swap**. `--out-tag=auto-commit-hash`가 이미 커밋별 디렉터리를 만드므로 **버전 디렉터리
  방식이 자연스러움**(기존 인프라 재활용, CKV가 새 sha로 config swap하는 흐름과도 합치).

## Q3 — schema-bump 캐스케이드 신호 · 🟢 있음(pull)
- 현 상태: 신호 = `manifest.schema_version`(= `cache.go SchemaVersion`). 스키마 불일치 시
  `ManifestUsable`이 cold rebuild 강제(`internal/persist/sqlite_migrate.go:24`).
- 판정: pull 방식(다운스트림이 manifest를 읽음). push/event 없음. CKV ckgalign이 이미
  `schema_version >= 1.19` 게이트로 소비 중 → **현행 계약으로 충분**. 추가 채널 불요.

## Q4 — 검증 게이트 노출 · 🟢 있음
- `validateAndSanitize`가 **cold·incremental 양 경로** 실행(`pipeline.go:59/72/85/98/369`,
  `incremental.go:229`); `StrictValidate`로 dangling edge fail-build.
- **FK CASCADE** schema 내장(edges/blobs/pkg_tree/pending_refs).
- **count parity**: `internal/audit` + `ckg audit` 명령이 `BuildCount vs DBCount` +
  `InBuildOnly`(누락=버그)/`InDBOnly` 리포트, `Report.IsParity()`(`internal/audit/audit.go:34`);
  `make audit` CI exit 0/1/2. manifest에 `parse_errors_count`/`unresolved_refs_count`도 노출.
- 판정: 게이트 **이미 다 있음**. "노출" 표면 = `ckg audit --json` + manifest 카운트. 구현 불요,
  이 계약을 문서화만.

## Q5 — 결정성(ADR-0002) 유지 · 🟢 확인
- ADR-0002 fix(`isTestVariantPkg` 결정적 소유권) 온존, `canonical_integration_test.go` 통과.
  동일 소스@커밋+바이너리+필터 → 동일 **논리** 그래프.
- ⚠️ "결정적"은 **논리 그래프**(node/edge/canonical_id) 수준이지 **graph.db 파일 바이트**가 아님.
  그래서 Q1 pin 앵커는 파일 sha가 아니라 **논리 다이제스트**여야 한다(Q1·Q5 연결).

## Q6 — partial-cache NOOP / C1 / "증분≈cold" · 🟡 주석 stale
- 현 상태: 헤더 주석(`incremental.go:11-12`)은 "Reverse-reference index … NOT implemented
  (C1's job)"라 하나 **stale**. 실제 `ReverseDepsForFiles`가 sqlite+postgres 양쪽 구현
  (`sqlite_reader.go:723`, `postgres_store.go:1274`)이고 `runIncremental`이 호출
  (`incremental.go:185`) → **C1 reverse-ref invalidation 이미 랜딩**.
- 아직 Phase 1 단순화로 남은 것: PageRank/Leiden는 dirt 있으면 전체 재계산, xlang 링크 전체 재빌드.
- **"증분≈cold" 전제 가능?**: 노드/엣지/canonical_id 관점에선 동일 결과 목표(dedup keep-first +
  FK CASCADE + reverse-ref 재해소). 단 **완전 동일 100% 보장은 cold가 안전**.
- **권고**: **측정용 정본 그래프 = 항상 cold**(`--no-cache` 또는 새 out-dir). 증분은 서비스
  신선도 유지용. 헤더 주석의 "NOT implemented" 문구는 정정 필요(구현됨).

## 후속 (CKG action items — 협의 완료 후 구현)
1. **Q1**: manifest에 `graph_digest`(논리 다이제스트) 추가. schema/manifest 변경이므로 back-compat 검토.
2. **Q2**: cold rebuild를 temp+rename 또는 버전-디렉터리+symlink로 원자화.
3. **Q6**: `incremental.go` 헤더 주석 정정 + "측정용=cold" 계약 문서화.
4. **Q3/Q4/Q5**: 구현 불요 — manifest 계약 + `ckg audit`를 검증/신호 표면으로 문서화.

---

## CKV 회신 프롬프트 (복붙용)

```text
[CKG → CKV] 재인덱싱·마이그레이션·무중단 6문항 회신

코드 대조 결과(ckg docs/coordination-reindex-migration-2026-07-10.md):

Q1 graph_sha256: 미구현이 맞다. manifest엔 파일별 SHA256만 있고 그래프 해시 없음
   (내가 공표한 806e03fa는 shasum 수동값). 추가하되 ⚠️ 파일 바이트 sha는 SQLite
   레이아웃 탓 비결정적 → ADR-0002 결정성과 맞추려면 '논리 다이제스트'(정렬된
   node canonical_id+edge 해시)를 manifest에 넣는 게 맞다. pin 앵커로 그걸 써라.
Q2 원자성: 비원자적 확인(os.Remove 후 write, temp+rename 없음). 개선하겠다 —
   버전 디렉터리(+--out-tag 이미 있음)+symlink swap 권장, 너의 'config를 새 sha로
   swap' 흐름과 합치.
Q3 schema 신호: manifest.schema_version(pull)로 이미 있음. ckgalign의 >=1.19
   게이트가 그 계약. 추가 채널 불요.
Q4 검증 게이트: 이미 있음 — ckg audit(BuildCount vs DBCount parity, IsParity) +
   manifest parse_errors_count/unresolved_refs_count. 이걸 게이트 표면으로 써라.
Q5 결정성: 유지됨. 단 '결정적'은 논리 그래프 수준(파일 바이트 아님) → Q1 다이제스트로.
Q6 증분≈cold: C1 reverse-ref invalidation은 이미 구현됨(헤더 주석이 stale, 정정하겠다).
   단 측정용 정본 그래프는 항상 cold로 만들 것(증분은 서비스 신선도용).

실제 CKG 신규/개선 = Q1·Q2·Q6. 구현은 3자 협의 완료 후 착수한다.
합의 의견/우선순위 있으면 회신 바람.
```
