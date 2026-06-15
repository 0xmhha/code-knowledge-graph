> **ARCHIVED 2026-06-15.** The W-A/W-B/W-C implementation it dispatched is
> complete (landed by 2026-05-18). Historical execution record; design intent
> lives in `docs/design/*.md`. Kept for provenance.

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
| **W-A** | Go | Cross-function lock propagation (D1) | M (~300-400 LOC) | P1 | ✅ **LANDED 2026-05-11** (Stage B DFS depth=5, opt-in `--lock-propagation`) |
| **W-B** | TS | async/await + heritage (extends/implements) | M (~700 LOC) | **P0** | Spec 합의 완료, 구현 미시작 |
| **W-C** | Sol | Inheritance + interface dispatch + using For | L (~1100-1200 LOC) | **P0** | W4 (abstract/library SubKind) ✅ 2026-05-11 land; W1 (inheritance / is-clause) ✅ 2026-05-11 land; W2 (virtual/override) ✅ 2026-05-11 land; W3 (interface dispatch AMBIGUOUS) ✅ 2026-05-11 land; W6 미시작 |

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
- `docs/SCHEMA.md` 갱신: 노드 34 → 35, 엣지 38 → 40 (schema 1.9 W2/W3b
  까지 흡수한 카운트 기준).
- detector 변경 없음 (slot 만 예약). 회귀: `TestAllNodeTypes_Stable` +
  `TestAllEdgeTypes_Stable` + `pkg/types/...` 전체.

**Status — 2026-05-11**: ✅ **LANDED**. Schema `1.9` → `1.10` bump in
`internal/buildpipe/cache.go`. enum slots:
- `NodeAwaitPoint` → AllNodeTypes() index 34 (W-B)
- `EdgeAwaits` → AllEdgeTypes() index 38 (W-B)
- `EdgeOverrides` → AllEdgeTypes() index 39 (W-C)

8 files touched (enums.go, types_test.go, cache.go, sqlite.go, SCHEMA.md,
DISPATCH-WITHIN-LANG-SEMANTICS.md, ts-async-await-and-interface.md,
solidity-inheritance-and-interface-dispatch.md). Detector code 0. Go
regression `--no-cache` diff vs schema 1.9: edges + nodes counts identical
(new slots are unused — no rows emitted).

**⚠️ schema 1.9 작업과 충돌 가능성 1건**: 다른 세션도 enums.go 에 append
하고 있을 수 있음. **이 PR 진입 전 main 의 enums.go 최신 상태 반드시
확인**, append 위치를 schema 1.9 추가 항목 *뒤* 로 둠.

