// Package impl satisfies defs.Store across the package boundary. This is
// the production case the implements pass must cover (sqliteStore implements
// persist.Store across packages).
package impl

import "ckgimplementsxpkg.test/defs"

// Compile-time satisfaction check — also forces packages.Load to pull defs
// even when only impl is requested directly.
var _ defs.Store = (*MemStore)(nil)

// MemStore satisfies defs.Store with both methods. The implements pass must
// emit impl.MemStore -> defs.Store using go/types' cross-package scope.
type MemStore struct {
	data map[string]string
}

// Get is a value-receiver method.
func (m MemStore) Get(key string) (string, bool) {
	v, ok := m.data[key]
	return v, ok
}

// Put is a pointer-receiver method — exercises the *T method-set check.
func (m *MemStore) Put(key, val string) {
	if m.data == nil {
		m.data = map[string]string{}
	}
	m.data[key] = val
}
