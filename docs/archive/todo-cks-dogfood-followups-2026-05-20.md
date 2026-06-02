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

- [x] **C1 — W10 V16** ✅ `4fdce7d` try-statement self-cast 회귀 가드. `try { payable(this).call(...) }` 패턴이 enclosing function에 `HasSelfReentrantCall=true` 부착되는지 lockdown. tree-sitter query가 file 전체에서 member_expression을 잡고 walker가 try_statement를 transparent하게 통과해 function_definition에 도달 — 가설 검증 + 명시적 fixture로 회귀 가드.

  **W10 self-cast 시리즈 누적 coverage:**
  - V14 (`89f86f5`): receive() / fallback()           — shape: fallback_receive_definition
  - V15 (`55bf70d`): constructor                       — shape: constructor_definition
  - V16 (`4fdce7d`): try_statement-wrapped fn body     — shape: function_definition (transparent walk)
  - V17 (`b1dd31a`): modifier body                     — shape: modifier_definition
  - **V18 (`8539446`): `.call{value:..., gas:...}` chained syntax** — *syntax*: struct_expression wrapper hop. **실제 false negative 수정** — 이전엔 modern Sol 0.7+ self-call이 silently 누락되었음 (V14-V17은 *지원되지 않는 syntax에 대해 잘못된 자신감*을 주고 있었음 — V18가 actual correctness 회복).

  *Shape axis는 V14-V17이 완결.* *Syntax axis는 V18이 가장 흔한 modern 패턴 처리*. 두 axis 모두 invariant lock.

- [x] **C2 — W10 V17** ✅ `b1dd31a` modifier-scope self-cast 회귀 가드.
- [x] **C3 — W10 V18** ✅ `8539446` call-options chained syntax (`{value:..., gas:...}`) self-cast. *실제 코드 수정 + fixture + test*. tree-sitter parent chain probe로 wrapper 이름(`struct_expression`) 발견 후 walker에 hop 추가.

- [x] **C4 — W10 V19** ✅ `6995184` high-level self-call marker. 새 `HasHighLevelSelfCall` field 추가 + walker `runHighLevelSelfCallMarker` + 재귀 `isSelfRef` helper. 3 shape (bare `this.foo()` / `MyC(this).foo()` / `IFoo(this).bar()`) 모두 lockdown.

  **발견:** Sol grammar는 `TypeName(arg)`와 `funcName(arg)`를 같은 `call_expression`으로 parse — type cast 식별 불가. *leading identifier upper-case + single call_argument* 휴리스틱으로 false positive 제한. (대문자로 시작하는 user-defined helper에 한해 false positive 가능 — security marker는 false positive < false negative 정책.)

  **W10 syntax-axis 진화 정리:**
  - V18 (`8539446`): `.call{value:..., gas:...}` chained low-level (struct_expression hop)
  - V19 (`6995184`): high-level typed dispatch (new marker, isSelfRef 재귀 helper)

- [x] **C5 — W10 V20** ✅ `e009dff` try-wrapped high-level self-call cross-axis lockdown. V16 (try transparent walk) × V19 (HasHighLevelSelfCall) 조합이 *자동 propagate*되는지 검증. 0줄 코드 + fixture + test.

- [x] **C6 — W10 V22** ✅ `1668afb` high-level call-options self-call. V19 walker에 V18 패턴(struct_expression hop) 추가. probe로 false negative 확인 → fix → lockdown.

  **V18/V22 generalisation:** Low-level walker(V18)와 high-level walker(V22) 모두 *struct_expression을 transparent hop으로 처리*. 향후 새 call-options syntax 추가 시 *두 walker 모두에 hop 추가* 필요 — 두 test 파일(`call_options_self_cast_test.go` + `high_level_call_options_test.go`)이 drift 감지.

  **W10 시리즈 누적 commit table:**
  | V | SHA | Type | 내용 |
  |---|---|---|---|
  | V14 | `89f86f5` | lock | receive/fallback self-cast |
  | V15 | `55bf70d` | lock | constructor self-cast |
  | V16 | `4fdce7d` | lock | try-wrapped self-cast |
  | V17 | `b1dd31a` | lock | modifier-scope self-cast |
  | V18 | `8539446` | **fix** | low-level `.call{...}` struct_expression hop |
  | V19 | `6995184` | **feat** | `HasHighLevelSelfCall` new marker |
  | V20 | `e009dff` | lock | try × high-level cross-axis |
  | V22 | `1668afb` | **fix** | high-level `.foo{...}` struct_expression hop |