### Phase 5 — 본 구현 (병렬 가능)
| W | 의존성 | 병렬? |
|---|--------|------|
| W-A (Go lock 전파) | 없음 | ✅ **LANDED 2026-05-11** |
| W-B W1 (TS heritage) | 없음 (schema bump 후) | ✅ **LANDED 2026-05-11** |
| W-B W2 (TS async) | W-B W1 완료 권장 | ✅ **LANDED 2026-05-11** |
| W-B W3 (viewer style) | W-B W2 완료 권장 | ✅ **LANDED 2026-05-11** |
| W-B W4 (measurement) | W-B W1+W2 완료 | ✅ **LANDED 2026-05-12** |
| W-C W1 (Sol inheritance) | 없음 (schema bump 후) | ✅ **LANDED 2026-05-11** |
| W-C W2 (Sol virtual/override) | W-C W1 완료 | ✅ **LANDED 2026-05-11** |
| W-C W3 (Sol interface dispatch) | W-C W1 완료 | ✅ **LANDED 2026-05-11** |
| W-C carry-over (V1 batch) | W-C W11 완료 | ✅ **LANDED 2026-05-18** (W6 V2.19 free-function 4-entry partial-recovery lock; W8 V1 HasLowLevelCall + HasValueTransfer markers complementing W7.1 edge emission; W9 V1 inheritance offset on SlotIndex via DFS over parents adjacency; W10 V1.1 YulBuiltins []string enumerating security-critical EVM opcodes inside assembly blocks. Five items deferred with documented rationale: W6 V2.x operator-form recovery (grammar-blocked, fragile shape per V2.17), W8 V2 function-pointer dispatch (function-type tracking infrastructure absent), W9 V2 bit-packing (Sol §11.1 layout complex; primitive-only version wrong for arrays/structs), W9 V3 mapping slot derivation (keccak runtime computation), W10 V2 Yul receiver resolution (fragile yul_path → Sol identifier mapping), W11 V1 real parser→persist→BuildPack integration (substantial fixture cost)). |
| W-C W11 (H3 evidence regression safety) | W-C W10 완료 | ✅ **LANDED 2026-05-18** (V0 H3 BuildPack regression test `TestBuildPack_SolGraphRegression` in pkg/evidence/sol_integration_test.go. Sol-shaped fakeStore staging all new W-C fields (Node.SubKind, SlotIndex, HasAssembly; Edge.DispatchKind, Order) plus mixed EXTRACTED/AMBIGUOUS commits and hunks. Locks four invariants: (1) BuildPack survives the new field shapes without panic, (2) §11.3 AMBIGUOUS-leak boundary holds, (3) Sol commit subjects flow into Pack so H4 issue-ID extraction can find them, (4) timestamp DESC ordering survives. Catches the most likely upstream regression — silent serialization or assembly failure — without standing up a real graph.db fixture (deferred to V1+). H4 ExtractIssueIDs corpus precision/recall test already exists in `internal/temporal/issueid_test.go`, so V0 focuses on the closer gap). |
| W-C W10 (Sol inline assembly marker) | W-C W9 완료 | ✅ **LANDED 2026-05-18** (V0 HasAssembly presence flag: `Node.HasAssembly bool` (omitempty). New `runAssemblyMarker` walker queries every `assembly_statement` node, resolves to the enclosing callable via `nearestFunctionQnameAndStart` (handles function / modifier / constructor / fallback), and sets HasAssembly=true on matching NodeFunction / NodeModifier rows via post-Pass-1 in-place mutation. V0 detects presence only — Yul-internal op detection (delegatecall, sstore, sload, selfdestruct) and receiver resolution deferred to V1+. Grammar v1.2.11 exposes `assembly_statement` as a top-level kind so query is shape-stable). |
| W-C W9 (Sol storage slot index) | W-C W7 완료 | ✅ **LANDED 2026-05-18** (V0 per-contract slot index: `Node.SlotIndex int` (omitempty); `runStateVarDecl` maintains a `slotPerContract` counter keyed on `nearestContractName`, incremented for non-mapping NodeField emits only. Mapping state-vars (NodeMapping) skip the counter — their slot is derived dynamically at runtime per Sol spec §11.1. V0 ignores bit-packing (uint8 counts as one full slot) and inheritance offsets (each contract restarts at slot 0); V1+ adds both. Design doc: `docs/design/solidity-storage-slot-index.md`. Golden fixture unaffected — slot 0 omitted from JSON keeps existing emits stable). |
| W-C W8 (Sol contract-type cast dispatch) | W-C W7 완료 | ✅ **LANDED 2026-05-18** (V0 contract-type cast: `runContractCastDispatch` walker re-uses W3's `matchInterfaceDispatch` predicate for the same `TypeName(args).method` AST shape, `resolveContractCastRef` looks up `byName[NodeContract]` instead of `NodeInterface`. Disjoint from W3 — same name can't be both Contract and Interface in a project, so no double-emit. EdgeInvokes ConfAmbiguous + DispatchKind="contract_cast". Closes the dispatch trio: W3 interface / W7.1 low-level / W8 contract-type). |
| W-C W7 (Sol cross-contract / storage / modifier) | W-C W6 완료 | ✅ **LANDED — W7.1 2026-05-17 + W7.2 2026-05-18 + W7.3 2026-05-18** (W7.1 V0 low-level call: `runLowLevelCalls` + `resolveLowLevelCallRef`, EdgeInvokes ConfAmbiguous + DispatchKind="low_level_call". W7.2 V0 storage location: NodeField.SubKind = visibility + immutable; `constant` keyword and parameter location deferred to V1+ since grammar v1.2.11 drops them from the AST. W7.3 V0 modifier composition: `runHasModifier` extended to compute source-order index via sibling enumeration → Edge.Order field (new, omitempty); `runModifierOverride` walker detects `modifier m() override {}` and emits EdgeOverrides via existing W2 resolver path (NodeModifier qnames added to funcByQName alongside NodeFunction for parent-lookup symmetry). PendingRef.Order field also new. Golden fixture stable — Order=0 omitted from JSON keeps single-modifier emit unchanged). |
| W-C W6 (Sol using For) | W-C W1 완료 | ✅ **LANDED 2026-05-12 (V0-V2.4) / 2026-05-13 (V2.5+)** (V0 binding + V1.0-V1.13 14-tier dispatch + V1.14-V1.21 family validations + V1.22-V1.24 callable kinds + V1.25-V1.27 lightweight guards + V1.28-V1.29 import alias + V1.30 block-shadow V0 + V2.0 line-range scope-aware + V2.1 interface receiver + V2.2 multi-binding + V2.3 library-body guard + V2.4 cross-file multi-binding + V2.5 operator-form limitation lock + V2.6 free-function form rediscovery + V2.7 contract-scope operator-form lock + V2.8 file-level free-function form lock + V2.9 bare free-function alias lock + V2.10 mixed bare/aliased multi-import fix + V2.11 bare path-only import guard + V2.12 UDVT + using-for guard + **V2.13 diamond inheritance multi-parent binding fix** (V1.2 BFS 의 "if !exists" guard 가 first-ancestor-wins 로 후속 ancestor binding 을 drop → local-snapshot + ancestor-union 으로 수정) + **V2.14 interface-body using-for variants lock** (3-variant probe inside `interface { }` body — IBare/IFree → 1 EdgeUsesFor each (V0/V2.6 shape match), IOp → 0 edges (V2.7-style operator-form AST mismatch); interface-scope phantom edges flagged as known graph artifact since interfaces have no state to bind methods on) + **V2.15 same-line shadow byte-precision fix** (V2.0 line-only filter admits both decls when outer + inner-block shadow + both use sites all sit on one line; strict-`>` declLine tiebreak left first-appended outer winning, dropping the inner use site → resurfaces V1.30 V0 false-negative on one line. Fix: PendingRef gains ByteOffset; localDecl gains declStartByte/scopeEndByte; selectLocalDecl switches to byte containment + max declStartByte tiebreak when bytes are uniformly populated, falls back to V2.0 line-only otherwise. RED→GREEN, no regression in 45+ using-for tests or cross-parser) + **V2.16 grammar-blocked items survey** (doc-only — consolidates V1.2/V1.8/V2.5/V2.6/V2.7 scattered claims into one 7-row classification table. Findings: 1 grammar-block remaining (file-level `using ... global;`), 1 query gap remaining (operator-form using_alias), 1 carry-over claim invalidated (free-function form, V2.6 rediscovered), 2 out-of-scope (Yul / pre-0.5.0 `var`), 2 already-complete (tuple destructuring, imports); operator-form query extension flagged as highest-leverage V2.17+ candidate) + **V2.17 operator-form grammar-block lock + V2.16 row 2 reclassification** (V2.16 highest-leverage recommendation invalidated by empirical AST dump on vendored grammar v1.2.11 — `using_alias` is NOT a valid node type, operator-form `using {f as +} for T;` parses with NO `using_directive` node (entire braced body misclassified as state_variable_declaration wrapped in ERROR nodes), free-function form `using {Math.add}` is also degraded but with fortuitous `type_alias` partial recovery that V0 query incidentally captures. Reclassified row 2 from B (query gap) → A (grammar reject), reclassified row 3 from "Not a gap" → "A-partial". V2.17 locks library-scope operator-form at 0 edges (new cell complementing V2.5 file / V2.7 contract / V2.14 IOp interface). No query change — grammar bump or ERROR-tolerant walker required) + **V2.18 file-level using directive ERROR-tolerant walker** (V2.16 row 1 closure. V2.18 AST probe confirms `source_file > ERROR "using ..."` shape is recoverable: library name + bound type extractable from named children. `runFileLevelUsingFor` in using_for.go walks ERROR children, filters by `strings.HasPrefix(text, "using ")`, fans out PendingRef pair (dispatchKindUsingFor + dispatchKindUsingForTypeBind) per contract/interface in v.nodes (library subkinds excluded to avoid self-binding phantom edges). Two fixtures: single-contract `using ... global;` + multi-contract `using ...;` both pass with correct EdgeUsesFor + EdgeCalls dispatch wiring. Shares all infrastructure with runUsingFor — purely additive. Row 1 status flipped: A still blocked → A-recovered. Operator-form (row 2) deferred — V2.18 probe confirmed no recoverable shape, requires grammar bump); W7+ shift V2.19+) |

