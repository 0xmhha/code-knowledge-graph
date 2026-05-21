# 작업 투두 — 2026-05-20 정리

> 출처: 2026-05-19 cks dogfood-eval 결과를 반영한 ckg 후속 작업 정리.
> 본 문서는 진행 추적용이며, 항목별 상세 컨텍스트는
> `docs/followups-from-cks-dogfood-2026-05-19.md` 참조.

## 진행 상태

- [x] **A1** `git push origin main` — 사용자가 수동 처리 완료 (2026-05-20)

## B. cks dogfood 후속 (우선순위 順)

| # | 항목 | 영향 | 표면 | 상태 |
|---|---|---|---|---|
| CKG-1 | `SearchFTS`에 BM25 score/rank 반환 | High | 작음 | ✅ `2f89b17` |
| CKG-2 | `SearchFTS`에 native filter (language) | High | 중간 | ✅ `570e5ec` |
| CKG-4 | Symbol lookup `kinds []SymbolKind` 다중 처리 | Mid | 중간 | ✅ `d34a2eb` |
| CKG-3 | Cross-snapshot 정책 결정 | High | 가변 | ⏸️ 보류 (cks 시나리오 필요) |
| CKG-5 | Traversal depth=2 측정 | Mid | 측정 | ✅ `c80b1c5` `b3db16f` `b308c1c` |
| CKG-6 | `pkg/store.Reader` 공개 surface 정리 | Mid | 작음 | ✅ `78edfc5` |
| CKG-7 | `persist.Manifest` 일부 노출 | Low | 작음 | ✅ `d487fbe` |

### CKG-1 세부 — `SearchFTS` 점수 반환 ✅ `2f89b17`

**구현 요약:**
- `internal/persist/search_hit.go` — `SearchHit{Node, Score, RawScore}` 타입 + `normalizeSearchHits` 헬퍼
- `StoreReader.SearchFTS` 시그니처 `[]SearchHit` 로 변경
- SQLite: `-bm25(nodes_fts)` (sign-flip), `ORDER BY raw_score DESC`
- PG: `ts_rank(search_vector, plainto_tsquery)`, `ORDER BY raw_score DESC`
- `Search()` 시그니처 유지 — 내부 `nodesFromHits` 어댑터로 호환
- `llmSafeStoreReader.SearchFTS` — AMBIGUOUS 필터 후 점수 보존
- 회귀 테스트 3종: ScoreMonotonic / ScoreRangeNormalized / SingleHitScoreOne

**다운스트림 액션 (E2 후속):** cks `internal/ckgclient/real.go:149-155` 가짜 점수 제거 가능.

### CKG-2 세부 — native filter pushdown ✅ `570e5ec`

**구현 요약:**
- `SearchFTSOptions{Language string}` 추가 (`internal/persist/search_hit.go`)
- `StoreReader.SearchFTS(q, limit, opts)` 시그니처 확장
- SQLite/PostgreSQL 모두 동적 SQL — `opts.Language != ""` 일 때만 WHERE 추가
- `Search()` 어댑터는 `SearchFTSOptions{}` 전달, 기존 호출자 7곳 무영향
- path glob은 client-side 유지 (CKG-2 follow-up 문서 권장)
- 회귀 테스트 3종: LanguagePushdown / LanguageEmptyMatchesAll / LanguageNoMatch

**다운스트림 액션 (E2 후속):** cks `internal/ckgclient/real.go`의 `FilterOverfetchRatio=3` over-fetch + client-side language filter 제거 가능. path glob은 그대로 유지.


### CKG-4 세부 — multi-kind symbol lookup ✅ `d34a2eb`

**구현 요약:**
- `FindSymbolOptions{Language, Kinds []types.NodeType}` 추가
- `FindSymbol(name, exact, opts)`로 시그니처 재배치 — `lang`을 Options로 이동
- SQLite: 기존 `placeholders(n)` 헬퍼 재사용해 `type IN (?, ?, ...)` 조립
- PostgreSQL: `$N` placeholder 인라인 생성
- 빈 `Kinds` → 원래 WHERE plan 유지 (planner cost regression 없음)
- 회귀 테스트 4종: KindsSingle / KindsMultiple / KindsEmptyMatchesAll / KindsNoMatch

**다운스트림 액션 (E2 후속):** cks Stage 2 `arch_explain` 의도에서 N round-trip → 1 query로 단축 가능. dedupe는 Citation key 기준 클라이언트 측 유지.


### CKG-3 세부 — Cross-snapshot 정책 결정 ⏸️ **보류**

**보류 사유 (2026-05-20 결정):**

