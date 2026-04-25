package eval_test

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/eval"
)

func TestAllowedTools(t *testing.T) {
	cases := []struct {
		name    string
		b       eval.Baseline
		wantNil bool
		want    []string
	}{
		{
			name:    "alpha returns nil",
			b:       eval.BaselineAlpha,
			wantNil: true,
		},
		{
			name: "beta returns get_subgraph",
			b:    eval.BaselineBeta,
			want: []string{"get_subgraph"},
		},
		{
			name: "gamma returns 5 tools",
			b:    eval.BaselineGamma,
			want: []string{"find_symbol", "find_callers", "find_callees", "get_subgraph", "search_text"},
		},
		{
			name: "delta returns get_context_for_task",
			b:    eval.BaselineDelta,
			want: []string{"get_context_for_task"},
		},
		{
			name:    "unknown baseline returns nil",
			b:       eval.Baseline("omega"),
			wantNil: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eval.AllowedTools(tc.b)
			if tc.wantNil {
				if got != nil {
					t.Errorf("want nil, got %v", got)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Errorf("len mismatch: want %v, got %v", tc.want, got)
				return
			}
			for i, w := range tc.want {
				if got[i] != w {
					t.Errorf("index %d: want %q, got %q", i, w, got[i])
				}
			}
		})
	}
}

func TestSystemPrompt(t *testing.T) {
	cases := []struct {
		name        string
		b           eval.Baseline
		wantSubstr  string
		wantEmpty   bool
	}{
		{
			name:       "alpha contains raw source mention",
			b:          eval.BaselineAlpha,
			wantSubstr: "Raw source files",
		},
		{
			name:       "beta mentions get_subgraph",
			b:          eval.BaselineBeta,
			wantSubstr: "get_subgraph",
		},
		{
			name:       "gamma mentions find_symbol",
			b:          eval.BaselineGamma,
			wantSubstr: "find_symbol",
		},
		{
			name:       "delta mentions get_context_for_task",
			b:          eval.BaselineDelta,
			wantSubstr: "get_context_for_task",
		},
		{
			name:      "unknown baseline returns empty string",
			b:         eval.Baseline("omega"),
			wantEmpty: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eval.SystemPrompt(tc.b)
			if tc.wantEmpty {
				if got != "" {
					t.Errorf("want empty string for unknown baseline, got %q", got)
				}
				return
			}
			if len(got) == 0 {
				t.Errorf("want non-empty prompt for baseline %q", tc.b)
			}
			if !strings.Contains(got, tc.wantSubstr) {
				t.Errorf("want prompt to contain %q, got %q", tc.wantSubstr, got)
			}
		})
	}
}
