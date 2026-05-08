# Track C — CKS 6-Graph Detector Gap Diagnosis

> Scope: G2 / G3 / G4 / G5 axes. Identify why each 0-count or sparse edge type
> is missing in the self-graph snapshot at `/tmp/ckg-self/graph.db`, classify
> root cause (no-detector / detector-bug / no-codebase-pattern), and recommend
> priority. Diagnosis only — no implementation.
>
> Reference snapshot: 22 distinct edge types observed; 6 edge types defined in
> `pkg/types/enums.go` + `web/viewer-next/src/lib/edges.ts:GRAPH_GROUPS` are
> entirely absent (`invokes`, `cancellation_path`, `rpc_calls`,
> `handles_message`, `uses_type`, `instantiates`).
>
> Method: `grep` for emit sites in `internal/parse/{golang,typescript,solidity}/`,
> cross-checked against actual codebase usage to separate "no detector" from
> "detector OK, no pattern in this corpus".

---

## 1. Summary Table

| Edge type        | Group | Count | Detector status       | Root cause                                                                                                | Fix size | Expected gain (self) | Priority |
|------------------|-------|-------|-----------------------|-----------------------------------------------------------------------------------------------------------|----------|----------------------|----------|
| `uses_type`      | G2    | 0     | **Missing entirely**  | No emit site in any parser; spec-listed but never implemented                                             | M        | 1k–3k (Go)           | **P0**   |
| `instantiates`   | G2    | 0     | **Missing entirely**  | No emit site in any parser                                                                                | S        | 100–300 (Go)         | **P1**   |
| `invokes`        | G3    | 0     | **Missing semantic split** | `parsePendingFromCall` always emits `EdgeCalls`; never differentiates interface dispatch vs static call | S–M      | redirects 100–300 of current `calls` | **P1** |
| `extends`        | G2    | 2     | Go OK; TS/Sol missing | Go detector at `implements.go:172-198` works (2 = `Store extends StoreReader/StoreWriter`); TS query lacks `class_heritage`/`extends_clause` capture; Solidity has no `is`-clause query | M        | +5–15 (TS Props)     | P2       |
| `implements`     | G2    | 20    | Go OK; TS/Sol missing | Go detector at `implements.go:125-165` saturates Go interfaces (~10 user-defined ifaces × satisfiers ≈ 20); TS has no `implements_clause` query | M | +0–10 (TS classes implement few ifaces) | P2 |
| `cancellation_path` | G3 | 0   | OK; codebase has none | Detector at `context_paths.go:53-65` is wired (parser.go:133, 146); grep confirms zero `context.WithCancel` usage in non-test code → **(b)** | none | 0 (genuine) | — |
| `timeout_path`   | G3    | 7     | OK; saturated         | All 3 production + 4 test `context.WithTimeout` sites detected; matches expected | none | 0 | — |
| `handles_message`| G5    | 0     | OK; codebase has none | Detector at `distributed.go:468-496` matches `func (T) M(args, *reply) error` shape; codebase doesn't use net/rpc | none | 0 (genuine) | — |
| `rpc_calls`      | G5    | 0     | Partial (net/rpc only) | Detector at `distributed.go:580-624` only catches `client.Call("Service.Method", ...)` literal-string form; codebase uses neither net/rpc nor gRPC | none in self; M for gRPC follow-up | 0 in self; ~50–200 on gRPC repos | P2 |
| `listens_on`     | G5    | 9     | OK; saturated         | All 9 `s.mux.HandleFunc` calls in `internal/server/server.go:66-74` detected | none | 0 | — |
| `binds_to`       | G5    | 2     | OK (Sol→TS only)      | `internal/link/xlang.go:38-43` matches Solidity `Vault` contract → 2 TS `Vault` classes (test fixture + impl); no Go binding edges defined | S–M for Go bindings | 0 in self | P3 |
| `spawns`         | G4    | 5     | OK                    | `statements.go:79-82`; codebase has 5 `go func` literals total | none | 0 | — |
| `sends_to`       | G4    | 3     | OK                    | `statements.go:90-100`, `concurrency.go:531-535`; codebase has 3 channel sends | none | 0 | — |
| `recvs_from`     | G4    | 4     | OK                    | `statements.go:108-118`, `concurrency.go:540-548`; codebase has 4 channel recvs | none | 0 | — |
| `acquires_lock`  | G4    | 2     | **Buggy**             | Detector at `concurrency.go:320-345` correct, but `statements.go:85` returns `false` from GoStmt → **lock calls inside `go func() { ... }()` literals never visited**. `language_runners.go:81-91` has 2 `errMu.Lock()` sites inside a goroutine literal that are silently dropped. | S | +2 acquires + +2 releases | **P1** |
| `releases_lock`  | G4    | 2     | **Buggy**             | Same `GoStmt` walk-skip bug as `acquires_lock`                                                            | S        | +2                   | **P1**   |
| `accessed_under_lock` | G4 | 3   | OK (limited by design) | `concurrency_underlock.go` only fires per-function: requires (a) at least one Lock/RLock call in body, (b) typesInfo, (c) field references inside body. Codebase has only 1 struct holding a mutex (`solidity.Parser`), so 3 fields × 1 mutex = 3 edges as expected. | none | 0 | — |

