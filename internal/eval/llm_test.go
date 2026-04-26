package eval_test

import (
	"errors"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/eval"
)

func TestAPIClientRequiresAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := eval.NewAPIClient("claude-sonnet-4-6")
	if !errors.Is(err, eval.ErrNoAPIKey) {
		t.Errorf("expected ErrNoAPIKey, got: %v", err)
	}
}
