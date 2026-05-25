# Remaining Work — 2026-05-26

> Single living tracker for what's still on the table. Replaces the
> per-track scattering in `CONTINUITY.md §5` /
> `CAPABILITY-AUDIT.md §5-7` / `eval/stablenet/HANDOFF.md §1` /
> `CKS-INTEGRATION-2026-05-23.md §3` — those documents stay
> authoritative for their respective topics; this file is the
> *priority queue* you read first when picking up the next session.

## 0. Snapshot (2026-05-26)

- **Branch**: `main`
- **Commits ahead of origin**: see `git log origin/main..HEAD` —
  recent session sequence:
  - `1b18a49` feat(search): default search_text to symbol-only
    NodeKinds, opt-out via whitelist
  - `808086f` feat(pr-breadcrumb): symbol → PR history via build-time
    git log scan (ckg-NEW-2/3/4)
- **Schema**: 1.12 (node_prs added by the most recent commit)
- **Retrieval baseline**: 12/12 R=1.00 P=1.00 F1=1.00 (eval/baseline/retrieval.json)
- **Eval LLM baseline**: cycle 9, α=0.396 / β=0.746 / γ=0.688 /
  δ=0.825, halu β/δ 0.000 (eval-trajectory.md)
- **6-axis emission**: all 40 edge types live except `awaits` (W-B
  AwaitPoint) and `overrides` (W-C virtual/override) — slot-only
  Schema 1.10 reservations
- **Public surface**: pkg/store + pkg/mcphandlers + pkg/types +
  pkg/bm25 + pkg/smartctx + pkg/evidence + pkg/impact +
  pkg/hunkmodifies are the external-import contract

## 1. Recently shipped (context for the next picker-upper)

In approximate session order, newest last:

| Commit | What it did |
|---|---|
| 75aeb60 | feat(search): AND/OR mode in search_text + pkg/store.SearchWithOpts |
| ad313a9 | test(retrieval): 12-fixture multi-keyword + B1 concurrency gold-set lock |
| f57d78a | docs: PROJECT-OVERVIEW + CAPABILITY-AUDIT + B1 drift fix in SCHEMA |
| da7e221 | feat(mcphandlers): public surface for the 8 MCP tool registrations (T-14) |
| 85f6705 | refactor(mcp): remove internal/mcp handler duplication (T-14b) |
| 1b18a49 | feat(search): default search_text to symbol-only NodeKinds |
| 808086f | feat(pr-breadcrumb): ckg-NEW-2/3/4 — symbol → PR history |

The cumulative effect: user's R-Build / R-Query / R-Accuracy
north-star (`docs/CAPABILITY-AUDIT.md §1`) is closed on the
ckg-standalone axis, and the public surface is large enough that
cks can `import .../pkg/mcphandlers` + call `RegisterAll` with no
internal/ access. The remaining work is *cks-side integration*
(Stream C) plus *capability improvements* that depend on external
data the synthetic corpus can't provide.

## 2. Priority queue

### P0 — unblock cks Stage C, finish the cross-repo handoff

| ID | Work | Estimate | Prereq | Acceptance |
|---|---|---|---|---|
| **ckg-NEW-9** | `pkg/bm25` external-import stability — add `pkg/bm25/example_external_test.go` exercising the cks usage pattern + SemVer note in `pkg/bm25/doc.go` | ~1 h | none | external_test compiles + passes; doc covers the stability promise |
| **ckg-NEW-5** | 12 ckv-fixture mirror task YAMLs in `eval/stablenet/tasks/` (pr69 / pr70 / pr72 / pr74 / pr77 / pr75 / pr73 / pr67 / pr63 / pr58 / pr56 / pr55) — bridges the gap between ckg's synthetic corpus and the stable-net base_sha fixtures cks scores | ~2-3 h | go-stablenet checkout under `~/Work/github/stable-net/go-stablenet-latest` (already present per CONTINUITY §4) | each YAML loads under `ckg eval-retrieval`; `make eval` passes the 12 new fixtures against `/tmp/ckg-stablenet/graph.db` |
| **ckg-NEW-8** | Stage B evaluation harness — runs the 12 mirror tasks × 4 baselines, produces a single JSON per fixture, diffs against committed baseline | ~3-4 h | ckg-NEW-5 lands | new `ckg stage-b` subcommand or `make eval-stage-b` target; baseline JSON committed under `eval/baseline/stage-b/`; the 12 fixtures all PASS at first commit |

