package eval

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// WriteReport summarizes results.csv into a Markdown report (spec §9.5).
func WriteReport(path string, results []Result) error {
	avg := map[Baseline]struct {
		Tokens, Score, N float64
	}{}
	for _, r := range results {
		a := avg[r.Baseline]
		a.Tokens += float64(r.InputTokens)
		a.Score += r.Score
		a.N++
		avg[r.Baseline] = a
	}
	type row struct {
		B         Baseline
		AvgTokens float64
		AvgScore  float64
	}
	var rows []row
	for b, a := range avg {
		rows = append(rows, row{B: b,
			AvgTokens: a.Tokens / a.N, AvgScore: a.Score / a.N})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].B < rows[j].B })

	var sb strings.Builder
	sb.WriteString("# CKG eval report\n\n")
	sb.WriteString("| Baseline | Avg input tokens | Avg score |\n|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&sb, "| %s | %.0f | %.3f |\n", r.B, r.AvgTokens, r.AvgScore)
	}
	sb.WriteString("\n## Hypothesis check\n\n")
	if a, ok := avg[BaselineAlpha]; ok {
		if d, ok := avg[BaselineDelta]; ok {
			savings := 1 - (d.Tokens/d.N)/(a.Tokens/a.N)
			fmt.Fprintf(&sb, "- **H1** δ vs α token savings: **%.1f%%** (target ≥ 50%%)\n", savings*100)
			scoreDelta := d.Score/d.N - a.Score/a.N
			fmt.Fprintf(&sb, "- **H2** δ score - α score: **%+.3f** (target ≥ 0)\n", scoreDelta)
		}
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}
