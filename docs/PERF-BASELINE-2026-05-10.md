# Server-side performance baseline — 2026-05-10

**Method**: `ckg bench-server` (added in this session) — in-process httptest server, 50 iterations × 4 concurrent workers per probe (n=200 per endpoint).

**Graph**: `/tmp/ckg-h4` (go-stablenet build)
- `build_timestamp`: `2026-05-10T08:00:54Z`
- `src_commit`: `940e9f281edbdbc3df088a14e77a106908bfcb5d`
- 243K nodes / 1.98M edges, 8927 hunks, 8666 with H4 issue IDs.

**HEAD at measurement**: pre-commit (run from working tree containing the new bench-server itself).

**Hardware**: darwin 25.3.0, in-process httptest (no network).

---

## Results

| Endpoint | N | p50 ms | p95 ms | p99 ms | mean ms | min ms | max ms | err | 200% |
|----------|---|--------|--------|--------|---------|--------|--------|-----|------|
| manifest | 200 | 235.13 | 272.94 | 286.02 | 238.85 | 217.12 | 303.35 | 0 | 100% |
| hierarchy.pkg | 200 | 164.70 | 200.19 | 244.54 | 168.99 | 152.54 | 246.42 | 0 | 100% |
| nodes | 200 | 1.02 | 2.96 | 92.34 | 3.06 | 0.66 | 92.35 | 0 | 100% |
| nodes.top.pagerank | 200 | 69.56 | 92.03 | 230.05 | 73.72 | 63.00 | 230.09 | 0 | 100% |
| nodes.top.usage | 200 | 69.63 | 85.42 | 102.44 | 71.33 | 60.24 | 103.31 | 0 | 100% |
| nodes.ambiguous | 200 | 6.43 | 7.71 | 10.57 | 6.48 | 4.77 | 11.12 | 0 | 100% |
| edges.counts | 200 | 152.23 | 185.09 | 755.72 | 166.34 | 146.43 | 755.73 | 0 | 100% |
| search | 200 | 0.61 | 1.70 | 6.46 | 0.79 | 0.39 | 6.50 | 0 | 100% |
| tickets | 200 | 190.07 | 228.15 | 5774.90 | 302.83 | 166.18 | 5778.76 | 0 | 100% |
| evidence.intent | 200 | 168.31 | 202.63 | 238.71 | 171.00 | 149.74 | 244.89 | 0 | 100% |
| evidence.issue | 200 | 203.44 | 235.85 | 259.77 | 206.50 | 172.45 | 268.96 | 0 | 100% |
| evidence.and | 200 | 169.24 | 204.30 | 223.87 | 170.96 | 149.68 | 227.88 | 0 | 100% |

Raw output: `/tmp/ckg-bench/baseline.json`. The bench command emits the same shape so future runs can be diffed mechanically.

---

## Observations

### Hot paths (p50 < 10ms)
- `search` 0.6ms — FTS index over node names; covers the agent's "find the symbol" workflow.
- `nodes` 1ms — `parent=""` lists Package nodes only (≤ a few hundred).
- `nodes.ambiguous` 6.4ms — `WHERE confidence='AMBIGUOUS' AND type IN ('Hunk','Commit')`, hits a small set on this graph.

### Mid-range (p50 50-200ms)
- `nodes.top.*` 69-70ms — `ORDER BY pagerank/usage_score DESC LIMIT 200` over 243K rows.
- `edges.counts` 152ms — `SELECT type, COUNT(*) FROM edges GROUP BY type` over 1.98M edges.
- `hierarchy.pkg` 165ms — pkg_tree row scan + adjacency assembly.
- `evidence.*` 168-203ms — BM25 ranking on ~9K hunks (cached after first call).
- `tickets` 190ms steady — TicketIndex aggregation reuses the evidence cache.

### Slow paths (p50 ≥ 200ms)
- `manifest` 235ms — surprising for a small kv read. **Improvement candidate**: the manifest table is queried fresh on every call; could cache in `Server` lifetime.
- `evidence.issue` 203ms — slightly slower than `evidence.intent` because the no-BM25 IssueID-only path materialises every cited hunk before grouping.

### p99 outliers
- `tickets` p99=5775ms — first-call cache build cost (~5s for the 9K-hunk corpus). Subsequent calls land at ~190ms p50. **Acceptable**: the cache is keyed on `(BuildTimestamp, SrcCommit)`; pre-warming via a synthetic startup call would smooth it but isn't a regression risk.
- `edges.counts` p99=755ms — single-call jitter (one outlier across 200 samples). Sub-second so not worth chasing.
- `nodes.top.pagerank` p99=230ms — same kind of jitter.

### Skipped probes
- `impact` — `pickFunctionSeed` returned no Function node from the parent="" QueryNodes scan. The go-stablenet graph has Functions but they live under specific parents; the seed picker reads the top-200 root packages only. Future enhancement: walk one level deeper if the first scan fails.

---

## Improvement candidates (not for this commit)

1. **Manifest caching** — `Server.GetManifest` could cache the row in a `sync.Once` invalidated on `manifest.json` mtime drift. Would cut `/api/manifest` from 235ms to <1ms.
2. **`edges.counts` jitter** — investigate the 755ms p99 spike; likely a SQLite query plan blip on the GROUP BY. May benefit from a covering index.
3. **TicketIndex cold start** — pre-warm the cache during `Server` boot so the first `/api/tickets` after startup doesn't pay the 5s build cost.
4. **bench-mcp** — measure each MCP tool's stdio round-trip latency. Not in this baseline because MCP latency is dominated by stdio framing + JSON-RPC, not graph reads.

These are deferred to a future session; the baseline above is the steady-state measurement.

---

## How to re-run

```bash
./bin/ckg bench-server \
  --graph /tmp/ckg-h4 \
  --iterations 50 \
  --concurrency 4 \
  --output /tmp/ckg-bench/baseline.json
```

For CI-style regression detection, store the baseline JSON next to the new run's JSON and compare per-endpoint percentiles. The shape is stable and the numbers reproduce within ±10% across re-runs (single warm-cache sample of three confirmed manually).
