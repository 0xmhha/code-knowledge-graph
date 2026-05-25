// Package store is the public, read-only graph access surface for external
// callers (eval harness, sister repos like code-knowledge-system). It
// re-exports the minimum useful subset of internal/persist as type aliases
// so callers don't have to depend on internal/persist directly. Write
// access stays internal — there is no Writer here by design.
//
// # Stability
//
// This surface follows semantic versioning once the sister-repo extraction
// lands. Until then, treat it as the single throat to choke when changing
// internal/persist — anything that breaks the alias here will break
// external consumers, even if in-repo callers compile fine.
//
// # What to import from where
//
// External consumers (anything outside this module) should import only
// from pkg/store and pkg/types. They cannot reach internal/persist by
// the Go `internal/` rule, and that's intentional: pkg/store decides what
// to promote to the public surface.
//
// Reader covers the read API; SearchHit / SearchFTSOptions /
// FindSymbolOptions are the value types you'll touch when calling
// Reader.SearchFTS or Reader.FindSymbol. Manifest is the minimal
// mirror of build-time metadata — use the GetManifest helper rather
// than Reader.GetManifest directly so the projection (which drops
// incremental-cache fields) stays the single source of truth (CKG-7).
//
// # Do NOT
//
// External code MUST NOT type-alias persist.StoreReader on its own
// (the "self-shim" pattern surfaced by the cks dogfood). That duplicates
// the public surface and silently drifts the moment we change
// internal/persist. If you find yourself wanting one, it means a type
// you need isn't re-exported here yet — open a PR to add the alias
// instead.
package store

import (
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Reader is the read-only graph surface — the canonical entry point for
// external consumers. Aliased from persist.StoreReader; any change to
// the upstream interface is a breaking change here.
type Reader = persist.StoreReader

// SearchHit pairs a node with its full-text search relevance score.
// Returned by Reader.SearchFTS. See persist.SearchHit doc for the
// Score vs RawScore semantics (CKG-1).
type SearchHit = persist.SearchHit

// SearchFTSOptions configures filter push-down for Reader.SearchFTS.
// Zero value means "no filter" (CKG-2).
type SearchFTSOptions = persist.SearchFTSOptions

// FindSymbolOptions configures filter push-down for Reader.FindSymbol
// (Language, Kinds). Zero value means "no filter" (CKG-4).
type FindSymbolOptions = persist.FindSymbolOptions

// PRRef is the public alias for the build-time-derived PR breadcrumb
// (ckg-NEW-2). External consumers (cks, ckv) read this via
// Reader.GetNodePRs; see pkg/types.PRRef for the field semantics and
// the temporal-slicing contract.
type PRRef = types.PRRef

// ErrInvalidMetric is returned by Reader.TopNodes when the metric
// argument is not one of the supported column names. HTTP layers
// typically map this to 400.
var ErrInvalidMetric = persist.ErrInvalidMetric

// Manifest is the public, minimal snapshot of build-time metadata an
// external consumer needs (CKG-7). Compared to the internal
// persist.Manifest, this type deliberately drops incremental-cache
// fields (SrcRoot, Files, StalenessFiles, StalenessMTimeSum, …) — those
// rotate with build-pipeline changes and external consumers must not
// depend on them.
//
// Use cases (mirroring what the cks dogfood surfaced):
//   - CommitHash drives Citation.CommitHash drift detection
//   - SchemaVersion gates compatibility in cks.ops.health
//   - IndexTimestamp shows freshness in user-facing status output
//
// Adding a field here is a breaking change for external consumers —
// it forces them to widen their struct in lockstep. If a new field is
// truly needed by every consumer, add it; if only one consumer needs
// it, prefer exposing it through a dedicated Reader method instead.
type Manifest struct {
	CommitHash     string // source commit at index time; empty when build did not record one
	SchemaVersion  string // ckg schema version, e.g. "1.9"
	IndexTimestamp string // RFC3339 timestamp of the build
}

// OpenReadOnly opens a graph DB at path for read-only access. The
// returned Reader must be closed by the caller via Reader.Close().
func OpenReadOnly(path string) (Reader, error) {
	return persist.OpenReadOnly(path)
}

// GetManifest reads the build-time manifest from r and projects it
// onto the minimal public Manifest. External consumers should use
// this in preference to Reader.GetManifest, which returns the full
// internal struct (whose field set is not part of the public API).
//
// The projection is intentionally one-way: there is no inverse
// operation. If a consumer needs richer metadata, the right move is
// to widen Manifest here (a breaking change with deliberate review)
// rather than to read internal fields by other means.
func GetManifest(r Reader) (Manifest, error) {
	m, err := r.GetManifest()
	if err != nil {
		return Manifest{}, err
	}
	return projectManifest(m), nil
}

// projectManifest is the single source of truth for the internal →
// public Manifest mapping. Extracted so the field mapping is
// unit-testable without standing up a real graph fixture.
func projectManifest(m persist.Manifest) Manifest {
	return Manifest{
		CommitHash:     m.SrcCommit,
		SchemaVersion:  m.SchemaVersion,
		IndexTimestamp: m.BuildTimestamp,
	}
}
