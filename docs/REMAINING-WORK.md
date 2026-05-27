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
- **Schema**: 1.13 (search_tokens camelCase pre-split column)
- **Retrieval baseline**: 14/14 R=1.00 P=1.00 F1=1.00 (eval/baseline/retrieval.json)
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
| **ckg-NEW-5** | 11 stable-net mirror fixture YAMLs (T04-T14) in `eval/stablenet/tasks/`. Total 14 tasks (3 existing + 11 new). All YAML-valid. Covers pr77/70/72/74/75/73/67/63/58/56/55. 2026-05-27 |
| **ckg-NEW-8** | Stage B evaluation harness: `make eval-stage-b` target + `make eval-stage-b-baseline-update`. Env var expansion in corpus_path (`${STABLENET_SRC}`). Baseline dir `eval/baseline/stage-b/`. 2026-05-27 |
| **T-08** | dumpFiles deterministic shuffle seeded by task ID (`hash/fnv` + `math/rand`). Same seed → same files; different seeds → different selection. Tests: `TestDumpFiles/deterministic_shuffle_by_seed`. 2026-05-27 |
| **search_text statement opt-in** | R14 fixture `eval/retrieval/R14-search-text-statement-opt-in.yaml` — validates `node_kinds=["IfStmt"]` opt-in overrides default symbol-only filter. Baseline 14/14. 2026-05-27 |
| **T-03** | file:line citation validator `internal/eval/citation.go`. ExtractCitations regex + ValidateCitations (file exists → line in node range). Integrated into runOne. Tests: 7 cases (extract + validate + integration). 2026-05-27 |
| **T-11** | `ckg bench-index` subcommand: full build (cold) → touch 1 file → incremental build, reports speedup ratio + p50/p95 with --iterations. Source copied to temp dir to avoid modifying original. JSON/text output. 2026-05-27 |
| **T-06** | 16 additional task YAMLs (T15-T30) in `eval/stablenet/tasks/`. Total 30 tasks: symbol_set=12, rubric=15, code_patch=3. Covers WBFT (8), system contracts (6), core blockchain (8), eth handler (5), misc (3). 2026-05-27 |
| **T-15** | `eval/stablenet/sync_tasks.py`: bidirectional sync YAML ↔ known-issues.jsonl. `--check` for drift detection, `--apply` for YAML→JSONL write. Requires PyYAML. 2026-05-27 |
| **CamelCase tokeniser** | Schema 1.13: `nodes.search_tokens TEXT` column with `pkg/bm25.Tokenize` pre-split. `nodes_fts` now indexes 5 columns. `HandleDeposit` → `handle deposit` tokens. R10/R06 fixtures widened. Baseline 14/14. 2026-05-27 |

The cumulative effect: user's R-Build / R-Query / R-Accuracy
north-star (`docs/CAPABILITY-AUDIT.md §1`) is closed on the
ckg-standalone axis, and the public surface is large enough that
cks can `import .../pkg/mcphandlers` + call `RegisterAll` with no
internal/ access. The remaining work is *cks-side integration*
(Stream C) plus *capability improvements* that depend on external
data the synthetic corpus can't provide.

## 2. Priority queue

### P0 — (empty: all items shipped)

All P0 items shipped. cks Stage C integration is unblocked.

### P1 — (empty: all items shipped)

All former P1 items shipped in the Tier 1 + Tier 2 session (2026-05-27).

### P2 — capability improvements that aren't blocking anyone

| ID | Work | Estimate | Notes |
|---|---|---|---|
| ~~CamelCase tokeniser~~ | ~~FTS5 camelCase pre-split~~ — shipped 2026-05-27 (schema 1.13) | — | — |
| ~~T-03~~ | ~~file:line citation validator~~ — shipped 2026-05-27, see Recently shipped | — | — |
| ~~T-06~~ | ~~27 LLM-eval task YAMLs~~ — shipped 2026-05-27 (T15-T30 added, total 30) | — | — |
| ~~T-11~~ | ~~Incremental index time KPI~~ — shipped 2026-05-27, see Recently shipped | — | — |
| ~~T-15~~ | ~~task YAML ↔ known-issues.jsonl sync~~ — shipped 2026-05-27 | — | — |

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

### All Tiers shipped (2026-05-27)

P0 empty. P1 empty. Remaining work is P2 capability improvements
and P3 within-language semantics — none blocking cks Stage C.

To run Stage B evaluation:
```
make eval-stage-b                     # build graph + run 14 tasks × 4 baselines
make eval-stage-b-baseline-update     # promote results to baseline
```

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
