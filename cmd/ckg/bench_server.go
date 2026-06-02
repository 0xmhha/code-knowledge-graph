// cmd/ckg/bench_server.go — `ckg bench-server` measures p50/p95/p99
// latency of every /api/* endpoint against a real graph.db. Serves a
// performance baseline that future PRs can re-run to detect
// regressions; the JSON output is machine-readable so a comparison
// table can be diffed in CI.
//
// The server runs in-process via httptest (no port allocation, no
// cleanup races). Each endpoint is hit `iterations` times sequentially
// per worker, with `concurrency` workers in parallel — the same load
// profile the dogfood plan calls "warm cache, steady-state read".
//
// MCP tool latency is intentionally out of scope: the stdio transport
// would require subprocess spawning and JSON-RPC framing, which
// dwarfs the cost of the HTTP measurement we actually care about for
// regression-tracking. A future bench-mcp subcommand can layer on
// when that signal becomes valuable.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/internal/server"
)

// benchProbe is one endpoint to measure. Path may include query
// params; the bench loop appends nothing else. seedQname picks a
// real node from the graph for /api/impact (which 404s without one)
// — left blank for endpoints that don't need a seed.
type benchProbe struct {
	Name string
	Path string
}

// defaultProbes covers every /api/* surface that returns useful
// payload on a typical graph. The seed for /api/impact is filled in
// at runtime from the graph's first Function node so the bench
// doesn't need a fixture-specific qname.
func defaultProbes(seedQname string) []benchProbe {
	probes := []benchProbe{
		{Name: "manifest", Path: "/api/manifest"},
		{Name: "hierarchy.pkg", Path: "/api/hierarchy?kind=pkg"},
		{Name: "nodes", Path: "/api/nodes?limit=200"},
		{Name: "nodes.top.pagerank", Path: "/api/nodes/top?metric=pagerank&limit=200"},
		{Name: "nodes.top.usage", Path: "/api/nodes/top?metric=usage&limit=200"},
		{Name: "nodes.ambiguous", Path: "/api/nodes/ambiguous"},
		{Name: "edges.counts", Path: "/api/edges/counts"},
		{Name: "search", Path: "/api/search?q=consensus"},
		{Name: "tickets", Path: "/api/tickets?limit=100"},
		{Name: "evidence.intent", Path: "/api/evidence?intent=consensus+metrics&k=5&budget_tokens=4000"},
		{Name: "evidence.issue", Path: "/api/evidence?issue_id=GH-66&k=5&budget_tokens=4000"},
		{Name: "evidence.and", Path: "/api/evidence?intent=consensus+metrics&mode=and&k=5&budget_tokens=4000"},
	}
	if seedQname != "" {
		probes = append(probes, benchProbe{
			Name: "impact",
			// Pre-encoded; cobra is a CLI flag parser so we can't rely
			// on net/url here without dragging in extra deps. The
			// seedQname is constrained to ASCII identifiers in
			// practice.
			Path: "/api/impact?seed_qname=" + seedQname + "&depth=2",
		})
	}
	return probes
}

// benchResult is one row in the output JSON / text table.
type benchResult struct {
	Endpoint     string  `json:"endpoint"`
	N            int     `json:"n"`
	Concurrency  int     `json:"concurrency"`
	P50Ms        float64 `json:"p50_ms"`
	P95Ms        float64 `json:"p95_ms"`
	P99Ms        float64 `json:"p99_ms"`
	MeanMs       float64 `json:"mean_ms"`
	MinMs        float64 `json:"min_ms"`
	MaxMs        float64 `json:"max_ms"`
	ErrorCount   int     `json:"error_count"`
	Status200Pct float64 `json:"status_200_pct"`
}

// benchOutput is the full bench-server result envelope. Captures the
// graph fingerprint so a future re-run can confirm it's measuring the
// same corpus before comparing numbers.
type benchOutput struct {
	GeneratedAt    string        `json:"generated_at"`
	GraphPath      string        `json:"graph_path"`
	BuildTimestamp string        `json:"build_timestamp"`
	SrcCommit      string        `json:"src_commit"`
	Iterations     int           `json:"iterations"`
	Concurrency    int           `json:"concurrency"`
	Results        []benchResult `json:"results"`
}