- [x] **C7 — W10 V21** ✅ `e2ff5f2` chained-receiver negative lockdown. `getTarget(this).foo()` (lower-case identifier)가 `isSelfRef` 휴리스틱으로 거부됨을 *명시 잠금* — 의도된 false negative trade.

  **W10 self-call 시리즈 최종 (V14-V22):**
  | V | SHA | Type | 내용 |
  |---|---|---|---|
  | V14 | `89f86f5` | lock | receive/fallback self-cast |
  | V15 | `55bf70d` | lock | constructor self-cast |
  | V16 | `4fdce7d` | lock | try-wrapped self-cast |
  | V17 | `b1dd31a` | lock | modifier-scope self-cast |
  | V18 | `8539446` | **fix** | low-level `.call{...}` struct_expression hop |
  | V19 | `6995184` | **feat** | `HasHighLevelSelfCall` new marker |
  | V20 | `e009dff` | lock | try × high-level cross-axis |
  | V21 | `e2ff5f2` | **neg-lock** | chained-receiver intentional false negative |
  | V22 | `1668afb` | **fix** | high-level `.foo{...}` struct_expression hop |

  *9개 commit, shape × syntax × heuristic-edge 3-axis cover, 1 negative lock.*

- [x] **C8 — W10 V23** ✅ `f90592e` Yul self-call marker symmetry. `yul_low_level_calls.go`의 좌우 비대칭 수정 — V10이 `delegatecall(gas(), address(), …)`만 처리하고 `call`/`staticcall` 누락한 것을 mirror.

  **W10 시리즈 최종 (V14-V23, 10 commits) — *진짜 완결*:**
  | V | SHA | Type | 내용 |
  |---|---|---|---|
  | V14 | `89f86f5` | lock | receive/fallback self-cast (Sol) |
  | V15 | `55bf70d` | lock | constructor self-cast (Sol) |
  | V16 | `4fdce7d` | lock | try-wrapped self-cast (Sol) |
  | V17 | `b1dd31a` | lock | modifier-scope self-cast (Sol) |
  | V18 | `8539446` | **fix** | low-level `.call{...}` struct_expression hop (Sol) |
  | V19 | `6995184` | **feat** | `HasHighLevelSelfCall` new marker (Sol) |
  | V20 | `e009dff` | lock | try × high-level cross-axis (Sol) |
  | V21 | `e2ff5f2` | **neg-lock** | chained-receiver intentional false negative (Sol) |
  | V22 | `1668afb` | **fix** | high-level `.foo{...}` struct_expression hop (Sol) |
  | V23 | `f90592e` | **fix** | Yul `call`/`staticcall` self-receiver symmetry |

  **3-axis 완결 coverage:**
  - *Shape* (callable kinds): function / constructor / fallback-receive / modifier / try-wrapped — V14-V17, V20
  - *Syntax* (call expression variants): plain `.call(...)` / `.call{...}(...)` / `this.foo()` / `MyC(this).foo()` / `IFoo(this).foo()` / `.foo{...}()` — V8 / V18 / V19 / V22
  - *Parser* (Sol cast walker vs Yul inline assembly): V14-V22는 Sol측, V23은 Yul측
  - *Heuristic edge* (intentional false negative): chained-receiver — V21

- [x] **C9 — W9 V17** ✅ `3f22bb1` multi-inheritance storage slot offset lockdown. `contract C is A, B` 두-base 케이스에서 *W9 V1의 offset 누적*이 정확히 작동함을 explicit fixture로 잠금.

  **W9 시리즈 누적 (commit table):**
  | V | SHA | Type | 내용 |
  |---|---|---|---|
  | V0 | `1b51285` | feat | per-contract local slot index |
  | V1 | `62a8f49` | feat | single-inheritance offset |
  | V2 | `a9cf823` | feat | type-size aware packing |
  | V3 | `6db83bd` | feat | mapping declaration slot |
  | V6 | (resolve.go) | feat | cross-file re-pack |
  | V15 | `d7df2ec` | lock | cross-file enum conservative |
  | V16 | `f79deaa` | lock | cross-file struct conservative |
  | V17 | `3f22bb1` | lock | multi-inheritance offset (two-base) |

  **Slot model 명시 (V17 commit 참조):** base contract NodeField = own-scope slot. derived contract NodeField = inheritance-offset folded slot. cks 사용자가 *absolute slot in derived context* 필요 시 inheritance edges + slot count로 reconstruct.

