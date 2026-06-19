package coll1

// MaxItems is a package-level const — symbol-identity Phase 1 must emit a
// canonical id ckgresolve.test/coll1.MaxItems for it.
const MaxItems = 16

// defaultName is a package-level var — canonical id
// ckgresolve.test/coll1.defaultName.
var defaultName = "set"

// blank is a package-level blank var — B1: the `_` identifier must NOT get a
// canonical id (many `var _` declarations would otherwise collide on <pkg>._).
var _ = defaultName

// Padded has a blank padding field — its `_` field must also get no canonical id.
type Padded struct {
	_ [4]byte
	X int
}

// useLocal declares a function-local `var` — B2: a local var must NOT get a
// canonical id (its <pkg>.localOnly is non-unique across functions and is not a
// retrieval target). Only package-level MaxItems/defaultName do.
func useLocal() int {
	var localOnly = MaxItems
	return localOnly
}
