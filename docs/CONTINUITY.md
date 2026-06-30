# Continuity — cross-session / cross-machine entry point

> Updated 2026-06-30. *Single entry point* for a new session or new machine
> picking up the ckg work. Deliberately short — every section links to the
> authoritative source. **For project purpose read `docs/VISION.md`; for the
> doc map read `docs/DOC-MAP.md`; "what is true now" = code + git.**

## 1. Snapshot (where the project is)

- **Branch**: `main`
- **Doc governance**: 3-tier model live — VISION (Tier 1), ADR (`docs/adr/`,
  Tier 2), status (Tier 3); `docs/DOC-MAP.md` is the index.
- **Symbol identity — COMPLETE.** The `canonical_id` chain (ckg→ckv→cks) is
  merged. Decisions: ADR-0001 (identity), ADR-0002 (deterministic graph
  composition — primary packages own production files), ADR-0003 (deprecate the
  Postgres backend, closing the old "item 7"). The status/handoff docs are
  archived (`docs/archive/symbol-identity-*`, `docs/archive/HANDOFF-2026-06-19-*`).
- **Build determinism (ADR-0002)**: `buildFileIndex` gives primary (non-test-variant)
  packages deterministic ownership of production files; test variants only add
  `_test.go`. Same source+commit+binary → same graph.
- **Canonical corpus build**: `--files-from` filters under `eval/stablenet/`
  (`stablenet-files.json` = no tests; `stablenet-files-with-tests.json` = binary
  scope + tests). See README "Building a graph".
- **Eval framework (LLM-driven)**: production-ready. Metrics: `docs/eval-trajectory.md`.
- **Schema**: 1.23 (authoritative: `docs/SCHEMA.md` → `internal/buildpipe/cache.go`).
- **Active cross-repo coordination (2026-06-29/30)**: canonical graph published
  to CKV/CKS/coding-agent; live doc = `docs/coordination-response-ckg-2026-06-29.md`.
  Open on others' side: CKV match-rate re-measure, coding-agent D-5.

## 2. Where to read next

| If you want to … | Read |
|---|---|
| Understand the eval-framework series (11 cycles, metrics) | `docs/eval-trajectory.md` |
| Cross-project cks integration plan (R9-R13, ckg-NEW-1..9) | `eval/stablenet/CKS-INTEGRATION-2026-05-23.md` |
| Current P0 task status (closed + open) | `eval/stablenet/HANDOFF.md` |
| Phase-by-phase dogfood follow-up tracker (archived) | `docs/archive/todo-cks-dogfood-followups-2026-05-20.md` |
| Walker symmetry matrix (parse-sol lockdown work) | `internal/parse/solidity/WALKER_SYMMETRY.md` |
| Why the FTS5 bug was important | `internal/persist/sqlite.go::rewriteFTSQuery` + commit `2a4db90`/`8e8bf9b` |

## 3. Environment setup (new machine)

