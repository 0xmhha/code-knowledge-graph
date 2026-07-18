# Continuity — cross-session / cross-machine entry point

> Updated 2026-07-15. *Single entry point* for a new session or new machine
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
- **graph_digest pin anchor + atomic cold rebuild (#53)**: `internal/buildpipe/graph_digest.go`
  publishes a deterministic code-graph digest to the manifest (json + in-db row);
  cold rebuild is atomic via `graph.db.building` → `os.Rename`. Canonical pr-77-2
  graph digest = `4be26516…`.
- **Keyword-retrieval eval (#57)**: LLM-free `eval-retrieval` fixtures —
  `eval/ckv-mirror` (CKV fixture mirror, `make eval-ckv-mirror`, 12/12) and
  `eval/stablenet-keyword` (real corpus, `make eval-stablenet-keyword GRAPH=<dir>`, 8/8).
- **Eval framework (LLM-driven)**: production-ready. Metrics: `docs/archive/eval-trajectory.md`.
- **Schema**: 1.23 (authoritative: `docs/SCHEMA.md` → `internal/buildpipe/cache.go`).

## 1b. Remaining work — **no open CKG code items** (2026-07-15)

The reindex/graph_digest coordination and the ckg_node_id retirement closed; the
status/coordination docs are archived under `docs/archive/` (see the archive
notes there). What's left is not CKG code work:

- **C1 (optional, deferred)**: widen `goCanonicalID` coverage
  (`internal/parse/golang/declarations.go`). Deferred by design — empty
  `canonical_id` is mostly by-design (promoted/synthetic methods, function-local
  var/const), and changing it bumps the schema and re-digests the published graph
  (CKV/coding-agent ripple) for ~zero gain.
- **Other sessions (track only)**: D1 CKV re-align + graph_digest end-to-end on
  pr-77-2 (CKG published the digest; `ckgalign.ReadCoords` auto-consumes);
  D2 coding-agent "~23% recall" source (D-5); D3 ckv/cks `ckg_node_id` removal
  (ckv done 07-11, cks 07-12).

**Deferred design backlog (not scheduled; surfaced by the 2026-07-18 docs cleanup).**
These are the only genuinely-unimplemented features left in the design specs; both
were consciously deferred, not dropped:

- **W4 message-queue / pub-sub** (`docs/design/schema-1.9-spec.md` §W4): Kafka /
  NATS / RabbitMQ / SQS detection → a `Topic` node + `publishes_to` / `consumes_from`
  edges. No `NodeTopic` / `EdgePublishesTo` exists in `pkg/types/enums.go`. W1–W3
  (HTTP/gRPC interop) shipped; W4 is the sole remaining stage of that spec.
- **Concurrency Stage 2** (`docs/archive/spec-ckg-v0.2.md` §2 Stage 2): SSA-based
  `--deep` dataflow + `is_potential_race` candidate detection. Stage 1 (Mutex +
  acquires/releases/accessed_under_lock) shipped; Stage 2 is spec-only.

## 2. Where to read next

| If you want to … | Read |
|---|---|
| Understand the eval-framework series (11 cycles, metrics) | `docs/archive/eval-trajectory.md` |
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
| **ckg** (this) | `~/Work/github/code-knowledge-graph` | self | schema 1.23; canonical_id chain complete |
| **cks** | `~/Work/github/code-knowledge-system` | tracks ckg/ckv origin HEADs (bumped) | `e456698` carries canonical_id through to `contract.Hit`; B7 join fixtures added |
| **ckv** | `~/Work/github/code-knowledge-vector` | (separate session active) | do not modify from this session; `#16` added ollama/bge-m3 default + Qwen3 options |
| **stablenet (target corpus)** | `~/Work/github/test/analysis-test-3` (@ `0bf2f4d1b`) | — | canonical graph = `~/Work/github/knowledge-data/pr-77-2/graph.db` (schema 1.23, 183,121 nodes; built with `stablenet-files-with-tests.json`) |

## 5. Next-action priority queue

**No open CKG code items — see §1b.** The 2026-05 Lane X / ckg-NEW queue and the
07-11/07-15 remaining-work snapshots are all resolved (mcphandlers surface,
node_prs, search_text AND/OR + precision, Korean/CJK test, T-14b shim, awaits/
overrides emission, graph_digest + atomic rebuild #53, keyword-retrieval eval
#57). What remains is C1 (deferred) and other-session items (D1–D3), listed in
§1b. Their archived evidence: `docs/archive/REMAINING-WORK-2026-07-15.md`.

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
