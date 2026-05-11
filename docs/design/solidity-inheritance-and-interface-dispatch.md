# Solidity Inheritance + Interface Dispatch — Design Spec

> Scope: extend the Solidity parser (`internal/parse/solidity/`) so the graph
> captures (a) contract / interface inheritance via the `is`-clause,
> (b) `super` calls + `virtual` / `override` modifier semantics,
> (c) interface-typed dynamic dispatch (`IERC20(addr).transfer(...)`) — the
> single most common pattern in real-world Solidity that the current parser
> cannot model.
>
> **Status**: design draft 2026-05-11. No code changes.
> **Out of scope**: cross-contract security analysis (reentrancy, access
> control — that's senior-secops territory), assembly blocks, EVM-level
> opcodes, low-level `call` / `delegatecall` / `staticcall` (separate spec).
> **Adjacent docs**: `docs/design/track-c-detector-gap.md` §2.4 (Sol `extends`
> already flagged P2 — "no query for is-clause"), `docs/design/schema-1.9-spec.md`
> (cross-language — Sol↔TS already partially covered via `binds_to`).

---

## §0. Cold start

- **무엇**: Sol 파서가 (1) `contract Child is Parent` 의 상속 관계,
  (2) `interface IFoo` ↔ `contract Impl is IFoo` 의 구현 관계,
  (3) `IFoo(addr).bar()` 형태의 interface 기반 dynamic dispatch — 셋 다
  graph 로 표현하지 못한다.
- **왜**: Solidity 의 90% 이상은 OpenZeppelin / 자체 base contract 상속을
  사용하며, ERC-20/721/4626 같은 표준은 interface 매칭이 호출 모델의 핵심.
  현재 graph 는 "이 컨트랙트가 어떤 표준을 implement 하는가", "ERC20 호출이
  실제로 어느 Token 컨트랙트로 라우팅되는가" 같은 1차 질의에 답을 못함.
- **어떻게**:
  - (A) 새 노드 type 없음. 기존 `EdgeExtends` 활용 (`contract is`),
    `EdgeImplements` 활용 (`contract is interface`).
  - (B) `super.foo()` / `virtual` / `override` 를 위한 새 엣지 `overrides`
    (Method → Method) 추가.
  - (C) `IFoo(addr).bar()` 패턴 → `EdgeInvokes` (Method → Method) — 단
    target 은 abstract (interface method) 이고 실제 dispatch 는 runtime.
    AMBIGUOUS confidence 로 분류.
- **선행**: `track-c-detector-gap.md` §2.3 의 `invokes` semantic split (Go-기준
  P1) — Sol 에서도 이 엣지 타입이 필요. Go 측에서 구현되면 Sol 도 동일 idiom
  사용 가능.

---

## §1. 현재 상태

### 1.1 Sol 파서가 *capture 하는* tree-sitter 노드

`internal/parse/solidity/queries.go` 전체:

| Query | 매칭 |
|-------|------|
| `contract_declaration` | Contract 노드 |
| `function_definition` | Function 노드 |
| `modifier_definition` | Modifier 노드 |
| `event_definition` | Event 노드 |
| `struct_declaration` | Struct 노드 |
| `enum_declaration` | Enum 노드 |
| `state_variable_declaration` | Variable / Mapping 노드 |
| `emit_statement` | `emits_event` 엣지 |
| `modifier_invocation` | `has_modifier` 엣지 |

### 1.2 *capture 안 하는* 것 — Sol grammar 가 노출하는데 미사용

`internal/parse/solidity/binding/parser.c` 의 symbol 테이블 확인 결과:

| Tree-sitter symbol | 의미 | 현재 |
|---|---|---|
| `sym_interface_declaration` (id 348) | `interface IFoo { ... }` | ❌ — 파서가 Contract 로 잘못 분류 (실제론 별개 노드) |
| `sym_inheritance_specifier` (id 351) | `is X, Y` 클로즈 | ❌ |
| `sym_virtual` (id 162) | `function foo() virtual` | ❌ (signature 에 누락) |
| `anon_sym_override` (id 157) | `function foo() override` | ❌ |
| `sym_emit_statement` (id 402) | (이미 capture) | ✅ |

### 1.3 *capture 안 되는 패턴* — Sol idiom

| 패턴 | 의미 | graph 표현 (현재 / 목표) |
|---|---|---|
| `contract A is B, C` | 다중 상속 | 없음 / `A extends B`, `A extends C` |
| `contract A is IFoo` | interface 구현 | 없음 / `A implements IFoo` |
| `super.foo()` | parent 구현 호출 | 없음 / `f calls/invokes parent.foo` (override 체인) |
| `function foo() virtual override` | dispatch entry point | 없음 / `child.foo overrides parent.foo` |
| `IERC20(token).transfer(to, amount)` | interface dispatch | 없음 / `f invokes IERC20.transfer` (AMBIGUOUS) |
| `using SafeMath for uint` | trait-like extension | 없음 / 별도 spec |
| `abstract contract` | 부분 구현 | 없음 / Contract `SubKind: "abstract"` |
| `library` | static helper | Contract 로 분류 (구분 안 됨) / `NodeContract` 의 `SubKind: "library"` |

### 1.4 영향

- ERC-20 토큰 호출이 어느 토큰 컨트랙트로 라우팅되는지 graph traversal 불가.
- 상속 체인 안 의 `super.foo()` 가 어느 parent 구현을 가리키는지 불명.
- Diamond 패턴 / proxy 패턴 분석 완전 불가 (별도 spec 영역, 본 spec 의
  out-of-scope).
- "이 컨트랙트가 ERC-721 표준을 구현하는가?" 같은 1차 질의에 답 못함.

### 1.5 track-c-detector-gap.md 와의 관계

| 항목 | track-c (P2) | 본 spec |
|------|--------------|---------|
| Sol `extends` (is-clause) | 진단 ("no query for is-clause") | 구현 plan + 다중 상속 처리 |
| Sol `implements` | 진단 없음 | **신규** plan |
| Sol `super` / virtual / override | 언급 없음 | **신규** plan |
| Sol interface dispatch | 언급 없음 | **신규** plan (가장 큰 가치) |

---

## §2. 목표 동작

### 2.1 새 노드 / 엣지

| 항목 | 종류 | 설명 |
|------|------|------|
| `NodeContract.SubKind` 추가 값 | (기존 컬럼) | "contract" / "interface" / "abstract" / "library" |
| `NodeFunction.SubKind` 추가 값 | (기존) | "function" / "virtual" / "override" / "virtual_override" |
| `EdgeExtends` (기존) | Edge | `contract X is Y` (Y 가 contract) |
| `EdgeImplements` (기존) | Edge | `contract X is I` (I 가 interface) |
| `EdgeInvokes` (기존, 활성화) | Edge | `IFoo(addr).bar()` |
| `EdgeOverrides` (신규) | Edge | child.method → parent.method (`override` 키워드) |

신규 엣지 1종 (`overrides`), 신규 NodeType 없음.

### 2.2 신뢰도 정책

- `extends` (`is Parent`, Parent 가 contract): EXTRACTED — solc 가 강제하는
  syntax.
- `implements` (`is IFoo`, IFoo 가 interface): EXTRACTED — `is` 클로즈에서
  identifier 가 interface 임을 같은 빌드 안에서 resolve 가능하면 EXTRACTED,
  unresolved 면 INFERRED (PendingRef → drop / AMBIGUOUS).
- `overrides`: EXTRACTED — `override` 키워드 명시.
- `invokes` (interface dispatch `IFoo(addr).bar`): **AMBIGUOUS** —
  실제 dispatch 는 runtime address. graph 는 interface method 만 알 수
  있음. LLM 노출 시 hunk-graph §11.3 wrapper 패턴 동일 적용 — `Recovery` /
  `Dispatch Possibilities` 같은 사람 surface 에서만 노출.
- `extends` + `implements` 가 같은 클래스에 다중 적용 (Sol 다중 상속) →
  각각 별개 엣지.

### 2.3 schema 영향

- 신규 엣지 1종 → `pkg/types/enums.go` 의 `AllEdgeTypes()` append.
- 신규 NodeType 없음 → `AllNodeTypes()` 변경 없음.
- 기존 `SubKind` 컬럼 활용 — 마이그레이션 없음.
- bump: schema 1.8 → 1.10 (1.9 cross-language 와 동시 진행 시 1.11 가능).
  TS spec 과 bump 통합 가능성은 §5.Q8.

---

## §3. 검출 알고리즘

### 3.1 (A) Inheritance — `is`-clause

`queries.go` 에 추가:

```scheme
; contract X is A, B { ... }
(contract_declaration
  name: (identifier) @contract_name
  (inheritance_specifier (user_defined_type (identifier) @parent_name))) @decl

; interface I is J { ... }
(interface_declaration
  name: (identifier) @iface_name
  (inheritance_specifier (user_defined_type (identifier) @parent_name))) @decl
```

(정확한 grammar field name 은 `JoranHonig/tree-sitter-solidity` 의
`node-types.json` 확인 필수 — `inheritance_specifier` 의 자식 구조가
grammar 버전 dependency.)

declarations.go 처리:
1. parent_name 을 resolve — 같은 파일/패키지의 노드라면 즉시 ID 매핑,
   아니면 PendingRef.
2. parent 노드 type 확인 — `Interface` 면 `EdgeImplements`,
   `Contract` 면 `EdgeExtends`.

**다중 상속**: `inheritance_specifier` 가 여러 parent 를 가질 수 있음 —
loop 으로 각각 emit.

**Linearization (C3)**: Solidity 0.6+ 는 C3 linearization 강제 — graph
표현은 직접 부모만 edge 로 두고, transitive 는 traversal 시 계산. C3 순서
별도 보존 필요 시 `Signature` 필드에 stash (`"extends: [A, B, C]"`).

### 3.2 (B) Interface declaration 분리

현재 `queryContract = (contract_declaration ...)` 만 있고 `interface_declaration`
은 별개 grammar 노드 (id 348). 새 query 추가:

```scheme
(interface_declaration name: (identifier) @name) @decl
```

emit 시:
- `NodeType: Interface` (또는 `Contract` + SubKind="interface" — §5.Q2 결정)
- `SubKind: "interface"`

### 3.3 (C) Virtual / Override / Super

`function_definition` 의 자식 노드에서 `virtual` / `override` 키워드 확인.
tree-sitter 가 modifier list 안에 노출.

```scheme
(function_definition
  name: (identifier) @fn_name
  (virtual)? @virtual_marker
  (override_specifier)? @override_marker) @decl
```

declarations.go:
- `virtual` 발견 → Function.SubKind = "virtual"
- `override` 발견 → Function.SubKind = "override"
  (양쪽 다이면 "virtual_override")
- `override(A, B)` 같은 명시적 parent 지정 시 PendingRef 로 `overrides`
  엣지 emit. 명시 없으면 같은 이름의 parent function 을 resolve.go 에서
  매핑.

`super.foo()` 패턴은 body walk 에서:
```
on `member_expression { object: identifier("super"), property: X }`:
  enclosing function = current function (FN)
  PendingRef{Src: FN.id, EdgeType: EdgeCalls, TargetQName:
    parent_contract.X}
```
이후 resolve.go 에서 inheritance chain 따라 가장 가까운 X 정의 매핑.

### 3.4 (D) Interface dispatch — `IFoo(addr).bar()`

가장 어려운 패턴. body walk 에서:

```
on `call_expression { function: member_expression {
       object: call_expression { function: identifier(X), arguments: [_] },
       property: Y } }`:
  if X resolves to an Interface node:
    emit PendingRef{
      Src: enclosing_fn.id,
      EdgeType: EdgeInvokes,
      TargetQName: X.Y,
      Confidence: AMBIGUOUS
    }
```

resolve.go 에서 `X.Y` 는 Interface 의 Method 노드와 매핑. 매핑 성공 →
AMBIGUOUS edge emit. 실패 → drop.

**왜 AMBIGUOUS**: address 가 가리키는 실제 컨트랙트는 runtime 결정. graph
가 보여줄 수 있는 것은 "이 함수가 IFoo 인터페이스의 bar 를 호출하는데, 실제
구현체 후보는 `EdgeImplements` 로 IFoo 를 implement 하는 모든 컨트랙트의
bar 메서드들". 이 fan-out 은 viewer / Recovery 패널에서 별도 query 로 제공:

```sql
-- "이 invokes edge 의 가능한 dispatch target"
WITH iface AS (SELECT dst FROM edges WHERE id = ?invokes_edge_id)
SELECT n.qualified_name
FROM edges e JOIN nodes n ON n.id = e.src
WHERE e.type = 'implements' AND e.dst = (
  SELECT contract_id_of_method FROM nodes WHERE id = (SELECT dst FROM iface)
);
```

### 3.5 noise control

- Library call (`using SafeMath for uint; a.add(b)`): library 의 method 가
  receiver type 의 method 처럼 dispatch. V0 에서는 단순 `calls` (resolve
  실패 → drop) 으로 처리. 별도 spec.
- Modifier dispatch (이미 `has_modifier` 로 capture 됨): 신규 작업 불필요.
- Abstract contract 의 abstract method: function body 가 비어있음. function
  노드는 emit, `calls` edge 는 0 — 자연스럽게 처리됨.

---

## §4. 구현 계획

### 4.1 W1 — Inheritance + Interface declaration (가장 작음)

1. `queries.go` 에 `queryInterface` + `queryInheritance` 추가
2. `declarations.go` 에 interface visitor 분기
3. inheritance specifier 처리 (PendingRef 라우팅)
4. parent type 분류 (Contract vs Interface) → `extends` vs `implements`
5. 단위 테스트 fixture:
   - `testdata/inheritance/single.sol` — 단순 단일 상속
   - `testdata/inheritance/multiple.sol` — 다중 상속 (C3)
   - `testdata/inheritance/iface_impl.sol` — interface 구현
   - `testdata/inheritance/diamond.sol` — diamond 상속

추정 사이즈: 250~350 LOC + 4 fixture.

### 4.2 W2 — Virtual / Override / Super

1. function definition 시 virtual/override modifier 캡처 → SubKind
2. `EdgeOverrides` enum 추가 → `pkg/types/enums.go` append
3. `super.foo()` body walk
4. resolve.go 에 inheritance-aware lookup 추가
5. 단위 테스트:
   - `testdata/override/basic.sol` — single override
   - `testdata/override/super_call.sol` — super 호출
   - `testdata/override/explicit_override.sol` — `override(A, B)`

추정 사이즈: 200~300 LOC + 3 fixture. enums.go 변경.

### 4.3 W3 — Interface dispatch

1. body walk 에서 `IFoo(addr).bar()` 패턴 인식
2. AMBIGUOUS PendingRef emit
3. resolve.go 에서 Interface.Method 매핑
4. `llmSafeStoreReader` wrapper (hunk-graph §11.3 패턴) 가 AMBIGUOUS invokes
   를 LLM 으로부터 차단하는지 회귀 (이미 wrapper 가 일반 AMBIGUOUS 차단
   하므로 자동)
5. viewer 의 "Possible Dispatch Targets" 패널 (선택, 별도 PR)
6. 단위 테스트:
   - `testdata/dispatch/erc20.sol` — IERC20 호출
   - `testdata/dispatch/multi_impl.sol` — 여러 impl 후보 fan-out 확인

추정 사이즈: 300~400 LOC + 2 fixture + (선택) viewer.

### 4.4 W4 — abstract / library SubKind

1. `abstract contract` → SubKind="abstract"
2. `library` → SubKind="library"
3. tree-sitter modifier 확인
4. 단위 테스트

추정 사이즈: 50~100 LOC + 2 fixture. (가장 단순, 첫 작업으로 wrap-up
가능)

### 4.5 W5 — 측정 + handoff

OpenZeppelin / Aave / Uniswap 등 실세계 컨트랙트 빌드해서 KPI 측정:

```bash
./bin/ckg build --src=<openzeppelin-contracts> --out=/tmp/ckg-sol-oz
sqlite3 /tmp/ckg-sol-oz/graph.db "
  SELECT type, COUNT(*) FROM edges
  WHERE type IN ('extends','implements','overrides','invokes')
  GROUP BY type;
"
```

---

## §5. 결정 필요 항목

> **STATUS — 2026-05-11**: 10개 항목 모두 합의 완료. 결정 요약은 §5.0
> 참조. 각 Q 의 옵션·trade-off 원본은 §5.Q1 이하 read-only 보존
> (Why 문서화 목적 — 결정 재고 시 출발점).

### §5.0. 결정 결과 (2026-05-11)

| Q | 결정 | 권고 일치? | 비고 |
|---|------|-----------|------|
| Q1 | 기존 NodeInterface 재사용 (Go/TS 와 동일 idiom) | ✅ | spec 작성 시 "신규"라 잘못 표기 — 실제론 `pkg/types/enums.go:13` 이미 존재 |
| Q2 | NodeContract + SubKind="library" | ✅ | — |
| Q3 | C3 linearization 순서를 Signature 필드에 stash | ✅ | 향후 별도 컬럼 승격 가능 |
| Q4 | `overrides` 방향: child.method → parent.method | ✅ | — |
| Q5 | Interface dispatch = AMBIGUOUS | ✅ **+강화** | 사용자 인사이트: 소스에 impl 유무 무관 — 외부 배포 컨트랙트 케이스 (impl 없음) 도 AMBIGUOUS 가 *오히려 더* 적절 |
| Q6 | fallback/receive = Function + SubKind | ✅ | — |
| Q7 | Constructor chain = 일반 `calls` 엣지 | ✅ | — |
| Q8 | TS + Sol 합쳐 schema 1.10 bump | ✅ | enums.go 단일 수정 PR |
| Q9 | `using For` 본 spec 에 포함 | ❌ **divergent** | 권고는 (a) 별도 spec. 사용자 결정으로 사이즈 +200~300 LOC, resolve.go 에 contract-scoped library 매핑 추가 → **W6 신설** (§4 참조) |
| Q10 | Diamond/Proxy 명시적 out-of-scope | ✅ | — |

**구현 영향 요약**:
- 신규 NodeType 0종 (NodeInterface 재사용)
- 신규 EdgeType 1종 (`overrides`) + Q9 으로 `using For` 처리 — 새 엣지 도입 여부는 W6 설계 시 결정
- 신규 SubKind 값: Contract = {"contract","interface","abstract","library"}, Function = {"function","virtual","override","virtual_override","fallback","receive"}
- W 단계: W1~W5 (기존 §4) + **W6 (using For)** 추가
- schema 1.10 bump 의 절반 (나머지 절반은 TS spec)

원본 옵션 비교는 §5.Q1 이하 블록 참조.

---

### Q1. interface 를 `NodeContract` (with SubKind) 로 둘 것인가, 새 `NodeInterface` 로?

- (a) **새 NodeType `Interface`** — TS Interface 와 동일 idiom, surface
  단순
- (b) `NodeContract` + SubKind="interface" — schema 변경 없음, viewer
  filter 가 SubKind 까지 알아야 함
- (c) `NodeContract` + SubKind + 가상의 SubType — overkill

**권고**: (a). schema bump 자명, TS/Sol 양쪽 surface 일관성. (단 schema
1.10 bump 의 비용 검토 필요.)

### Q2. `library` 도 별도 NodeType?

- (a) NodeContract + SubKind="library"
- (b) 새 NodeType `Library`

**권고**: (a). library 는 syntactic 변종, Contract 노드의 컬럼만으로 충분.

### Q3. 다중 상속의 C3 linearization 순서 보존

- (a) `Signature` 필드에 `"extends: [A, B, C]"` stash
- (b) 새 컬럼 `linearization_order` — 마이그레이션
- (c) 보존 안 함 — `extends` 엣지의 순서 무관

**권고**: (a). 정보 보존 + 마이그레이션 없음. 향후 (b) 로 승격 가능.

### Q4. `overrides` 엣지의 방향

- (a) **child.method → parent.method** ("child overrides parent")
- (b) parent.method → child.method (역방향)
- (c) bidirectional 두 엣지

**권고**: (a). "이 메서드는 무엇을 override 하는가" 가 자주 묻는 방향.
역방향은 graph traversal 한 hop 으로 가능.

### Q5. interface dispatch 의 confidence

§2.2 에 AMBIGUOUS 권고했으나 사용 시나리오에 따라:

- (a) **AMBIGUOUS** — LLM 차단, 사람만 봄
- (b) INFERRED — LLM 도 봄, "후보 중 하나" 신호
- (c) EXTRACTED 로 emit + `dispatch_target_set` 별도 메타 — 정보량 ↑

**권고**: (a). 잘못된 dispatch target 하나만 봐도 LLM 이 분석 오도 가능
— hunk-graph §11.3 의 unreachable 패턴과 같은 리스크.

### Q6. fallback / receive 함수

Solidity 의 `fallback()` / `receive()` 도 Function 노드?

- (a) 그렇다 — name="fallback"/"receive", SubKind="fallback"/"receive"
- (b) 별도 NodeType — overkill

**권고**: (a). 자명.

### Q7. constructor / 다중 constructor 호출 (`A(x) B(y)`)

`constructor() A(arg1) B(arg2)` 패턴의 parent constructor 호출.

- (a) `calls` 엣지로 — Constructor → Parent.Constructor
- (b) 별도 `init_chain` 엣지 — schema bump
- (c) 무시

**권고**: (a). 일반 calls 와 같은 의미, idiom 일관.

### Q8. schema bump 합병 여부

본 spec + TS spec 모두 schema bump 필요. 같은 bump (1.10) 에 합칠지 분리할지.

- (a) **합쳐서 1.10 bump** — release 한 번, validator 회귀 한 번
- (b) 분리 (Sol = 1.10, TS = 1.11) — 작업 진행 순서 dependency
- (c) cross-language schema 1.9 와 함께 합쳐 1.9 (모든 작업이 한 schema)

**권고**: (a). TS 와 Sol 작업이 병렬 진행 가능하다면 같은 bump 가 자연.
schema 1.9 cross-language 와는 분리 (다른 dimension).

### Q9. `using For` (library extension) 처리 시점

V0 에서는 무시 권고했으나 실세계 코드의 30% 이상이 OpenZeppelin SafeMath
류 사용. 미처리 시 `add`/`sub` 같은 method call 이 unresolved 됨.

- (a) **V0 무시** (본 spec 범위 외)
- (b) 별도 `using_for` 엣지 추가 — schema bump 일부 사용
- (c) 본 spec 에 포함 — 사이즈 ↑

**권고**: (a). 별도 spec `solidity-using-for.md` 후속.

### Q10. Diamond / Proxy 패턴

OpenZeppelin proxy, EIP-2535 diamond. `delegatecall` 기반 dispatch 는 본 spec
완전히 out-of-scope.

- (a) **명시적 out-of-scope** — 본 spec 의 §0 에 명시 (이미 됨)
- (b) Phase 5 로 포함

**권고**: (a). 별도 spec (`solidity-proxy-delegatecall.md`).

---

## §6. 테스트 전략

### 6.1 fixture (inheritance)

```solidity
// testdata/inheritance/erc20_like.sol
interface IERC20 {
  function transfer(address to, uint256 amount) external returns (bool);
  function balanceOf(address owner) external view returns (uint256);
}

abstract contract ERC20Base is IERC20 {
  mapping(address => uint256) internal _balances;
  function balanceOf(address owner) public view virtual override returns (uint256) {
    return _balances[owner];
  }
  function transfer(address to, uint256 amount) public virtual override returns (bool);
}

contract MyToken is ERC20Base {
  function transfer(address to, uint256 amount) public override returns (bool) {
    require(_balances[msg.sender] >= amount, "insufficient");
    _balances[msg.sender] -= amount;
    _balances[to] += amount;
    return true;
  }
}
```

기대:
- `MyToken extends ERC20Base` (EXTRACTED)
- `ERC20Base extends IERC20` → 실제는 `implements` (IERC20 는 interface)
- `MyToken.transfer overrides ERC20Base.transfer` (EXTRACTED)
- `ERC20Base.balanceOf overrides IERC20.balanceOf`
- ERC20Base SubKind = "abstract"
- IERC20 NodeType = `Interface` (Q1 결정 후)

### 6.2 fixture (dispatch)

```solidity
// testdata/dispatch/vault.sol
import "./erc20_like.sol";

contract Vault {
  function deposit(IERC20 token, uint256 amount) external {
    token.transfer(address(this), amount);   // dispatch
  }
}
```

기대:
- `Vault.deposit invokes IERC20.transfer` (AMBIGUOUS)
- traversal: `IERC20.transfer ← implements MyToken.transfer` 으로 후보 fan-out
  가능

### 6.3 회귀

기존 `golden_test.go` 의 노드/엣지 카운트가 새 detectors 활성화 후 어떻게
변하는지 golden 갱신. 기존 emit 사라짐 없음 확인 (append-only 원칙).

### 6.4 self-graph (testdata/synthetic)

`internal/parse/solidity/testdata/synthetic/` 의 Vault fixture 기준 KPI
diff 측정.

### 6.5 실세계 corpus

OpenZeppelin contracts repo 빌드해서 sample query:
```sql
SELECT n.qualified_name, COUNT(e.id) AS dispatch_count
FROM nodes n
JOIN edges e ON e.src = n.id
WHERE n.type IN ('Function','Method') AND e.type = 'invokes'
GROUP BY n.id
ORDER BY dispatch_count DESC LIMIT 20;
```

---

## §7. 참조

- 현재 Sol 파서:
  - `internal/parse/solidity/parser.go` (entry)
  - `internal/parse/solidity/declarations.go` (visitor)
  - `internal/parse/solidity/queries.go` (현재 queries)
  - `internal/parse/solidity/resolve.go` (Pass 2)
  - `internal/parse/solidity/binding/parser.c` (grammar — symbol id 참조)
- track-c 갭 진단: `docs/design/track-c-detector-gap.md` §2.4 (Sol extends)
- Cross-language link 기존: `internal/link/xlang.go` (Sol↔TS `binds_to`)
- Go 의 implements 참고 구현: `internal/parse/golang/implements.go`
- Sol grammar: `JoranHonig/tree-sitter-solidity` (vendored v1.2.11)

---

## §8. 작업 순서

1. **§5 결정 항목 10개에 사용자 답변 받기** (Q1 NodeType 결정이 schema
   bump 좌우)
2. W4 — `abstract` / `library` SubKind (가장 단순, warm-up)
3. W1 — Inheritance + Interface declaration
4. W2 — Virtual / Override / Super
5. W3 — Interface dispatch (가장 가치 있고 가장 어려움)
6. W5 — 측정 + handoff

W1 후 W2/W3 는 의존. W4 는 다른 W 들과 독립.
