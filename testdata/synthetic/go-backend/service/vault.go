package service

import "synth.test/backend/domain"

type Vault struct {
    wallets map[string]*domain.Wallet
}

func New() *Vault { return &Vault{wallets: map[string]*domain.Wallet{}} }

func (v *Vault) Deposit(req domain.DepositRequest) error {
    w, ok := v.wallets[req.From]
    if !ok {
        w = &domain.Wallet{Owner: req.From}
        v.wallets[req.From] = w
    }
    w.Balance += req.Amount
    return nil
}
