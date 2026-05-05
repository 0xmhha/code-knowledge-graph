// Package store is the public, read-only graph access surface for external
// callers (eval harness, future sister repos). It re-exports the minimum
// useful subset of internal/persist as type aliases so callers don't have
// to depend on internal/persist directly. Write access stays internal —
// there is no Writer here by design.
//
// Stability: this surface follows semantic versioning once the sister-repo
// extraction lands. Until then, treat it as the single throat to choke when
// changing internal/persist.StoreReader — anything that breaks the alias
// here will break external consumers, even if in-repo callers compile fine.
package store

import "github.com/0xmhha/code-knowledge-graph/internal/persist"

// Reader is the read-only graph surface.
type Reader = persist.StoreReader

// OpenReadOnly opens a graph DB at path for read-only access. The returned
// Reader must be closed by the caller via Reader.Close().
func OpenReadOnly(path string) (Reader, error) {
	return persist.OpenReadOnly(path)
}
