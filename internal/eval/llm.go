package eval

import (
	"context"
	"errors"
	"os"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// ErrNoAPIKey is returned by NewLLMClient when ANTHROPIC_API_KEY is unset.
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

// LLMClient wraps the Anthropic Messages API. Construct one per ckg eval run.
type LLMClient struct {
	c     *anthropic.Client
	model string
}

func NewLLMClient(model string) (*LLMClient, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, ErrNoAPIKey
	}
	c := anthropic.NewClient(option.WithAPIKey(key))
	return &LLMClient{c: &c, model: model}, nil
}

// Complete runs a single message exchange. tools is a list of MCP-style tool
// JSONSchema definitions (or nil for α). The implementation here is the V0
// minimum: it sends `system + user`, captures `usage`, and returns. For
// baselines β/γ/δ that need real tool calls, the runner (Task 35) will loop
// until no tool_use is requested.
func (l *LLMClient) Complete(ctx context.Context, system, user string, tools []anthropic.ToolUnionParam) (LLMResult, error) {
	msg, err := l.c.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(l.model),
		MaxTokens: 4096,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
		Tools: tools,
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
