# TypeScript Async/Await + Interface Graph — Design Spec

> Scope: extend the TS parser (`internal/parse/typescript/`) so the graph
> captures (a) async/await semantics — Promise creation, await suspension
> points, async function chains — and (b) interface / class heritage —
> `implements`, `extends`, `declaration merging` — neither of which is
> currently emitted.
>
> **Status**: design draft 2026-05-11. Schema 1.10 slot reserved
> 2026-05-11 (NodeAwaitPoint, EdgeAwaits appended to `pkg/types/enums.go`;
> see `docs/DISPATCH-WITHIN-LANG-SEMANTICS.md` §2 Phase 4 Status block).
> **W1 heritage** ✅ **LANDED 2026-05-11** — `internal/parse/typescript/heritage.go`
> + 5 fixtures + 4 test functions covering same-file / cross-file / unresolved
> drop / edge-direction.
> **W2 async/await** ✅ **LANDED 2026-05-11** — `internal/parse/typescript/async.go`
> + 5 fixtures + 3 test functions. Function/Method SubKind="async", NodeAwaitPoint
> + EdgeAwaits emit at every `await_expression` inside a Function/Method interval
> (top-level await dropped per V0 scope). Schema 1.10 slots NodeAwaitPoint / EdgeAwaits
> now produce data.
> **W3 viewer style** ✅ **LANDED 2026-05-11** — `web/viewer-next/src/lib/edges.ts`
> 에 `awaits` / `overrides` 등록 + GRAPH_GROUPS / DEFAULT_EDGE_TYPES 갱신.
> **W4 measurement** ✅ **LANDED 2026-05-12** — self-graph (TS 82 files)
> 측정: 245 AwaitPoint, 245 EdgeAwaits, 48 async Function, 118 async Method,
> 6 EdgeExtends, 7 EdgeImplements. pair invariant 검증 OK. 상세 §4.4 LANDED 블록.
> **Out of scope**: cross-language async (Go ↔ TS HTTP — that's schema 1.9
> W series), JSX render graph, React-specific hooks dependency graph,
> TypeScript decorators (already partially captured via `queryDecorator`).
> **Adjacent docs**: `docs/design/track-c-detector-gap.md` §2.4–§2.5
> (TS `extends` / `implements` already flagged P2), `docs/design/schema-1.9-spec.md`
> (cross-language interop — disjoint dimension).

---

## §0. Cold start

- **무엇**: TS 파서가 (1) async/await 흐름과 (2) interface heritage 를
  graph 로 표현하지 못한다. 현재 6,846 TS 노드 중 async 관련 엣지 0,
  `extends`/`implements` 엣지 0 (track-c-detector-gap.md §2.4-§2.5 측정).
- **왜**: TS 코드의 의미적 단위 절반 이상이 async 흐름 또는 interface 기반
  dispatch — graph 가 이것을 빠뜨리면 "어디서 await 하면 어디까지 막히는가",
  "이 interface 의 구현체가 누구인가" 같은 가장 자주 묻는 질문에 답 불가.
- **어떻게**: 두 갈래 독립 작업.
  - (A) 새 노드/엣지 1종: `AwaitPoint` (statement-level), `awaits` 엣지
    (Function → AwaitPoint → AsyncCallSite). Promise 생성·resolve 패턴은
    부수적으로 캡처.
  - (B) tree-sitter query 확장: `class_heritage`, `implements_clause`,
    `interface_extends_clause` 추가. 기존 `EdgeExtends` / `EdgeImplements`
    재사용 (schema 변경 없음).
- **선행**: 없음. schema 1.8 위에 append-only. (B) 는 schema bump 없음;
  (A) 는 새 NodeType `AwaitPoint` 추가 → schema 1.9 또는 별도 마이너 bump
  결정 필요 (§5.Q1).

---

## §1. 현재 상태

### 1.1 TS 파서가 *capture 하는* tree-sitter 노드

`internal/parse/typescript/queries.go` 전체:

| Query | 매칭 |
|-------|------|
| `class_declaration` | Class 노드 |
| `interface_declaration` | Interface 노드 |
| `function_declaration` | Function 노드 |
| `method_definition` | Method 노드 |
| `import_statement` | Import 노드 |
| `decorator (call_expression)` | Decorator 노드 |
| `type_alias_declaration` | TypeAlias 노드 |
| `enum_declaration` | Enum 노드 |
| `call_expression` (P3) | calls pending refs |
| (statement) IfStmt / LoopStmt / SwitchStmt / ReturnStmt / CallSite | `internal/parse/typescript/statements.go` |

### 1.2 *capture 안 하는* 것

| Tree-sitter node | 의미 | 현재 |
|------------------|------|------|
| `class_heritage` → `extends_clause` | `class A extends B` | ❌ 무시 |
| `class_heritage` → `implements_clause` | `class A implements I, J` | ❌ 무시 |
| `extends_clause` (interface) | `interface I extends J, K` | ❌ 무시 |
| `await_expression` | `await foo()` | ❌ 무시 |
| `async` keyword modifier | `async function foo()` | ❌ 무시 (signature 에도 누락) |
| `function_expression` w/ `async` | `async () => ...` | ❌ |
| `yield_expression` (generator) | `yield foo()` | ❌ (별개 spec 후보) |
| `new_expression` | `new Foo()` | ❌ (Go `instantiates` 와 평행, track-c §2.2 P1) |

### 1.3 영향

- "어느 클래스가 `IUserService` 를 구현하는가?" — graph 로 답 못함
  (`grep "implements IUserService"` 로 fallback).
- "이 async function 의 await chain 이 얼마나 깊은가?" — node 단위 보이지
  않음.
- React hooks 처럼 callback heavy 한 코드의 control flow 가 평면화됨.

### 1.4 track-c-detector-gap.md 와의 관계

| 항목 | track-c (P2) | 본 spec |
|------|--------------|---------|
| TS `extends` | 진단만 ("class_heritage missing") | 구현 plan |
| TS `implements` | 진단만 | 구현 plan |
| TS async/await | 언급 없음 | **신규** |
| `instantiates` | Go-only P1 | 본 spec 의 §3.4 에서 TS 도 함께 다룸 (선택) |

본 spec 은 track-c 의 P2 항목을 실행 가능한 plan 으로 격상 + async/await
신규 plan 을 추가.

---

## §2. 목표 동작

### 2.1 새 노드/엣지

| 타입 | 종류 | 출처 | 신뢰도 |
|------|------|------|--------|
| `NodeAwaitPoint` (신규) | Node | `await_expression` 위치 | EXTRACTED |
| `EdgeExtends` (기존) | Edge | `class A extends B`, `interface I extends J` | EXTRACTED (이름 매칭) / INFERRED (cross-file) |
| `EdgeImplements` (기존) | Edge | `class A implements I` | EXTRACTED / INFERRED |
| `EdgeAwaits` (신규) | Edge | Function/Method → AwaitPoint | EXTRACTED |
| `EdgeAsyncCall` (신규 옵션) | Edge | AwaitPoint → CallSite | EXTRACTED |

신규 노드 1종 + 신규 엣지 2종 + 기존 엣지 2종 활성화. `pkg/types/enums.go`
의 `NodeType` / `EdgeType` 슬라이스에 append-only.

### 2.2 신뢰도 정책

- `extends` / `implements`: 이름이 같은 파일/패키지 안에서 즉시 resolve 되면
  EXTRACTED. cross-file → PendingRef → INFERRED. 미해결 → drop (resolve.go
  의 기존 drop policy 와 동일).
- `awaits`: `await` 키워드 위치는 항상 EXTRACTED (parser 가 직접 봄). 단
  `await` 의 대상 함수가 실제로 async 인지 결정은 `signature` 필드의 "async"
  prefix 또는 reverse-lookup 필요.
- Declaration merging (TS 의 같은 이름 interface 가 여러 파일에서 확장되는
  case): 모든 선언을 별개 노드로 두고 `extends` 로 묶기 — V0 단순화.

