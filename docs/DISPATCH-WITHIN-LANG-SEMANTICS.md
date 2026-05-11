# Dispatch — Within-Language Semantics Implementation

> 다음 작업을 시작할 때 **이 문서 하나만 읽으면** 어디서 시작해서 어디로
> 끝나는지 알 수 있도록 정리한 dispatch 가이드. 26개 결정 항목은 모두
> 합의 완료 (2026-05-11), 각 spec 의 §5.0 에 박제됨.
>
> **상태**: 2026-05-11 작성. 본 인덱스의 작업이 모두 land 될 때까지 유효.
> **유관 진행 작업**: schema 1.9 W1~W2 (cross-language interop, HTTP/gRPC)
> 가 별도 세션에서 진행 중. **직교 dimension** 이므로 enums.go append 만
> 충돌 방지하면 병렬 진행 가능.

---

## §0. Cold start — 이 문서 받자마자 할 것

1. `git pull` — 다른 세션 commit 흡수
2. 본 문서 §3 의 "어디서 시작?" 표 확인
3. 해당 spec 의 §5.0 결정 결과 확인 (이미 박제됨, 재논의 불필요)
4. §4 의 코딩 규칙 한 번 훑고 진입

읽지 말 것:
- ❌ 각 spec 의 §5.Q1~Q10 원본 옵션 비교 (이미 §5.0 으로 종결)
- ❌ schema 1.9 spec (별도 세션 영역)

---

## §1. 작업 범위 한눈에 — 4건

| ID | 언어 | 주제 | 사이즈 | 우선순위 | 상태 |
|----|------|------|--------|----------|------|
| **W-D** | (cross) | `pkg/types/enums.go` stale comment 정정 | XS (~30 LOC) | P2 (but first) | 코드는 land 됨, commit 만 pending |
| **W-A** | Go | Cross-function lock propagation (D1) | M (~300-400 LOC) | P1 | Spec 합의 완료, 구현 미시작 |
| **W-B** | TS | async/await + heritage (extends/implements) | M (~700 LOC) | **P0** | Spec 합의 완료, 구현 미시작 |
| **W-C** | Sol | Inheritance + interface dispatch + using For | L (~1100-1200 LOC) | **P0** | W4 (abstract/library SubKind) ✅ 2026-05-11 land; W1/W2/W3/W6 미시작 |

### 1.1 참조 문서 경로

- 인덱스: `docs/NEXT-CANDIDATES-WITHIN-LANG-SEMANTICS.md`
- W-A spec: `docs/design/go-cross-function-lock-propagation.md`
- W-B spec: `docs/design/ts-async-await-and-interface.md`
- W-C spec: `docs/design/solidity-inheritance-and-interface-dispatch.md`
- 결정 합의 원본 패턴 참조: `docs/design/hunk-graph.md` §11 (8 결정)
- 진행 중 충돌 영역 확인용: `docs/design/schema-1.9-spec.md`

---

## §2. 권장 진행 순서 (6 phase)

### Phase 1 — 결정 합의 ✅ 완료 (2026-05-11)
모든 §5.0 박제 완료. 추가 합의 불필요.

### Phase 2 — W-D land (docs-only PR, 가장 작음)
- `pkg/types/enums.go` 의 `NodeMutex` (lines ~35) + lock edge types
  (`acquires_lock` / `releases_lock` / `accessed_under_lock`, lines ~138)
  주석이 "B1 Wave 5 will emit; the parser does not produce yet" 로 잘못
  표기되어 있던 부분이 이미 정정됨 (이전 세션에서 적용).
- 본 세션이 commit 만 처리하면 됨 — diff 확인 후 land.

### Phase 3 — Sol W4 warm-up (schema 변경 없음, 가장 빠른 첫 win)
- `abstract contract` → `NodeContract.SubKind="abstract"`
- `library` → `NodeContract.SubKind="library"`
- `~100 LOC` + 2 fixture
- detector 변경만, enums.go / SCHEMA.md 무변경
- 다른 세션 schema 1.9 작업과 마찰 0

