# Archived

This repository (`code-knowledge-graph`, CKG) is **archived and read-only** as
of July 2026.

## Where the code went

CKG has been consolidated — with its full commit history — into the single
`knowledge-system` module:

**https://github.com/0xmhha/knowledge-system**

It now lives there as the **graph engine**:

| Old location (this repo) | New location (knowledge-system) |
|---|---|
| `internal/` | `internal/graph/` |
| `pkg/` (except bm25) | `pkg/graph/` |
| `pkg/bm25/` | `pkg/bm25/` (shared across engines) |
| `cmd/ckg/` | `cmd/graph/` |
| MCP entrypoint | `cmd/graph-mcp/` |
| `docs/` | `docs/graph/` |
| `web/viewer-next/` | `tools/viewer/` |
| `testdata/`, `eval/`, `Makefile`, `README.md`, `CLAUDE.md` | `graph/` |
| `policies/stablenet/`, `eval/stablenet/` | `projects/stablenet/` |

The two sister projects were archived and consolidated the same way:

- `code-knowledge-vector` (CKV) → `knowledge-system` vector engine
- `code-knowledge-system` (CKS) → `knowledge-system` system engine

## Status

No further development, issues, or pull requests happen here. All work
continues in `knowledge-system`. This repository is retained for historical
reference only.
