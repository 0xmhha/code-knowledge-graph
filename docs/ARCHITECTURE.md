# CKG Architecture (V0)

Single Go binary `ckg` with five subcommands sharing a SQLite store.

```
detect → parse → link → graph → cluster → score → persist
```

- **Parsers**: `golang.org/x/tools/go/packages` (Go), tree-sitter (TS/Sol)
- **Cluster**: package-tree (deterministic) + Leiden topic overlay (3 resolutions)
- **Storage**: `modernc.org/sqlite` (CGO-free), embedded schema, blobs in DB
- **Viewer**: vanilla JS + lit-html + 3d-force-graph, embed.FS
- **MCP**: stdio, six tools, in-process Store reads
- **Eval**: 4 baselines × N tasks → CSV + report.md

See `spec-ckg-v0-prototype.md` §3 for the full diagram and §4 for the build pipeline.
