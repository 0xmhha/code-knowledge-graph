# CKS 통합 관점 추가 정의 — 2026-05-23

> 본 문서는 ckg 외부 (cks repo) 에서 진행된 통합 점검 세션의 결과를 ckg 측 문서에
> back-port 한 것이다. 작업 책임은 이 repo 의 현 작업자 (HANDOFF.md T-* 시리즈
> 진행자) 에게 있고, 본 섹션은 *컨텍스트 + 추가 요구사항 + 권장 작업 명세* 만
> 제공한다.
>
> **trigger**: cks 측에서 ckv/ckg/cks 통합 작업 우선순위를 점검하다가 사용자가
> *ckv 의 진짜 역할*을 vector-only 가 아닌 **"한국어/모호 표현 → 코드 정확
> 키워드 변환 (vocabulary bridge)"** 으로 명시. 이로 인해 ckg 가 받는 입력의 가정
> (영문 keyword) 이 명확해지고, 동시에 ckg 가 *cks 통합을 위해* 추가로 제공해야 할
> 표면이 식별됨.
>
> **관련 문서 (반드시 cross-link)**:
> - cks: `code-knowledge-system/docs/research/knowledge-data-best-practice-2026-05-22.md`
> - ckv: `code-knowledge-vector/docs/evaluation-design-2026-05-22.md` §10 (cks 통합 관점 추가 라운드)

---

## 0. Executive Summary

ckg 의 현 작업 (5/11 HANDOFF + 5/19~5/23 cycle 6/7/8 + T-04 V0~V3) 은
*이미 사용자 시나리오의 70%를 cover* 한다. cks 통합 관점에서 *추가로* 필요한
것은 다음 5 영역으로 좁혀진다:

1. **PR-aware 옵션 A** — symbol-level PR breadcrumb (사용자 명시 5/22, ckv §10.5)
2. **Stage B 평가** — ckv fixture 12 개의 ckg-side mirror (사용자 명시 "작은 단위부터 검증")
3. **Public surface 정리** — T-14 의 `pkg/mcphandlers/` + ckg-NEW-4 의 PR accessor (cks import 차단 해소)
4. **한국어 query 토크나이저 검증** — cycle 6 의 Hangul separator 는 *답변 파싱*용. *FTS5 / qname matching* 측은 별도 검증 필요
5. **CKG-3 cross-snapshot 재평가** — cks fixture 12 개가 *base_sha → head_sha* 시간축을 명시적 활용. 보류 사유 (cks 시나리오 부재) 해소됨

본 문서는 HANDOFF.md / EXECUTION_STRATEGY.md 의 *대체* 가 아니라 *추가*. T-02 ~
T-15 진행은 그대로 유지. T-14 와 ckg-NEW-2~4 는 *같은 public-surface 영역*이라
함께 진행이 효율적.

---

## 1. 사용자 명세 추가 (cks 인터랙션, 2026-05-22)

`stablenet-ai-agent` HLD 의 기존 가정 (CKS deep-dive §11 KPI) 에 사용자가
cks 통합 세션 (2026-05-22) 에서 추가한 요구:

| 신규 요구 | 본질 | ckg 측 영향 |
|---|---|---|
| **R9: Vocabulary bridge** | 사람의 모호/한국어 → 코드 정확 키워드 변환 | **ckv 책임** — ckg 는 *영문 keyword 받는* 가정 그대로. 단 *한국어 query 가 들어와도 깨지지 않게* (graceful degradation) 검증 필요 |
| **R10: Multi-stage evaluation** | E1 intent / E2 location / E3 plan / E4 code 4 단계 분해 | ckg 는 **E2 location identification** 책임 — 정확 keyword → 정확 file/symbol. 기존 retrieval fixture (5/5 baseline) 가 그대로 활용 |
| **R11: 점진 fixture 학습** | F1~F4 (interactive CLI → MCP feedback → web UI → PR) | ckg 가 *fixture record API* 제공하면 ckv 가 wrap. 우선순위 낮음 — Stage C 단계 |
| **R12: PR-aware retrieval** | "왜 이렇게 고쳤어?" 류 query 대응 | **ckg PR-aware 옵션 A (symbol-level breadcrumb) 가 핵심** — 사용자 결정 A+B+C 모두 채택. ckg 가 A 의 backend |
| **R13: 3-leg BM25 임시 적용** | ckv/ckg/cks 모두에 BM25 임시 + 평가로 결정 | ckg `pkg/bm25.Scorer` 는 이미 production-quality. ckv 가 import 가능한 형태로 *공개 surface 보장* 필요 |

