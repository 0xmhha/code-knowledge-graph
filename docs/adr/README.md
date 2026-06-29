# Architecture Decision Records (ADR)

Each significant design decision is **one file** here, named
`NNNN-short-slug.md` (e.g. `0001-canonical-symbol-id.md`).

## Why ADRs

When a decision changes during implementation, we do **not** edit the old
rationale away — that is what caused "which design is right?" re-litigation. We
add a **new** ADR and mark the old one **Superseded by** it. The supersession
chain *is* the answer to "what's the current decision and why".

## Rules

1. **One decision per file.** Don't bundle.
2. **Never delete a decided ADR.** To change a decision, write a new ADR and set
   the old one's status to `Superseded by ADR-NNNN`, with a one-line reason.
3. **Status values:** `Proposed` · `Accepted` · `Superseded by ADR-NNNN` ·
   `Deprecated`.
4. **Ground truth for "is it done?"** is code + git, not the ADR. The ADR
   records the *decision*, status reports (Tier 3) record *implementation state*.
5. Register every new ADR in the index below and in `docs/DOC-MAP.md`.

## Template

```markdown
# ADR-NNNN: <title>

- **Status:** Proposed | Accepted | Superseded by ADR-NNNN | Deprecated
- **Date:** YYYY-MM-DD
- **Supersedes:** ADR-NNNN (or —)

## Context
What forces the decision? What constraints, what conflicting docs/code did we
find? (Cite file:line / commit when relevant.)

## Decision
What we decided, stated as the rule future readers should follow.

## Consequences
What becomes easier/harder. Migration impact. Follow-up work (link Tier 3 status
docs, don't inline the live status).

## Alternatives considered
Briefly, and why rejected.
```

## Index

| ADR | Title | Status |
|---|---|---|
| [0001](0001-canonical-symbol-id.md) | Canonical symbol identity (`canonical_id`) | Accepted |
| [0002](0002-staged-graph-composition.md) | Staged graph composition — deterministic production core + test overlay | Accepted |
| [0003](0003-deprecate-postgres-backend.md) | Deprecate the PostgreSQL storage backend (SQLite is the sole maintained target) | Accepted |
