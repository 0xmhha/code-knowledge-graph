package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile is the local helper for dropping JSON fixtures under
// t.TempDir(). Returns the directory the fixture was written into.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

const retrievalBaselineFixture = `{
  "aggregate": {"recall": 1.0, "precision": 0.95, "f1": 0.975},
  "fixtures_total": 10, "passed": 10, "failed": 0
}`

const validateBaselineFixture = `[{"Validator": "schema", "Issues": null}]`

// TestCheckRetrieval_NoRegression confirms the happy path: latest
// metrics within tolerance of baseline, no failures returned.
func TestCheckRetrieval_NoRegression(t *testing.T) {
	base := t.TempDir()
	latest := t.TempDir()
	writeFile(t, base, "retrieval.json", retrievalBaselineFixture)
	writeFile(t, latest, "retrieval.json", retrievalBaselineFixture)

	failures := checkRetrieval(base, latest, defaultTolerance)
	if len(failures) != 0 {
		t.Errorf("identical baseline vs latest should pass; got %v", failures)
	}
}

// TestCheckRetrieval_ImprovementAllowed confirms the gate is one-
// sided — a *better* latest must not trip the regression check.
// Operators promote improvements via `make eval-baseline-update`.
func TestCheckRetrieval_ImprovementAllowed(t *testing.T) {
	base := t.TempDir()
	latest := t.TempDir()
	writeFile(t, base, "retrieval.json", retrievalBaselineFixture)
	writeFile(t, latest, "retrieval.json", `{
		"aggregate": {"recall": 1.0, "precision": 1.0, "f1": 1.0},
		"fixtures_total": 10, "passed": 10, "failed": 0
	}`)

	failures := checkRetrieval(base, latest, defaultTolerance)
	if len(failures) != 0 {
		t.Errorf("improvement above baseline must not regress; got %v", failures)
	}
}

// TestCheckRetrieval_RegressionDetected confirms a precision drop
// beyond tolerance surfaces as a failure with the labelled metric.
func TestCheckRetrieval_RegressionDetected(t *testing.T) {
	base := t.TempDir()
	latest := t.TempDir()
	writeFile(t, base, "retrieval.json", retrievalBaselineFixture)
	writeFile(t, latest, "retrieval.json", `{
		"aggregate": {"recall": 1.0, "precision": 0.60, "f1": 0.75},
		"fixtures_total": 10, "passed": 7, "failed": 3
	}`)

	failures := checkRetrieval(base, latest, defaultTolerance)
	if len(failures) == 0 {
		t.Fatal("precision drop of 0.35 must be flagged")
	}
	// Expect both precision and f1 to be flagged; failed-count rise too.
	if len(failures) < 3 {
		t.Errorf("want 3 failures (precision, f1, failed), got %d: %v", len(failures), failures)
	}
}

// TestCheckRetrieval_ToleranceAbsorbsJitter confirms small drops
// inside the tolerance window don't trip the gate. Tied retrieval
// scores can yield tiny float wobbles between runs; the tolerance
// exists exactly to absorb that noise.
func TestCheckRetrieval_ToleranceAbsorbsJitter(t *testing.T) {
	base := t.TempDir()
	latest := t.TempDir()
	writeFile(t, base, "retrieval.json", retrievalBaselineFixture)
	writeFile(t, latest, "retrieval.json", `{
		"aggregate": {"recall": 1.0, "precision": 0.94, "f1": 0.965},
		"fixtures_total": 10, "passed": 10, "failed": 0
	}`)

	failures := checkRetrieval(base, latest, defaultTolerance)
	if len(failures) != 0 {
		t.Errorf("0.01 drop should fall inside 0.02 tolerance; got %v", failures)
	}
}

// TestCheckValidate_NoRegression covers the happy validate path.
func TestCheckValidate_NoRegression(t *testing.T) {
	base := t.TempDir()
	latest := t.TempDir()
	writeFile(t, base, "validate.json", validateBaselineFixture)
	writeFile(t, latest, "validate.json", validateBaselineFixture)

	failures := checkValidate(base, latest)
	if len(failures) != 0 {
		t.Errorf("identical validate snapshots should pass; got %v", failures)
	}
}

// TestCheckValidate_IssueRiseDetected: schema validation MUST
// converge on zero; rising issue counts get flagged so they land
// as fixes, not silent baseline drift.
func TestCheckValidate_IssueRiseDetected(t *testing.T) {
	base := t.TempDir()
	latest := t.TempDir()
	writeFile(t, base, "validate.json", validateBaselineFixture) // 0 issues
	writeFile(t, latest, "validate.json", `[
		{"Validator": "schema", "Issues": [{"id": "1"}, {"id": "2"}]}
	]`)

	failures := checkValidate(base, latest)
	if len(failures) != 1 {
		t.Fatalf("want 1 failure for issue-rise; got %d: %v", len(failures), failures)
	}
}

// TestValidateReport_IssueCount aggregates across multiple
// validators — the validate.json envelope is [Validator, …] and the
// gate needs the total to be regression-checkable.
func TestValidateReport_IssueCount(t *testing.T) {
	r := validateReport{
		{Validator: "schema", Issues: []any{1, 2, 3}},
		{Validator: "llm", Issues: []any{4, 5}},
		{Validator: "freshness", Issues: nil},
	}
	if got := r.IssueCount(); got != 5 {
		t.Errorf("IssueCount: got %d, want 5", got)
	}
}
