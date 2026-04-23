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
	if got.SrcCommit != "abc123" || got.Languages["go"] != 10 {
		t.Errorf("round-trip mismatch: got %+v", got)
	}
}