---

## 2. Per-Edge Detail

### 2.1 `uses_type` (G2) — **P0**

- **Current state**: 0 edges across all 3 languages.
- **Detector status**: **No emit site exists.** Confirmed by
  `grep -rn "EdgeUsesType" internal/parse/ --include="*.go" | grep -v _test.go`
  → empty.
- **Definition reference**: `pkg/types/enums.go:96` declares the edge type;
  `web/viewer-next/src/lib/edges.ts:50, 173` registers it under G2; spec
  §5.2 lists it; nothing emits it.
- **What it should match (Go)**:
  - Function/method parameter types: `func F(x pkg.Type)` →
    `Function uses_type pkg.Type` (one edge per distinct param/return type).
  - Struct field types: `type S struct { f pkg.Type }` →
    `Struct uses_type pkg.Type`.
  - Variable / constant declared type.
  - Type assertion: `x.(pkg.Type)`.
  - Best implemented as a post-pass that walks `types.Info.Types` /
    `types.Info.Defs` and, for each named type referenced in a
    Function/Method/Field/Variable signature, emits one edge to the existing
    qname-resolved node (skip self-reference, skip primitive types).
- **Self-graph estimate**: 2782 functions × ~2 distinct param types average +
  349 fields with non-primitive types ≈ 1k–3k edges.
- **Difficulty**: M. Requires new go/types pass (similar shape to
  `EmitImplementsEdges` at `implements.go:47-202`). TS/Sol versions need
  tree-sitter type queries — separate work.
- **Recommended fix**: Implement Go-only first; add it to
  `LoadAndResolve` post-Resolve hook alongside `EmitImplementsEdges`
  (`resolve.go:140`). Defer TS/Sol.

### 2.2 `instantiates` (G2) — **P1**

- **Current state**: 0 edges.
- **Detector status**: **No emit site.** Confirmed by
  `grep -rn "EdgeInstantiates" internal/parse/`.
- **What it should match (Go)**:
  - Composite literal: `Type{...}` or `&Type{...}` → caller `instantiates` Type.
  - `new(Type)` calls.
  - `make(Type)` for non-channel types (channels already have `Channel` nodes).
- **Self-graph estimate**: ~100–300 edges (struct literals are common but
  many target stdlib types we don't have nodes for, so the resolver-drop rate
  will be high).
- **Difficulty**: S. AST pattern is straightforward
  (`*ast.CompositeLit` with `*ast.Ident` / `*ast.SelectorExpr` Type). Resolve
  via existing qname → node ID map (same shape as `parsePendingFromCall`).
- **Recommended fix**: Add to `statements.go`'s body walker as a new
  `*ast.CompositeLit` case alongside the existing CallExpr handling.
- **Caveat**: Emitting only on **named** types (skip slice/map/struct literals
  with anonymous element types) keeps the noise floor down.

### 2.3 `invokes` (G3) — **P1**