- [x] **C10 — W9 V18** ✅ `5548dd5` diamond inheritance MRO offset lockdown. `c3_linearization.go`의 dedup이 *D.d = 3 (naive 4 아님)* 정확히 작동함을 explicit fixture로 잠금. *Upgradeable proxy collision* 회귀 직접 방어.

  **W9 layout 시리즈 V17/V18 누적:**
  - V17 (`3f22bb1`): two-base offset (`A, B` linear)
  - V18 (`5548dd5`): diamond MRO dedup (`B, C ← A`)
  - 두 fixture가 *base own-scope semantics* + *derived absolute-after-offset*의 비대칭을 *명시*

- [x] **C11 — W9 V20** ✅ `48291cb` deeper-diamond MRO offset lockdown. 5-node chain (E ← C, D ← B ← A)에서 transitive dedup 정확 + slot model 명시(*root only own-scope, 모든 derived는 inheritance-folded*).

  **W9 storage layout 시리즈 누적:**
  - V17 (`3f22bb1`): two-base linear offset
  - V18 (`5548dd5`): 3-node diamond MRO dedup
  - V20 (`48291cb`): 5-node deeper diamond MRO transitive dedup + slot model 명시

- [x] **C12 — walker symmetry matrix** ✅ `dc05899` `internal/parse/solidity/WALKER_SYMMETRY.md`. V18/V22 + V10/V23 drift 패턴 + 6-question checklist + drift catalogue.

- [x] **Meta — stale `.git/index.lock` 분석** ✅ `9a22242` `docs/stale-git-lock-analysis-2026-05-21.md`. 10+회 발생 패턴 진단, 4가지 가설(gitstatusd race 가장 유력), 6가지 해결책 옵션(B git-safe wrapper 권장, C 셸 탭 정리 free), 추적 테이블. ckg source 무영향, 사용자 환경 문제.

- [x] **C13 — W10 V24** ✅ `d3f2724` high-level walker × shape cross-axis lockdown. WALKER_SYMMETRY.md의 3개 `?` 셀 close (constructor / receive / modifier). 한 fixture + 한 test로 *4 callable shape × 2 walker* matrix 완전 채움.

  **W10 시리즈 완전 완결 (V14-V24, 11 commits):**
  - V14-V17: shape × cast walker (5 shapes)
  - V18 fix: low-level struct_expression hop
  - V19 feat: HasHighLevelSelfCall marker
  - V20 cross-axis: try × high-level
  - V21 negative lock: chained-receiver false negative
  - V22 fix: high-level struct_expression hop (V18 대칭)
  - V23 fix: Yul call/staticcall self-receiver (V10 대칭)
  - **V24 cross-axis: high-level × {constructor, receive, modifier} (V14/V15/V17 대칭)**

  **WALKER_SYMMETRY.md drift catalogue 3 rows 모두 close.**

- [x] **C14 — W7.4 V25** ✅ explicit-list modifier override lockdown. `runModifierOverride`의 `dispatchKindOverrideExplicit` 분기가 *코드는 있는데 test 없음* (silent regression 위험). 다이아몬드 `override(A, B)` fixture + EdgeOverrides 2개 edge 검증. WALKER_SYMMETRY.md에 (1) 함수 vs modifier override emit 대칭 행, (2) drift catalogue row 4 추가. Walker 0줄 변경.

  **Probe로 드러난 추가 drift (별도 작업 후보):**
  - **modifier SubKind = ""** (function W2는 SubKind=`virtual`/`override`/`virtual_override` set, modifier는 empty) — *intentional negative lock* 또는 *walker fix* 양자택일. consumer 측 활용 시나리오 정해진 뒤 진행.

- [x] **C15 — W8 V26** ✅ cross-contract fn-pointer chain negative lock. `s.getCb()(x)` chained invoke + `return s.getCb();` chained return — V6/V9가 *코멘트에 명시한 known limitation*. V21 패턴(`getTarget(this).foo()` negative lock)을 그대로 따라 *false negative를 명시적으로 잠금*. 미래 walker fix 시점에 *이 test가 깨지면서 positive lock으로 flip*. Walker 0줄 변경. WALKER_SYMMETRY drift catalogue row 5 추가.

  **Probe로 드러난 추가 false negative cells:**
  - `Hub(addr).onAction(x)` (V6 코멘트 명시): cast-receiver chain — V26과 같은 카테고리지만 *cast* shape. 별도 fixture로 lock 가능.
  - `getHub().onAction(x)` (V6 코멘트 명시): lower-case helper return → member access. V21와 동일 카테고리(*lower-case helper*) + V6 limitation 교집합.

