# Symbol Identity — implementation status & remaining work (ckg)

Companion to the design contract in **code-knowledge-system
`docs/symbol-identity-design.md`** (merged, PR #16). This file tracks the ckg
implementation and the work still to do across ckg / ckv / cks.

> **Decision vs status:** the *decision* (what canonical_id is, identity format,
> exact-resolution rule) is recorded in **[adr/0001-canonical-symbol-id.md](adr/0001-canonical-symbol-id.md)**.
> This file holds only the *live implementation status* — keep status here, keep
> the rationale in the ADR.

> **Status note (verified 2026-06-15):** the Phase 1 foundation branch
> `feat/canonical-symbol-id` was **merged into `main` via PR #21** (`1a9698c`,
> 2026-06-12) — this doc landed in that same PR. Continue from `main`, not from
> the (now-merged) branch. The status markers below were re-verified against the
> `main` tree on 2026-06-15: ✅ done · ❌ not started · 🔶 partial · ⏸ runtime task.

## Where we are

**Phase 0 — design:** done (merged in cks).

**Phase 1 — ckg canonical id: FOUNDATION done** (merged to `main`, PR #21).
Implemented and tested (build + `internal/persist` + `internal/parse/...` green):

- `pkg/types/node.go` — added `Node.CanonicalID` (json `canonical_id,omitempty`):
  the globally-unique, import-path-qualified identity (e.g.
  `github.com/ethereum/go-ethereum/core/vm.(*EVM).Call`). `QualifiedName` stays the
  short, suffix-searchable display form.
- `internal/parse/golang/declarations.go` — `goCanonicalID(obj go/types.Object)`
  builds the id for **Go functions and methods** (receiver-pointer aware); wired
  into `visitFuncDecl` (set only when `typesInfo != nil`).
- `internal/persist/schema.sql` — `nodes.canonical_id TEXT` column (marked
  "schema 1.16" **in the SQL comment only** — the cache-gating
  `buildpipe/cache.go SchemaVersion` constant is still `"1.15"`; see Phase 1
  item 4, which is therefore a prerequisite for item 5, not just a signal).
- `internal/persist/sqlite_migrate.go` — `ensureCanonicalIDColumn` (idempotent
  ALTER, mirrors attrs/search_tokens) wired into `Migrate()`.
- `internal/persist/sqlite_writer.go` / `sqlite_reader.go` — `canonical_id`
  round-trips through `InsertNodes` and `GetNode`.
- `internal/parse/golang/resolve_test.go` —
  `TestCanonicalID_DistinguishesSameNameAcrossPackages` proves two same-named
  methods in different packages get distinct ids.

Everything is **additive**: `qualified_name`, node IDs (`sha256(qname|lang|startByte)`),
edges, and all existing consumers are unchanged. No reindex has been run yet, so the
live `data/ckg-stablenet/graph.db` does **not** carry `canonical_id` values yet.

## Remaining work

### Phase 1 (ckg) — finish canonical id

> **Progress (branch `feat/canonical-symbol-id-phase1`, 2026-06-17):** items 1,
> 2, 3, 4, 6 done (Go + Solidity + TypeScript + proto canonical_id, exact
> resolution, schema bump, tests). Remaining: item 5 (reindex — now unblocked),
> item 7 (Postgres parity — deferred, status quo per decision). Schema baseline
> had moved to 1.18 (PR #23), so the bump landed as **1.18 → 1.19** (not 1.16).
> New gap found: **Postgres has no `canonical_id` column** (sqlite-only) — see
> item 7.

1. ✅ **Wire the remaining Go node kinds** in `declarations.go` — done. A shared
   `setLastCanonicalID` helper now sets `canonical_id` in `emitTypeSpec`
   (types/structs/interfaces), `emitFields` (`<importpath>.<Type>.<Field>`,
   derived from the owning type's id), `emitInterfaceMethod`
   (`<importpath>.<Interface>.<Method>`, distinct from concrete impls), and
   `emitValueSpec` (package const/var). Covered by `TestCanonicalID_AllGoNodeKinds`.
2. ✅ **Other languages** — done. All three tree-sitter/custom parsers now set
   `canonical_id` with the relative file path as qualifier (no import path):
   Solidity `<relpath>:<Contract>.<func>(<paramTypes>)` (param-type signature
   separates overloads; file path separates v1/v2 dirs — `runFunctionDecl` +
   `funcParamSignature`, post-pass for other kinds), TypeScript `<relpath>:<qname>`
   (inline in `declarations.go`), proto `<relpath>:<qname>` (post-pass in
   `visitor.go`). Covered by `TestCanonicalID_SolidityOverloads` + the refreshed
   Solidity/TS golden snapshots (which now include canonical_id).
3. ✅ **Canonical resolution** — done for ckg. `FindByCanonicalID` added to
   `StoreReader` + sqlite (+ Postgres stub). The traversal family
   (find_callers/find_callees/get_subgraph/change_history) now resolves a
   canonical-id seed exactly via `resolveSeed` step 0 (`pkg/mcphandlers/helpers.go`),
   and `canonical_id` is surfaced in tool output so agents can feed it back.
   The multi-match=**error** guard was already in place from PR #23 (`resolveSeed`
   returns `ambiguous`+candidates, never a silent pick — verified by
   `TestResolveSeed`); forward call edges are already qualified by PR #23's typed
   resolver, so no bare-name collisions there. Covered by the new canonical
   subtest in `TestResolveSeed` + `TestFindByCanonicalID`.
   *Optional future refinement:* traverse by node ID (not resolved qname) for
   absolute precision when several nodes share one qualified_name.
4. ✅ **Schema version bump** — done. `const SchemaVersion` in
   `internal/buildpipe/cache.go` bumped **1.18 → 1.19** (the cache-key
   contributor; invalidates the build cache so a reindex repopulates
   `canonical_id`). Prerequisite for item 5, now satisfied.
5. ⏸ **Reindex go-stablenet** (graph build is LLM-free, minutes; item 4 done)
   to a scratch out first, validate that real collisions resolve uniquely (e.g. a
   `Size` method in `core/types` vs `consensus/wbft/core` gets distinct canonical
   ids), then to the shared `data/ckg-stablenet`.
6. 🔶 **Tests:** `TestCanonicalID_DistinguishesSameNameAcrossPackages` plus new
   `TestCanonicalID_AllGoNodeKinds` (type/field/interface-method/const/var +
   interface-vs-concrete) and `TestFindByCanonicalID`. Still need: Solidity
   overload (blocked on item 2).
7. ❌ **Postgres `canonical_id` parity** (newly found): the Postgres schema and
   `pgNodeColumns` carry no `canonical_id` column — PR #21 added it to sqlite
   only. `pgStore.FindByCanonicalID` is a documented not-found stub. Add the
   column + writer/reader round-trip for Postgres-backed graphs.

### Phase 2 (ckv = `../code-knowledge-vector`, separate repo) — additive canonical field (no re-embed)
- Add an additive `canonical_id` to `pkg/types.Chunk` and the search `Hit`
  (omitempty), populated from the aligned ckg node. Alignment is already
  **positional** (`internal/ckgalign`), so it is name-agnostic — do NOT change the
  embed-text prefix, so **no re-embed** is needed. Migrate in place
  (`cmd/ckv/migrate.go` runner + reparse). Tests: every aligned chunk carries the
  ckg canonical id; vectors byte-identical.

### Phase 3 (cks = `../code-knowledge-system`, separate repo) — exact resolution + two anchor kinds + data migration
- `internal/ckgclient/real.go`: resolve by canonical id; drop the `defs[0]`
  fallback in `resolveQname`/`resolveNodeID`/`resolveSeedFile` (multi-match = error
  for the traversal family).
- MCP tool docs (`internal/mcp/graph.go`, `analysis.go`) advertise a
  `consensus.wbft.Finalize` form ckg does not store — fix them to the real
  identity.
- Domain-knowledge anchor schema (`docs/domain-knowledge/shared/entry.schema.yaml`):
  add `kind: def | loc`. `def` requires a uniquely-resolvable symbol and
  `line == definition line`; `loc` carries `enclosing_symbol` + arbitrary `line`
  (no def-line rule). Teach `cks-anchor-refresh` (line==def for `def`,
  range-containment for `loc`, never repoint), give `cks-inventory-check` a ckg
  handle to assert each `def` symbol resolves uniquely, and update
  `internal/domainexport` rendering per kind.
- **Data migration:** the go-stablenet entries (146+ symbol+line anchors, growing —
  another session added validator-set / gov / storage-slot / minter entries).
  ~1 in 6 are `loc`/`enclosing_symbol`; descriptive symbol strings (e.g.
  `"ValidateTransaction (Berlin gate)"`) move into `reason`.

**Unchanged across all phases:** `pkg/contract.Citation` (file:line), the composer
pipeline, ckv embeddings, and the ckg↔ckv positional alignment.

## How to resume
The foundation branch is merged, so work from `main`:
```
git switch main && git pull             # PR #21 already merged here
go build ./... && go test ./internal/parse/... ./internal/persist/...
```
Pick up at "Phase 1 remaining" item 1. Tackle item 4 (SchemaVersion bump)
before item 5 (reindex) — the reindex is a no-op for `canonical_id` until the
cache key changes.

## Caveats
- **Concurrent sessions** are actively editing ckg/ckv/cks. Sync at phase
  boundaries and keep PRs small to avoid rebase churn. (The foundation already
  merged to `main` via PR #21; branch each new slice off current `main`.)
- The live cks MCP serves the graph it loaded at process start; a rebuilt
  `graph.db` is only picked up after a cks restart.
- Estimated remaining effort: ~6–9 focused implementation+test blocks (~4–8h of
  work), plus per-phase review/merge and concurrent-session coordination.
