# Go Cross-Function Lock Propagation — Design Spec (D1 SSA Stage 2)

> Scope: extend the existing B1 lock detector (`internal/parse/golang/concurrency.go`
> + `concurrency_underlock.go`) so that `accessed_under_lock` edges are also
> emitted for fields touched by **callee** functions when their callers hold a
> mutex. Currently the detector is intra-function only; this is the
> "Stage 2 SSA territory" referenced in `concurrency.go:17` and
> `concurrency_underlock.go:24`.
>
> **Status**: design draft 2026-05-11. No code changes.
> **Out of scope**: cross-language locks (no equivalent semantic in TS/Sol),
> happens-before race detection (Go race detector territory), runtime trace
> ingestion (schema 1.9 W series).
> **Adjacent docs**: `docs/design/track-c-detector-gap.md` §2.9 (the
> goroutine-body lock-walk bug; already fixed in current code) and §2.10
> (`accessed_under_lock` limited-by-design note).

---

## §0. Cold start (이 spec 처음 읽는 경우)

- **무엇**: 현재 `f()` 안에서 `mu.Lock()` 한 뒤 `g()`를 호출하면 `g()`가
  접근하는 필드는 `accessed_under_lock` 엣지를 받지 못한다. 호출 그래프를
  따라 lock-state를 전파해서 `g()`의 필드 접근도 `accessed_under_lock`
  엣지를 emit하도록 확장한다.
- **왜**: 실제 Go 코드의 critical section은 대부분 helper 메서드로
  쪼개져 있다. 현재 구현은 inline body 만 보므로 go-stablenet self-graph
  에서 약 30~50% 의 실제 lock-protected 접근을 놓치고 있다고 추정 (§4.1
  측정 권장).
- **어떻게**: Pass 2 Resolve 직후 후처리 단계로 `callgraph` (이미 emit된
  `calls`/`invokes` 엣지)와 mutex-hold state를 합성. AST-only 패스로
  시작 → 정확도 부족 시 `golang.org/x/tools/go/ssa` 도입.
- **선행**: 없음. 현재 B1 Phase 1~4 위에 append. 단 `track-c-detector-gap.md`
  §2.9 의 `acquires_lock` goroutine bug 수정이 main에 있어야 baseline 가능
  (이미 수정됨, `statements.go:84` 참조).

---

## §1. 현재 상태 (B1 Phase 1~4)

### 1.1 emit되는 노드/엣지

| 항목 | 위치 | 신뢰도 |
|------|------|--------|
| `NodeMutex` (sync.Mutex/RWMutex; field/var/local) | `concurrency.go:37-171` | EXTRACTED \| INFERRED |
| `acquires_lock` / `releases_lock` | `concurrency.go:320-345` | EXTRACTED \| INFERRED |
| `accessed_under_lock` (intra-function) | `concurrency_underlock.go:39-67` | INFERRED |

### 1.2 명시적 미구현 (코드/문서가 인정한 갭)

| 갭 | 근거 | 영향 |
|----|------|------|
| Cross-function lock 전파 | `concurrency.go:17` ("Stage 2 SSA-based cross-function lock chain analysis is D1's scope") | `f`가 lock 보유 중 `g` 호출 시 `g`의 필드 접근에 엣지 미emit — **false negative** |
| 정밀한 lexical scope | `concurrency_underlock.go:13-21` ("ANY field access inside a function that performs ANY Lock/RLock is treated as accessed under all mutexes") | Lock 이전 / Unlock 이후 접근도 엣지 — **false positive** |
| Named-function goroutine body | `concurrency.go:530-531` | `go worker()` 형태에서 channel/lock 엣지 누락 |

### 1.3 운영 통계 (go-stablenet 셀프 분석, `archive/STATUS-REPORT-2026-05-04.md`)

- Mutex 노드: 170
- `acquires_lock`: 781, `releases_lock`: 834
- `accessed_under_lock`: 2916

이 숫자가 cross-function 전파 후 얼마나 늘어나는지가 본 spec 의 핵심 KPI
(§4.1 — D1 baseline 측정).

---

## §2. 목표 동작

### 2.1 새 엣지 emit 규칙

`accessed_under_lock(field, mutex)` 를 다음 두 경로에서 emit:

| 경로 | 기존 (B1) | 본 spec (D1) |
|------|-----------|--------------|
| (a) 같은 함수 body 안에서 `mu.Lock()` 후 `s.field` 접근 | ✅ | ✅ (유지) |
| (b) `mu.Lock()` 후 `helper()` 호출, helper 가 `s.field` 접근 | ❌ | ✅ (신규) |

### 2.2 신뢰도 정책