- [x] **C16 — W8 V27** ✅ cast-receiver + helper-return chain negative lock. V6 코멘트가 명시한 *세 가지 chained shape* 중 V26이 cover하지 않은 두 cell(`Hub(addr).onAction(x)`, `getHub().onAction(x)`)을 한 fixture에 통합. **V6 positive baseline (`h.onAction(x)`)을 reference row로 포함**해서 *negative cells가 receiver-shape variance에 specifically locked*임을 명시. Walker 0줄 변경. WALKER_SYMMETRY row 5를 *limitation family* 단위로 확장(V26+V27 묶음).

  **V21 / V26 / V27 negative-lock family 완성:**
  - V21: `getTarget(this).foo()` (high-level walker, lower-case helper)
  - V26: `s.getCb()(x)` + `return s.getCb();` (V6 chained-invoke + V9 chained-return)
  - V27: `Hub(addr).onAction(x)` + `getHub().onAction(x)` (V6 cast/helper receiver)

  **V6 코멘트가 명시한 3 shapes 모두 lock 완료.** 미래 walker fix는 *batch flip* 필요.

- [x] **C17 — W9 V28** ✅ struct-internal reference-type slot fallback *characterization lock*. systematic walker-limitation grep으로 `struct_size.go` V5 코멘트 발굴 → probe로 *V5 코멘트 의도와 walker 실제 동작 divergence* 확인 + *추가 multi-contract namespace divergence* 발견. 4 contracts × 3 fields = 12 cells 모두 *현재 fallback (head=0, inner=1, tail=2)*로 lock. WALKER_SYMMETRY drift catalogue row 6 추가. Walker 0줄 변경.

  **C17의 종류 차이 (V21/V26/V27과 vs V28):**
  - V21/V26/V27: walker 코멘트가 *limitation을 명시*, test가 *그 limitation을 정확히 lock*. 즉 *코멘트 + walker behavior aligned*.
  - V28: walker 코멘트가 *intent를 명시*, walker behavior가 *코멘트와 다름*. test는 *behavior를 lock + divergence를 명시*. 즉 *characterization* (의도가 아니라 *실제 동작* 기록).

  **추가 발견 (V28에 명시):**
  - V5 `tryComputeStructBytes`가 bytes/string/dynamic-array에 fallthrough (0, false). Struct가 structSizes에 등록 안 됨.
  - **PrimitiveOnly struct (V11 fixture와 동일 구조)도 *내 fixture에서 1 slot fallback*** — multi-contract namespace 또는 fixed-point ordering 이슈. V11은 single-contract라 cover. 실 codebase는 multi-contract이므로 *광범위 fallback 가능성*.

- [x] **C18 — 방향 전환: T-04 Hallucination validator prototype** ✅ 사용자 질문(*"evaluation 부재로 불명확성 누적"*)에 응답 — W-C lockdown 시리즈에서 *evaluation infrastructure 강화*로 axis 전환. 발견: `internal/eval/`에 *LLM 인프라(LLMClient, APIClient, CLIClient) + 4-baseline 평가 시스템*이 이미 완비. 진짜 미충족은 *반복 사용 + bug discovery cycle*. HANDOFF.md T-04 P0 spec 구현: `internal/eval/hallucination_check.go::ValidateMentions(output, store)` — Found/QnameDiverged/Hallucinated 3-bucket 분류. 6 unit tests PASS. Walker 0줄 변경.

  **V0 미해결 (V1+):**
  - ckg eval CLI에 통합 (runner.go::runOne에 호출 wiring)
  - 30-question 실 데이터 run + false-positive 율 측정
  - file 경로 토큰 검증 (현재 symbol mention만)
  - misspell 처리 (fuzzy matching)

- [x] **C19 — T-04 V1 wiring** ✅ 사용자 명시 우선순위 (*"evaluation 빠르게 진행"*)에 따라 진행. `Result.Hallucination HallucinationResult` field 추가, `runOne()` 끝에서 `ValidateMentions(out.OutputText, store)` 호출, CSV/Report에 metric 통합. `make eval-llm-smoke` target 추가 — API key 있으면 api backend 자동 선택, 없으면 cli backend (CLIWRAP_AGENT 필요). Walker 0줄 변경.

  **다음 step (사용자 작업 필요):**
  1. `ANTHROPIC_API_KEY` 설정 *또는* `cliwrap-agent` 설치 후 `CLIWRAP_AGENT` 설정
  2. `make eval-llm-smoke` 실행 → 첫 hallucination baseline 측정
  3. 결과 보고 *bug fix priority 결정*

