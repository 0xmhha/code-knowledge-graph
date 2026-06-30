# CKG — Code Knowledge Graph

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL_v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![CI](https://github.com/0xmhha/code-knowledge-graph/actions/workflows/ci.yml/badge.svg)](https://github.com/0xmhha/code-knowledge-graph/actions/workflows/ci.yml)

Build a queryable **knowledge graph** from a code path. Point CKG at a
directory and it parses the source (Go / TypeScript / Solidity) into a
graph of files, symbols, and relationships you can query from the CLI,
an MCP-enabled LLM, or a 3D web viewer.

## Features

- **Multi-language parsing** — Go, TypeScript, Solidity via tree-sitter
- **Persistent graph store** — SQLite (the maintained backend; the PostgreSQL backend is deprecated, see [ADR-0003](docs/adr/0003-deprecate-postgres-backend.md))
- **Multiple query surfaces** — REST API, MCP server, 3D viewer
- **Incremental builds** — file-level caching for fast re-indexing
- **Rich schema** — 33 node kinds, dependency / call / git-history edges

## Quick start

```bash
git clone https://github.com/0xmhha/code-knowledge-graph
cd code-knowledge-graph
make build

# 1. Build a graph from any code path
./bin/ckg build --src=/path/to/repo --out=/tmp/ckg

# 2. Query via HTTP + 3D viewer
./bin/ckg serve --graph=/tmp/ckg --open      # http://localhost:8080

# 3. Query from Claude Code via MCP
claude mcp add ckg --command ./bin/ckg --args "mcp,--graph=/tmp/ckg"
```

## Building a graph

`ckg build` parses a source tree into a graph database (`<out>/graph.db`).
Common patterns:

```bash
# Whole tree, auto-detect languages (Go / TypeScript / Solidity / proto)
./bin/ckg build --src=/path/to/repo --out=/tmp/ckg

# Restrict to specific languages
./bin/ckg build --src=/path/to/repo --out=/tmp/ckg --lang=go,sol

# Build from a specific commit — uses a git worktree, leaves the --src
# working tree untouched, and indexes only that commit's tracked files
./bin/ckg build --src=/path/to/repo --out=/tmp/ckg --at-commit=<sha>

# Index only a curated file set via a --files-from whitelist
./bin/ckg build --src=/path/to/repo --out=/tmp/ckg \
    --files-from=eval/stablenet/stablenet-files-with-tests.json
```

The build is **deterministic**: the same source tree + commit + ckg binary
yields the same graph (see [`docs/adr/0002-staged-graph-composition.md`](docs/adr/0002-staged-graph-composition.md)).

**Filtering (`--files-from`).** A JSON `{ "include": [...], "exclude": [...] }`
of globs (`**` spans any path depth). Without it, ckg indexes the whole tree
(every `.go`/`.ts`/`.sol`/`.proto`, tests included). Two ready-made filters for
the go-stablenet corpus live under [`eval/stablenet/`](eval/stablenet/):

| Filter | Tests | Use |
|---|---|---|
| `stablenet-files.json` | excluded | clean retrieval-eval corpus |
| `stablenet-files-with-tests.json` | included | knowledge-data (cks/ckv), keeps test few-shot value |

Add `--out-tag=auto-commit-hash` to suffix the output dir with the source's
short commit SHA (e.g. `--out=/data/stablenet` → `/data/stablenet-0bf2f4d1b`).
Run `ckg build --help` for the full flag list.

## Commands

| Command | Purpose |
|---|---|
| `ckg build`           | Parse a code path into a graph database |
| `ckg serve`           | HTTP API + embedded 3D viewer |
| `ckg mcp`             | MCP server for LLM agents (Claude Code, etc.) |
| `ckg export-static`   | Export viewer + chunked JSON for static hosting |
| `ckg export-postgres` | Migrate a SQLite graph to PostgreSQL (deprecated, ADR-0003) |
| `ckg eval`            | Compare graph-context vs raw-file context on benchmark tasks |
| `ckg audit`           | Validate graph integrity |

Run `ckg <command> --help` for flags.

## Production deployment

`ckg serve` ships an embedded viewer for local use. For shared
deployments, split the static viewer from the API:

```bash
./bin/ckg export-static --graph=/tmp/ckg --out=/srv/ckg/static
./bin/ckg serve --graph=/tmp/ckg --port=8080 --no-viewer
```

Front both with a reverse proxy: `/api/* → :8080`, `/* → /srv/ckg/static`.

## Documentation

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — pipeline + subcommand overview
- [`docs/SCHEMA.md`](docs/SCHEMA.md) — node and edge enumeration
- [`docs/EVAL.md`](docs/EVAL.md) — eval harness and scoring
- [`docs/STUDY-GUIDE.md`](docs/STUDY-GUIDE.md) — background on Leiden, MCP, tree-sitter, 3D layout

## Contributing

Contributions are welcome. To get started:

1. Fork the repository and create a feature branch from `main`.
2. Run `make test` and `make lint` before submitting.
3. Use conventional commit prefixes (`feat:`, `fix:`, `docs:`, ...).
4. Open a pull request describing the change and its motivation.

For larger changes, please open an issue first to discuss the design.
Report bugs and request features at
<https://github.com/0xmhha/code-knowledge-graph/issues>.

## License

CKG is licensed under the **GNU Affero General Public License v3.0** —
see [LICENSE](LICENSE).

AGPL-3.0 requires that if you run a modified version of CKG as a
network-accessible service, you must make the corresponding source
available to its users.
