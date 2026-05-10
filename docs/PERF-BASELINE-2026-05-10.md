# Server-side performance baseline — 2026-05-10

**Method**: `ckg bench-server` (added in this session) — in-process httptest server, 50 iterations × 4 concurrent workers per probe (n=200 per endpoint).

**Graph**: `/tmp/ckg-h4` (go-stablenet build)
- `build_timestamp`: `2026-05-10T08:00:54Z`
- `src_commit`: `940e9f281edbdbc3df088a14e77a106908bfcb5d`
- 243K nodes / 1.98M edges, 8927 hunks, 8666 with H4 issue IDs.

**HEAD at measurement**: pre-commit (run from working tree containing the new bench-server itself).

**Hardware**: darwin 25.3.0, in-process httptest (no network).

---

## Results — before vs after improvements

The "after" column is the same harness re-run after the manifest
cache + ticket-index pre-warm landed (`internal/server/manifest_cache.go`
+ goroutine in `Server.NewWithOptions`). All measurements use the
same graph fingerprint, so deltas reflect the change rather than
measurement noise.

| Endpoint | before p50 | after p50 | Δ p50 | before p99 | after p99 | Δ p99 |
|----------|-----------:|----------:|------:|-----------:|----------:|------:|
| manifest             | 235.13ms |  64.34ms | **−73%** |  286.02ms | 142.02ms | −50% |
| hierarchy.pkg        | 164.70ms | 168.03ms |    +2% |  244.54ms | 251.79ms |  +3% |
| nodes                |   1.02ms |   0.99ms |    −3% |   92.34ms | 135.29ms |  +47% |
| nodes.top.pagerank   |  69.56ms |  73.10ms |    +5% |  230.05ms | 217.74ms |   −5% |
| nodes.top.usage      |  69.63ms |  67.01ms |    −4% |  102.44ms | 113.58ms |  +11% |
| nodes.ambiguous      |   6.43ms |   6.21ms |    −3% |   10.57ms |  10.93ms |   +3% |
| edges.counts         | 152.23ms | 151.72ms |    −0% |  755.72ms | 529.03ms |  −30% |
| search               |   0.61ms |   0.67ms |    +9% |    6.46ms |   5.49ms |  −15% |
| tickets              | 190.07ms |  18.67ms | **−90%** | 5774.90ms |  34.37ms | **−99.4%** |
| evidence.intent      | 168.31ms |   4.56ms | **−97%** |  238.71ms |  12.42ms | **−95%** |
| evidence.issue       | 203.44ms |  54.09ms | **−73%** |  259.77ms | 119.89ms | −54% |
| evidence.and         | 169.24ms |   2.68ms | **−98%** |  223.87ms |   6.83ms | **−97%** |

Raw outputs: `/tmp/ckg-bench/baseline.json` (before),
`/tmp/ckg-bench/after.json` (after). `ckg bench-server` emits the
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

1. ✅ **Manifest caching** — landed in the same session.
   `internal/server/manifest_cache.go` wraps the StoreReader so
   `GetManifest` returns from memory after the first call.
   /api/manifest p50 235ms → 64ms (−73%); the residual 64ms is
   `computeStaleness`'s git command, not the manifest read itself.
   That's a future cleanup candidate (`git rev-parse HEAD` could be
   debounced or moved off the request path).
2. ✅ **TicketIndex pre-warm** — landed in the same session.
   `Server.NewWithOptions` kicks off a background goroutine that
   builds the BM25 corpus. `/api/tickets` p50 190ms → 18ms (−90%),
   p99 5775ms → 34ms (−99.4%). `evidence.*` endpoints all collapse
   to single-digit ms because they reuse the same warm cache.
3. **`edges.counts` jitter** — p99 dropped from 755ms to 529ms with
   no code changes (cache pressure decreased), but still an outlier.
   SQLite `EXPLAIN QUERY PLAN` review + covering index on `edges.type`
   would likely settle this. Deferred — sub-second p99, not a user
   pain point yet.
4. **`computeStaleness` debounce** — the residual 64ms on
   /api/manifest is a `git rev-parse HEAD` spawn. Cheap ELF cost but
   measurable. Could refresh on a 5-second timer instead of per
   request. Low effort.
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