**Status — 2026-05-11**: W-A (Go cross-function lock propagation) ✅ landed.
`internal/buildpipe/lock_propagation.go` (Stage B DFS depth=5, visited-set
cycle defence, `calls`+`invokes` traversal) + Go-parser field-touch
side-channel + 6 fixture + 6 test. Self-graph KPI: 33 → 68
`accessed_under_lock` edges (+106%). Opt-in via `--lock-propagation`
(default false); incremental path skipped with warning. §5.0 결정 8건
모두 구현 (Q1 Stage B / Q2 INFERRED 통일 / Q3 calls+invokes / Q4 INFERRED
강제 / Q5 opt-in / Q6 dedup / Q7 별도 testdata / Q8 enums comment).

W-C W1 (Sol inheritance `is`-clause) ✅ landed.
`internal/parse/solidity/inheritance.go` + 5 fixture + resolver 확장.
같은 빌드에서 EdgeExtends 8 / EdgeImplements 5 emit (cross-file 2건은
INFERRED, 나머지 EXTRACTED). 상세는
`docs/design/solidity-inheritance-and-interface-dispatch.md` §4.1 Status
블록 참조. W-C W2 (virtual/override) 진입 unblock.

W-C W2 (Sol virtual/override modifier → EdgeOverrides) ✅ landed.
`internal/parse/solidity/overrides.go` (~230 LOC) +
`internal/parse/solidity/overrides_test.go` (~250 LOC) + 6 fixture +
resolver 2-pass split (Pass 2a W1 inheritance, Pass 2b W2 overrides).
같은 빌드 (testdata/overrides) 에서 EdgeOverrides 6 emit
(simple=1 / super_call=2 / multi_explicit=2 / cross_file=1; 5 EXTRACTED
+ 1 INFERRED). Function SubKind 라벨링 `{function, virtual, override,
virtual_override}` 모두 적용. W1 회귀 0 (EdgeExtends 8 / EdgeImplements
5 보존). §7.0 Go regression `--lang=go` diff = 0. W-C W3 (interface
dispatch AMBIGUOUS) 진입 unblock.