핵심 관찰: ckg 의 *공개 surface* (T-14) 가 cks 통합 차단 요소였는데, 사용자가 추가
요구한 *PR breadcrumb + qname canonical + BM25 import* 도 같은 public-surface 영역.
따라서 T-14 작업 중 *3 가지 추가 surface* 를 함께 고려.

---

## 2. ckg 현재 상태 매트릭스 (5/22 기준, cks 통합 관점)

| 사용자 요구 (cks 시나리오) | ckg 현재 capability | gap | 대응 작업 |
|---|---|---|---|
| 영문 keyword → callers/callees | T01 baseline (find_callers depth=1 정확) | depth>1 회귀 test 없음 | **T-12** (기존) |
| 영문 keyword → file/symbol | EV1 Phase 2 retrieval (5/5 fixture baseline) | stable-net corpus 대상 미측정 | **ckg-NEW-5** (Stage B fixture mirror) |
| 한국어 query 그대로 입력 | cycle 6 Hangul separator (답변 파싱만) | FTS5 / qname matcher 한국어 동작 미검증 | **ckg-NEW-1** (tokenizer 검증) |
| PR description 검색 | 없음 (ckv 책임) | — (out of scope) | — |
| Symbol → 최근 변경 PR list | 없음 | metadata + git log scan | **ckg-NEW-2/3/4** (PR-aware A) |
| qname canonicalization | CKG-6 ✅ (`pkg/store.Reader`) | cks 가 wrap 할 정확 호출 방식 문서 부재 | **ckg-NEW-6** (호출 가이드) |
| BM25 score (cross-tool) | `pkg/bm25.Scorer` interface 완성 (CKG-1 ✅) | ckv import path 합의 안 됨 | **ckg-NEW-9** (module 안정성 보증) |
| Hallucination 자동 측정 | T-04 V0~V3 진행 중 | 30-question 셋 측정 미실시 | **T-04 V4** (기존 진행) |
| Cross-snapshot 검색 | CKG-3 보류 (cks 시나리오 부재) | cks fixture 12 개가 base_sha → head_sha 명시 사용 | **ckg-NEW-7** (CKG-3 재평가) |
| Citation accuracy KPI | §11 100% 측정 ✅ | stable-net 12 fixture 측정 미실시 | **ckg-NEW-5** 와 동시 |
| EvidencePack 표면 | `pkg/smartctx`, `pkg/evidence` | S1 (cks) 이관 결정 (EXECUTION §2) | 유지 — 이관 시 deprecate-friendly |

---

## 3. ckg 신규 작업 (cks 인터랙션 도출)

HANDOFF.md T-* 외에 다음 작업이 *cks 통합 + 사용자 R9~R13 충족*에 필요:

| ID | 작업 | 사용자 명세 | LOC | 의존 | 우선 |
|---|---|---|---|---|---|
| **ckg-NEW-1** | 한국어 query 토크나이저 검증 (FTS5 + qname matcher) — 깨지지 않는 graceful degradation 보장 | R9 | ~80 | 없음 | P1 |
| **ckg-NEW-2** | `pkg/store.Node` 에 `RecentPRs []PRRef` 메타 추가 — symbol-level PR breadcrumb 데이터 | R12 (A 옵션) | ~120 | 없음 | P0 |
| **ckg-NEW-3** | Temporal slicing — `Node.RecentPRsBefore(cutoff time.Time) []PRRef` | R12 (leakage 방지) | ~50 | NEW-2 | P0 |
| **ckg-NEW-4** | `pkg/store` public accessor for PR breadcrumb — cks 가 wrap 할 형태 | C2/C4 (cks fusion) | ~50 | NEW-2 | P0 |
| **ckg-NEW-5** | ckv fixture 12 개의 ckg-side mirror — Stage B 평가 task YAML | R10 | YAML 만 | T-04 V4 | P0 |
| **ckg-NEW-6** | qname canonical helper 사용 가이드 (cks wrap 시 정확 호출 패턴 문서) | R10 | docs 만 | 없음 | P1 |
| **ckg-NEW-7** | CKG-3 cross-snapshot 정책 재평가 (cks 시나리오 도착) | R12 (시간축) | 결정 | NEW-3 | P1 |
| **ckg-NEW-8** | Stage B evaluation harness — ckv fixture 12 개를 영문 keyword 변환 후 ckg 평가 | R10 (Stage B 분리) | ~150 | NEW-5 | P0 |
| **ckg-NEW-9** | `pkg/bm25.Scorer` 외부 import 안정성 보증 — semantic versioning + integration test | R13 (3-leg BM25) | ~50 | 없음 | P1 |

총 ~500 LOC + YAML. **T-14 (pkg/mcphandlers 신설) 와 ckg-NEW-2~4 / ckg-NEW-9 는
같은 public-surface 영역** — 함께 진행이 효율적.

### 3.1 ckg-NEW-1: 한국어 query 토크나이저 검증

cycle 6 에서 *답변 토큰화* 측에 Hangul separator 추가됨 (`extractSymbols`).
그러나 *query 입력* 측 (FTS5 + qname matcher) 의 한국어 동작은 미검증.

**검증 시나리오** (`internal/persist/tokenize_test.go` 또는 신설):

```go
// Query 가 한국어를 포함할 때 ckg 가 깨지지 않아야 함 (graceful degradation)
TestFTS5Query_KoreanInput_Graceful(t *testing.T) {
    // 가정: vocab bridge (ckv) 가 모든 한국어를 영문으로 변환한다.
    // 그러나 *바이패스 케이스* 가 있을 수 있으므로 ckg 가 한국어를 직접 받아도
    // panic 하지 않고 *결과 없음* 으로 처리해야 한다.
    cases := []string{
        "AnzeonTipEnv 한국어 혼합",
        "0번 블록",                  // 순한국어
        "WBFT (합의 알고리즘)",       // 영문 + 한국어
    }
    for _, q := range cases {
        // FTS5 query 가 syntax error 안 나야 함
        _, err := store.SearchFTS(q, 10, opts)
        require.NoError(t, err)  // graceful
    }
}
```

**의도하지 않음**: 한국어 query 가 영문 결과를 *retrieve* 하는 것 — 이건 ckv 책임.
**의도함**: ckg 가 *깨지지 않는* 동작 + *결과 없음* 의 정상 반환.

### 3.2 ckg-NEW-2/3/4: PR-aware 옵션 A 구현

사용자 결정 (cks 보고서 §4.2, ckv §10.5): A + B + C 모두 채택.
- **A** (ckg 측 책임): Symbol-level PR breadcrumb — *그 symbol 을 변경한 PR list* metadata
- **B** (cks 측 책임): PR context tool — A backend 사용
- **C** (agent layer): Pre-flight warning injection — B 위에

#### 데이터 스키마

```go
// pkg/types/node.go 확장 (ckg-NEW-2)
type Node struct {
    // 기존 필드 (유지)
    Qname     string
    File      string
    StartLine int
    EndLine   int
    // ...

    // 신규 (R12)
    RecentPRs []PRRef  // 그 symbol 의 line_range 와 겹치는 변경의 PR list
}

// 신규 타입
type PRRef struct {
    Number       int         // GitHub PR number
    Title        string
    BaseSHA      string
    HeadSHA      string
    Summary      string      // PR title + Background 첫 문장
    MergedAtUTC  time.Time   // *** temporal slicing key (ckg-NEW-3) ***
    Repo         string      // "owner/name" (PR 출처)
}
```

