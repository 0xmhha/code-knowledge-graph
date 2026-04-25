# CKG Eval (V0)

## Run

```bash
export ANTHROPIC_API_KEY=sk-...
ckg build --src=testdata/synthetic --out=/tmp/ckg-synth
ckg eval --tasks='eval/tasks/synthetic-*.yaml' --graph=/tmp/ckg-synth \
         --baselines=alpha,beta,gamma,delta --out=eval/results
cat eval/results/report.md
```

## Baselines

| Code | Tools allowed | Notes |
|---|---|---|
| alpha | none | raw file dump appended to user prompt |
| beta  | get_subgraph | one whole-graph fetch |
| gamma | find_*, get_subgraph, search_text | granular ping-pong (V0: not actually multi-turn) |
| delta | get_context_for_task | smart 1-shot ★ |

## Hypotheses

- **H1**: δ ≤ 50% of α tokens
- **H2**: δ score ≥ α score (no regression)

The auto-generated report.md tabulates both.