### 2.3 schema 영향

- `NodeAwaitPoint` 추가 → `pkg/types/enums.go` 의 `AllNodeTypes()` 끝에
  append. positional indices 보존 (test `TestAllNodeTypes_Stable` 통과
  유지).
- `EdgeAwaits`, `EdgeAsyncCall` 추가 → 동일.
- `track-c-detector-gap.md` 의 `extends` / `implements` 는 enum 에 이미
  존재 — 새 type 추가 없음.

bump: schema 1.8 → 1.10 (1.9 는 cross-language interop 으로 예약).

**Status — 2026-05-11**: ✅ schema 1.10 slot **reserved**. `NodeAwaitPoint`
appended at `AllNodeTypes()` index 34, `EdgeAwaits` at `AllEdgeTypes()`
index 38. `EdgeAsyncCall` 은 §5 Q3 결정 따라 잠재적으로 추가 가능 — 본
Phase 4 bump 에서는 미포함 (필요 시 별도 후속 PR). Detector emission 은
Phase 5 (W2) 진입 시까지 0 — `internal/parse/typescript/*` 변경 없음.

---

## §3. 검출 알고리즘

### 3.1 (B) heritage — tree-sitter query 확장

`internal/parse/typescript/queries.go` 에 추가:

```scheme
; class C extends B
(class_declaration
  name: (type_identifier) @class_name
  (class_heritage
    (extends_clause value: (_) @extends_target))) @decl

; class C implements I, J
(class_declaration
  name: (type_identifier) @class_name
  (class_heritage
    (implements_clause (type_identifier) @impl_target))) @decl

; interface I extends J, K
(interface_declaration
  name: (type_identifier) @iface_name
  (extends_type_clause (type_identifier) @extends_target)) @decl
```

(Tree-sitter-typescript 의 정확한 node name 은 `tree-sitter playground`
또는 `node-types.json` 으로 확정 — `interface_extends_clause` vs
`extends_type_clause` 등 grammar 버전 dependency 가 있음.)

`declarations.go` 에 새 visitor 추가:
- class extends 발견 → `PendingRef{SrcID: class_id, EdgeType:
  EdgeExtends, TargetQName: extends_target}`
- class implements 발견 → `PendingRef{... EdgeImplements ...}`
- interface extends 발견 → `EdgeExtends` (TS interface 는 multiple
  extends 가능 — 각각 separate edge)

Pass 2 Resolve 는 기존 `resolve.go` 의 qname → node ID 매핑 로직 재사용.

### 3.2 (A) async/await — body walk 확장

`statements.go` 의 body walker 에 추가:

```
on tree-sitter node `await_expression`:
  startLn, startBy, endLn, endBy = position
  awaitID = MakeID(parent_qname + ".await@" + startBy, "ts", startBy)
  emit Node{ID: awaitID, Type: NodeAwaitPoint, Name: "await", ...}
  emit Edge{Src: enclosing_function_id, Dst: awaitID, Type: EdgeAwaits, ...}

  // 옵션: await 의 대상이 CallExpression 이면 추가 엣지
  if await_expression.argument is call_expression:
    callee = await_expression.argument
    callSiteID = ... (이미 CallSite 노드 emit 했다면 그 ID 재사용)
    emit Edge{Src: awaitID, Dst: callSiteID, Type: EdgeAsyncCall, ...}
```

`async` modifier 는 Function/Method 노드의 `Signature` 필드 prefix 로 추가
(`"async function foo(...)"`) — 새 컬럼 없이 signature 안에 sub-information
유지. (Go 의 `complexity` 처럼.)

### 3.3 noise control

- 같은 함수 안에서 `await` 가 여러 번 나오면 각각 별개 AwaitPoint 노드
  emit (line-distinguishable). dedup 하지 않음 — 위치별 추적이 의미.
