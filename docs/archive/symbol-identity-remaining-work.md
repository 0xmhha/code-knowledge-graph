> **ARCHIVED 2026-06-30 — COMPLETE.** Every item (Phase 1 items 1–7, Tier A/B/C)
> is done or resolved. The decisions live in the ADRs:
> [`adr/0001-canonical-symbol-id.md`](../adr/0001-canonical-symbol-id.md) (identity),
> [`adr/0002-staged-graph-composition.md`](../adr/0002-staged-graph-composition.md)
> (deterministic build), [`adr/0003-deprecate-postgres-backend.md`](../adr/0003-deprecate-postgres-backend.md)
> (item 7 → deprecate Postgres). Kept for provenance; the "How to resume" /
> "remaining effort" prose below is historical and no longer accurate.

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
edges, and all existing consumers are unchanged. A validation reindex of
go-stablenet has been run on the Phase 1 branch (see "Validation results"); any
*shared* `EVAL_DB_ROOT` graph still needs a rebuild under schema 1.19 to carry
`canonical_id`.

## Next — prioritized work queue (2026-06-19)

Phase 1 (ckg) is done except item 7. The remaining effort, in priority order:

**Tier A — highest leverage (makes Phase 1 pay off; canonical_id only helps once
downstream *consumes* it). Separate repos / sessions.**
- **A1 — cks Phase 3** (`../code-knowledge-system`): 🔶 code+tooling done on
  branch `feat/canonical-id-resolution`. Commits: `6609d12` (dep bump +
  `FindByCanonicalID` adapter + canonical-first resolution dropping silent
  `defs[0]` + MCP-doc fix), `8425fa3` (anchor `kind: def|loc` struct + schema,
  additive), `f480de6` (`cks-anchor-refresh` never repoints loc), `a9b2282`
  (`cks-inventory-check --graph` asserts def-symbol uniqueness; `domainexport`
  renders per kind). **A1-3b 🔶 partial** (cks branch `feat/anchor-kind-migration`,
  `1990e77`): of 244 anchors (174 with symbol+line), classified against the
  go-stablenet ckg graph with the maxShift=15 def/loc threshold → **125 def**
  (kind absent = def), **4 loc** migrated (`StateTransition.TransitionDb`
  check-points + `EVM.Call:213`; symbol→enclosing_symbol + `kind: loc`), **64
  file-only**. The ~10 def-uniqueness "errors" were **mostly false positives** — the
  `--graph` check counted symbol matches globally, ignoring the anchor's `file`
  (`API.Status` is global-ambiguous clique-vs-wbft but unique in its file). Fixed
  file-aware in cks PR #23. **Genuine residue (small data follow-ups, human
  judgment):** 7 in-file ambiguities where a bare symbol suffix-matches a type +
  fields (`EpochInfo`→3, `WBFTExtra`, `Status`, `FeeDelegateDynamicFeeTx.FeePayer`)
  — qualify the symbol; ~12 does-not-resolve warnings (pointer-receiver form
  `(*T).method` ckg stores as `pkg.T.method`, descriptive symbols like
  `"ValidateTransaction (Berlin gate)"` → move parenthetical to `reason` + loc, a
  doc-file anchor). `cks-inventory-check --graph` now reports these trustworthily.
- **A2 — ckv Phase 2** (`../code-knowledge-vector`): ✅ done on branch
  `feat/canonical-id-alignment` (commit `ebc3f31`): `ckgalign` copies ckg's
  `canonical_id` (column-probed for old graphs) onto `types.Chunk` + `query.Hit`,
  persisted in the sqlitevec store; embed text unchanged so no re-embed. The
  compatibility key is inherited from ckg's graph.db, not recomputed.
  - **Coordination (2026-06-29, [coordination-response-ckg-2026-06-29.md](coordination-response-ckg-2026-06-29.md)):**
    agreed the CKG↔CKV **join key = `canonical_id`** (no separate B7 normalization;
    ckv inherits the ADR-0001 format byte-for-byte; non-symbol nodes fall back to
    node ID). ⚠️ **Population gate is cache `SchemaVersion >= 1.19`, not the
    SQL-column-appearance 1.16** — a PRAGMA column-probe passes on 1.16–1.18 graphs
    but the values are NULL (`cache.go`: "pre-1.19 DBs carry it empty"). ckv's
    coordination doc was corrected to gate on `>= 1.19` / current schema (1.22).
    CKG action items: reindex go-stablenet at 1.22 for the ≥90% match-rate
    measurement; confirm/extend `search_tokens` (qname+signature+doc-comment) for
    D4; add a shared integration fixture (≥1.19 gate + `@<line>` dup cases).

