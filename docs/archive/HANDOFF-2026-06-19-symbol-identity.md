> **ARCHIVED 2026-06-30 — effort complete.** The ckg→ckv→cks canonical_id chain
> is merged and the work is done. Decisions: [`adr/0001`](../adr/0001-canonical-symbol-id.md) /
> [`adr/0002`](../adr/0002-staged-graph-composition.md) / [`adr/0003`](../adr/0003-deprecate-postgres-backend.md).
> Note: the canonical measurement graph cited here (`/tmp/ckg-eval/…`, sha 16ee6fb7)
> was later superseded by the filtered `knowledge-data/pr-77-2` build — see the live
> coordination doc. Kept for provenance.

# HANDOFF — symbol-identity (canonical_id) effort · 2026-06-19

> **Self-contained resume doc.** Read only this to understand the full state and
> continue on another machine. Tier 3 (dated snapshot). Authoritative "why" =
> `docs/adr/0001-canonical-symbol-id.md`; live item status =
> `docs/symbol-identity-remaining-work.md`; ground truth for "what is true now" =
> code + git in each repo.

## 1. What this effort is (one paragraph)

Three sister repos compose a code-knowledge system: **ckg** (graph DB of a source
tree) → **ckv** (vector + vocab bridge) → **cks** (orchestrator the coding agent
talks to). A symbol's short `qualified_name` is **not globally unique** (e.g.
`core/types.Size` vs `consensus/wbft/core.Size`), so a lookup that matched
several nodes could silently return the wrong code. This effort gives every node
a globally-unique, import-path-qualified **`canonical_id`** (ADR-0001), makes ckv
carry it, and makes cks resolve on it exactly (no silent first-of-N pick). The
chain ckg→ckv→cks is now complete and merged.

## 2. Repos (sibling clones under ~/Work/github/)

| Repo | Path | Role | Module |
|---|---|---|---|
| ckg | `code-knowledge-graph` | source → graph DB (SQLite), `canonical_id` producer | `github.com/0xmhha/code-knowledge-graph` |
| ckv | `code-knowledge-vector` | vector + vocab; inherits `canonical_id` onto chunks | `github.com/0xmhha/code-knowledge-vector` |
| cks | `code-knowledge-system` | orchestrator; resolves seeds by `canonical_id` | `github.com/0xmhha/code-knowledge-system` |

Verification used two more local repos (NOT part of the system):
- `test/pr-77` — a go-stablenet (go-ethereum fork, WBFT consensus) checkout; the
  code whose domain-knowledge claims were verified. Has `build/bin/gstable`.
- `chainbench` — local multi-node WBFT chain harness (CLI at `~/.local/bin/chainbench`).

## 3. Status — DONE & MERGED (per repo)

**ckg** (cache `SchemaVersion` was **1.21** at this handoff; now **1.22** —
PR #31's `simple_name` suffix-lookup column bumped it, unrelated to canonical_id —
in `internal/buildpipe/cache.go`):
- Phase 1: `canonical_id` for all parsers — Go (all node kinds), Solidity
  (`<relpath>:<Contract>.<func>(<paramTypes>)`, param sig separates overloads),
  TypeScript/proto (`<relpath>:<qname>`). PRs #21,#23,#24.