W-C W3 (Sol interface dispatch `IFoo(addr).bar()` → EdgeInvokes
AMBIGUOUS) ✅ landed.
`internal/parse/solidity/dispatch.go` (~155 LOC) +
`internal/parse/solidity/dispatch_test.go` (~230 LOC) + 4 fixture
(simple / chained / cross-file pair) + resolver Pass 2b 분기
(`resolveInterfaceDispatchRef`). 같은 빌드 (testdata/dispatch) 에서
EdgeInvokes 6 emit (모두 AMBIGUOUS, §5.0 Q5 결정 — 파일 경계와 무관하게
runtime address 가 dispatch 결정의 출처). per-fixture: simple=2 / chained=3
/ cross_file=1. 음성 케이스 (`address(this)` 원시 cast, `super.foo()`
identifier-object, unknown 식별자) 모두 false positive 0 — 검출
predicate 의 음성 contract 명시. W1/W2/W4 회귀 0
(testdata/inheritance: extends 8 / implements 5 / overrides 1 보존,
testdata/overrides: extends 6 / overrides 6 보존). §7.0 Go regression
`--lang=go` diff = 0. W-C W6 (using For) 진입 unblock.

W-C W6 V0 (Sol `using For` library binding → EdgeUsesFor) ✅ landed
2026-05-12. `internal/parse/solidity/using_for.go` (~100 LOC) +
queries.go `queryUsingFor` (tree-sitter-solidity v1.2.13 `using_directive`
→ `type_alias` → `identifier` 경로) + resolve.go `resolveUsingForRef`
(same-file ConfExtracted / cross-file ConfInferred / 미해결 drop) +
schema 1.10 EdgeUsesFor enum slot append (commit `19c99da`) + 5 fixture
+ 5 test (specific / wildcard / multi-library / contract-scoped /
negative). Q9-1 (b) 결정 (2026-05-12 사용자 정당한 지적 채택 — Solidity
trait-like binding semantics 가 first-class EdgeType, extends /
implements / overrides / has_modifier 와 동급). V0 scope = binding
declaration only. §7.0 Go regression `--lang=go` diff = 0. Viewer
edges.ts G2 카테고리에 amber dashed 등록 + DEFAULT_EDGE_TYPES on by
default.

