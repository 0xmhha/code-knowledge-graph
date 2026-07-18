# DOC-MAP — documentation index & tier map

> **Read this first** before reviewing or adding docs. It says which document is
> authoritative and which tier it belongs to, so you don't have to re-derive
> that by scanning everything. **Rule: whenever you add, move, or supersede a
> doc, update its line here in the same change.**

## Tiers

- **Tier 1 — VISION/purpose**: why the project exists. Append-mostly, never
  pruned. Input to cleanups, never a target.
- **Tier 2 — DESIGN/specs**: how something was decided or specified. Superseded,
  not deleted (move to `archive/` with a "superseded by …" note). New decisions
  go in `docs/adr/`.
- **Tier 3 — STATE/status**: point-in-time snapshots, remaining-work, handoffs.
  Dated, disposable, regenerable from code + git.
- **ARCHIVE**: historical / superseded snapshots under `docs/archive/`.

**Ground-truth rule:** for "what is true *now*", code + git win over any doc.
For "why we decided X", the ADR / Tier 2 doc wins. For "what we're aiming at",
Tier 1 wins.

## Tier 1 — Vision / purpose

| Doc | Covers |
|---|---|
| [VISION.md](VISION.md) | **Start here.** Purpose, CKG/CKV/CKS triangle, retrieval-accuracy north star, public boundary. Absorbs the durable prose from the now-archived PROJECT-OVERVIEW / PROJECT-BLUEPRINT-ALIGNMENT |
| [ARCHITECTURE.md](ARCHITECTURE.md) | 1-page architecture: 7-pass pipeline, five surfaces, six-graph axis, four languages |

## Tier 2 — Design / specs (authoritative for "how/why decided")

| Doc | Covers |
|---|---|
| [ARCHITECTURE-DETAILED.md](ARCHITECTURE-DETAILED.md) | Full architecture: pipeline, cache routing, storage abstraction, CLI |
| [SCHEMA.md](SCHEMA.md) | **Authoritative** node/edge enumeration + schema version history |
| [CODE-STRUCTURE.md](CODE-STRUCTURE.md) | Visual index: package structure, pipeline, six-graph axis, cache routing |
| [INCREMENTAL.md](INCREMENTAL.md) | Incremental build cache: cache key, manifest v2, invalidation rules |
| [EVAL.md](EVAL.md) | Eval CLI: 4 baselines (α/β/γ/δ), backends, output schema |
| [design/hunk-graph.md](design/hunk-graph.md) | Temporal hunk-graph design (H1–H4) |
| [design/go-cross-function-lock-propagation.md](design/go-cross-function-lock-propagation.md) | Lock propagation spec (W-A) |
| [design/ts-async-await-and-interface.md](design/ts-async-await-and-interface.md) | TypeScript semantics spec (W-B) |
| [design/solidity-inheritance-and-interface-dispatch.md](design/solidity-inheritance-and-interface-dispatch.md) | Solidity inheritance/dispatch spec (W-C) |
| [design/solidity-cross-contract-storage-modifier.md](design/solidity-cross-contract-storage-modifier.md) | Solidity low-level call / storage / modifier spec (W7) |
| [design/solidity-storage-slot-index.md](design/solidity-storage-slot-index.md) | Solidity EVM storage slot indexing (W9) |
| [design/schema-1.9-spec.md](design/schema-1.9-spec.md) | Cross-language interop spec — W1–W3 (HTTP/gRPC) landed; **W4 message-queue pending** (see CONTINUITY) |
| [design/track-c-detector-gap.md](design/track-c-detector-gap.md) | Detector gap diagnosis / priority matrix (P0/P1 gaps closed in `7b32031`; historical) |

## Tier 3 — State / status / remaining-work (dated, disposable)

| Doc | Covers |
|---|---|
| [CONTINUITY.md](CONTINUITY.md) | **Cross-session cold-start entry point** — snapshot + remaining-work (the sole live Tier 3 status doc) |
| [VERIFICATION-CHECKLIST.md](VERIFICATION-CHECKLIST.md) | PR-ready surface fan-out checklist |
| [STUDY-GUIDE.md](STUDY-GUIDE.md) | External concept primer (Leiden, MCP, tree-sitter, …) |
| [HYDRATION-PATTERN.md](HYDRATION-PATTERN.md) | Viewer hydration pattern (React) |

## ADR — Architecture Decision Records

| Doc | Covers |
|---|---|
| [adr/README.md](adr/README.md) | ADR index + template. One decision = one file; supersede, don't delete. |
| [adr/0001-canonical-symbol-id.md](adr/0001-canonical-symbol-id.md) | Canonical symbol identity (`canonical_id`) decision — Accepted |
| [adr/0002-staged-graph-composition.md](adr/0002-staged-graph-composition.md) | Staged graph composition: deterministic production core + test overlay (fixes test-variant pollution) — Accepted |
| [adr/0003-deprecate-postgres-backend.md](adr/0003-deprecate-postgres-backend.md) | Deprecate the PostgreSQL backend; SQLite is the sole maintained target (closes symbol-identity item 7) — Accepted |

## Archive

`docs/archive/` — historical/superseded snapshots and handoffs (dated). Kept for
provenance; never the authoritative answer. Archived in the 2026-06-15 cleanup:

- `REMAINING-WORK.md` — superseded by CONTINUITY + CAPABILITY-AUDIT (work landed PR #12–#22)
- `HANDOFF-2026-05-29.md` — superseded by CONTINUITY as cold-start entry
- `NEXT-CANDIDATES-WITHIN-LANG-SEMANTICS.md`, `DISPATCH-WITHIN-LANG-SEMANTICS.md` — W-A/W-B/W-C work complete; design intent lives in `design/*.md`
- `analysis/` — dated point-in-time measurements & flow walkthroughs (see `archive/analysis/README.md`)

Archived in the 2026-06-30 cleanup (symbol-identity effort complete):

- `symbol-identity-remaining-work.md` — all items done/resolved; decisions in ADR-0001/0002/0003
- `HANDOFF-2026-06-19-symbol-identity.md` — canonical_id effort merged; resume doc no longer needed

Archived in the 2026-07-15 cleanup (coordination closed, no open CKG work):

- `REMAINING-WORK-2026-07-15.md` — all CKG code items done; residual C1/D1–D3 carried into CONTINUITY
- `coordination-response-ckg-2026-06-29.md` — canonical_id join / graph_digest coordination closed
- `coordination-reindex-migration-2026-07-10.md` — reindex/zero-downtime coordination closed; Q1/Q2/Q6 shipped in #53
- `retire-ckg-node-id.md` — CKG portion closed (no code change); ckv/cks did the removal

Archived in the 2026-07-18 docs cleanup (stale status/spec docs; verified against code):

- `PROJECT-OVERVIEW.md` — superseded by VISION (purpose) + CONTINUITY (status); schema/tool-count/T-14 facts stale
- `PROJECT-BLUEPRINT-ALIGNMENT.md` — every "Tier A — needed" item shipped (Policy/SecurityPattern nodes, 1-shot API, PR "why" history); role now in VISION
- `CAPABILITY-AUDIT.md` — all tracked capability gaps closed in code
- `eval-trajectory.md` — frozen C18–C37 eval history; metrics mirrored in CONTINUITY §3
- `SELF-VERIFICATION.md` — command flows valid but expected values stale (schema 1.6, 7 tools); CONTINUITY is sole live status
- `spec-ckg-v0.2.md` — foundation spec superseded: Postgres roadmap → ADR-0003, schema 1.0/1.1 → 1.23; residual pending items (concurrency Stage 2, cache Phase 2) carried into CONTINUITY
