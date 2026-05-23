# Continuity — cross-session / cross-machine entry point

> 2026-05-23. *Single entry point* for a new session or new machine picking
> up the ckg dogfood / eval / cks-integration work. Everything below is
> deliberately short — every section links to the authoritative source.

## 1. Snapshot (where the project is)

- **Branch**: `main`
- **Local commits ahead of origin**: 0 (all pushed)
- **Latest commits** (top 5):
  - `563dc63` docs(eval/stablenet): cks integration cross-machine handoff (other session)
  - `efe9db7` feat(viewer): grid layout + boot-once data model (other session)
  - `8e8bf9b` fix(persist): narrow FTS power-user gate (ckg core bug, this session)
  - `2a4db90` fix(persist+eval): FTS5 dotted-identifier (ckg core bug, this session)
  - `46693a6` feat(eval): UserPromptBytes metric for H1
- **Eval framework**: production-ready, T-04/T-05 closed
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

| ID | Action | Estimate | Trigger |
|---|---|---|---|
| **B-2** | cks `go.mod` ckg version bump | 5 min | after user pushes ckg commits (already done — `origin/main` synced) |
| **C** | Prompt engineering V2 — reduce `http.HandleFunc`/`mux.HandleFunc` LLM noise | 1 h | none |
| **D** | smartContext budget audit — H1 cost-benefit curve | 1 h | none |
| **E** | Task fixture expansion (T02-T30) — coverage breadth | 1-2 h | none |
| **Defer** | CKG-3, EV1 Phase 3, W-C series resume, HANDOFF T-12/T-13 | — | per CKS-INTEGRATION recommendation |

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