| 케이스 | Confidence |
|--------|-----------|
| (a) intra-function (이미 INFERRED) | INFERRED (그대로) |
| (b) cross-function, callee 가 동일 mutex 의 Lock 도 acquire | EXTRACTED — locking 의도 명백 |
| (b) cross-function, callee 는 lock 무관 | INFERRED |
| (b) callee 가 *다른* mutex acquire | AMBIGUOUS (lock ordering bug 후보로 surface) |

`AMBIGUOUS` 라벨은 사용자가 `Recovery panel` 류 surface에서 별도 확인할 수
있게 한다 — schema 1.8 §11.3 hybrid 와 동일한 패턴.

### 2.3 새 엣지 타입은 없다

`accessed_under_lock` 한 종류만 확장. `pkg/types/enums.go` 변경 없음. 따라서
schema bump 도 없음 (1.8 유지). 단 새 엣지가 다수 추가되면 viewer 의 노이즈
임계점에 영향 — §3.3 노이즈 컨트롤 참조.

---

## §3. 검출 알고리즘

### 3.1 Stage A — AST-only 1-hop 전파 (먼저 시도, 작음)

후처리 단계로 `emitDerivedPasses` 직후에 새 함수 추가:

```
propagateLockedFieldAccess(g):
  for each Function/Method node f in g.Nodes:
    held = mutexes locked in f.body          // 기존 collectHeldMutexes
    if held is empty: continue
    for each `calls` or `invokes` edge (f -> callee) in g.Edges:
      if callee is not in g.Nodes: continue
      for each field touched in callee.body: // 새 traversal
        emit accessed_under_lock(field, mutex) for each mutex in held
```

- **장점**: 추가 의존성 없음. 1-hop만 보므로 cycle 위험 없음. 30분~1시간
  분량 PR.
- **단점**: 2-hop 이상 (f→g→h) 는 놓침. helper 가 channel send 로 우회하면
  놓침.
- **추정 gain (self-graph)**: `accessed_under_lock` 2916 → 4000~5500
  (rough; §4.1 측정으로 확정).

### 3.2 Stage B — Reachability-bounded 전파 (정확도 개선)

DFS 로 lock-holding 함수에서 reachable 한 모든 함수의 필드 접근을 합산.
Cycle 방지를 위해 visited set 사용. depth limit 권장 (e.g. 3-5).

```
propagateLockedFieldAccessDFS(g, maxDepth=5):
  callees = adjacency list from `calls`/`invokes` edges
  for each Function f with held = mutexes-locked-in-f.body, non-empty:
    visited = {f}
    queue = [(f, 0)]
    while queue:
      (n, depth) = queue.pop()
      if depth > maxDepth: continue
      for each field touched in n.body:
        emit accessed_under_lock(field, mutex) for each mutex in held
      for each callee c in callees[n]:
        if c not in visited:
          visited.add(c)
          queue.push((c, depth+1))
```

- **장점**: 실제 critical section 의 transitive reach 캡처.
- **단점**: DFS depth limit 가 새 매직넘버. 큰 그래프에서 노이즈 증가
  위험. confidence 라벨링이 더 어려움 (depth 1 vs 5 의 신뢰 차이).

**권고**: Stage A로 먼저 launch, KPI 측정 후 Stage B는 별도 PR.

### 3.3 노이즈 컨트롤

- **stdlib / 외부 패키지 함수 호출 무시**: `g.Nodes` 에 없는 callee 는 skip
  (이미 §3.1 에 포함). 이로 인해 `mu.Lock(); log.Printf(...)` 같은 false
  positive 자동 차단.
- **읽기 전용 callee 우대 — out of scope**: 향후 `reads_field` /
  `writes_field` 분리가 들어오면 read-only 함수는 RLock 만 매칭하도록 정밀화
  가능. V0 에서는 단순화 유지.
- **deduplication**: 동일 `(field, mutex)` 쌍은 한 번만 emit. 기존
  `concurrency_underlock.go:51-58` 의 `emitted` map 패턴 재사용.

### 3.4 Stage C — SSA 기반 정밀화 (장기, 별도 spec)

`golang.org/x/tools/go/ssa` + `callgraph` + `pointer` 분석으로:
- 정확한 lexical scope (`Lock` 과 `Unlock` 사이만)
- Interface dispatch (현재 미구현)
- Pointer aliasing (다른 변수로 같은 mutex 참조)

이 단계는 SSA dependency 가 비싸고 (`packages.LoadAllSyntax` + `ssa.Build`),
빌드 시간이 1.5~2x 증가 예상. 별도 spec (`d1-ssa-propagation.md`)에서 다룬다.

---

## §4. 구현 계획

### 4.1 Pre-work — 측정 (필수)

PR 작성 전 baseline 확보:

```bash
./bin/ckg build --src=<repo> --out=/tmp/ckg-d1-baseline
sqlite3 /tmp/ckg-d1-baseline/graph.db \
  "SELECT COUNT(*) FROM edges WHERE type='accessed_under_lock';"
sqlite3 /tmp/ckg-d1-baseline/graph.db \
  "SELECT confidence, COUNT(*) FROM edges WHERE type='accessed_under_lock' GROUP BY confidence;"
```

- go-stablenet 같이 lock-heavy 한 fixture 사용 권장.
- Stage A 적용 후 동일 측정 → diff 가 KPI.

### 4.2 W1 — Stage A 구현 (S 사이즈, ≤200 LOC)

1. `internal/parse/golang/lock_propagation.go` 신규 (별도 파일, B1 코드 손
   안 댐 — 다른 세션 충돌 방지).
2. 진입점: `internal/buildpipe/pipeline.go:emitDerivedPasses` 의 `score.Compute`
   직전.
3. 입력: `*graph.Graph` (이미 모든 노드/엣지 emit 완료).
4. 출력: g.Edges 에 `accessed_under_lock` 추가.
5. 단위 테스트: `lock_propagation_test.go` — 3 fixture
   - (a) caller locks, callee touches one field (positive)
   - (b) caller locks, callee touches stdlib (skip, no edge)
   - (c) caller doesn't lock, callee touches field (no edge — guard)

### 4.3 W2 — validateAndSanitize 게이트 통과 확인

새 엣지가 `graph.Inspect` 의 dangling endpoint 검사를 통과하는지 확인.
이미 g.Nodes 안의 node ID 만 src/dst 로 쓰므로 통과해야 하지만
`internal/graph/validate.go` 의 lenient/strict 동작 확인 필수.

### 4.4 W3 — 측정 + 핸드오프

`PERF-BASELINE-2026-05-xx.md` (별도 새 파일) 에 before/after:
- 노드/엣지 카운트
- 빌드 시간
- viewer 의 lock 패널 가시성 변화 (스크린샷)

---

## §5. 결정 필요 항목

각 항목은 사용자 답변을 받아 W1 PR 본문에 반영. 답변 받기 전 구현 진입
금지 (schema 1.8 §11 8 결정과 동일 패턴).

### Q1. Stage A 의 default depth

Stage A 는 1-hop 만 본다고 §3.1 에 적었으나, callee 가 또 다른 helper 를
부르는 흔한 패턴 (`f → lockedHelper → reallyTouchesField`) 은 놓침.

- (a) 1-hop fixed (가장 단순, false negative 큼)
- (b) depth=2 fixed (sweet spot 추정, 노이즈 통제 가능)
- (c) Stage B 곧바로 도입 (코드 복잡도 증가, KPI 보강)

**권고**: (b). 측정 후 (c) 로 이행.

### Q2. AMBIGUOUS 라벨링 시점

§2.2 의 "다른 mutex acquire" 케이스를 AMBIGUOUS 로 분류한다고 했으나
실제로 그것이 항상 bug 후보는 아님 (정상 패턴: outer lock 해제 후 inner
lock acquire). 노이즈 가능성.

- (a) AMBIGUOUS 라벨 사용 — 사용자 surface 에 노출
- (b) INFERRED 로 통일 — 모호 케이스도 일단 묶음
- (c) 별도 엣지 타입 `lock_order_concern` 추가 — schema bump 필요

**권고**: (b). AMBIGUOUS 는 hunk-graph §11.3 패턴처럼 LLM 노출 시 cost 가
크고 사용자 confusion 위험.

### Q3. interface dispatch (`invokes` 엣지) 처리

현재 `track-c-detector-gap.md` §2.3 에서 `invokes` 가 0 (semantic split
미구현). D1 전파에서 `invokes` 엣지를 따라가야 interface 메서드 호출 너머의
필드 접근을 잡을 수 있다. 그러나 `invokes` 가 P1 으로 분류된 별도 작업.

- (a) `invokes` 구현 완료 대기 (의존성 명시)
- (b) `calls` 만 따라감 (interface dispatch 케이스 false negative 수용)
- (c) `invokes` 와 `calls` 둘 다 따라감 (둘 다 emit 시 즉시 활성화 — 명시
  의존 없이 자연스럽게 활성화)

**권고**: (c). 코드 변경량 동일, future-proof.

### Q4. Goroutine body 의 cross-function 전파

`go helperWithLock()` 의 helper 안 필드 접근은 어떻게?

- (a) 동일 규칙 적용 — goroutine body 도 caller 의 lock 상속
- (b) goroutine 경계 차단 — `go func` 안은 별도 분석 단위
- (c) goroutine 전용 confidence (INFERRED 강제) — 동기성 보장 약함을 신호

