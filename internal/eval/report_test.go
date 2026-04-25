package eval_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/eval"
)

func readReport(t *testing.T, dir string) string {
	t.Helper()
	buf, err := os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	return string(buf)
}

func TestWriteReport(t *testing.T) {
	t.Run("empty results", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "report.md")
		if err := eval.WriteReport(path, nil); err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		content := readReport(t, dir)

		if !strings.Contains(content, "# CKG eval report") {
			t.Error("missing '# CKG eval report' header")
		}
		if !strings.Contains(content, "| Baseline |") {
			t.Error("missing table header")
		}
		if !strings.Contains(content, "## Hypothesis check") {
			t.Error("missing '## Hypothesis check' section")
		}
		if strings.Contains(content, "**H1**") {
			t.Error("H1 should not appear with empty results")
		}
		if strings.Contains(content, "**H2**") {
			t.Error("H2 should not appear with empty results")
		}
	})

	t.Run("alpha only no H1 H2", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "report.md")
		results := []eval.Result{
			{TaskID: "T01", Baseline: eval.BaselineAlpha, InputTokens: 100, Score: 0.5},
		}
		if err := eval.WriteReport(path, results); err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		content := readReport(t, dir)

		if !strings.Contains(content, "# CKG eval report") {
			t.Error("missing header")
		}
		if !strings.Contains(content, "| alpha |") {
			t.Error("missing alpha row")
		}
		if !strings.Contains(content, "## Hypothesis check") {
			t.Error("missing hypothesis section")
		}
		// No delta → H1/H2 must not appear
		if strings.Contains(content, "**H1**") {
			t.Error("H1 should not appear with only alpha results")
		}
		if strings.Contains(content, "**H2**") {
			t.Error("H2 should not appear with only alpha results")
		}
	})

	t.Run("alpha and delta valid H1 H2", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "report.md")
		// alpha: avg 1000 tokens, avg score 0.5; delta: avg 400 tokens, avg score 0.6
		results := []eval.Result{
			{TaskID: "T01", Baseline: eval.BaselineAlpha, InputTokens: 1000, Score: 0.5},
			{TaskID: "T02", Baseline: eval.BaselineAlpha, InputTokens: 1000, Score: 0.5},
			{TaskID: "T01", Baseline: eval.BaselineDelta, InputTokens: 400, Score: 0.6},
			{TaskID: "T02", Baseline: eval.BaselineDelta, InputTokens: 400, Score: 0.6},
		}
		if err := eval.WriteReport(path, results); err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		content := readReport(t, dir)

		if !strings.Contains(content, "**H1**") {
			t.Error("H1 should appear when both alpha and delta have results")
		}
		if !strings.Contains(content, "**H2**") {
			t.Error("H2 should appear when both alpha and delta have results")
		}
		// savings = 1 - 400/1000 = 0.6 → 60.0%
		if !strings.Contains(content, "60.0%") {
			t.Errorf("H1 token savings should show 60.0%%, got:\n%s", content)
		}
		// scoreDelta = 0.6 - 0.5 = +0.100
		if !strings.Contains(content, "+0.100") {
			t.Errorf("H2 score delta should show +0.100, got:\n%s", content)
		}
		// alpha row should appear before delta row
		alphaIdx := strings.Index(content, "| alpha |")
		deltaIdx := strings.Index(content, "| delta |")
		if alphaIdx < 0 || deltaIdx < 0 {
			t.Errorf("missing alpha or delta row; alphaIdx=%d deltaIdx=%d", alphaIdx, deltaIdx)
		} else if alphaIdx > deltaIdx {
			t.Errorf("alpha row should appear before delta row in output")
		}
	})

	t.Run("alpha zero tokens no H1 H2", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "report.md")
		// alpha has InputTokens=0 → a.Tokens=0 → H1/H2 skipped per WriteReport condition
		results := []eval.Result{
			{TaskID: "T01", Baseline: eval.BaselineAlpha, InputTokens: 0, Score: 0.5},
			{TaskID: "T01", Baseline: eval.BaselineDelta, InputTokens: 400, Score: 0.6},
		}
		if err := eval.WriteReport(path, results); err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		content := readReport(t, dir)

		if !strings.Contains(content, "## Hypothesis check") {
			t.Error("missing hypothesis section")
		}
		if strings.Contains(content, "**H1**") {
			t.Error("H1 should not appear when alpha has zero tokens")
		}
		if strings.Contains(content, "**H2**") {
			t.Error("H2 should not appear when alpha has zero tokens")
		}
	})

	t.Run("all 4 baselines ordering", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "report.md")
		results := []eval.Result{
			{TaskID: "T01", Baseline: eval.BaselineDelta, InputTokens: 400, Score: 0.6},
			{TaskID: "T01", Baseline: eval.BaselineGamma, InputTokens: 600, Score: 0.55},
			{TaskID: "T01", Baseline: eval.BaselineBeta, InputTokens: 800, Score: 0.52},
			{TaskID: "T01", Baseline: eval.BaselineAlpha, InputTokens: 1000, Score: 0.5},
		}
		if err := eval.WriteReport(path, results); err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		content := readReport(t, dir)

		alphaIdx := strings.Index(content, "| alpha |")
		betaIdx := strings.Index(content, "| beta |")
		gammaIdx := strings.Index(content, "| gamma |")
		deltaIdx := strings.Index(content, "| delta |")
		if alphaIdx < 0 || betaIdx < 0 || gammaIdx < 0 || deltaIdx < 0 {
			t.Fatalf("one or more baseline rows missing: alpha=%d beta=%d gamma=%d delta=%d",
				alphaIdx, betaIdx, gammaIdx, deltaIdx)
		}
		if !(alphaIdx < betaIdx && betaIdx < gammaIdx && gammaIdx < deltaIdx) {
			t.Errorf("rows not in α→β→γ→δ order: alpha=%d beta=%d gamma=%d delta=%d",
				alphaIdx, betaIdx, gammaIdx, deltaIdx)
		}
	})

	t.Run("unknown baseline sorts after delta", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "report.md")
		// unknown baseline exercises baselineOrder's return-4 path
		results := []eval.Result{
			{TaskID: "T01", Baseline: eval.Baseline("omega"), InputTokens: 100, Score: 0.4},
			{TaskID: "T01", Baseline: eval.BaselineAlpha, InputTokens: 200, Score: 0.5},
		}
		if err := eval.WriteReport(path, results); err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		content := readReport(t, dir)
		alphaIdx := strings.Index(content, "| alpha |")
		omegaIdx := strings.Index(content, "| omega |")
		if alphaIdx < 0 || omegaIdx < 0 {
			t.Fatalf("missing rows: alpha=%d omega=%d", alphaIdx, omegaIdx)
		}
		if alphaIdx > omegaIdx {
			t.Errorf("alpha should appear before unknown baseline omega: alpha=%d omega=%d",
				alphaIdx, omegaIdx)
		}
	})
}