W-C W6 V1.0 (Sol `using For` state-var dispatch → EdgeCalls) ✅ landed
2026-05-12. `runUsingForCalls` + `matchStateVarMethodCall` predicate
(member_expression 의 `<identifier>.<identifier>(...)` 인식) +
`resolveUsingForCallRef` (4-step chain: funcID → containerID →
typeName → libraryName → libraryFunctionID). State-variable type
인덱스: NodeField QualifiedName 을 `<Container>.<varName>` 으로 qualify
(runFunctionDecl 와 동일 idiom) + NodeField.Signature 에 declared
typeName 저장 (extractTypeNameText). Per-contract binding map
(contractID, typeName | "*") → libraryName 은 Pass 2 에서 신규
`using_for_typebind` PendingRef 들을 sweep해서 채움 — graph edge 는
아니고 binding 정보 carrier. 4 fixture + 4 test (state_var_dispatch,
wildcard_dispatch, specific_over_wildcard for Q9-3 (a) 검증,
no_binding_negative). golden snapshot 갱신 (NodeField qname +
Signature 추가). Confidence: ConfExtracted (same-file) / ConfInferred
(cross-file) — W3 처럼 AMBIGUOUS 으로 downgrade 안 함 (library
dispatch 는 binding 만 알면 statically determinable). V1.0 carry-over:
parameter receiver, return-value chaining, free-function form, file-
level using, inherited using directive 모두 V1.1+. §7.0 Go regression
`--lang=go` diff = 0.

W-C W6 V1.1 (Sol `using For` parameter-receiver dispatch → EdgeCalls)
✅ landed 2026-05-12. function parameter 의 declared type 을
paramTypes 인덱스 ((funcID, paramName) → typeName) 로 색인 +
resolveUsingForCallRef 의 receiver type lookup 에 state-var → parameter
fallback 추가. parameter 정보는 `emitParameterMetaPending` 가
function_definition 의 named children (tree-sitter shape: parameter
직계 child) 를 순회하며 `dispatchKindUsingForParamType` PendingRef 로
emit. Anonymous parameter (name field 부재) 는 skip — 매칭될 receiver
identifier 가 없음. 3 fixture + 3 test (param_receiver,
state_and_param mixed, anonymous_param 가드). 25/25 PASS, vet clean.
V1.2+ carry-over: return-value chaining, free-function form, file-level
using directive, inherited using.

W-C W6 V1.11 (Sol `using For` nested struct field receiver
`<obj>.<field1>.<field2>.<method>`) ✅ landed 2026-05-12. V1.10 의
자연 depth-2 확장. `matchNestedStructFieldReceiverMethodCall` predicate
(outer.object → mid_member → inner_member → identifier — pure 4-level
member access chain, no calls in between) + 신규
`dispatchKindUsingForNestedStructFieldCall` +
`resolveUsingForNestedStructFieldCallRef` 6-step chain (funcID →
containerID → objType → structFieldTypes[objType][field1] = field1Type
→ structFieldTypes[field1Type][field2] = field2Type → libraryName →
libraryFunctionID). V1.10 와 disambiguate: V1.10 의 outer.object =
inner_member (depth 1), V1.11 의 outer.object = mid_member 이고
mid_member.object = inner_member (depth 2). caller dispatch state-var
→ V1.9 → V1.10 → V1.11 → V1.3-V1.8. real-world nested config / account-
of-user 패턴 처리. 3 fixture + 3 test (basic, inner field unknown drop,
middle field not-a-struct drop). 25/25 PASS, vet clean. V1.12+
carry-over: depth ≥ 3 nested struct fields, this 변형, multi-return
tuple destructuring, cross-file struct validation.

W-C W6 V1.10 (Sol `using For` struct-field receiver
`<obj>.<field>.<method>`) ✅ landed 2026-05-12. obj 는 state-var/
parameter 의 declared type 이 known struct, field 는 그 struct 의
member. `runStructFieldMeta` (queryStruct 매칭 + struct_body 의
struct_member 순회 → side-channel PendingRef 로 (structName, fieldName,
fieldType) 색인) + `matchStructFieldReceiverMethodCall` predicate
(V1.9 와 동일 shape 단 inner.object 가 "this" 아닌 identifier) + 신규
`dispatchKindUsingForStructFieldCall` + `resolveUsingForStructFieldCallRef`
6-step chain (funcID → containerID → objType → structFieldTypes lookup
→ fieldType → libraryName → libraryFunctionID). 신규 structFieldTypeMap
타입. real-world OpenZeppelin 의 흔한 `info.amount.add(x)` 패턴 처리.
3 fixture + 3 test (basic struct state-var, unknown field drop, struct
param fallback). 25/25 PASS, vet clean. V1.11+ carry-over: multi-return
tuple destructuring, nested struct field, cross-file struct validation.

