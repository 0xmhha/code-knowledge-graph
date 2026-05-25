package service

import (
	"sync"

	"synth.test/backend/domain"
)

// SafeVault adapts Vault with a mutex so concurrent SafeDeposit callers
// don't race on the underlying wallet map. The presence of this type is
// the load-bearing fixture for B1 Stage 1 concurrency emission:
// retrieval fixtures R11/R12 lock the Mutex node and lock-protected
// caller relationships against regression in
// internal/parse/golang/concurrency*.go.
type SafeVault struct {
	mu    sync.Mutex
	vault *Vault
}

// NewSafeVault constructs a SafeVault around a fresh Vault. Kept
// separate from New so callers that don't need locking pay no cost.
func NewSafeVault() *SafeVault {
	return &SafeVault{vault: New()}
}

// SafeDeposit acquires the mutex, delegates to the underlying vault,
// and releases on return via defer. The body reads s.vault (a struct
// field) inside the critical section — that read drives the
// accessed_under_lock(field, mutex) edge.
func (s *SafeVault) SafeDeposit(req domain.DepositRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vault.Deposit(req)
}