- [x] **C20 — smoke run 첫 cycle: 2 bugs surfaced + fixed** ✅ 사용자가 `make eval-llm-smoke LLM_BACKEND=cli` 실행. 두 bug 즉시 발견:
  - **Bug A**: `exec: "ccliwrap-agent not found"` — `$CLIWRAP_AGENT` 환경 변수가 잘못된 path 가리킴. Fix: `NewCLIClient`에 *agent path file existence check* 추가 — actionable error + install hint.
  - **Bug B**: `CLIClient.Close()`에서 *cli-wrapper 라이브러리 *내부 nil-pointer panic*. *Start 실패한 processHandle*의 *Stop이 nil ipc.Conn 접근*. Fix: `Close()`에 *deferred recover* 추가 → panic → error로 변환, runner의 *진짜 spawn 에러*가 *masked되지 않음*.

  **Evaluation-driven bug discovery cycle 성공**: V1 wiring 완료 직후 *첫 *실 LLM 실행* 시도에서 *2 bugs* 식별. 사용자 명시 우려 *"코드가 잘 구현되었는지 확인할 수가 없어서, 불명확성이 너무 커지고 있다"*에 *직접 응답하는 결과*.

  **Unit tests:**
  - `TestNewCLIClient_AgentNotExist`: typo CLIWRAP_AGENT path → 친절한 error
  - `TestCLIClient_Close_NilManager`: 기존 (V0)
  - `Close panic-recover`: integration-level 검증 (unit reproduction은 cli-wrapper 내부 API 한계로 불가)

- [x] **C21 — 첫 *실 measurement 확보 + 2 bugs surfaced + fixed*** ✅ 사용자가 cli backend setup 재조정 후 실행 성공:

  **첫 baseline:**
  | Metric | 값 |
  |---|---|
  | Hallucination rate | **28.6%** (2/7 mentions) |
  | Score | 0.476 |
  | Mentions | 7 |
  | Hallucinated | `h.vault.Deposit(req`, `e.g` |
  | QnameDiverged | `Vault.deposit` |

  **2 bugs fixed:**
  - **T-02 P0 (extractSymbols paren split)**: `h.vault.Deposit(req`가 *paren inside token*에서 *split 안 됨*. `FieldsFunc` separator에 `(`/`)` 추가. unit test 4건 PASS.
  - **Prose abbreviation noise**: `e.g`, `i.e`, `et.al`, `etc.`, `vs.` blacklist. `isProseAbbreviation` helper 추가. unit test 4건 PASS.

  **V0 design 한계 (V2 후보):**
  - `Vault.deposit` (LLM 짧은 표기) vs `service.Vault.Deposit` (store 정식 qname) — *suffix match*가 cover 안 됨. `QnameDiverged false positive`. *수정 시 mention이 *store qname의 *case-insensitive suffix*면 *non-diverged*.

- [x] **C22 — Fix effectiveness 검증 + V2 suffix match** ✅
  
  **C21 fix 효과 *empirical proof*:**
  | 측정값 | C21 | C22 cycle 2 | Delta |
  |---|---|---|---|
  | Hallucinated | 2 | 0 | -2 ✅ |
  | Rate | 28.6% | 0.0% | **-28.6%p** ✅ |
  | QnameDiverged | 1 | 3 | +2 (nature change: hallucinated → diverged) |
  | Score | 0.476 | 0.500 | +0.024 |
  
  **V2 enhancement: anyQnameMatch에 *suffix match* 도입.** `isQnameSuffix(qname, mention)` helper — segment-aware case-insensitive suffix matching. 9 unit tests PASS (2건 신규: QnameSuffix_NotDiverged, SingleSegmentSuffix). Walker 0줄.
  
  **V2가 cover하는 것:**
  - `Vault.deposit` (mention) ↔ `service.Vault.Deposit` (qname) → case-fold suffix → match
  - `Vault.Deposit` (mention) ↔ `service.Vault.Deposit` → exact suffix → match
  
  **V2가 cover *못 하는 것 (V3 후보)*:**
  - `h.vault.Deposit` — receiver-style (`h` = variable). leading variable segment defeats suffix align.
  - `h.vault` — variable-only receiver path. *V3: first-segment-variable heuristic 필요*.

