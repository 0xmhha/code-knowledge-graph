# parse-sol Walker Symmetry Matrix

> Why this exists: the W-C W10 series (V14-V23, 2026-05-19 ~ 2026-05-21)
> exposed *two* cross-walker drifts where one walker received an invariant
> fix and a sibling walker that should have received the same fix didn't.
> Both bugs surfaced *days later* through probe-driven discovery:
>
>  - **V18 → V22**: V18 added a `struct_expression` hop to the *Sol cast
>    walker* (low-level `.call{value:x}(...)` syntax). V19 introduced the
>    parallel *Sol high-level walker* without the hop, so
>    `this.foo{value:x}()` silently lost `HasHighLevelSelfCall` until V22
>    mirrored the hop.
>  - **V10 → V23**: V10 added `isYulSelfAddress` self-receiver detection
>    for Yul `delegatecall(gas(), address(), ...)`. The sibling Yul `call`
>    and `staticcall` shapes carried the *same* receiver but were not
>    handled — until V23 mirrored the detection.
>
> Both gaps would have been caught at design time if we had a *systematic
> view of which markers each walker is expected to set on which input
> shapes*. This file is that view. Use it when adding a walker, fixing a
> walker, or reviewing a PR that touches one.

## Walker inventory

| Walker file | Purpose | Marker(s) it can set |
|---|---|---|
| `external_call_cast.go` (`runExternalCallCastMarker`) | Sol cast-shape low-level self-cast detection | `HasExternalCall`, `HasSelfReentrantCall`, `HasSelfDelegatecallDead` |
| `high_level_self_call.go` (`runHighLevelSelfCallMarker`) | Sol typed dispatch self-call detection | `HasHighLevelSelfCall` |
| `yul_low_level_calls.go` (`runYulLowLevelCalls`) | Yul `call` / `delegatecall` / `staticcall` from inline assembly | `HasLowLevelCall`, `HasSelfReentrantCall`, `HasSelfDelegatecallDead` |
| `chained_external_call.go` | Sol chained `getTarget().call(...)` shape | `HasExternalCall` |
| `assembly_marker.go` | inline-assembly presence + Yul opcode catalogue | `HasAssembly`, `YulBuiltins` |

## Symmetry pairs (invariant equivalence)

The cells below state which (walker, input shape) pairs are expected to
produce *the same marker* on the enclosing callable. A `✓` is a pair that
*has been verified by an explicit lockdown test*; an empty cell is a pair
where the marker is N/A; a `?` is a pair that *needs review* (potential
drift candidate).

### Self-call surface — `HasSelfReentrantCall` and `HasSelfDelegatecallDead`

| Receiver shape | Sol cast walker | Yul low-level walker | High-level walker | Locked by |
|---|---|---|---|---|
| `payable(this).call(...)` | ✓ HasSelfReentrantCall | — | — | V8 / V14-V17 |
| `address(this).call(...)` | ✓ HasSelfReentrantCall | — | — | V8 |
| `payable(this).call{value:x}(...)` | ✓ HasSelfReentrantCall (V18 hop) | — | — | V18 |
| `payable(this).delegatecall(...)` | ✓ HasSelfDelegatecallDead | — | — | V9 |
| `payable(this).transfer(...)` | ✓ HasSelfReentrantCall (V12 method admit) | — | — | V12 |
| `payable(this).send(...)` | ✓ HasSelfReentrantCall | — | — | V12 |
| Yul `call(gas(), address(), …)` | — | ✓ HasSelfReentrantCall | — | V23 |
| Yul `staticcall(gas(), address(), …)` | — | ✓ HasSelfReentrantCall | — | V23 |
| Yul `delegatecall(gas(), address(), …)` | — | ✓ HasSelfDelegatecallDead | — | V10 |
| `this.foo()` | — | — | ✓ HasHighLevelSelfCall | V19 |
| `IFoo(address(this)).foo()` | — | — | ✓ HasHighLevelSelfCall (isSelfRef recursion) | V19 |
| `MyContract(address(this)).foo()` | — | — | ✓ HasHighLevelSelfCall | V19 |
| `this.foo{value:x}()` | — | — | ✓ HasHighLevelSelfCall (V22 hop) | V22 |
| `getTarget(this).foo()` (lower-case helper) | — | — | **✗ intentional false negative** | V21 (negative lock) |

### EdgeOverrides emit — function vs modifier walker symmetry

The W2 function-override walker and the W7.3 modifier-override walker
share `funcByQName` and `resolveOverridesRef`, but emit through two
*separate* AST-walk paths (`emitFunctionDeclWithOverride` vs
`runModifierOverride`). Both walks branch on `len(override.explicitParents)`:
bare `override` → `dispatchKindOverride`, explicit `override(A, B)` →
one `dispatchKindOverrideExplicit` PendingRef per listed parent.

| Override shape | Function walker | Modifier walker | Locked by |
|---|---|---|---|
| Bare `override` (single parent) | ✓ EdgeOverrides | ✓ EdgeOverrides | W2 / W7.3 V0 |
| Explicit `override(A, B)` (diamond) | ✓ one EdgeOverrides per parent | ✓ one EdgeOverrides per parent | W2 / **V25** |

### Enclosing-callable shape — `nearestFunctionQnameAndStart` accepted kinds

A marker that keys on the enclosing callable ID should fire regardless of
which callable shape contains the call site. Verified for the cast walker;
the high-level walker shares the same helper, so the same shape acceptance
propagates transparently.

