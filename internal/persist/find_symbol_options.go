package persist

import "github.com/0xmhha/code-knowledge-graph/pkg/types"

// FindSymbolOptions configures filter push-down for StoreReader.FindSymbol.
// Zero value means "no filter" — every match on the name (and exactness mode)
// passes through.
//
// See docs/followups-from-cks-dogfood-2026-05-19.md item CKG-4 for the
// downstream motivation: cks Stage 2's `arch_explain` intent fetches
// Function / Type / Interface symbols separately, paying N round-trips
// for what should be one. With Kinds set, the SQL layer returns a kind-
// tagged result in a single query so cks can dedupe by Citation key on
// the way back.
type FindSymbolOptions struct {
	// Language pushes a `language = ?` predicate when non-empty.
	// Empty string disables the predicate.
	Language string

	// Kinds restricts results to the named NodeTypes (SQL `type IN (...)`).
	// Empty slice (or nil) disables the predicate — all kinds are returned.
	// Duplicates are tolerated (the SQL planner dedupes); callers don't need
	// to deduplicate.
	Kinds []types.NodeType
}
