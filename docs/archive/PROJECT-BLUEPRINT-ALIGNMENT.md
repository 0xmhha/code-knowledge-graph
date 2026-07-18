# CKG Blueprint Alignment & Refactoring Direction

> **ARCHIVED 2026-07-18.** Every "Tier A — needed" item this doc identified has
> since shipped: policy-meta nodes (`Policy`) + security-pattern nodes
> (`SecurityPattern`), 1-shot retrieval (`get_context_for_task` oneshot), and
> "왜"-history (PR title + cleaned commit body, `pkg/types/pr_ref.go`). Its
> stated tool count (8) and schema (1.13) are stale (now 10 / 1.23). CKG's role
> in the CKG/CKV/CKS triangle now lives in `docs/VISION.md`; live status in
> `docs/CONTINUITY.md`. Kept for the E2E blueprint scenario (§1) as provenance.
>
> **목적**: 사용자가 제시한 E2E 청사진을 정리하고, CKG가 그 안에서 맡을
> 역할을 명확히 한 뒤, 리팩토링 방향을 도출한다.
> **작성**: 2026-05-29

---

## 1. 사용자 청사진 — E2E 시나리오

### 1.1 흐름

```
[Jira ticket 생성 (template 포맷)]
    ↓ assignee가 Claude Code plugin 명령 실행
[Claude Code plugin (coding-agent)]
    ↓ Jira MCP로 ticket read
[보안 검토 skill: 민감정보 체크]
    ↓ pass
[Sonnet 모델: go-stablenet 작업 인식 → 전용 skill]
    ↓
[CKS MCP → CKV: ticket → "어떤 작업?" 의미 파악, 관련 키워드 추출 (rerank)]
    ↓
[CKS MCP → CKG: 키워드 → 관련 코드 + PR 히스토리 + 동시성 영향 모듈]
    ↓
[Claude Code: 종합 → 작업 plan + test 정의]
    ↓ 폴더 생성 (이름에 jira-ticket + timestamp), 문서 + 로그 저장
[설계 정밀화 → 검토 → 수정 → 다음 버전 (히스토리 관리)]
    ↓
[구현 전문 agent: 별도 브랜치, 분할 커밋, hook 활용 iteration]
    ↓ 완료 후 부모 agent에게 통지
[Evaluation agent: unit test → lint/fmt → 보안 → chainbench (실제 체인 구성)]
    ↓
[테스트 통과 시 PR 생성 → Jira ticket에 PR url 댓글]
    ↓
[human 코드 리뷰 → 코멘트 → plugin 명령으로 수정 cycle]
    ↓
[squash merge → Jira ticket complete]
```

### 1.2 핵심 가치

| 가치 | 측정 |
|---|---|
| **정확성** | 첫 시도 성공률, 재작업 횟수, 보안 취약점 발생 빈도 |
| **경제성** | LLM 토큰 사용량, 작업 완료 시간, 인력 투입 시간 |
| **확장성** | 새 도메인/언어 추가 비용, 데이터 갱신 비용 |

### 1.3 기존 방식 한계 (사용자 분석)

1. **Retrieval의 빈약함**: `grep` / `glob` / `read` 파이프라인은 "있는 것"만 찾음. 메타정보 (변경 이유, 정책 결정, 영향 모듈) 부재
2. **도메인 무지(無知)**: go-stablenet 특수성 (consensus 정책, gas 정책, validator 룰, Byzantine 방어 등) 반영 안 됨
3. **여러 관점 누락**: 보안, MEV, 동시성, governance 영향 등이 한 번에 고려되지 않아 재수정 발생
4. **토큰 비용 폭발**: 위 한계로 인한 cycle이 늘어남

### 1.4 해결 전략 (사용자 명시)