ckg 단독 결정 불가 — cks가 cross-commit 검색을 정말 필요로 하는지, 어떤 시나리오(시간순 회귀 추적? 두 commit 비교? 마이그레이션 영향 분석?)에서 쓰는지 모름. 작업 옵션 3개의 비용 차이가 매우 크므로(문서화 1줄 vs 스키마+25개 query 전면 수정) 시나리오 없이 옵션 선택은 위험.

**재개 조건 (다음 중 하나라도 충족 시):**
1. cks 팀이 cross-commit 검색이 필요한 *구체적 시나리오 1-2개* 제시 (어떤 질의 패턴, 어떤 UI에서 사용)
2. ckg 측에서 cross-commit 회귀 분석 같은 자체 use case가 생김
3. 일정 기간(예: 다음 분기) 경과해도 needs가 안 모이면 **옵션 A — 단일 snapshot 제약 명시적 문서화** 진행하고 cks 측 `Filter.CommitHash` 필드 제거 PR 동조

**현재 상태에서 분명한 것:**
- ckg는 처음부터 단일 snapshot 모델 (`Manifest.SrcCommit` single string, DB 스키마에 `snapshot_id` 컬럼 없음)
- cks `internal/ckgclient/real.go:144-147`이 `Filter.CommitHash`를 ignore 처리하고 "single snapshot" 주석 명시 — *기능 부재가 아니라 API drift*

**작업 옵션 (재개 시 평가):**

| 옵션 | ckg 변경 | cks 변경 | 비용 | 실제 cross-commit |
|---|---|---|---|---|
| A — 단일 snapshot 명시적 문서화 | 1줄 (docstring) | filter 필드 제거 | 매우 작음 | ❌ |
| B — Multi-snapshot 완전 지원 | 스키마+모든 query+빌드 | 필터 활용 | 매우 큼 | ✅ |
| C — 디렉토리 라우팅 (DB per commit) | 작음 (path 컨벤션) | client 측 라우팅 | 중간 | 부분 |

### CKG-5 세부 — depth=2 측정 ✅ `c80b1c5` / `b3db16f` / `b308c1c`

**작업 구성 (3 commit):**
1. `c80b1c5` — bench-mcp `--depth-sweep` 옵션 추가 + `mcpProbe` Tool/Name 분리 + `pickFunctionSeed` 핫픽스 (`QueryNodes("")` → `TopNodes("pagerank")`)
2. `b3db16f` — 측정 데이터(`docs/ckg5-depth-sweep.json`) + 분석 리포트(`docs/ckg5-depth-sweep-report-2026-05-20.md`)
3. `b308c1c` — `find_callers`/`find_callees` default 1→2 적용 (tool description에 리포트 링크)

**측정 결과 요약:**
- depth=2 p99이 4 tool 모두 30ms 이하
- `get_subgraph` p99이 d=1(122ms) > d=2(51ms) — depth가 분산을 줄이는 반직관적 결과
- `find_callees` d=1의 0.24ms는 측정 노이즈 베이스라인 → 17.8× ratio 과장됨

**다운스트림 액션 (E2 후속):** cks 다음 dogfood에서 `mcp-tool-handlers` / `stamp-integrity-lookup` recall 변동 확인. recall ceiling 0.67이 회복되면 ckg 측 default 변경 정당화됨.

### CKG-6 세부 — `pkg/store` 공식 surface 확장 ✅ `78edfc5`

**구현 요약:**
- `pkg/store`에 `SearchHit`, `SearchFTSOptions`, `FindSymbolOptions`, `ErrInvalidMetric` alias 추가
- 패키지 doc 강화 — 외부 사용자 가이드, "self-shim 금지" 명시
- 컴파일 가드 + `TestPublicSurface_CanConstructOptions` — 외부 사용자 관점에서 옵션 구성 가능성 검증
- `Manifest`는 의도적 제외 — CKG-7에서 *축소 mirror struct*로 별도 노출

**근본 발견:** `pkg/store.Reader = persist.StoreReader` alias만으로는 부족했음 — Go에서 interface alias는 메서드 시그니처를 전이하지만, *반환 타입은 별도 노출* 필요. cks가 `[]SearchHit`를 받아도 그 타입을 이름붙일 수 없으면 mirror struct 만들 수밖에 없었던 이유.

**다운스트림 액션 (E2 후속):** cks `internal/ckgclient/real.go:1-50`의 자작 `persist.StoreReader` alias 제거 → `github.com/0xmhha/code-knowledge-graph/pkg/store` 직접 import로 교체.

### CKG-7 세부 — `Manifest` 축소 mirror ✅ `d487fbe`

