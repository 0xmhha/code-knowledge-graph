# ckg Follow-ups Surfaced by cks Dogfood — 2026-05-19

> Source: `code-knowledge-system` (cks) ran `make dogfood-eval` indexing cks itself
> via ckg. This document lists ckg-side gaps and bugs **observed from a downstream
> consumer's perspective**. The reproductions and fixes belong in this repo; the
> cks-side wrappers will follow.
>
> Companion docs:
> - cks repo: `docs/followups-from-dogfood-2026-05-19.md`
> - ckv repo: `<ckv>/docs/followups-from-cks-dogfood-2026-05-19.md`

## Context

cks consumes ckg via `pkg/store.Reader` (`OpenReadOnly` + `SearchFTS` + symbol /
edge traversal). cks-side adapter is `internal/ckgclient/real.go`. All findings
here are derived from indexing cks itself, querying through the cks composer, and
scoring against curated `expected_citations` in `eval/scenarios/*.yaml`.

ckg status: in active development. Symptoms below are reported as a downstream
consumer; root-cause analysis and fixes belong to this repo.

## Open items (ckg-side)

| # | Priority | Item | Evidence from cks | Suggested direction |
|---|---|---|---|---|
| CKG-1 | High | `SearchFTS` does not return BM25 score or rank | `internal/ckgclient/real.go:149-155` synthesizes `1 - i/(N+1)` as a fake score because the call returns nodes only | Add a numeric BM25 score (or at minimum a stable rank) to the `Node`-like return of `SearchFTS`. Downstream rerankers need to distinguish "1 strong unique-identifier hit" from "5 weak common-word hits" — without a real score the consumer has to fall back to MAX heuristics and still loses information. The cks-side fix `b0ff3fa fix(stage1): rerank by max(hit.Score), not sum` patches the worst pathology but it is a workaround. |
| CKG-2 | High | `SearchFTS(query, limit)` has no filter API | `internal/ckgclient/real.go:140-147` over-fetches by `FilterOverfetchRatio=3` and post-filters client-side on `Language` and `PathGlob` | Native filter args (at least `language` and a glob/path-prefix) eliminate the over-fetch tax and the off-by-recall bug when filters drop most of a small page. Even a `WHERE language = ?` push-down would help — the path glob can stay client-side if expensive. |
| CKG-3 | High | No cross-commit / multi-snapshot search | `Filter.CommitHash` is in the cks API contract but ignored — `internal/ckgclient/real.go:144-147` documents "the entire index represents one snapshot" | Confirm whether cross-commit search is on the roadmap. If yes, expose snapshot selection. If no, document the single-snapshot constraint in `pkg/store/store.go` so consumers don't add per-commit filters that silently no-op. |
| CKG-4 | Mid | Symbol traversal: single-kind only per call | cks Stage 2 has to issue N round-trips for N `SymbolKind`s (fns + types + interfaces for `arch_explain` intent) | Accept `kinds []SymbolKind` in the symbol lookup API and return a kind-tagged result. Cuts round-trips and lets cks dedupe by Citation key in one pass. |
| CKG-5 | Mid | Graph traversal depth: cks observes weak cross-file expansion at depth=1 | `mcp-tool-handlers` and `stamp-integrity-lookup` scenarios plateau at recall=0.67 even after the rerank fix | Probe: with `TraversalDepth=2` does recall climb without an unacceptable latency penalty? If yes, consider whether the default for the existing traversal API should be 2, or whether cks should just raise it. Either way, the answer needs measurement on real graphs, which is ckg's territory. |
| CKG-6 | Mid | Surface naming: `pkg/store.Reader` is what cks codes against today | cks `internal/ckgclient/real.go:1-50` type-aliases over the internal `persist.StoreReader` | Confirm `pkg/store.Reader` (or whichever public surface) is the stable API for external consumers. If it is, fold the internal alias upward; if not, point cks at the intended public type. Right now cks depends on a thin shim it created itself. |
| CKG-7 | Low | Manifest fields exposed to consumers | cks has a `ManifestSnapshot` mirror struct because the upstream `persist.Manifest` is internal | Re-expose a stable, minimal `Manifest`-like type (commit hash, schema version, index timestamp) via `pkg/store`. cks uses it for the `cks.ops.health` MCP tool and for the `Citation.CommitHash` drift signal. |

## Reproduction from cks side

```bash
# Build a fresh ckg index of cks itself:
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-system
make ckg-index            # writes .ckg-data/graph.db
make dogfood-eval         # cks-eval runs scenarios, writes eval/reports/baseline-dogfood.json
make dogfood-eval-summary # prints per-scenario recall + per-intent rollup
```

The cks adapter that exercises the ckg API is `internal/ckgclient/real.go` —
look there for the exact calls (`SearchFTS`, `SymbolLookup`, `Traverse`)
and the workarounds documented in comments.

## Suggested order

CKG-1 (real score) → CKG-2 (filter pushdown) → CKG-4 (multi-kind) yields the
biggest cks recall lift in the smallest API surface area. CKG-3 / CKG-5 / CKG-7
are clarifications more than new code. CKG-6 is a one-line `type` move once a
public name is chosen.
