# CKG — Vision & Purpose (Tier 1, north star)

> **This is the durable "why" of the project.** It is an **append-mostly** file:
> the purpose, the three-project triangle, and the first-class metric do not
> change as implementation evolves. When cleaning up or consolidating docs,
> this file is an **input, never a target** — do not delete or shrink it to make
> a status report tidy. Dated numbers (eval scores, node/edge counts, schema
> versions) live in Tier 3 status docs, **not here**.

## What CKG is

**CKG (Code Knowledge Graph)** turns a source tree into a **graph database**.
It is a single Go binary (CGO-free by default) that parses **Go / TypeScript /
Solidity** and emits a deterministic graph (SQLite default, PostgreSQL opt-in)
of symbols, call edges, temporal hunks, and the distributed surface (HTTP/gRPC).

## The three-project triangle (CKG / CKV / CKS)

CKG is *one* of three sister projects designed to compose:

- **CKG** (this repo) — source → **graph** DB. Symbol identity, call edges,
  temporal hunks, distributed surface. Deterministic SQL store.
- **CKV** (`code-knowledge-vector`, separate repo) — source → **vector**
  knowledge + **vocabulary bridge** (Korean / vague → exact English keywords).
- **CKS** (`code-knowledge-system`, separate repo) — orchestrator. A coding
  agent talks to CKS; CKS uses CKV (vocab bridge) → CKG (keyword retrieval) →
  LLM (request + retrieved code), runs tests, and opens a PR on green.

## CKG's job in the triangle (the responsibility boundary)

> **Given exact English keywords + a project's graph DB, return precisely the
> code the coding agent needs.**

Concretely, for a keyword (e.g. `"GovStaking deposit"`) CKG returns:
1. **Related code** — definition + call relationships + type dependencies.
2. **PR history** — which PR changed what, and why.
3. **Concurrency-impacted modules** — code affected via shared lock / channel /
   atomic.

## The first-class metric

> Because that is CKG's job, **keyword-retrieval accuracy is the first-class
> metric.** The standing north star is: **6-axis graph build + keyword-query
> retrieval at high accuracy**, measured by the `eval` surface against a gold
> set. (The *current* score is a Tier 3 status fact, tracked in
> `docs/archive/eval-trajectory.md` and `eval/baseline/`, not here.)

## Core values

Three user-articulated values shape every design trade-off (moved here from
`docs/archive/PROJECT-BLUEPRINT-ALIGNMENT.md` §1.2 so they are not lost in cleanups):

1. **Accuracy** — return precisely the right code; wrong-answer is worse than
   no-answer (this is why retrieval accuracy is the first-class metric).
2. **Token economy** — supply dense, pre-computed knowledge so the LLM is
   called less often and with smaller, sharper context.
3. **Extensibility** — additive schema, language-pluggable parsers, stable
   `pkg/` contract; growth must not break existing graphs or consumers.

## Why CKG exists — the problem it removes

A plain `grep` / `glob` / `read` pipeline only finds *what literally exists* and
carries no meta-knowledge: it misses *why* something changed (PR/policy
history), *what else is impacted* (concurrency / cross-module), and *which
domain rules apply* (e.g. go-stablenet consensus / gas / validator / Byzantine
rules). That blindness forces re-work cycles and explodes token cost. CKG
supplies the structural, temporal, and impact knowledge that closes that gap,
so the LLM is called **less often and with more accurate context**.

## The six-axis graph

The graph emits edges across six axes (every axis has live emitters):
Structural · Semantic · Execution · Concurrency · Distributed · Temporal.
(The authoritative node/edge enumeration lives in `docs/SCHEMA.md` — Tier 2.)

## The public boundary (stable contract)

External consumers (cks, ckv) **must NOT** reach into `internal/`. The stable
public API is everything under `pkg/` (`pkg/types`, `pkg/store`, `pkg/bm25`,
`pkg/smartctx`, `pkg/evidence`, `pkg/impact`, …). Treat `pkg/` changes as
contract changes.

---

*Source basis: `docs/archive/PROJECT-OVERVIEW.md`,
`docs/archive/PROJECT-BLUEPRINT-ALIGNMENT.md` (both archived 2026-07-18),
`docs/ARCHITECTURE.md`. This file is the distilled, timeless statement of
intent; the fuller status references it drew from are now in `docs/archive/`.*
