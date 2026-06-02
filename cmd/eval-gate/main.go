// eval-gate is a CI regression gate that compares the latest `make
// eval` output against the committed baseline. Exits non-zero when
// any tracked metric drifts beyond the per-metric tolerance.
//
// This is the LLM-free half of the eval surface (Stage B is the
// LLM-driven half — too expensive to run on every PR). It runs in
// ~2 minutes end-to-end and gates:
//
//   - eval/baseline/retrieval.json vs eval/results/latest/retrieval.json
//     Aggregate recall, precision, f1 must each stay within the
//     tolerance. Drop below tolerance → regression.
//   - eval/baseline/validate.json  vs eval/results/latest/validate.json
//     Issue count must not increase. The schema validator is meant
//     to converge on zero; new findings should land as code fixes,
//     not as silent baseline drift.
//
// Run:  go run ./cmd/eval-gate  (after `make eval` has written the
// `eval/results/latest/` artefacts)
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
)

// defaultTolerance is the per-metric float64 slack the gate allows
// before flagging a regression. 0.02 (2pp on recall/precision/f1)
// is loose enough to absorb numerical jitter on tied retrieval
// scores while still catching real regressions.
const defaultTolerance = 0.02

type retrievalReport struct {
	Aggregate struct {
		Recall    float64 `json:"recall"`
		Precision float64 `json:"precision"`
		F1        float64 `json:"f1"`
	} `json:"aggregate"`
	FixturesTotal int `json:"fixtures_total"`
	Passed        int `json:"passed"`
	Failed        int `json:"failed"`
}

// validateReport mirrors the array-of-validator shape that
// `ckg validate --format=json` emits (one entry per registered
// validator, each with its own Issues list). We aggregate Issues
// length across the array so the gate has a single scalar to
// regression-check.
type validateReport []validatorRun

type validatorRun struct {
	Validator string `json:"Validator"`
	Issues    []any  `json:"Issues"`
}

func (r validateReport) IssueCount() int {
	n := 0
	for _, v := range r {
		n += len(v.Issues)
	}
	return n
}

func main() {
	baselineDir := flag.String("baseline", "eval/baseline", "directory holding the committed baseline JSON files")
	latestDir := flag.String("latest", "eval/results/latest", "directory holding the latest `make eval` JSON output")
	tolerance := flag.Float64("tolerance", defaultTolerance, "per-metric drift tolerance (default 0.02 = 2 percentage points)")
	flag.Parse()

	var failures []string

	if msgs := checkRetrieval(*baselineDir, *latestDir, *tolerance); len(msgs) > 0 {
		failures = append(failures, msgs...)
	}
	if msgs := checkValidate(*baselineDir, *latestDir); len(msgs) > 0 {
		failures = append(failures, msgs...)
	}

	if len(failures) == 0 {
		fmt.Println("eval-gate: PASS — no regressions detected")
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "eval-gate: FAIL — %d regression(s)\n", len(failures))
	for _, f := range failures {
		_, _ = fmt.Fprintf(os.Stderr, "  - %s\n", f)
	}
	os.Exit(1)
}

func checkRetrieval(baselineDir, latestDir string, tolerance float64) []string {
	var base, latest retrievalReport
	if err := readJSON(baselineDir+"/retrieval.json", &base); err != nil {
		return []string{fmt.Sprintf("read baseline retrieval.json: %v", err)}
	}
	if err := readJSON(latestDir+"/retrieval.json", &latest); err != nil {
		return []string{fmt.Sprintf("read latest retrieval.json: %v", err)}
	}
	var out []string
	out = appendDriftDown(out, "retrieval.aggregate.recall", base.Aggregate.Recall, latest.Aggregate.Recall, tolerance)
	out = appendDriftDown(out, "retrieval.aggregate.precision", base.Aggregate.Precision, latest.Aggregate.Precision, tolerance)
	out = appendDriftDown(out, "retrieval.aggregate.f1", base.Aggregate.F1, latest.Aggregate.F1, tolerance)
	if latest.Failed > base.Failed {
		out = append(out, fmt.Sprintf("retrieval.failed rose: baseline=%d, latest=%d", base.Failed, latest.Failed))
	}
	return out
}

func checkValidate(baselineDir, latestDir string) []string {
	var base, latest validateReport
	if err := readJSON(baselineDir+"/validate.json", &base); err != nil {
		return []string{fmt.Sprintf("read baseline validate.json: %v", err)}
	}
	if err := readJSON(latestDir+"/validate.json", &latest); err != nil {
		return []string{fmt.Sprintf("read latest validate.json: %v", err)}
	}
	bn, ln := base.IssueCount(), latest.IssueCount()
	if ln > bn {
		return []string{fmt.Sprintf(
			"validate.issues rose: baseline=%d, latest=%d (schema validation must not regress)",
			bn, ln)}
	}
	return nil
}

// appendDriftDown reports drift only in the harmful direction (latest
// below baseline by more than tolerance). Improvements above the
// baseline never fail the gate — they should land, and the operator
// updates the baseline via `make eval-baseline-update` when they're
// confident the improvement is real.
func appendDriftDown(out []string, label string, baseline, latest, tolerance float64) []string {
	if latest+tolerance < baseline {
		return append(out, fmt.Sprintf(
			"%s dropped: baseline=%.4f, latest=%.4f (drift %.4f exceeds tolerance %.2f)",
			label, baseline, latest, baseline-latest, tolerance))
	}
	if math.IsNaN(latest) {
		return append(out, fmt.Sprintf("%s is NaN in latest output", label))
	}
	return out
}

func readJSON(path string, dst any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}
