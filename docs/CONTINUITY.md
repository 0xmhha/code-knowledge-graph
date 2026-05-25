# Continuity — cross-session / cross-machine entry point

> 2026-05-26. *Single entry point* for a new session or new machine picking
> up the ckg dogfood / eval / cks-integration work. Everything below is
> deliberately short — every section links to the authoritative source.

## 1. Snapshot (where the project is)

- **Branch**: `main`
- **Local commits ahead of origin**: 0 pushed; **uncommitted Lane X work in tree** (see §9 for the file list and recommended commit split)
- **Latest pushed commits** (top 5):
  - `d4aed40` docs(viewer): README — make build + ckg serve as the prod path
  - `ac01132` docs(handoff): note viewer-session uncommitted files in CONTINUITY §7
  - `98260a8` docs(handoff): cross-session continuity (CONTINUITY + eval-trajectory + HANDOFF close)
  - `c7de329` docs(viewer): handoff doc — environment setup + Claude collab rules
  - `563dc63` docs(eval/stablenet): cks integration cross-machine handoff
- **Lane X (2026-05-25 → 26)**: complete, in-tree only, awaiting commit
  - search_text + pkg/store now expose AND/OR mode (`SearchFTSOptions.Mode`, `Reader.SearchWithOpts`)
  - retrieval gold-set: 5 → **12 fixtures**, aggregate **R=1.00 P=1.00 F1=1.00**
  - B1 Stage 1 concurrency emit (Mutex / acquires_lock / releases_lock / accessed_under_lock) was already live in `internal/parse/golang/concurrency*.go` — docs drift fixed, R11/R12 fixtures lock against future regression
  - User north-star ("6-axis graph DB + keyword query at 100%") first lock achieved
- **Eval framework (LLM-driven)**: production-ready, T-04/T-05 closed
- **Last smoke run**: 2026-05-22 (cycle 9), 12/12 LLM calls, all four baselines clean

## 2. Where to read next

| If you want to … | Read |
|---|---|
| Understand the eval-framework series (11 cycles, metrics) | `docs/eval-trajectory.md` |
| Cross-project cks integration plan (R9-R13, ckg-NEW-1..9) | `eval/stablenet/CKS-INTEGRATION-2026-05-23.md` |
| Current P0 task status (closed + open) | `eval/stablenet/HANDOFF.md` |
| Phase-by-phase dogfood follow-up tracker | `docs/todo-cks-dogfood-followups-2026-05-20.md` |
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

## 7. Pending handoff — other-session uncommitted changes

As of 2026-05-23 commit `98260a8`, two files in the working tree are
modified/untracked but belong to a different working session
(viewer / web/viewer-next). They are left for the *viewer session*
owner to commit so attribution stays with the original author.

| File | State | Owner |
|---|---|---|
| `internal/server/web_assets/index.html` | modified (−20/+8) | viewer session |
| `web/viewer-next/README.md` | untracked (6.9 KB, created 2026-05-21) | viewer session (likely missed by commit `c7de329`) |

This session (eval/cks/T-04 series) deliberately did NOT stage or
modify either file. If you are the viewer-session owner picking up
the work, both changes appear to be ready-to-commit handoff items
— review and commit under your authorship.

## 9. Lane X uncommitted file list + recommended commit split

The 2026-05-25 → 26 Lane X work touches 27 files in three logical groups.
The split below pairs *behaviour change* with *its own tests* and isolates
the *data-only* and *docs-only* moves so each commit has a single review
concern.

### Commit A — capability: search_text AND/OR mode + SearchWithOpts surface

```
internal/persist/search_hit.go         (SearchFTSOptions.Mode field)
internal/persist/sqlite.go             (SearchFTS mode branch + helpers + SearchWithOpts)
internal/persist/postgres_store.go     (PG parity)
internal/persist/store_interface.go    (SearchWithOpts interface method)
internal/persist/search_mode_test.go   (NEW: 5 integration tests)
internal/persist/postgres_exporter_test.go  (mockStoreReader.SearchWithOpts)
internal/mcp/h3_filter.go              (llmSafeStoreReader.SearchWithOpts forwarder)
internal/mcp/h3_filter_test.go         (fakeStore.SearchWithOpts + leak-guard table row)
internal/mcp/tools.go                  (registerSearchText mode/language args)
internal/eval/hallucination_check_test.go  (fakeStore.SearchWithOpts)
internal/eval/retrieval/runner.go      (runSearchText mode/language args)
```

Subject suggestion:
`feat(search): AND/OR mode in search_text + pkg/store.SearchWithOpts`

### Commit B — measurement: fixture corpus + B1 synthetic + baseline

```
eval/retrieval/R01-find-callers-vault-deposit.yaml  (M: + service.SafeVault.SafeDeposit)
eval/retrieval/R03-find-callers-service-new.yaml    (M: + service.NewSafeVault, P=1.0)
eval/retrieval/R04-find-symbol-vault.yaml           (M: + TS Vault + SafeVault.vault)
eval/retrieval/R05-search-text-deposit.yaml         (M: AND mode disposition)
eval/retrieval/R06-...yaml                          (NEW: OR multi-keyword Go)
eval/retrieval/R07-...yaml                          (NEW: 3-token AND)
eval/retrieval/R08-...yaml                          (NEW: TS language filter)
eval/retrieval/R09-...yaml                          (NEW: Sol language filter)
eval/retrieval/R10-...yaml                          (NEW: single-keyword strict Go)
eval/retrieval/R11-find-symbol-mutex.yaml           (NEW: B1 Mutex node lock)
eval/retrieval/R12-find-callees-safedeposit.yaml    (NEW: B1 lock-protected delegation lock)
eval/baseline/retrieval.json                        (M: 5 → 12 fixtures, R=1.00 P=1.00 F1=1.00)
testdata/synthetic/go-backend/service/concurrent.go (NEW: SafeVault Mutex fixture)
```

Subject suggestion:
`test(retrieval): 12-fixture multi-keyword + B1 concurrency gold-set lock`

### Commit C — docs: drift correction + overview/audit

```
docs/SCHEMA.md           (M: Mutex + acquires/releases/accessed_under_lock no longer slot-reserved)
docs/PROJECT-OVERVIEW.md (NEW: 3-stream index, 5-surface map, cross-link doc map)
docs/CAPABILITY-AUDIT.md (NEW: R-Build/R-Query/R-Accuracy gap matrix, Lane X/Y plan)
docs/CONTINUITY.md       (M: §1 snapshot + §5 next-action queue + §9 commit split — this section)
```

Subject suggestion:
`docs: PROJECT-OVERVIEW + CAPABILITY-AUDIT + B1 drift fix in SCHEMA`

The three commits are independent (A's tests pass with stock fixtures; B
recalibrates the fixtures against the new SearchFTS behaviour A introduced;
C is text-only). Land in A → B → C order if pre-commit hooks gate by
behaviour change; otherwise any order works.

## 8. Conventions in this repo

- Commit messages reference cycle IDs (C18-C37) for the eval series and
  W-C V## for the parse-sol lockdown series. Both numbering systems are
  chronological, not topic-organised.
- `docs/` carries living planning docs (the TODO tracker, the analyses).
  `eval/stablenet/` carries the HANDOFF and the cross-machine integration
  doc. `internal/parse/solidity/WALKER_SYMMETRY.md` carries the parse-sol
  lockdown matrix.
- Test files use the `_test.go` suffix; package-internal tests use
  `package eval` (not `eval_test`) for access to unexported helpers.
- Korean prose in the docs is intentional — the project author switches
  freely between Korean and English; commit messages and code identifiers
  stay English, everything else can be either.
