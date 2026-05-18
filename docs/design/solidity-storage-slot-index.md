# Solidity Storage Slot Index — Design Spec (W-C W9)

> Scope: extend the Solidity parser so each `NodeField` (state variable
> emitted by `runStateVarDecl`) carries the slot index it occupies in
> EVM storage. Enables storage-collision detection, upgrade-safety
> analysis, and answering "which state-var lives at slot N" without
> re-deriving the layout from source.
>
> **Status**: design 2026-05-18. V0 implementation in same series.
>
> **Out of scope for V0** (deferred to V1+):
>   - Bit-packing within a slot (multiple sub-32-byte values sharing
>     one slot per Sol's natural alignment rules).
>   - Inheritance offset (child contract storage starts at parent's
>     slot count). V0 reports per-contract indices, not absolute EVM
>     slots.
>   - `mapping` and dynamic array slot derivation (hash-based slot
>     calculation per Sol spec §11.1).
>   - Inline `assembly { sload/sstore }` references.

---

## §0. Cold start

- **무엇**: 각 state-var (NodeField) 가 자신이 속한 contract 안에서
  EVM storage slot 인덱스를 갖게 함. V0 는 declaration-order index,
  V1+ 는 actual packed/inherited slot.

- **왜**:
  - Upgrade-safe contracts (Proxy / Diamond pattern) 는 storage
    layout 이 일치해야 안전. 현재 graph 는 layout 정보 미보존 → upgrade
    분석 불가.
  - Storage collision (delegatecall 시 caller/callee 의 slot 0 충돌)
    도 동일 이유로 invisible.
  - 보안 audit tooling 의 1차 query: "이 storage slot 에 누가 write
    하는가", "이 slot 이 어느 변수인가" — slot index 없이는 불가.

- **어떻게**:
  - `pkg/types/node.go` Node 에 `SlotIndex int` 필드 추가 (omitempty).
    NodeField 외에는 zero (생략).
  - `runStateVarDecl` walker 가 each contract 안에서 state_variable_
    declaration 순서대로 0, 1, 2, ... 부여. mapping subtype 은 skip.
  - V0 는 declaration-order only — packing 무시 (uint8 도 1 slot).
    inheritance offset 무시 (per-contract index).

---

## §1. 현재 상태

`runStateVarDecl` (declarations.go:214) 가 NodeField 를 emit:
- `Name` (변수명)
- `QualifiedName` (`Container.varName`)
- `Signature` (type text)
- `SubKind` (W7.2: visibility + immutable encoded)

다음이 빠짐:
- Slot index (V0 본 spec 추가)
- Type byte-size (V1+ packing 계산용)
- Inheritance offset (V1+)

`runMappingDecl` 은 NodeMapping 별도 emit — V0 미해당.

---

## §2. 목표 동작 (V0)

```solidity
contract Token {
    uint256 totalSupply;          // SlotIndex = 0
    address owner;                // SlotIndex = 1  (V0: 1 slot, V1+: packed at slot 1 byte 0..19)
    uint8   decimals;             // SlotIndex = 2  (V0: 1 slot, V1+: shares slot with `owner`)
    mapping(address => uint256) balances;  // SlotIndex omitted (NodeMapping, separate path)
    bool    paused;               // SlotIndex = 3  (V0: 1 slot; V1+: depends on packing)
}
```

V0 emit per non-mapping state-var:
- `NodeField.SlotIndex = N` where N is the 0-indexed position among
  non-mapping state-vars in the same contract.

Mappings: NodeMapping path unchanged. (mapping slot derivation in
V1+ — `keccak256(key . slot)` per Sol spec.)

Inherited contracts: V0 reports per-contract indices. `contract B is
A` where A has 3 state-vars and B adds 1 → B's first var has
SlotIndex=0 in V0 (not 3). V1+ adds inheritance offset for absolute
EVM slots.

---

## §3. 검출 알고리즘 (V0)

`runStateVarDecl` 의 emit loop 안에서 contractID 단위로 counter
유지:

```
slotPerContract := map[contractID]int

for each state_variable_declaration matched:
    name, type, ... extract  (existing logic)
    if isMapping: emit NodeMapping (existing path) — skip slot
    else:
        containerID := nearestContractID
        slot := slotPerContract[containerID]
        slotPerContract[containerID]++
        emit NodeField with SlotIndex = slot
```

`nearestContractID` 는 nearestContractName 의 ID 변환. 이미 W6 V1.0
패턴으로 NodeContract id = MakeID(name, "sol", nameStartByte) 형태 →
nearestContractName + 컨테이너 byName 인덱스 미사용 (Pass 1 walker
는 자신의 v.nodes 만 봄). 간단한 방법: emit 순서대로 같은
contractName 단위로 counter.

V0 가정:
- state_variable_declaration 의 emit 순서 = source order (tree-sitter
  query match 순서) — query 가 같은 query 안에서 source order 보장
  하는지 확인 필요. AST DFS 순서이면 안전.

---

## §4. 구현 계획

### W9 V0 — slot index per contract (이 commit)

- `pkg/types/node.go`: `SlotIndex int` (omitempty) 추가.
- `internal/parse/solidity/declarations.go runStateVarDecl`: contract
  단위 counter 로 slot 할당. NodeMapping 은 skip.
- `internal/parse/solidity/storage_slot_test.go`: fixture + test
  asserting slot indices for ERC-20-style state-vars (non-mapping
  first, then mapping which skips slot, then more vars).
- Golden fixture refresh if existing state-vars produce non-zero
  SlotIndex (Token.name in synthetic fixture would now carry
  SlotIndex=0).

Estimated ~80 LOC + golden refresh.

### W9 V1+ — packing + inheritance + mapping derivation (future)

- Type-size lookup (`uint8`=1, `uint16`=2, ..., `uint256`=32,
  `address`=20, `bool`=1, fixed-size arrays, packed structs).
- Bit-packing rule: pack sub-32-byte values into same slot when
  consecutive in source.
- Inheritance offset: walk EdgeExtends parents, sum their slot counts.
- Mapping slot derivation: `keccak256(key, slot)` — too complex for
  static graph, may stay as documented-limitation.

---

## §5. 결정 (V0)

| # | 결정 | 선택 | 근거 |
|---|------|------|------|
| W9-D1 | Slot field 위치 | `Node.SlotIndex int` (omitempty) | 새 필드, narrow scope. Signature 재사용은 type text 와 충돌. SubKind 재사용은 W7.2 visibility 와 충돌. |
| W9-D2 | V0 packing | 무시 (uint8 = 1 slot) | full Sol packing 은 V1+. V0 도 non-packed contract (대다수 audit target) 에서 정확. |
| W9-D3 | V0 inheritance | 무시 (per-contract index) | inheritance offset 은 EdgeExtends adjacency 필요 — Pass 2. V0 는 Pass 1 walker 단계에서 끝남. |
| W9-D4 | Mapping skip | V0 미해당 (NodeMapping path 그대로) | mapping slot 은 dynamic, keccak 해야 함. V1+ 별도 spec. |
| W9-D5 | declaration order = slot order | yes | tree-sitter query 의 match 순서가 source order 라는 전제. 다른 walker 들 (W7.3 modifier order) 와 동일 가정. |

---

## §6. Out of scope (V0)

- Storage packing (V1+).
- Inheritance offset (V1+).
- Mapping / dynamic array slot derivation (V1+ or out of scope).
- Inline assembly storage references.
- Diamond storage pattern (`bytes32 constant SLOT = keccak("...")`)
  — those use opaque slot keys, separate analysis surface.
