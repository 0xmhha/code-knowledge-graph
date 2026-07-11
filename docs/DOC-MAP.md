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
| [VISION.md](VISION.md) | **Start here.** Purpose, CKG/CKV/CKS triangle, retrieval-accuracy north star, public boundary |
| [PROJECT-OVERVIEW.md](PROJECT-OVERVIEW.md) | Fuller single-page overview (note: dated status numbers inside are Tier 3) |
| [PROJECT-BLUEPRINT-ALIGNMENT.md](PROJECT-BLUEPRINT-ALIGNMENT.md) | CKG's role in the cross-repo blueprint, E2E scenario |
| [ARCHITECTURE.md](ARCHITECTURE.md) | 1-page architecture: 7-pass pipeline, five surfaces, six-graph axis |

## Tier 2 — Design / specs (authoritative for "how/why decided")

| Doc | Covers |
|---|---|
| [ARCHITECTURE-DETAILED.md](ARCHITECTURE-DETAILED.md) | Full architecture: pipeline, cache routing, storage abstraction, CLI |
| [spec-ckg-v0.2.md](spec-ckg-v0.2.md) | Foundation spec: parser migration, concurrency analysis, Postgres, incremental cache |
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
| [design/schema-1.9-spec.md](design/schema-1.9-spec.md) | Cross-language interop spec (HTTP/gRPC, W1–W4) |
| [design/track-c-detector-gap.md](design/track-c-detector-gap.md) | Detector gap diagnosis / priority matrix |

## Tier 3 — State / status / remaining-work (dated, disposable)

| Doc | Covers |
|---|---|
| [CONTINUITY.md](CONTINUITY.md) | Cross-session cold-start entry point: snapshot + next-action queue |
| [coordination-response-ckg-2026-06-29.md](coordination-response-ckg-2026-06-29.md) | CKG → CKV 협의 회신: canonical_id join key (=canonical_id), population gate >= 1.19 정정, BM25 소유권, flow-corpus control-flow 제공 |
| [retire-ckg-node-id.md](retire-ckg-node-id.md) | [포인터] `ckg_node_id` 은퇴 → `canonical_id` 단일화 (마스터: cks `docs/retire-ckg-node-id.md`). CKG는 코드 변경 없음 — canonical_id 커버리지만 선택 과제 |
| [REMAINING-WORK-2026-07-10.md](REMAINING-WORK-2026-07-10.md) | 문서 vs 코드 대조로 추린 미완료·오류만: CAPABILITY-AUDIT의 P0 3건은 이미 구현(stale 정정 대상), 실제 열린 항목은 미push 커밋 + 선택 과제 |
| [coordination-reindex-migration-2026-07-10.md](coordination-reindex-migration-2026-07-10.md) | CKG → CKV 회신(재인덱싱·마이그레이션·무중단 6문항): Q1 graph 다이제스트 신규·Q2 cold 원자성 개선·Q6 증분 주석 정정이 실작업; Q3/Q4/Q5는 충족. 구현은 협의 완료 후 |
| [eval-trajectory.md](eval-trajectory.md) | 11-cycle eval trajectory + metrics progression |
| [SELF-VERIFICATION.md](SELF-VERIFICATION.md) | Self-verification manual / checklist |
| [CAPABILITY-AUDIT.md](CAPABILITY-AUDIT.md) | North-star → capability gap mapping (requirements reference) |
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