- **Current state**: 0 edges; `calls` has 2,060.
- **Detector status**: **Semantic split missing.** `statements.go:174-184`
  hard-codes `EdgeType: types.EdgeCalls` for every CallExpr regardless of
  whether the callee is a static function or an interface method.
- **Why it matters**: The viewer styles `calls` (`color: 0xffffff`) and
  `invokes` (`color: 0xffaa00`) differently
  (`web/viewer-next/src/lib/edges.ts:37-38`) so virtual dispatch is currently
  visually merged with direct calls.
- **What it should match**: At resolve time
  (`resolve.go:56-90`), when `pr.TargetQName` resolves to a Method on an
  Interface (not a concrete type), emit `EdgeInvokes` instead of `EdgeCalls`.
  Equivalent typed signal: in `parsePendingFromCall`, peek the CallExpr's
  resolved `*types.Selection` — if `Recv()` is an interface, mark the pending
  ref with `EdgeInvokes`.
- **Self-graph estimate**: would redirect 100–300 of the existing 2,060
  `calls` edges into `invokes` (rough estimate: parser/store/validator
  interface dispatch).
- **Difficulty**: S–M. Cleanest path is to thread `typesInfo` into
  `parsePendingFromCall` and decide at AST time. The resolve.go fallback
  (when an unresolved pending ref might point to either) can stay `calls`
  since AMBIGUOUS handling is out of scope.

### 2.4 `extends` (G2) — P2

- **Current state**: 2 edges (interface embedding only, both in
  `internal/persist/store_interface.go`: `Store extends StoreReader`,
  `Store extends StoreWriter`).
- **Detector status (Go)**: OK. `implements.go:172-198` walks
  `iface.NumEmbeddeds()` for every interface and emits the extend edge.
- **Detector status (TS)**: Missing. `queries.go:7` has only
  `interface_declaration name:` — no `class_heritage` / `extends_clause`
  capture, so TS class hierarchies (e.g. React component subclasses) emit
  zero `extends`.
- **Detector status (Solidity)**: Missing. No query for `is`-clause
  (`contract Foo is Bar`).
- **Self-graph estimate**: codebase has very few TS class hierarchies (the
  viewer is mostly functional React); +5–15 edges at most. Solidity test
  fixtures might add ~3–5.
- **Difficulty**: M for TS (need tree-sitter `class_heritage` query +
  visitor extension to emit pending refs), M for Sol.

### 2.5 `implements` (G2) — P2

- **Current state**: 20 edges (Go-only).
- **Detector status (Go)**: OK and saturated. Detector at
  `implements.go:125-165` checks every concrete-type × interface pair via
  `gotypes.Implements`. The 20 edges cover:
  - 4 stores → `audit.store` (pgStore, sqliteStore, fakeStore, mockStoreReader)
  - 4 parsers → `parse.Parser` (golang, solidity, typescript, parse_test.fakeParser)
  - 4 stores → `persist.StoreReader/StoreWriter` (pg, sqlite × 2)
  - 2 stores → `persist.Store` (pg, sqlite)
  - other singletons (TopicTree, Okapi, APIClient/CLIClient, two Validators)

  This is correct: only ~10 user-defined Go interfaces have multiple impls,
  and the empty-interface skip at `implements.go:128-131` prevents the O(N)
  noise from `interface{}`.
- **Detector status (TS)**: Missing — no `implements_clause` query.
- **Self-graph estimate (TS)**: codebase is mostly hooks; near-zero classes
  implement interfaces. +0–5 edges.
- **Difficulty**: M for TS only.

### 2.6 `cancellation_path` (G3) — no fix needed

- **Current state**: 0 edges.
- **Detector status**: **Wired and working.** `context_paths.go:53-65`
  invoked from `parser.go:133` (typed mode) and `parser.go:146` (AST-only).
  `context_paths.go:139-147` maps `WithCancel` / `WithCancelCause` →
  `EdgeCancellationPath`.