### P1 — close the HANDOFF P0 items the X-fixture work didn't already cover

| ID | Work | Estimate | Notes |
|---|---|---|---|
| **T-12** | find_callers depth>1 regression test (HANDOFF.md T-12) | ~30 min | partially covered by R01/R03 at depth=2; an explicit depth=3 fixture against go-stablenet would close the gap |
| **T-13** | impact_of_change determinism regression test | ~30 min | spawn 100 calls against the same seed_qname, assert identical node/edge sets |
| **ckg-NEW-6** | qname canonical-helper usage guide (docs only) | ~30 min | one-page write-up in `pkg/store/doc.go` showing the cks wrapping pattern for qname normalisation |
| **ckg-NEW-7** | CKG-3 cross-snapshot policy — adopt Option C (directory routing, `--out-tag=auto-commit-hash`) per CKS-INTEGRATION §3.4 | ~1-2 h | adds the auto-tag flag to `ckg build`; the fixture runner's existing pattern (`/tmp/ckg-stablenet-${SHA}`) becomes the recommended convention |

### P2 — capability improvements that aren't blocking anyone

| ID | Work | Estimate | Notes |
|---|---|---|---|
| **CamelCase tokeniser** | Lift the FTS5 unicode61 limitation R10 documents — `HandleDeposit` should split into `handle` + `deposit` so `deposit*` prefix matches. Custom FTS5 tokeniser is C-extension territory (incompatible with modernc/sqlite); the cheap alternative is build-time pre-split, storing an extra `nodes.search_tokens` column the FTS5 content table indexes alongside name/qname | ~4-6 h | bumps schema; R10 expected widens to include HandleDeposit + every camelCase variant after the change |
| **search_text statement-only opt-in** | Symmetric to the X-NodeKinds narrowing — a query like `node_kinds=["IfStmt","ReturnStmt"]` should work for callers that *do* want control-flow rows. Already mechanically possible (SearchFTSOptions.NodeKinds accepts any whitelist); needs a fixture + doc example | ~30 min | new R13 fixture under `eval/retrieval/` |
| **T-02** | extractSymbols normalisation — file-extension blacklist (`.go` / `.ts` / `.sol` / `.md` / `.yaml` / `.json` etc.) + receiver-style normalisation (`*pkg.Type.Method` → `pkg.Type.Method`) | ~1 h | HANDOFF T-02 |
| **T-03** | file:line citation validator (new `pkg/eval` subpackage or `internal/eval/citation.go`) | ~2 h | HANDOFF T-03 |
| **T-06** | 27 LLM-eval task YAMLs (T04-T30) under `eval/tasks/` | ~3-4 h | depends on ckg-NEW-5's stable-net coverage |
| **T-07** | dumpFiles `_test.go` + testdata exclusion in `internal/eval/runner.go::dumpFiles` | ~15 min | HANDOFF T-07 |
| **T-08** | dumpFiles deterministic shuffle (seeded by task ID) | ~30 min | HANDOFF T-08; depends on T-07 |
| **T-09** | FTS sigil bypass narrowing — power-user route only on `*` + `"`, not `()` / `:` | ~30 min | HANDOFF T-09; partial fix in 8e8bf9b |
| **T-11** | Incremental index time KPI — `ckg bench-index` subcommand | ~2-3 h | HANDOFF T-11 |
| **T-15** | task YAML ↔ known-issues.jsonl sync tool — `eval/stablenet/sync_tasks.py` | ~1-2 h | HANDOFF T-15; depends on T-06 |

### P3 — within-language semantics (paused; cks utility unconfirmed)