| Callable shape | Cast walker | High-level walker | Locked by |
|---|---|---|---|
| `function_definition` | ✓ | ✓ (V20 cross-axis) | V14 / V20 |
| `constructor_definition` | ✓ | ✓ (V24 cross-axis) | V15 / V24 |
| `fallback_receive_definition` | ✓ | ✓ (V24 cross-axis) | V14 / V24 |
| `modifier_definition` | ✓ | ✓ (V24 cross-axis) | V17 / V24 |
| try-statement (transparent walk) | ✓ | ✓ (V20) | V16 / V20 |

All five rows are now `✓ / ✓` on both walker columns. `V24` shares a
single fixture (`testdata/yul_receiver/sol_high_level_shape_cross.sol`)
with one contract per remaining shape — constructor / receive / modifier —
and a single test that asserts the high-level marker fires on all three.
The fixture also pins HasSelfReentrantCall=false to keep the high-level
vs low-level axis separation explicit per shape.

## Checklist when adding a new walker

If you are introducing a walker that sets a marker which any of the
walkers above already sets, ask the following before merging:

1. **Member-expression query**: does the walker query the whole tree, or
   does it window itself to a syntactic scope (a function body, a
   `try_statement`, a `modifier_definition`)? Windowing is correct for
   declaration walks (W9 storage layout) and wrong for use-site walks
   (W10 self-call) — V16 / V20 / V17 were free because the cast walker
   queries the whole tree.

2. **`call_expression` / `struct_expression` parent chain**: if the new
   walker matches member expressions, does it traverse `struct_expression`
   between member and call? V18 and V22 are both struct_expression hops;
   missing the hop is the V19-shape false negative.

3. **`isSelfRef` recursion**: if the walker accepts cast-wrapped
   self-references (interface / contract cast around `this`), does it
   recurse through every cast shape isSelfRef understands? Adding a new
   cast shape (e.g. UDVT cast) means updating isSelfRef once for *both*
   the cast walker's `isSelfCast` and the high-level walker's `isSelfRef`.

4. **Yul / Sol pair**: if the walker handles a Sol shape that has a Yul
   counterpart (low-level call, value transfer, address arithmetic), is
   there a corresponding `yul_*.go` walker that handles the inline-
   assembly version? The Sol-Yul drift is the V10/V23 pattern.

5. **`nearestFunctionQnameAndStart`**: if the walker keys its marker on
   the enclosing callable's ID, does it use the shared helper rather than
   re-implementing the parent walk? Bypassing the helper recreates the
   four-shape lockdown work that V14-V17 already did.

6. **Lockdown test placement**: every new (walker, marker) pair needs a
   `*_test.go` file under `internal/parse/solidity/`. The fixture goes
   under `testdata/<topic>/`. The test must assert both the positive
   case AND that *unrelated* markers (the axis siblings) stay false.

## Historical drift catalogue

Keep this list short and honest — these are the drifts that the W-C
series actually surfaced, not theoretical concerns.

| # | Drift | Walker A | Walker B | Symmetric fix |
|---|---|---|---|---|
| 1 | struct_expression hop | cast (V18) | high-level (V22) | Both walkers now traverse the same wrapper |
| 2 | self-receiver detection | Yul delegatecall (V10) | Yul call / staticcall (V23) | All three opcodes share `isYulSelfAddress` |
| 3 | callable shape coverage gap | cast (V14/V15/V17 — 4 shapes) | high-level (V19 — only function body) | V24 added the constructor / receive / modifier cross-axis fixture so all four shapes are now locked on both walker columns |
| 4 | explicit-list override branch coverage | function (W2 — tested) | modifier (`runModifierOverride.dispatchKindOverrideExplicit` — shipped without a lockdown test) | V25 added `testdata/modifier_composition/explicit_override.sol` with a diamond `override(A, B)` parent list; the test asserts both EdgeOverrides edges and that src/dst are NodeModifier |
| 5 | non-bare receiver shapes for fn-pointer call detection (known limitation family) | V6 `matchStateVarMethodCall` — bare-identifier receivers only; V9 — bare-identifier return expressions only | (a) `s.getCb()(x)` chained-invoke and `return s.getCb();` chained-fetch — V26; (b) `Hub(addr).onAction(x)` cast-receiver and `getHub().onAction(x)` helper-return-receiver — V27 | V26 + V27 negative-lock both fixtures (`chained_cross_contract.sol`, `cast_and_helper_receiver.sol`). V27 also pins the V6 positive baseline (`h.onAction(x)` → `HasFunctionPointerCall=true`) inside the same fixture so a future regression on the bare path is diagnosed as "general regression" rather than "negative locks unexpectedly flipped." All four cells across V26/V27 share the same root cause and must move together when the limitation gets resolved. Mirrors V21's negative-lock pattern for `HasHighLevelSelfCall` |
| 6 | struct-internal reference-type slot count + multi-contract struct sizing (characterization) | V5 `tryComputeStructBytes` — only primitives / fixed arrays / known structs resolve; mapping / dynamic bytes / dynamic string / dynamic `T[]` members hit `(0, false)` fallthrough; multi-contract files with same-named structs land on 1-slot state-var fallback | V28 characterizes the full 4×3 grid in `testdata/storage_slot_packing/struct_internals_unresolvable.sol`: PrimitiveOnly / WithMap / WithBytes / WithDynArray each produce `head=0, inner=1, tail=2` instead of V5's stated intent of `tail=4`. **Two divergences entangled**: (i) V5 comment intent vs walker behaviour on reference-type members; (ii) all-primitive struct sizes cleanly in isolation (V11 fixture) but falls back here — root cause unidentified at V28 time. A walker fix must address both or the cross-flip protocol identifies the partial fix |

If you fix a new drift, append a row. The catalogue itself is the lesson —
*reading it before adding a walker is how the next drift gets prevented*.