#### Build-time 자동 채우기

```go
// internal/build/pr_history.go (신규)
func ScanPRHistory(srcRoot string) (map[NodeID][]PRRef, error) {
    // 1. git log --merges --pretty='%H|%aI|%s' 로 merge commit 추출
    // 2. (#NNN) 패턴 매치 → PR number 추출
    // 3. 각 merge commit 의 git diff --name-only --files-with-matches 로
    //    변경된 file:line range 추출
    // 4. 각 file:line range 가 *어떤 Node 의 (file, start_line, end_line)*과
    //    겹치는지 매칭
    // 5. Node ID → []PRRef 매핑 반환
    //
    // gh API 호출은 *optional* (network 의존). git log 만으로 80% cover.
    // Title + Background 본문은 git log -p 의 commit body 에 포함됨 (squash merge 가정).
}
```

#### Public accessor (ckg-NEW-4)

```go
// pkg/store/store.go 확장
type Reader interface {
    // 기존 (유지)
    SearchFTS(query string, limit int, opts SearchFTSOptions) ([]SearchHit, error)
    FindSymbol(name string, exact bool, opts FindSymbolOptions) ([]Node, error)
    // ...

    // 신규
    GetNodePRs(nodeID NodeID, cutoff time.Time) ([]PRRef, error)
}
```

cks 가 이 accessor 를 wrap 해서 B (PR context tool) 구성.

### 3.3 ckg-NEW-5/8: Stage B evaluation harness

사용자 명세 "작은 단위부터 검증". ckv 의 fixture 12 개를 ckg-side 로 mirror.

**ckv fixture (12 개)**:
- pr69 / pr70 / pr72 / pr74 (기존)
- pr77 / pr75 / pr73 / pr67 / pr63 / pr58 / pr56 / pr55 (신규, ckv §10.3)

**ckg-side mirror task YAML 형식**:

```yaml
# eval/stablenet/tasks/T04-pr77-anzeon-tip-callers.yaml
id: T04-pr77-anzeon-tip-callers
corpus: go-stablenet
corpus_path: ${STABLENET_SRC}/eth/gasprice
description: |
  Identify the function that decides whether AnzeonTipEnv.currentBlock should
  refresh, and find all callers of SetCurrentBlock that pass header GasTip.
expected_kind: symbol_set
expected:
  symbols:
    - AnzeonTipEnv.SetCurrentBlock
    - AnzeonTipEnv.gasTipChanged
    - RemotesBelowTip
# Stage B 측정 시 ckv vocabulary 변환을 *외부에서* 적용 후 ckg eval 호출
# 즉 ckg 는 영문 keyword 만 받음
related_pr: 77
base_sha: 0bf2f4d1b...  # ckv §10.3 와 정합
scoring:
  type: precision_recall
  threshold:
    precision: 0.7
    recall: 0.7
```

**evaluation harness (ckg-NEW-8)**:
- `ckg eval --tasks='eval/stablenet/tasks/T04-*.yaml,T05-*.yaml,...' \
    --baselines=alpha,beta,gamma,delta`
- 결과: 각 fixture 에 대한 4-baseline precision/recall + α/β/γ/δ token comparison
- ckv-side 평가와 동일 fixture → cks 통합 단계에서 *동일 fixture 로 hybrid measurement*

### 3.4 ckg-NEW-7: CKG-3 cross-snapshot 재평가

**보류 사유 (HANDOFF.md / dogfood-followups)**:
> "ckg 단독 결정 불가 — cks 가 cross-commit 검색을 정말 필요로 하는지, 어떤
> 시나리오에서 쓰는지 모름"

**해소 (2026-05-22)**: cks fixture 12 개가 *base_sha → head_sha* 시간축을 명시적 활용:

| 시나리오 | 시간축 | ckg 가 보여야 할 것 |
|---|---|---|
| Fixture eval (PR 적용 전 상태) | `base_sha` 시점 | 그 시점의 graph (PR 적용 *전*) |
| Fixture grading (PR 적용 후 변경 검증) | `head_sha` 시점 | 그 시점의 graph (PR 적용 *후*) |
| PR breadcrumb (사용자 시나리오) | `cutoff = now` | 모든 과거 PR (temporal slice 됨) |

**옵션 재평가** (HANDOFF dogfood-followups CKG-3):

| 옵션 | 현재 평가 | 비고 |
|---|---|---|
| A — 단일 snapshot 명시적 문서화 | 부족 | fixture eval 이 base_sha / head_sha 두 시점 필요. A 만으로는 cks fixture flow 불가능 |
| B — Multi-snapshot 완전 지원 | 비용 너무 큼 | 스키마 + 모든 query 변경. 본 작업 범위에 안 맞음 |
| **C — 디렉토리 라우팅 (DB per commit)** | **권장** | `ckg build --out=/tmp/ckg-stablenet-${BASE_SHA}` 패턴. *이미 ckg-NEW-2 의 git log scan 과 같은 git 메타 활용*. fixture runner 가 base_sha 별 DB 디렉토리 spawn |

**ckg-NEW-7 결정**: 옵션 C 채택 권장. 변경 사항:
- ckg build 가 `--out` 에 commit hash suffix 자동 부착 옵션 (`--out-tag=auto-commit-hash`)
- fixture runner 가 base_sha 별 *별도 graph DB* spawn
- cross-commit 검색은 *애플리케이션 레벨* (cks 가 두 DB 를 동시 mount) 에서

본 결정은 ckg-NEW-2 (PR breadcrumb) 의 git log scan 인프라와 *코드 공유 가능* — 한
번 build 시점에 git 메타를 scan 해서 *모든 PR 의 base_sha 자동 추출*.

### 3.5 ckg-NEW-9: `pkg/bm25.Scorer` 외부 import 안정성

사용자 결정 R13 (3-leg BM25 임시 적용): ckv 가 `ckg/pkg/bm25.Scorer` 를 import 해서
임시 적용. 이를 위해 ckg 측에서 보장해야 할 것:

1. **공개 API 안정성**: `pkg/bm25.Scorer` interface 가 v1 안정. breaking 변경 금지
2. **Go module path 확정**: `github.com/0xmhha/code-knowledge-graph/pkg/bm25` 명시 (이미 OK)
3. **외부 사용자 integration test**: 별도 mini-project (`pkg/bm25/example_external_test.go`) 가 ckv 의 사용 패턴을 시뮬레이션 — 외부 사용자 관점 회귀 보장
4. **versioning 문서**: `pkg/bm25/README.md` 또는 `pkg/bm25/doc.go` 에 *외부 사용 시 SemVer 약속* 명시

**작업량**: ~50 LOC (외부 사용 example test) + ~20 lines docs.

---

## 4. Stage B 평가 명세 (cks 통합 전 ckg 단독 검증)

사용자 명시 원칙 "작은 단위부터 검증". Stage A (ckv 단독) → Stage B (ckg 단독)
→ Stage C (cks 통합) 순. 본 절은 Stage B 의 ckg 측 책임.

### 4.1 Stage B 측정 대상

