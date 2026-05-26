# Remaining Work — 2026-05-27

> Single living tracker for what's still on the table. Replaces the
> per-track scattering in `CONTINUITY.md §5` /
> `CAPABILITY-AUDIT.md §5-7` / `eval/stablenet/HANDOFF.md §1` /
> `CKS-INTEGRATION-2026-05-23.md §3` — those documents stay
> authoritative for their respective topics; this file is the
> *priority queue* you read first when picking up the next session.

## 0. Snapshot (2026-05-27)

- **Branch**: `main`
- **Commits ahead of origin**: see `git log origin/main..HEAD` —
  recent session sequence:
  - `1b18a49` feat(search): default search_text to symbol-only
    NodeKinds, opt-out via whitelist
  - `808086f` feat(pr-breadcrumb): symbol → PR history via build-time
    git log scan (ckg-NEW-2/3/4)
- **Schema**: 1.12 (node_prs added by the most recent commit)
- **Retrieval baseline**: 13/13 R=1.00 P=1.00 F1=1.00 (eval/baseline/retrieval.json)
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

| Commit / ID | What it did |
|---|---|
| 75aeb60 | feat(search): AND/OR mode in search_text + pkg/store.SearchWithOpts |
| ad313a9 | test(retrieval): 12-fixture multi-keyword + B1 concurrency gold-set lock |
| f57d78a | docs: PROJECT-OVERVIEW + CAPABILITY-AUDIT + B1 drift fix in SCHEMA |
| da7e221 | feat(mcphandlers): public surface for the 8 MCP tool registrations (T-14) |
| 85f6705 | refactor(mcp): remove internal/mcp handler duplication (T-14b) |
| 1b18a49 | feat(search): default search_text to symbol-only NodeKinds |
| 808086f | feat(pr-breadcrumb): ckg-NEW-2/3/4 — symbol → PR history |
| **T-02** | extractSymbols normalisation — file-extension blacklist (30 exts) + receiver-style `*`/`&` strip. Impl: `internal/eval/runner.go:246-427`. Tests: `runner_internal_test.go` (`TestExtractSymbols_FileExtensionBlacklist`, `TestExtractSymbols_ReceiverNormalisation`) |
| **T-09** | FTS sigil bypass narrowed to `*` + `"` only (parens/colon removed). Impl: `internal/persist/sqlite.go:805-809`. Partial fix 8e8bf9b, verified complete 2026-05-27 |
| **ckg-NEW-9** | `pkg/bm25` external-import stability: `doc.go` (SemVer promise) + `example_external_test.go` (Scorer interface contract). 2026-05-27 |
| **ckg-NEW-6** | qname canonical-helper usage guide: `pkg/store/doc.go` — FindSymbol→NeighborhoodByQname resolution pattern + normalisation rules. 2026-05-27 |
| **T-07** | dumpFiles `_test.go`/`testdata` exclusion: `internal/eval/runner.go:432-441`. Tests: `runner_test.go` (test_files_excluded, testdata_directory_skipped). 2026-05-27 |
| **T-12** | find_callers depth=3 fixture: `eval/retrieval/R13-find-callers-depth3.yaml`. Baseline 13/13 R=1.00 P=1.00 F1=1.00. 2026-05-27 |
| **T-13** | impact_of_change 100-call determinism: `pkg/mcphandlers/impact_test.go:TestImpact_Deterministic_100`. 2026-05-27 |
| **ckg-NEW-7** | `--out-tag` flag on `ckg build`: `auto-commit-hash` appends 12-char short SHA; literal values appended verbatim. Tests: `cmd/ckg/build_test.go`. 2026-05-27 |

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
| **ckg-NEW-5** | 12 ckv-fixture mirror task YAMLs in `eval/stablenet/tasks/` (pr69 / pr70 / pr72 / pr74 / pr77 / pr75 / pr73 / pr67 / pr63 / pr58 / pr56 / pr55) — 3/14 YAMLs exist (T01/T02/T03), 11 remaining | ~2-3 h | go-stablenet checkout under `~/Work/github/stable-net/go-stablenet-latest` (already present per CONTINUITY §4) | each YAML loads under `ckg eval-retrieval`; `make eval` passes the 12 new fixtures against `/tmp/ckg-stablenet/graph.db` |
| **ckg-NEW-8** | Stage B evaluation harness — runs the 12 mirror tasks × 4 baselines, produces a single JSON per fixture, diffs against committed baseline | ~3-4 h | ckg-NEW-5 lands | new `ckg stage-b` subcommand or `make eval-stage-b` target; baseline JSON committed under `eval/baseline/stage-b/`; the 12 fixtures all PASS at first commit |

