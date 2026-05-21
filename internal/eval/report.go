package eval

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// WriteReport summarizes results.csv into a Markdown report (spec §9.5).
//
// T-04 V1 (2026-05-21): every baseline row now carries hallucination
// statistics in addition to score. The summary table gains two
// columns (avg hallucination rate, total mentions), and a per-task
// detail section lists the literal hallucinated / qname-diverged
// symbols for triage. The detail section is the one consumers read
// when the rate is non-zero ("which symbol is the LLM making up?"),
// so it is intentionally placed *before* the H1/H2 hypothesis check.
func WriteReport(path string, results []Result) error {
	avg := map[Baseline]struct {
		Tokens, Score, HalluRate, HalluTotal, N float64
	}{}
	for _, r := range results {
		a := avg[r.Baseline]
		a.Tokens += float64(r.InputTokens)
		a.Score += r.Score
		a.HalluRate += r.Hallucination.Rate
		a.HalluTotal += float64(r.Hallucination.Total)
		a.N++
		avg[r.Baseline] = a
	}
	type row struct {
		B             Baseline
		AvgTokens     float64
		AvgScore      float64
		AvgHalluRate  float64
		AvgHalluTotal float64
	}
	var rows []row
	for b, a := range avg {
		rows = append(rows, row{B: b,
			AvgTokens: a.Tokens / a.N, AvgScore: a.Score / a.N,
			AvgHalluRate: a.HalluRate / a.N, AvgHalluTotal: a.HalluTotal / a.N})
	}
	sort.Slice(rows, func(i, j int) bool { return baselineOrder(rows[i].B) < baselineOrder(rows[j].B) })

	var sb strings.Builder
	sb.WriteString("# CKG eval report\n\n")
	sb.WriteString("| Baseline | Avg input tokens | Avg score | Avg hallucination rate | Avg mentions |\n|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&sb, "| %s | %.0f | %.3f | %.3f | %.1f |\n",
			r.B, r.AvgTokens, r.AvgScore, r.AvgHalluRate, r.AvgHalluTotal)
	}

	// Per-row hallucination detail (T-04 V1). Only print rows that
	// actually have hallucinated or qname-diverged symbols — a clean
	// run produces a single "no issues" line so the report stays
	// short.
	sb.WriteString("\n## Hallucination detail (T-04)\n\n")
	any := false
	for _, r := range results {
		if len(r.Hallucination.Hallucinated) == 0 && len(r.Hallucination.QnameDiverged) == 0 {
			continue
		}
		any = true
		fmt.Fprintf(&sb, "### %s / %s\n", r.TaskID, r.Baseline)
		fmt.Fprintf(&sb, "- mentions=%d, rate=%.3f\n", r.Hallucination.Total, r.Hallucination.Rate)
		if len(r.Hallucination.Hallucinated) > 0 {
			fmt.Fprintf(&sb, "- hallucinated: `%s`\n", strings.Join(r.Hallucination.Hallucinated, "`, `"))
		}
		if len(r.Hallucination.QnameDiverged) > 0 {
			fmt.Fprintf(&sb, "- qname-diverged: `%s`\n", strings.Join(r.Hallucination.QnameDiverged, "`, `"))
		}
		sb.WriteString("\n")
	}
	if !any {
		sb.WriteString("No hallucinated or qname-diverged symbols across all rows.\n")
	}

	sb.WriteString("\n## Hypothesis check\n\n")
	if a, ok := avg[BaselineAlpha]; ok && a.N > 0 && a.Tokens > 0 {
		if d, ok := avg[BaselineDelta]; ok && d.N > 0 {
			savings := 1 - (d.Tokens/d.N)/(a.Tokens/a.N)
			fmt.Fprintf(&sb, "- **H1** δ vs α token savings: **%.1f%%** (target ≥ 50%%)\n", savings*100)
			scoreDelta := d.Score/d.N - a.Score/a.N
			fmt.Fprintf(&sb, "- **H2** δ score - α score: **%+.3f** (target ≥ 0)\n", scoreDelta)
		}
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// baselineOrder gives the canonical α/β/γ/δ index for stable report ordering.
// Unknown baselines sort last.
func baselineOrder(b Baseline) int {
	switch b {
	case BaselineAlpha:
		return 0
	case BaselineBeta:
		return 1
	case BaselineGamma:
		return 2
	case BaselineDelta:
		return 3
	}
	return 4
}