**Status — 2026-05-11**: ✅ **DONE**. plain `contract` 도 명시적으로
`SubKind="contract"` 으로 라벨 (빈 문자열 → "contract" 로 승격, W1 의
interface SubKind 와 idiom 일치). 변경 라인 합 ~390 (코드 215 + 테스트
117 + fixture 38 + golden patch 2줄). Go regression diff = 0. 상세 변경
은 `docs/design/solidity-inheritance-and-interface-dispatch.md` §4.4 의
Status 블록 참조.

### Phase 4 — schema 1.10 bump (TS + Sol 합쳐 단일 PR)
- W-B + W-C 의 신규 NodeType / EdgeType 을 한 번에 `enums.go` 에 append.
- 새로 추가 (append-only, 절대 insert 금지):
  - `NodeAwaitPoint` → `AllNodeTypes()` 끝
  - `EdgeAwaits` → `AllEdgeTypes()` 끝
  - `EdgeOverrides` → `AllEdgeTypes()` 끝
- SubKind 값 확장 표 (코드 변경 없음, 문서만):
  - `NodeContract.SubKind`: `{"contract","interface","abstract","library"}`
  - `NodeFunction.SubKind`: `{"function","async","virtual","override",
    "virtual_override","fallback","receive"}`
- `docs/SCHEMA.md` 갱신: 노드 34 → 35, 엣지 35 → 37
- detector 변경 없음 (slot 만 예약). 회귀: `TestAllNodeTypes_Stable` +
  `TestAllEdgeTypes_Stable` + `pkg/types/...` 전체.

**⚠️ schema 1.9 작업과 충돌 가능성 1건**: 다른 세션도 enums.go 에 append
하고 있을 수 있음. **이 PR 진입 전 main 의 enums.go 최신 상태 반드시
확인**, append 위치를 schema 1.9 추가 항목 *뒤* 로 둠.

### Phase 5 — 본 구현 (병렬 가능)
| W | 의존성 | 병렬? |
|---|--------|------|
| W-A (Go lock 전파) | 없음 | ✅ 단독 진행 가능 |
| W-B W1 (TS heritage) | 없음 (schema bump 후) | ✅ W-A 와 병렬 |
| W-B W2 (TS async) | W-B W1 완료 권장 | 순차 |
| W-C W1 (Sol inheritance) | 없음 (schema bump 후) | ✅ |
| W-C W2 (Sol virtual/override) | W-C W1 완료 | 순차 |
| W-C W3 (Sol interface dispatch) | W-C W1 완료 | 순차 |
| W-C W6 (Sol using For) | W-C W1 완료 | 순차 또는 마지막 |

### Phase 6 — 측정 + 핸드오프
- 각 spec 의 `§4 측정` 단계 (self-graph / 실세계 corpus 빌드)
- KPI before/after 기록
- 새 `SESSION-HANDOFF-<date>.md` 의 §6 후보에 등재
- 다음 dimension (또는 schema 1.11) 로 인계

---

## §3. 어디서 시작? — 결정 트리

```
지금 main HEAD 가 schema 1.9 W2 (HTTP client detection) 이상인가?
├── NO → 다른 세션 진행 대기 (main 흡수 후 재시작)
└── YES → enums.go 의 NodeMutex 주석이 정정된 상태인가?
        ├── NO → W-D commit 부터 (Phase 2)
        └── YES → 어떤 사이즈로 시작?
                ├── 가장 작게 (~100 LOC) → Sol W4 (Phase 3)
                ├── 중간 (~300-400 LOC, 의존성 0) → W-A (Phase 5)
                └── 가장 큰 가치 → Phase 4 schema bump → W-C (Phase 5)
```

---

## §4. 코딩 규칙 (반드시 준수)

### 4.1 enums.go 변경

- **append-only**: 기존 `AllNodeTypes()` / `AllEdgeTypes()` 슬라이스의 어느
  위치에도 insert 금지. 끝에만 append. 이유: 기존 positional indices 가
  hash-derived ID 와 test snapshot 에 박혀 있음 (`TestAllNodeTypes_Stable`).
