package buildpipe_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// W-C W11 V13 — schema version downgrade guard audit.
//
// Background: SchemaVersion is the cache-key contributor that
// invalidates the manifest when the extraction schema drifts.
// TestIncremental_SchemaBumpInvalidates already locks the legacy-
// upward direction (older persisted schema like "1.0" vs current
// "1.11" → full rebuild). V13 audits the symmetric direction —
// a manifest written by a NEWER binary (e.g. SchemaVersion="1.99"
// in the DB) being opened by an older binary whose constant is
// still "1.11".
//
// Desired safety contract:
//
//   - matching SchemaVersion → usable (cache hit path).
//   - older persisted schema  → NOT usable (legacy invalidation).
//   - newer persisted schema  → NOT usable (downgrade guard:
//     older binary cannot reason about rows / columns introduced
//     by a future schema; full rebuild is the only safe path).
//   - nil manifest            → NOT usable (defensive).
//   - empty Files             → NOT usable (defensive).
//
// A regression that loosens the strict equality (e.g. `>=`
// semver compare without the newer-side guard) would silently
// allow an older binary to overwrite a future-schema DB with
// downgraded data — exactly the failure mode this audit
// guards against.
func TestManifestUsable_DowngradeGuard(t *testing.T) {
	const ckg = "0.1.0"
	const future = "1.99"

	files := []persist.FileEntry{
		{Path: "a.go", SHA256: "deadbeef", NodeIDs: []string{"n1"}},
	}

	cases := []struct {
		name string
		man  *persist.Manifest
		want bool
	}{
		{
			name: "nil manifest is not usable",
			man:  nil,
			want: false,
		},
		{
			name: "matching schema + ckg + non-empty files is usable",
			man: &persist.Manifest{
				SchemaVersion: buildpipe.SchemaVersion,
				CKGVersion:    ckg,
				Files:         files,
			},
			want: true,
		},
		{
			name: "older persisted schema is not usable",
			man: &persist.Manifest{
				SchemaVersion: "1.0",
				CKGVersion:    ckg,
				Files:         files,
			},
			want: false,
		},
		{
			name: "newer persisted schema is not usable (downgrade guard)",
			man: &persist.Manifest{
				SchemaVersion: future,
				CKGVersion:    ckg,
				Files:         files,
			},
			want: false,
		},
		{
			name: "empty Files block is not usable",
			man: &persist.Manifest{
				SchemaVersion: buildpipe.SchemaVersion,
				CKGVersion:    ckg,
				Files:         nil,
			},
			want: false,
		},
		{
			name: "empty CKGVersion is not usable",
			man: &persist.Manifest{
				SchemaVersion: buildpipe.SchemaVersion,
				CKGVersion:    "",
				Files:         files,
			},
			want: false,
		},
		{
			name: "drifted CKGVersion is not usable",
			man: &persist.Manifest{
				SchemaVersion: buildpipe.SchemaVersion,
				CKGVersion:    "0.0.1",
				Files:         files,
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := buildpipe.ManifestUsable(tc.man, ckg)
			if got != tc.want {
				t.Errorf("ManifestUsable: got %v, want %v", got, tc.want)
			}
		})
	}
}