- Arrow function 안의 await 는 enclosing function 으로 anchor 결정 필요
  — track-c §7 의 "TS body walk P3 arrow-function nested edge case" 와 같은
  알려진 한계 영역.

### 3.4 (선택) `new_expression` → `instantiates` 엣지

track-c §2.2 의 Go `instantiates` 와 평행하게 TS 도 같은 엣지 emit.
`new Foo()` → `Function instantiates Class`.

별도 PR 으로 분리 권장 (heritage / async 와 결합도 낮음).

---

## §4. 구현 계획

### 4.1 W1 — heritage (B)

1. `queries.go` 에 3 query 추가 (위 §3.1)
2. `declarations.go` 에 visitor 분기 추가
3. PendingRef 라우팅 — `EdgeExtends` / `EdgeImplements`
4. 단위 테스트: `declarations_test.go` 또는 신규 `heritage_test.go`
   - fixture: class extends class / class implements interface / interface
     extends interface / multiple implements
5. Pass 2 cross-file resolution — 기존 패스 그대로 동작 확인

추정 사이즈: 200~300 LOC + 4 fixture. 의존성 없음.

#### LANDED 2026-05-11 (W-B W1)

- 구현: `internal/parse/typescript/heritage.go` (284 LOC). Hand-rolled
  walker over `class_declaration` + `interface_declaration` subtrees
  (tree-sitter query 우회 — 사유: pair-of-clauses 구조 분해는 query
  capture로 표현 어색).
- Wire: `declarations.go::visit()` 에 `v.runHeritage()` 호출 추가
  (queryClass / queryInterface 직후).
- Resolver: `resolve.go::resolveHeritageRef()` — same-file=ConfExtracted,
  cross-file=ConfInferred, 미해결=drop. Class/Interface 전용 인덱스
  `heritageByName` 로 동명 Function/Method 오염 방지.
- DispatchKind 태그: `"heritage"` (golang/grpc.go `"grpc"`, solidity/
  inheritance.go `"inherit"` 와 동일 idiom).
- 지원 shape:
  - `class Derived extends Base implements IBase {}` → 1 EdgeExtends + 1 EdgeImplements
  - `class Multi extends Base implements IFoo, IBar, IBaz {}` → 1 EdgeExtends + 3 EdgeImplements
  - `interface IChild extends IBase {}` → 1 EdgeExtends
  - `interface IUnion extends IFoo, IBar {}` → 2 EdgeExtends
  - 트레일링 식별자 추출: bare identifier / `Ns.Foo` / `Foo<T>` 모두 처리
- Fixtures: `testdata/heritage/{simple_extends,class_implements,
  interface_extends,multiple_implements,cross_file_base,cross_file_child}.ts`.
- 테스트: `heritage_test.go` 4 함수:
  - `TestTSHeritage_FixtureMatrix` — 4 same-file fixture × (extends/implements) child→parent map 검증
  - `TestTSHeritage_CrossFile` — base/child 분리 → ConfInferred 검증
  - `TestTSHeritage_UnresolvedDropped` — child만 ParseFile하면 dangling 0
  - `TestTSHeritage_EdgeDirection` — 모든 부모가 Src로 등장하지 않음 (방향 invariant)
- 회귀: 25/25 PASS, vet clean. Go 빌드 영향 0 (TS 전용 변경).

### 4.2 W2 — async/await (A)

1. `statements.go` 에 `await_expression` visitor 추가
2. `pkg/types/enums.go` 에 `NodeAwaitPoint` / `EdgeAwaits` / `EdgeAsyncCall`
   append
3. `Signature` 필드에 async prefix 합성 (declarations.go function visitor)
4. 단위 테스트: `async_test.go`
5. `internal/graph/validate.go` 가 새 NodeType / EdgeType 자동 통과 확인
   (`AllNodeTypes()` / `AllEdgeTypes()` 갱신 후)

추정 사이즈: 300~400 LOC + 5 fixture. enums.go 변경으로 prompt cache 영향
— `prompt-cache.md` 의 append-only 원칙 준수 (insert 금지).

