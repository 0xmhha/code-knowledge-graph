package eval

import (
	"fmt"
	"math"
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
// symbols for triage.
//
// Axis 1 (2026-05-22): the summary table now also reports
// population standard deviation alongside each mean, so multi-shot
// runs (--n-runs > 1) surface the LLM-side non-determinism the
// third smoke run made visible (3 single-shots produced 0/0/4
// hallucinations). Single-shot runs (n=1) report std=0 — the
// columns stay structurally consistent across shot counts.
func WriteReport(path string, results []Result) error {
	type bAgg struct {
		scores, halluRates, tokens, halluTotals []float64
	}
	byBaseline := map[Baseline]*bAgg{}
	for _, r := range results {
		a := byBaseline[r.Baseline]
		if a == nil {
			a = &bAgg{}
			byBaseline[r.Baseline] = a
		}
		a.scores = append(a.scores, r.Score)
		a.halluRates = append(a.halluRates, r.Hallucination.Rate)
		a.tokens = append(a.tokens, float64(r.InputTokens))
		a.halluTotals = append(a.halluTotals, float64(r.Hallucination.Total))
	}
	type row struct {
		B                           Baseline
		Runs                        int
		MeanTokens, StdTokens       float64
		MeanScore, StdScore         float64
		MeanHalluRate, StdHalluRate float64
		MeanHalluTotal              float64
	}
	var rows []row
	for b, a := range byBaseline {
		mt, st := meanStd(a.tokens)
		ms, ss := meanStd(a.scores)
		mr, sr := meanStd(a.halluRates)
		mh, _ := meanStd(a.halluTotals)
		rows = append(rows, row{B: b, Runs: len(a.scores),
			MeanTokens: mt, StdTokens: st,
			MeanScore: ms, StdScore: ss,
			MeanHalluRate: mr, StdHalluRate: sr,
			MeanHalluTotal: mh})
	}
	sort.Slice(rows, func(i, j int) bool { return baselineOrder(rows[i].B) < baselineOrder(rows[j].B) })

	var sb strings.Builder
	sb.WriteString("# CKG eval report\n\n")
	sb.WriteString("| Baseline | N | Avg input tokens | Score (mean ± std) | Hallucination rate (mean ± std) | Avg mentions |\n|---|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&sb, "| %s | %d | %.0f | %.3f ± %.3f | %.3f ± %.3f | %.1f |\n",
			r.B, r.Runs, r.MeanTokens,
			r.MeanScore, r.StdScore,
			r.MeanHalluRate, r.StdHalluRate,
			r.MeanHalluTotal)
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
	if a, ok := byBaseline[BaselineAlpha]; ok && len(a.tokens) > 0 {
		if d, ok := byBaseline[BaselineDelta]; ok && len(d.tokens) > 0 {
			meanAlphaTokens, _ := meanStd(a.tokens)
			meanDeltaTokens, _ := meanStd(d.tokens)
			meanAlphaScore, _ := meanStd(a.scores)
			meanDeltaScore, _ := meanStd(d.scores)
			if meanAlphaTokens > 0 {
				savings := 1 - meanDeltaTokens/meanAlphaTokens
				fmt.Fprintf(&sb, "- **H1** δ vs α token savings: **%.1f%%** (target ≥ 50%%)\n", savings*100)
				scoreDelta := meanDeltaScore - meanAlphaScore
				fmt.Fprintf(&sb, "- **H2** δ score - α score: **%+.3f** (target ≥ 0)\n", scoreDelta)
			}
		}
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// meanStd returns the arithmetic mean and population standard
// deviation of xs. Empty slices yield (0, 0); single-element slices
// yield (xs[0], 0) because population std is well-defined at N=1.
// "Population" rather than "sample" std is correct here: we have
// every measured run, not a sample drawn from a larger population.
func meanStd(xs []float64) (mean, std float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean = sum / float64(len(xs))
	if len(xs) == 1 {
		return mean, 0
	}
	var sqSum float64
	for _, x := range xs {
		d := x - mean
		sqSum += d * d
	}
	std = math.Sqrt(sqSum / float64(len(xs)))
	return
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
