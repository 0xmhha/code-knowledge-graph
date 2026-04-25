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

## Documentation

- `docs/spec-ckg-v0-prototype.md` — full design spec
- `docs/STUDY-GUIDE.md` — background on Leiden / MCP / staleness / tree-sitter / 3D layout
- `docs/SCHEMA.md` — node and edge enumeration
- `docs/ARCHITECTURE.md` — subcommand + pipeline overview
- `docs/EVAL.md` — baseline + scoring details