#### LANDED 2026-05-11 (W-B W2)

- 구현: `internal/parse/typescript/async.go` (~180 LOC). Hand-rolled
  walker over the parse tree — visits every `await_expression` and
  anchors it on the smallest enclosing Function/Method interval via
  `findEnclosingFn` (reused from body_walk.go).
- Wire: `declarations.go::visit()` 에 `v.runAsync()` 호출 추가
  (`runBodyStatements()` 직후).
- async modifier 감지: `declarations.go::runQuery` 내부에 분기 추가 —
  NodeFunction / NodeMethod 의 name capture 의 parent chain 을 거슬러
  function_declaration / method_definition / function_expression /
  arrow_function 의 직계 children 에 `async` 키워드가 있는지 검사.
  결과를 `SubKind="async"` 로 emit (default `SubKind=""`).
- §5.0 결정 사항 구현:
  - Q1: NodeAwaitPoint 신규 NodeType ✅ (enums.go slot 34 이미 reserved)
  - Q2: Function/Method SubKind="async" ✅
  - Q3: EdgeAsyncCall skip — EdgeAwaits 한 방향만 ✅
  - Q5: 선언 합치기 V0 — 각 await 별개 AwaitPoint ✅
- AwaitPoint 모양:
  - QualifiedName: `<parentQname>#AwaitPoint@<startByte>` (statements.go
    convention 과 동일)
  - Name: `"await <callee>"` 추출 가능 시, 아니면 `"await"`. callee
    shapes: bare identifier / `obj.foo()` / `foo()` / `member.prop`
  - Confidence: 항상 EXTRACTED (위치 직접 관찰)
- V0 한계 (track-c §7 carry-over):
  - 모듈 최상위 `await` → 인접 enclosing function 없음 → drop
  - 화살표 함수 body 내 await → 외곽 named function 에 anchor (intervals
    walker 가 arrow function 을 별도 interval 로 분리하지 않음)
  - `for await ... of` 는 LoopStmt 만 emit, 암묵적 per-iteration await
    별도 AwaitPoint 생성 안 함
- Fixtures: `testdata/async/{async_function, async_method, multi_awaits,
  non_async, await_in_branch}.ts` (5 files).
- 테스트: `async_test.go` 3 함수:
  - `TestTSAsync_FixtureMatrix` — 5 fixture × (await-by-parent, async
    SubKind set, pair invariant) 검증
  - `TestTSAsync_AwaitPointSchemaInvariants` — line/byte 범위, language,
    confidence, qname 마커, name prefix 검증
  - `TestTSAsync_TopLevelAwaitDropped` — 모듈 최상위 await drop 검증
- 회귀: 25/25 PASS, vet clean. Go 빌드 영향 0 (TS 전용 변경).

### 4.3 W3 — viewer + edge style

`web/viewer-next/src/lib/edges.ts` 에 새 엣지 색상/그룹 등록.
- `awaits`: G3 (control-flow) 그룹, dash 패턴
- `async_call`: G3, solid
- `implements` / `extends`: 이미 G2 등록됨 (track-c §2.4 참조)

#### LANDED 2026-05-11 (W-B W3)

- `web/viewer-next/src/lib/edges.ts` 에 5종 신규 edge style 등록 (W-B
  뿐 아니라 schema 1.9 W series + W-C 도 함께 — viewer 통합 일괄 처리).
- W-B 관련 추가분:
  - `awaits` (G3 Execution, light blue dashed) — 등록 + default ON
  - `overrides` (G2 Semantic, deep blue solid) — W-C W2 와 공유, 등록 + default ON
- `GRAPH_GROUPS` 갱신: G2 +overrides, G3 +awaits.
- `DEFAULT_EDGE_TYPES` 갱신: 두 edge 모두 boot view ON (dash 라 시각적
  비용 낮고, 가시화 가치 ↑).
- Self-check 주석: 34→39 non-hidden edges (G1=3 / G2=13 / G3=5 / G4=6 /
  G5=7 / G6=5).