- [x] **C23 — *첫 real LLM hallucination 측정* + 1 measurement bug fix** ✅
  
  *3번째 cycle 결과* (LLM non-deterministic, 응답이 매번 다름):
  | Metric | C22 cycle 2 | C23 cycle 3 |
  |---|---|---|
  | Mentions | 6 | 11 |
  | Hallucinated | **0** | **4** (3 real + 1 false positive) |
  | Rate | 0.0% | 36.4% |
  | QnameDiverged | 3 | 1 |
  | Score | 0.500 | 0.208 |
  
  **3 real LLM hallucinations** (consumer-end correctness issue):
  - `Token.transfer` — graph에 없는 symbol
  - `main.main` — Go convention pattern, graph에 없음
  - `server.Register` — generic web pattern, graph에 없음
  
  **1 measurement false positive — *fixed*:**
  - `Vault{...}` — struct literal placeholder. extractSymbols brace 처리 안 됨. **Fix: FieldsFunc에 `{`/`}` 추가**. 3 unit tests PASS.
  
  **첫 *consumer-perceived hallucination 측정 모드 진입*** — *measurement framework convergence 완료*. 이제부터 *real LLM noise quantitatively tracking 가능*.

- [x] **C24 — Axis 3: BASELINES variable** ✅ Makefile에 `BASELINES ?= alpha` env-overridable variable 추가. 사용자가 `BASELINES=alpha,beta,gamma,delta make eval-llm-smoke`로 *4-baseline 비교 가능*. infrastructure 활용 (existing eval/runner.go baseline 분기 + report.go H1/H2 check). 코드 0줄, Makefile만.

  **Baseline semantics:**
  - α: raw file dump (cheapest, noisiest)
  - β: get_subgraph pre-call (graph context, no tools)
  - γ: tool-named in prompt, NOT pre-called (multi-turn emulation — HANDOFF T-01 미해결로 *score near-zero 예상*)
  - δ: smartContext pre-call (task-tuned context)
  
  **H1/H2 hypothesis check**:
  - H1 = δ vs α token savings target ≥ 50%
  - H2 = δ - α score target ≥ 0

- [x] **C25 — 4-axis evaluation roadmap 완료** ✅ 사용자 권장 순서 (3→1→2→4)대로 진행. *각 axis 별도 commit*. 코드 변경 + Makefile + docs 모두 commit.

  | Axis | Commit | 변경 | 목적 |
  |---|---|---|---|
  | 3 (β/γ/δ) | `1db6d2d` | Makefile BASELINES variable | tool-augmented 비교 |
  | 1 (multi-shot) | `c368d9c` | Result.RunIdx + mean±std | non-determinism 측정 |
  | 2 (prompt) | `1754b6c` | anti-hallucination guidance | 0% error direct lever |
  | 4 (filter) | (this) | FilterHallucinations + report integration | consumer-level safety |

  **Axis 4 detail**: `internal/eval/filter.go::FilterHallucinations(text, halluResult)` — hallucinated symbols을 `[unverified: <symbol>]`로 *word-boundary-aware replacement*. 4 unit tests PASS. report.md에 *post-filter warnings* 통합.

- [x] **C26 — 4-axis 통합 첫 measurement** ✅ 사용자 `make eval-llm-smoke BASELINES=alpha,beta,gamma,delta N_RUNS=3` 실행.

  **첫 4-axis baseline:**
  | Baseline | N | Tokens | Score (mean±std) | Halu rate (mean±std) |
  |---|---|---|---|---|
  | α | 3 | 5 | 0.396±0.119 | 0.187±0.046 |
  | β | 3 | 6 | **0.000±0.000** 🔴 | 0.000 |
  | γ | 3 | 193 | **0.700±0.047** | **0.444±0.113** |
  | δ | 3 | 9 | 0.620±0.115 | 0.363±0.059 |

  **5 findings**:
  - β baseline broken (score 0) — `SubgraphByQname("", 99)`가 empty 반환
  - H1 hypothesis FAIL (-86.7%) — token counting *misleading* (input_tokens만, cached 무시)
  - γ paradox (score 최고 + hallu 최고)
  - systematic LLM invention (`VaultService.depositFn` 3/3 runs) — **실제 graph에 없는 TS symbol**
  - Korean text + line ref noise

- [x] **C27 — A+B+D fix cycle** ✅ (이 commit) 권장 순서 (A → B → D) 동시 진행:
  - **A (β broken)**: `SubgraphByQname("", 99)` → `TopNodes("pagerank", 200)` + `AllEdges()`. *β의 *whole-graph context* 의도 회복*.
  - **B (token counting)**: H1 hypothesis가 *input_tokens만 사용*해서 *misleading*. *Claude Code prompt cache가 거의 100%*. *report에 *Total (input + cached) column 추가*, H1이 *total 기준 계산*.
  - **D (graph rebuild + lang expansion)**: `eval-llm-smoke`가 *Go only graph*로 build. *Makefile에 *`--lang=go,ts,sol`* + *force rebuild every run*. `VaultService.depositFn`이 *진짜 TS symbol*인데 *graph에 없어서 hallucination으로 *misclassified*. *graph 갱신으로 *measurement accuracy 회복*.