**Tier B — ckg quality (this repo; shrinks the residual ~4% non-uniqueness found
in item-5 validation).**
- **B1 — skip non-symbols** ✅ done: `canonical_id` is no longer emitted for the
  blank identifier `_` (`goCanonicalID` guard + field/interface-method paths;
  schema 1.19 → 1.20). Promoted/synthetic methods already carry none (verified).
  Test: `TestCanonicalID_BlankIdentifierSkipped`.
- **B2 — package-level-only const/var** ✅ done: `emitValueSpec` sets
  `canonical_id` only when go/types says the object is package-level
  (`obj.Parent() == obj.Pkg().Scope()`); function-local `var`s get none,
  removing the ~1,000 same-named-local collisions. Schema 1.20 → 1.21. Test:
  `TestCanonicalID_LocalVarSkipped`.
- **B3 — line-qualify same-file same-name (on collision)** ✅ done: a per-file
  post-pass (`stampFilePath` → `lineQualifyDuplicateCanonicalIDs` at the single
  post-ParseFile chokepoint) appends `@<line>` only to canonical_ids shared by
  >1 node in the same file (minified-JS `function t`, multiple Go `init`).
  Unique ids stay line-independent. Schema 1.20 → 1.21. Test:
  `TestLineQualifyDuplicateCanonicalIDs`.
- **B4 — proto double-prefix** ✅ done: the proto canonical post-pass strips the
  leading `proto:` from the qname, so ids read `<relpath>:<pkg>.<Sym>`. Schema
  1.20 → 1.21. Test: `TestCanonicalID_NoDoubleProtoPrefix`.

**Tier C — RESOLVED (2026-06-29).**
- **C1 — item 7**: ✅ **decided — deprecate the Postgres backend**
  ([ADR-0003](adr/0003-deprecate-postgres-backend.md)). Postgres is opt-in
  (`--db <DSN>`), unused, CI-untested, and already lags the SQLite schema
  (`canonical_id` + `simple_name` both absent). No `canonical_id` parity will be
  implemented; sqlite is the sole maintained backend. The `pgStore.FindByCanonicalID`
  stub + missing columns are accepted deprecated-backend gaps, not bugs. Removal
  is a separate later decision per the ADR.

Execution: **B1 ✅ → A2 ✅ → A1 core ✅ → A1-3 tooling ✅ → B2/B3/B4 ✅ → A1-3b loc-classify ✅ (def-uniqueness + 17 review anchors = data follow-up) → C1 (next).**

## Remaining work

### Phase 1 (ckg) — finish canonical id

> **Progress (branch `feat/canonical-symbol-id-phase1`, updated 2026-06-19):**
> items **1–6 done** (Go + Solidity + TypeScript + proto canonical_id, exact
> resolution, schema bump 1.18 → 1.19, tests, and go-stablenet reindex validated
> — see "Validation results"). Only remaining: item 7 (Postgres parity —
> deferred, status quo per decision). PR #24 carries items 1–4, 6; item 5 is a
> runtime validation (no code) plus a small `LANG` Makefile var for multi-lang
> eval builds.

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
5. ✅ **Reindex go-stablenet** — done & validated (2026-06-19). Built via
   `make eval-build-dbs LANG=auto` (a new `LANG ?= go` Makefile var lets the
   eval build include sol/proto without changing its Go-only default) over
   `/Users/.../go-stablenet` (1297 go + 294 sol + 4 proto) to a scratch
   `EVAL_DB_ROOT`: **251,236 nodes / 1,974,320 edges**. See "Validation results"
   below. Promotion to a shared `EVAL_DB_ROOT` is a runtime/ops step, not code.
6. ✅ **Tests** — `TestCanonicalID_DistinguishesSameNameAcrossPackages`,
   `TestCanonicalID_AllGoNodeKinds` (type/field/interface-method/const/var +
   interface-vs-concrete), `TestCanonicalID_SolidityOverloads`, and
   `TestFindByCanonicalID` + the `TestResolveSeed` canonical subtest. Solidity
   golden snapshots also lock canonical_id.
7. ✅ **Postgres `canonical_id` parity — WON'T DO (deprecated).** The Postgres
   schema / `pgNodeColumns` carry no `canonical_id` column and
   `pgStore.FindByCanonicalID` is a not-found stub. **Decision
   ([ADR-0003](adr/0003-deprecate-postgres-backend.md), 2026-06-29):** the
   Postgres backend is deprecated (opt-in, unused, CI-untested, already lagging
   the SQLite schema) — sqlite is the sole maintained backend, so no parity is
   added. The stub + missing columns are accepted deprecated-backend gaps.