**구현 요약:**
- `pkg/store.Manifest` (3 필드만: `CommitHash`, `SchemaVersion`, `IndexTimestamp`)
- `pkg/store.GetManifest(r Reader) (Manifest, error)` 헬퍼 — internal → public 변환 봉인
- `projectManifest` (package-private) — 매핑 단일 진실 점, unit-testable
- 회귀 테스트: 변환 정확성(internal package test) + zero-value + 외부 사용성

**Mirror vs alias 선택 근거:** `persist.Manifest`는 incremental-cache 내부 필드(`Files`, `StalenessFiles`, `StalenessMTimeSum`, `SrcRoot`) 포함. alias하면 cks가 cache 필드에 silently 의존 → 다음 cache 메커니즘 진화 시 외부 호환성 부채로 작용. mirror가 *경계 보존*.

**다운스트림 액션 (E2 후속):** cks `ManifestSnapshot` mirror struct 제거, `store.Manifest`로 교체. `store.GetManifest(reader)` 한 줄 호출로 대체 가능.

## D. 잔여 정리

- [x] **D1** viewer ESLint 4 warnings 정리 ✅ `5348e53` — App.tsx unused-disable 제거, GraphCanvas.tsx + usePersistedState.ts에 의도 명시 disable, TicketIndex.tsx에 stable setter dep 추가
- [x] **D2 + D3** Makefile fmt/fmt-check + opt-in pre-commit hook ✅ `0c5dce1` — `make lint` deps에 `fmt-check` 포함되어 기존 CI가 자동으로 drift 차단, `.githooks/pre-commit`은 opt-in (`make install-hooks`)

## E. 크로스 레포 동조

- [x] **E1** ckv companion 문서 확인 ✅ — ckv `docs/followups-from-cks-dogfood-2026-05-19.md` 이미 존재 (CKV-1~7, 7 항목). CKV-1은 cks-side 이슈로 재배정되어 ckv 측에서 close (commit `42bb7f2`). 나머지 6 항목은 ckv 작업자 별도 진행 중. ckg 측 추가 액션 없음.

  **ckg 변경의 ckv-side 영향 (이번 세션 누적):**
  - CKG-1/2/4/6/7: SQL graph DB 경로 — ckv vector engine과 별개. **영향 없음**.
  - CKG-5: `find_callers/callees` default 1→2. cks composer가 ckg에서 더 많은 노드를 가져와 ckv에 *추가 query*를 보낼 가능성 있음. **간접 영향 가능** — 다음 cks dogfood 측정에서 ckv latency/recall 변동 관찰 권장.
  - D1/D2/D3: 내부 lint/format. **ckv 영향 없음**.

- [ ] **E2** cks 측 워크어라운드 제거 PR (CKG-1, CKG-2, CKG-4 구현 후)
  - `internal/ckgclient/real.go:149-155` 가짜 점수 (`1-i/(N+1)`) → `SearchHit.Score` 사용
  - `FilterOverfetchRatio=3` over-fetch + client-side language filter → `SearchFTSOptions{Language}` 푸시다운
  - `arch_explain` intent N round-trips → `FindSymbolOptions{Kinds: [...]}` 1 query
  - CKG-6/7 결과: self-shim `persist.StoreReader` alias 제거 → `pkg/store` 직접 import + `store.GetManifest()` 헬퍼 사용

## F. 보안 / 평가 인프라 (2026-05-20 사용자 신규 지시)

- [x] **V1** viewer npm audit fix ✅ `c3cff0c` — `next` (high) + `brace-expansion` (moderate) transitive 업데이트, package.json 미변경, 0 vulnerabilities.
- [x] **EV1 Phase 1** LLM-free `make eval` + baseline ✅ `648ee59` — `ckg benchmark --format json` 추가 + Makefile `eval`/`eval-baseline-update` 타겟 + `eval/baseline/` 첫 measurement. validate / benchmark / bench-mcp 3종 measurement.

  **첫 baseline (사용자가 푸시 후 노드 카운트는 코드 변경에 따라 달라짐):**
  - validate: 0 issues
  - benchmark: 132.75× reduction (avg 4,432 tok/query vs 588,365 tok corpus)
  - bench-mcp: 8 probes errors=0, p99 ceiling 130 ms (impact_of_change_d2)