- Typecheck PASS, self-check 일관성 (EDGE_STYLE keys ⇄ GRAPH_GROUPS
  합집합 일치).
- Commit `7af9ce4`.

### 4.4 W4 — measurement + handoff

self-graph 빌드 후 카운트 변화 측정:
- extends/implements: 0 → ?
- AwaitPoint: 0 → ? (web/viewer-next 가 React 라 hooks 안 async 적음 — 적을
  수 있음)

go-stablenet 은 TS 가 없으므로 별도 fixture (e.g. TanStack Query, Next.js
샘플) 빌드 권장.

#### LANDED 2026-05-12 (W-B W4)

Self-graph TS subset (CKG 본 레포의 `web/viewer-next/**` + `internal/parse/
typescript/testdata/**` 등 82 TS files) 기준 측정:

| 항목 | Before (Phase 4 land 직후) | After (W-B W1/W2 land 후) | 변화 |
|------|---------------------------|----------------------------|------|
| TS Class | 14 | 14 | — |
| TS Interface | 55 | 55 | — |
| TS Function | 2,214 | 2,214 | — |
| TS Method | 4,565 | 4,565 | — |
| async Function (SubKind="async") | 0 | **48** | +48 (2.2% of Function) |
| async Method (SubKind="async") | 0 | **118** | +118 (2.6% of Method) |
| **NodeAwaitPoint** | 0 | **245** | +245 |
| **EdgeAwaits** | 0 | **245** | +245 (pair invariant ✅) |
| **EdgeExtends (TS)** | 0 | **6** | +6 |
| **EdgeImplements (TS)** | 0 | **7** | +7 |

밀도 보조 지표:
- AwaitPoint / async callable = 245 / (48+118) = **1.48 awaits per async
  callable** — 단일 async function 이 평균 1.5 회 suspension 함을 의미.
- Heritage edge / Class+Interface = 13 / 69 = **18.8%** — 클래스/인터페이스
  중 약 1/5 이 inheritance chain 에 참여 (React component 가 추상화 평면화
  되어 있는 영향으로 추정).

W series (schema 1.9, 본 측정에 함께 잡힘):
- listens_on (TS HTTP 서버): 22
- http_calls (TS HTTP 클라이언트, W2): 9
- grpc_calls (TS gRPC-web client, W3c): 7

go-stablenet TS fixture 별도 빌드 (TanStack Query / Next.js 샘플)는 보류 —
self-graph 측정으로 schema 1.10 slot 활성화 + pair invariant 검증 만족.
향후 KPI 변화 추적용으로 별도 fixture 빌드는 W5+ 후속에서 진행.

빌드 명령 (재현):
```bash
go run ./cmd/ckg build --src . --out /tmp/ckg-wb-w4 --no-cache --lang ts
sqlite3 /tmp/ckg-wb-w4/graph.db \
  "SELECT type, COUNT(*) FROM nodes WHERE language='ts' GROUP BY type ORDER BY 2 DESC"
```

---

## §5. 결정 필요 항목

> **STATUS — 2026-05-11**: 8개 항목 모두 합의 완료. 결정 요약은 §5.0
> 참조. 각 Q 의 옵션·trade-off 원본은 §5.Q1 이하 read-only 보존.

### §5.0. 결정 결과 (2026-05-11)

| Q | 결정 | 권고 일치? | 비고 |
|---|------|-----------|------|
| Q1 | 신규 NodeType `AwaitPoint` | ✅ | schema 1.10 bump 의 일부 |
| Q2 | Function/Method SubKind="async" | ✅ | Mutex SubKind 와 동일 idiom |
| Q3 | `EdgeAsyncCall` skip — `awaits` 만 | ✅ | AwaitPoint 의 line/byte 위치로 callee 조인 |
| Q4 | multiple extends 각각 별개 엣지 | ✅ | Go interface embedding 과 동일 |
| Q5 | Declaration merging V0: 첫 선언만 노드, 나머지 drop | ✅ | V0 단순화. 향후 `same_as` 엣지 도입 검토 가능 |
| Q6 | JSX `<Foo />` V0 무시 — 별도 spec | ✅ | `ts-jsx-component-graph.md` 후속 |
| Q7 | Generator (`function*` / `yield`) 별도 spec | ✅ | 본 spec 범위 축소 |
| Q8 | `Promise.then` 체인 V0 무시 | ✅ | modern TS 는 await 우세, then 은 일반 calls 로 처리됨 |

