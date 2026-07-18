# CKG Architecture (1-page)

Single Go binary `ckg` (~20 cobra subcommands) exposing **five production
surfaces** — `build`, `serve`, `mcp`, `eval`, `audit` — over a SQLite store.

```
detect → parse → link → graph → cluster → score → persist   (7-pass build)
```

- **Parsers**: `golang.org/x/tools/go/packages` (Go), tree-sitter (TypeScript,
  Solidity), hand-rolled Proto parser (`internal/parse/proto`) — four languages.
- **Cluster**: package-tree (deterministic) + Leiden topic overlay (3 resolutions)
- **Storage**: `modernc.org/sqlite` (CGO-free), embedded schema, blobs in DB
  (PostgreSQL export deprecated — see `docs/adr/0003-deprecate-postgres-backend.md`)
- **Viewer**: Next.js 3D force-graph (`web/viewer-next`), embedded via `embed.FS`
- **MCP**: stdio, **ten** tools (`pkg/mcphandlers/registerall.go`), in-process Store reads
- **Eval**: keyword-retrieval fixtures + LLM baselines (α/β/γ/δ) → CSV + report.md

Authoritative detail: `docs/ARCHITECTURE-DETAILED.md`, `docs/CODE-STRUCTURE.md`,
`docs/SCHEMA.md`. "What is true now" = code + git.


