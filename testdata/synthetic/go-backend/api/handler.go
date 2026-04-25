package api

import (
    "encoding/json"
    "net/http"

    "synth.test/backend/domain"
    "synth.test/backend/service"
)

type Handler struct {
    vault *service.Vault
}

func NewHandler() *Handler { return &Handler{vault: service.New()} }

func (h *Handler) HandleDeposit(w http.ResponseWriter, r *http.Request) {
    var req domain.DepositRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, err.Error(), 400)
        return
    }
    if err := h.vault.Deposit(req); err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    w.WriteHeader(204)
}