**구현 영향 요약**:
- 신규 NodeType 1종 (`AwaitPoint`) → `pkg/types/enums.go AllNodeTypes()` append
- 신규 EdgeType 1종 (`awaits`) → `AllEdgeTypes()` append
- 기존 EdgeType 활성화: `extends`, `implements` (TS 쪽)
- 신규 SubKind 값: Function = {"function","async"} (기존 패턴 확장)
- W 단계: W1 heritage, W2 async/await, W3 viewer style, W4 measurement
- schema 1.10 bump 의 절반 (나머지 절반은 Sol spec)

원본 옵션 비교는 §5.Q1 이하 블록 참조.

---

### Q1. AwaitPoint 를 새 NodeType 으로 둘 것인가?

대안:
- (a) **신규 NodeType `AwaitPoint`** — schema 1.10 bump
- (b) 기존 `NodeCallSite` 의 sub_kind 로 표현 — schema 변경 없음, 그러나
  await 인지 일반 call 인지 클라이언트가 sub_kind 봐야 구분
- (c) 엣지 type 만 새로 (`awaits`) — Source/Dst 둘 다 기존 노드, 정보 손실

**권고**: (a). 위치별 surface 분리가 viewer/MCP 에서 가치 ↑. Hunk 노드도
같은 방식으로 schema 1.8 에 추가됨 (선례).

### Q2. async modifier 의 표현

- (a) Function 노드의 `Signature` 필드에 prefix
- (b) Function 노드의 `SubKind` 필드 ("async" / "")
- (c) 새 컬럼 `is_async bool` — 마이그레이션 필요

**권고**: (b). SubKind 는 Mutex 의 "mutex"/"rwmutex" 와 동일 패턴 — 이미
확립된 idiom.

### Q3. `EdgeAsyncCall` 가 정말 필요한가?

`Function awaits AwaitPoint` 만으로도 충분할 수 있음. AwaitPoint 의 위치
(line/byte) 로 caller→callee 도 추정 가능.

- (a) **추가** — explicit edge, traversal 1 hop 단축
- (b) skip — `awaits` 만으로 시작, 필요 시 follow-up

**권고**: (b). 엣지 타입 추가는 cost; 가치 입증 후 별도 PR.

### Q4. TS `extends` 의 multiple inheritance (interface)

TS interface 는 multiple extends 가능 (`interface I extends A, B, C`). 각각
별개 엣지 emit?

- (a) 각각 별개 엣지 — Go `extends` 와 동일 idiom (interface embedding 도
  multiple)
- (b) 첫 번째만 emit — 정보 손실
- (c) 단일 엣지에 다중 target — 데이터 모델 위배

**권고**: (a). 자명.

### Q5. Declaration merging 처리

TS 는 같은 이름 interface 가 여러 파일에서 선언 가능 (merge). 노드 1개로
합칠 것인가 N개로 둘 것인가?

- (a) N개 별개 노드, qname 동일 — node ID 충돌 → MakeID 가 byte offset 포함
  하므로 자동 해결, but resolve.go 가 qname → ID 매핑할 때 어느 ID 를
  쓸지 결정 필요
- (b) 첫 선언만 노드로 — 나중 선언은 drop
- (c) 모든 선언을 별개 노드 + 별도 edge type `declaration_merge_with`

**권고**: (a) + qname → ID 매핑 시 "첫 등장" 우선, 나머지는 `same_as` edge
로 보조 연결 — 단 `same_as` 는 새 edge type, 도입 신중. V0 는 (b) 로 단순화
권장. drop 된 선언은 INFERRED 라벨로 별도 PR 에서 처리.

