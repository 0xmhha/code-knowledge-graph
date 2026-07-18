# CKG Project Overview — 2026-05-25

> **ARCHIVED 2026-07-18.** Superseded by `docs/VISION.md` (purpose) +
> `docs/CONTINUITY.md` (live status). Dated facts here are stale (schema was
> 1.15, now 1.23; "9 tools", now 10; "T-14 pending", now shipped;
> awaits/overrides "slot-only", now emitted). Kept for provenance only —
> ground truth = code + git.

> Single-page entry for **figuring out what CKG is, what it does today, and
> what direction it's heading**. Written for a cold reader who has not seen
> any of the 11-cycle eval trajectory, the cks integration plan, or the
> within-language semantics design docs.
>
> Cross-links to authoritative sources at the end of each section. Do not
> duplicate detail here — this is an index, not a snapshot.

## 1. What CKG is

| Field | Value |
|---|---|
| Name | **CKG (Code Knowledge Graph)** |
| Version | v0.2.x, schema **1.15** (authoritative: docs/SCHEMA.md) |
| Language | Go 1.25.5, single binary, CGO-free by default |
| Input | Go / TypeScript / Solidity source trees |
| Output | Graph DB (SQLite default, PostgreSQL `--db` opt-in) — **37 NodeTypes × 43 EdgeTypes** (authoritative: docs/SCHEMA.md) |
| Validated corpus | go-stablenet 2,142 files → 214K nodes / 652K edges (audit PARITY); stablenet 313 MB graph 210K/708K (cycle 9 baseline) |

CKG is *one* of three sister projects designed to compose:

- **CKG** (this repo) — source → **graph** DB. Symbol identity, call edges,
  temporal hunks, distributed surface (HTTP/gRPC). Deterministic SQL store.
- **CKV** (`code-knowledge-vector`, separate session) — source → **vector**
  knowledge data + **vocabulary bridge** (Korean / vague → exact English
  keywords). NOT vector-only; the bridge role was clarified 2026-05-22.
- **CKS** (`code-knowledge-system`, separate session) — orchestrator. Coding
  agent talks to CKS; CKS uses CKV (vocab bridge) → CKG (keyword retrieval)
  → LLM (request + retrieved code), runs unit + LLM-MCP-driven e2e tests,
  opens PR on green.

CKG's job in this triangle: **given exact English keywords + a project's
graph DB, return precisely the code the coding agent needs.** That makes
*keyword retrieval accuracy* the first-class metric.

## 2. The five surfaces

`ckg` is a cobra root command with ~20 subcommands. Five are user-facing
production surfaces; the rest are utilities (`bench-*`, `validate`,
`benchmark`, `query`, `report`, `quickstart`, `export-*`).

| Surface | What it does | Backend module |
|---|---|---|
| `build` | 7-pass pipeline: detect → parse → resolve → graph → xlang → temporal → cluster → score → persist. Incremental cache. `--files-from` restricts to a user-supplied file list. | `internal/buildpipe` |
| `serve` | HTTP API + embedded Next.js viewer (3D force-graph). 13+ `/api/*` routes. | `internal/server` |
| `mcp` | stdio MCP server. **9 tools**: find_symbol, find_callers, find_callees, get_subgraph, search_text, get_context_for_task, impact_of_change, concurrency_impact, evidence_for_intent. | `internal/mcp` |
| `eval` | 4-baseline LLM comparison (α raw / β whole-graph / γ tools / δ smartContext). Hallucination validator. Retrieval gold-set. | `internal/eval`, `internal/eval/retrieval` |
| `audit` | `go/packages.Load` vs DB parity check; exit 0/1/2 for CI gating. | `internal/audit` |

## 3. Six-graph axis

The graph emits edges across six axes. Coverage rates are the CKS deep-dive
%-of-target estimates per axis, not implementation completeness — every
axis has at least some emitters live.

```
Axis              Edges (active + reserved)                            CKS cov
───────────────  ─────────────────────────────────────────────────────  ───────
Structural       contains, defines, imports, exports                     50%
Semantic         references, implements, extends, uses_type,             70%
                 reads_field, writes_field, has_modifier, has_decorator,
                 emits_event, reads_mapping, writes_mapping
Execution        calls, invokes (+ dispatch_kind metadata, schema 1.7)   60%
Concurrency      spawns, sends_to, recvs_from,                           80%
                 timeout_path, cancellation_path (P2),
                 acquires_lock, releases_lock, accessed_under_lock
                   (B1 Stage 1 live — Mutex nodes + lock edges emitted
                    by internal/parse/golang/concurrency*.go)
Distributed      listens_on, handles_message, rpc_calls, binds_to,       70%
                 http_calls (W2), grpc_listens_on/grpc_calls (W3b)
Temporal         changed_in, blame,                                       90%
                 has_hunk, adjacent, modifies (H1-H4, schema 1.8)
─── slot-only (no emission yet, schema 1.10) ───
                 awaits (W-B, TS async/await suspension)
                 overrides (W-C, Solidity virtual/override)
```

