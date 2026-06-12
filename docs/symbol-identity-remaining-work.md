# Symbol Identity — implementation status & remaining work (ckg)

Companion to the design contract in **code-knowledge-system
`docs/symbol-identity-design.md`** (merged, PR #16). This file tracks the ckg
implementation on branch **`feat/canonical-symbol-id`** and the work still to do
across ckg / ckv / cks.

## Where we are

**Phase 0 — design:** done (merged in cks).

**Phase 1 — ckg canonical id: FOUNDATION done** (commit `1403f10`, this branch).
Implemented and tested (build + `internal/persist` + `internal/parse/...` green):

- `pkg/types/node.go` — added `Node.CanonicalID` (json `canonical_id,omitempty`):
  the globally-unique, import-path-qualified identity (e.g.
  `github.com/ethereum/go-ethereum/core/vm.(*EVM).Call`). `QualifiedName` stays the
  short, suffix-searchable display form.
- `internal/parse/golang/declarations.go` — `goCanonicalID(obj go/types.Object)`
  builds the id for **Go functions and methods** (receiver-pointer aware); wired
  into `visitFuncDecl` (set only when `typesInfo != nil`).
- `internal/persist/schema.sql` — `nodes.canonical_id TEXT` column (schema 1.16).
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
1. **Wire the remaining Go node kinds** in `declarations.go` (currently only
   func/method): types/structs/interfaces (`emitTypeSpec`), fields (need the
   enclosing type to qualify: `<pkg>.<Type>.<Field>`), package-level const/var
   (`emitValueSpec` — these ARE emitted as nodes but resolution drops them today),
   and interface methods (distinct id from each concrete impl). Use
   `v.typesInfo.ObjectOf(ident)` at each site; extend `goCanonicalID` for the
   field case (its `*types.Var` has no receiver — derive the owner type).
2. **Other languages:** Solidity (`<dir>/<Contract>.<func>(<paramTypes>)` — the
   parameter-type signature is required to separate overloads, and the version
   directory to separate v1/v2; see `internal/parse/solidity/`), TypeScript, and
   proto. They have no Go import path, so the file/package path is the qualifier.
3. **Canonical resolution:** add an exact `FindByCanonicalID` path in
   `internal/persist/sqlite_reader.go` and make the traversal family
   (find_callers/get_subgraph/impact_analysis) resolve on the canonical id; a
   short-name lookup that matches >1 node must be an **error**, not the silent
   `defs[0]` it is today. Also update the Pass-2 call resolver
   (`internal/parse/golang/resolve.go`) to key on canonical id where available
   (note `qualifiedStaticTarget` in `statements.go` already uses go/types for
   forward edges — align it to emit canonical-id-form targets).
4. **Schema version bump** to 1.16 (find where the manifest `schema_version`
   string is produced; bumping signals the change and invalidates the build cache
   so a reindex repopulates `canonical_id`).
5. **Reindex go-stablenet** (graph build is LLM-free, minutes) to a scratch out
   first, validate that real collisions resolve uniquely (e.g. a `Size` method in
   `core/types` vs `consensus/wbft/core` gets distinct canonical ids), then to the
   shared `data/ckg-stablenet`.
6. **Tests:** Solidity overload, interface vs concrete, const/var, field.

### Phase 2 (ckv) — additive canonical field (no re-embed)
- Add an additive `canonical_id` to `pkg/types.Chunk` and the search `Hit`
  (omitempty), populated from the aligned ckg node. Alignment is already
  **positional** (`internal/ckgalign`), so it is name-agnostic — do NOT change the
  embed-text prefix, so **no re-embed** is needed. Migrate in place
  (`cmd/ckv/migrate.go` runner + reparse). Tests: every aligned chunk carries the
  ckg canonical id; vectors byte-identical.

### Phase 3 (cks) — exact resolution + two anchor kinds + data migration
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
```
git switch feat/canonical-symbol-id     # this branch (or use a worktree)
go build ./... && go test ./internal/parse/... ./internal/persist/...
```
Pick up at "Phase 1 remaining" item 1.

## Caveats
- **Concurrent sessions** are actively editing ckg/ckv/cks. Sync at phase
  boundaries and keep PRs small to avoid rebase churn. (This branch was developed
  off ckg `main`; rebase before PR if `main` moved.)
- The live cks MCP serves the graph it loaded at process start; a rebuilt
  `graph.db` is only picked up after a cks restart.
- Estimated remaining effort: ~6–9 focused implementation+test blocks (~4–8h of
  work), plus per-phase review/merge and concurrent-session coordination.
