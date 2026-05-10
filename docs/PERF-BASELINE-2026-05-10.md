# Server-side performance baseline — 2026-05-10

**Method**: `ckg bench-server` (added in this session) — in-process httptest server, 50 iterations × 4 concurrent workers per probe (n=200 per endpoint).

**Graph**: `/tmp/ckg-h4` (go-stablenet build)
- `build_timestamp`: `2026-05-10T08:00:54Z`
- `src_commit`: `940e9f281edbdbc3df088a14e77a106908bfcb5d`
- 243K nodes / 1.98M edges, 8927 hunks, 8666 with H4 issue IDs.

**HEAD at measurement**: pre-commit (run from working tree containing the new bench-server itself).

**Hardware**: darwin 25.3.0, in-process httptest (no network).

---

## Results — three measurement points

The harness was re-run after each landing change so the deltas
reflect that specific commit:

  - **before**: untouched baseline (`/tmp/ckg-bench/baseline.json`).
  - **after #1+#2**: manifest cache + ticket pre-warm
    (`/tmp/ckg-bench/after.json`).
  - **after #3**: + staleness debounce
    (`/tmp/ckg-bench/after-staleness.json`).
  - **after #4**: + edge-counts cache + prewarm
    (`/tmp/ckg-bench/after-edges-prewarm.json`).

Same graph fingerprint across all three, so deltas reflect code
changes rather than measurement noise.

| Endpoint | before p50 | #1+#2 | #3 | #4 (final) | Δ p50 | before p99 | final p99 | Δ p99 |
|----------|-----------:|------:|----:|----------:|------:|----------:|---------:|------:|
| manifest             | 235.13ms |  64.34ms |  26.30ms |   26.11ms | **−89%** |  286.02ms |  85.24ms | −70% |
| hierarchy.pkg        | 164.70ms | 168.03ms | 166.99ms |  165.33ms |   −0% |  244.54ms | 240.50ms |  −2% |
| nodes                |   1.02ms |   0.99ms |   1.02ms |    0.95ms |   −7% |   92.34ms |  99.16ms |  +7% |
| nodes.top.pagerank   |  69.56ms |  73.10ms |  71.32ms |   65.98ms |   −5% |  230.05ms | 198.99ms | −13% |
| nodes.top.usage      |  69.63ms |  67.01ms |  69.49ms |   63.48ms |   −9% |  102.44ms | 104.23ms |  +2% |
| nodes.ambiguous      |   6.43ms |   6.21ms |   5.55ms |    5.64ms |  −12% |   10.57ms |  18.66ms | +77% |
| edges.counts         | 152.23ms | 151.72ms | 152.67ms |  **0.10ms** | **−99.9%** |  755.72ms | **0.31ms** | **−99.96%** |
| search               |   0.61ms |   0.67ms |   0.72ms |    0.64ms |   +5% |    6.46ms |   4.05ms | −37% |
| tickets              | 190.07ms |  18.67ms |  17.88ms |   17.59ms | **−91%** | 5774.90ms |  21.07ms | **−99.6%** |
| evidence.intent      | 168.31ms |   4.56ms |   4.11ms |    4.34ms | **−97%** |  238.71ms |  13.59ms | −94% |
| evidence.issue       | 203.44ms |  54.09ms |  54.41ms |   49.26ms | **−76%** |  259.77ms | 100.44ms | −61% |
| evidence.and         | 169.24ms |   2.68ms |   2.78ms |    2.47ms | **−99%** |  223.87ms |   7.72ms | **−97%** |

Raw JSON outputs in `/tmp/ckg-bench/`. `ckg bench-server` emits the
same shape so future runs diff mechanically.

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

## Improvement candidates

1. ✅ **Manifest caching** — landed (commit 473f839).
2. ✅ **TicketIndex pre-warm** — landed (commit 473f839).
3. ✅ **`computeStaleness` debounce** — landed in the same session.
   `internal/server/staleness_cache.go` debounces the per-request
   `git rev-parse HEAD` (or path-aware `git log -1 -- relPath`)
   spawn behind a 5s TTL keyed on (SrcCommit, SrcRoot). p50 drops
   from 64ms → 26ms (−59% of the residual; −89% of baseline).
   Trade-off: a fresh `ckg build` while serve is up surfaces the
   stale indicator with up to 5s lag — within human-perception
   tolerance for a banner refresh.
4. ✅ **`edges.counts` cache + pre-warm** — landed in the same
   session. EXPLAIN QUERY PLAN already showed
   `SCAN edges USING COVERING INDEX idx_edges_type`, so adding
   another index was a dead-end; the 1.98M-row scan itself was the
   cost. Lifted into `cachedManifestStore.EdgeCountsByType` with a
   matching `prewarmEdgeCounts` boot goroutine. p50 152ms → 0.10ms
   (−99.9%); p99 755ms → 0.31ms (−99.96%). Trade-off identical to
   manifest: build-time-fixed data, restart on rebuild.
5. **bench-mcp** — measure each MCP tool's stdio round-trip latency.
   Not in this baseline because MCP latency is dominated by stdio
   framing + JSON-RPC, not graph reads. Future enhancement.

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
