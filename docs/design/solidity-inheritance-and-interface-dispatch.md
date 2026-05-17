# Solidity Inheritance + Interface Dispatch — Design Spec

> Scope: extend the Solidity parser (`internal/parse/solidity/`) so the graph
> captures (a) contract / interface inheritance via the `is`-clause,
> (b) `super` calls + `virtual` / `override` modifier semantics,
> (c) interface-typed dynamic dispatch (`IERC20(addr).transfer(...)`) — the
> single most common pattern in real-world Solidity that the current parser
> cannot model.
>
> **Status**: design draft 2026-05-11. W4 (abstract/library SubKind)
> ✅ landed 2026-05-11 (commit `f7a8515`). Schema 1.10 slot for
> `EdgeOverrides` reserved 2026-05-11 (appended to `pkg/types/enums.go`;
> see `docs/DISPATCH-WITHIN-LANG-SEMANTICS.md` §2 Phase 4 Status block).
> W1 / W2 / W3 ✅ LANDED 2026-05-11. W6 V0 (using For binding) ✅
> **LANDED 2026-05-12** — EdgeUsesFor (Contract → Library) first-class
> emit. W6 V1.0~V1.13 (14-tier dispatch resolution) ✅ **LANDED
> 2026-05-12** — state-var (V1.0) / parameter (V1.1) / inherited (V1.2)
> / return chain (V1.3) / cross-contract (V1.4) / depth-2 (V1.5) /
> deep cross-contract (V1.6) / depth-3 (V1.7) / generic walker (V1.8)
> / this-receiver (V1.9) / struct-field (V1.10) / nested struct field
> (V1.11) / generic member-chain walker arbitrary depth (V1.12) /
> this-prefixed nested member chain (V1.13). V1.14 ✅ cross-file
> struct validation — V1.10/V1.13 fixtures across file boundary,
> ConfInferred 검증. V1.15 single-return local-var receiver —
> `Type x = expr; x.method(...)` 패턴, localVarTypes 인덱스 신규,
> V1.0/V1.10/V1.11/V1.12 receiver lookup 에 state-var → param →
> local-var 3-tier fallback (lookupReceiverType helper). V1.16 ✅
> **multi-return tuple destructuring (LHS explicit types)** —
> `(Ta a, Tb b) = foo(); a.method(...)` 패턴, variable_declaration_tuple
> 의 typed slot 각각이 V1.15 localVarTypes 에 emit (V1.15 인프라 재사용).
> file-level using directive (0.8.13+) + free-function form
> (`using {f1, f2} for T`) 둘 다 tree-sitter-solidity v1.2.13 grammar
> 한계로 ERROR-node parse — V1.x grammar 업그레이드 대기.
> V1.17 ✅ **Solidity shadowing precedence fix** — `lookupReceiverType`
> 순서가 state-var → param → local-var 였으나 Solidity 의미상 inner
> scope 가 outer 를 shadow 함 → local-var → param → state-var 로
> 수정. local 이 state-var 를 shadow 하는 false-negative 사례 제거.
> V1.18 cross-file tuple validation — V1.14 idiom 의 V1.16
> 적용. multi-file fixture (struct + library / V1.16 tuple caller)
> 로 ConfInferred 정상 동작 확인.
> V1.19 named return parameters → paramTypes —
> `function f() returns (uint256 result)` 같은 패턴의 named return
> slot 이 미캡처되어 receiver `result.method()` dispatch drop 됐던
> false-negative 수정.
> V1.20 additional scope captures — (a) for-loop init variable
> regression guard. (b) try/catch returns clause named-param false-
> negative 수정 (collectLocalVarMetaPending 에 try_statement 분기 추가
> + emitTryReturnsBinding helper).
> V1.21 catch_clause named parameter — `catch Type(Ta a) { ... }` 의
> catch parameter 가 V1.20 try_statement 분기 밖이라 미캡처 → false-
> negative 수정. collectLocalVarMetaPending 에 catch_clause 분기 추가,
> V1.20 의 emitTryReturnsBinding helper 재사용.
> V1.22 modifier body scope captures —
> modifier_definition 의 parameter/local 이 V1.1/V1.15 emit 에서 미캡처
> (runFunctionDecl 만 호출됨) → modifier body 내 receiver dispatch drop.
> runModifierMeta walker 신규 + NodeModifier qname 에 Container 접두사
> 추가 + Pass 1.5 containerIDByFuncID 에 NodeModifier 포함 +
> nearestFunctionQnameAndStart 가 modifier_definition 도 인식.
> V1.23 constructor_definition graph node + meta walker —
> queryConstructor + runConstructorDecl 신규. NodeFunction with
> SubKind="constructor", synthetic qname "Container.constructor".
> V1.22 idiom 그대로.
> V1.24 ✅ **fallback_receive_definition graph node + meta walker** —
> tree-sitter v1.2.13 가 fallback() 과 receive() 를 동일 노드 kind
> (fallback_receive_definition) 으로 lump. queryFallbackReceive +
> runFallbackReceiveDecl 신규 — source text 첫 토큰 ("fallback" /
> "receive") 으로 disambiguate, synthetic qname "Container.fallback" /
> "Container.receive", SubKind 동일. V1.22 meta idiom + nearestFunction
> QnameAndStart 확장.
> V1.25 free function regression guard — Sol 0.7.4+ file-level
> function_definition. phantom EdgeCalls 미생성 확정.
> V1.26 abstract contract body scope regression guard — abstract 가
> contract_declaration AST kind 와 동일. V1.x using-for indexing 이
> abstract 도 자동 cover 함을 fixture 로 잠금.
> V1.27 inherited modifier `using` regression guard — V1.0 + V1.2 +
> V1.22 3-way intersection guard.
> V1.28 aliased import resolution —
> `import {SafeMath as SM} from "./util.sol"; using SM for uint256;`
> 패턴의 false-negative 수정. declVisitor 에 per-file importAliases
> map + runImportAliases walker + runUsingFor 에서 alias substitute.
> V1.29 whole-file alias qualified using — namespaceAliases set +
> leading namespace identifier skip + collision false-positive 차단.
> V1.30 block-scoped shadowing (V0 first-decl-wins) — outer-decl 보존.
> V2.0 line-range scope-aware localVar lookup — V1.30 V0 trade-off
> 해소, narrowest-scope-wins.
> V2.1 interface receiver via using-for guard + multi-binding known
> limitation lock.
> V2.2 multi-binding for same type — bindings 가 multi-value 로 확장,
> resolveBindingLib helper 도입 — V2.1 의 known limitation 해소.
> V2.3 library body using-for regression guard — V0 query 의 3-container
> uniform matching robustness 검증.
> V2.4 cross-file multi-binding regression guard — V2.2 multi-binding
> cross-file 동작 확인.
> V2.5 operator-form using directive limitation lock — Sol 0.8.19+
> `using {f as +} for T global;` (file-level operator-form) 가 0
> EdgeUsesFor 발산 lock.
> V2.6 ✅ **free-function form rediscovery (contract scope)** —
> V2.5 의 limitation 이 사실 file-level scope 제약 때문이었음을
> empirical 검증. Sol 0.8.13+ `using {Math.add, Math.sub} for
> uint256;` (contract-scoped free-function form) 은 V0 queryUsingFor
> 가 incidental capture: type_alias / using_alias 노드 구분과 무관하게
> `(using_directive ... (identifier) @lib)` 패턴이 `Math` 식별자를
> 매칭. V0 의 "Math.add brace shape ERROR" 가정이 v1.2.13 에서는
> 부분 false. V1.0+ dispatch chain 도 정상 통과 (Vault.run →
> Math.add EdgeCalls).
> V2.7 ✅ **contract-scope operator-form limitation lock** —
> V2.6 mirror question 검증. Sol 0.8.19+ `contract Calc {
> using {Math.add as +} for uint256; }` (contract scope + operator
> form) 은 V2.6 와 달리 0 EdgeUsesFor 발산. `as +` 가 alias-entry
> 에 `user_definable_operator` 자식을 추가해 V0 의 `(type_alias
> (identifier) @lib)` 패턴이 더 이상 매칭되지 않음.
> V2.8 ✅ **file-level free-function form limitation lock** —
> 2x2 (scope × alias-shape) 매트릭스의 마지막 사분면. Sol 0.8.13+
> `using {Math.add} for uint256 global;` (file scope + free-function
> form) 도 0 EdgeUsesFor 발산. queryUsingFor 의 3 top-level
> alternative 가 모두 `contract_body` 안만 매칭하므로 source_file
> 직속 using_directive 는 alias-entry shape 와 무관하게 scope
> 제외 — V2.5 와 동일 결과. 이로써 "scope" 가 dominant axis 임이
> empirical 로 확정 (file-level → 항상 0 edges, alias-shape 영향
> 무).
> V2.9 ✅ **bare free-function alias limitation lock** —
> contract-scope alias-shape axis 의 3rd variant. Sol 0.8.13+
> `contract Calc { using {addPlusOne} for uint256; }` (qualifier
> 없는 bare free function name) 은 0 EdgeUsesFor 발산. EdgeUsesFor
> schema (Contract → Contract/library) 에는 bare 함수가 binding
> target 으로 적합하지 않고, V0 query 가 capture 하더라도 byName
> lookup 이 실패. V1.0+ dispatch chain 도 resolveBindingLib 의
> `lib.method` qname 조회 candidate 가 없어 EdgeCalls 미발산.
> Triplet (contract scope × alias entry shape):
>   - V2.6 library-qualified  → 1 edge  (V0 incidental capture)
>   - V2.7 operator-form      → 0 edges (AST shape mismatch)
>   - V2.9 bare free-function → 0 edges (no library target)
> V2.10 ✅ **mixed bare/aliased multi-import fix** — V1.28
> `runImportAliases` 가 mixed-entry single import (`import
> {SafeMath, Address as A} from ".../lib.sol";`) 에서 alias 를
> 잘못 pair 했음. 원인: V1.28 V0 의 positional bucket zip
> (`importNames=[SafeMath, Address]`, `aliases=[A]`, `zip[0] =
> (A, SafeMath)`) 이 source 순서가 아닌 bucket index 로 짝지어
> A → SafeMath 라는 잘못된 매핑 생성. Fix: single-pass source-
> order walk 으로 `import_name` 직후의 `alias` 만 pair, bare
> entry 는 lastImportName 을 overwrite. V1.28/V1.29 기존 케이스
> 도 동일 로직으로 회귀 없이 처리. RED test 첫 시도 실패 →
> empirical bug 노출 → fix → GREEN.
> V2.11 ✅ **bare path-only import regression guard** —
> Solidity default form `import "./lib.sol";` (curly-brace 없음,
> alias 없음, namespace 없음) 의 cross-file 동작 lock. V1.14
> cross-file dispatch infrastructure 의 global byName 인덱스가
> 정상 처리: `using SafeMath for uint256;` → byName[NodeContract]
> ["SafeMath"] 매칭 → Vault → SafeMath EdgeUsesFor (ConfInferred)
> + Vault.compute → SafeMath.add EdgeCalls 모두 발산. 4 가지
> import shape 모두 unified test surface 달성:
>   - V1.28 single/multi named alias  (`{Lib as L}`)
>   - V1.29 namespace alias            (`"./lib" as L`)
>   - V2.10 mixed bare + aliased       (`{LibA, LibB as L2}`)
>   - V2.11 bare path-only             (`"./lib"`)
> V2.12 ✅ **user-defined value type (UDVT) + using-for** —
> Sol 0.8.8+ `type Amount is uint256;` 가 using-for receiver type
> 으로 정상 작동함을 lock. V0 query `source: (_) @type` 가 UDVT
> 를 wrapping 하는 type_name → user_defined_type 노드를
> `extractTypeNameText` 로 정상 정규화 ("Amount" 식별자 추출),
> bindings[Vault]["Amount"] = ["Math"] 로 binding 후 V1.0 state-
> var dispatch 가 receiver call `balance.double()` 를 Math.double
> 로 정상 해소. UDVT 가 primitive type / library type 과 동등하게
> dispatch surface 에서 처리됨을 empirical 검증.
> V2.13 ✅ **diamond inheritance + multi-parent binding union fix** —
> V1.2 BFS inheritance propagation 의 hidden bug. `contract Child
> is A, B` 에서 A 와 B 가 같은 type 에 다른 library binding 을
> 가지면, child 가 양쪽 모두 inherit 해야 (V2.2 multi-binding
> semantics extended across inheritance) 하지만, V1.2 V0 의
> "if !exists" guard 가 첫 ancestor 의 binding 이 slot 을 차지한
> 후 후속 ancestor 의 binding 을 silently drop. Fix: BFS 시작 전
> child 의 LOCAL binding key 를 snapshot 으로 보존 → shadowing
> 판단은 snapshot 으로만, ancestor-to-ancestor 는 de-duplicated
> union (slices.Contains 로 중복 제거). V2.10 이후 두 번째 actual
> bug fix V-cycle. RED on first run → fix → GREEN.
> V2.14 ✅ **interface-body using-for variants lock** — V0 query
> (queries.go §W6) nests `using_directive` under
> `interface_declaration`, so interface bodies ARE walked. V2.14
> probes all three `using` variants inside an interface body
> (semantically nonsensical — interfaces have no state to bind
> methods on) in one fixture:
>   - `IBare` : `using SafeMath for uint256;`        → 1 EdgeUsesFor
>     (V0 happy path matches type_alias).
>   - `IFree` : `using {Math.add} for uint256;`      → 1 EdgeUsesFor
>     (V2.6-style incidental @lib capture inside type_alias child).
>   - `IOp`   : `using {Math.add as +} for uint256;` → 0 EdgeUsesFor
>     (V2.7-style AST shape mismatch — `user_definable_operator`
>     child breaks the type_alias match path).
> All 3 predictions held on first run — clean GREEN, no resolver
> fix. Phantom edges to libraries from interface declarations are
> a known graph artifact (V0's query is shape-driven, scope-agnostic);
> downstream consumers should treat interface-scope EdgeUsesFor as
> structural noise rather than dispatch-relevant binding.
> Contrast table extension (cf. V2.5/V2.6/V2.7/V2.9):
>   - scope:file      × variant:operator-form → 0 edges (V2.5)
>   - scope:contract  × variant:free-func     → 1 edge  (V2.6)
>   - scope:contract  × variant:operator-form → 0 edges (V2.7)
>   - scope:interface × variant:type-alias    → 1 edge  (V2.14 IBare)
>   - scope:interface × variant:free-func     → 1 edge  (V2.14 IFree)
>   - scope:interface × variant:operator-form → 0 edges (V2.14 IOp)
> V2.15 ✅ **same-line shadow byte-precision fix** — V2.0 carry-over
> closure. When outer-scope decl + inner-block shadow + both use
> sites all sit on a single physical line, V2.0's line-only filter
> (`declLine ≤ useSiteLine ≤ scopeEndLine`) admits both decls for
> either use site, and the strict-`>` `declLine > bestDeclLine`
> tiebreak leaves the first-appended outer winning. Inner use
> drops (= V1.30 V0 false-negative resurfacing, scoped to same-line
> code).
>
> Fix:
>   - `PendingRef.ByteOffset int` (parser.go) — Sol parser populates
>     at every use-site emit (12 sites in using_for.go).
>   - `localDecl.{declStartByte, scopeEndByte}` (resolve.go) — emit
>     chain encodes via 5-part TargetQName
>     `varName|typeName|scopeEndLine|declStartByte|scopeEndByte`.
>   - `lookupReceiverType(..., useSiteByte int, ...)` — 4 callers
>     pass pr.ByteOffset.
>   - `selectLocalDecl`: byte-containment + max declStartByte
>     tiebreak when `useSiteByte > 0 && allHaveBytes(decls)`. Falls
>     back to V2.0 line-only when bytes are mixed/absent
>     (defensive — partial parser upgrades stay correct).
>
> RED→GREEN cycle. Fixture compresses V2.0's shadow_inner_use.sol
> function body to one line. V2.0 produced 1 edge (SafeMath.add
> only, outer wins). V2.15 produces both (Other.tag + SafeMath.add)
> via byte-offset containment. Zero regression across 45+ using-for
> tests, golang/typescript/proto parsers, go vet clean.
>
> Architectural note: V2.15 closes V2.0's stated carry-over with
> the smallest possible cross-parser surface — `ByteOffset int`
> on PendingRef is read only by the Sol resolver; other parsers
> leave it at default 0 and selectLocalDecl falls back to V2.0
> behavior. No behavioral change for Go/TS/Proto callers.
>
> V2.18 ✅ **file-level using directive ERROR-tolerant walker** —
> V2.16 row 1 closure (single-row scope, operator-form deferred per
> V2.17 evidence). Sol 0.8.13+ file-level `using LibName for T
> [global];` directives are grammar-blocked in vendored
> tree-sitter-solidity v1.2.11: they parse into a single ERROR child
> of `source_file` with `using` keyword misclassified as an identifier
> inside a fake user_defined_type. V2.18 AST probe 2026-05-17
> confirmed the inner shape is recoverable:
>
>   source_file
>     ERROR "using SafeMath for uint256 [global];"
>       type_name (user_defined_type (identifier "using"))   ← keyword
>       identifier "SafeMath"                                ← lib name
>       type_name (primitive_type "uint256")                 ← bound type
>       [identifier "global"]                                ← optional
>
> `runFileLevelUsingFor` in using_for.go walks source_file's direct
> ERROR children, gates on `strings.HasPrefix(text, "using ")`,
> extracts library + bound type from named children, and emits the
> same PendingRef pair runUsingFor uses for contract-body directives.
> Per-container fan-out: one emit per contract/interface in v.nodes
> (NodeContract with SubKind != "library" + NodeInterface) preserves
> Sol semantics (file-level binding applies to all containers in
> source). Library subkinds excluded to prevent self-binding phantom
> edges (Math → Math) — empirically caught during walker bring-up.
>
> Two fixtures lock the behavior:
>   - file_level_using_global.sol: single Vault contract + `using
>     SafeMath for uint256 global;` → 1 EdgeUsesFor (Vault → SafeMath)
>     + 1 EdgeCalls (Vault.compute → SafeMath.add via dispatch wiring).
>   - file_level_using_multi_contract.sol: VaultA + VaultB + `using
>     SafeMath for uint256;` (no global) → 2 EdgeUsesFor + 2 EdgeCalls,
>     one per contract. Confirms fan-out is correct.
>
> Sharing infrastructure with runUsingFor was deliberate: same
> dispatchKindUsingFor / dispatchKindUsingForTypeBind constants, same
> alias normalisation (namespaceAliases / importAliases), so dispatch
> resolution requires no changes. The walker is purely additive.
>
> Why operator-form deferred (V2.17 row 2 stays at category A): V2.18
> AST probe confirmed V2.7's operator-form fixture parses with NO
> using_directive node at all — the braced body becomes a
> `state_variable_declaration` with `as` as the state-var name. A
> recovery walker would need to discriminate true state-vars from this
> misparse — too fragile for the value. Wait for grammar bump.
>
> Zero regression across 50+ Sol tests, golang/typescript/proto
> parsers, go vet clean. V2.16 row 1 status flipped from "A still
> blocked" to "A-recovered (V2.18)".
>
> V2.18 carry-over (V2.19+):
>   - Upstream tree-sitter-solidity bump tracking — V2.18 walker retires
>     when source_file using_directive parses natively.
>   - W6 closure decision — only row 2 (operator-form, requires grammar
>     bump) and row 3 (free-function partial recovery, post-bump
>     re-verify) remain. Cost-benefit ratio for further V-cycles is
>     thin; W7+ probably a better focus shift.
>
> V2.17 ✅ **operator-form grammar-block lock + V2.16 row 2
> reclassification** — V2.16 carry-over closure changed direction
> mid-cycle. Original plan: extend queryUsingFor with a `using_alias`
> arm to fix operator-form (the "highest-leverage" V2.16
> recommendation). Empirical AST dump on the vendored grammar
> (v1.2.11, `internal/parse/solidity/binding/`) 2026-05-17 invalidated
> the premise:
>   (a) `using_alias` is NOT a valid node type in the vendored grammar.
>       Tree-sitter rejects any query referencing it with "Invalid node
>       type using_alias". The V0/V2.5 spec comments cited it
>       speculatively.
>   (b) Operator-form `using {Math.add as +} for T;` parses with NO
>       `using_directive` node at all. The braced body is misclassified
>       as a `state_variable_declaration` wrapped in ERROR nodes.
>   (c) Free-function form `using {Math.add, Math.sub} for T;` parses
>       to a degraded `using_directive` containing `ERROR` + `type_alias`
>       + `ERROR "}"`. V2.6's 1-edge "rediscovery" was a fortuitous
>       partial-parse artifact, not first-class grammar support.
> Conclusion: operator-form belongs in V2.16 category A (grammar reject),
> not B (query gap). No query change can produce edges from missing
> AST nodes. V2.5 / V2.7 / V2.14 IOp 0-edge locks are correct as-is.
> V2.17 deliverable: lock the same 0-edge behavior at library scope
> (the only non-file scope previously untested for operator-form),
> capture full AST-shape evidence in the test prologue, and update
> V2.16 row 2 + row 3 + carry-over to reflect the empirical truth.
> Future grammar bump → V2.17's failing assertion forces a coordinated
> lock-flip across V2.5 / V2.7 / V2.14 IOp / V2.17 (file-level may
> stay 0 due to row 1's independent block).
>
> V2.16 ✅ **grammar-blocked items survey** — V2.15 carry-over
> closure. Consolidates all "grammar-blocked" claims scattered
> across V1.2 / V1.8 / V2.5 / V2.6 / V2.7 commits into one
> classification table. Three categories:
>
>   A. **Grammar reject** (parser produces ERROR nodes)
>   B. **Query gap** (grammar OK, V0 query doesn't capture)
>   C. **Out of scope** (intentional — separate spec or deprecated)
>
> Survey rows (status as of vendored tree-sitter-solidity v1.2.11
> / v1.2.13 — `internal/parse/solidity/binding/`):
>
> | # | Item | Category | Evidence | Action |
> |---|------|----------|----------|--------|
> | 1 | File-level `using LibName for T [global];` (0.8.13+ at source_file scope) | **A-recovered (V2.18)** | Grammar still blocks (parser wraps in ERROR per V1.2 / V2.18 dumps). V2.18 (2026-05-17) added `runFileLevelUsingFor` walker that pattern-matches `source_file > ERROR "using ..."` and extracts library name + bound type from named children. Per-container fan-out (one edge per contract/interface in the file, library subkinds excluded) preserves Sol semantics. V2.5 file-level *operator-form* stays at 0 — `{f as +}` body has no recoverable identifier shape (V2.17). | Recovery walker landed. Re-verify after upstream grammar bump (walker becomes redundant once `using_directive` parses at source_file scope natively). |
> | 2 | Operator-form `using {f as +} for T;` (0.8.19+, any scope) | **A** grammar reject (V2.17 RECLASSIFIED from B) | Empirical AST dump 2026-05-17 on vendored grammar v1.2.11: braced operator-form body is misparsed as a `state_variable_declaration` wrapped in ERROR nodes; NO `using_directive` node emitted. `using_alias` referenced in V0/V2.5 spec comments does not exist as a valid node type. V2.5 / V2.7 / V2.14 IOp / V2.17 all lock 0 edges across file / contract / interface / library scopes. | Grammar bump required (query extension is impossible — there's nothing for a query to match). OR ERROR-tolerant custom walker, same shape as row 1. |
> | 3 | Free-function form `using {Math.add, Math.sub} for T;` (0.8.13+, contract / interface body) | **A-partial** (degraded but recoverable) | V2.17 AST dump 2026-05-17: parses to a `using_directive` containing `ERROR`+`user_defined_type`+`type_alias`+`ERROR "}"` — i.e. the braces are ERROR but the inner qualified `Math.add` produces a fortuitous `type_alias` child whose identifier V0's existing query incidentally captures. V2.6 / V2.14 IFree lock the 1-edge result. NOT first-class grammar support — fragile partial-recovery artifact that could shift on grammar bump. | None for now (V2.6 lock holds). Re-verify post-grammar-bump that the partial recovery still produces the same shape, else flip locks. |
> | 4 | Inline assembly / Yul (`assembly { let x := ... }`) | **C** out of scope | Sol parser doesn't descend into `assembly_statement` bodies. Yul has its own grammar / type system / dispatch model. | Separate workstream. Track in a Yul-specific spec; not part of W6. |
> | 5 | Pre-0.5.0 `var` keyword (`var x = expr;`) | **C** deprecated | V1.17 reassessment 2026-05-12: removed from scope. modern Sol (0.5.0+) requires explicit types. | None — production code uses explicit types. |
> | 6 | Identifier-slot tuple destructuring (`(uint a, ) = foo();`) | **Captured (V1.16 family)** | V1.16-V1.18 multi-return tuple destructuring covers LHS explicit types; cross-file validated in V1.18. Anonymous slots (no name) silently skip per parser-required-fields. | None — V1.16 family already lands this. |
> | 7 | Wildcard imports (`import * as Lib from "./lib"`) | **Captured (V1.28-V1.29 + V2.10-V2.11)** | V1.28 aliased imports / V1.29 whole-file alias / V2.10 mixed bare+aliased / V2.11 bare path-only. All four major shapes locked. | None — import surface complete. |
>
> Net summary (post-V2.18 row 1 recovery):
>   - **1 grammar-blocked item remaining** (category A): row 2 operator-
>     form `using {f as +} for T;`. V2.18 AST probe confirmed it has no
>     recoverable shape (V2.7 case parses with NO using_directive node;
>     braced body misclassified as `state_variable_declaration` with
>     "as" misread as a state-var name). A recovery walker would have
>     to discriminate true state-vars from this misparse — too fragile.
>     Requires grammar bump.
>   - **1 row recovered by walker** (V2.18, row 1): file-level using
>     directive now produces 1 EdgeUsesFor per contract/interface in
>     the file + downstream dispatch wiring. Library subkinds excluded
>     from fan-out (no self-binding phantom edges).
>   - **1 partial-recovery item** (row 3): free-function form still
>     works via V0's existing query (fortuitous `type_alias` partial
>     recovery). Re-verify post-grammar-bump.
>   - **2 items out-of-scope** (rows 4-5) — Yul + deprecated `var`.
>   - **2 items already complete** (rows 6-7) — tuple destructuring + imports.
>
> Action priority (V2.19+ candidates):
>   1. Upstream grammar bump tracking — JoranHonig/tree-sitter-solidity
>      version that natively supports operator-form. Coordinate lock-flips
>      across V2.5 / V2.7 / V2.14 IOp / V2.17 when it lands. Re-probe
>      row 1 to confirm V2.18 walker can retire (becomes redundant once
>      using_directive parses at source_file scope natively).
>   2. Operator-form recovery walker spike (row 2) — only if grammar bump
>      isn't on the horizon. Would need careful discrimination against
>      real state-vars to avoid false positives. Lower confidence than
>      V2.18's file-level walker which has a unique ERROR-text prefix.
>   3. Yul dispatch (row 4) — separate workstream, large surface.
>
> V2.18 carry-over (V2.19+):
>   - W6 closure decision: 1 remaining grammar-block + 1 fragile partial
>     recovery + walker maintenance burden. The cost-benefit ratio for
>     further V-cycles in using-for keeps shrinking. Consider shifting
>     focus to W7+ topics unless real-world Sol corpora surface operator-
>     form heavy usage worth a fragile walker.
> Pre-declared identifier-slot tuple 은 modern Sol 에서 `var` keyword
> deprecated (0.5.0+) 로 실용 사례 거의 없음 — V1.17 reassessment 결과
> scope 에서 제외.
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

**Status — 2026-05-11**: ✅ schema 1.10 slot **reserved**. `EdgeOverrides`
appended at `AllEdgeTypes()` index 39 (W-B `EdgeAwaits` 가 index 38 을
점유). `EdgeImplements` / `EdgeExtends` 는 enum 에 이미 존재 — 새 추가
없음. SubKind 값 확장 (`NodeContract.SubKind` ⊇ {abstract, library},
`NodeFunction.SubKind` ⊇ {virtual, override, virtual_override, fallback,
receive}) 은 문자열 컨벤션이라 enum 변경 불필요. Detector emission 은
Phase 5 (W1/W2/W3/W6) 진입 시까지 0 — `internal/parse/solidity/*` 본
Phase 4 bump 에서는 무변경.

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

- Library call (`using SafeMath for uint; a.add(b)`): W6 V0 ✅ LANDED
  2026-05-12 — `using SafeMath for uint;` directive 자체는 EdgeUsesFor
  (Contract → Library) 로 가시화. `a.add(b)` 의 dispatch resolution
  (→ SafeMath.add EdgeCalls) 은 V1 follow-up (receiver type 추론 인프라
  필요, §4.6.6 V0 한계 carry-over).
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

**Status — 2026-05-11**: ✅ **LANDED**. 실제 변경:
- `internal/parse/solidity/inheritance.go` (신규, ~100 LOC) — `is`-clause
  detector + PendingRef emit. 모든 parent reference 를 Pass 1 에서
  provisional `EdgeExtends` 로 큐잉하고 Pass 2 에서 reclassify.
- `internal/parse/solidity/inheritance_test.go` (신규, ~200 LOC) — 3 test
  function (single-file table-driven, cross-file, interface emit
  regression). 7 subtest PASS.
- `internal/parse/solidity/queries.go` — `queryInterface` +
  `queryInheritance` alt-branch query (contract_declaration /
  interface_declaration 양쪽 매치).
- `internal/parse/solidity/abstract_library.go` — `runInterfaceDecl()`
  추가 (NodeInterface emit, SubKind="interface"). 기존 emit 헬퍼 재사용.
- `internal/parse/solidity/declarations.go` — `visit()` 에
  `runInterfaceDecl()` + `runInheritance()` 호출 wire.
- `internal/parse/solidity/resolve.go` — Pass 2 에 `resolveInheritanceRef`
  추가. Contract/Interface 양쪽 by-name index 사용, (childType,
  parentType) 조합으로 edge type 결정. Interface 우선 lookup 으로 solc
  네임스페이스 의미 모방.
- `internal/parse/solidity/testdata/inheritance/*.sol` (5 fixture):
  `simple_extends.sol` / `multiple_inheritance.sol` /
  `interface_implements.sol` / `abstract_extends.sol` (same-file) +
  `cross_file_parent.sol` + `cross_file_child.sol` (cross-file pair).

**KPI (build --lang=sol on `testdata/inheritance/`)**:
- 47 nodes / 54 edges total
- Contract nodes: 12 plain + 1 abstract; Interface nodes: 7
- `EdgeExtends` = 8 (7 EXTRACTED, 1 INFERRED = ChildToken→BaseToken)
- `EdgeImplements` = 5 (4 EXTRACTED, 1 INFERRED = ChildToken→IExternal)
- Multiple inheritance (Mixed extends BaseContract + implements IFoo +
  implements IBar) 정상 분리 emit
- Interface-to-interface (IB extends IA) 는 EdgeExtends (NOT EdgeImplements)
  — solc 와 동일 분류 의미

**Verification**:
- `go test ./internal/parse/solidity/... -count 1` 전 PASS (golden 포함)
- `go test ./... -count 1` 전 PASS
- `go vet ./...` clean
- §7.0 Go regression — `--lang=go` baseline diff = 0 (Go-only 빌드는
  Sol 작업 영향 없음, 8개 edge type 카운트 그대로)
- Cross-file resolution 정상 (ChildToken→{BaseToken,IExternal} INFERRED)

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

**Status — 2026-05-11**: ✅ **LANDED**. 실제 변경:
- `internal/parse/solidity/overrides.go` (신규, ~230 LOC) — virtual /
  override modifier scan, function SubKind 라벨링
  (`function`/`virtual`/`override`/`virtual_override`), EdgeOverrides
  PendingRef 큐잉. 두 dispatch kind (bare `override` → resolver 가
  parents walk; `override(A,B)` → 명시적 parent 별 직접 lookup).
- `internal/parse/solidity/overrides_test.go` (신규, ~250 LOC) — 3 test
  function (SingleFile / CrossFile / W1Regression). 11 subtest PASS.
- `internal/parse/solidity/declarations.go` — `runDecl(queryFunction,
  NodeFunction)` 을 `runFunctionDecl()` (SubKind-aware) 로 교체. 다른
  decl 경로는 그대로.
- `internal/parse/solidity/resolve.go` — Pass 2 를 2a (W1 inheritance) /
  2b (W2 overrides + 기존 emits/has_modifier/writes_mapping) 로 분리.
  W2 bare-override 처리는 W1 의 EdgeExtends/EdgeImplements 인접 리스트를
  토대로 부모 walk; 동일 contract/function 이름이 여러 파일에 존재할 때
  file-scoped 매칭으로 동명이접 (homonym) 해소.
- `internal/parse/solidity/testdata/overrides/` 6 fixture:
  `simple_override.sol` / `super_call.sol` / `virtual_no_override.sol` /
  `multiple_inheritance_override.sol` + `cross_file_parent.sol` +
  `cross_file_child.sol`.
- `internal/parse/solidity/testdata/sol_contract_golden.json` — Function
  노드 sub_kind="function" 추가 (`-update` 자동 갱신, 노드/엣지 카운트
  무변경).

**KPI (build --lang=sol on `testdata/overrides/`)**:
- 29 nodes / 35 edges total
- EdgeOverrides = 6 (5 EXTRACTED, 1 INFERRED = ChildVault→BaseVault)
- per-fixture breakdown: simple=1, super_call=2 (Mid→Base, Top→Mid),
  virtual_no_override=0, multi_explicit=2 (C→A, C→B), cross_file=1
- Function SubKind 라벨 모든 fixture 검증: virtual / override /
  virtual_override / function

**Verification**:
- `go test ./internal/parse/solidity/... -count 1` 전 PASS (golden 포함)
- `go test ./... -count 1` 25/25 PASS
- `go vet ./...` clean
- §7.0 Go regression — `--lang=go` baseline diff = 0 (Go-only 빌드는
  Sol 작업 영향 없음, 8개 edge type 카운트 그대로)
- W1 회귀 0 — testdata/inheritance 에서 EdgeExtends 8 / EdgeImplements 5
  유지 (사전 baseline 과 동일). EdgeOverrides 는 abstract_extends.sol 의
  Concrete.thing→AbstractBase.thing 1건 신규 emit (W4 fixture 가 W2 로
  덤으로 검출됨, 자연스러운 부수효과)

**Scope split note**: `super.foo()` body-walk 은 design §3.3 의 후반부
설명대로 declaration-time EdgeOverrides 와 분리해 W3 (interface dispatch)
와 같은 resolver 경로를 공유할 수 있어 W2 atomic scope 밖. W2 fixture
super_call.sol 은 declaration-time override 체인만 검증한다.

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

**Status — 2026-05-11**: ✅ **LANDED**. 실제 변경:
- `internal/parse/solidity/dispatch.go` (신규, ~155 LOC) — `member_expression`
  AST 형태 매칭으로 `Type(args).method` 패턴을 감지하고 EdgeInvokes
  PendingRef 큐잉. tree-sitter S-expression 으로 표현 불가능한 중첩
  predicate (object 가 call_expression 이면서 그 function 이 identifier)
  은 Go 후처리로 분리. `unwrapExpression` 헬퍼로 grammar 가 끼우는
  `expression` wrapper 한 겹씩 벗기며 매칭.
- `internal/parse/solidity/dispatch_test.go` (신규, ~230 LOC) — 4 test
  function (SingleFile / CrossFile / NoFalsePositive / W1W2Regression).
  9 subtest PASS. 음성 케이스 `address(this)` / `super.foo()` / unknown
  type 모두 명시적 검증.
- `internal/parse/solidity/declarations.go` — `visit()` 에 `runDispatch()`
  호출 wire. 위치는 `runInheritance()` 다음, `collectABI()` 앞 (Pass 1
  body-walk 검출은 declaration emit 이후가 자연 순서).
- `internal/parse/solidity/resolve.go` — Pass 2b 에 W3 분기 + 신규
  `resolveInterfaceDispatchRef()`. TargetQName 을 "TypeName.MethodName"
  로 split, Interface byName 인덱스로 1차 필터 (모르는 식별자 drop) +
  funcByQName 으로 2차 lookup. 동명 candidate 가 여러 파일에 있으면
  source 파일 우선 (W2 와 동일 idiom).
- `internal/parse/solidity/testdata/dispatch/` 4 fixture:
  `simple_dispatch.sol` / `chained_dispatch.sol` + `cross_file_iface.sol`
  + `cross_file_caller.sol`.

**KPI (build --lang=sol on `testdata/dispatch/`)**:
- 22 nodes / 24 edges total
- EdgeInvokes = 6 (모두 AMBIGUOUS, §5.0 Q5)
- per-fixture breakdown: simple=2 (Caller.send→IERC20.transfer,
  Caller.check→IERC20.balanceOf), chained=3 (Router.route→{IFoo.bar,
  IBar.baz}, Router.proxy→IFoo.something), cross_file=1
  (ExternalCaller.run→IExternalAPI.execute)
- W1 (EdgeExtends/EdgeImplements) = 0 / W2 (EdgeOverrides) = 0 — 본
  fixture 는 dispatch only, 검출 순수성 확인

**Verification**:
- `go test ./internal/parse/solidity/... -count 1` 전 PASS (W1/W2/W4 회귀 0)
- `go test ./... -count 1` 24 packages PASS
- `go vet ./...` clean
- §7.0 Go regression — `--lang=go` baseline diff = 0 (Sol 작업이 Go
  그래프에 영향 0; 8개 edge type 카운트 그대로)
- testdata/inheritance / testdata/overrides 회귀 0 (각각
  extends=8/implements=5/overrides=1, extends=6/overrides=6 보존)
- 음성 케이스 (`address(this)`, `super.foo()`, unknown identifier) 모두
  EdgeInvokes 0건 — false positive 없음

**Scope split note**: §3.4 의 `super.foo()` body-walk 은 W3 의 직접 scope
밖 (declaration-time override 는 W2 가 이미 emit). Spec §4.3 의 "viewer
의 Possible Dispatch Targets 패널" 은 별도 PR. `using For` 는 W6.

### 4.4 W4 — abstract / library SubKind

1. `abstract contract` → SubKind="abstract"
2. `library` → SubKind="library"
3. tree-sitter modifier 확인
4. 단위 테스트

추정 사이즈: 50~100 LOC + 2 fixture. (가장 단순, 첫 작업으로 wrap-up
가능)

**Status — 2026-05-11**: ✅ **구현 완료**. SubKind 값 확정: plain `contract`도
명시적으로 `SubKind="contract"` 발행 (기존 빈 문자열 → "contract" 로 승격,
W1 의 interface 검출과 같은 라벨 idiom 유지). 변경 파일:
- `internal/parse/solidity/abstract_library.go` (신규, ~170 LOC)
- `internal/parse/solidity/abstract_library_test.go` (신규)
- `internal/parse/solidity/queries.go` (`queryLibrary` 추가)
- `internal/parse/solidity/declarations.go` (`runContractDecl` /
  `runLibraryDecl` 진입, `nearestContractName` 가 library_declaration /
  interface_declaration 까지 walk)
- `internal/parse/solidity/testdata/subkind/{abstract,library,plain}.sol`
- `internal/parse/solidity/testdata/sol_contract_golden.json` (sub_kind
  필드만 추가, 노드/엣지 카운트 무변경)

검증: 9 test PASS (`go test ./internal/parse/solidity/... -count 1`),
`go vet ./...` clean, Go 영역 빌드 KPI diff = 0 (Sol 작업이 Go 그래프에
영향 0). 빌드된 그래프에서 `SELECT name, sub_kind FROM nodes WHERE
type='Contract'` 가 `Base/abstract`, `SafeMath/library`, `Simple/contract`
세 row 정확히 반환.

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

### 4.6 W6 — `using For` (library extension)

W6 는 Q9 (§5.0) 결정으로 신설된 단계. `using SafeMath for uint;` 같은
library extension 을 인식해 `a.add(b)` 가 `SafeMath.add(a, b)` 로
dispatch 되는 의미를 그래프에 반영. 실세계 Sol 코드의 30% 이상이
OpenZeppelin SafeMath / Address / EnumerableSet 류를 쓰므로 이걸 처리
못하면 method call 의 상당 비율이 unresolved 됨 (§3.5 한계 그대로).

W4 가 이미 library 자체를 `NodeContract + SubKind="library"` 로 emit
하므로 W6 는 *binding + dispatch* 만 추가하면 됨 (library declaration
emit 은 재발명 안 함).

#### 4.6.1 목표 동작 (V0 scope)

**V0 = binding declaration emit only**. Q9-1 (b) 채택의 핵심 가치인
"이 contract 가 어떤 library 를 binding 했나" 를 first-class EdgeType 으로
가시화. 세 가지 using directive 형태 인식:

```solidity
using SafeMath for uint256;        // 특정 타입 binding (가장 흔함)
using SafeMath for *;              // 모든 타입 (전역 binding)
using {SafeMath.add, SafeMath.sub} for uint256;  // Solidity 0.8.13+ free function form
```

기대 동작 (V0):

```solidity
library SafeMath {
  function add(uint a, uint b) internal pure returns (uint) { ... }
}

contract Vault {
  using SafeMath for uint256;
  // graph: Vault  --using_for-->  SafeMath   (EXTRACTED 또는 INFERRED)

  function deposit(uint256 amount) external {
    uint256 total = balance.add(amount);
    // V0: NodeCallSite emit but no EdgeCalls to SafeMath.add (receiver type
    //     추론 인프라 미구현). V1 에서 처리.
  }
}
```

**V0 scope 결정 (2026-05-12)**: method call resolution
(`balance.add()` → SafeMath.add EdgeCalls) 는 V1 follow-up. 이유:
- receiver type 추론을 위해 state variable / parameter declared type
  인덱스가 parser-side 에 필요 (NodeField 의 Signature 필드 또는 별도
  side-channel). 본 V0 에서 도입하지 않으면 false negative 다수 발생.
- Q9-1 (b) 의 핵심 가치 (binding 가시화) 는 EdgeUsesFor emit 만으로
  달성. dispatch resolution 은 직교 dimension.
- W6 V0 land 후 사용자 실세계 데이터로 type 인덱스 우선순위 판단 가능.

#### 4.6.2 검출 알고리즘 (V0)

2-stage:

1. **Using directive parsing** — tree-sitter `using_directive` 노드를
   queries.go 의 새 query 로 캡처. 세 변형 (specific / wildcard /
   free-function) 모두 `library_name` + `type_name` (또는 `*`) 페어로
   정규화 후 PendingRef emit (DispatchKind="using_for", SrcID=contractID,
   TargetQName=libraryName).

2. **Library resolution** — Resolve Pass 2 에서 PendingRef 의
   TargetQName 을 byName[NodeContract] 인덱스로 lookup (library 는
   W4 에서 NodeContract + SubKind="library" 로 emit 됨). 매칭 시
   EdgeUsesFor (Contract → Library) emit:
   - same-file → ConfExtracted
   - cross-file → ConfInferred
   - 미해결 → drop (다른 PendingRef 들과 동일 strict-purge 정책)

알고리즘 의도: V0 에서는 contract-scoped binding 의 *존재* 만 가시화.
typeName 정보는 PendingRef 의 `Line` 필드 + edge 자체의 src/dst 로
충분 — 같은 contract 에서 여러 type binding 이면 같은 contract→library
쌍에 대해 여러 EdgeUsesFor 가 emit (typeName 별로 dedup 안 함). V1 에서
method call resolution 추가 시 typeName 인덱스가 의미를 가짐.

#### 4.6.3 결정 결과 — Q9 후속 (2026-05-12)

W6 의 graph 표현 방법:

| 옵션 | 그래프 표현 | schema | LOC | trade-off |
|------|-----------|--------|-----|-----------|
| **(b) 별도 EdgeUsesFor** | (Contract→Library) `using_for` + dispatch 는 `calls` | schema 1.10 append (EdgeUsesFor 추가) | +50 over (c) | Solidity binding semantics first-class — extends/implements/overrides/has_modifier 와 동급. viewer/SQL 단일 edge filter 로 "어디 binding?" 답 가능 |
| (c) EdgeCalls 재사용 | dispatch 는 기존 `calls`. binding 은 detector 내부 (graph 미표시) | 변경 없음 | baseline | graph 표현 일관성 위반 — extends 도 syntactic level 이지만 first-class 인데 using_for 만 invisible |

**결정 (2026-05-12)**: **Q9-1 = (b)** EdgeUsesFor 신규.

이유:
- **Solidity semantics first-class 표현**. extends / implements / overrides
  / has_modifier / emits_event / reads_mapping 등 다른 Sol-specific 의미가
  모두 first-class EdgeType. `using_for` 만 invisible 하면 그래프 일관성
  위반 — 사용자의 정당한 지적 (2026-05-12 결정 turn).
- **추가 비용 실제로 미미**. EdgeType 1 줄 + AllEdgeTypes append + viewer
  edges.ts 등록 (G2 카테고리, 1 entry) = ~40 LOC. 본체 +200 LOC 대비 20%
  미만.
- **사용자 query 직관**. "이 contract 가 어떤 library 를 binding 했나" 가
  `WHERE edge.type='using_for'` 한 줄로 답 가능. SQL 재구성 필요 없음.
- **viewer 통합 cost 같은 idiom**. 직전 turn `7af9ce4` 에서 5종 edge 를
  같은 패턴 (G2 등록, 1 entry) 으로 통합 — 추가 1 entry 는 동일 cost 패턴.

**Q9-2 = (a)** Receiver type 추론 — V0 식별자만 (state variable / parameter
type). Return value chaining (`foo().bar.add(1)`) 은 별도 spec 으로 분리.

**Q9-3 = (a)** Wildcard binding fallback — specific-first (특정 타입
binding 우선, 없으면 `*` fallback).

#### 4.6.4 구현 sketch (V0)

```
internal/parse/solidity/
  using_for.go         (신규 ~100 LOC)
    - runUsingFor()                      ← visit() 에서 호출
    - parse using_directive subtree
      (세 형태: specific / wildcard / free-function)
    - emit PendingRef (DispatchKind="using_for", SrcID=contractID,
      TargetQName=libraryName)
  queries.go
    - queryUsingFor 추가

  resolve.go
    - Pass 2 분기: dispatchKindUsingFor → byName[NodeContract] (Library
      는 NodeContract + SubKind="library") lookup → EdgeUsesFor emit
    - same-file ConfExtracted / cross-file ConfInferred / 미해결 drop

internal/parse/solidity/testdata/using_for/
  specific_binding.sol      (1 type binding)
  wildcard_binding.sol      (`for *` form)
  multi_library.sol         (한 contract 에 여러 binding)
  cross_contract.sol        (binding 이 contract 별로 분리됨 검증)
  no_binding_negative.sol   (using 없이 method 호출 → drop 유지)

internal/parse/solidity/using_for_test.go
  - TestUsingFor_SpecificBinding
  - TestUsingFor_WildcardForm
  - TestUsingFor_MultiLibrary
  - TestUsingFor_ContractScoped       ← 같은 library 가 두 contract 에
                                        binding → 두 개의 EdgeUsesFor
  - TestUsingFor_NegativeNoBinding    ← drop 검증 (false positive 가드)
```

추정 사이즈: 100~150 LOC + 5 fixture + 5 test. resolve.go Pass 2 에
한 분기 추가 (~30 LOC). receiver-type 추론 헬퍼는 V1 에서 추가.

#### LANDED 2026-05-12 (W-C W6 V0)

- 구현: `internal/parse/solidity/using_for.go` (~100 LOC) + queries.go
  `queryUsingFor` (tree-sitter-solidity v1.2.13 `using_directive` →
  `type_alias` → `identifier` 경로). contract / library / interface
  body 모두 capture.
- Wire: `declarations.go::visit()` 에 `runDispatch()` 직후 `v.runUsingFor()`
  호출 추가.
- Resolver: `resolve.go::resolveUsingForRef()` — byName[NodeContract]
  lookup + `pickSameFileCandidate` helper (M2 review 와 동일 idiom).
  same-file ConfExtracted / cross-file ConfInferred / 미해결 drop.
- DispatchKind 태그: `"using_for"` — inheritance.go `"inherit"`, dispatch.go
  `"interface_dispatch"`, overrides.go `"override"/"override_explicit"` 와
  동일 idiom.
- Fixtures: `testdata/using_for/{specific_binding, wildcard_binding,
  multi_library, cross_contract, no_binding_negative}.sol` (5 files).
- 테스트: `using_for_test.go` 5 함수:
  - `TestUsingFor_SpecificBinding` — 1 EdgeUsesFor + ConfExtracted 검증
  - `TestUsingFor_WildcardForm` — `for *` 형태 (typeName 미surface)
  - `TestUsingFor_MultiLibrary` — 한 contract 의 2 library binding → 2
    edges
  - `TestUsingFor_ContractScoped` — 두 contract 같은 library binding →
    2 distinct edges sharing Dst (contract-scoped 의미 검증)
  - `TestUsingFor_NegativeNoBinding` — directive 없으면 0 edge (false-
    positive 가드)
- Schema: `pkg/types/enums.go` EdgeUsesFor 추가 (Commit A `19c99da`,
  schema 1.10 index 40 append).
- 회귀: 25/25 PASS, vet clean. §7.0 Go regression: TS/Go 영향 0.
- Viewer: `web/viewer-next/src/lib/edges.ts` G2 카테고리에 amber dashed
  등록 + DEFAULT_EDGE_TYPES on by default.

V0 carry-over (V1 follow-up)
- Method-call dispatch resolution (`balance.add(...)` → SafeMath.add
  EdgeCalls) — receiver type 인덱스 필요. **V1.0 LANDED 2026-05-12**
  (state-variable receiver 한정, 아래 LANDED 블록 참조).
- Free-function form `using {f1, f2} for T` — 별도 AST shape (using_alias
  child). V1.1+
- File-level using directive (0.8.13+ global binding) — contract scope
  외 위치. V1.1+
- Inherited using directive 상속 처리. V1.1+

#### LANDED 2026-05-12 (W-C W6 V1.0 — state-variable receiver dispatch)

- 구현 추가:
  - `internal/parse/solidity/using_for.go`: `runUsingForCalls` detector
    (member_expression 의 `<identifier>.<identifier>(...)` shape 인식)
    + `matchStateVarMethodCall` predicate + 두 신규 dispatch kind
    (`using_for_typebind`, `using_for_call`).
  - `internal/parse/solidity/queries.go`: queryUsingFor 에 `@type`
    capture 추가 (specific binding 의 type_name + wildcard 의
    any_source_type 양쪽 처리).
  - `internal/parse/solidity/declarations.go::runStateVarDecl`:
    NodeField QualifiedName 을 `<Container>.<varName>` 으로 qualify
    (runFunctionDecl 와 동일 idiom). NodeField.Signature 에 typeName
    저장 (extractTypeNameText helper 추가). golden snapshot 갱신.
  - `internal/parse/solidity/resolve.go`: Pass 1.5 에 stateVarTypes
    인덱스 구축 (qname prefix 기반). Pass 2 에 typebind 분기 (binding
    map 채움, edge emit 안 함) + using_for_call 분기 (`resolveUsingForCallRef`
    helper 호출).
- Architecture (4-step resolution chain in `resolveUsingForCallRef`):
  1. funcID → enclosing containerID (containerIDByFuncID, W-C W2 M1+M3
     review 의 reverse index 재사용)
  2. (containerID, receiverName) → typeName (stateVarTypes)
  3. (containerID, typeName) → libraryName (bindings); wildcard `*`
     fallback per Q9-3 (a)
  4. `<libraryName>.<methodName>` → libraryFunctionID (funcByQName)
- Confidence: ConfExtracted when both endpoints same-file; ConfInferred
  cross-file. W3 처럼 ConfAmbiguous 로 downgrade 하지 않음 — library
  dispatch 는 binding 만 알면 statically determinable.
- Fixtures: `testdata/using_for_v1/{state_var_dispatch, wildcard_dispatch,
  specific_over_wildcard, no_binding_negative}.sol`.
- 테스트: `using_for_v1_test.go` 4 함수:
  - `TestUsingForV1_StateVarDispatch` — 2 EdgeCalls (Vault.deposit→
    SafeMath.add, Vault.withdraw→SafeMath.sub) 검증
  - `TestUsingForV1_WildcardDispatch` — `for *` 만 있을 때 wildcard
    바인딩 활성화
  - `TestUsingForV1_SpecificOverWildcard` — Q9-3 (a) specific-first
    검증 (specific + wildcard 둘 다 있을 때 specific 우선)
  - `TestUsingForV1_NoBindingNegative` — using 없으면 0 EdgeCalls
- 회귀: 25/25 PASS, vet clean. §7.0 Go regression: Sol-only 변경.
- 가시화: 기존 EdgeCalls 가족 — viewer 추가 작업 없음 (W6 V0 의
  EdgeUsesFor 가 별도 가시화).

V1.0 carry-over (V1.1+ follow-up)
- Parameter receiver: **V1.1 LANDED 2026-05-12** (아래 LANDED 블록).
- Return-value chaining: `foo().add(x)` 같은 expression. 미세 정적
  추론, V0 한계 §4.6.6 의 receiver type 항목. **V1.2+**
- Free-function form `using {f1, f2} for T`: 별도 AST (using_alias). **V1.2+**
- File-level using directive (0.8.13+). **V1.2+**
- Inherited using directive (base contract 의 using 가 child 에 상속). **V1.2+**

#### LANDED 2026-05-12 (W-C W6 V1.1 — parameter receiver dispatch)

- 구현 추가:
  - `internal/parse/solidity/overrides.go::runFunctionDecl`: Function 노드
    emit 직후 `emitParameterMetaPending(v, id, declNode)` 호출. function_definition
    의 named children (tree-sitter shape: parameter는 직계 named child)
    순회, `parameter.name` + `parameter.type` 추출 후 dispatchKindUsingForParamType
    PendingRef emit (SrcID=funcID, TargetQName=`<paramName>|<typeName>`).
    Anonymous parameters (name field 부재) 는 skip.
  - `internal/parse/solidity/resolve.go`:
    - 신규 `paramTypeMap` 타입 (funcID → paramName → typeName).
    - Pass 2 사전 빌드 loop 에 `using_for_param_type` 분기 추가
      (bindings 와 함께 같은 sweep). switch 로 정리.
    - resolveUsingForCallRef signature 에 paramTypes 인자 추가.
    - receiver type lookup: state-var miss 시 paramTypes fallback.
      state-var first 이유 — Solidity scoping 상 parameter 가 state var
      를 shadow 못함 (solc error), 즉 순서는 hot-path 최적화일 뿐
      correctness 영향 없음.
- 4-step resolution chain 수정:
  1. funcID → containerID (변경 없음)
  2. **(containerID, receiverName) → typeName via stateVarTypes →
     fallback (funcID, receiverName) → typeName via paramTypes**
  3. (containerID, typeName | "*") → libraryName (변경 없음)
  4. `<libraryName>.<methodName>` → libraryFunctionID (변경 없음)
- Fixtures: `testdata/using_for_v11/{param_receiver, state_and_param,
  anonymous_param}.sol`.
- 테스트: `using_for_v11_test.go` 3 함수:
  - `TestUsingForV11_ParamReceiverDispatch` — 1 EdgeCalls
    (Calc.double → Math.times) for `x.times(2)` where x is uint256
    parameter.
  - `TestUsingForV11_StateAndParamMixed` — 같은 library 가 state-var
    와 parameter 모두에서 dispatch 됨. 두 path 가 서로 mask 안 함.
  - `TestUsingForV11_AnonymousParamSkipped` — name 없는 parameter 는
    paramTypes 인덱스에 진입 안 함 (anonymous receiver 가 매칭될 일
    없음, false-positive 가드).
- 회귀: 25/25 PASS, vet clean. §7.0 Go regression: Sol-only 변경.
- 가시화: EdgeCalls 가족 — V1.0 와 같이 viewer 추가 작업 없음.

V1.1 carry-over (V1.2+ follow-up)
- Return-value chaining (`foo().add(x)`): 정적 추론 인프라 필요
  (call-expression 의 return type 추적). **V1.3+**
- Free-function form: 별도 AST (using_alias). **V1.3+**
- File-level using directive (module-scope binding). **grammar 한계
  (v1.2.13 ERROR-node) — V1.x grammar 업그레이드 후 진입**
- Inherited using directive (base contract → child 상속). **V1.2 LANDED
  2026-05-12** (아래 블록).

#### LANDED 2026-05-12 (W-C W6 V1.2 — inherited using directive)

- 구현 추가:
  - `internal/parse/solidity/resolve.go`: Pass 2 binding 사전 빌드
    loop 직후, 모든 container 에 대해 inheritance graph 의 ancestors
    를 BFS 로 순회하면서 각 ancestor 의 bindings 를 descendant 에
    merge. child-scope 의 typeName entry 는 보존 (Solidity scoping
    semantics — local declaration shadows inherited).
  - cycle 방어: visited set per child 로 inheritance loop 방지.
  - parents adjacency 재사용: W1 inheritance graph 의 결과물 `parents`
    map 그대로 활용 — 새 인프라 없이 V1.2 처리.
- 구현 제거 (V1.2 attempted file-level using 의 revert):
  - 처음 V1.2 = file-level using directive 로 시작했으나 tree-sitter-
    solidity v1.2.13 grammar 가 `using LibName for T;` (0.8.13+
    source_file 직접 child) 를 ERROR-node 로 parse 함을 AST dump 로
    확인 (cmd_probe 임시 도구 사용 후 제거).
  - 작성한 `runUsingForFile` / `dispatchKindUsingForFile` / fan-out
    로직 전부 revert. spec/queries.go 에 grammar 한계 노트만 보존.
  - file-level 는 grammar 업그레이드 시 별도 작업으로 진입.
- Architecture (V1.2 BFS pseudocode):
  ```
  for each childID in containerNameByID:
      visited = {childID}
      queue = parents[childID]
      while queue not empty:
          ancestorID = queue.pop_front()
          if ancestorID in visited: continue
          visited.add(ancestorID)
          for (typeName, libName) in bindings[ancestorID]:
              if typeName not in bindings[childID]:
                  bindings[childID][typeName] = libName
          queue.extend(parents[ancestorID])
  ```
- Confidence: V1.2 propagation 은 binding map 만 채움 — EdgeUsesFor 는
  parent 의 declaration site 에만 emit (Child 에 synthetic edge 안
  만듦). 의미: graph 가 "어디 binding 이 declared 됐나" 를 정확히
  표현하고, 동시에 dispatch resolution 은 inherited binding 도 인식.
- Fixtures: `testdata/using_for_v12/{inherited_basic, inherited_multi_level,
  inherited_child_overrides}.sol`.
- 테스트: `using_for_v12_test.go` 3 함수:
  - `TestUsingForV12_InheritedBasic` — single-level: Child.bump →
    ParentLib.inc 검증. EdgeUsesFor 는 Parent → ParentLib 만 (Child
    에 synthetic 없음).
  - `TestUsingForV12_InheritedMultiLevel` — Grand → Parent → Child
    transitive BFS: Parent.tap + Child.tap2 모두 GrandLib.tap 으로
    resolve.
  - `TestUsingForV12_InheritedChildOverrides` — child-scope binding
    이 inherited binding 을 shadow 함을 검증 (Solidity scoping).
- 회귀: 25/25 PASS, vet clean. §7.0 Go regression: Sol-only.

V1.2 carry-over (V1.3+ follow-up)
- Return-value chaining (`foo().add(x)`). **V1.3 LANDED 2026-05-12**
  (아래 블록).
- Free-function form `using {f1, f2} for T`. **grammar 한계 — V1.x
  업그레이드 후 진입 (V1.3 시도 시 `{Math.add, Math.sub}` brace shape
  가 ERROR-node 로 parse 됨을 AST dump 로 확인).**
- File-level using directive (0.8.13+) — grammar 업그레이드 후.

#### LANDED 2026-05-12 (W-C W6 V1.3 — return-value chaining)

V1.3 진행 중 발견 — V1.3 첫 후보였던 free-function form 도 file-level
처럼 grammar 한계 (`using {Math.add, Math.sub} for T;` 의 brace shape
가 ERROR-node 로 parse 됨, AST dump 검증). V1.3 scope 를 **return-value
chaining** 으로 재설정.

- 구현 추가:
  - `internal/parse/solidity/overrides.go::runFunctionDecl`:
    `emitFunctionReturnMetaPending` 호출 추가. function_definition 의
    `return_type` field (`return_type_definition`) 의 첫 `parameter`
    child 에서 type field 추출 → `dispatchKindUsingForFnReturn`
    PendingRef emit. multi-return tuple 은 첫 슬롯만 (V0).
  - `internal/parse/solidity/using_for.go`: `matchChainedMethodCall`
    predicate 신규 — `<identifier>(...).<method>(...)` shape 매칭.
    inner identifier 가 plain function name 인 경우만 (Type cast
    `IFoo(addr).bar()` 는 W3 의 책임 — 그쪽이 먼저 매칭). 새
    `dispatchKindUsingForChainCall` 상수 + runUsingForCalls 분기.
  - `internal/parse/solidity/resolve.go`:
    - 신규 `funcReturnTypeMap` 타입 (funcID → first-return typeName).
    - Pass 2 사전 빌드 loop 의 switch 에 `using_for_fn_return` case
      추가 — funcReturnTypes 채움.
    - main loop 의 silent-skip 분기에 `using_for_fn_return` 추가.
    - 신규 `resolveUsingForChainCallRef` (5-step chain): funcID →
      containerID → innerFuncID (qname `<container>.<innerFn>` lookup)
      → returnType (funcReturnTypes) → libraryName (bindings) →
      libraryFunctionID (funcByQName).
- 5-step resolution chain (V1.3 chained-call only):
  1. funcID → containerID (containerIDByFuncID, 재사용)
  2. innerFnName → innerFuncID (funcByQName, same-contract qname 우선)
  3. innerFuncID → returnTypeName (funcReturnTypes — V1.3 신규)
  4. (containerID, returnTypeName) → libraryName (bindings + `*` fallback)
  5. `<libraryName>.<methodName>` → libraryFunctionID
- Confidence: ConfExtracted (caller + library 같은 file) /
  ConfInferred (cross-file). 내부 function 의 file 은 confidence 영향
  안 줌 — drop 으로만 uncertainty 표현.
- Fixtures: `testdata/using_for_v13/{return_chain_basic,
  return_chain_no_binding, return_chain_unknown_fn}.sol`.
- 테스트: `using_for_v13_test.go` 3 함수:
  - `TestUsingForV13_ReturnChainBasic` — Vault.run → ChainLib.add
    via factory()'s uint256 return.
  - `TestUsingForV13_ReturnChainNoBinding` — return type 가 binding
    엔트리 없으면 drop (false-positive 가드).
  - `TestUsingForV13_ReturnChainUnknownFn` — inner identifier 가
    declared function 가 아니면 drop.
- 회귀: 25/25 PASS, vet clean. §7.0 Go regression: Sol-only.

V1.3 carry-over (V1.4+ follow-up)
- Cross-contract chaining (`obj.foo().bar()`): inner expression 이
  member_expression 인 경우. receiver chain 추적 필요.
- Multi-return tuple (`returns (uint256, address)` 의 두번째 슬롯
  receiver 로 활용).
- Free-function form / file-level using — grammar 업그레이드 대기.

#### 4.6.5 §3.5 갱신 예정

본 W6 land 시 §3.5 noise control 의 "Library call ... V0 에서는 단순
calls (resolve 실패 → drop) ... 별도 spec" 항목 → "W6 로 binding 자체는
가시화 (EdgeUsesFor). method call dispatch resolution 은 V1 follow-up"
으로 갱신.

#### 4.6.6 V0 한계

- **Method call dispatch resolution 미구현**: V0 에서는 binding
  declaration 만 emit. `balance.add(...)` 같은 호출은 NodeCallSite 는
  emit 되지만 EdgeCalls 는 SafeMath.add 로 연결 안 됨. V1 에서 추가
  예정. 우회: 사용자가 contract 의 EdgeUsesFor 들을 본 뒤 해당 library
  의 함수를 직접 확인 (graphify-style multi-hop query).
- **Receiver type 추론 인프라 (V1)**: state variable 의 declared type +
  function parameter type 인덱스. NodeField 의 Signature 필드 또는
  parser-side side-channel (W-A 의 FuncFieldTouches 패턴) 도입 필요.
- **Free function using (`using {f1, f2} for T`)**: Solidity 0.8.13+
  문법. V0 에서는 library name 단위로 binding emit — 개별 함수 list
  까지 분해 안 함 (한 EdgeUsesFor per library / per directive).
- **`using for *` 와 stdlib types (uint, address)**: V0 에서는 `*`
  자체를 type 정보로 graph 표기 안 함. 같은 (contract, library) 쌍에
  대해 specific binding 과 wildcard binding 이 모두 있으면 두 개의
  EdgeUsesFor 가 emit (dedup 안 함).
- **Inherited using directive**: Solidity 0.8.13+ `internal using` 은
  base contract 에서 child 로 상속됨. V0 에서는 contract 자체의
  binding 만 집계, 상속 binding 무시 → drop. follow-up.

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