- `Reader.FindByCanonicalID` (sqlite; Postgres is a documented not-found stub).
- Resolution: `pkg/mcphandlers` resolves a canonical-id seed; multi-match bare
  name = ambiguous (never silent pick — that guard predated this via PR #23-era).
- Refinements B1–B4 (PR #25, #26): skip `_`; package-level-only const/var; proto
  double-`proto:` strip; line-qualify same-file duplicate ids (`@<line>`).
- item 5: go-stablenet reindexed & validated (collisions resolve uniquely).
- `LANG ?= go` Makefile var so `make eval-build-dbs LANG=auto` indexes sol/proto.

**ckv** (PR #9 merged):
- `internal/ckgalign` copies ckg's `canonical_id` (column-probed for old graphs)
  onto `pkg/types.Chunk` + `internal/query.Hit`, persisted in the sqlitevec store.
- Embed text unchanged → **no re-embed**. Key is *inherited* from ckg's graph.db,
  not recomputed (this is why it's compatible by construction).

**cks** (PRs #21,#22,#23,#24 merged):
- `internal/ckgclient`: bumped ckg dep; `FindByCanonicalID` adapter;
  `resolveQname/resolveNodeID/resolveSeedFile` resolve canonical-first and return
  *unresolved* on multi-match (dropped silent `defs[0]`); MCP tool-doc fix.
- Anchor `kind: def|loc` (schema + struct + `cks-anchor-refresh` never repoints
  loc + `cks-inventory-check --graph` def-uniqueness, **file-scoped** + tally).
- `domainexport` renders loc as `in <enclosing>:line (loc)`.
- Data: 4 deep-inside anchors → `kind: loc`; 10 ambiguous def symbols qualified;
  6 `ValidateTransaction (<gate>)` → loc; **4 entries marked verified**.

## 4. The verification that was performed (code + live chain)

The 4 cks entries below were `status: verified` but lacked provenance; their
summaries were confirmed against `test/pr-77` code AND a live 4-validator
chainbench chain, then `verified_by: mhha`, `last_verified_at: 2026-06-19`,
`priority: P0` were added.

- `A3.validator_set.set_construction` — validator order = genesis order (no sort);
  `QuorumSize=ceil(Size-F)`; roundRobin `+1` / sticky proposer; `validatorMu` RWMutex.
- `A4.gov_validator.genesis_initialization` — `initializeValidator` seeds in
  genesis order; `len(members)==len(validators)==len(blsKeys)`; dup skip; mappings.
- `A4.gov_minter.mint_proposal_allowance` — 0x1002/0x1003 contracts; `MintProof`
  tuple; `DefaultMaxMinterAllowance` 10B*10^18; per-minter allowance on FiatToken.
- `A4.system_contracts.storage_slot_layout` — keccak slot formulas
  (`CalculateMappingSlot/DynamicSlot/IncrementHash`), short/long bytes packing.

**Key empirical finding:** genesis `validators` order **is** the registered list
order. `istanbul_getValidators` at any *committed* block == genesis order; only
`"latest"` momentarily differs (in-flight proposer view — a measurement artifact,
NOT a reordering). `eth_getStorageAt` on the computed
`CalculateDynamicSlot(0x33,0)` returned exactly `validators[0]`, tying the
storage-slot formula + genesis-init + order together on-chain.

## 5. Remaining / optional follow-ups (NOT started — no branches)

1. **cks — 6 review anchors** (reported by `cks-inventory-check --graph`, left as
   warnings, need human judgment):
   - `Namespace: "istanbul"` (a config value, not a symbol) — remove or reshape.
   - `.claude/docs/build-source-files.md` anchor — belongs in `existing_doc_ref`.
   - 4 package-level vars (`quorumConsensusProtocolLengths`, `EthPeerRegistered`,
     `protocolMaxMsgSize`, `wbft.ErrStoppedEngine`) — ckg does not index them →
     investigate whether **ckg should emit package-level vars** (a ckg coverage
     question), or fix the symbol form.
2. **ckv — `feat/ckv-invariants-pkg`** branch (pushed to origin, NOT merged):
   another session's in-progress work exposing `FindInvariants`/`GetConventions`
   via `pkg/ckv`. **Intentionally excluded** from the PR sweep; the owning session
   finishes it. Do not merge without their review.
3. **ckg — Tier C / item 7**: Postgres `canonical_id` parity *or* an ADR to
   deprecate the Postgres backend (sqlite is the de-facto only target; pg is
   untested in CI). Decision needed first. See `symbol-identity-remaining-work.md`.

## 6. How to resume on a new machine

```sh
# clone the three sibling repos under ~/Work/github/ (+ test/pr-77, chainbench for verification)
# Go is managed by gvm here (go1.25.x); run go/make via a login shell if PATH misses it.

# ckg: build (ALWAYS via make → outputs to bin/, gitignored; never `go build` to repo root)
cd code-knowledge-graph && make build-no-viewer && make test

# rebuild the go-stablenet ckg graph (read-only oracle for cks anchor checks).
# needs STABLENET_SRC + EVAL_DB_ROOT; LANG=auto includes sol/proto:
make eval-build-dbs LANG=auto STABLENET_SRC=<go-stablenet path> EVAL_DB_ROOT=/tmp/ckg-eval

# cks: validate the domain anchors against that graph
cd ../code-knowledge-system
go run ./cmd/cks-inventory-check -project docs/domain-knowledge/projects/go-stablenet \
    -graph /tmp/ckg-eval/stablenet-<sha>/graph.db        # expect 0 errors, 6 warnings (the review set)

# spin up a local multi-validator chain to verify behavior empirically
cd ../test/pr-77                       # must have build/bin/gstable
chainbench init --profile default      # 4 validators + 1 EN; genesis at /tmp/node-data/genesis.json
chainbench start                       # http 8501-8505
# verify validator order at a COMMITTED block (not "latest"):
curl -s -XPOST http://127.0.0.1:8505 -d '{"jsonrpc":"2.0","method":"istanbul_getValidators","params":["0x10"],"id":1}'
chainbench stop
```

## 7. Gotchas learned this session (save time)

- **ckg graph.db is regenerable** (deterministic from source) → never edit it;
  it's only a read-only oracle. **cks domain-knowledge YAML entries are curated**
  → edit those (the "root data"); they are NOT regenerable.
- **Squash-merge + rebase**: after a squash merge, the branch's individual commits
  aren't ancestors of main. To prepare the next stacked PR: sync main, then
  `git rebase --onto main <last-merged-commit> <branch>` to replay only the new
  commits. `git branch -d` will refuse the merged branch (use `-D`).
- **chainbench validator order**: query a fixed/committed block, not `"latest"`.
- **`cks-inventory-check --graph` is file-scoped** (a symbol globally ambiguous
  but unique in its file is fine). 0 errors / 6 warnings is the expected baseline.
- **Two SchemaVersion constants** in ckg — `buildpipe/cache.go` (cache key / forces
  reindex) vs `persist/manifest.go` (back-compat). canonical_id work bumps the
  cache one. Was 1.21 for the canonical_id work (B2/B3/B4); now **1.22** after
  PR #31 added the `simple_name` column (a perf change, not canonical_id).
- Build via `make`; `go build ./cmd/...` without `-o` drops a binary in the repo
  root (a stray `/ckg` was gitignored to prevent committing it).

## 8. PR ledger (all merged unless noted)

- ckg: #21 #22(docs) #23 #24 #25(B1) #26(B2/B3/B4) — merged.
- ckv: #9 — merged. `feat/ckv-invariants-pkg` — open branch, excluded.
- cks: #21 #22 #23 #24 — merged.

As of this handoff: no pending PRs except the intentionally-excluded ckv branch;
all three repos' `main` are clean and synced.
