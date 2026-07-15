# CKG → CKV 협의 회신 — canonical_id join · 2026-06-29

> **ARCHIVED 2026-07-15 — 협의 종결.** canonical_id join key(=canonical_id)·graph_digest 공표 합의 완료. 결정은 ADR-0001 + `archive/coordination-reindex-migration-2026-07-10.md`로 승계. Provenance용 보존.

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
- ⚠️ **정정(코드 확인 2026-06-29): D4 핵심 목표 이미 충족 — 코드 변경 없이 종결.** `nodes_fts`
  FTS5(`schema.sql`)가 `name, qualified_name, signature, doc_comment, search_tokens` 5컬럼 전부
  인덱싱(`rebuild`로 채움, `sqlite_writer.go`). signature·doc_comment는 이미 키워드 corpus 포함 —
  PR #40 메시지 "its signature is FTS-indexed"가 입증(R06 'vault'). `search_tokens`는 name/qname
  split 토큰 전용. 남은 micro-gap(signature/doc 내부 식별자 sub-word split)은 precision 리스크
  대비 이득 낮아 fixture로 필요 입증 전까지 보류(YAGNI).

## 협의 결정 (CKG 측)
- **매칭률 ≥90% 실측: 동의.** 대상 = go-stablenet(검증 코퍼스). 측정 그래프는 현재
  스키마(1.22)로 `make eval-build-dbs LANG=auto` 재빌드한 graph.db(1.16 아님). CKG가
  경로/sha 공유 → CKV 정렬·측정.
- **Flow-corpus Phase 2 / control-flow: CKG 제공 가능.** 함수 내부 제어흐름을 이미
  노드(IfStmt/Loop/Switch/CallSite/Return/AwaitPoint) + 엣지(calls/invokes/awaits/
  writes_field/rpc_calls)로 보유. 약식 `pkg.Func` ↔ 노드 조인은 B7(canonical_id) 선행.

## 후속 (CKG action items)
1. go-stablenet을 **`0bf2f4d1b`(PR-77 버그-부모)** 커밋 + 현재 스키마(1.22)로 재빌드 →
   graph.db 경로/sha/manifest schema_version를 CKV·CKS·coding-agent에 공유(D-1/D-2).
