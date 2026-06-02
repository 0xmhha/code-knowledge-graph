// cmd/ckg/eval_retrieval.go — `ckg eval-retrieval` runs the LLM-free
// retrieval probes from eval/retrieval/*.yaml against a built graph
// and emits per-fixture recall/precision/F1 plus an aggregate gate
// result. EV1 Phase 2.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/eval/retrieval"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

func newEvalRetrievalCmd() *cobra.Command {
	var (
		graph    string
		fixtures string
		output   string
	)
	cmd := &cobra.Command{
		Use:   "eval-retrieval",
		Short: "Run LLM-free retrieval-accuracy probes against a graph (EV1 Phase 2)",
		Long: `Load every YAML fixture under --fixtures, run each probe
against the graph at --graph, and score the result symbols against the
fixture's expected set. Per-fixture recall/precision/F1 plus an
aggregate gate (any fixture below its threshold → non-zero exit).

The probe-tool layer mirrors the MCP tools (find_callers, find_callees,
find_symbol, search_text) so fixture YAML stays close to how a real
consumer would call them.

Output JSON shape:
  {
    "graph": "...",
    "fixtures_total": N,
    "passed": M,
    "failed": K,
    "aggregate": { "recall": ..., "precision": ..., "f1": ... },
    "results": [ { "id": ..., "score": {...}, "got": [...], "pass": bool }, ... ]
  }

Exit codes:
  0  every fixture met its threshold
  1  one or more fixtures failed
  2  internal error (graph open, fixture parse, etc.)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			db := filepath.Join(graph, "graph.db")
			store, err := persist.OpenReadOnly(db)
			if err != nil {
				return fmt.Errorf("open graph: %w", err)
			}
			defer func() { _ = store.Close() }()

			fs, err := retrieval.LoadFixtures(fixtures)
			if err != nil {
				return fmt.Errorf("load fixtures: %w", err)
			}
			if len(fs) == 0 {
				return fmt.Errorf("no *.yaml fixtures found under %s", fixtures)
			}

			results, err := retrieval.RunAll(store, fs)
			if err != nil {
				return fmt.Errorf("run fixtures: %w", err)
			}

			report := buildRetrievalReport(graph, results)
			if err := writeReport(output, report); err != nil {
				return err
			}
			// Print a one-line stderr summary so humans see the gate
			// outcome without scrolling through JSON. The numeric
			// detail lives in the report file (or stdout when
			// --output=-).
			_, _ = fmt.Fprintf(os.Stderr,
				"eval-retrieval: %d/%d passed (aggregate R=%.2f P=%.2f F1=%.2f)\n",
				report.Passed, report.FixturesTotal,
				report.Aggregate.Recall, report.Aggregate.Precision, report.Aggregate.F1)
			if report.Failed > 0 {
				// Non-zero exit, but not via Cobra's error wrapping —
				// the error message would shadow the JSON output. Use
				// os.Exit so the report file already wrote.
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory containing graph.db (required)")
	cmd.Flags().StringVar(&fixtures, "fixtures", "eval/retrieval",
		"directory containing retrieval *.yaml fixtures")
	cmd.Flags().StringVar(&output, "output", "-",
		"path for JSON output ('-' for stdout)")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}

// retrievalReport is the JSON shape consumed by make eval +
// eval/baseline/retrieval.json. Stable field names — renaming any of
// these breaks the regression diff.
type retrievalReport struct {
	Graph         string          `json:"graph"`
	FixturesTotal int             `json:"fixtures_total"`
	Passed        int             `json:"passed"`
	Failed        int             `json:"failed"`
	Aggregate     retrieval.Score `json:"aggregate"`
	Results       []retrievalRow  `json:"results"`
}

type retrievalRow struct {
	ID            string          `json:"id"`
	Tool          string          `json:"tool"`
	Pass          bool            `json:"pass"`
	PassRecall    bool            `json:"pass_recall"`
	PassPrecision bool            `json:"pass_precision"`
	Score         retrieval.Score `json:"score"`
	Got           []string        `json:"got"`
}

// buildRetrievalReport collapses per-fixture results into the JSON shape.
// Aggregate recall/precision/F1 are micro-averaged over the union of
// all expected/got sets — single number that summarises the run, no
// per-fixture weighting choice required.
func buildRetrievalReport(graph string, results []retrieval.Result) retrievalReport {
	r := retrievalReport{
		Graph:         graph,
		FixturesTotal: len(results),
		Results:       make([]retrievalRow, 0, len(results)),
	}
	var allExp, allGot []string
	for _, res := range results {
		if res.Pass() {
			r.Passed++
		} else {
			r.Failed++
		}
		r.Results = append(r.Results, retrievalRow{
			ID:            res.Fixture.ID,
			Tool:          res.Fixture.Probe.Tool,
			Pass:          res.Pass(),
			PassRecall:    res.PassRecall,
			PassPrecision: res.PassPrecision,
			Score:         res.Score,
			Got:           res.Got,
		})
		allExp = append(allExp, res.Fixture.Expected.Symbols...)
		allGot = append(allGot, res.Got...)
	}
	r.Aggregate = retrieval.ComputeScore(allExp, allGot)
	// Aggregate diagnostic lists would be huge and uninteresting (a
	// per-fixture detail already shows them) — drop to keep the
	// top-level report tight.
	r.Aggregate.Missing = nil
	r.Aggregate.Extra = nil
	return r
}

func writeReport(path string, report retrievalReport) error {
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	if path == "-" || path == "" {
		_, _ = os.Stdout.Write(payload)
		_, _ = os.Stdout.Write([]byte{'\n'})
		return nil
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}
