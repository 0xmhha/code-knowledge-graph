package smartctx

import (
	"testing"
	"time"
)

// TestOptionsDefaults locks the published default values so a casual
// edit to withDefaults can't silently regress them. These numbers are
// load-bearing for the eval δ baseline measurement — changing them
// without a Stage B re-run would invalidate the comparison.
func TestOptionsDefaults(t *testing.T) {
	got := Options{}.withDefaults()
	if got.BudgetTokens != 8000 {
		t.Errorf("BudgetTokens: got %d, want 8000", got.BudgetTokens)
	}
	if got.MaxBodies != 5 {
		t.Errorf("MaxBodies: got %d, want 5", got.MaxBodies)
	}
	if got.CandidateLimit != defaultCandidateLimit {
		t.Errorf("CandidateLimit: got %d, want %d", got.CandidateLimit, defaultCandidateLimit)
	}
	if got.CandidateLimit != 100 {
		t.Errorf("CandidateLimit default drifted: got %d, want 100 (P0 #3)", got.CandidateLimit)
	}
	if got.RankedCap != defaultRankedCap {
		t.Errorf("RankedCap: got %d, want %d", got.RankedCap, defaultRankedCap)
	}
	if got.RankedCap != 50 {
		t.Errorf("RankedCap default drifted: got %d, want 50 (P0 #3)", got.RankedCap)
	}
	if got.MaxSummaries != defaultMaxSummaries {
		t.Errorf("MaxSummaries: got %d, want %d", got.MaxSummaries, defaultMaxSummaries)
	}
	if got.MaxSummaries != 25 {
		t.Errorf("MaxSummaries default drifted: got %d, want 25 (P0 #3)", got.MaxSummaries)
	}
	if got.PRsPerNode != 3 {
		t.Errorf("PRsPerNode: got %d, want 3", got.PRsPerNode)
	}
	if got.ImpactDepth != 1 {
		t.Errorf("ImpactDepth: got %d, want 1", got.ImpactDepth)
	}
	if got.HopDepth != defaultHopDepth {
		t.Errorf("HopDepth: got %d, want %d", got.HopDepth, defaultHopDepth)
	}
	if got.HopDepth != 1 {
		t.Errorf("HopDepth default drifted: got %d, want 1 (P2 #7)", got.HopDepth)
	}
	if got.HopFrontierCap != defaultHopFrontierCap {
		t.Errorf("HopFrontierCap: got %d, want %d", got.HopFrontierCap, defaultHopFrontierCap)
	}
	if got.HopFrontierCap != 200 {
		t.Errorf("HopFrontierCap default drifted: got %d, want 200 (P2 #7)", got.HopFrontierCap)
	}
	if !got.PRCutoff.IsZero() {
		t.Errorf("PRCutoff should be zero (no cutoff) when unset, got %v", got.PRCutoff)
	}
}

// TestOptionsHopDepthClamp confirms the loader caps HopDepth at
// maxHopDepth — a caller that asks for 5 hops gets 3 instead, with
// no error. Deeper hops degenerate into "the whole reachable graph"
// without useful retrieval signal beyond what BM25 already surfaces.
func TestOptionsHopDepthClamp(t *testing.T) {
	got := Options{HopDepth: 5}.withDefaults()
	if got.HopDepth != maxHopDepth {
		t.Errorf("HopDepth=5 should clamp to maxHopDepth=%d; got %d", maxHopDepth, got.HopDepth)
	}
}

// TestOptionsOverrides confirms that non-zero caller values survive
// withDefaults intact — the defaults must only fill zero values.
func TestOptionsOverrides(t *testing.T) {
	custom := Options{
		BudgetTokens:   4000,
		MaxBodies:      3,
		CandidateLimit: 200,
		RankedCap:      80,
		MaxSummaries:   40,
		HopDepth:       2,
		HopFrontierCap: 150,
		PRsPerNode:     5,
		ImpactDepth:    2,
		PRCutoff:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	got := custom.withDefaults()
	if got.BudgetTokens != 4000 {
		t.Errorf("BudgetTokens override lost: %d", got.BudgetTokens)
	}
	if got.MaxBodies != 3 {
		t.Errorf("MaxBodies override lost: %d", got.MaxBodies)
	}
	if got.CandidateLimit != 200 {
		t.Errorf("CandidateLimit override lost: %d", got.CandidateLimit)
	}
	if got.RankedCap != 80 {
		t.Errorf("RankedCap override lost: %d", got.RankedCap)
	}
	if got.MaxSummaries != 40 {
		t.Errorf("MaxSummaries override lost: %d", got.MaxSummaries)
	}
	if got.PRsPerNode != 5 {
		t.Errorf("PRsPerNode override lost: %d", got.PRsPerNode)
	}
	if got.ImpactDepth != 2 {
		t.Errorf("ImpactDepth override lost: %d", got.ImpactDepth)
	}
	if got.HopDepth != 2 {
		t.Errorf("HopDepth override lost: %d", got.HopDepth)
	}
	if got.HopFrontierCap != 150 {
		t.Errorf("HopFrontierCap override lost: %d", got.HopFrontierCap)
	}
	if got.PRCutoff.IsZero() {
		t.Errorf("PRCutoff override lost: %v", got.PRCutoff)
	}
}
