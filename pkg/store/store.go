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
// Reader.SearchFTS or Reader.FindSymbol. Manifest is intentionally
// not re-exported here yet — see docs/followups-from-cks-dogfood-2026-05-19.md
// item CKG-7 for the planned minimal Manifest mirror.
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

// ErrInvalidMetric is returned by Reader.TopNodes when the metric
// argument is not one of the supported column names. HTTP layers
// typically map this to 400.
var ErrInvalidMetric = persist.ErrInvalidMetric

// OpenReadOnly opens a graph DB at path for read-only access. The
// returned Reader must be closed by the caller via Reader.Close().
func OpenReadOnly(path string) (Reader, error) {
	return persist.OpenReadOnly(path)
}
