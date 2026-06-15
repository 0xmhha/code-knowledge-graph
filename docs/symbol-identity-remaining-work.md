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
1. ❌ **Wire the remaining Go node kinds** in `declarations.go` (currently only
   func/method, set in `visitFuncDecl:275`; `emitTypeSpec:156` and
   `emitValueSpec:226` do NOT call `goCanonicalID`): types/structs/interfaces
   (`emitTypeSpec`), fields (need the enclosing type to qualify:
   `<pkg>.<Type>.<Field>`), package-level const/var (`emitValueSpec` — these ARE
   emitted as nodes but resolution drops them today), and interface methods
   (distinct id from each concrete impl). Use `v.typesInfo.ObjectOf(ident)` at
   each site; extend `goCanonicalID` for the field case (its `*types.Var` has no
   receiver — derive the owner type).
2. ❌ **Other languages:** Solidity (`<dir>/<Contract>.<func>(<paramTypes>)` — the
   parameter-type signature is required to separate overloads, and the version
   directory to separate v1/v2; see `internal/parse/solidity/`), TypeScript, and
   proto. They have no Go import path, so the file/package path is the qualifier.
   Verified: `CanonicalID` is set in **no** parser outside `golang/`.
3. ❌ **Canonical resolution:** add an exact `FindByCanonicalID` path in
   `internal/persist/sqlite_reader.go` (none exists today) and make the traversal
   family (find_callers/get_subgraph/impact_analysis) resolve on the canonical id;
   a short-name lookup that matches >1 node must be an **error**, not the silent
   pick it is today (`FindSymbol:315` does `qualified_name` LIKE matching and
   returns up to `LIMIT 100` with no multi-match guard). Also update the Pass-2
   call resolver (`internal/parse/golang/resolve.go`) to key on canonical id where
   available (note `qualifiedStaticTarget` in `statements.go:268` already uses
   go/types for forward edges — align it to emit canonical-id-form targets).
4. ❌ **Schema version bump** to 1.16: change `const SchemaVersion` in
   `internal/buildpipe/cache.go:108` from `"1.15"` to `"1.16"`. This is the
   cache-key contributor — bumping invalidates the build cache so a reindex
   repopulates `canonical_id`. **Prerequisite for item 5:** without it the cache
   is not invalidated and a rebuild will reuse stale rows that have no
   `canonical_id`.
5. ⏸ **Reindex go-stablenet** (graph build is LLM-free, minutes; do item 4 first)
   to a scratch out first, validate that real collisions resolve uniquely (e.g. a
   `Size` method in `core/types` vs `consensus/wbft/core` gets distinct canonical
   ids), then to the shared `data/ckg-stablenet`.
6. 🔶 **Tests:** only `TestCanonicalID_DistinguishesSameNameAcrossPackages`
   (`resolve_test.go:15`) exists today. Still need: Solidity overload, interface
   vs concrete, const/var, field.

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