1. **Knowledge data 구축**: 코드 + 메타정보(히스토리, impact, 정책)를 미리 저장
2. **Vector + Graph 이중 검색**: CKV(의미) + CKG(키워드/구조)
3. **CKS 오케스트레이션**: coding-agent가 cks를 통해 ckv/ckg 활용
4. **LLM 사용 빈도↓ + 정확도↑**: 기존 retrieval 패턴 모방하되 토큰 절감

---

## 2. CKG의 역할 — 청사진 내 위치

### 2.1 CKG가 책임지는 것

청사진의 `[CKS MCP → CKG: 키워드 → 관련 코드 + PR 히스토리 + 동시성 영향 모듈]` 단계 전체.

| 입력 | CKG가 반환해야 할 것 |
|---|---|
| 키워드 (예: "GovStaking deposit") | 1. **관련 코드**: 정의 + 호출 관계 + 타입 의존 |
| | 2. **PR 이력**: 어떤 PR이 어떤 이유로 어떤 변경을 했는지 |
| | 3. **동시성 영향 모듈**: lock 공유, channel 공유, atomic 공유 등으로 영향 받는 다른 코드 |
| | 4. **정책 메타정보** *(신규 필요)*: 해당 코드와 연결된 chain config, fork, governance 룰 |
| | 5. **보안 패턴** *(신규 필요)*: Byzantine 공격 가능 지점, MEV 패턴, reentrancy 등 |

### 2.2 CKG가 책임지지 않는 것 (다른 컴포넌트)

| 컴포넌트 | 책임 |
|---|---|
| **CKV** | 자연어/Korean → 정확한 영문 키워드 (vocabulary bridge), 의미적 유사도 검색 |
| **CKS** | coding-agent ↔ CKV/CKG 오케스트레이션, 답변 rerank, retrieval 패턴 |
| **coding-agent (plugin)** | Jira read, plan 문서 생성, sub-agent 호출, hook 관리, PR 생성, Jira 업데이트 |
| **chainbench (MCP)** | 실제 go-stablenet 네트워크 구성 + 동작 검증 |

### 2.3 인터페이스 안정성 — coding-agent 입장

CKG는 cks를 거쳐 호출되지만, **mcphandlers의 public surface가 그 약속**입니다.
현재 8 tools:

- `find_symbol`, `find_callers`, `find_callees`, `get_subgraph`, `search_text`
- `evidence_for_intent`, `impact_of_change`, `get_context_for_task`

이 surface는 청사진의 "코드 + PR 히스토리 + 동시성 영향"을 1-shot으로 제공하는 형태로 진화할 필요가 있습니다. (현재는 각각 다른 tool 호출 필요)

---

## 3. 현재 CKG 기능 — Gap 분석

### 3.1 이미 갖춘 것 ✅

| 기능 | 상태 | 청사진 매핑 |
|---|---|---|
| Symbol 추출 (Go/TS/Sol) | ✅ | 관련 코드 |
| Call graph (find_callers/callees) | ✅ | 관련 코드 |
| Subgraph 확장 | ✅ | 관련 코드 |
| FTS 검색 + camelCase 토크나이저 | ✅ | 키워드 검색 |
| PR breadcrumb (symbol → PR 이력) | ✅ | PR 히스토리 |
| Impact analysis (impact_of_change) | ✅ | 영향 분석 (기초) |
| smartContext (BM25 + PR + usage rerank) | ✅ | 자동 컨텍스트 선별 |
| Schema 1.13 (search_tokens) | ✅ | 효율 검색 |
| MCP 서버 + 8 tools public surface | ✅ | CKS 통합 준비 |
| Lock propagation (Go, W-A) | ⚠️ 부분 | 동시성 영향 모듈 |

### 3.2 청사진에 비추어 필요한 것 ❌

#### Tier A — 청사진 직결 (CKG가 책임지는 것)

