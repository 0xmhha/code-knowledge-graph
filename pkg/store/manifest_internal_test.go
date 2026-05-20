package store

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// TestManifestProjection_FieldMapping verifies the GetManifest helper
// reads the right field from the internal Manifest into each public
// field. The mapping is small enough that a single fixture covers it
// — if a future refactor renames an internal field, the test surfaces
// the breakage here instead of letting it silently zero out a public
// field at runtime.
//
// This test lives in `package store` (not store_test) so it can
// construct persist.Manifest directly — that struct is internal to
// the module, which is exactly the point of the projection.
func TestManifestProjection_FieldMapping(t *testing.T) {
	internal := persist.Manifest{
		SchemaVersion:  "1.9",
		SrcCommit:      "deadbeef1234567890abcdef",
		BuildTimestamp: "2026-05-20T12:00:00Z",
		// Internal-only fields that MUST NOT leak into the public mirror.
		SrcRoot:           "/Users/me/repo",
		SrcRelPath:        "internal",
		StalenessMTimeSum: 1234567,
		Files: []persist.FileEntry{
			{Path: "a.go", Language: "go"},
		},
	}

	got := projectManifest(internal)
	want := Manifest{
		CommitHash:     "deadbeef1234567890abcdef",
		SchemaVersion:  "1.9",
		IndexTimestamp: "2026-05-20T12:00:00Z",
	}
	if got != want {
		t.Errorf("projectManifest mismatch:\n got %+v\nwant %+v", got, want)
	}
}

// TestManifestProjection_ZeroValue confirms an empty internal Manifest
// projects to an empty public Manifest — no panic, no surprise default
// values that would mislead a freshness/drift signal in cks.ops.health.
func TestManifestProjection_ZeroValue(t *testing.T) {
	got := projectManifest(persist.Manifest{})
	if got != (Manifest{}) {
		t.Errorf("zero-value projection produced non-zero output: %+v", got)
	}
}