Confidence triple on every edge: `EXTRACTED` (direct AST/types.Info) /
`INFERRED` (heuristic dispatch) / `AMBIGUOUS` (unreachable history hidden
from LLM consumers via `llmSafeStoreReader` wrapper).

Source of truth: `docs/SCHEMA.md`.

## 4. Public package surface

External consumers (cks, ckv) must NOT reach into `internal/`. Stable
public packages, all under `pkg/`:

| Package | Stable surface | Consumer note |
|---|---|---|
| `pkg/types` | NodeType, EdgeType, Confidence, Node, Edge | Append-only enums; reordering = ID hash breakage |
| `pkg/store` | `Reader` (alias of persist.StoreReader), `SearchHit`, `SearchFTSOptions`, `FindSymbolOptions`, `Manifest`, `OpenReadOnly`, `GetManifest` | CKG-1/2/4/6/7 closed. **No `mcphandlers` Register* surface yet — T-14 pending** |
| `pkg/bm25` | BM25 scorer | ckv R13 plan: import for 3-leg BM25 measurement. **External-import stability not formally tested yet (ckg-NEW-9)** |
| `pkg/smartctx` | `BuildContext` (δ baseline's tool-free retriever) | Stable. Prose-query robustness (T-10) deferred to cks Layer 3 |
| `pkg/evidence` | `BuildPack` (H3 EvidencePack assembler, supports `Mode: "and"`) | S1 (cks) migration decided; keep stable until then |
| `pkg/hunkmodifies` | hunk → CodeNode interval overlap | Stable (H2, schema 1.8) |
| `pkg/impact` | `impact_of_change` MCP backend | Stable |

**Key gap**: MCP tools (`internal/mcp/`) are not yet exposed under
`pkg/mcphandlers/` — cks cannot reuse the 9-tool registration code. This
is the *load-bearing T-14 P0 blocker* for cks S1 entry.

## 5. Active streams (where work flows)

Three active streams, each with its own tracker document.

### 5.1 Stream A — Eval methodology convergence (closed for now)

**Status**: T-04/T-05 CLOSED. 11-cycle hallucination-validator trajectory
hit a production-grade noise floor (β/δ halu rate 0.000, H2 +0.429).

**Cycle 9 baseline** (2026-05-22):
| Baseline | Score (mean±std) | Halu rate | UserPromptBytes |
|---|---|---|---|
| α raw file dump | 0.396±0.119 | 0.083 | 2,245 |
| β whole-graph | 0.746±0.046 | 0.000 | 69,422 |
| γ 5 tools | 0.688±0.037 | 0.122 | 157 |
| δ smartContext | **0.825±0.035** | **0.000** | 12,612 |

**Open follow-ups**: C (prompt V2), D (smartContext budget audit), E
(T02-T30 fixture expansion). All informational improvements — gated by
whether Stream C wants the measurements they produce.

Source: `docs/eval-trajectory.md`, `docs/CONTINUITY.md`, `docs/todo-cks-dogfood-followups-2026-05-20.md`.

### 5.2 Stream B — Within-language semantics (design ready, implementation paused)

**Status**: 26 design decisions agreed (2026-05-11). Schema 1.10 reserved
the AwaitPoint node + awaits/overrides edges. Detectors not yet implemented.

| Track | Language | Estimate | Status |
|---|---|---|---|
| W-A | Go cross-function lock propagation (Stage B DFS, `--lock-propagation` opt-in flag) | ~300-400 LOC | Plumbing live, propagator partial |
| W-B | TypeScript async/await + heritage (interface/extends/implements) | ~700 LOC | Slot reserved, detector pending |
| W-C | Solidity inheritance + interface dispatch + `using For` | ~1100-1200 LOC | Slot reserved, detector pending |

Source: `docs/archive/NEXT-CANDIDATES-WITHIN-LANG-SEMANTICS.md`, `docs/design/*.md`.

### 5.3 Stream C — cks/ckv/ckg cross-repo integration (current north star)

**Status**: Most recent stream. The 2026-05-22 cks integration session
added user requirements R9-R13. The 2026-05-23 plan (`CKS-INTEGRATION-2026-05-23.md`)
documents 9 new ckg-side tasks (ckg-NEW-1..9, ~500 LOC) that align ckg
with the cks orchestrator pattern.

**Open P0** (cks-entry blockers):

| ID | Work | LOC |
|---|---|---|
| T-14 | `pkg/mcphandlers/` surface (cks needs to import the 9-tool registrations) | ~moderate (move + thin wrapper) |
| ckg-NEW-2 | `pkg/types.Node.RecentPRs` + `PRRef` (R12 PR-aware retrieval) | ~120 |
| ckg-NEW-3 | Temporal slicing (`RecentPRsBefore(cutoff)`) — leakage prevention | ~50 |
| ckg-NEW-4 | `pkg/store.Reader.GetNodePRs` accessor | ~50 |
| ckg-NEW-5 | 12 ckv-fixture-mirror task YAMLs (pr69/70/72/74/77/75/73/67/63/58/56/55) | YAML only |
| ckg-NEW-8 | Stage B evaluation harness (14 tasks × 4 baselines) | ~150 |
| T-12 | `find_callers` depth>1 regression test | small |
| T-13 | `impact_of_change` determinism regression test | small |

**Open P1**:

| ID | Work | LOC |
|---|---|---|
| ckg-NEW-1 | Korean-query graceful degradation (R9 vocab-bridge bypass safety) | ~80 |
| ckg-NEW-6 | qname canonical helper usage guide | docs |
| ckg-NEW-7 | CKG-3 cross-snapshot policy (Option C recommended: directory routing per commit hash) | decision |
| ckg-NEW-9 | `pkg/bm25` external-import stability guarantee (R13 3-leg BM25) | ~50 |
| T-02 / T-03 / T-06 / T-07 / T-08 / T-09 / T-11 / T-15 | various measurement-infra hardening | small each |

**Recommended order** (CKS-INTEGRATION §8):
- **Day 1**: T-14 + ckg-NEW-2/3/4/9 — settle the public surface in one PR
- **Day 2**: ckg-NEW-1 + ckg-NEW-5 — Korean graceful + 12 fixture YAMLs
- **Day 3**: ckg-NEW-8 — Stage B harness, first measurement
- **Day 4-5**: T-04 V4 (30-question hallucination measurement) + regression cycles

Source: `eval/stablenet/CKS-INTEGRATION-2026-05-23.md`, `eval/stablenet/HANDOFF.md`.

## 6. The first-step success criterion

User-articulated north star (2026-05-25): **6-axis DB build + keyword-query
retrieval at 100% accuracy.** This is the single bar Stream C P0 work
must hit before Stage C (cks integration) becomes meaningful.

Today's score: `eval/baseline/retrieval.json` records 5/5 fixtures passing
with aggregate **R=1.00 / P=0.60 / F1=0.75**. The precision shortfall is
in R05 (`search_text` smart-routing returns more candidates than the
gold set requires). Stream C's ckg-NEW-5 expands the fixture set from
5 to 14; ckg-NEW-8 runs Stage B over the 14-task corpus.

The capability audit (`docs/CAPABILITY-AUDIT.md`) breaks the 100%-accuracy
goal into concrete gaps.

## 7. Cross-link index

| Topic | Doc |
|---|---|
| Architecture (1 page) | `docs/ARCHITECTURE.md` |
| Architecture (deep, 994 lines) | `docs/ARCHITECTURE-DETAILED.md` |
| Schema (node/edge types, version history) | `docs/SCHEMA.md` |
| Code structure (visual index, doc map) | `docs/CODE-STRUCTURE.md` |
| Foundation spec (v0.2 parser/cache/PG) | `docs/spec-ckg-v0.2.md` |
| Eval CLI usage + baselines | `docs/EVAL.md` |
| Eval 11-cycle trajectory (C18-C37) | `docs/eval-trajectory.md` |
| Cross-session entry / current snapshot | `docs/CONTINUITY.md` |
| Stream B (within-lang) | `docs/archive/NEXT-CANDIDATES-WITHIN-LANG-SEMANTICS.md` + `docs/design/*` |
| Stream C P0 task tracker | `eval/stablenet/HANDOFF.md` |
| Stream C cks integration plan | `eval/stablenet/CKS-INTEGRATION-2026-05-23.md` |
| Stream C todo (cks dogfood follow-ups) | `docs/todo-cks-dogfood-followups-2026-05-20.md` |
| Hunk-graph design (H1-H4, §11 decisions) | `docs/design/hunk-graph.md` |
| Walker symmetry matrix (parse-sol W10) | `internal/parse/solidity/WALKER_SYMMETRY.md` |
| Verification checklist (PR-ready 4-axis surface) | `docs/VERIFICATION-CHECKLIST.md` |
| Capability audit (the gap → work mapping) | `docs/CAPABILITY-AUDIT.md` |

## 8. What was decided in this overview session

- Stream A's C/D/E follow-ups are **isolated improvements**, valid but
  not load-bearing for the cks integration north star.
- Stream B (W-A/W-B/W-C) is **paused** — design ready, but cks-side
  utility is unconfirmed; resume after Stream C validates the surface.
- Stream C is the **active direction**. Day-1 work (T-14 +
  ckg-NEW-2/3/4/9 in one PR) is the highest-leverage move because it
  unblocks every downstream cks task and settles the public API in a
  single review.
- Capability gaps for "6-axis + keyword retrieval at 100%" are
  enumerated separately in `CAPABILITY-AUDIT.md`.
