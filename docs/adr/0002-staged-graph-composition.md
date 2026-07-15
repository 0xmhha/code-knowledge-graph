# ADR-0002: Staged graph composition — deterministic production core + test overlay

- **Status:** Accepted
- **Date:** 2026-06-29
- **Supersedes:** —

## Context

CKG must produce a **deterministic** graph: the same source tree at the same
commit must yield the same graph on any machine. This is foundational — the
CKG↔CKV match-rate and the coding-agent PR-77 A/B (see
`docs/archive/coordination-response-ckg-2026-06-29.md`) are only valid if the measured
graph is reproducible.

The Go loader sets `Tests:true` in `packages.Load`
(`internal/detect/golang.go:156`) so `_test.go` files surface — test code is
deliberately indexed because it is high-value **few-shot** material (it shows how
production APIs are actually called). That intent is correct and is retained.

But `Tests:true` makes `packages.Load` return, for a package `P`, **both** the
primary build package `P` and a **test-variant** package `P [P.test]` whose
`CompiledGoFiles` re-includes every production file of `P` (plus its `_test.go`
files). `buildFileIndex` (`internal/parse/golang/parser.go`) flattened all
packages into one `path → typedFile` map with **first-seen-wins** dedup, on the
assumption that the test-variant's `TypesInfo` is equivalent for shared
production files.

The defect: **`packages.Load` does not guarantee package order**, so which
variant is "first-seen" for a given production file is nondeterministic. Measured
on go-stablenet (`core/state` + `p2p` + `ethclient`, 2026-06-29 probe):
**17.5% of production files were owned by a test-variant package** rather than
their primary package. The same source can therefore resolve a production file's
symbols under a different package context (the test variant has the package's
`_test.go` types additionally in scope) across runs/machines — a latent
non-reproducibility this project cannot tolerate, and one that feeds
type-resolution-dependent passes (concurrency receiver matching, field-touch
analysis).

Scope note (to prevent a recurring misdiagnosis): this is **not** the cause of
the ~90% `canonical_id` population on Go `Method` nodes. That is by design —
`EmitPromotedMethods` (`internal/parse/golang/promoted.go`) materialises synthetic
nodes for embedded-field method promotion and intentionally leaves their
`canonical_id` empty (the *declaring* method carries the id). The test-variant
package shares the primary's import path, so `canonical_id` values are identical
either way; what the order-dependence affects is reproducibility and resolution
context, not the id.

`make gstable` compiles only the production package set into the binary; `_test.go`
files are not in it. So "what is the canonical code" and "what is few-shot test
context" are two distinct tiers that the build had been conflating.

## Decision

Compose the graph in **two explicit, deterministic stages**, each with a single
documented selection rule. File→package ownership is **order-independent**.

1. **Stage 1 — production core (authoritative).** Every production (`.go`,
   non-`_test.go`) file is owned by its **primary build package** — the
   non-test-variant package, i.e. the `make gstable` / `go build` compile set. A
   production symbol's nodes (and its `canonical_id`) are always derived from the
   primary package's `TypesInfo`. A **test-variant package may never override**
   the resolution of a production file.
2. **Stage 2 — test overlay (additive few-shot).** `_test.go` files (which exist
   only in test-variant / external-test packages) contribute their **own**
   symbols (`TestXxx`, `Example*`, helpers, test-only types) and their edges into
   production symbols. They never re-emit production symbols. (Richer
   test-scope semantics — explicit `scope=test` marking, exclusion from
   canonical-id measurement denominators — are follow-up work tracked in Tier 3,
   not required by this ADR.)

A package is a **test variant** iff its `packages.Package.ID` contains `.test]`
(the `P [P.test]` form) or ends in `.test` (the synthesized test-main). The
composition rule: assign primary packages first, then let test variants fill only
files no primary package compiled (i.e. the `_test.go` files).

Because this changes how production symbols are resolved (and makes a previously
order-dependent result deterministic), a rebuild must repopulate the affected
columns: **bump the cache-key `SchemaVersion`** in `internal/buildpipe/cache.go`
(not the manifest one) — see CLAUDE.md "two SchemaVersion constants".

## Consequences

- The graph becomes **reproducible**: production-file ownership no longer depends
  on `packages.Load` order. The canonical graph used for cross-repo measurement
  (CKG↔CKV match-rate, PR-77 A/B) is now trustworthy.
- Test code is **retained** as few-shot context (Stage 2), so no retrieval value
  is lost; it is simply prevented from shadowing the production core.
- Existing cached graphs are invalidated by the cache-key bump and rebuild
  deterministically. Live status / measurement numbers: Tier 3
  `docs/archive/coordination-response-ckg-2026-06-29.md` and
  `docs/archive/symbol-identity-remaining-work.md`.
- Stage 2's explicit test-scope semantics are deferred; Stage 1 already stops the
  pollution by giving primary packages ownership.

## Alternatives considered

- **`Tests:false` (drop test files entirely).** Rejected: loses the few-shot
  value of test code, which is a deliberate, stated goal. The problem was never
  "tests are indexed" but "test variants override production resolution".
- **Keep first-seen-wins but sort packages.** Rejected as fragile: it encodes the
  invariant in a sort comparator far from the dedup site. Classifying primary vs
  test-variant and giving primary explicit priority states the rule where it is
  enforced.
- **Dedup downstream (drop duplicate nodes after the fact).** Rejected: the
  duplication is in *resolution context*, not just node rows — the wrong
  `TypesInfo` produces wrong/empty `canonical_id` before any node-level dedup
  could run.