### Validation results (item 5, go-stablenet reindex, 2026-06-19)

Reindexed go-stablenet (251,236 nodes / 1,974,320 edges). Ground-truth queries
against the resulting sqlite `graph.db`:

**Population** — symbol nodes carry `canonical_id` as expected; statement/meta
nodes correctly do not:

| node type (go) | total | with canonical_id |
|---|---|---|
| Function | 6,497 | 6,497 (100%) |
| Method | 8,438 | 8,438 (100%) |
| Struct | 1,779 | 1,779 (100%) |
| Field | 7,943 | 7,943 (100%) |
| Constant | 1,655 | 1,655 (100%) |
| CallSite / IfStmt / git Commit·Hunk | — | 0 (by design) |

Solidity 2,664 and proto 409 symbol nodes also populated.

**Core goal — cross-package collisions resolve uniquely (✅):**
- The 28 `Size` methods across packages get **28 distinct** canonical ids.
- Even an identical short `qualified_name` is disambiguated: `prque..Size`
  resolves to `…/common/prque.(*LazyQueue).Size` vs `…/common/prque.(*Prque).Size`.
- Go Method uniqueness is **99.98%** (2 collisions / 8,438 — see below).

**Solidity overloads (✅):** parameter-type signatures separate real OpenZeppelin
overloads, e.g. `AccessControl._checkRole(bytes32)` vs `(bytes32,address)`,
`Address.functionCall(address,bytes)` vs `(address,bytes,string)`; function-type
params are captured.

**Residual non-uniqueness (~4% of canonical ids; all explained — not a scheme
defect):**
- *Minified vendored JS* (`graphql/internal/graphiql/graphiql.min.js`): ~293
  single-letter `function t/i/n…` reuse the same `<relpath>:<name>` (no
  intra-file scope qualifier). Degenerate — a minified bundle indexed as source.
- *Go blank identifier* `_` (109): not a real symbol.
- *Same-named local `var`* within a package (`gspec`, `engine`, `funds`, …,
  ~1,000): `canonical_id = <importpath>.<name>` has no function/scope qualifier.
- *Legitimately non-unique by Go rules*: `init` functions (Go allows many per
  package), duplicated test-stub types (the 2 Method collisions are a mock
  `freezer` type with `Ancients`/`Freeze` defined in both
  `core/blockchain_repair_test.go` and `core/blockchain_sethead_test.go`), and
  generated `.pb.go`.

**Optional future refinements (do NOT block Phase 1):**
- Scope-qualify local-variable canonical ids (or skip non-package-level vars
  and `_`), and line-qualify same-file same-name functions, to remove the
  minified-JS / local-var noise.
- proto canonical id double-prefixes (`<relpath>:proto:<pkg>.<msg>`) because the
  proto qname already carries a `proto:` prefix — cosmetic; consider stripping.
- Skip emitting `canonical_id` for `_` and for synthetic/promoted methods.

### Phase 2 (ckv = `../code-knowledge-vector`, separate repo) — additive canonical field (no re-embed)
> ✅ **Done** — branch `feat/canonical-id-alignment` (`ebc3f31`). See the Tier A
> "Next" entry above for the implementation summary. Note: the working approach
> was the `ckgalign` Load column-probe + verbatim copy (not a `cmd/ckv/migrate.go`
> runner — the in-place ALTER + next aligned build covers population).

- Add an additive `canonical_id` to `pkg/types.Chunk` and the search `Hit`
  (omitempty), populated from the aligned ckg node. Alignment is already
  **positional** (`internal/ckgalign`), so it is name-agnostic — do NOT change the
  embed-text prefix, so **no re-embed** is needed. Migrate in place
  (`cmd/ckv/migrate.go` runner + reparse). Tests: every aligned chunk carries the
  ckg canonical id; vectors byte-identical.

### Phase 3 (cks = `../code-knowledge-system`, separate repo) — exact resolution + two anchor kinds + data migration
> 🔶 **Core done** — branch `feat/canonical-id-resolution` (`6609d12`): the
> resolution + MCP-doc items below are complete. **A1-3 remaining**: the anchor
> `kind: def|loc` schema + skills + domainexport + data migration.

- ✅ `internal/ckgclient/real.go`: resolve by canonical id; drop the `defs[0]`
  fallback in `resolveQname`/`resolveNodeID`/`resolveSeedFile` (multi-match now
  returns unresolved, not a silent pick).
- ✅ MCP tool docs (`internal/mcp/graph.go`) advertised a
  `consensus.wbft.Finalize` form ckg does not store — fixed to real
  qualified_name / bare-name / canonical_id examples.
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
