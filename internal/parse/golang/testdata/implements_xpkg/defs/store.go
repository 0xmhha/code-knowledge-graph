// Package defs declares interfaces consumed by sibling packages. Mirrors the
// production case where (e.g.) persist.Store is defined in one package and
// satisfied by a struct in another.
package defs

// Store is the cross-package interface the impl package must satisfy.
// Two methods so go/types satisfaction is non-trivial.
type Store interface {
	Get(key string) (string, bool)
	Put(key, val string)
}
