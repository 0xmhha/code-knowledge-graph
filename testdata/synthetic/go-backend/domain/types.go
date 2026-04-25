package domain

type Wallet struct {
    Owner    string
    Balance  uint64
}

type DepositRequest struct {
    From   string
    Amount uint64
}