W-C W6 V1.9 (Sol `using For` `this.<state-var>.<method>` receiver)
✅ landed 2026-05-12. Sol stylistic variant of V1.0's bare-name
receiver. `matchThisReceiverMethodCall` predicate (inner member is
`this.<state-var>`) + reuses V1.0's `dispatchKindUsingForCall` and
resolver (encoding `<state-var>|<method>` identical after `this`
prefix strip). No new resolver helper required. Caller dispatch
state-var → V1.9 → V1.3-V1.8 (V1.9 catches `this.x.method()` before
V1.4 wastes a stateVarTypes lookup on the literal "this"). 3 fixture
+ 3 test (basic, no-state-var drop, this-vs-bare coexistence).
25/25 PASS, vet clean. V1.10+ carry-over: struct-field receivers
(`info.amount.foo()`), multi-return tuple destructuring, general
member-of-member.

W-C W6 V1.8 (Sol `using For` generic iterative chain walker)
✅ landed 2026-05-12. V1.3/V1.5/V1.7 same-contract + V1.4/V1.6
cross-contract 의 hardcoded pattern 을 iterative walker 로 통합 —
임의 depth N 처리. `matchGenericChain` predicate (outer 부터
inner-most 까지 walk, two modes: same/cross) + 신규
`dispatchKindUsingForGenericChainCall` + `resolveUsingForGenericChainCallRef`
iterative resolution (callerContainer → starting namespace → for-each
segment threading through funcReturnTypes → final return type →
binding lookup → libraryFunctionID). PendingRef encoding 가변 길이
(`same|<empty>|<segs>|method` or `cross|<obj>|<segs>|method`). caller
dispatch state-var → V1.3 → V1.4 → V1.5 → V1.6 → V1.7 → V1.8 (V1.8
가 depth ≥ 4 same / depth ≥ 3 cross 만 unique). 3 fixture + 3 test
(depth-4 same, depth-3 cross, depth-5 same generic scale). V1.3-V1.7
hardcoded 는 명확한 shape 별 코드 path 보존 위해 V1.8 후에도 유지
(deprecation 은 V2+ 검토). 25/25 PASS, vet clean. V1.9+ carry-over:
multi-return tuple slot, member-of-member receivers.

W-C W6 V1.7 (Sol `using For` depth-3 same-contract chained dispatch
`<fn>().<fn>().<fn>().<method>`) ✅ landed 2026-05-12. V1.5 (depth-2)
의 한 링크 더. `matchTripleChainedMethodCall` predicate (4-level
nested AST recurse) + 신규 `dispatchKindUsingForTripleChainCall` +
`resolveUsingForTripleChainCallRef` 9-step chain (funcID →
containerID → fn1FuncID → returnType1 → fn2FuncID in returnType1 →
returnType2 → fn3FuncID in returnType2 → returnType3 → libraryName →
libraryFunctionID). V1.5 와 disambiguate: V1.5 의 innerCall 위치가
V1.7 의 L2, V1.7 의 innerCall 은 L1. caller dispatch state-var →
V1.3 → V1.4 → V1.5 → V1.6 → V1.7. 3 fixture + 3 test (basic,
middle_unknown drop, no_binding drop). 25/25 PASS, vet clean.
V1.8+ carry-over: cross-contract depth-3, depth ≥ 4, generic walker
refactor 가능 (V1.3/V1.5/V1.7 의 hardcoded pattern 통합), multi-return
tuple slot.

