package eval

import (
	"context"
	"errors"
	"os"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// ErrNoAPIKey is returned by NewAPIClient when ANTHROPIC_API_KEY is unset.
var ErrNoAPIKey = errors.New("ANTHROPIC_API_KEY not set")

// LLMResult bundles a single completion's output text and usage counters.
type LLMResult struct {
	OutputText        string
	InputTokens       int
	OutputTokens      int
	CacheReadTokens   int
	CacheCreateTokens int
	NumToolCalls      int
}

// LLMClient is the abstraction the eval runner uses for completions. The
// Anthropic Messages API (APIClient) and the Claude Code CLI (CLIClient)
// both implement it. Close releases backend-specific resources (e.g.,
// shutting down a cli-wrapper Manager); APIClient.Close is a no-op.
type LLMClient interface {
	Complete(ctx context.Context, system, user string) (LLMResult, error)
	Close() error
}

// APIClient wraps the Anthropic Messages API. Construct one per ckg eval run.
type APIClient struct {
	c     *anthropic.Client
	model string
}

// NewAPIClient constructs an APIClient. It reads ANTHROPIC_API_KEY from the
// environment and returns ErrNoAPIKey when unset.
func NewAPIClient(model string) (*APIClient, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, ErrNoAPIKey
	}
	c := anthropic.NewClient(option.WithAPIKey(key))
	return &APIClient{c: &c, model: model}, nil
}

// Complete runs a single message exchange via the Anthropic API. The V0
// implementation does not loop over tool_use round-trips; the runner
// pre-resolves any tool calls in-process before invoking Complete.
func (l *APIClient) Complete(ctx context.Context, system, user string) (LLMResult, error) {
	msg, err := l.c.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(l.model),
		MaxTokens: 4096,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		return LLMResult{}, err
	}
	out := LLMResult{
		InputTokens:       int(msg.Usage.InputTokens),
		OutputTokens:      int(msg.Usage.OutputTokens),
		CacheReadTokens:   int(msg.Usage.CacheReadInputTokens),
		CacheCreateTokens: int(msg.Usage.CacheCreationInputTokens),
	}
	for _, b := range msg.Content {
		if b.Type == "text" {
			out.OutputText += b.Text
		}
	}
	return out, nil
}

// Close releases resources. APIClient holds none; this is a no-op so the
// interface contract is uniform across backends.
func (l *APIClient) Close() error { return nil }