| 차원 | 측정 질문 | 메트릭 | 진입 조건 |
|---|---|---|---|
| 영문 keyword → file/symbol recall | "정확 영문 keyword 가 들어오면 ckg 가 정확 결과를 반환하는가" | precision_recall, MRR | ckg-NEW-5 (12 task mirror) |
| 한국어 query graceful degradation | "한국어가 들어와도 panic 안 하고 empty 반환하는가" | TestFTS5Query_KoreanInput_Graceful PASS | ckg-NEW-1 |
| PR breadcrumb 정확도 | "변경된 symbol 의 PR list 가 *실제 git 변경 이력*과 일치하는가" | grep-기반 자동 검증 | ckg-NEW-2/3 |
| qname canonical 일관성 | "동일 symbol 의 qname 이 항상 같은 형태로 반환되는가" | unit test | CKG-6 ✅ + NEW-6 docs |
| α/β/γ/δ 4-baseline | 각 baseline 별 retrieval token efficiency | H1 (≥50% token saving) / H2 (no regression) | 기존 인프라 |
| Multi-stage E2 (location identification) | "ckv-side vocabulary 거친 후 ckg 가 정확 file/symbol 찾는가" | F1 (file) + F1 (symbol) | ckg-NEW-8 |
| Citation accuracy KPI | "ckg 반환 citation 의 file:line 이 실제로 존재하는가" | T-03 validator | T-03 (P0 기존) |
| Hallucination | "LLM 응답에 fake symbol 이 있는가" | T-04 V4 (30-question 측정) | T-04 V4 (P0 기존) |

### 4.2 측정 순서

```
Step 1: ckg-NEW-1 (한국어 graceful) 단위 test PASS
Step 2: ckg-NEW-5 (12 task YAML) 작성 — pr77/75/73/67/63/58/56/55 추가
Step 3: ckg-NEW-2/3/4 (PR breadcrumb) 구현 + git log scan
Step 4: ckg-NEW-8 (Stage B harness) — 12 task × 4 baseline 측정
Step 5: 결과 분석 → 회귀 detect → fix → 재측정
Step 6: T-04 V4 (Hallucination 30-question 측정) 와 결합
Step 7: 결과를 cks 측에 전달 — Stage C 통합 단계 진입
```

### 4.3 Stage B 결과 → cks 사용

cks 가 Stage C 에서 사용할 ckg-side baseline:
- 영문 keyword recall: *cks vocab bridge 가 변환만 잘 하면* ckg-side 가 cover 한다는 보장
- 한국어 graceful: cks 의 *bypass 케이스* 안전
- PR breadcrumb: cks 가 B (PR context tool) 의 backend 로 사용
- 4-baseline: cks 가 *동일 fixture 로 hybrid (ckv+ckg+cks-BM25)* 측정 시 비교 기준

---

## 5. T-14 (pkg/mcphandlers) 와 ckg-NEW-2~4/9 동시 진행 권장

HANDOFF.md T-14 의 본질: ckg 의 MCP 핸들러 코드를 외부 (cks repo) 에서 import 가능
하도록 `pkg/mcphandlers/` 신설. **cks S1 진입 차단 요소**.

본 세션이 추가한 작업도 같은 영역:
- ckg-NEW-2: `pkg/types.Node` 확장 (RecentPRs 필드)
- ckg-NEW-3: `pkg/types.PRRef` 신규 + temporal slicing
- ckg-NEW-4: `pkg/store.Reader` 확장 (GetNodePRs)
- ckg-NEW-9: `pkg/bm25` 외부 import 안정성

**권장 진행 순서**:

```
1. T-14 (pkg/mcphandlers) 신설 — 기존 internal 핸들러를 pkg/ 로 export
   동시에:
2. pkg/types.Node + PRRef 확장 (ckg-NEW-2)
3. pkg/store.Reader 에 GetNodePRs / RecentPRsBefore 추가 (ckg-NEW-3/4)
4. pkg/bm25 external test 추가 (ckg-NEW-9)
5. 한 commit / 한 PR 로 4 가지 surface 동시 정착 — cks 가 *한 번에* import 시작 가능
```

**이유**: public surface 는 *한 번에 정착*해야 외부 사용자 (cks) 의 import 안정성이
보장됨. 1 → 2 → 3 → 4 를 별도 PR 로 진행하면 *4 번의 외부 호환성 검증* 필요. 한 번에
정착 + 외부 사용 example test 1 회 검증이 효율적.

---

## 6. fixture mapping (ckv 12 ↔ ckg task)

ckv §10.3 의 12 fixture 와 ckg 의 기존 3 task + 신규 8 task 매핑:

| ckv fixture | ckg 기존 | ckg 신규 task | 카테고리 |
|---|---|---|---|
| pr69 (genesis decodePrealloc) | T03 (systemcontracts) 부분 | — | genesis_governance |
| pr70 (effectiveGasPrice) | — | **T-pr70-effectivegasprice** | receipt_derivation |
| pr72 (eth_GetReceiptsByHash) | — | **T-pr72-getreceiptsbyhash** | rpc_method |
| pr74 (txMaxSize 256KB) | — | **T-pr74-txmaxsize** | tx_pool |
| pr77 (AnzeonTipEnv refresh) | — | **T-pr77-anzeon-tip-callers** | gas_policy |
| pr75 (future-view check) | T02 (wbft-prepare) 부분 | **T-pr75-wbft-futureview** | consensus_wbft |
| pr73 (Account.Extra/GovCouncil) | T03 (systemcontracts) 부분 | **T-pr73-govcouncil-sync** | genesis_governance |
| pr67 (secp256r1 fork move) | — | **T-pr67-secp256r1-boho** | hardfork |
| pr63 (GovMinter burn refund) | — | **T-pr63-govminter-burn-refund** | gov_minter |
| pr58 (chainconfig engine) | — | **T-pr58-chainconfig-engine** | chain_config |
| pr56 (comma config) | — | **T-pr56-system-contract-init** | system_contract_init |
| pr55 (race condition) | — | **T-pr55-wbft-race** | consensus_wbft_concurrency |
| T01 (NewBlockChain callers, 기존) | T01 유지 | — | core_blockchain |

→ **ckg-side 신규 task 11 개** + 기존 T01/T02/T03 유지 = 총 14 task.

각 신규 task 의 base_sha 는 ckv §10.3 와 *완전 정합* (ckv 가 ckg 를 spawn 시 같은
base_sha 사용 → 동일 graph snapshot 위에서 측정).

---

## 7. 사용자 결정 항목 (ckg 측에서 명확히 확인 필요)

### 7.1 ckg-NEW-2/3/4 commit timing

옵션 1: **T-14 와 같은 PR 로 정착** — public surface 한 번에. cks 측 import 차단
한 번에 해소.

옵션 2: **T-14 먼저, ckg-NEW-2~4 후속 PR** — T-14 작업이 이미 진행 중이면 분리.

→ **권장**: 옵션 1 (한 번에 정착). 단 T-14 작업자가 동의해야.

### 7.2 ckg-NEW-7 (CKG-3 cross-snapshot)

본 문서 권장: 옵션 C (디렉토리 라우팅, DB per commit).

대안:
- 옵션 A 유지 (단일 snapshot 명시 문서화) — fixture eval 이 *외부 orchestration*
  으로 base_sha 별 별도 build 호출. ckg 측 코드 0.
- 옵션 C — ckg build 가 `--out-tag=auto-commit-hash` 자동 부착. ckg-NEW-2 의
  git log scan 인프라 재사용.

→ **권장**: 옵션 C. 단 외부 orchestration (옵션 A) 으로도 fixture eval 충분히 가능
하므로 우선순위는 P1 로.

### 7.3 viewer-next dirty 파일 (`internal/server/api.go`, `web/viewer-next/...`)

본 세션 이전 작업의 잔여. eval 작업과 *별개*. 사용자 결정 필요:
- A. 그대로 commit (UI 변경 + screenshot 정리)
- B. eval 작업과 분리한 별도 PR 로 commit
- C. drop (실패한 실험 잔여)

→ 본 문서 범위 외. ckg 작업자가 직접 결정.

---

## 8. 다음 세션 (ckg 작업자) 진입 권장 순서

본 문서 정착 후 ckg 작업자의 작업 순서 권장:

```
Day 1:
  1. 본 문서 §0~§3 정독 (15 분)
  2. ckv §10 정독 — fixture 12 개 + ckv-NEW-1~9 (30 분)
  3. cks 보고서 R-A/B/C/D 정독 (30 분)
  4. T-14 + ckg-NEW-2/3/4/9 — public surface 한 번에 정착 (4~6 시간)

Day 2:
  5. ckg-NEW-1 (한국어 graceful) 단위 test (2 시간)
  6. ckg-NEW-5 (11 신규 task YAML) — ckv §10.3 기반 자동 매핑 (3 시간)

Day 3:
  7. ckg-NEW-8 (Stage B harness) (4 시간)
  8. Stage B 1차 측정 — 14 task × 4 baseline (1 시간 측정 + 분석)

Day 4-5:
  9. T-04 V4 (Hallucination 30-question 측정) — 본 작업의 클라이맥스
  10. Stage B 회귀 detect → fix → 재측정
  11. 결과 → cks 측에 전달

병행:
  - ckg-NEW-6 (qname canonical 사용 가이드) — docs 만, 30 분
  - ckg-NEW-7 (CKG-3 결정) — 옵션 C 결정만, 1 시간

이후:
  Stage C (cks 통합) 은 별도 repo / 별도 세션
```

---

## 9. cks 측 보고서 + ckv §10 cross-link

본 문서와 동기적으로 작성된 외부 문서:

- **cks 보고서**: `code-knowledge-system/docs/research/knowledge-data-best-practice-2026-05-22.md`
  - R-A: Code RAG semantic gap 해결 best practice 카탈로그 (12 기법)
  - R-B: go-stablenet 권장 기술 스택 (Tier 1/2/3)
  - R-C: ckg ↔ ckv 키워드 공유 기술 (C1-C5)
  - R-D: cks 측 신규 기능 (D1-D7, ~1030 LOC)

- **ckv §10**: `code-knowledge-vector/docs/evaluation-design-2026-05-22.md` §10
  - 사용자 명세 R9~R13 정착
  - Multi-stage evaluation (E1-E4)
  - fixture 12 개 자동 매핑
  - ckv-NEW-1~9 (9 개 신규 작업)
  - Stage A/B/C 평가 체계

본 문서는 **ckg-side mirror** — 같은 사용자 명세를 ckg 측 작업으로 분해.

| 사용자 명세 | ckv §10 작업 | ckg 본 문서 작업 |
|---|---|---|
| R9 (vocab bridge) | ckv-NEW-1 (alias), ckv-NEW-8 (glossary loader) | ckg-NEW-1 (한국어 graceful) |
| R10 (multi-stage eval) | ckv-NEW-4 (E1/E2/E3 metric), ckv-NEW-5 (fixture 12) | ckg-NEW-5/8 (Stage B mirror + harness) |
| R11 (점진 fixture) | ckv-NEW-2 (record mode) | — (Stage C 단계) |
| R12 (PR-aware) | ckv-NEW-3 (PR corpus), ckv-NEW-6/7 (PR breadcrumb data) | ckg-NEW-2/3/4 (PR breadcrumb metadata) |
| R13 (3-leg BM25) | ckv-NEW-9 (chunk-aware BM25) | ckg-NEW-9 (`pkg/bm25` 외부 안정성) |

→ ckv ↔ ckg 작업은 **서로 의존하지 않음** (병렬 가능). 단 *fixture 12 개 base_sha* 만
양쪽이 동일하게 사용 (cross-check).

---

## 10. 변경 이력

| 일자 | 변경 |
|---|---|
| 2026-05-23 | 초안. cks 측 통합 점검 세션 결과 back-port. 사용자 명세 R9 (vocab bridge graceful) / R10 (multi-stage eval Stage B) / R11 (점진 fixture) / R12 (PR-aware A) / R13 (BM25 외부 import 안정성) 추가. ckg-NEW-1~9 신규 작업 명세 (총 ~500 LOC). Stage B 평가 명세 (§4). T-14 와 동시 진행 권장 (§5). fixture mapping (§6). CKG-3 cross-snapshot 옵션 C 권장 (§3.4). 다음 세션 작업 순서 (§8). |