W-C W6 V1.6 (Sol `using For` deep cross-contract chained dispatch
`<obj>.<fn>().<fn>().<method>`) ✅ landed 2026-05-12. V1.4 (cross-
contract 1-link) + V1.5 (same-contract depth-2) 의 합성.
`matchDeepCrossContractChain` predicate (8-level AST recurse: outer →
middle call → middle member → inner call → inner member with
identifier receiver) + 신규 `dispatchKindUsingForDeepCrossChainCall` +
`resolveUsingForDeepCrossChainCallRef` 8-step chain (funcID →
containerID → receiverType → innerFn1FuncID in receiverType namespace
→ returnType1 → innerFn2FuncID in returnType1 namespace → returnType2
→ libraryName → libraryFunctionID). V1.5 와 disambiguate: V1.5 의
inner call_expression.function 이 identifier, V1.6 의 그 자리는
member_expression. caller dispatch V1.5 → V1.6. 3 fixture + 3 test
(basic, unknown_middle drop, no_binding drop). 25/25 PASS, vet clean.
V1.7+ carry-over: depth ≥ 3 generic chains, multi-return tuple slot.

W-C W6 V1.5 (Sol `using For` depth-2 chained dispatch
`<fn>().<fn>().<method>`) ✅ landed 2026-05-12. V1.3 (same-contract
1-link chain) 의 깊이 확장. `matchDeepChainedMethodCall` predicate
(outer member_expression.object 가 middle call_expression, middle
.function 이 middle member_expression, middle .object 가 inner
call_expression, inner .function 이 identifier) + 신규
`dispatchKindUsingForDeepChainCall` + `resolveUsingForDeepChainCallRef`
7-step chain (funcID → containerID → innerFn1FuncID → returnType1 →
innerFn2FuncID in returnType1 namespace → returnType2 → libraryName →
libraryFunctionID). V1.4 와 disambiguate: V1.4 의 inner member.object
가 identifier, V1.5 의 그 자리는 call_expression. caller dispatch
순서 V1.4 → V1.5. 3 fixture + 3 test (basic, middle_unknown,
primitive_middle drop). 25/25 PASS, vet clean. V1.6+ carry-over:
cross-contract deep chains (`obj.foo().bar().baz()`), depth >= 3
generic chains, multi-return tuple.

W-C W6 V1.4 (Sol `using For` cross-contract chained dispatch
`<obj>.<fn>().<method>`) ✅ landed 2026-05-12. receiver obj 가 state
var 또는 parameter (V1.0/V1.1 인덱스 재사용); typeName 이 known
Contract / Interface 이면 inner method 의 declaration 을 그 container
의 namespace 에서 찾고, 그 method 의 return type 을 V1.3 처럼 receiver
type 으로 사용. `matchCrossContractChain` predicate (outer
member_expression.object 가 inner call_expression, inner
call_expression.function 이 inner member_expression) + 신규
`dispatchKindUsingForCrossChainCall` + `resolveUsingForCrossChainCallRef`
7-step chain (funcID → containerID → receiverType → innerFuncID →
returnType → libraryName → libraryFunctionID). Type cast
(`IFoo(addr).bar()`) 는 inner function 이 identifier 라 V1.3 가 먼저
매칭, V1.4 는 member_expression 만. 3 fixture + 3 test (basic,
unknown_method drop, primitive_drop). 25/25 PASS, vet clean. V1.5+
carry-over: deeper chains, multi-return tuple slot.

W-C W6 V1.3 (Sol `using For` chained-call dispatch `<fn>().<method>`)
✅ landed 2026-05-12. V1.3 첫 시도 = free-function form (`using {Lib.f1,
Lib.f2} for T`) 였으나 V1.2 file-level 처럼 tree-sitter-solidity v1.2.13
grammar 한계 (`{...}` brace shape 가 ERROR-node) 로 revert. V1.3 scope
를 **return-value chaining** 으로 재설정. function 의 `return_type`
field 에서 첫 슬롯의 typeName 을 funcReturnTypes 인덱스 ((funcID) →
returnTypeName) 로 색인 + matchChainedMethodCall predicate (inner
expression 이 plain function call 인 member_expression shape) + 신규
resolveUsingForChainCallRef helper 의 5-step chain. multi-return tuple
의 첫 슬롯만 V0 — 두번째+는 V1.4. 3 fixture + 3 test (basic chain,
no-binding drop, unknown-fn drop). 25/25 PASS, vet clean. V1.4+
carry-over: cross-contract chaining (`obj.foo().bar()`), multi-return
tuple slot.

