package buildpipe_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// TestPipelineRunsOnGoFixture exercises the full Pass 1..4 pipeline on the
// existing Go resolve fixture (its own go.mod under
// internal/parse/golang/testdata/resolve). Asserts that nodes were persisted
// and that a staleness fingerprint (git commit OR mtime) was recorded.
func TestPipelineRunsOnGoFixture(t *testing.T) {
	out := t.TempDir()
	_, err := buildpipe.Run(buildpipe.Options{
		SrcRoot:    "../parse/golang/testdata/resolve",
		OutDir:     out,
		Languages:  []string{"auto"},
		CKGVersion: "test",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	store, err := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer store.Close()

	m, err := store.GetManifest()
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if m.Stats["nodes"] == 0 {
		t.Errorf("expected nodes > 0, got 0")
	}
	if m.SrcCommit == "" && m.StalenessMethod != "mtime" {
		t.Errorf("expected staleness fingerprint, got method=%q commit=%q",
			m.StalenessMethod, m.SrcCommit)
	}
}

// TestPipelineXLangBinding builds a synthetic mini multi-lang fixture
// (1 .sol contract + 1 .ts class with the matching name) and asserts that
// at least one binds_to edge was emitted by the cross-language linker (T20)
// and persisted to SQLite.
func TestPipelineXLangBinding(t *testing.T) {
	out := t.TempDir()
	_, err := buildpipe.Run(buildpipe.Options{
		SrcRoot:    "testdata/synthetic",
		OutDir:     out,
		Languages:  []string{"auto"},
		CKGVersion: "test",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	store, err := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer store.Close()

	rows, err := store.QueryEdgesByType("binds_to")
	if err != nil {
		t.Fatalf("QueryEdgesByType: %v", err)
	}
	if len(rows) == 0 {
		t.Errorf("expected at least one binds_to edge, got 0")
	}
}