| 기능 | 현재 상태 | 청사진 요구 |
|---|---|---|
| **동시성 영향 모듈 (완전)** | W-A 부분만 | W-A Stage B DFS 완료, W-B (TS async), W-C (Sol inheritance) — 청사진 §1.1 "동시성 영향 모듈"이 핵심 가치 |
| **정책 메타정보 노드** | 없음 | chain config, fork block, governance rule, validator 룰 → 코드 노드에 연결 (예: `params.AnzeonForkBlock` → 활성화 코드 + 비활성화 코드) |
| **보안 취약점 패턴 노드** | 없음 | Byzantine 공격 가능 지점, MEV 패턴, reentrancy 지점, 재진입 가능 모듈을 그래프에 표현 |
| **"왜" 이력 (commit message + PR description)** | PR breadcrumb은 PR 번호만 | 변경 이유(정책 결정, 보안 패치 등)를 텍스트로 attach → CKV가 의미 검색 가능 |
| **1-shot retrieval API** | 8개 tool 호출 필요 | "이 키워드에 대해 코드+히스토리+영향+정책+보안을 한 번에" — `get_context_for_task` 확장 |

#### Tier B — 데이터 갱신 / 운영

| 기능 | 현재 상태 | 청사진 요구 |
|---|---|---|
| **Incremental update** | ✅ A3 캐시 | 청사진 §4.4 ("추가 데이터 업데이트/갱신") — 이미 갖춤 |
| **per-commit graph** | ✅ `--at-commit` + `--out-tag=auto-commit-hash` | 다중 브랜치 동시 분석 가능 |
| **실시간 무효화** | 없음 (build 단위) | Watch mode? push 시 자동 rebuild 트리거? |
| **CI/CD 통합** | 없음 (EV1 Phase 3 미적용) | PR 자동 검증 |

#### Tier C — Stage B에서 드러난 약점

| 약점 | 측정값 | 개선 방향 |
|---|---|---|
| δ score | 0.335 (β 0.441의 76%) | smartContext 후보 30→100, 2-hop 옵션, task type별 packing 전략 |
| δ cite precision | 0.817 (최고지만 90%+ 목표) | start_line/end_line 정밀화, signature 정확성 |
| γ latency | 137s | parallel tool calls, gammaMaxTurns 축소 |
| γ score | 0.364 | system prompt 강화, 도구 선택 가이드 |

#### Tier D — 코드 클린업 (clean code)

세션이 누적되며 V0/V1/V2/V3 패턴이 혼재. 별도 audit 필요.

---

## 4. 청사진 vs 현재 — 우선순위

### 4.1 의사결정 기준

| 기준 | 가중 |
|---|---|
| **청사진의 핵심 가치** (정확성, 경제성) | high |
| **현재 측정에서 드러난 약점** | high |
| **CKG의 책임 영역** (다른 컴포넌트 영역 침범 안 함) | high |
| **데이터 갱신 비용** | mid |
| **clean code (유지보수성)** | mid |

### 4.2 추천 우선순위

#### P0 — 청사진 핵심 (CKG의 가치 강화)

1. **"왜" 이력 강화**: PR breadcrumb에 commit message + PR description body를 attach
   - 효과: CKV의 의미 검색에 가장 큰 입력. "왜 이렇게 짰지?"를 LLM 호출 없이 답할 수 있음
   - 변경: schema에 `node_pr_descriptions` 또는 `prs.description` 추가, build 단계에서 git log + GitHub API 옵션

2. **1-shot retrieval API**: `get_context_for_task`를 확장하여 PR 이력 + impact + 동시성 영향을 1회 호출에 포함
   - 효과: coding-agent 입장에서 도구 호출 회수 감소 → 토큰/시간 절감
   - 변경: smartContext 결과에 PR/impact/concurrent affected를 같은 JSON으로 통합

3. **smartContext 정확도 개선** (Stage B Tier 2와 동일)
   - δ score 0.335 → β(0.441)에 근접
   - 후보 수 확대, 2-hop, task type packing

#### P1 — 도메인 메타정보

