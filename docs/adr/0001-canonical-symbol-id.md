# ADR-0001: Canonical symbol identity (`canonical_id`)

- **Status:** Accepted
- **Date:** 2026-06-12 (foundation merged); recorded as ADR 2026-06-15
- **Supersedes:** —

## Context

CKG's job is to return *precisely* the code an agent asks for (see
`docs/VISION.md`). The pre-existing node identity, `qualified_name`, is the
short, suffix-searchable **display** form (e.g. `core.Size`) and is **not
globally unique**: the same short name recurs across packages/contracts (e.g. a
`Size` method in `core/types` vs `consensus/wbft/core`). When a lookup matched
more than one node, resolution silently picked `defs[0]` — i.e. it could return
the *wrong* definition. That directly undermines the first-class metric
(retrieval accuracy).

The design contract was decided cross-repo and merged in CKS
(`code-knowledge-system docs/symbol-identity-design.md`, PR #16). This ADR
records the **CKG-side decision**; live implementation status lives in the Tier 3
doc `docs/archive/symbol-identity-remaining-work.md`, not here.

## Decision

1. **Add a globally-unique, import-path-qualified `CanonicalID`** to every node,
   **additive** alongside `qualified_name` (JSON `canonical_id,omitempty`).
   `qualified_name` stays the short display/search form; `canonical_id` is the
   exact identity used for resolution.
2. **Identity format** (per language; the qualifier is the import path for Go,
   and the file/dir path where there is no import path):
   - Go function: `<importpath>.<Func>`
   - Go method: `<importpath>.(<*?RecvType>).<Method>` (pointer star preserved)
   - Go type/const/var: `<importpath>.<Name>`; field: `<pkg>.<Type>.<Field>`
   - Solidity: `<dir>/<Contract>.<func>(<paramTypes>)` — the parameter-type
     signature separates overloads; the version directory separates v1/v2
   - TypeScript / proto: file/package path as the qualifier (no import path)
   - When identity cannot be formed unambiguously (e.g. builtins with no
     package), leave `CanonicalID` **empty** rather than emit an ambiguous id.
3. **Resolution must be exact.** A short-name lookup that matches **>1 node is an
   error** for the traversal family (find_callers / get_subgraph /
   impact_analysis) — never a silent `defs[0]`. Add an exact `FindByCanonicalID`
   path; key the call resolver on canonical id where available.
4. **Additivity is mandatory:** node IDs (`sha256(qname|lang|startByte)`), edges,
   `qualified_name`, and all existing consumers stay unchanged. CKV alignment is
   positional, so **no re-embed** is required downstream.
5. **A reindex must repopulate `canonical_id`**, which means bumping the
   **cache-key** `SchemaVersion` in `internal/buildpipe/cache.go` (not the
   manifest back-compat one) — see CLAUDE.md "two SchemaVersion constants".

## Consequences

- Traversal results become provably unique or an explicit error — no silent
  wrong-definition returns. This is the mechanism by which retrieval accuracy is
  enforced, not just measured.
- Rollout is phased and cross-repo: Phase 1 (ckg identity + resolution), Phase 2
  (ckv additive field, no re-embed), Phase 3 (cks exact resolution + anchor
  kinds + data migration). Live per-phase status: see
  `docs/archive/symbol-identity-remaining-work.md`.
- Because the field is additive + `omitempty`, the **manifest** `SchemaVersion`
  is intentionally **not** bumped; only the **cache** key changes so rebuilds
  repopulate the column.

## Alternatives considered

- **Make `qualified_name` itself globally unique.** Rejected: it is the
  user-facing, suffix-searchable display form; lengthening it to full import
  paths would break search ergonomics and every existing consumer.
- **Keep `defs[0]` with heuristic ranking.** Rejected: any silent pick can
  return the wrong definition, which is exactly the failure the project exists to
  remove.