**권고**: (c). Goroutine 은 caller scope 와 별개 schedule 이므로 lock-state
상속이 항상 옳지는 않음.

### Q5. 빌드 시간 예산

Stage A 가 노드 수 N, 평균 callee fanout f 에 대해 O(N·f·avg_body_field_count)
연산. go-stablenet 추정: 220K · 3 · 5 ≈ 3M ops, sub-second. 그러나 누적
체크 필요.

- (a) 빌드 시간 +5% 미만이면 default 활성화
- (b) opt-in flag (`--lock-propagation`) 로 시작, 측정 후 default 전환
- (c) cold path 만 활성화 — incremental 빌드 skip

**권고**: (b). 운영자 회피 경로 확보.

### Q6. 기존 INFERRED accessed_under_lock 엣지와 dedup

Stage A 가 emit 하는 엣지 중 일부는 (a) 케이스로도 이미 emit 됐을 수 있음
— 같은 함수 안에서 helper 도 호출하고 직접 필드 접근도 함.

- (a) `emitted` map 으로 dedup — 첫 emit 의 confidence 유지
- (b) confidence 가 더 높은 엣지로 overwrite — EXTRACTED 우선
- (c) 두 엣지를 별도로 보존 — 분석 가능하지만 viewer 노이즈

**권고**: (b). EXTRACTED > INFERRED > AMBIGUOUS 우선순위.

### Q7. Test fixture 위치

`internal/parse/golang/testdata/concurrency/` 에 cross-fn 케이스 추가? 아니면
별도 `testdata/lock_propagation/` ?

- (a) 기존 디렉토리 — 발견성 ↑
- (b) 별도 디렉토리 — 격리 ↑, 다른 B1 테스트와 충돌 위험 ↓

**권고**: (b). 새 fixture 가 기존 B1 회귀 테스트 결과를 의도치 않게 바꿀
위험 차단.

### Q8. Schema/문서 업데이트

`pkg/types/enums.go:35-38` 의 stale 주석 ("B1 Wave 5 will emit; the parser
does not produce Mutex nodes yet") 갱신 시점.

- (a) 본 spec 의 W1 PR 에 동봉
- (b) 별도 docs-only PR (prompt cache 영향 최소화)
- (c) 코드 변경 없이 docs/SCHEMA.md 만 수정

**권고**: (b). enums.go 는 hot path (모든 세션 컨텍스트), 변경 시 cache
무효화 비용 (rules/prompt-cache.md).

---

## §6. 테스트 전략

### 6.1 단위 — fixture 기반

```go
// testdata/lock_propagation/single_hop.go
type Counter struct {
  mu sync.Mutex
  n  int
}
func (c *Counter) Inc() {
  c.mu.Lock()
  defer c.mu.Unlock()
  c.touch()           // helper
}
func (c *Counter) touch() { c.n++ }  // <- accessed_under_lock(n, mu) expected
```

### 6.2 회귀 — golden snapshot

B1 의 기존 accessed_under_lock 카운트가 *증가* 만 하고 감소하지 않음 확인.

### 6.3 self-graph KPI

`/tmp/ckg-self-d1/graph.db` 만들어서:
- accessed_under_lock 카운트 비교
- 새로 추가된 엣지의 destination mutex 가 의미적으로 합리적인지 sampling
  (10건 정도 manual)

---

## §7. 참조

- 기존 구현:
  - `internal/parse/golang/concurrency.go` (B1 Phase 1~3)
  - `internal/parse/golang/concurrency_underlock.go` (B1 Phase 4)
  - `internal/parse/golang/statements.go:84` (lock-walk 버그 수정 후 상태)
- 관련 design doc:
  - `docs/design/track-c-detector-gap.md` §2.9, §2.10
  - `docs/spec-ckg-v0.2.md` §2 R2.x (concurrency invariants)
- 운영 통계: `docs/archive/STATUS-REPORT-2026-05-04.md` line 29
- Schema enum (수정 대상 아님): `pkg/types/enums.go:142-145` (lock edge types)
- 향후 SSA 도입 시 참고: `golang.org/x/tools/go/ssa`,
  `golang.org/x/tools/go/callgraph`

---

## §8. 작업 순서 (다음 세션 시작점)

1. **이 spec 의 §5 결정 항목 8개에 사용자 답변 받기**
2. W1 — `lock_propagation.go` 신규 + 3 fixture (~200 LOC)
3. 측정 (§4.1) — before/after KPI 기록
4. W2 — validateAndSanitize 회귀
5. W3 — 핸드오프 작성 (`SESSION-HANDOFF-<date>.md` 의 §6 후보에 등재)

W2/W3 는 W1 후 동일 세션에서 처리 가능. W1 만 별도 세션 권장 (결정 합의
시간 소요).