4. **정책 메타정보 노드 (go-stablenet 특화)**
   - `params/config.go`의 fork block, `consensus/wbft/*` 정책, `systemcontracts/*` 등을 별도 NodeKind로
   - 코드 노드 ↔ 정책 노드 edge: `governed_by`, `activated_at`, `gas_policy_of`
   - 효과: LLM이 "이 코드는 어떤 정책의 결과인가?" 즉시 알 수 있음

5. **보안 패턴 노드**
   - Byzantine 공격 가능 지점, reentrancy 가능 함수, validator 권한 의존 함수 등을 라벨링
   - 정적 분석 + 도메인 지식 룰 기반
   - 효과: 코드 수정 시 보안 영향 즉시 감지

#### P2 — Stage B 약점 해결

6. **γ 개선** (latency, score)
7. **δ 개선** (score, cite precision 유지)
8. **W-A Stage B DFS 완성, W-B/W-C 미루지 않고 진입**

#### P3 — 운영 / clean code

9. **CI eval gate (EV1 Phase 3)**
10. **코드 클린업 (V0/V1/V2/V3 잔재 통합, naming, 미사용 코드)**
11. **실시간 갱신 (file watcher)**

#### Hold — 다른 컴포넌트 영역

- CKV 구현 (별도 프로젝트)
- CKS 오케스트레이션 (별도 프로젝트)
- coding-agent plugin (별도 프로젝트)
- chainbench MCP (별도 프로젝트)

---

## 5. 다음 작업 — 구체화

청사진을 정렬한 결과, **P0 + P3(코드 클린업)을 묶어서 진행**하는 것을 추천합니다.

### 5.1 Phase A: 코드 클린업 audit (이번 세션)

목표: 향후 P0/P1 기능 추가 전에 코드베이스 정리

작업:
- 사용되지 않는 코드 / 함수 / 변수 식별
- 중복된 헬퍼 통합
- 거대 함수 분리
- naming inconsistency 수정
- V0/V1/V2/V3 잔재 통합
- 테스트 부재 영역 보강

도구: `go vet`, `staticcheck`, `unused`, 수동 read

산출물: `docs/CODE-AUDIT-2026-05-29.md` (gitignored — 일회성)

### 5.2 Phase B: P0 기능 추가 (다음 세션)

- "왜" 이력 강화 (PR description 통합)
- 1-shot retrieval API
- smartContext 정확도 개선

### 5.3 Phase C: P1 도메인 메타정보

- 정책 메타정보 노드 설계
- 보안 패턴 노드 설계
- 데이터 모델 확장 (schema 1.14?)

### 5.4 Phase D: 운영 + W-B/W-C 진입

- CI eval gate
- W-B (TS async/await)
- W-C (Sol inheritance)

---

## 6. 우선 결정 사항 — 사용자 확인 필요

| 결정 | 옵션 | 영향 |
|---|---|---|
| **이번 세션 범위** | (a) 클린업 audit + 시작 / (b) audit만 + 다음 세션에 실행 / (c) P0 바로 진입 | (a)가 균형 |
| **"왜" 이력 출처** | (a) git log message만 / (b) GitHub PR description 추가 (별도 API 필요) | (b)가 정확하지만 외부 의존 |
| **정책 메타정보 정의 누가?** | (a) CKG 코드에 룰 하드코딩 / (b) 별도 YAML/JSON 정책 파일 / (c) CKS에 위임 | (b)가 유지보수 좋음 |
| **W-B/W-C 우선순위 격상** | (a) P1 유지 / (b) P0 격상 (cks 통합 가시화되면) | 현 상태 (a) 추천 |

---

## 7. 즉시 시작 가능 — 코드 클린업 (Phase A)

다음 도구로 baseline 측정:

```bash
# 미사용 코드
go vet ./...
staticcheck ./...
unused ./...

# 복잡도
gocyclo -over 15 ./...

# 중복
dupl -threshold 80 ./...
```

이 결과로 우선순위 정렬 후 작업.
