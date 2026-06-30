# ADR-0003: Deprecate the PostgreSQL storage backend

- **Status:** Accepted
- **Date:** 2026-06-29
- **Supersedes:** —

## Context

CKG has two storage backends behind the `Store` interface: the default local
**SQLite** `graph.db`, and an **opt-in PostgreSQL** backend selected by passing a
`--db <DSN>` flag (`cmd/ckg/build.go`, `serve.go`, `validate.go`) or via the
dedicated `ckg export-postgres` command. The intended use case was a shared,
server-hosted graph DB for multi-client `serve` instead of a local file.

In practice:

- **It is unused.** Every CKG/CKV/CKS workflow uses the local SQLite graph; no
  consumer points at a Postgres DSN.
- **It is untested.** `.github/workflows/ci.yml` provisions no Postgres service,
  so the `pgStore` paths are not exercised by `go test -race` / the eval gate.
- **It already lags the SQLite schema.** `pgStoreSchema` carries neither
  `canonical_id` (schema 1.19, ADR-0001) nor `simple_name` (schema 1.22);
  `pgStore.FindByCanonicalID` is a documented not-found stub
  (`internal/persist/postgres_store.go`). Keeping it at parity is a standing tax
  on every additive schema change for a backend nobody runs.

This is the open decision tracked as item 7 / Tier C ("C1") in
`docs/archive/symbol-identity-remaining-work.md`: implement Postgres `canonical_id`
parity, or stop treating Postgres as a parity target.

## Decision

**Deprecate the PostgreSQL backend.** SQLite is the sole supported and maintained
storage backend.

1. **No parity obligation.** New schema columns / features are NOT required to
   mirror to `pgStore`. The existing gaps (`canonical_id`, `simple_name`,
   `FindByCanonicalID` stub) are accepted as deprecated-backend gaps, **not
   bugs** — item 7 / C1 is closed by this decision, not by implementing parity.
2. **Keep the code compiling, mark it deprecated.** The `--db` flags,
   `OpenPostgres*`, `pgStore`, the exporter, and `ckg export-postgres` remain so
   nothing breaks, but are documented as deprecated/unsupported. Do not invest in
   extending them.
3. **CI is not expected to test Postgres.** No Postgres service is added to CI;
   the absence of pg coverage is intentional, not a gap to close.
4. **Removal is a separate, later decision.** If it is confirmed that no external
   consumer depends on the backend, a follow-up ADR + PR may delete it
   (≈1,900 LOC across `postgres_store.go`, `postgres_exporter.go`,
   `export_postgres.go`, plus the `pgx` dependency). This ADR does **not** remove
   it — deprecate first, reversibly.

## Consequences

- The last open symbol-identity item (item 7 / C1) is resolved: no Postgres
  `canonical_id` work. Update `docs/archive/symbol-identity-remaining-work.md` to point
  item 7 / Tier C at this ADR.
- Future additive-schema changes only round-trip through the SQLite
  reader/writer; reviewers should not request Postgres mirroring.
- A later removal PR becomes a clean, self-contained change guarded by this ADR's
  "confirm no consumers first" condition.

## Alternatives considered

- **Implement Postgres `canonical_id` (+ `simple_name`) parity.** Rejected:
  effort spent making an unused, CI-untested backend match SQLite, with the same
  tax recurring on every future schema change.
- **Remove the backend now.** Deferred, not rejected: removal is more invasive
  (drops a public command and the `pgx` dependency) and harder to reverse.
  Deprecate-first records the decision immediately while keeping the door open,
  per CLAUDE.md "supersede, don't delete" / "destructive cleanup = plan first".
- **Keep it as a silent best-effort backend.** Rejected: "compiles but silently
  lags schema" is exactly the confidently-wrong failure mode CKG avoids — an
  explicit deprecation is honest about the support level.