- **주석에 출처 spec 경로 명시**: 새 enum 옆 주석에
  "see docs/design/<spec>.md" 추가. 이미 schema 1.1, 1.3, 1.4, 1.6, 1.8 이
  같은 패턴 (enums.go 안에서 grep 으로 확인 가능).

### 4.2 새 detector 코드는 별도 파일

- 다른 세션 충돌 회피 + 본 spec 의 결정 격리 목적.
- 권장 위치:
  - Go lock 전파: `internal/parse/golang/lock_propagation.go` 또는
    `internal/buildpipe/lock_propagation.go` (score.Compute 직전 진입점)
  - TS heritage: `internal/parse/typescript/heritage.go` (declarations.go
    분기 추가 + 신규 파일)
  - TS async: `internal/parse/typescript/async.go`
  - Sol inheritance: `internal/parse/solidity/inheritance.go`
  - Sol dispatch: `internal/parse/solidity/dispatch.go`
  - Sol using For: `internal/parse/solidity/using_for.go`

### 4.3 test fixture 위치

| W | 위치 |
|---|------|
| W-A | `internal/parse/golang/testdata/lock_propagation/` (별도, 기존 concurrency/ 와 분리 — Q7 결정) |
| W-B heritage | `internal/parse/typescript/testdata/heritage/` |
| W-B async | `internal/parse/typescript/testdata/async/` |
| W-C inheritance | `internal/parse/solidity/testdata/inheritance/` |
| W-C dispatch | `internal/parse/solidity/testdata/dispatch/` |
| W-C using For | `internal/parse/solidity/testdata/using_for/` |
| W-C W4 (abstract/library) | `internal/parse/solidity/testdata/subkind/` 또는 기존 synthetic 확장 |

### 4.4 PendingRef 라우팅 (cross-file resolution 필요 시)

새 엣지가 cross-file 인 경우 (W-B heritage, W-C inheritance, W-C dispatch)
는 Pass 1 에서 `PendingRef` 로 두고 Pass 2 `Resolve` 에서 매핑.
참고 구현:
- `internal/parse/golang/resolve.go` — pending → edge resolution 패턴
- `internal/parse/golang/implements.go:EmitImplementsEdges` — typed
  post-pass 패턴
- `internal/parse/typescript/resolve.go` — TS 측 매핑 위치

### 4.5 confidence 라벨

각 spec §2 의 결정 그대로:
- W-A: cross-fn 전파 = INFERRED 통일 (다른 mutex 케이스도 INFERRED)
- W-A: goroutine body 전파 = INFERRED 강제 (별도 confidence)
- W-B: extends/implements = EXTRACTED (같은 파일/패키지) | INFERRED
  (cross-file)
- W-B: awaits = EXTRACTED (parser 직접 인식)
- W-C: extends/implements = EXTRACTED, overrides = EXTRACTED
- W-C: interface dispatch (`IFoo(addr).bar()`) = **AMBIGUOUS** (Q5 결정,
  사용자 강화) — `llmSafeStoreReader` wrapper 가 자동 차단

### 4.6 commit message

- prefix 자유 (`feat:` / `fix:` / `docs:` / `chore:` 표준)
- `Co-Authored-By` 또는 `Generated with [Claude Code]` 류 attribution
  **절대 금지** — 사용자 명시 룰 (`~/.claude/CLAUDE.md`)
- 본문에 *why* + before/after 측정값 권장
- 한국어/영어 자유

### 4.7 회귀 게이트

PR 진입 전:
```bash
go build ./...
go test ./... -count 1 2>&1 | grep -E '^(ok|FAIL)'
```
모두 통과 필요.

---

## §5. divergent 결정 강조 (반드시 인지)

권고와 다르게 결정된 항목 2건 — 구현자가 spec 권고만 보고 잘못 진행하기
쉬움.

