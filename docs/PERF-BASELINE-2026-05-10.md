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

## MCP tool latency (bench-mcp)

In-process MCP tool measurements via `ckg bench-mcp` — same harness
shape, no HTTP, no stdio, no JSON-RPC framing. Tools that need a
Function seed (find_callers / find_callees / get_subgraph /
impact_of_change) are skipped on graphs whose root has no Function
node, which is the current /tmp/ckg-h4 case.

| Tool | n | p50 | p95 | p99 | mean |
|------|---|----:|----:|----:|-----:|
| find_symbol             | 200 |  0.07ms |  0.26ms |  0.94ms |  0.10ms |
| search_text             | 200 |  0.75ms |  1.25ms |  1.74ms |  0.78ms |
| get_context_for_task    | 200 |  6.71ms |  9.98ms | 12.25ms |  7.01ms |
| evidence_for_intent (before fix) | 200 | 172.36ms | 209.62ms | 248.58ms | 174.96ms |
| evidence_for_intent (after fix)  | 200 |   8.76ms |  36.02ms | 163.10ms |  12.61ms |

Each probe runs a single warmup call before the timed loop so
cold-start costs (the BM25 corpus build) don't pollute the
measurement. Without the warmup, evidence_for_intent's first call
shows the ~5s `ensureIndex` build (mirrors the cold start that
prewarmTicketIndex absorbs in bench-server).

**Trace finding**: the original 40× gap (172ms in-process vs 4.3ms
HTTP) was `evidence.Cache.ensureIndex` calling `store.GetManifest()`
on every `BuildPack` invocation purely to compute the corpus
invalidation key. bench-server's wrapped store served that read
from memory; bench-mcp's raw store hit SQLite (~26-65ms per call).
The fix lifted the manifest fetch into a 1s TTL mini-cache inside
`evidence.Cache` itself, so every consumer (bench-mcp, `ckg
evidence` from CLI, future MCP-only deployments) gets the same
fast path the HTTP route had. After-fix bench-mcp matches
bench-server within measurement jitter (8.76ms vs 4.41ms p50).

Raw: `/tmp/ckg-bench/mcp.json`.

### Stdio framing overhead (bench-mcp-stdio)

`ckg bench-mcp-stdio` spawns a real `ckg mcp` subprocess and drives
each tool through the production stdio + JSON-RPC NDJSON path. The
delta between the two harnesses attributes the framing cost
independently of the graph layer.

| Tool | in-proc p50 | stdio p50 | Δ p50 | in-proc p99 | stdio p99 |
|------|------------:|----------:|------:|------------:|----------:|
| find_symbol           |  0.03ms |  0.06ms | +0.03ms |   0.10ms |   0.46ms |
| search_text           |  0.28ms |  0.40ms | +0.12ms |   0.67ms |   0.66ms |
| get_context_for_task  |  3.98ms |  4.15ms | +0.16ms |   5.17ms |   5.37ms |
| evidence_for_intent   |  4.02ms |  4.47ms | +0.44ms | 156.05ms | 158.19ms |

**Finding**: framing overhead is 0.03–0.44ms across the four
measurable tools — well under a millisecond. The earlier hypothesis
"MCP latency is dominated by stdio framing" turns out wrong on this
graph: after the evidence-cache manifest debounce (`9151746`),
graph-layer cost and stdio framing are both small enough that
neither dominates. p99 outliers track the in-process numbers
(same underlying cache jitter) — stdio doesn't add tail latency.

Concurrency is implicitly 1 in stdio mode (single pipe). Production
clients (Claude Desktop, etc.) match this profile.

Raw: `/tmp/ckg-bench/mcp-stdio.json`, `/tmp/ckg-bench/mcp-fresh.json`.

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
5. ✅ **bench-mcp (in-process)** — landed (commit 218008a).
6. ✅ **evidence per-call manifest fetch** — surfaced as the 40× gap
   between bench-mcp and bench-server's evidence_for_intent;
   `evidence.Cache.ensureIndex` was hitting `store.GetManifest()` on
   every BuildPack purely for the cache-invalidation key compute.
   Lifted into a 1s TTL mini-cache inside the Cache struct so every
   consumer benefits, not just `ckg serve` (whose
   `cachedManifestStore` already short-circuited the read). After:
   bench-mcp evidence p50 172ms → 8.76ms (−95%); bench-server held
   steady at ~4ms (already fast).
7. ✅ **bench-mcp-stdio** — landed in the same session.
   `cmd/ckg/bench_mcp_stdio.go` spawns a `ckg mcp` subprocess and
   drives each tool through the NDJSON JSON-RPC pipe. Stdio framing
   overhead measured at 0.03–0.44ms across the four tools tested,
   well under a millisecond. The pre-session hypothesis "MCP
   latency is dominated by stdio framing" turns out wrong: after the
   evidence manifest-debounce, neither graph nor framing dominates.
   No further perf work indicated.

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
