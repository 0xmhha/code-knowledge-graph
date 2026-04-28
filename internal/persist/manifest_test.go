package persist_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

func TestManifestRoundTrip(t *testing.T) {
	store, err := persist.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}

	m := persist.Manifest{
		SchemaVersion: "1.0", CKGVersion: "0.1.0",
		BuildTimestamp:  "2026-04-23T12:00:00Z",
		SrcRoot:         "/tmp/src",
		SrcRelPath:      "testdata/synthetic",
		SrcCommit:       "abc123",
		StalenessMethod: "git",
		Languages:       map[string]int{"go": 10},
		Stats:           map[string]int{"nodes": 100, "edges": 200},
	}
	if err := store.SetManifest(m); err != nil {
		t.Fatalf("SetManifest: %v", err)
	}
	got, err := store.GetManifest()
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	// Assert every field that participates in the kv-row table so a future
	// refactor can't silently drop one (e.g. forgetting to add a new field
	// to the row list in SetManifest or the kv map in GetManifest).
	if got.SchemaVersion != "1.0" {
		t.Errorf("SchemaVersion = %q, want %q", got.SchemaVersion, "1.0")
	}
	if got.CKGVersion != "0.1.0" {
		t.Errorf("CKGVersion = %q, want %q", got.CKGVersion, "0.1.0")
	}
	if got.BuildTimestamp != "2026-04-23T12:00:00Z" {
		t.Errorf("BuildTimestamp = %q, want %q", got.BuildTimestamp, "2026-04-23T12:00:00Z")
	}
	if got.SrcRoot != "/tmp/src" {
		t.Errorf("SrcRoot = %q, want %q", got.SrcRoot, "/tmp/src")
	}
	if got.SrcRelPath != "testdata/synthetic" {
		t.Errorf("SrcRelPath = %q, want %q", got.SrcRelPath, "testdata/synthetic")
	}
	if got.SrcCommit != "abc123" {
		t.Errorf("SrcCommit = %q, want %q", got.SrcCommit, "abc123")
	}
	if got.StalenessMethod != "git" {
		t.Errorf("StalenessMethod = %q, want %q", got.StalenessMethod, "git")
	}
	if got.Languages["go"] != 10 {
		t.Errorf("Languages[go] = %d, want 10", got.Languages["go"])
	}
	if got.Stats["nodes"] != 100 || got.Stats["edges"] != 200 {
		t.Errorf("Stats = %+v, want {nodes:100, edges:200}", got.Stats)
	}
}