### 5.1 W-A Q1: Stage B DFS 직행 (권고: Stage A 1-hop)

- 사용자 결정: **§3.2 Stage B Reachability-bounded DFS** (depth 3-5),
  **NOT** §3.1 Stage A 1-hop
- 구현 영향:
  - 사이즈: ~200 → 300-400 LOC
  - cycle 방지 visited set 필요
  - depth limit 매직넘버 (`maxDepth=5`)
- 추가 결정 연동:
  - Q3: calls + invokes 둘 다 traversal
  - Q4: goroutine body 진입 시 INFERRED 강제
  - Q5: `--lock-propagation` opt-in flag (default off)

### 5.2 W-C Q9: using For 본 spec 에 포함 (권고: 별도 spec)

- 사용자 결정: `using SafeMath for uint; a.add(b)` 패턴을 W-C 안에 처리
- 구현 영향:
  - W6 신설 (원 spec 의 §4 에 없는 단계)
  - 사이즈 +200~300 LOC
  - `resolve.go` 에 contract-scoped library 매핑 필요 (`using X for T` 가
    선언된 contract 안에서만 `T.method()` 가 X.method 로 dispatch)
  - 새 엣지 타입 도입 여부는 W6 설계 시 결정 — 가능하면 일반 `calls` 로
    resolve 권장 (스키마 영향 최소화)

---

## §6. 충돌 회피 체크리스트 (각 PR 진입 전)

```
□ git pull (다른 세션 commit 흡수, schema 1.9 진척 확인)
□ 본 PR 의 대상 spec 의 §5.0 표 출력 → self-check
□ enums.go 변경이 있다면:
    □ append 위치가 다른 세션의 schema 1.9 추가 *뒤*인지 확인
    □ TestAllNodeTypes_Stable / TestAllEdgeTypes_Stable 통과
    □ AllNodeTypes() / AllEdgeTypes() 슬라이스도 동시 갱신
□ 새 detector 가 다른 파일을 건드리지 않는지 (별도 파일 권장)
□ test fixture 가 기존 디렉토리 침범 안 했는지
□ docs/SCHEMA.md 의 노드/엣지 카운트 업데이트 (Phase 4 일 때)
□ confidence 라벨이 §5.0 결정과 일치하는지
□ commit message 에 attribution 마커 없음
□ go build ./... + go test ./... 패스
```

PR 본문 템플릿:
```markdown
## 작업
- spec: docs/design/<spec>.md §X.Y (W<N>)
- §5.0 결정 반영: Q<X>=<choice>, ...

## 변경
- 신규 파일: ...
- enums.go: append (있다면)
- testdata: 추가 fixture N건

## 회귀
- go test ./... ✅ (NN/NN ok)
- before/after KPI (있다면)

## 후속
- 다음 W stage: W<M> (또는 종료)
```

---

## §7. 빠른 시작 — 1 메시지로 dispatch

진행 중 세션에 다음만 송신해도 됨:

```
docs/DISPATCH-WITHIN-LANG-SEMANTICS.md 읽고 §3 결정 트리 따라 진입할 W
stage 정한 뒤 시작해줘. 26개 결정은 각 spec §5.0 에 박제됨, 재논의 불필요.
divergent 2건만 §5 강조 참조 (Go Q1 Stage B 직행, Sol Q9 using For 포함).
```

---

## §8. 참조 (외부 컨텍스트가 필요할 때)

- 직전 핸드오프: `docs/SESSION-HANDOFF-2026-05-10.md`
- 진행 중 schema 1.9: `docs/design/schema-1.9-spec.md`
- 진단 baseline: `docs/design/track-c-detector-gap.md` (W-B / W-C 의 일부
  항목은 여기서 P2 진단으로 시작됨)
- spec V0.2: `docs/spec-ckg-v0.2.md` (concurrency / interface 정의의 ground
  truth)
- prompt cache 영향: `~/.claude/rules/prompt-cache.md` (enums.go 같은 hot
  path 변경 시점 결정에 참고)