- [x] **EV1 Phase 2** Gold-set retrieval accuracy ✅ `7679410` — `internal/eval/retrieval/` 패키지 + `ckg eval-retrieval` CLI + `eval/retrieval/*.yaml` 5 fixture (find_callers x2 / find_callees x1 / find_symbol x1 / search_text x1). Synthetic-index 사용 (코드 변경 시 expected 안정성). `make eval` step 5/5에 통합. 첫 baseline: 5/5 passed, aggregate R=1.00 P=0.60 F1=0.75.

  **lockdown 발견:** R04 fixture 작성 중 SQLite LIKE의 ASCII case-insensitive 매치 동작 발견 → `api.Handler.vault` (소문자 field) 가 `Vault` 검색에 매치됨. 의도된 동작이라 fixture에 명시적 expected로 포함, 향후 case-sensitive 회귀 시 fail.

  **다음 ratchet 후보:** R05 recall_min을 0.66 → 1.0 (SQLite/PG FTS 토큰화 parity 확인 후), precision 게이트 강화 (statement-node 제외 필터 추가 후).
- [ ] **EV1 Phase 3** CI 통합 — ⏸️ **보류** (2026-05-21).

  **현재 차단 사유:** ci.yml 자동 편집이 보안 hook(`PreToolUse:Edit`)으로 차단됨. 작성된 워크플로 snippet은 *모든 `run:` 명령이 static command*이고 `github.event.*` 같은 untrusted 입력을 사용하지 않아 *실제 주입 위험은 없으나*, 자동 적용은 정책상 막혀 있어 **사용자 수동 적용**이 필요.

  **재개 절차 (사용자 수동 적용):**
  1. `.github/workflows/ci.yml`의 `smoke:` job 다음(파일 끝)에 아래 snippet 그대로 추가.
  2. push → GitHub Actions 탭에서 `eval` job 실행 확인. 첫 실행은 baseline과 동일하므로 ✅ pass + artifact 생성.
  3. 의도된 회귀 PR(예: 일부러 R01 expected를 잘못 적은 PR)로 gate 동작 검증.
  4. 작업 완료 후 본 항목 ✅로 marking.

  **권장 snippet (보관):**

  ```yaml
    eval:
      # LLM-free regression gate (EV1 Phase 1+2):
      #   1. ckg build (self-index of this repo)
      #   2. ckg validate / benchmark / bench-mcp  — informational JSON
      #      captured as the `eval-results` artifact for baseline diffing
      #   3. ckg build (synthetic corpus) + ckg eval-retrieval
      #      — fails the job when any fixture's recall / precision falls
      #      below its committed threshold. This is the load-bearing gate.
      #
      # Parallel with test / lint / audit so a slow retrieval probe
      # doesn't block fast-feedback signal. Single ubuntu runner is
      # sufficient: the probes hit pure SQLite and the metrics are not
      # OS-specific.
      #
      # All `run:` steps are static commands with no untrusted
      # github.event.* interpolation, so workflow-injection guidance
      # does not apply here.
      runs-on: ubuntu-latest
      steps:
        - uses: actions/checkout@v4
        - uses: actions/setup-go@v5
          with: { go-version: '1.25' }
        - run: make build-no-viewer
        - run: make eval
        - name: baseline drift (informational)
          if: always()
          run: |
            if ! diff -ru eval/baseline eval/results/latest > /tmp/eval-drift.txt; then
              echo "::warning::eval baseline drift detected (see job artifact for full diff)"
              head -100 /tmp/eval-drift.txt
            fi
        - uses: actions/upload-artifact@v4
          if: always()
          with:
            name: eval-results
            path: eval/results/latest/
            retention-days: 14
  ```

  **설계 근거:**
  - **Gate**: `make eval`의 5번째 step(`ckg eval-retrieval`)이 recall/precision threshold 위반 시 exit 1. job 자체가 fail → PR block.
  - **Non-fatal drift step**: validate/benchmark/bench-mcp의 numeric 변동은 통계적 노이즈(timestamps, src_commit rotation). warning만 띄우고 진행. 실제 회귀 검출은 *retrieval gate*가 부담.
  - **Artifact always**: `if: always()` — fail 시에도 JSON 보존 → 어떤 fixture가 어떤 expected를 잃었는지 console 없이 디버그 가능.
  - **Runner**: ubuntu-latest 단일. eval probes는 OS 비의존(SQLite 자체 결정성), matrix 비용 무가치.
  - **Job 의존성 없음**: lint/audit와 동급, smoke처럼 `needs: test` 사용 안 함. 빠른 실패 피드백.

  **시간 비용 예상:** self-index ~30s + synthetic-index ~10s + bench-mcp 50 iters × 8 probes ~30s + 기타 ~10s = **~1-2분**. lint job(~30초)보다 느리지만 audit/smoke와 비슷 수준.

## G. 미해결 / 후속 세션 작업

- [ ] **C1** W-C 회귀 가드 시리즈 다음 invariant 후보 발굴 (현재 V14~V16까지 누적) — 발견적 작업, 별도 세션
- [ ] **E2** cks 측 워크어라운드 제거 PR — cks repo 작업, 별도 세션
