# CKG 기능 검증 계획 — 키워드 → 확실한 코드 + 수정 히스토리

> 목적: 그래프 비교평가(α/β/γ/δ, LLM 판정)의 복잡도를 걷어내고,
> **"CKG DB가 키워드에 대해 ① 확실한 코드 위치와 ② 수정 히스토리 기록을
> 신뢰성 있게 쿼리할 수 있는가"** 를 **결정론적으로**(LLM 없이) 검증한다.

- **대상**: CKG (code-knowledge-graph) — go-stablenet 데이터셋
- **DB**: `code-knowledge-system/data/ckg-stablenet/graph.db` (schema 1.15, 노드 221k, node_prs 117k행/811 PR)
- **Ground truth (정답)**: go-stablenet **디스크 소스 + git 이력** — manifest `src_commit`(`91459d72…`)과 동일 체크아웃이므로 대조가 결정론적
- **산출물**: `Report.md` (+ `results.json`)

---

## 1. 검증 능력(capability)과 매핑

| 사용자 요구 | CKG 저장소(table/edge) | 쿼리 표면 |
|---|---|---|
| 키워드 → 확실한 코드 | `nodes_fts`(FTS5) → `nodes`(file_path,start/end_line,byte) + `blobs`(원본) | SQL / `ckg query` |
| 키워드 → 수정 히스토리 | `node_prs`(PR 단위) + `changed_in`/`blame`/`has_hunk`(커밋·헌크 단위) | SQL / MCP `change_history` |

## 2. 검증 항목 & 측정 지표 (사용자 4지표 매핑)

### C1. 키워드 → 코드 (Code Locating)
키워드(심볼명)로 FTS 검색 → **정의 노드**(Function/Method/Struct/Interface/Field/Constant/Variable/Type)를 surface하고, 그 위치·원본이 디스크와 일치하는지 검증.

- **위치 정확도**: 반환된 `file_path`가 디스크에 존재 + `start_line`에 심볼 선언이 실재 + `blobs.source` == 디스크 `[start_byte:end_byte]` (원본 무결성).
- **정답률(recall)**: ground-truth 키워드 중 정의 노드를 surface한 비율.
- **오류 건수(hallucination)**: 없는 파일/라인, 선언 불일치, blob≠디스크.
- **응답량**: 정의 1건 페이로드(시그니처+blob) 바이트 vs 파일 전체 — 타깃 회수 효율 참고치.

### C2. 키워드 → 수정 히스토리 (Change History)
심볼 노드 → `node_prs`(+커밋 엣지)로 수정 이력을 회수하고, git 이력과 대조.

- **히스토리 정밀도(precision)**: node_prs가 심볼에 귀속시킨 각 PR이 **실제로 그 심볼의 파일을 수정**했는가(go-stablenet git에서 `(#N)` 커밋이 해당 파일을 touch). 결과를 `verified / contradicted / not-found(upstream geth PR 등 로컬 미존재)` 3버킷으로 보고.
- **히스토리 정답률(recall)**: 수기 검증된 사실(예: `gasTipChanged`↔#77, `SetCurrentBlock`↔#77, `RemotesBelowTip`↔#77)을 node_prs가 포함하는가. (각 사실은 하네스가 git로 재확인.)
- **오류 건수**: contradicted 귀속 + 누락된 expected PR.

## 3. Ground truth (`ground-truth.json`)
- **C1 키워드**: txpool/legacypool/gasprice/core-types/consensus/wbft/core/systemcontracts 전반 14개 (모두 사전 존재 확인).
- **C2 히스토리 사실**: go-stablenet 네이티브 PR(#77 등)로 수기 검증된 (심볼, 파일, 필수 PR) 쌍. 정밀도 버킷은 C1 심볼 전체의 귀속을 대상으로 측정.

## 4. 하네스 (`verify.py`)
완전 결정론적, LLM 미사용. 모든 DB 사실 쿼리(FTS는 `sqlite3` 3.51 CLI), 디스크/`git` 대조.

```
python3 verify.py            # 전체 실행 → Report.md + results.json
python3 verify.py --smoke 3  # 앞 3개 키워드만 (하네스 검증용)
```

처리:
1. manifest에서 `src_root` 확인.
2. **C1**: 키워드별 FTS 정의 쿼리 → 디스크 라인/blob 대조 → 위치정확도·recall·오류 집계.
3. **C2-정밀도**: C1 심볼들의 node_prs 귀속을 git `(#N)`-touch로 검증 → verified/contradicted/not-found.
4. **C2-정답률**: 수기 사실을 node_prs에서 확인(+git 재확인).
5. `Report.md`(4지표 표 + 항목별 PASS/FAIL + 발견사항) 작성.

## 5. 합격 기준 (제안)
- C1 위치 정확도 ≥ 95%, 오류 0건(blob 무결성은 100% 기대).
- C2 정밀도(verified 기준) ≥ 90%, contradicted 0건.
- C2 recall: 수기 사실 충족률 보고 — **미충족은 결함으로 기록**(예상: `RemotesBelowTip`↔#77 누락 가능성, 사전 스모크에서 관측됨).

## 6. 정직한 한계
- node_prs 귀속 알고리즘(blame/PR-touch 매핑)의 세부는 빌드 코드 확인 후 Report에 명시.
- upstream geth PR 번호는 go-stablenet git에서 검증 불가 → `not-found`로 분류(정밀도 분모에서 제외, 별도 보고).
- 단일 데이터셋(go-stablenet) 기준 — 일반화 아님.