func newBenchServerCmd() *cobra.Command {
	var (
		graph       string
		iterations  int
		concurrency int
		output      string
	)
	cmd := &cobra.Command{
		Use:   "bench-server",
		Short: "Measure /api/* p50/p95/p99 latency against a graph.db",
		Long: `Spin up an in-process server bound to the given graph and
hit every /api/* endpoint with a parallel load profile, reporting
p50/p95/p99/mean/min/max latencies + error counts as JSON.

Designed as a regression baseline: re-run after any change touching
internal/server or pkg/* read paths and diff the JSON to spot
unintended slowdowns. The server runs via httptest so there's no
port allocation or cleanup race; safe to invoke from CI.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if iterations <= 0 || iterations > 10000 {
				return fmt.Errorf("--iterations must be in [1, 10000], got %d", iterations)
			}
			if concurrency <= 0 || concurrency > 64 {
				return fmt.Errorf("--concurrency must be in [1, 64], got %d", concurrency)
			}
			db := filepath.Join(graph, "graph.db")
			store, err := persist.OpenReadOnly(db)
			if err != nil {
				return fmt.Errorf("open graph: %w", err)
			}
			defer func() { _ = store.Close() }()

			seed, err := pickFunctionSeed(store)
			if err != nil {
				// Non-fatal — just skip the impact probe.
				_, _ = fmt.Fprintf(os.Stderr, "ckg bench-server: no Function node for impact seed; skipping that probe\n")
			}
			probes := defaultProbes(seed)

			srv := server.New(store, nil)
			ts := httptest.NewServer(srv)
			defer func() { ts.Close() }()

			_, _ = fmt.Fprintf(os.Stderr, "bench-server: graph=%s iters=%d concurrency=%d probes=%d\n",
				graph, iterations, concurrency, len(probes))

			results := make([]benchResult, 0, len(probes))
			for _, p := range probes {
				r := runBench(ts.URL, p, iterations, concurrency)
				results = append(results, r)
				_, _ = fmt.Fprintf(os.Stderr, "  %-26s p50=%6.2f p95=%6.2f p99=%6.2f mean=%6.2f errors=%d\n",
					r.Endpoint, r.P50Ms, r.P95Ms, r.P99Ms, r.MeanMs, r.ErrorCount)
			}

			man, _ := store.GetManifest()
			out := benchOutput{
				GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
				GraphPath:      graph,
				BuildTimestamp: man.BuildTimestamp,
				SrcCommit:      man.SrcCommit,
				Iterations:     iterations,
				Concurrency:    concurrency,
				Results:        results,
			}
			payload, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal output: %w", err)
			}
			if output == "-" || output == "" {
				_, _ = os.Stdout.Write(payload)
				_, _ = os.Stdout.Write([]byte{'\n'})
				return nil
			}
			if err := os.WriteFile(output, append(payload, '\n'), 0o644); err != nil {
				return fmt.Errorf("write output: %w", err)
			}
			_, _ = fmt.Fprintf(os.Stderr, "bench-server: wrote %s\n", output)
			return nil
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory containing graph.db (required)")
	cmd.Flags().IntVar(&iterations, "iterations", 100,
		"requests per probe per worker (total = iterations * concurrency)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4,
		"parallel workers driving each probe (≤ 64)")
	cmd.Flags().StringVar(&output, "output", "-",
		"path for JSON output ('-' or '' for stdout)")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}

// pickFunctionSeed returns the qualified_name of a Function node from the
// graph. The bench harness uses it as the /api/impact seed so we don't need
// a fixture-specific qname per graph.
//
// QueryNodes("") was the original implementation but it returns top-level
// Packages only (see internal/persist/sqlite.go QueryNodes contract), which
// means the loop below would never find a Function. We switched to
// TopNodes(pagerank) — the ranking is type-agnostic so Functions surface
// naturally, and the top of the ranking is where the most interesting
// bench seeds live anyway (hub functions exercise more of the graph).
func pickFunctionSeed(store persist.StoreReader) (string, error) {
	nodes, err := store.TopNodes("pagerank", 500)
	if err != nil {
		return "", err
	}
	for _, n := range nodes {
		if string(n.Type) == "Function" && n.QualifiedName != "" {
			return n.QualifiedName, nil
		}
	}
	return "", fmt.Errorf("no Function node found")
}

// runBench hits one probe `iterations * concurrency` total times,
// across `concurrency` workers running iterations sequentially each.
// Latencies merge into one slice; status counts are aggregated. The
// probe returns one benchResult.
func runBench(baseURL string, probe benchProbe, iterations, concurrency int) benchResult {
	type sample struct {
		dur    time.Duration
		status int
		err    error
	}
	samples := make(chan sample, iterations*concurrency)
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for w := 0; w < concurrency; w++ {
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 30 * time.Second}
			for i := 0; i < iterations; i++ {
				start := time.Now()
				resp, err := client.Get(baseURL + probe.Path)
				dur := time.Since(start)
				if err != nil {
					samples <- sample{dur: dur, err: err}
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				samples <- sample{dur: dur, status: resp.StatusCode}
			}
		}()
	}
	wg.Wait()
	close(samples)

	durs := make([]float64, 0, iterations*concurrency)
	errCount := 0
	ok200 := 0
	for s := range samples {
		durs = append(durs, float64(s.dur.Microseconds())/1000.0)
		if s.err != nil {
			errCount++
			continue
		}
		if s.status == 200 {
			ok200++
		}
	}
	r := benchResult{
		Endpoint:    probe.Name,
		N:           len(durs),
		Concurrency: concurrency,
		ErrorCount:  errCount,
	}
	if len(durs) == 0 {
		return r
	}
	r.Status200Pct = 100.0 * float64(ok200) / float64(len(durs))
	r.MinMs = minF(durs)
	r.MaxMs = maxF(durs)
	r.MeanMs = meanF(durs)
	r.P50Ms = percentile(durs, 50)
	r.P95Ms = percentile(durs, 95)
	r.P99Ms = percentile(durs, 99)
	return r
}

// percentile computes the p-th percentile (0..100) using nearest-rank.
// Mutates the input slice (sorts in place); callers don't need the
// original ordering.
func percentile(values []float64, p int) float64 {
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	rank := (p * len(values)) / 100
	if rank >= len(values) {
		rank = len(values) - 1
	}
	if rank < 0 {
		rank = 0
	}
	return values[rank]
}

func minF(xs []float64) float64 {
	m := xs[0]
	for _, x := range xs {
		if x < m {
			m = x
		}
	}
	return m
}

func maxF(xs []float64) float64 {
	m := xs[0]
	for _, x := range xs {
		if x > m {
			m = x
		}
	}
	return m
}

func meanF(xs []float64) float64 {
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}