### Q6. JSX 처리

`<Foo />` 는 본질적으로 `React.createElement(Foo)` — `instantiates`?
`calls`? `references`?

- (a) JSX 자체는 무시 (V0) — JSX 는 React 종속
- (b) `references` 엣지로 약하게 — Component 사이 의존 가시화
- (c) `instantiates` — Component = Class 처럼 취급

**권고**: (a). JSX 는 별도 spec (`ts-jsx-component-graph.md`) 권장.

### Q7. Generator (`yield`) 처리

`function*` / `yield expression` 는 async 와 형제 — 동일 spec 에 포함?

- (a) **별도 spec** — generator 는 사용 빈도 낮고 의미가 다름
- (b) 동일 spec — `NodeYieldPoint` 신규
- (c) AwaitPoint 와 같은 노드로 — sub_kind 로 구분

**권고**: (a). 본 spec 의 범위 축소.

### Q8. callbacks vs await — Promise.then 체인

`foo().then(x => ...)` 패턴은 어떻게?

- (a) 무시 — async/await 만 우선
- (b) `then` 호출을 별도 엣지 (`promise_chain`) 로 — schema bump
- (c) 일반 `calls` 로 처리 — 정보 손실

**권고**: (a). 본 spec 의 범위 축소. 별도 follow-up.

---

## §6. 테스트 전략

### 6.1 fixture (heritage)

```typescript
// testdata/heritage/single.ts
interface Service { do(): void }
abstract class Base { protected name: string = ""; }
class UserService extends Base implements Service {
  do() {}
}
```

기대 엣지:
- `UserService extends Base` (EXTRACTED)
- `UserService implements Service` (EXTRACTED)

### 6.2 fixture (async)

```typescript
// testdata/async/chain.ts
async function fetchUser(id: string) {
  const res = await api.get(`/u/${id}`);   // AwaitPoint #1
  const json = await res.json();           // AwaitPoint #2
  return json;
}
```

기대 노드/엣지:
- 2 AwaitPoint 노드 (다른 line)
- `fetchUser awaits AwaitPoint#1`, `fetchUser awaits AwaitPoint#2`
- Function 노드의 SubKind = "async"

### 6.3 회귀

기존 statement_test.go / declarations_test.go 의 노드 카운트 변동 없음 확인
(append-only 보장).

### 6.4 self-graph (web/viewer-next)

```bash
./bin/ckg build --src=web/viewer-next --out=/tmp/ckg-ts-self
sqlite3 /tmp/ckg-ts-self/graph.db "
  SELECT type, COUNT(*) FROM edges
  WHERE type IN ('extends','implements','awaits','async_call')
  GROUP BY type;
"
```

---

## §7. 참조

- 현재 TS 파서:
  - `internal/parse/typescript/parser.go` (entry)
  - `internal/parse/typescript/declarations.go` (visitor)
  - `internal/parse/typescript/queries.go` (tree-sitter query 정의)
  - `internal/parse/typescript/statements.go` (body walker)
- track-c 갭 진단: `docs/design/track-c-detector-gap.md` §2.4–§2.5
- TS grammar 노드 참조: `tree-sitter-typescript` upstream `node-types.json`
  (vendored 안에 없음 — 외부 grammar repo)
- Edge style: `web/viewer-next/src/lib/edges.ts`
- Go `extends`/`implements` 참고 구현: `internal/parse/golang/implements.go`
  (`EmitImplementsEdges`)

---

## §8. 작업 순서

1. **§5 결정 항목 8개에 사용자 답변 받기** (Q1 NodeType 결정이 schema
   bump 여부 좌우)
2. W1 — heritage 구현 (단순, 의존성 없음)
3. W2 — async/await 구현 (`enums.go` 수정 동반)
4. W3 — viewer style 등록 + 측정
5. W4 — handoff

W1 과 W2 는 의존 없음 → 병렬 PR 가능. W3 는 W1+W2 끝나야 의미 있음.
