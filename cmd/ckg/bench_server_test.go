package main

import (
	"testing"
)

// TestPercentile_NearestRank locks in the percentile semantics used
// by bench-server: nearest-rank, sort-in-place, edge-clamp on rank ≥
// len. A regression here would shift every published baseline number
// without explanation.
func TestPercentile_NearestRank(t *testing.T) {
	cases := []struct {
		name string
		in   []float64
		p    int
		want float64
	}{
		{"single", []float64{42}, 50, 42},
		{"single_p99", []float64{42}, 99, 42},
		{"sorted_p50", []float64{1, 2, 3, 4, 5}, 50, 3},
		{"unsorted_p50", []float64{5, 1, 3, 4, 2}, 50, 3},
		{"p95_clamps_to_last", []float64{1, 2, 3, 4, 5}, 95, 5},
		{"p99_clamps_to_last", []float64{1, 2, 3, 4, 5}, 99, 5},
		{"p0_first", []float64{10, 20, 30}, 0, 10},
		{"empty_returns_zero", []float64{}, 50, 0},
	}
	for _, tc := range cases {
		// percentile mutates input — copy so the case data isn't
		// poisoned by an earlier run if the slice gets reused.
		cp := append([]float64(nil), tc.in...)
		if got := percentile(cp, tc.p); got != tc.want {
			t.Errorf("%s: percentile(%v, %d) = %v, want %v", tc.name, tc.in, tc.p, got, tc.want)
		}
	}
}

// TestBenchServerCmd_RequiresGraph mirrors the other CLI guard tests
// (evidence_test.go) — the cobra flag annotation alone provides this,
// but locking it here means a future contributor can't quietly drop
// MarkFlagRequired without test coverage flagging it.
func TestBenchServerCmd_RequiresGraph(t *testing.T) {
	cmd := newBenchServerCmd()
	cmd.SetArgs([]string{})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when --graph not provided")
	}
}

// TestBenchServerCmd_RejectsBadIterations covers the input validation
// path — non-positive or absurdly large iteration counts are
// programmer mistakes, not "best effort". Fail fast with a clear
// message.
func TestBenchServerCmd_RejectsBadIterations(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"zero", []string{"--graph", "/tmp/x", "--iterations", "0"}},
		{"negative", []string{"--graph", "/tmp/x", "--iterations", "-1"}},
		{"too_large", []string{"--graph", "/tmp/x", "--iterations", "999999"}},
		{"concurrency_zero", []string{"--graph", "/tmp/x", "--concurrency", "0"}},
		{"concurrency_too_large", []string{"--graph", "/tmp/x", "--concurrency", "1000"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newBenchServerCmd()
			cmd.SetArgs(tc.args)
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			if err := cmd.Execute(); err == nil {
				t.Errorf("expected error for %v, got nil", tc.args)
			}
		})
	}
}

// TestDefaultProbes_SeedConditional locks the documented behaviour:
// the impact probe is only included when a seedQname is provided,
// so graphs without Function nodes don't crash the bench loop on a
// 404 from /api/impact.
func TestDefaultProbes_SeedConditional(t *testing.T) {
	without := defaultProbes("")
	with := defaultProbes("pkg.Foo")
	if len(with) != len(without)+1 {
		t.Errorf("seed should add exactly one probe; without=%d with=%d",
			len(without), len(with))
	}
	hasImpact := func(probes []benchProbe) bool {
		for _, p := range probes {
			if p.Name == "impact" {
				return true
			}
		}
		return false
	}
	if hasImpact(without) {
		t.Errorf("defaultProbes(\"\") should NOT include impact probe")
	}
	if !hasImpact(with) {
		t.Errorf("defaultProbes(seed) should include impact probe")
	}
}
