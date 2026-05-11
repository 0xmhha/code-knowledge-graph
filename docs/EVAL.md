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
| gamma | find_*, get_subgraph, search_text | granular ping-pong (V0: not actually multi-turn — see L3 below) |
| delta | get_context_for_task | smart 1-shot ★ |

## Hypotheses

- **H1**: δ ≤ 50% of α tokens
- **H2**: δ score ≥ α score (no regression)

The auto-generated report.md tabulates both.

## Backend selection

`--llm-backend` switches between two transports:

| Backend | Flag | Token attribution | When to use |
|---|---|---|---|
| **api** | `--llm-backend=api` (requires `ANTHROPIC_API_KEY`) | Anthropic API `usage` block — `input_tokens` covers system prompt + context + user message in one figure | **H1/H2 정량 비교**. Counts are the unambiguous billing-aligned numbers and reflect what `δ vs α` actually consumes |
| cli | `--llm-backend=cli` | `claude --output-format json` `usage` block — Claude CLI internally attributes system + cached context to `cache_read_input_tokens`; only the trailing user message lands in `input_tokens` | Useful for smoke runs / when `ANTHROPIC_API_KEY` is unavailable, **but not for H1**: `input_tokens` collapses to single digits across all four baselines, so the δ vs α ratio compares constants |

### CLI mode token classification limit (verified 2026-05-11)

The 2026-05-11 go-stablenet verification recorded:

```
baseline  avg input_tokens  avg cached_tokens
α         9                 251,628
β         9                 136,120
γ         10                300,085
δ         11                334,824
```

The `input_tokens` ≈ 10 for every baseline is **not a CKG eval bug** — it
is how Claude CLI reports usage. The retrieved context (file dump for α,
subgraph for β, smart context for δ) ends up entirely in
`cache_read_input_tokens` because the CLI prepends it via its caching
layer. H1's "δ uses ≤ 50% of α input_tokens" therefore degenerates to
"single-digit ratio of single digits" under CLI.

**Mitigation**:
1. Run H1/H2 comparisons with `--llm-backend=api` (canonical billing-aligned
   numbers).
2. For CLI-only environments, treat `cached_tokens` as the effective input
   load and compare `input + cached` across baselines (the `cached_tokens`
   column is already emitted in `results.csv` and surfaced in `report.md`).
3. cli `input_tokens` are still useful for measuring *delta-over-context*
   (per-turn user prompt growth), just not absolute retrieval cost.

### Eval output CSV columns (post-2026-05-11)

`results.csv` carries 10 columns:

| column | meaning |
|---|---|
| task_id | Task.ID from YAML |
| baseline | alpha / beta / gamma / delta |
| input_tokens | per-call input tokens (see backend note above) |
| output_tokens | model's generated tokens |
| cached_tokens | `cache_read_input_tokens` + `cache_creation_input_tokens` |
| score | numeric task score (0..1) |
| latency_ms | wall-clock time for the LLM call |
| num_tool_calls | tool invocations during the run (γ only in V0) |
| stale | true if the graph manifest moved during the run |
| raw_output | LLM's raw response text — added 2026-05-11 for post-hoc score audit (L2 fix) |

The `raw_output` column is the canonical surface for diagnosing
unexpected low scores; it captures the response the scorer used, so
issues like `extractSymbols` mismatch (LLM said `*pkg.Func`, expected
said `pkg.Func`) are visible without re-running the eval.
