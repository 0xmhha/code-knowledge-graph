# CKS Spec 비판적 검토

> **대상**: `/Users/0xtopaz/work/github/onlyhyde/study/projects/stablenet-ai-agent/claudedocs/04-cks-deep-dive.md` (1,312줄, 14 sections)
> **검토 관점**: 논리적 일관성 / 기술적 현실 가능성 / 운영 안정성 / 더 좋은 방안 가능성
> **결론 한 줄**: 전반 골격은 견고. 다만 **논리 모순 6건, 기술 현실성 의문 7건, 운영 안정성 위험 7건**이 있고 **12건의 더 나은 방안**을 제안함. **B1 (task_type↔playbook 미매핑) / C1 (mTLS-on-stdio) / D1 (sanitize fail-closed의 전체 outage 야기) 3건은 spec 그대로 구현 시 production 운영이 어려우며 우선 수정 권장**.
>
> **선행 문서**: `docs/analysis/CKS-SPEC-COMPLIANCE.md` (CKG의 spec 충실도 매트릭스)
> **마지막 갱신**: 2026-05-05

---

## 목차

A. [충분히 잘 설계된 부분](#a-충분히-잘-설계된-부분-변경-권장-안-함)
B. [논리적 모순 / 누락 (6건, P1)](#b-논리적-모순--누락-6건-p1)
C. [기술적 현실성 의문 (7건, P2)](#c-기술적-현실성-의문-7건-p2)
D. [운영 안정성 위험 (7건, P3)](#d-운영-안정성-위험-7건-p3)
E. [더 좋은 방안 제안 (12건)](#e-더-좋은-방안-제안-12건)
F. [종합 평가](#f-종합-평가)
G. [Appendix — 우선순위 매트릭스](#g-appendix--우선순위-매트릭스)

---

## A. 충분히 잘 설계된 부분 (변경 권장 안 함)

| 항목 | 평가 |
|---|---|
| **4-Layer 책임 분리** (Storage / Working Memory / Pager / Query API) | 🟢 클래식한 RAG 아키텍처를 코드 도메인에 정확히 매핑 |
| **CPU 비유 교정** (§2) | 🟢 "context = RAM"이 맞고 "Pager가 누락된 핵심 계층"이라는 진단이 정확 |
| **Citation Enforcement 의무화** (§6.4 결정 5) | 🟢 hallucination 방지의 단일 가장 효과적 메커니즘 |
| **Multi-backend 분담** (§4) | 🟢 Graph(구조) / Vector(의미) / BM25(정확) / AST(문법) / File(원본) — 각각의 약점이 다른 backend로 보완됨 |
| **Evidence Pack 표준 형식** (§6.1) | 🟢 task_type/goal/risks/concurrency_context의 명시는 LLM context의 질적 향상 |
| **Playbook 사전 정의** (§6.3) | 🟢 free exploration 대비 token 효율 명백 |
| **Sanitize 단일 진입점** (§6.2 step 8.5) | 🟢 default-deny 보장의 정공법 |
| **Runtime Evidence를 Phase 2~3로 분리** (§13) | 🟢 graceful degradation 원칙 — 정적 분석만으로도 작동 |

---

## B. 논리적 모순 / 누락 (6건, P1)

### B1. **task_type 8종 vs playbook 4종 mismatch** (§6.2 ↔ §6.3)

spec §6.2 step 0은 task_type 8종으로 분류:
> `task_type ∈ {bug_fix, feature_add, refactor, perf_optimization, concurrency_safety, io_reliability, security_review, architecture_explain}`

그런데 §6.3은 playbook **4종만** 정의: `bug_fix / feature_add / concurrency_safety / architecture_explain`.

**누락된 4종 (refactor / perf_optimization / io_reliability / security_review)에 대한 playbook 또는 fallback rule 없음**. 의사코드 `playbook = select_playbook(task_type)`이 unmapped task_type에서 어떻게 동작하는지 미정의.

→ **권장**: 4 task_type을 default playbook으로 매핑하거나, 8개 모두 playbook 정의. (§E1 참조)

### B2. **`handles` vs `handles_message` 의미 중복** (§4.1)

- Graph 2 Semantic: **`handles`** = handler/callback → 처리 대상 event type
- Graph 5 Distributed: **`handles_message`** = handler → message type

두 edge의 의미적 차이가 모호. spec §4.1 본문은 "`handles`는 dispatch 매핑"이라 하고, "`handles_message`"는 별다른 정의 없음. **같은 edge를 두 곳에서 emit해야 하는지, 우선순위가 어떤지 불명**.

→ **권장**: 통합하거나, "consensus 메시지(P2P)는 G5, in-process event는 G2"처럼 명시적 경계. (§E2)

### B3. **`writes` vs `modifies` 경계 모호** (§4.1)

- Graph 2 Semantic: **`writes`** = "함수가 쓰는 상태/필드 (`modifies`의 필드 수준 세분화)"
- Graph 3 Execution: **`modifies`** = "함수가 수정하는 상태 (struct/package 수준)"

spec 자체가 "modifies의 필드 수준 세분화 = writes"라고 정의하지만, **그러면 항상 `writes`가 emit될 때 `modifies`도 emit되어야 하는가?** 한 함수가 struct.field에 쓰면 G2 writes + G3 modifies 양쪽 모두? 또는 둘 중 하나만?

→ **권장**: "writes는 정밀 추적 가능 시 우선, modifies는 fallback" 같은 dispatch 규칙 명시. 또는 modifies를 writes로 통합 후 granularity를 attribute(`field` / `struct` / `package`)로 표현. (§E3)

### B4. **Working Memory의 fact dedup / conflict 처리 부재** (§5.4)

LLM이 `remember_fact`로 fact를 누적하는데, **동일 (subject, predicate)에 대해 conflict하는 fact가 들어왔을 때 처리 규칙 없음**.

예:
- step 1: `remember_fact("handleCommitMsg", "has_bug", "nil deref at line 142")`
- step 5: `remember_fact("handleCommitMsg", "has_bug", "verified fix at line 142")`

→ **권장**: `(subject, predicate, source_step_id)`로 unique key + temporal versioning. conflict는 archive + 최신 우선. (§E12)

### B5. **Test 노드의 emit 책임 누락** (§4.1 ↔ §4.7)

§4.1에 Test 노드 정의 + `tests` edge 정의. 그러나 **§4.7 3수준 파서 파이프라인 표에 Test 노드를 어느 수준이 emit하는지 명시 없음**.

추정: 수준 2의 Go testing 패키지 인식 필요 (e.g. `func TestXxx(t *testing.T)` + `t.Run` subtests). 그러나 spec에 명시 없으면 구현자마다 다르게 해석.

→ **권장**: §4.7 표에 "수준 2: Test 노드 + tests edge (testing 패키지 시그니처 인식)" 추가.

### B6. **`changed_in` 정밀도 정의 모호** (§4.1)

> `changed_in`: symbol → commit

**file 단위인지 symbol(line range) 단위인지 명시 없음**. spec §4.1은 symbol 단위라고 시사하지만, git diff는 line 단위 → symbol에 매핑하려면 each commit의 patch를 AST 위치와 교차 검사해야 함 (비용 큼). file 단위 fallback이면 spec과 다른 정밀도.

→ 이미 CKG가 file-level로 구현했고 spec V1+에서 line-level 보강 예정. **spec에 정밀도 옵션 명시 필요** — `changed_in.granularity ∈ {file, symbol_via_line_range}`.

---

## C. 기술적 현실성 의문 (7건, P2)

### C1. **stdio MCP transport에서 mTLS는 비현실적** (§7.5)

spec §7.5 "Caller 전제: mTLS verified caller — envelope `caller` 필드가 client cert SAN과 일치".

**stdio는 본질적으로 TLS layer 없음**. fork + stdin/stdout JSON-RPC는 cert 검증 불가능. spec은 stdio/HTTP/SSE 3 transport를 모두 지원한다고 (§2.3) 했으나 mTLS 요건은 stdio에 비현실적.

→ **권장**: stdio는 process-level identity (Unix socket + `SO_PEERCRED`, Windows named pipe + token), HTTP/SSE만 mTLS. spec §7.5 caller 검증을 transport별로 분리 명시. (§E4)

### C2. **5 backend 단일 프로세스의 메모리 압박**

go-stablenet급 corpus(2,142 files / 217K nodes / 669K edges) 기준 추정:

| Backend | 추정 RAM |
|---|---|
| Graph DB | ~200MB |
| Vector (217K × 384-dim float32) | **~330MB** |
| BM25 (tantivy/bleve) | ~100MB |
| AST cache (직렬화 후 lazy load 안 하면) | **~1GB+** |
| File blob | git 자체 |
| **총 합** | **1.5~2GB** |

대규모 monorepo (10× 크기)면 단일 프로세스 RAM 16GB 이상 요구. spec §4 분리 운영(별도 service)을 명시하지 않으면 OOM 위험.

→ **권장**: AST cache는 lazy load (메모리 캐시는 LRU N=1000), Vector + BM25는 SQLite 단일 파일에 통합 (`sqlite-vec` extension), 메모리 budget envelope에 명시. (§E9)

### C3. **incremental indexing의 수준 2/3 비용 명시 누락** (§4.6)

spec은 "변경 파일만 재파싱 → 해당 파일의 노드/엣지만 update"를 명시. **그러나**:

- 수준 2 (`go/packages`)는 **패키지 단위 로드** — 단일 파일 변경도 패키지 전체 재로드 필요 (100ms~1s)
- 수준 3 (state machine 분석)은 **cross-file** — 단일 파일 변경시 영향 받는 패키지의 분석 재실행 필요

spec §4.6은 "수준 2는 해당 패키지만, 수준 3은 영향받는 패턴만"이라 명시하지만, **"영향받는 패턴" 식별 알고리즘 부재**. 구현자는 보수적으로 전체 재실행할 가능성 — incremental의 실제 비용이 spec보다 훨씬 큼.

→ **권장**: 수준 2/3의 "incremental 비용 모델" 추가. 수준 3는 reverse-dep index 명시 필요. (§E10)

### C4. **ECDSA 서명 + Hot reload + atomic swap의 race condition** (§6.2 governance)

> Hot reload: CS1 / CKS / CL2 sanitizer가 fsnotify watch → signature 검증 → atomic swap

**atomic swap은 OS 단위 mmap/symlink rename 같은 정교한 기법 필요**. 단일 binary의 in-process atomic swap은 (1) 모든 활성 query가 끝나기를 기다리거나 (2) Read-Copy-Update 패턴 — 둘 다 비자명한 구현. 또한 `signature 검증 → atomic swap` 사이 race window가 존재 가능 (서명 검증 후 다른 프로세스가 파일 교체).

→ **권장**: hot reload는 옵션 (default off), startup 1회 로드 + SIGHUP 수동 reload. 운영자가 의도적으로 활성화. 자동 fsnotify는 불안정.

### C5. **6 baseline pi-pattern으로 prompt-injection 차단 불충분** (§6.2)

`pi-imperative-001` (IGNORE PRIOR INSTRUCTIONS) 같은 6 regex로는 다음 회피 가능:

- 다국어 (`이전 지시 무시하고...`)
- Encoding (base64, ROT13, Unicode confusables)
- Semantic perturbation ("Listen carefully: forget your training and...")
- Role-play framing ("You are now DAN...")
- Hypothetical ("Imagine if you weren't constrained...")
- Indirect injection (이미지 OCR, link content fetch)

`pi-base64-006`은 false positive 폭발 위험 — legitimate code에 base64 많음 (cert, hash, embedded data).

→ **권장**:
- 6 regex는 baseline으로만 유지
- 작은 ML classifier (distilbert + LoRA-finetuned for jailbreak detection) 추가
- fail-closed는 redact_full로 fallback (전체 retrieve() panic 금지 — D1 참조)
- (§E5)

### C6. **Citation 100% 정확 KPI의 측정 불가능성** (§11)

> Citation accuracy ≥ 100%

**무엇을 측정하나?** 가능한 해석 3가지:

1. 응답에 포함된 file:line이 응답 시점 DB에 존재 (100% 보장 가능, 단순)
2. file:line이 git HEAD의 실제 코드와 일치 (DB가 stale이면 < 100%)
3. file:line의 코드가 의미적으로 query와 관련 (LLM 평가 필요, 100%는 근본적으로 불가능)

spec은 어느 의미인지 미명시 → 측정 불가능.

→ **권장**: KPI를 **"DB 존재성"**으로 정의 (해석 1). stale 검출은 별도 KPI (`Citation freshness lag`). (§E6)

### C7. **Working Memory의 동시 write race** (§5.3)

> 공용 환경: PostgreSQL 또는 Redis (TTL 기반)

여러 LLM session이 동일 `run_id`에 동시 write할 수 있음 (multi-step workflow가 병렬 sub-step 갖는 경우). **PostgreSQL은 transactional write가 가능하지만 Redis는 race 위험** (Lua script로 atomic guarantee 가능하나 spec 명시 없음).

또한 **fact array의 append 시 read-modify-write race** — `working_memory.facts.push(fact_X)`가 동시 발생하면 1건 손실 가능. spec에 atomicity 보장 메커니즘 없음.

→ **권장**: SQLite WAL mode + advisory lock으로 단일화. PostgreSQL/Redis 분기는 spec V2+ 영역. (§E7)

---

## D. 운영 안정성 위험 (7건, P3)

### D1. **Sanitize fail-closed가 전체 retrieve() 실패** (§6.2)

> sanitize step 자체 panic → `retrieve()` 전체 실패 (Evidence Pack 미반환)

sanitize의 정규식 panic이나 timeout 1건이 **전체 CKS 응답을 막음**. circuit breaker 없음. 한 corrupt sanitization rule이 전체 시스템 outage를 야기.

→ **권장**:
- 패턴별 timeout (e.g. 100ms per regex)
- 패턴 panic은 해당 패턴만 skip + warning, retrieve()는 진행
- 전체 sanitize 실패 시 redact_full 단일 fallback (panic 안 함)
- 누적 fail count > threshold 시 sanitize 비활성화 + ops alert
- (§E5)

### D2. **{run_id} state-store 디렉토리 폭증** (§5.3)

> Cleanup: 완료된 run은 일정 기간 후 archive (eval data로 활용)

**"일정 기간"이 정의 안 됨, archive policy 부재**. 수개월 운영 시 수십만 run_id 디렉토리 누적 → inode 고갈, ls/find 느려짐.

→ **권장**:
- TTL 명시 (e.g. 30일)
- Archive는 단일 tarball/SQLite로 압축 후 raw 디렉토리 삭제
- 운영자가 디스크 quota 설정 가능

### D3. **매 query마다 git rev-parse HEAD 호출 비용** (§4.6)

> Freshness Checker 동작: `current_head = git rev-parse HEAD`

**git command exec은 cheap이지만 high-frequency MCP query에선 누적**. Docker container / read-only mount / git submodule edge cases에서 실패 가능. 또한 fork+exec overhead (수 ms × 분당 수백 query).

→ **권장**:
- File-system mtime 기반 cheap check (1초당 1회 cap)
- Force refresh는 명시적 호출만
- git unavailable 환경 graceful skip + 1회 warning

### D4. **Sanitization rules.yaml 단일 장애점** (§6.2)

> Fail-closed: 파일 부재 또는 signature 검증 실패 시 해당 subsystem startup abort

**파일 corruption 또는 서명 키 분실 시 모든 ckg 인스턴스 시작 못 함**. 백업/롤백 메커니즘 없음. 분산 환경에선 모든 노드가 동시에 down.

→ **권장**:
- Embedded fallback (built-in baseline rules) — startup blocker가 되지 않도록
- Last-known-good cache: 이전 정상 로드된 rules를 local cache → 새 파일 검증 실패시 last-known-good 사용 + alert
- 다단 검증: signature → schema validation → eval 가능 여부

### D5. **5 MCP Tool Group namespace conflict** (§7.4)

spec §7.4 표는 `cks.index.*` / `cks.graph.*` / `cks.context.*` / `cks.memory.*` / `cks.ops.*` 5종. 그러나:

- `find_symbol`이 `cks.index.find_symbol`인지 `cks.context.find_symbol`인지 명확하지 않은 도구가 있음 (e.g. `semantic_search`은 context인데 index 같기도)
- 같은 capability를 여러 group에 등록할지 spec 명시 없음

→ **권장**: capability ↔ group을 1:1로 강제 (single source of truth), spec §7.4 표를 unique mapping으로 강화.

### D6. **Index materialization의 부분 실패 미처리** (§12.1 Phase D)

> Phase D. Index Materialization | Graph DB + Vector DB + BM25 + AST Cache + Freshness metadata | 4개 storage backend 초기화

**한 backend (예: Vector embedding) 실패시 처리 정책 없음**. 부분 실패 → Graph는 있지만 Vector 없는 일관성 없는 상태로 query 응답.

→ **권장**:
- Phase D를 transactional로 — 모두 성공 또는 모두 rollback
- 또는 "graceful degradation" 명시: Vector 실패시 BM25 fallback (이건 §10에 있음, 일관성 OK)
- Bootstrap report에 "어떤 backend가 누락됐는지" 명시

### D7. **Direct CLI 경로의 audit trail 단절** (§7.5)

> manifest_ref 필수 — CKS도 workflow 안에서 호출되므로 audit trace. **Direct CLI 경로(developer tool)는 예외**.

**security 구멍**. Direct CLI = 개발자 직접 호출 = audit 안 됨. 그런데 직접 CLI는 production graph에 접근 가능 → audit 필요.

→ **권장**: Direct CLI도 local audit log (sqlite의 `audit_log` 테이블)에 기록. caller=`cli:user@hostname`으로 명시.

---

## E. 더 좋은 방안 제안 (12건)

### E1. task_type → playbook 매핑을 명시
8 task_type을 4 playbook + override attribute로:
```yaml
task_type_to_playbook:
  bug_fix: bug_fix
  feature_add: feature_add
  refactor: feature_add        # 기존 패턴 학습 같음
  perf_optimization: bug_fix   # 병목 찾기 = anchor → impact
  concurrency_safety: concurrency_safety
  io_reliability: bug_fix
  security_review: bug_fix     # CVE 패턴 → impact
  architecture_explain: architecture_explain
```

### E2. handles vs handles_message 통합
spec에 둘 다 있을 이유 없음. **`handles_message`로 통합** (G5 Distributed가 더 specific). G2의 `handles`는 삭제.

### E3. writes vs modifies 통합
**`writes` 단일 edge + `granularity` attribute** (`field` / `struct` / `package`). G3의 `modifies` 삭제. spec 단순화 + 구현자 모호성 제거.

### E4. mTLS는 HTTP/SSE transport에만, stdio는 process-level identity
spec §7.5 caller 검증을 transport별로 분리:
- stdio: `caller = "cli:" + os.User + "@" + os.Hostname` (audit only, 신뢰는 OS file permission)
- HTTP/SSE: mTLS cert SAN
- Unix socket: `SO_PEERCRED`로 PID/UID 추출

### E5. Sanitize는 LLM-classifier로 보강 + circuit breaker
- 6 regex baseline + 작은 ML classifier (distilbert-jailbreak)
- Per-pattern timeout (100ms)
- Pattern panic은 해당 pattern skip + warning, retrieve() 진행
- 누적 fail rate > 1% 시 sanitize 비활성화 + alert

### E6. Citation Enforcement는 schema-level Validator
응답 직전 Validator:
```
for snippet in evidence_pack.context_snippets:
    if not regex_match(r'^([\w/.-]+):(\d+)(-\d+)?$', snippet.citation):
        evidence_pack.warnings.append(f"invalid_citation_format: {snippet.id}")
        continue
    file_path, line = parse(snippet.citation)
    if not store.exists(file_path, line):
        evidence_pack.warnings.append(f"citation_not_found: {snippet.id}")
        continue  # snippet 제거
```

### E7. Working Memory persist는 단일 backend (SQLite WAL mode)
- 단일 환경/공용 환경 분기 제거
- SQLite WAL + advisory lock으로 동시 안전
- PostgreSQL/Redis는 V2+ 옵션

### E8. RRF + 작은 학습된 reranker
- RRF는 cold start 좋지만 long tail 약함
- 최종 top-K(20)에만 cross-encoder (bge-reranker-base) 적용
- 학습 데이터는 LLM-as-judge로 부트스트랩 (eval 결과 활용)

### E9. 메모리 압박 회피 아키텍처
- Vector + BM25는 SQLite 단일 파일 (`sqlite-vec`)
- AST cache는 LRU(1000) + 직렬화 + lazy load
- File blob은 git에 의존 (별도 저장 X)
- Graph DB만 in-memory + working set

### E10. incremental indexing 비용 모델 명시
spec §4.6에 추가:

| 변경 단위 | 수준 1 | 수준 2 | 수준 3 |
|---|---|---|---|
| 1 file | 10ms | 100ms (해당 패키지) | 동기적: 영향 분석 후 결정 / 백그라운드: 전체 |
| 1 package | 100ms | 1s | 1~10s |
| 전체 rebuild | 10s+ | 30s+ | 60s+ |

### E11. Phase 2~3 Runtime Evidence를 KPI에 반영
§11 KPI에 추가:
- **Race detection coverage** (정적 분석으로 잡은 race 중 `go test -race`로 검증된 비율)
- **Concurrency edge precision** (정적 emit 대비 런타임 실증된 비율)

### E12. fact dedup/conflict policy 명시
spec §5에 추가:
```yaml
fact_storage:
  unique_key: (subject, predicate)
  on_conflict:
    policy: replace_with_archive
    archive_table: facts_history
    keep_latest_n: 5
  invalidation:
    on: source_step_id_invalidated
    cascade: dependent_decisions
```

---

## F. 종합 평가

| 영역 | 평가 |
|---|---|
| **전체 골격** | 🟢 4-Layer 분리, Pager 강조, multi-backend, Citation 의무화 — 모두 견고 |
| **논리 일관성** | 🟡 6건 모순/누락 — 주로 정의 모호, dispatch 규칙 부재 |
| **기술 현실성** | 🟡 7건 의문 — mTLS-on-stdio, 메모리 압박, sanitize fail-closed 등 |
| **운영 안정성** | 🟡 7건 위험 — 단일 장애점, race condition, audit 단절 |
| **개선 여지** | 🔵 12건 제안 — spec V2 작업 시 적용 권장 |

**핵심 메시지**: 본 spec은 **설계 의도와 거시적 구조는 매우 견고**합니다. CPU 비유 교정, Pager의 핵심성 발견, multi-backend 분담, Citation Enforcement, Sanitize default-deny 모두 정확한 진단입니다. 다만:

- **세부 정의의 모호성** (B1-B6)
- **stdio/메모리/incremental의 비현실적 가정** (C1-C3)
- **fail-closed의 가용성 위험** (D1, D4)

은 V2 spec에서 보완이 필요합니다.

특히 **B1 (task_type↔playbook 미매핑)**, **C1 (mTLS-on-stdio)**, **D1 (sanitize fail-closed가 전체 outage 야기)** 3건은 **spec 그대로 구현하면 production 운영이 어렵습니다** — 우선 수정 권장.

---

## G. Appendix — 우선순위 매트릭스

### 즉시 수정 권장 (production 차단 요인)

| ID | 항목 | 분류 | 영향 |
|---|---|---|---|
| **B1** | task_type 8종 ↔ playbook 4종 미매핑 | 논리 모순 | unmapped task_type에서 retrieve() 동작 미정의 |
| **C1** | stdio MCP에 mTLS 요구 | 기술 비현실 | spec 그대로 구현 불가능 |
| **D1** | sanitize fail-closed가 retrieve() 전체 panic | 운영 안정성 | 한 corrupt rule이 전체 outage |

### V2 보완 권장 (구현 가능하나 개선 여지)

| ID | 항목 | 분류 |
|---|---|---|
| B2 | handles vs handles_message 중복 | 논리 |
| B3 | writes vs modifies 경계 모호 | 논리 |
| B4 | fact dedup/conflict 부재 | 논리 |
| B5 | Test 노드 emit 책임 누락 | 논리 |
| B6 | changed_in 정밀도 모호 | 논리 |
| C2 | 5 backend 메모리 압박 | 기술 |
| C3 | incremental 수준 2/3 비용 누락 | 기술 |
| C5 | 6 pi-pattern 불충분 | 기술 |
| C6 | Citation 100% KPI 측정 불가 | 기술 |
| C7 | Working Memory 동시 write race | 기술 |

### V3 후순위 (정책/거버넌스)

| ID | 항목 | 분류 |
|---|---|---|
| C4 | ECDSA hot reload race | 안정성 |
| D2 | run_id 디렉토리 폭증 | 안정성 |
| D3 | git rev-parse 비용 | 안정성 |
| D4 | sanitization rules 단일 장애점 | 안정성 |
| D5 | 5 Tool Group namespace conflict | 안정성 |
| D6 | Phase D transactional 보장 | 안정성 |
| D7 | Direct CLI audit 단절 | 안정성 |

### 제안 (12 improvements)

| ID | 영역 | 핵심 |
|---|---|---|
| E1 | playbook 매핑 | 8 task_type → 4 playbook 명시 |
| E2 | edge 통합 | handles_message 단일 |
| E3 | edge 통합 | writes + granularity attr |
| E4 | identity | transport별 caller 검증 |
| E5 | sanitize | ML classifier + circuit breaker |
| E6 | citation | schema validator |
| E7 | working memory | SQLite WAL 단일 |
| E8 | retrieval | RRF + small reranker |
| E9 | 메모리 | sqlite-vec + LRU AST |
| E10 | incremental | 비용 모델 명시 |
| E11 | KPI | runtime evidence 포함 |
| E12 | facts | dedup/conflict policy |

---

**End of CKS spec critical review.** 본 문서는 spec V2 작업 또는 CKG 구현 시 spec과 실제 사이의 gap을 인식하기 위한 reference. `CKS-SPEC-COMPLIANCE.md`(CKG ↔ spec 충실도)와 함께 읽으면 spec 자체의 품질과 구현의 품질을 분리해서 판단 가능.
