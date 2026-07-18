# Solidity Cross-Contract / Storage / Modifier Composition — Design Spec (W-C W7)

> Scope: extend the Solidity parser (`internal/parse/solidity/`) so the graph
> captures three currently-invisible dimensions:
>
> 1. **W7.1 — low-level call dispatch**: `target.call(...)`,
>    `target.delegatecall(...)`, `target.staticcall(...)`. Currently zero edges
>    are emitted. These are the workhorse primitives for proxy contracts,
>    upgradeable patterns, and anything that bypasses interface dispatch —
>    invisible to W3.
>
> 2. **W7.2 — storage location metadata**: `storage` / `memory` / `calldata` /
>    `constant` / `immutable` keywords on state vars + function parameters.
>    NodeField currently captures the type but drops the location qualifier.
>    Storage-vs-memory is the single most important security distinction in
>    Sol; without it, the graph can't differentiate "writes to chain state"
>    from "operates on a function-local copy".
>
> 3. **W7.3 — modifier composition**: existing `EdgeHasModifier` captures the
>    fact that `function f() onlyOwner whenNotPaused {}` *has* two modifiers
>    but loses both (a) the application order (Sol semantics: outer-to-inner,
>    affects reentrancy guards) and (b) modifier inheritance (`override` on
>    `modifier` definitions). Both matter for any security-property check.
>
> **Status**: **LANDED** (header updated 2026-07-18; the original "no
> implementation yet" draft note is superseded). W7.1 low-level call dispatch
> (`internal/parse/solidity/low_level_call.go` → `invokes`), W7.2 storage-location
> metadata (SubKind in `declarations.go` `runStateVarDecl`), and W7.3 modifier
> composition + `override` (`modifier_composition_test.go`, `overrides.go`) are
> all emitted. This doc is now a historical design record. V1+ deferrals remain
> in §5a (function-pointer dispatch, bit-packing, mapping-slot derivation, Yul).
>
> **Prerequisites**: W-C W1 (inheritance), W-C W2 (override), W-C W6 (using-for
> resolver infrastructure) — all landed.
>
> **Out-of-scope**: Yul/inline assembly dispatch (separate spec — covered by
> V2.16 row 4 deferral), storage slot index calculation (W7.2 V0 surfaces the
> location qualifier only; slot indexing is a follow-up requiring layout
> resolution across inheritance), cross-contract bytecode-level analysis
> (separate workstream).

---

## §0. Cold start

- **무엇**: 세 dimension 동시 확장:
  - W7.1: `target.call(abi.encodeWithSignature("foo()"))` 패턴이 graph 에서
    완전히 invisible. AMBIGUOUS dispatch 로 인식 못함.
  - W7.2: `uint256 storage x = arr[i]` vs `uint256 memory x` 가 parse 시
    type 만 보존되고 location 폐기.
  - W7.3: `function f() onlyOwner whenNotPaused {}` 에서 modifier 적용 순서가
    edge 에 안 담김.

- **왜**:
  - W7.1: 실제 Sol 코드의 ~10-30% (proxy / upgradeable / delegatecall pattern)
    가 low-level call 사용. interface dispatch 못 잡으면 graph 가 "이
    contract 가 누구를 호출하는가" 질문에 큰 누락. 보안 분석 ("이
    delegatecall 의 target 이 안전한가") 불가능.
  - W7.2: storage location 은 Sol 의 가장 중요한 의미 구분.
    `function transfer(string memory s)` vs `function transfer(string
    storage s)` 는 EVM 동작이 근본 다름. graph 가 둘을 구별 못하면
    storage 충돌 / pointer aliasing 같은 분석 불가.
  - W7.3: modifier 적용 순서는 reentrancy / access-control 의 핵심.
    `nonReentrant onlyOwner` vs `onlyOwner nonReentrant` 는 보안 동작
    다름. 그래프가 순서 정보 잃으면 security audit 의 1차 query 가 깨짐.

- **어떻게**:
  - W7.1: 새 `member_expression` predicate (V0 strict-purge), `EdgeInvokes`
    재사용 + `DispatchKind="low_level_call"` + `ConfAmbiguous`. Receiver
    resolution 은 W6 V1.0-V1.x lookupReceiverType infrastructure 직접 재사용.
  - W7.2: `state_variable_declaration` / `parameter` 노드의 location
    keyword 자식 추출, `NodeField.SubKind` 에 직접 인코딩. `EdgeWritesMapping`
    같은 emit 사이트는 변경 없음 (W7.2 는 metadata only V0).
  - W7.3: 기존 `EdgeHasModifier` PendingRef 에 `Order int` 필드 추가
    (PendingRef.ByteOffset 이 W6 V2.15 에서 추가된 것과 동일 패턴).
    `override` keyword 가 modifier_definition 에 있으면 W2 의
    `EdgeOverrides` infrastructure 재사용해 emit.

---

## §1. 현재 상태

### 1.1 W7.1 low-level call — what's captured / not

| 패턴 | 현재 동작 | 원인 |
|------|----------|------|
| `IFoo(addr).bar()` | W3 ✅ EdgeInvokes AMBIGUOUS | dispatch.go runDispatch |
| `target.call(...)` | ❌ no edge | runDispatch predicate rejects (object is identifier, not call_expression) |
| `target.delegatecall(...)` | ❌ no edge | 동일 |
| `target.staticcall(...)` | ❌ no edge | 동일 |
| `target.send(value)` | ❌ no edge | 동일 (value transfer, not method call) |
| `target.transfer(value)` | ❌ no edge | 동일 |

dispatch.go 의 §0 explicitly out-of-scope: `addr.call(...)` etc.

### 1.2 W7.2 storage location — what's captured / not

`runStateVarDecl` (declarations.go:208) walks state_variable_declaration 와
NodeField 생성. 현재 캡처:
- `Name` = 변수 이름
- `Signature` = type text (e.g. "uint256", "mapping(address => uint256)")
- `SubKind` = "" (empty)

NOT captured:
- visibility keyword (public / private / internal / external) — partial
- mutability keyword (constant / immutable)
- location keyword (storage / memory / calldata) — function parameter 만

`emitParameterMetaPending` (overrides.go) — parameter 의 type 은 paramTypes
인덱스에 저장되나 location 은 폐기됨.

### 1.3 W7.3 modifier composition — what's captured / not

`queryHasModifier = (modifier_invocation (identifier) @mod) @stmt`. Pass 2
`EdgeHasModifier` (Function → Modifier) emit. 캡처:
- modifier 가 적용됐다는 사실

NOT captured:
- 적용 순서 (same function 의 multi-modifier 들 사이 order)
- modifier 자체의 `override` keyword (modifier 도 inheritance 통해 override
  가능, Sol 0.6.0+)

---

## §2. 목표 동작

### 2.1 W7.1 V0 — low-level call

```solidity
function callTarget(address target, bytes memory data) external {
    (bool ok, ) = target.call(data);                  // EdgeInvokes (AMBIGUOUS)
    target.delegatecall(data);                        // EdgeInvokes (AMBIGUOUS)
    address(other).staticcall(data);                  // EdgeInvokes (AMBIGUOUS)
}
```

V0 emit:
- 1 EdgeInvokes per detected call (`call` / `delegatecall` / `staticcall`).
- `DispatchKind = "low_level_call"` (new constant).
- `Confidence = ConfAmbiguous` always — target is runtime address, never
  resolvable at AST time.
- Edge `Src = enclosing function ID`, `Dst = receiver node ID if resolvable,
  else drop` (V0 strict-purge same as W3).

Receiver resolution chain (re-uses W6 infrastructure):
1. If receiver is bare identifier → look up state-var / param / local-var
   (lookupReceiverType chain). Type → byName index → if NodeContract /
   NodeInterface, that's the target.
2. If receiver is `address(x)` cast → drop in V0 (no contract-type tracking yet).
3. If receiver is chain (e.g. `factory.getX().call(...)`) → drop in V0.

Out of V0:
- `send` / `transfer` (value-transfer primitives, not method calls — separate
  edge type `EdgeTransfersValue` candidate for V1.x).
- Function-pointer call (`fn.selector` form).
- Yul-level call ops.

### 2.2 W7.2 V0 — storage location metadata

```solidity
contract C {
    uint256 public x;                          // SubKind: "storage_public"
    uint256 private y;                         // SubKind: "storage_private"
    uint256 constant Z = 42;                   // SubKind: "constant"
    uint256 immutable W;                       // SubKind: "immutable"

    function f(string memory s, bytes calldata b) external {
        string storage ref = stored;           // parameter location preserved
    }
}
```

V0 emit:
- `NodeField.SubKind` populated with encoded location for state-vars.
  Encoding: `"<visibility>_<location>"` or single-token for constant /
  immutable. Empty string means unknown (legacy fallback).
- `paramTypes` 인덱스에 location 정보 추가 (param 의 SubKind 직접 노드에
  반영하기 어려운 경우 paramTypes map shape 을 (type, location) 튜플로
  확장).
- `EdgeWritesMapping` 같은 기존 emit 은 변경 없음 — V0 는 metadata only.

Out of V0:
- Storage slot **index** 계산 (packed struct layout, inherited slot
  offsets) — W7.2 V1+.
- function-local `<Type> storage ref = ...` 형태의 storage pointer aliasing
  추적 — W7.2 V1+.

### 2.3 W7.3 V0 — modifier composition

```solidity
contract C {
    modifier onlyOwner() { require(msg.sender == owner); _; }
    modifier nonReentrant() { /* ... */ _; }

    function withdraw() external nonReentrant onlyOwner {
        // executes nonReentrant wrapper FIRST, then onlyOwner inside it
    }
}

contract Child is Parent {
    modifier onlyOwner() override { /* ... */ _; }
}
```

V0 emit:
- `EdgeHasModifier` 에 `Order int` 필드 추가 (Edge 구조 확장). Multi-modifier
  function 에서 modifier_invocation 의 AST 순서를 0-indexed 로 기록.
- `EdgeOverrides` 를 modifier-pair 에 대해서도 emit. modifier_definition 에
  `override` keyword 가 있으면 W2 의 EdgeOverrides infrastructure 재사용 —
  부모 contract 의 동명 modifier 로 link.

Out of V0:
- Modifier body 안의 `_` placeholder 위치 분석 (modifier 실행 시점이
  before/after/wrap — 현재는 항상 wrap 으로 간주).
- Cross-modifier 부수효과 그래프 (한 modifier 가 다른 modifier 의 state 변경).

---

## §3. 검출 알고리즘

### 3.1 W7.1 detection (low-level call)

새 walker `runLowLevelCalls` in `using_for.go` 또는 신규 `low_level_call.go`:

```
walk member_expression nodes
predicate: property is identifier ∈ {call, delegatecall, staticcall}
          object is identifier (V0: simple receiver only)
          parent is call_expression
emit:
  enclosingFunctionID → receiver-resolved-target as EdgeInvokes
  DispatchKind = "low_level_call"
  Confidence = ConfAmbiguous (post-resolve)
```

Resolver (`resolveLowLevelCallRef`): receiver name → lookupReceiverType
(state-var / param / local-var) → if type is a known contract/interface
in byName, that's the Dst; else drop.

### 3.2 W7.2 detection (storage location)

Extend `runStateVarDecl` (declarations.go:208) — currently extracts type +
name. Add visibility + mutability + location children walk. SubKind encoding:

```
constant         → SubKind = "constant"
immutable        → SubKind = "immutable"
public / external / internal / private → SubKind = "storage_<vis>"
(default)        → SubKind = "storage_internal"  (Sol default)
```

For function parameters, extend `emitParameterMetaPending` (overrides.go)
to also record location in paramTypes — needs shape change from
`map[string]string` to `map[string]paramSlot{type, location}`.

### 3.3 W7.3 detection (modifier composition)

Two-part extension:

**(a) Order**: extend `queryHasModifier` matcher's emit loop to track
`childIndex` (0-indexed position of modifier_invocation among siblings
within the function_definition). PendingRef carries `Order int`. Edge.go
gains `Order int` (omitempty).

**(b) Override**: query modifier_definition's children for `override`
keyword (currently W2 only walks function_definition). When present, emit
PendingRef with DispatchKind="override" (reuse W2 infrastructure). Resolver
already handles bare `override` walking parent contracts — modifier path
joins same code by NodeType check.

---

## §4. 구현 계획

### 4.1 W7 design doc — *this commit*

- `docs/design/solidity-cross-contract-storage-modifier.md` (this file).
- `docs/DISPATCH-WITHIN-LANG-SEMANTICS.md` tracking row 추가.
- 추정 ~500 LOC 문서.

### 4.2 W7.1 V0 — low-level call

- Files touched:
  - `internal/parse/solidity/low_level_call.go` (new, ~150 LOC).
  - `internal/parse/solidity/resolve.go` (resolveLowLevelCallRef ~50 LOC).
  - `internal/parse/solidity/declarations.go` visit() wire (1 line).
- Tests:
  - `low_level_call_test.go` with fixtures covering call / delegatecall /
    staticcall + state-var / param / local-var receiver.
- Estimated ~250 LOC, 1 commit.

### 4.3 W7.2 V0 — storage location SubKind

- Files touched:
  - `internal/parse/solidity/declarations.go` runStateVarDecl extension
    (~50 LOC).
  - `internal/parse/solidity/overrides.go` emitParameterMetaPending shape
    change (~80 LOC).
  - `internal/parse/solidity/resolve.go` paramTypes consumers updated
    (signature widening).
- Tests:
  - `storage_location_test.go` with fixtures covering each visibility +
    mutability + parameter location.
- Estimated ~200 LOC + signature ripple, 1 commit (medium ripple).

### 4.4 W7.3 V0 — modifier composition

- Files touched:
  - `internal/parse/parser.go` PendingRef.Order field (~5 LOC).
  - `internal/parse/solidity/declarations.go` runHasModifier extension
    (~30 LOC for Order encoding).
  - `internal/parse/solidity/declarations.go` modifier_definition override
    walk (~40 LOC).
  - `pkg/types/edge.go` Edge.Order int field (~5 LOC).
  - `internal/parse/solidity/resolve.go` ordered EdgeHasModifier emit
    (~10 LOC).
- Tests:
  - `modifier_composition_test.go` with multi-modifier function +
    modifier override fixture.
- Estimated ~150 LOC, 1 commit.

### 4.5 측정 + 핸드오프

- Self-graph KPI: count new EdgeInvokes (low_level_call) + count NodeField
  with non-empty SubKind + count EdgeHasModifier with Order > 0.
- W7 carry-over (W7.x+ V1+): storage slot index, value-transfer edges,
  contract-type cast tracking, modifier body `_` placeholder analysis,
  Yul integration.

---

## §5. 결정 (locked 2026-05-17)

| # | 결정 | 선택 | 근거 |
|---|------|------|------|
| W7-D1 | low-level call 의 edge type | 기존 `EdgeInvokes` 재사용 + DispatchKind | W3 가 이미 ConfAmbiguous 로 동일한 의미 (runtime dispatch) 처리. 새 edge type 추가는 schema bump 비용 vs 가치 thin. |
| W7-D2 | low-level call 의 receiver `address(x)` 캐스트 | V0 drop | contract-type tracking 인프라 없음. 가치 있는 candidate 지만 W7.1 V1+ 로. |
| W7-D3 | storage location 의 표현 방식 | NodeField.SubKind 단일 토큰 (`"constant"`, `"immutable"`, `"storage_<vis>"`) | 새 field 추가 (schema bump) vs SubKind 재사용. 후자가 cheaper, downstream consumer 가 SubKind 를 이미 처리 (`abstract`, `library` 등). |
| W7-D4 | paramTypes shape change | `map[string]paramSlot` 로 widening (type + location) | 기존 `map[string]string` 은 location 못 담음. 모든 consumer (4 callers) 가 동시에 변경되어야 — V2.15 PendingRef extension 패턴과 동일. |
| W7-D5 | modifier order 표현 | `Edge.Order int` (omitempty) 신규 필드 | EdgeHasModifier 만 의미 있음. 다른 edge type 은 Order 의미 없음 → omitempty 로 다른 edge 영향 0. |
| W7-D6 | modifier override 의 emit 경로 | 기존 W2 EdgeOverrides 재사용 (NodeType-agnostic) | W2 resolver 가 이미 method-pair / interface-method-pair 처리. modifier-pair 도 동일 idiom. 새 resolver 불필요. |
| W7-D7 | 서브트랙 진행 순서 | W7.1 → W7.2 → W7.3 | W7.1 이 self-contained (resolver 재사용 only). W7.2 가 paramTypes ripple 있음. W7.3 가 PendingRef + Edge 양쪽 ripple. 점진적 risk 증가 순. |
| W7-D8 | Yul / inline assembly | Out of scope (W7.x 외부) | Yul 은 별도 grammar context. V2.16 row 4 deferral 과 일관. 별도 spec workstream. |

---

## §5a. V1 carry-over deferral notes (2026-05-18)

W-C carry-over batch landed (W6 V2.19, W8 V1, W9 V1, W10 V1.1). Five
items intentionally deferred — each with a documented rationale below
so the next pass can revisit with full context rather than re-deriving
the trade-offs.

### Deferred — W6 V2.x operator-form recovery walker

V2.17's AST probe established that `using {f as +} for T;` parses
with no `using_directive` node at all (the braced body becomes a
malformed `state_variable_declaration`). A recovery walker would
need to discriminate this misparse from real state-var declarations,
which is fragile in practice. Defer until either the upstream
tree-sitter-solidity grammar is bumped (cleanest fix) or a specific
use case justifies the false-positive risk.

### Deferred — W8 V2 function-pointer dispatch

`stored = registry.handler; stored(args);` — Sol-supported but the
parser currently has no function-type tracking on state vars or
locals. Without that infrastructure, V0 recovery would only catch
the trivial case of inline `(function(...)).call(...)` which is
rare in practice. Worth one bigger session of infrastructure work
when the use case lands.

### Deferred — W9 V2 bit-packing

Sol storage layout §11.1 packs sub-32-byte fields into shared slots
when consecutive, but the rules entangle type size, fixed-array
alignment, struct embedding, and dynamic-type-skip semantics. A
primitive-only version (uint8 / uint16 / address / bool / bytes_N)
is straightforward but is wrong for the array and struct cases
that most production contracts care about. Either land the full
algorithm or defer; W9 V1 explicitly does the latter.

### Deferred — W9 V3 mapping slot derivation

Mapping slot computation is `keccak256(key, slot)` at runtime — the
slot the mapping declaration occupies is well-defined, but the
per-key slot is dynamic. Either model the declaration slot only
(largely covered by W9 V1 already, just with NodeMapping skipped)
or surface key-encoding details for off-chain analysis. The
incremental value over W9 V1 is small until a concrete consumer
needs it.

### Deferred — W10 V2 Yul receiver resolution

V1.1 surfaces `delegatecall` / `call` / `staticcall` opcode names
on each callable. V2 would resolve the second argument (target
address) of these ops back to a Sol identifier via `yul_path` →
Sol-scope mapping, then chase the identifier through
lookupReceiverType (the W6 chain). Fragile because yul_path
identifiers can shadow Sol identifiers, the binding direction
isn't always recoverable, and the target is frequently a temporary
loaded from storage. Wait for a concrete demand.

### Deferred — W11 V1 real parser → persist → BuildPack integration

W11 V0 (`TestBuildPack_SolGraphRegression`) used a Sol-shaped
fakeStore. V1 would run the real parser on a synthetic git
repository, persist via SQLite, then call BuildPack. The fixture
cost is non-trivial (test-only git history with stable hashes,
plus a small Sol+TS+Go corpus). Perf baseline and cross-language
fixture regressions belong here too. NEXT-CANDIDATES-2026-05-10
estimated this as Mid effort — defer until the regression matrix
is genuinely sparse.

---

## §6. Out of scope (W7 외부, 향후 spec 후보)

- Yul / inline assembly dispatch — 별도 grammar context, 별도 spec.
- Storage slot **index** 계산 — packed struct, inherited slot offset 등.
  W7.2 V0 는 location 만; index 는 V1+ 또는 별도 W8.
- Contract-type cast tracking (`MyContract(addr).foo()`) — interface
  dispatch 의 자매 패턴, W3 와 별도. 별도 spec 후보.
- Cross-contract value transfer (`addr.transfer(value)` / `addr.send`) —
  W7.1 V0 가 method call 만 다룸. value transfer 는 별도 edge type 후보.
- Function pointer / function selector dispatch — Sol 0.8.x 의 `fn.selector`
  / `using {f as +}` operator dispatch. 후자는 V2.17 grammar block 미해결.

---

## §7. Adjacent docs / references

- W-C 본 문서: `docs/design/solidity-inheritance-and-interface-dispatch.md`
  (W1-W6 + W6 V0-V2.18 narrative).
- Dispatch tracking index: `docs/DISPATCH-WITHIN-LANG-SEMANTICS.md`.
- Track-C detector gap (P0/P1 모두 closed 7b32031):
  `docs/design/track-c-detector-gap.md`.
- W3 interface dispatch resolver (W7.1 의 idiom 원본):
  `internal/parse/solidity/dispatch.go`.
- W6 lookupReceiverType (W7.1 receiver resolution 재사용 대상):
  `internal/parse/solidity/resolve.go` §lookupReceiverType.