Required:
- Go (toolchain version per `go.mod`)
- SQLite (for ckg's storage layer)
- One LLM backend (`make eval-llm-smoke` needs at least one)

LLM backend — pick one:
- **API backend (recommended)**: `export ANTHROPIC_API_KEY=...`
- **CLI backend**: `claude` binary on PATH AND `export CLIWRAP_AGENT=$(which cliwrap-agent)`
  (install: `go install github.com/0xmhha/cli-wrapper/cmd/cliwrap-agent@latest`)

Smoke test the eval pipeline:
```bash
make eval-llm-smoke BASELINES=alpha,beta,gamma,delta N_RUNS=3
# ~5-10 minutes, 12 LLM calls
# reads: eval/results/latest/report.md
```

Expected outcome (cycle 9 baseline): α score ~0.4, β ~0.75, γ ~0.69, δ ~0.83;
β and δ hallucination rate 0.000; H2 +0.4.

## 4. Cross-repo dependency map

| Repo | Path | ckg dependency | Notes |
|---|---|---|---|
| **ckg** (this) | `~/Work/github/tools/code-knowledge-graph` | self | 0 commits ahead of origin |
| **cks** | `~/Work/github/tools/code-knowledge-system` | `v0.0.0-20260513121714-85391f87b404` (2026-05-13) | 9-day outdated; cks build is clean (uses 2-arg SearchFTS adapter); FTS bug **does not affect cks** at runtime (see §6) |
| **ckv** | `~/Work/github/tools/code-knowledge-vector` | (separate session active) | do not modify from this session |
| **stablenet (target corpus)** | `~/Work/github/stable-net/go-stablenet-latest` | — | graphify-out/ + `/tmp/ckg-stablenet/graph.db` (313 MB, 210K nodes, 708K edges, built 2026-05-21) |

## 5. Next-action priority queue

Post-Lane X (2026-05-26): the capability-first lane closed the user-articulated
first-step goal. The queue below is what to do *after* the Lane X commit
lands.

| ID | Action | Estimate | Trigger |
|---|---|---|---|
| **T-14 (parallel surface)** | ✅ Done. `pkg/mcphandlers/` created with the 8 Register*, `RegisterAll`, and `NewLLMSafeReader`. internal/mcp kept intact as the production path; cks can now `import .../pkg/mcphandlers` without internal/ access. Smoke tests cover the cks-side wiring pattern. | — | — |
| **T-14b (cleanup)** | ✅ Done. Deleted the 5 duplicate handler files from internal/mcp (tools/get_context/impact/evidence/h3_filter); moved the 9 handler-related test files into pkg/mcphandlers/ via `git mv`. internal/mcp now carries only `server.go::Run` (delegates to mcphandlers.RegisterAll), `bench.go::NewBenchHandlers`, and the e2e + Run smoke tests. | — | — |
| **ckg-NEW-2/3/4** | ✅ Done. `pkg/types.PRRef` + `Node.RecentPRs` (omitempty), `node_prs` SQL table (schema 1.11 → 1.12), `internal/buildpipe.ScanPRHistory` (git log + `(#NNN)` regex + patch line-range overlap + remote.origin.url → owner/repo), `Reader.GetNodePRs(nodeID, cutoff time.Time)` with strict-before temporal slicing. Cold build self-emits node_prs from `git log`; non-git trees gracefully return empty. | — | — |
| **ckg-NEW-9** | `pkg/bm25` external-import stability — `pkg/bm25/example_external_test.go` + SemVer doc in `pkg/bm25/doc.go` | 1 h | bundle with ckg-NEW-2/3/4 |
| **search_text precision tightening** | ✅ Done. SearchFTSOptions.NodeKinds whitelist with a default symbol-only filter strips statement (IfStmt/LoopStmt/CallSite/ReturnStmt/SwitchStmt/AwaitPoint), meta (Commit/Hunk), and path-only (Import/Export) nodes at the SQL layer. R05/R06/R07/R08/R10 expecteds narrowed accordingly; `pkg/types.NodeType.IsSymbol` + `types.SymbolNodeTypes` are the canonical classification; cks/ckv can opt out by passing `node_kinds=types.AllNodeTypes()`. CamelCase tokeniser stays deferred (would lift the HandleDeposit limitation R10 documents). | — | — |
| **ckg-NEW-5/8** | 12 ckv-fixture mirror task YAMLs + Stage B harness over the synthetic→stable-net corpus | 3-4 h | cks-side import smoke done |
| **Defer** | CKG-3 (Option C dir-routing), EV1 Phase 3 CI (user-manual snippet ready), W-A/W-B/W-C resume, HANDOFF T-12/T-13 (now fixture-covered by R11/R12 + the Stream B design slots) | — | per CKS-INTEGRATION §3.4 / §8 |

The Stream A follow-ups (C prompt-V2 / D smartContext audit / E LLM-task
expansion) are still valid but downgraded to *post Stream C* — the Stream
C work measures over the same fixture set and decides whether Stream A
improvements move the needle.

Cross-project items (separate sessions):
- cks-side methodology transfer (this series → cks's own evaluation)
- ckv evaluation work (separate session active)
- cks integration ckg-NEW-1..9 (per `CKS-INTEGRATION-2026-05-23.md`)

## 6. What was just found (B-Phase 1 cks audit, 2026-05-23)

Direct quote of the surprising finding:

> cks's `extractKeywords` uses `identifierRE = /[A-Za-z][A-Za-z0-9_]{2,}/`
> (no dot). So cks's BM25Search call site never sends dotted-identifier
> queries to ckg's FTS5. The FTS5 dotted-identifier bug fixed in commits
> `2a4db90`/`8e8bf9b` **does not affect cks at runtime**.
>
> The two-cycle FTS fix is still load-bearing for ckg's own eval pipeline
> (δ smartContext baseline) and for any future ckg consumer that *does*
> pass dotted identifiers (e.g., MCP `find_symbol` with a fully-qualified
> qname). cks happens to be a non-affected consumer today.
>
> Phase 2 (cks-side change) is therefore minimal: just a `go.mod` version
> bump so future ckg fixes flow through. No workaround removal needed —
> there was no workaround on cks's side.

This was the explicit reason to write the docs you're reading now: the
finding lived only in the conversation context until this commit.

## 7. Conventions in this repo

- See `CLAUDE.md` for the working agreement (build/test/lint, conventions,
  doc discipline) and `docs/DOC-MAP.md` for the documentation tier map.
- Commit messages reference cycle IDs (C18-C37) for the eval series and
  W-C V## for the parse-sol lockdown series. Both numbering systems are
  chronological, not topic-organised.
- `docs/` carries the living docs (tier map, VISION, ADRs, status); dated
  snapshots and superseded designs live in `docs/archive/`.
  `eval/stablenet/` carries the HANDOFF and the cross-machine integration
  doc. `internal/parse/solidity/WALKER_SYMMETRY.md` carries the parse-sol
  lockdown matrix.
- Test files use the `_test.go` suffix; package-internal tests use
  `package eval` (not `eval_test`) for access to unexported helpers.
- Korean prose in the docs is intentional — the project author switches
  freely between Korean and English; commit messages and code identifiers
  stay English, everything else can be either.