- **Verification**: `grep -rn "context\.WithCancel\|context\.WithCancelCause"
  --include="*.go"` excluding testdata returns **0 hits**. The codebase
  genuinely doesn't use context cancellation outside the detector's own
  fixture.
- **Conclusion**: Case **(b)** from the original ticket — detector is fine,
  the codebase has no pattern. Re-validation will happen automatically on
  any repo that uses `context.WithCancel` (e.g. testdata fixture
  `internal/parse/golang/testdata/context_paths/fixture.go:20-26`
  exercises both variants).

### 2.7 `handles_message` (G5) — no fix needed

- **Current state**: 0 edges.
- **Detector status**: OK at `distributed.go:468-496`. Matches the
  `func (T) Method(args A, reply *R) error` shape with INFERRED confidence.
- **Why 0 in self-graph**: codebase doesn't use `net/rpc`; no Go method has
  this exact signature shape (the closest are HTTP handlers which use
  `(http.ResponseWriter, *http.Request)` — different signature).
- **Future expansion candidate**: gRPC server methods (`func (s *server)
  Foo(ctx context.Context, req *pb.FooRequest) (*pb.FooResponse, error)`)
  match a different shape. Adding a second matcher would be a separate
  follow-up — out of scope here.

### 2.8 `rpc_calls` (G5) — P2 (defer)

- **Current state**: 0 edges.
- **Detector status**: Partial. `distributed.go:580-624` matches only
  `client.Call("Service.Method", args, reply)` with a string literal first
  arg. Documented gap: gRPC stub calls (`stub.RpcMethod(ctx, req)`) are NOT
  detected (`distributed.go:32-36`).
- **Why 0 in self-graph**: Codebase uses neither net/rpc nor gRPC.
- **Recommended action**: defer. Adding gRPC detection requires recognising
  the auto-generated `XClient` interface, which means cross-package
  `types.Info` traversal — heavy work for a corpus that doesn't exercise it.
  Re-prioritise when a real gRPC repo enters the eval set.

### 2.9 `acquires_lock` / `releases_lock` (G4) — **P1 — buggy detector**

- **Current state**: 2 + 2 edges (only on `solidity.Parser.abiMu`).
- **Bug**: `statements.go:77-85` handles `*ast.GoStmt` and explicitly
  `return false` to prevent re-walking the goroutine body via the outer
  `ast.Inspect`. The body is then re-walked only by
  `emitGoroutineChannelEdges` (`concurrency.go:516-553`), which handles
  ONLY `SendStmt` and `UnaryExpr` — **not `CallExpr`**. So
  `mu.Lock()` / `mu.Unlock()` calls inside `go func() { ... }()` literals
  are silently skipped.
- **Confirmation**: `internal/buildpipe/language_runners.go:81-91` has 2
  `errMu.Lock()` + 2 `errMu.Unlock()` sites inside a goroutine literal. The
  Mutex node `buildpipe.parseConcurrent.errMu` IS emitted (the
  `scanFuncBodyForMutexLocals` walker is independent), but no lock edges
  attach to it.
  ```sql
  SELECT n.qualified_name FROM nodes n WHERE n.type='Mutex';
  -- buildpipe.parseConcurrent.errMu          (no acquire/release edges)
  -- solidity.Parser.abiMu#mutex              (2 acquire + 2 release)
  ```
- **Recommended fix**: Either
  - **(a)** drop the `return false` in the GoStmt case so the outer
    `ast.Inspect` recurses into the goroutine body, OR
  - **(b)** extend `emitGoroutineChannelEdges` to also emit lock edges for
    CallExprs inside the goroutine.

  (a) is simpler but requires verifying `emitGoroutineChannelEdges` doesn't
  double-walk channel sends. Looking at the code, `emitGoroutineChannelEdges`
  is the goroutine-anchored emitter (Src = goroutineID), while the outer
  walker emits with Src = parentFuncID. Removing `return false` would emit
  channel edges twice (once from the function, once from the goroutine),
  which is the original reason for the guard. Therefore (b) is safer.