### P1 — (empty: all items shipped)

All former P1 items shipped in the Tier 1 + Tier 2 session (2026-05-27).

### P2 — capability improvements that aren't blocking anyone

| ID | Work | Estimate | Notes |
|---|---|---|---|
| **CamelCase tokeniser** | Lift the FTS5 unicode61 limitation R10 documents — `HandleDeposit` should split into `handle` + `deposit` so `deposit*` prefix matches. Custom FTS5 tokeniser is C-extension territory (incompatible with modernc/sqlite); the cheap alternative is build-time pre-split, storing an extra `nodes.search_tokens` column the FTS5 content table indexes alongside name/qname | ~4-6 h | bumps schema; R10 expected widens to include HandleDeposit + every camelCase variant after the change |
| **search_text statement-only opt-in** | Symmetric to the X-NodeKinds narrowing — a query like `node_kinds=["IfStmt","ReturnStmt"]` should work for callers that *do* want control-flow rows. Already mechanically possible (SearchFTSOptions.NodeKinds accepts any whitelist); needs a fixture + doc example | ~30 min | new R13 fixture under `eval/retrieval/` |
| **T-03** | file:line citation validator (new `pkg/eval` subpackage or `internal/eval/citation.go`) | ~2 h | HANDOFF T-03 |
| **T-06** | 27 LLM-eval task YAMLs (T04-T30) under `eval/tasks/` | ~3-4 h | depends on ckg-NEW-5's stable-net coverage |
| **T-08** | dumpFiles deterministic shuffle (seeded by task ID) | ~30 min | HANDOFF T-08; depends on T-07 |
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
| **CKG-3** decision | Closed: ckg-NEW-7 shipped `--out-tag=auto-commit-hash` (Option C directory routing) 2026-05-27 | done |
| **EV1 Phase 3** CI integration | Pre-commit hook policy blocks automated `.github/workflows/ci.yml` edits; the manual paste snippet is in `docs/todo-cks-dogfood-followups-2026-05-20.md §F` | User applies the snippet |
| **B-2** cks `go.mod` ckg version bump | cks repo, separate session | user pushes ckg HEAD + cks-side session picks up |
| **E2** cks workaround removal PR | cks repo, separate session | cks Stage C entry |
| **viewer-session uncommitted files** | `internal/server/web_assets/index.html` + `web/viewer-next/README.md` were touched by the viewer working session, not the eval/cks session — left for the original author per CONTINUITY §7 | viewer session resumes |
| **W-C lockdown series resume** | W7.5 / W9 V19 / W8 V28+ all fold into the W-B/W-C detector landing | P3 unpaused |
| **HANDOFF T-12/T-13** | Both closed 2026-05-27. T-12: R13 depth=3 fixture shipped. T-13: 100-call `TestImpact_Deterministic_100` shipped | done |

## 3. Recommended next-session order

Updated 2026-05-27. Tier 1 + Tier 2 shipped this session.

### Shipped — Tier 1 + 2 (2026-05-27)

- ~~ckg-NEW-9~~ pkg/bm25 SemVer + external test
- ~~ckg-NEW-6~~ pkg/store/doc.go qname guide
- ~~T-07~~ dumpFiles exclusion
- ~~T-12~~ R13 depth=3 fixture (13/13 baseline)
- ~~T-13~~ 100-call determinism test
- ~~ckg-NEW-7~~ `--out-tag=auto-commit-hash`

### Remaining — Tier 3, external deps (~5-7 h)

1. **ckg-NEW-5** (2-3 h) — 11 remaining stable-net mirror fixture
   YAMLs (3/14 exist). Needs go-stablenet checkout.
   `--out-tag=auto-commit-hash` now available for per-SHA directory routing
2. **ckg-NEW-8** (3-4 h) — Stage B harness. Depends on NEW-5

### After Tier 3

P0 row empties. P1 already empty. P2 capability work (CamelCase
tokeniser, T-08, T-11) and P3 within-language semantics can resume
as bandwidth allows.

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