2. ~~D4: `search_tokens` 확장~~ → **종결(코드 확인).** signature·doc_comment는 이미 `nodes_fts`
   5컬럼 인덱싱으로 키워드 corpus에 포함(PR #40 입증). 추가 작업 불요 — 위 Q4 정정 참조.
3. integration fixture를 양측 합의로 추가(≥1.19 게이트 + `@<line>` 중복 케이스 포함).
   - **CKG 半 완료(2026-06-29):** `TestCanonicalID_IntegrationContract_DeterministicAndAlignable`
     (`internal/parse/golang/canonical_integration_test.go`)가 join 계약의 CKG 쪽을 잠근다 —
     (a) **결정성**: 동일 소스 재빌드 시 canonical_id가 qname·(file,line) 양쪽으로 동일,
     (b) **정렬 전제조건**: canonical_id 보유 노드는 모두 ckgalign이 쓰는 (FilePath,StartLine,
     EndLine) 위치를 동반. `@<line>` 중복은 기존 `TestLineQualifyDuplicateCanonicalIDs`(buildpipe)가,
     ≥1.19 게이트는 CKV `hasPopulatedCanonical`가 담당. **CKV 半(청크가 동일 id 상속 단언)은 CKV 세션 소관.**

## coding-agent D-1~D-5 결정 (2026-06-29)

coding-agent §3-R(`coding-agent/docs/coordination-response-coding-agent-2026-06-29.md`)
가 제기한 5개 결정 중 CKG 소관 회신. CKV 문서에는 §1-R2로 동일 반영.

- **D-1 ★ 동의 — 재인덱싱 커밋 핀 `0bf2f4d1b`.** 커밋은 go-stablenet(HEAD 44d75d17)·
  test/pr-77(HEAD 2e83c318) 양쪽 존재(PR #75). 그래프는 (소스@커밋 + 스키마)로 결정적 →
  1회 재인덱싱으로 3자 커버. 제약: go-stablenet을 `0bf2f4d1b`로 detached+clean 체크아웃 후
  `make eval-build-dbs LANG=auto` 빌드. 임베딩 A/B는 동일 그래프 공유(CKG는 임베딩 비의존).
- **D-2 확인 — schema 1.22(≥1.19).** 현재 바이너리가 매니페스트에 1.22 스탬프 → canonical_id
  완전 채움. manifest schema_version + sha 공표.
- **D-3 동의 — parity 분리.** recall/rerank = 기존 cks search_text/semantic_search로 도달
  (proxy 불요) / flow·invariant = ckg가 데이터 제공, 도달은 cks 표면 노출 전제(cks 소관).
- **D-5 supersede 아님.** PR #40(`473bf1d`)은 `eval/baseline/retrieval.json` 단일 파일만 바꾼
  baseline 수치 갱신(R06=search_text, recall 이미 1.0, precision_min 0.5 recall-first 완화).
  "graph-gap P3(suffix-match resolver ~23% recall)"은 빌드 P3 Resolve 패스
  (`internal/parse.Resolve()`)의 엣지-해소 완전성 = 다른 레이어 → #40 무관.
  진짜 레버: PR #31 `simple_name` suffix 인덱스(머지됨) + deferred CamelCase 토크나이저(R10).
  coding-agent에 "~23%" 측정 출처(툴/fixture) 지목 요청.

## CKS §2-R2 / CKV §3-R-CKV-2 / §6 후속 수용 (2026-06-29)

5세션 수렴. CKS·CKV가 §1-R2 이후 추가한 CKG 관련 항목 수용. CKV 문서에 §1-R3로 동일 반영.

- **CKG = 정본 그래프 단독 생산자 (CKS §2-R2 수용).** `LANG=auto`(sol/proto) vs `--lang go`는
  다른 그래프를 낳으므로 **누구도 독자 재빌드 안 함** — CKG가 `0bf2f4d1b` + `LANG=auto`로
  만든 단일 canonical graph.db를 생산·공표(경로/sha/manifest schema_version), CKV 정렬 / CKS
  config 참조. CKG가 단일 산출물 책임.
- **매칭률 언어 스코프 = 공유언어(go/sol/ts/js), proto 제외 (CKV §3-R-CKV-2 수용).** CKG
  `LANG=auto`의 proto 심볼 노드(~409)는 CKV 미파싱 → 대응 청크 부재(설계상). 분모에서 proto
  제외 동의. **정밀화**: 분자의 CKG 노드는 canonical_id 보유 심볼 노드여야 join 유효(비심볼
  노드 위치정렬은 NULL → 제외).
- **모델 축: CKG 그래프 1회 (§6-3).** reindex-A(bge-m3)/B(Qwen3) 2회는 CKV 벡터 레이어;
  CKG graph.db는 임베딩 비의존 → 단일 그래프가 A·B 공통 서비스. CKG는 1회 빌드 + sha 공표.
- **비전 R1/R2 (§6-2):** R1(차원)은 CKG 비의존. R2(flow/invariant cks 노출)는 CKG가 데이터
  제공·노출은 cks 소관 → post-Phase-2 defer 금지 가드레일 동의.

## 정본 측정 그래프 공표 (D-1/D-2, 2026-06-29)

CKG가 단독 생산한 정본 graph.db. CKV 정렬 / CKS config / coding-agent PR-77 A/B는
**모두 이 산출물 하나**를 가리킨다(독자 재빌드 금지).

> ⚠️ **갱신(2026-06-30): 정본 그래프가 필터본으로 교체됨.** 초기 공표는 whole-tree
> (`/tmp/ckg-eval/…`, `--lang=auto`, sha `16ee6fb7…`, go/sol/ts/proto)였으나, **gstable
> 바이너리 스코프 + 관련 test** 필터를 적용한 빌드로 확정한다(아래). 차이: **ts/proto와
> 바이너리 외 디렉터리 제외 → 공유 스코프가 go+sol**. CKV/CKS/coding-agent는 *이* 그래프에
> 정렬·측정해야 한다(옛 /tmp whole-tree 그래프는 폐기).

```
path:    /Users/.../knowledge-data/pr-77-2/graph.db        (동일 머신의 sibling 세션 기준)
src:     /Users/.../test/analysis-test-3                    (go-stablenet @ 0bf2f4d1b, detached/clean)
commit:  0bf2f4d1b   (PR-77 버그-부모)
build:   ckg build --src=analysis-test-3 --lang=auto \
           --files-from=code-knowledge-graph/eval/stablenet/stablenet-files-with-tests.json
         → gstable 바이너리 디렉터리의 .go(+ systemcontracts sol), **test 포함**, ts/proto 제외
schema_version (manifest): 1.23                            (>= 1.19 → canonical_id 완전 채움)
sha256(graph.db): 806e03faa0369d75fffbcfed7327a62e5ada736a81f3555c25c23f639969ebd1
nodes / edges:    183,121 / 1,603,496   (그중 _test.go 노드 67,508 — test few-shot 포함)
```

**재현성:** ADR-0002(staged composition, schema 1.23) + 고정 필터(`stablenet-files-with-tests.json`)
적용본이다 — 동일 커밋·동일 바이너리·동일 필터로 재빌드하면 동일 그래프가 나온다.

**매칭률 분모 스코프 (필터본 기준):** **공유언어 = go+sol**(필터가 ts/proto 제외)의
canonical_id 보유 심볼 노드만 분모. 제외 대상:
- **ts/proto**: 필터에서 제외됨(이번 정본엔 노드 자체가 없음).
- **promoted/synthetic 메서드**(~915, `doc_comment="promoted from …"`): embedded-field 승격
- **promoted/synthetic 메서드**(~915, `doc_comment="promoted from …"`): embedded-field 승격
  합성 노드로 canonical_id가 설계상 비어있음(진짜 메서드는 declaring 타입에 있고 거기 canonical_id 보유).
  CKV에도 대응 청크 없음 → proto와 동일 논리로 분모 제외.
