package eval

import (
	"context"
	"strings"
	"testing"

	pkgstore "github.com/0xmhha/code-knowledge-graph/pkg/store"
)

func TestParseToolCalls_Single(t *testing.T) {
	text := `I'll search for it.
<tool_call name="find_symbol">
{"name": "Greet"}
</tool_call>`
	calls := parseToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if calls[0].Name != "find_symbol" {
		t.Errorf("name: got %q want find_symbol", calls[0].Name)
	}
	if !strings.Contains(string(calls[0].Args), "Greet") {
		t.Errorf("args missing Greet: %s", calls[0].Args)
	}
}

func TestParseToolCalls_Multiple(t *testing.T) {
	text := `Let me check both.
<tool_call name="find_callers">
{"qname": "a.Greet"}
</tool_call>
And also:
<tool_call name="find_callees">
{"qname": "a.Greet"}
</tool_call>`
	calls := parseToolCalls(text)
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
	if calls[0].Name != "find_callers" || calls[1].Name != "find_callees" {
		t.Errorf("names: got [%s, %s]", calls[0].Name, calls[1].Name)
	}
}

func TestParseToolCalls_None(t *testing.T) {
	text := `The answer is simply a.Greet at line 5.`
	calls := parseToolCalls(text)
	if len(calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(calls))
	}
}

func TestParseToolCalls_MalformedSkipped(t *testing.T) {
	text := `<tool_call name="find_symbol">no json here</tool_call>
<tool_call name="search_text">
{"query": "Greet"}
</tool_call>`
	calls := parseToolCalls(text)
	// First block has no JSON → args defaults to "{}"; second is valid.
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}
}

// mockLLM lets tests script the LLM's responses turn-by-turn.
type mockLLM struct {
	responses []string
	turn      int
}

func (m *mockLLM) Complete(ctx context.Context, system, user string) (LLMResult, error) {
	if m.turn >= len(m.responses) {
		return LLMResult{OutputText: "[no more scripted responses]"}, nil
	}
	r := m.responses[m.turn]
	m.turn++
	return LLMResult{OutputText: r, InputTokens: 100, OutputTokens: 20}, nil
}

func (m *mockLLM) CompleteWithTools(ctx context.Context, system, user string,
	store pkgstore.Reader) (LLMResult, error) {
	return runGammaPromptLoop(ctx, m, system, user, store)
}

func (m *mockLLM) Close() error { return nil }

func TestGammaPromptLoop_SingleTurnAnswer(t *testing.T) {
	store, _ := newEvalFixtureStore(t)
	llm := &mockLLM{responses: []string{"The answer is a.Greet."}}

	res, err := llm.CompleteWithTools(context.Background(), "system", "find Greet", store)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.OutputText, "a.Greet") {
		t.Errorf("output should contain a.Greet, got %q", res.OutputText)
	}
	if res.NumToolCalls != 0 {
		t.Errorf("expected 0 tool calls, got %d", res.NumToolCalls)
	}
}

func TestGammaPromptLoop_MultiTurn(t *testing.T) {
	store, _ := newEvalFixtureStore(t)
	llm := &mockLLM{responses: []string{
		`I'll search.
<tool_call name="find_symbol">
{"name": "Greet"}
</tool_call>`,
		`Found it. Final answer: a.Greet exists in a/a.go.`,
	}}

	res, err := llm.CompleteWithTools(context.Background(), "system", "find Greet", store)
	if err != nil {
		t.Fatal(err)
	}
	if res.NumToolCalls != 1 {
		t.Errorf("expected 1 tool call, got %d", res.NumToolCalls)
	}
	if !strings.Contains(res.OutputText, "a.Greet") {
		t.Errorf("final answer should contain a.Greet, got %q", res.OutputText)
	}
	// Cumulative tokens
	if res.InputTokens != 200 || res.OutputTokens != 40 {
		t.Errorf("token sum: input=%d output=%d want 200/40", res.InputTokens, res.OutputTokens)
	}
}

func TestGammaPromptLoop_MaxTurnsHit(t *testing.T) {
	store, _ := newEvalFixtureStore(t)
	// All responses include a tool call → loop never terminates cleanly.
	never := `<tool_call name="search_text">
{"query": "Greet"}
</tool_call>`
	responses := make([]string, gammaMaxTurns)
	for i := range responses {
		responses[i] = never
	}
	llm := &mockLLM{responses: responses}

	res, err := llm.CompleteWithTools(context.Background(), "system", "find Greet", store)
	if err != nil {
		t.Fatal(err)
	}
	if res.NumToolCalls != gammaMaxTurns {
		t.Errorf("expected %d tool calls, got %d", gammaMaxTurns, res.NumToolCalls)
	}
	if !strings.Contains(res.OutputText, "max turns") {
		t.Errorf("expected max-turns marker in output")
	}
}
