package eval

import (
	"sort"
	"testing"
)

// TestExtractSymbols_LineRefBlacklist locks the line-ref fix
// surfaced by the 2026-05-22 smoke run (post-A+B+D). Claude cites
// source locations as `file.ext:N`. The whole token survives the
// file-extension blacklist because the trailing `:23` lifts it
// back into the candidate set (`handler.go` would drop, but
// `handler.go:23` does not). The fix splits on the last `:` and
// drops the token when the suffix is numeric AND the prefix ends
// in a known file extension.
func TestExtractSymbols_LineRefBlacklist(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "go line ref dropped",
			in:   "see handler.go:23 and core.NewBlockChain",
			want: []string{"core.NewBlockChain"},
		},
		{
			name: "ts line ref dropped",
			in:   "vault.ts:5 has the definition; service.Vault.Deposit calls it",
			want: []string{"service.Vault.Deposit"},
		},
		{
			name: "sol line ref dropped",
			in:   "Vault.sol:3 declares the function pkg.Helper.Run",
			want: []string{"pkg.Helper.Run"},
		},
		{
			name: "multi-digit line ref dropped",
			in:   "core/blockchain.go:1234 — symbol is core.BlockChain.Insert",
			want: []string{"core.BlockChain.Insert"},
		},
		{
			name: "colon without number kept as-is (no line ref)",
			in:   "pkg.Func:variant should survive",
			want: []string{"pkg.Func:variant"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractSymbols(tc.in)
			sort.Strings(got)
			sort.Strings(tc.want)
			if !slicesEqual(got, tc.want) {
				t.Errorf("extractSymbols(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestExtractSymbols_HangulSeparator locks the Korean-particle fix
// surfaced by the same smoke run. Claude wrote `Vault.deposit을`
// and `Vault.deposit은` — the symbol with a Korean accusative or
// topic particle attached. Hangul syllables (U+AC00..U+D7A3) are
// now separators in FieldsFunc, so the particle splits off and
// the bare `Vault.deposit` survives.
func TestExtractSymbols_HangulSeparator(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "accusative particle 을 splits",
			in:   "Vault.deposit을 호출합니다",
			want: []string{"Vault.deposit"},
		},
		{
			name: "topic particle 은 splits",
			in:   "Vault.deposit은 entry입니다",
			want: []string{"Vault.deposit"},
		},
		{
			name: "possessive particle 의 splits",
			in:   "core.NewBlockChain의 caller는 api.Handler.HandleDeposit",
			want: []string{"core.NewBlockChain", "api.Handler.HandleDeposit"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractSymbols(tc.in)
			sort.Strings(got)
			sort.Strings(tc.want)
			if !slicesEqual(got, tc.want) {
				t.Errorf("extractSymbols(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
