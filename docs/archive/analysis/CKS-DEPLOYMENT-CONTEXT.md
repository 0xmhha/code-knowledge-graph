# CKS Spec 위험 항목의 환경별 영향 가이드

> **목적**: `CKS-SPEC-REVIEW.md`의 P0 3건(B1·C1·D1)을 비롯한 위험 항목들이 **production(외부 노출/multi-tenant) vs 회사 내부 시스템(사내 도구)** 환경에서 어떻게 다른 영향을 미치는지 정리.
> spec 위험 우선순위는 환경에 따라 달라지므로, 운영 환경 분류 후 작업 순서를 결정하기 위한 reference.
>
> **선행 문서**:
> - `docs/analysis/CKS-SPEC-REVIEW.md` (spec 자체의 위험 식별)
> - `docs/analysis/CKS-SPEC-COMPLIANCE.md` (CKG 구현의 spec 충실도)
>
> **마지막 갱신**: 2026-05-05

---

## 목차

1. [환경 정의 — production vs 회사 내부 시스템](#1-환경-정의--production-vs-회사-내부-시스템)
2. [P0 3건 환경별 영향 분석](#2-p0-3건-환경별-영향-분석)
3. [환경별 실용적 우선순위](#3-환경별-실용적-우선순위)
4. [요약 매트릭스](#4-요약-매트릭스)
5. [사용자 케이스 식별 가이드](#5-사용자-케이스-식별-가이드)
6. [Appendix — 위협 모델 비교](#6-appendix--위협-모델-비교)

---

## 1. 환경 정의 — production vs 회사 내부 시스템

spec(`04-cks-deep-dive.md`)의 §7.5 mTLS, §6.2 fail-closed, §5.3 PostgreSQL/Redis 등 보안·안정성 요건은 모두 **production 가정**에 기반합니다. 사용자의 실제 운영 환경에 따라 위험의 무게가 크게 달라집니다.

| 환경 | 특징 | 예시 |
|---|---|---|
| **production (spec이 가정)** | 외부 사용자 / multi-tenant / untrusted 입력 다수 / 24/7 SLA / compliance 요구 (SOC2, ISO27001 등) | SaaS형 코드 분석 서비스, 여러 조직이 같은 ckg cluster 공유, 외부 PR/Jira 본문이 evidence pack에 포함 |
| **회사 내부 (단일 dev)** | 단일 개발자 local 도구 / OS-level isolation으로 충분 / 다운돼도 다른 경로 가능 | 개발자가 자신의 노트북에서 `ckg build` 실행 후 `ckg mcp`로 Claude Code 통합 |
| **회사 내부 (공용 서버)** | 사내 여러 개발자 공유 / 사내 네트워크 / audit는 약하게 필요 / 동료 신뢰 모델 | 팀 공용 ckg 서버, 여러 개발자가 동일 graph.db에 접근 |
| **회사 내부 (CI/CD 자동화)** | 사내 corpus + 사내 자동화 / 안정성 중요 / 보안 위협은 사내망 한정 | GitHub Actions/Jenkins에서 PR마다 ckg eval 실행 |

→ spec V2 작업이나 fix 우선순위 결정 시 **자기 환경이 어디에 속하는지 먼저 확정**해야 합니다.

---

## 2. P0 3건 환경별 영향 분석

### 2.1 B1 — task_type 8종 vs playbook 4종 미매핑

**문제 본질**: spec 자체의 정의 누락. 환경과 무관하게 발생.

| 환경 | 영향 강도 | 발현 시나리오 |
|---|---|---|
| production | 🔴 즉시 차단 | 외부 사용자가 `task_type=refactor`로 호출 → `select_playbook()` 결과 undefined → 500 error 또는 잘못된 playbook 매칭 → 응답 quality 저하 / 신뢰 손상 |
| 회사 내부 | 🟡 중간 | 사내 개발자가 직접 task_type을 4종(매핑된 것)으로만 호출하면 우회 가능. 또는 LLM client가 자체 fallback 결정. **단 spec대로 자동 분류하면 동일 문제** |

**핵심 차이**:
- production: 사용자 통제 불가 → 즉시 표면화
- 내부: 호출 코드 수정으로 우회 가능, 단 LLM이 자동 분류 시 동일 위험

**근본 해결 방향은 동일** — spec V2에서 8 task_type 모두 매핑 또는 4종으로 축소.

### 2.2 C1 — stdio MCP에 mTLS 요구 (환경 의존성 가장 큼)

**문제 본질**: stdio transport는 cert 검증이 기술적으로 불가능.

| 환경 | 영향 강도 | 분석 |
|---|---|---|
| production | 🔴 critical | (a) HTTP transport로만 운영 가능 — stdio 사용 불가, (b) **multi-tenant**: 다른 조직이 caller를 위장하면 다른 조직의 graph 접근 가능 → 권한 escalation, (c) audit trail 없음 → compliance 위반 (SOC2, ISO27001) |
| 회사 내부 (단일 dev) | 🟢 무관 | stdio = local process spawn — OS-level user/file permission으로 trust 확보 충분. mTLS 없어도 보안 OK |
| 회사 내부 (공용 서버) | 🟡 부분 적용 | 여러 개발자가 같은 ckg 인스턴스에 접근하면 audit 정도는 필요 — `caller=cli:user@hostname` 기록으로 충분, 진짜 mTLS는 과한 요구 |

**핵심 차이**: **C1은 production-specific 이슈**.
- 사내 도구로만 쓴다면 spec §7.5의 mTLS 요건은 무시 가능
- 대신 §E4 권장(stdio = OS-level identity)으로 충분

### 2.3 D1 — Sanitize fail-closed가 전체 retrieve() panic

**문제 본질**: 한 corrupt rule이 시스템 전체 outage.

| 환경 | 영향 강도 | 시나리오 |
|---|---|---|
| production | 🔴 critical | (a) **외부 PR/Jira/issue 본문이 evidence pack에 포함** → sanitize 필수 → 그런데 sanitize 자체가 outage 야기 시 SLA 위반, (b) 한 corrupt rule이 모든 tenant outage |
| 회사 내부 | 🟡 영향 큰폭 감소 | (a) **위협 모델이 약함** — 사내 corpus의 코드/commit/comment는 동료 작성 → prompt-injection 위협 낮음. sanitize 자체를 옵셔널로 둘 수 있음, (b) outage 발생해도 개발자 생산성 영향 — 다른 도구로 우회 가능, (c) 단, **외부 의존성 import의 코멘트**(npm/go module의 README/주석)는 여전히 untrusted |

**핵심 차이**: **D1은 환경에 따라 무게가 크게 다름**.
- production: sanitize 필수 + outage 위험 둘 다 critical
- 내부: sanitize 자체가 옵셔널 — fail-closed로 인한 outage 가능성도 자연스럽게 낮아짐
- **단, 외부 라이브러리 코드를 그래프에 포함하면 내부에서도 일부 sanitize 필요**

---

## 3. 환경별 실용적 우선순위

### 🏢 회사 내부 도구로만 사용하는 경우

#### spec V2에서 무시해도 되는 항목

| 항목 | 이유 |
|---|---|
| **C1** (mTLS-on-stdio) | OS-level user identity로 충분 |
| **C7** (PostgreSQL/Redis 동시 write race) | SQLite 단일 backend로 통합 |
| **D7** (Direct CLI audit 단절) | 사내라 audit 수준 낮춰도 OK |
| **C5의 일부** (다국어/encoding pi-pattern) | 사내 corpus 위협 모델 약함 |
| **D4의 강도 완화** (sanitization rules 단일 장애점) | sanitize 자체가 옵셔널이면 단일 장애점도 약화 |

#### 여전히 fix 필요 항목

| 항목 | 이유 |
|---|---|
| **B1** (task_type 미매핑) | 환경 무관 spec 결함 |
| **B2/B3/B4/B5/B6** (정의 모호) | 구현 일관성 |
| **C2** (메모리 압박) | corpus 크기 따라 OOM은 동일 위험 |
| **C3** (incremental 비용) | 사내라도 빌드 시간 영향 |
| **C6** (Citation KPI 모호) | 측정 가능해야 품질 관리 |
| **D6** (Index materialization 부분실패) | 그래프 일관성은 환경 무관 |

→ **결론**: 사내 도구라면 P0 3건 중 **C1·D1은 강도가 크게 낮아짐**. **B1만 spec V2에서 수정 필수**.

### 🌐 production 운영 (외부 사용자/multi-tenant)

P0 3건 모두 **즉시 수정 필수**:

| 항목 | 즉시 수정 방향 |
|---|---|
| **B1** | 외부 사용자 task_type 통제 불가 → 공식 매핑 8종 모두 정의 (E1) |
| **C1** | HTTP/SSE transport 강제 + mTLS 검증 + caller 격리 (E4) |
| **D1** | sanitize circuit breaker + per-pattern timeout + redact_full fallback (E5) |

추가로 다음도 함께 고려:
- **D2** (run_id TTL) — 누적 폭증 방지
- **D3** (git rev-parse 비용 누적) — high-frequency query 대비
- **D7** (audit) — compliance 의무

---

## 4. 요약 매트릭스

| 항목 | production 운영 영향 | 회사 내부 시스템 영향 |
|---|---|---|
| **B1** task_type 미매핑 | 🔴 즉시 외부 사용자 차단 | 🟡 호출 코드 우회 가능, 단 spec V2 fix 필요 |
| **C1** mTLS-on-stdio | 🔴 multi-tenant 권한 escalation, compliance 위반 | 🟢 무관 — OS user로 충분 |
| **D1** sanitize fail-closed outage | 🔴 SLA 위반 + 모든 tenant outage | 🟡 사내 corpus는 위협 약함 + outage 우회 가능. 외부 의존성 import 시만 일부 필요 |

---

## 5. 사용자 케이스 식별 가이드

다음 중 자신의 케이스를 확정하면 우선순위가 명확해집니다:

### Case 1 — 단일 개발자 local 도구

**예**: 개발자 본인이 자신의 노트북에서 `ckg build` 후 `ckg mcp`로 Claude Code 통합.

| 우선순위 | 작업 |
|---|---|
| **무시** | C1, C7, D1, D2, D3, D4, D5, D7 |
| **P0** | B1 (task_type 매핑), C2 (메모리 압박 대비), C3 (incremental 비용) |
| **P1** | B2~B6 (정의 모호 fix), C6 (Citation KPI 정의) |

### Case 2 — 사내 공용 ckg 서버 (여러 개발자)

**예**: 팀 공용 ckg 서버에 여러 개발자가 접근, MCP는 stdio가 아닌 HTTP일 수도.

| 우선순위 | 작업 |
|---|---|
| **무시** | C7 (PostgreSQL/Redis는 spec 분기 무시 가능) |
| **부분 적용** | C1 (mTLS 대신 audit log만), D1 (simple regex만, ML classifier 불요), D7 (`caller=cli:user@hostname` audit) |
| **P0** | Case 1과 동일 + D6 (graph 일관성) |
| **P1** | Case 1과 동일 |

### Case 3 — 사내 + 외부 의존성 corpus 분석

**예**: 사내 코드 + npm/go module 의존성을 함께 인덱싱.

| 우선순위 | 작업 |
|---|---|
| **부분 적용** | D1의 sanitize는 외부 패키지의 코멘트만 적용 (사내 코드는 trust) |
| **P0** | Case 2 + 외부 의존성 식별 메커니즘 (file_path prefix로 구분) |
| **P1** | Case 2와 동일 |

### Case 4 — 외부 SaaS 형태 운영

**예**: 여러 조직에 ckg를 SaaS로 제공, multi-tenant.

| 우선순위 | 작업 |
|---|---|
| **P0 즉시** | B1, C1, D1, D2, D7 모두 spec 그대로 + V2 보완 |
| **P0** | C2, C3, C5 (full ML classifier), C6, C7 |
| **추가 요구** | tenant 격리 (graph.db per tenant), rate limiting, audit log persistence, compliance 인증 (SOC2 등) |

---

## 6. Appendix — 위협 모델 비교

### 6.1 prompt-injection 노출 표면

| 위협 | production | 사내 |
|---|---|---|
| 외부 PR 본문 | 🔴 매우 높음 | (해당 없음) |
| 외부 Jira/issue 본문 | 🔴 매우 높음 | 🟡 사내 Jira는 동료 작성 |
| 외부 의존성 코멘트 (npm/go module) | 🔴 높음 | 🟡 corpus에 포함 시만 |
| Git commit message | 🟡 PR origin이면 위험 | 🟢 사내 commit은 trust |
| 코드 코멘트 (사내 작성) | 🟡 (외부 contributor PR) | 🟢 동료 작성 |
| README / docs | 🟡 외부 contribution | 🟢 사내 작성 |

→ 사내 환경은 **위협 표면 자체가 작음** — sanitize의 ROI도 낮음.

### 6.2 가용성 요구 비교

| 측면 | production | 사내 |
|---|---|---|
| SLA | 99.9%+ | 의도적 best-effort |
| 다운 시 영향 | 외부 사용자 응답 차단 → revenue/신뢰 손실 | 개발자 생산성 일시 저하 → 다른 도구로 우회 |
| recovery time 목표 | 분 단위 | 시간/일 단위 OK |
| circuit breaker 필요성 | 필수 | 권장 |

→ D1의 fail-closed outage는 production에선 SLA 위반, 사내에선 "일시 불편" 수준.

### 6.3 audit/compliance 요구 비교

| 항목 | production | 사내 |
|---|---|---|
| 인증 (SOC2, ISO27001) | 의무 (B2B) | 통상 불요 |
| 사용자 행위 audit | 모두 기록 + 보존 | 디버깅용으로만 |
| 데이터 격리 | tenant별 강제 격리 | 사내 사용자 모두 동일 권한 OK |
| 암호화 | 전송/저장 모두 | 사내망 한정 시 transport TLS만 |

→ C1 mTLS / D7 audit 단절은 production에선 compliance 차원에서 반드시 fix, 사내에선 의무 아님.

---

## 7. 한 줄 결론

> **CKS spec의 P0 3건(B1·C1·D1) 중 B1만 환경 무관 spec 결함이고, C1·D1은 production-specific 위험입니다. 회사 내부 도구로만 운영한다면 C1은 무시, D1은 simple regex + 옵셔널로 충분합니다. spec V2 작업 우선순위는 운영 환경 분류 후 결정해야 하며, 본 문서의 §5 케이스 식별로 자기 환경을 먼저 확정하세요.**

---

**End of CKS spec deployment-context guide.** 본 문서는 spec의 위험을 환경 종속/무관으로 분류하여 fix 우선순위 결정의 근거를 제공. `CKS-SPEC-REVIEW.md`(spec 위험 식별) + `CKS-SPEC-COMPLIANCE.md`(CKG 구현 충실도)와 함께 읽으면 "spec 위험 → 환경별 무게 → 구현 우선순위"의 3단 의사결정 가능.
