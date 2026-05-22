package eval

import (
	"sort"
	"testing"
)

// TestExtractSymbols_NumericLiteralBlacklist locks the cycle 7 fix
// for `0.7` / `1.0` / `0.5`-style float literals leaking through.
// The task description's threshold (`precision: 0.7`) and any
// LLM-quoted float ends up as a dot-bearing candidate.
func TestExtractSymbols_NumericLiteralBlacklist(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "threshold 0.7 dropped",
			in:   "Score reached 0.7 against core.NewBlockChain",
			want: []string{"core.NewBlockChain"},
		},
		{
			name: "version-like identifier preserved",
			in:   "Use v1.Func and 0.7 threshold",
			want: []string{"v1.Func"},
		},
		{
			name: "multi-digit float dropped",
			in:   "Confidence 0.75; symbol api.Handler.HandleDeposit",
			want: []string{"api.Handler.HandleDeposit"},
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

// TestExtractSymbols_SpecialCharSplit locks the cycle 7 fix for
// β's node-ID leak (`VaultService.depositFn#CallSite@153`) and
// Claude's arrow-prose (`NewHandler→service.New`). #, @, and
// U+2192 are now FieldsFunc separators.
func TestExtractSymbols_SpecialCharSplit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "node-id hash splits",
			in:   "VaultService.depositFn#CallSite@153 invoked the deposit",
			want: []string{"VaultService.depositFn"},
		},
		{
			name: "arrow separates caller and callee",
			in:   "Chain: NewHandler→service.New and HandleDeposit→service.Vault.Deposit",
			want: []string{"service.New", "service.Vault.Deposit"},
		},
		{
			name: "ampersat without prefix splits",
			in:   "see @decorator on pkg.Func.Handler",
			want: []string{"pkg.Func.Handler"},
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