W-C W6 V1.2 (Sol `using For` inherited binding propagation) ✅ landed
2026-05-12. 처음 V1.2 = file-level using (0.8.13+) 로 시작했으나
tree-sitter-solidity v1.2.13 grammar 가 source_file scope 의
using_directive 를 ERROR node 로 parse 함을 AST dump 로 확인 (cmd_probe
임시 도구 사용 + 제거). file-level 작업물 revert + grammar 한계
spec/queries.go 노트로 보존. V1.2 scope 를 **inherited using directive**
로 재설정: W1 inheritance graph 의 parents adjacency 재사용, Pass 2
binding map 사전 빌드 직후 BFS 로 ancestor 의 bindings 를 descendant
에 merge. child-scope typeName entry 보존 (Solidity scoping — local
shadows inherited). cycle 방어 visited set. EdgeUsesFor 는 parent 의
declaration site 에만 emit — Child 에 synthetic edge 안 만듦 (graph
가 "어디 declared 됐나" 정확히 표현). 3 fixture + 3 test
(inherited_basic, inherited_multi_level transitive BFS, inherited_child_
overrides shadow). 25/25 PASS, vet clean. V1.3+ carry-over:
return-value chaining, free-function form, file-level (grammar
업그레이드 후).

W-B W1 (TS heritage `extends`/`implements`) ✅ landed.
`internal/parse/typescript/heritage.go` (284 LOC) + 6 fixture +
`heritage_test.go` 4 test (FixtureMatrix / CrossFile / UnresolvedDropped /
EdgeDirection). Class/Interface 전용 인덱스 `heritageByName` 로 동명
Function/Method 부모-후보 오염 차단. same-file=ConfExtracted, cross-file=
ConfInferred, 미해결=drop. self-graph 측정: EdgeExtends 6 / EdgeImplements
7 emit. §7.0 Go regression diff=0. Commit `6f78427`.

W-B W2 (TS async/await — NodeAwaitPoint + EdgeAwaits) ✅ landed.
`internal/parse/typescript/async.go` (~180 LOC) + 5 fixture +
`async_test.go` 3 test (FixtureMatrix / SchemaInvariants / TopLevelDropped).
`declarations.go::runQuery` 가 NodeFunction/NodeMethod 의 name capture
parent chain 을 거슬러 `async` 키워드 검출 → SubKind="async" 부여.
`runAsync()` 가 await_expression 위치를 가장 가까운 Function/Method
interval 에 anchor (top-level await drop, V0). §5.0 Q1/Q2/Q3/Q5 결정 반영
(EdgeAsyncCall skip — AwaitPoint.StartByte 와 CallSite 의 위치 overlap 으로
future join). self-graph 측정: 245 AwaitPoint / 245 EdgeAwaits (pair
invariant ✅), 48 async Function / 118 async Method. §7.0 Go regression
diff=0. Commit `0866ef0`.

W-B W3 (viewer edge style + grouping) ✅ landed.
`web/viewer-next/src/lib/edges.ts` 에 W-B/W-C/W series 5종 (awaits,
overrides, http_calls, grpc_listens_on, grpc_calls) edge style + GRAPH_GROUPS
배치 + DEFAULT_EDGE_TYPES whitelist 갱신. Self-check 주석: 34→39
non-hidden edges (G1=3 / G2=13 / G3=5 / G4=6 / G5=7 / G6=5).
`npm run typecheck` PASS. Commit `7af9ce4`.

W-B W4 (measurement + handoff) ✅ landed 2026-05-12.
self-graph (CKG 본 레포 TS subset, 82 files) 빌드 후 KPI 측정:

| 항목 | Before | After | 변화 |
|------|--------|-------|------|
| async Function | 0 | 48 | +48 (2.2% of 2,214) |
| async Method | 0 | 118 | +118 (2.6% of 4,565) |
| NodeAwaitPoint | 0 | 245 | +245 |
| EdgeAwaits | 0 | 245 | +245 (pair invariant ✅) |
| EdgeExtends (TS) | 0 | 6 | +6 |
| EdgeImplements (TS) | 0 | 7 | +7 |

밀도: AwaitPoint / async callable = 245 / 166 = 1.48. Heritage edge /
(Class+Interface) = 13 / 69 = 18.8%. 보조 W series: listens_on 22 /
http_calls 9 / grpc_calls 7. 상세는
`docs/design/ts-async-await-and-interface.md` §4.4 LANDED 블록 참조.
go-stablenet TS fixture 빌드는 W5+ 후속으로 보류 (self-graph 측정으로
schema 1.10 slot 활성화 + pair invariant 검증 만족).

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
