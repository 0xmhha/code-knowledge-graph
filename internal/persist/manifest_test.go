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
	defer func() { _ = store.Close() }()
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
		GraphDigest:     "4be26516f2091d3494051961947cf89e7ee7faaa2d95d116f18b4788d345cfbe",
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
	// graph_digest must survive the in-db manifest round-trip — CKV/CKS read it
	// via `SELECT value FROM manifest WHERE key='graph_digest'`.
	if got.GraphDigest != m.GraphDigest {
		t.Errorf("GraphDigest = %q, want %q", got.GraphDigest, m.GraphDigest)
	}
}

// TestManifestRoundTrip_FilesV2 verifies that the schema-1.2 Files block
// (A3 incremental cache) survives Set/Get unchanged. Locks the FileEntry
// JSON shape so a future refactor can't silently drop a field.
func TestManifestRoundTrip_FilesV2(t *testing.T) {
	store, err := persist.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}

	want := []persist.FileEntry{
		{
			Path: "internal/foo/bar.go", Language: "go",
			SHA256:        "aaaa1111bbbb2222cccc3333dddd4444eeee5555ffff66667777888899990000",
			CacheKey:      "0000111122223333444455556666777788889999aaaabbbbccccddddeeeeffff",
			MTime:         1714291200000000000,
			ParserVersion: "go/go1.25.5",
			NodeIDs:       []string{"node_aaaa00000001", "node_bbbb00000002"},
			EdgeIDs:       []int64{42, 43, 44},
		},
		{
			Path: "src/index.ts", Language: "ts",
			SHA256:        "1111000022223333444455556666777788889999aaaabbbbccccddddeeeeffff",
			CacheKey:      "ffffeeeeddddccccbbbbaaaa999988887777666655554444333322221111ffff",
			MTime:         1714291300000000000,
			ParserVersion: "tree-sitter/v0.0.0-20240827",
			NodeIDs:       []string{"node_ts00000000001"},
			EdgeIDs:       nil, // no per-file edges produced
		},
	}
	m := persist.Manifest{
		SchemaVersion: persistSchemaVersionForTest(),
		CKGVersion:    "0.2.0", BuildTimestamp: "2026-04-28T10:00:00Z",
		SrcRoot:         "/tmp/src",
		StalenessMethod: "git",
		Languages:       map[string]int{"go": 1, "ts": 1},
		Stats:           map[string]int{"nodes": 3},
		Files:           want,
	}
	if err := store.SetManifest(m); err != nil {
		t.Fatalf("SetManifest: %v", err)
	}
	got, err := store.GetManifest()
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if len(got.Files) != len(want) {
		t.Fatalf("Files len = %d, want %d", len(got.Files), len(want))
	}
	for i := range want {
		if got.Files[i].Path != want[i].Path {
			t.Errorf("Files[%d].Path = %q, want %q", i, got.Files[i].Path, want[i].Path)
		}
		if got.Files[i].SHA256 != want[i].SHA256 {
			t.Errorf("Files[%d].SHA256 = %q, want %q", i, got.Files[i].SHA256, want[i].SHA256)
		}
		if got.Files[i].CacheKey != want[i].CacheKey {
			t.Errorf("Files[%d].CacheKey = %q, want %q", i, got.Files[i].CacheKey, want[i].CacheKey)
		}
		if got.Files[i].MTime != want[i].MTime {
			t.Errorf("Files[%d].MTime = %d, want %d", i, got.Files[i].MTime, want[i].MTime)
		}
		if got.Files[i].ParserVersion != want[i].ParserVersion {
			t.Errorf("Files[%d].ParserVersion = %q, want %q",
				i, got.Files[i].ParserVersion, want[i].ParserVersion)
		}
		if len(got.Files[i].NodeIDs) != len(want[i].NodeIDs) {
			t.Errorf("Files[%d].NodeIDs len = %d, want %d",
				i, len(got.Files[i].NodeIDs), len(want[i].NodeIDs))
		}
	}
}

// TestManifestRoundTrip_FilesAbsent_LegacyCompat verifies that an old
// (pre-1.2) manifest with no "files" key reloads cleanly with Files==nil.
// Mirrors the omitempty contract — a fresh GetManifest must not fabricate
// an empty slice into existence.
func TestManifestRoundTrip_FilesAbsent_LegacyCompat(t *testing.T) {
	store, err := persist.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(); err != nil {
		t.Fatal(err)
	}
	// Set a manifest without Files, simulating a 1.1 build.
	if err := store.SetManifest(persist.Manifest{
		SchemaVersion: "1.1", CKGVersion: "0.1.0",
		BuildTimestamp:  "2026-04-23T12:00:00Z",
		SrcRoot:         "/tmp/src",
		StalenessMethod: "mtime",
	}); err != nil {
		t.Fatalf("SetManifest: %v", err)
	}
	got, err := store.GetManifest()
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if got.Files != nil {
		t.Errorf("Files = %v (len %d), want nil for legacy manifest",
			got.Files, len(got.Files))
	}
}

// persistSchemaVersionForTest mirrors the buildpipe.SchemaVersion constant
// without importing the package (avoids import cycle in persist tests).
// Update when the constant moves. Current value tracks W-C W11 V8
// (2026-05-19): 1.11 for the nodes.attrs JSON-blob column.
func persistSchemaVersionForTest() string { return "1.11" }