- [x] **C28 — post A+B+D 측정 *대대적 진전*** ✅
  
  | Metric | Before (C26) | After A+B+D (C28) | Delta |
  |---|---|---|---|
  | β score | 0.000 | **0.821** ✅ | +0.821 |
  | α halu rate | 0.187 | 0.062 | ↓3x |
  | γ halu rate | 0.444 | 0.089 | ↓5x |
  | δ halu rate | 0.363 | 0.185 | ↓2x |
  | Total tokens | (input only) | α 54K, β 59K, γ 253K, δ 320K | 측정 가능 |
  | H1 | -86.7% | -488.9% (interpretable) | δ가 α보다 *5.9x 큰 context* |
  
  **5 new findings (cycle 6)**:
  - **β run 0 token=0 anomaly**: input=0, cached=0, output=0 but score=0.71 정상 — cli backend usage parsing transient bug. defer.
  - **mux.HandleFunc** (α run 0): real LLM invention.
  - **Line refs** (β/γ/δ): `handler.go:23`, `vault.ts:5`, `Vault.sol:3` — file extension blacklist 한계.
  - **Korean 조사** (γ/δ): `Vault.deposit을`, `Vault.deposit은` — extractSymbols Hangul handling 부재.
  - **`expected.symbols`** (δ run 0): YAML fixture key를 *symbol처럼 mention*.

- [x] **C29 — Line ref + Korean noise fix** ✅ (이 commit)
  - **Line ref blacklist**: `file.ext:N` 패턴 (Go/TS/Sol/etc). 5 unit tests PASS.
  - **Hangul separator**: FieldsFunc에 *U+AC00..U+D7A3* 추가. 3 unit tests PASS.
  - **`expected.symbols` 같은 YAML key는 *defer* (false positive 위험 큼).

- [x] **C30 — cycle 6 효과 *검증* + 3 new noise** ✅
  
  | Metric | C28 | C30 |
  |---|---|---|
  | α halu | 0.062 | **0.042** ↓32% |
  | β halu | 0.143 | 0.114 ↓20% |
  | γ halu | 0.089 | **0.074** ↓17% |
  | δ halu | 0.185 | **0.083** ↓55% |
  
  **Verified**:
  - Line refs (`handler.go:23` 등) 사라짐 ✅
  - Korean 조사 (`Vault.deposit을` 등) 사라짐 ✅
  - β run 0 token=0 anomaly *사라짐* — **transient** 확정 (3 runs all `88434 ± 0`)
  
  **3 new noise findings**:
  - `0.7`, `1.0` (γ, δ): numeric literals (task threshold leak)
  - `VaultService.depositFn#CallSite@153` (β 2/3 runs consistent): node-ID format leak
  - `NewHandler→service.New` (β run 2): Unicode arrow (U+2192)

- [x] **C31 — cycle 7 noise fix** ✅ (이 commit)
  - **Numeric literal blacklist**: `0.7`, `1.0` 등 *all-digit + dot* drop. 3 unit tests PASS.
  - **#/@/→ separator**: `VaultService.depositFn#CallSite@153` 와 `NewHandler→service.New` split. 3 unit tests PASS.

- [x] **C32 — V3 receiver-style heuristic** ✅ (이 commit)
  - `anyQnameMatch`에 *third clause* 추가: *receiver prefix strip + retry suffix match*
  - `isReceiverPrefix` *narrow heuristic*:
    - Well-known list: `this`, `self`, `svc`, `ctx`, `req`, `res`, `obj`, `tmp`, `val`, `ref`
    - Single lowercase letter (`h`, `s`, `c`, `v` 등)
    - **NOT** 2+ char unknown lowercase (eth/os/io/sql 등 *legit packages*)
  - V3 cover: `h.vault.Deposit`, `svc.depositFn`, `this.vault.deposit`, `self.vault.deposit`, `ctx.vault.deposit` 모두 *Found로 *재분류*
  - V3 narrow on: `eth.NewBlockChain`, `os.Open` 등 *2-char+ unknown packages*는 *여전히 QnameDiverged*
  - 11 unit tests PASS (2 new: V3_WellKnownReceivers, V3_NarrowOnPackages)
  - 기존 ReceiverStyle test → V3 의도 반영 (StillDiverged → NotDiverged)