| ID | Language | Work | Estimate | Status |
|---|---|---|---|---|
| **W-A** | Go | cross-function lock propagation (D1 Stage B DFS) — the `--lock-propagation` flag is already wired; the propagator is partial | ~300-400 LOC | Stage A AST is live; Stage B SSA-ish is the remaining work |
| **W-B** | TypeScript | async/await + heritage (interface/extends/implements). AwaitPoint slot (schema 1.10) reserved | ~700 LOC | design decisions resolved 2026-05-11; detector pending |
| **W-C** | Solidity | inheritance + interface dispatch + `using For`. overrides slot (schema 1.10) reserved | ~1100-1200 LOC | design decisions resolved 2026-05-11; detector pending |

Resume order if Stream C surfaces a need: W-B before W-C (TS detector
is the smaller; Sol detector reuses the dispatch helpers W-B
introduces).

### Deferred — external dependencies / decided to wait

| ID | Why deferred | Re-trigger |
|---|---|---|
| **CKG-3** decision | Reduced to a no-op once ckg-NEW-7 ships Option C (directory routing) | ckg-NEW-7 done |
| **EV1 Phase 3** CI integration | Pre-commit hook policy blocks automated `.github/workflows/ci.yml` edits; the manual paste snippet is in `docs/todo-cks-dogfood-followups-2026-05-20.md §F` | User applies the snippet |
| **B-2** cks `go.mod` ckg version bump | cks repo, separate session | user pushes ckg HEAD + cks-side session picks up |
| **E2** cks workaround removal PR | cks repo, separate session | cks Stage C entry |
| **viewer-session uncommitted files** | `internal/server/web_assets/index.html` + `web/viewer-next/README.md` were touched by the viewer working session, not the eval/cks session — left for the original author per CONTINUITY §7 | viewer session resumes |
| **W-C lockdown series resume** | W7.5 / W9 V19 / W8 V28+ all fold into the W-B/W-C detector landing | P3 unpaused |
| **HANDOFF T-12/T-13** | partial cover already shipped (R11/R12 fixture + within-design-doc spec); the *explicit* depth-3 + 100-call determinism tests are listed above under P1 | P1 done |

## 3. Recommended next-session order

Picking up from a clean local tree (commits already pushed):

1. **ckg-NEW-9** (1 h) — smallest P0, immediate cks Stage C unblock
2. **ckg-NEW-6** (30 min) — docs only; bundles cleanly with NEW-9
3. **ckg-NEW-7 decision** (1-2 h) — add `--out-tag=auto-commit-hash`,
   recommended commit pattern documented in CONTINUITY §4
4. **ckg-NEW-5** (2-3 h) — 12 stable-net mirror fixtures; needs a
   live go-stablenet checkout
5. **ckg-NEW-8** (3-4 h) — Stage B harness; depends on NEW-5 being
   on disk
6. **T-12 + T-13** (1 h together) — close the HANDOFF P0 box

After this sequence the cks integration P0 row is empty; P2
capability work and Stream B can resume as bandwidth allows.

## 4. Cross-links

| Topic | Doc |
|---|---|
| Cold-start guide | `docs/CONTINUITY.md` |
| Project identity + 3-stream overview | `docs/PROJECT-OVERVIEW.md` |
| Capability gap analysis | `docs/CAPABILITY-AUDIT.md` |
| Schema reference | `docs/SCHEMA.md` |
| HANDOFF-style P0 tracker (eval/stablenet origin) | `eval/stablenet/HANDOFF.md` |
| cks integration plan (R9-R13 + ckg-NEW-1..9) | `eval/stablenet/CKS-INTEGRATION-2026-05-23.md` |
| 11-cycle eval trajectory (Stream A history) | `docs/eval-trajectory.md` |
| Within-language semantics design (Stream B) | `docs/NEXT-CANDIDATES-WITHIN-LANG-SEMANTICS.md` + `docs/design/*.md` |
| Verification workflow | `docs/VERIFICATION-CHECKLIST.md` |
| Earlier follow-up tracker (dogfood-2026-05-20 era) | `docs/todo-cks-dogfood-followups-2026-05-20.md` |
