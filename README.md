# CKG — Code Knowledge Graph

Parse Go / TypeScript / Solidity source into a queryable graph. Browse it in 3D.
Query it from Claude Code via MCP. Validate hypotheses about graph-context vs
raw-file context with the built-in eval runner.

## Quick start (5 minutes)

```bash
git clone https://github.com/0xmhha/code-knowledge-graph
cd code-knowledge-graph
make build
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth
./bin/ckg serve --graph=/tmp/ckg-synth --open      # opens browser at localhost:8787
```

In Claude Code:

```bash
claude mcp add ckg --command ./bin/ckg --args "mcp,--graph=/tmp/ckg-synth"
```

To run the eval:

```bash
export ANTHROPIC_API_KEY=...
./bin/ckg eval --tasks='eval/tasks/synthetic-*.yaml' --graph=/tmp/ckg-synth \
               --baselines=alpha,beta,gamma,delta --out=eval/results
cat eval/results/report.md
```

## Deployment

`ckg serve` ships an embedded viewer for single-developer local use. For
shared or production deployments, prefer the **production-split** pattern:
host the viewer as a static site and run the API separately so each can
scale, cache, and be auth-fronted independently.

```bash
# 1. Build once. Static bundle = viewer assets + chunked JSON of the graph.
./bin/ckg export-static --graph=/tmp/ckg-real --out=/srv/ckg/static
#    Drop /srv/ckg/static behind any HTTP server (nginx, S3, Cloudflare Pages…).

# 2. Run the API alongside, without the embedded viewer mount.
./bin/ckg serve --graph=/tmp/ckg-real --port=8787 --no-viewer
#    Front it with your reverse proxy. /api/* is the only surface.
```

The static viewer reads `/api/*` from whatever origin it's served from, so a
reverse proxy that maps `/api/* → localhost:8787` and `/* → /srv/ckg/static`
gives a single hostname.

For viewer development (editing `web/viewer-next/` against a live graph):

```bash
make viewer
CKG_DEV_VIEWER_DIR=$(pwd)/internal/server/web_assets \
  ./bin/ckg serve --graph=/tmp/ckg-real --open
# Re-run `make viewer` after edits — browser reload picks them up; ckg
# binary stays running.
```

## Documentation

- `docs/STUDY-GUIDE.md` — background on Leiden / MCP / staleness / tree-sitter / 3D layout
- `docs/SCHEMA.md` — node and edge enumeration
- `docs/ARCHITECTURE.md` — subcommand + pipeline overview
- `docs/EVAL.md` — baseline + scoring details