- **Difficulty**: S (≤30 LOC).
- **Expected gain (self)**: +2 acquires + +2 releases. Plus, the
  `accessed_under_lock` pass might newly fire on `errMu` itself (it's
  currently a local var, not a struct field, so `fieldNodeIDs` won't have
  it — no cascading gain there).

### 2.10 Solidity-only edges (`writes_mapping`, `emits_event`, `has_modifier`)

Not in original scope (Track C is G2/G3/G4/G5), but noted: these fire only
in `testdata/synthetic` Vault fixtures. Self-graph counts (3, 1, 1) match
fixture content; no detector issue.

---

## 3. Priority Recommendations

### P0 — implement now

1. **`uses_type` (Go)**: highest information density, biggest visible gap.
   Estimated 1k–3k edges. Required for G2 to be more than implements/extends.

### P1 — next iteration

2. **`acquires_lock` / `releases_lock` goroutine-body bug**: small fix,
   restores 4 edges + correctness for any repo with locking inside
   goroutines (extremely common pattern).
3. **`invokes` semantic split**: small change to resolve.go / pending-ref
   construction; immediate viewer benefit (orange vs white edges).
4. **`instantiates` (Go)**: complements `uses_type`; small detector.

### P2 — defer

5. **TS `extends` / `implements` queries**: low gain on this codebase,
   but valuable for any TS-heavy corpus.
6. **gRPC support for `rpc_calls` / `handles_message`**: out of scope for
   the current corpus; revisit when a gRPC repo lands in the eval set.

### P3 — investigate later / out of scope

7. `binds_to` Go bindings (Solidity ↔ Go): no precedent in current code.
8. `retry_path` (G3): detector intentionally not implemented
   (`context_paths.go:35-38`); waiting on a typed retry primitive.

---

## 4. Open Questions

1. **`uses_type` granularity**: should we emit one edge per *distinct* type
   referenced by a function (collapsed) or one per occurrence (with line
   numbers)? Per-occurrence triples the count and helps "where is this
   type used?" queries; per-distinct-type matches the implements/extends
   idiom. Recommend per-distinct (consistent with G2 family).
2. **`invokes` for typed function values**: when a Go func value is stored
   in a struct and called via `s.callback(...)`, is that `calls` or
   `invokes`? Spec is silent. Recommend `calls` (only interface-method
   dispatch counts as `invokes`) for consistency with the Java/Kotlin
   tradition the spec inherits from.
3. **Lock-edge fix strategy**: prefer (a) walk-restore or (b)
   emitGoroutineChannelEdges-extension? (a) is cleaner but risks duplicate
   channel edges; (b) is safer but adds a code path.
4. **Should `uses_type` also fire for unresolved (stdlib) types?** If
   `func F(r io.Reader)` references `io.Reader` and we have no node for
   `io.Reader` in the graph, do we (i) drop the edge, (ii) synthesise a
   placeholder Interface node `import:io.Reader`, or (iii) emit AMBIGUOUS
   to a sentinel? Recommend (i) for V0 (matches existing Resolve drop
   policy at `resolve.go:69-71`).

---

## Appendix: References

- Edge enum: `pkg/types/enums.go:91-163`
- Group/style mapping: `web/viewer-next/src/lib/edges.ts:29-198`
- Self-graph DB: `/tmp/ckg-self/graph.db` (snapshot)
- Detector files (Go):
  - `internal/parse/golang/declarations.go` — visitor entry
  - `internal/parse/golang/statements.go` — body walk (calls, channels, spawns)
  - `internal/parse/golang/concurrency.go` — Mutex nodes + lock edges
  - `internal/parse/golang/concurrency_underlock.go` — accessed_under_lock
  - `internal/parse/golang/distributed.go` — listens_on / handles_message / rpc_calls
  - `internal/parse/golang/context_paths.go` — timeout_path / cancellation_path
  - `internal/parse/golang/implements.go` — implements / extends post-pass
- Detector files (TS): `internal/parse/typescript/queries.go`,
  `internal/parse/typescript/declarations.go`
- Detector files (Sol): `internal/parse/solidity/queries.go`,
  `internal/parse/solidity/declarations.go`
- Cross-language link: `internal/link/xlang.go`
