package eval_test

import (
	"os"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/eval"
)

func TestLLMClientRequiresAPIKey(t *testing.T) {
	old := os.Getenv("ANTHROPIC_API_KEY")
	t.Cleanup(func() { os.Setenv("ANTHROPIC_API_KEY", old) })
	os.Setenv("ANTHROPIC_API_KEY", "")
	if _, err := eval.NewLLMClient("claude-sonnet-4-6"); err == nil {
		t.Errorf("expected error when ANTHROPIC_API_KEY missing")
	}
}