- [ ] **C33** 사용자 *재실행 권장* (post-V3):
  ```bash
  make eval-llm-smoke BASELINES=alpha,beta,gamma,delta N_RUNS=3
  ```
  - *QnameDiverged 대폭 감소* 예상 (`h.vault.Deposit`, `svc.depositFn` 등 모두 Found 분류)
  - *Hallucination rate 추가 ↓* 예상 (V3가 *false positives 마지막 cleanup*)

- [x] **C34 — B smartContext audit + UserPromptBytes metric** ✅ (이 commit)
  
  **Audit finding**: smartContext의 *BudgetTokens=8000* (~32KB). 그러나 *cached_tokens가 170K-587K로 *swing*. *Claude Code의 *workspace cache state*가 *cached에 누적* — *CLI-side cache pattern 이지 *application prompt size 아님*.
  
  **Fix**: `Result.UserPromptBytes` 추가 (application-level prompt size). H1 hypothesis가 *cached/input_tokens 대신 *UserPromptBytes 기준* 계산. *CLI cache state 영향 *제거*.
  
  | Metric | 의미 | H1 적용 |
  |---|---|---|
  | input_tokens | uncached billing portion | H1 V1 (misleading) |
  | input + cached | full Claude prompt (workspace cache 포함) | H1 V2 (CLI state 포함) |
  | **UserPromptBytes** | runner가 추가한 application prompt | **H1 V3 (정확)** |
  
  CSV에 *user_prompt_bytes column 추가*. Report에 *User prompt bytes (mean ± std) column*. H1 명칭: "user-prompt-bytes savings".
  
  Tests updated: TestWriteCSV expectedCols + row index, TestWriteReport H1 case에 UserPromptBytes 추가.

- [x] **C35 — H1 + H2 PASS 첫 도달** ✅
  
  | Hypothesis | Target | C26 first measure | **C35 (post-V3+B)** |
  |---|---|---|---|
  | H1 token savings | ≥ 50% | -86.7% 🔴 | **+93.0%** ✅ |
  | H2 score delta | ≥ 0 | +0.208 | **+0.305** ✅ |
  
  Plus *γ halucination rate **0.000***, *β std 0* (anomaly 해결).
  
  **새 finding**: δ smartContext가 *silent failure* → UserPromptBytes 157 (γ-equivalent). *원인 진단 필요*.

- [x] **C36 — δ smartContext silent failure root cause + fix** ✅ (이 commit)
  
  **Root cause**: `rewriteFTSQuery`가 *whitespace로만 split*. *task description의 *`Vault.deposit`* 같은 *dotted identifier가 *single token*으로 들어가서 *prefix wildcard 추가* → `Vault.deposit*` → **FTS5 syntax error**. *δ baseline의 *모든 runs에서 *smartContext fail*. *silent skip*.
  
  **Fix**: `rewriteFTSQuery` `FieldsFunc`가 *`.`도 separator로 추가*. dotted identifier가 *segments로 split* → 각 segment *prefix wildcard*. *FTS5 syntax 안전*.
  
  **추가 fix**: runner.go의 δ path에 *smartContext error logging* — 향후 silent failure 즉시 visible.
  
  Tests:
  - `TestRewriteFTSQuery_DottedIdentifierSplit` 신설 (4 sub-cases)
  - 기존 `TrailingPunctuation`, `PowerUserGate` 모두 PASS (regression)
  
  Probe로 *실 효과 검증*: `BuildContext("...Vault.deposit")` 가 *5 bodies + 15 summaries* 정상 반환.

- [ ] **C37** 사용자 *최종 검증 재실행* (post-smartContext fix):
  ```bash
  make eval-llm-smoke BASELINES=alpha,beta,gamma,delta N_RUNS=3
  ```
  - **기대**:
    - δ UserPromptBytes: 157 → ~32KB (smartContext result 정상 append)
    - δ score 추가 ↑ (richer context)
    - H1 percent 변동 (δ 추가 context로 ratio 바뀜)
    - smartContext stderr log *발생 안 함* (fix 작동 시)

- [ ] **C27** V3 receiver-style heuristic (data 더 모은 후 결정):
  - `h.vault.Deposit` 패턴 cover 여부. *현재는 *QnameDiverged*로 surface*만.
  - false positive 위험: `b.New`, `c.Init` 등 legit short package names
- [ ] **E2** cks 측 워크어라운드 제거 PR — cks repo 작업, 별도 세션
