package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"

	pkgstore "github.com/0xmhha/code-knowledge-graph/pkg/store"
)

// γ tool-use loop — V1 (2026-05-28). Replaces the V0 placeholder where
// γ shipped tool names in the system prompt but never executed them.
// LLM now calls find_symbol/find_callers/find_callees/get_subgraph/
// search_text via Anthropic's tool_use protocol; each call dispatches
// in-process to the same pkgstore.Reader the other baselines use, then
// the result feeds the next LLM turn until stop_reason="end_turn".

const (
	gammaMaxTurns     = 8 // upper bound on LLM↔tool round-trips per task
	gammaSearchLimit  = 20
	gammaDepth        = 2
	gammaDefaultModel = "claude-sonnet-4-6"
	gammaMaxTokens    = 4096
)

// gammaResult mirrors LLMResult but tracks the total turn count for
// the multi-turn cost analysis (number of tool calls + 1 final text).
type gammaResult struct {
	outputText     string
	inputTokens    int
	outputTokens   int
	cacheRead      int
	cacheCreate    int
	turnCount      int
	totalToolCalls int
}

// gammaToolDefs returns the JSON schemas for the 5 retrieval tools the
// LLM may call during γ. Schemas mirror the MCP tool surface so a
// future migration to a real MCP loop stays mechanical.
func gammaToolDefs() []anthropic.ToolUnionParam {
	return []anthropic.ToolUnionParam{
		toolOf("find_symbol",
			"Find symbols by name (exact or partial). Returns matching nodes with file paths.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{"type": "string", "description": "symbol name to find"},
				},
				"required": []string{"name"},
			}),
		toolOf("find_callers",
			"Find functions/methods that call the given symbol. Returns caller nodes.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"qname": map[string]any{"type": "string", "description": "qualified name of the callee"},
				},
				"required": []string{"qname"},
			}),
		toolOf("find_callees",
			"Find functions/methods that the given symbol calls. Returns callee nodes.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"qname": map[string]any{"type": "string", "description": "qualified name of the caller"},
				},
				"required": []string{"qname"},
			}),
		toolOf("get_subgraph",
			"Get the subgraph around a symbol (BFS expansion up to depth).",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"qname": map[string]any{"type": "string"},
					"depth": map[string]any{"type": "integer", "description": "BFS depth (1-3)"},
				},
				"required": []string{"qname"},
			}),
		toolOf("search_text",
			"Full-text search across symbol names, qnames, signatures, docs.",
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
				"required": []string{"query"},
			}),
	}
}

func toolOf(name, desc string, schema map[string]any) anthropic.ToolUnionParam {
	return anthropic.ToolUnionParam{
		OfTool: &anthropic.ToolParam{
			Name:        name,
			Description: anthropic.String(desc),
			InputSchema: anthropic.ToolInputSchemaParam{
				Type:       "object",
				Properties: schema["properties"],
			},
		},
	}
}

// runGammaLoop executes a multi-turn tool-use conversation. Each LLM
// turn either ends with a text answer (stop_reason="end_turn") or
// requests one or more tool calls (stop_reason="tool_use"). Tool calls
// dispatch to the local store; their results feed the next turn.
//
// Returns when the LLM stops requesting tools, or when gammaMaxTurns
// is hit (whichever first). The accumulated input/output tokens cover
// the entire conversation.
func runGammaLoop(ctx context.Context, c *anthropic.Client, model, system, user string,
	store pkgstore.Reader) (gammaResult, error) {
	tools := gammaToolDefs()
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
	}
	var res gammaResult

	for turn := 0; turn < gammaMaxTurns; turn++ {
		res.turnCount = turn + 1

		msg, err := c.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.Model(model),
			MaxTokens: gammaMaxTokens,
			System:    []anthropic.TextBlockParam{{Text: system}},
			Messages:  messages,
			Tools:     tools,
		})
		if err != nil {
			return res, fmt.Errorf("turn %d: %w", turn, err)
		}

		res.inputTokens += int(msg.Usage.InputTokens)
		res.outputTokens += int(msg.Usage.OutputTokens)
		res.cacheRead += int(msg.Usage.CacheReadInputTokens)
		res.cacheCreate += int(msg.Usage.CacheCreationInputTokens)

		// Collect text and tool_use blocks from this response
		var toolUses []anthropic.ToolUseBlock
		var textParts []string
		for _, b := range msg.Content {
			switch b.Type {
			case "text":
				textParts = append(textParts, b.Text)
			case "tool_use":
				toolUses = append(toolUses, anthropic.ToolUseBlock{
					ID:    b.ID,
					Name:  b.Name,
					Input: b.Input,
					Type:  "tool_use",
				})
			}
		}

		if msg.StopReason != "tool_use" || len(toolUses) == 0 {
			res.outputText = strings.Join(textParts, "\n")
			return res, nil
		}

		// Append assistant message with the tool_use blocks
		assistantBlocks := make([]anthropic.ContentBlockParamUnion, 0, len(msg.Content))
		for _, b := range msg.Content {
			switch b.Type {
			case "text":
				assistantBlocks = append(assistantBlocks, anthropic.NewTextBlock(b.Text))
			case "tool_use":
				assistantBlocks = append(assistantBlocks, anthropic.NewToolUseBlock(b.ID, b.Input, b.Name))
			}
		}
		messages = append(messages, anthropic.NewAssistantMessage(assistantBlocks...))

		// Execute each tool, build a user message with tool_result blocks
		resultBlocks := make([]anthropic.ContentBlockParamUnion, 0, len(toolUses))
		for _, tu := range toolUses {
			res.totalToolCalls++
			result := executeGammaTool(store, tu.Name, tu.Input)
			resultBlocks = append(resultBlocks, anthropic.NewToolResultBlock(tu.ID, result, false))
		}
		messages = append(messages, anthropic.NewUserMessage(resultBlocks...))
	}

	res.outputText = "[gamma: max turns reached without final answer]"
	return res, nil
}

// executeGammaTool dispatches one tool call against the local store.
// Returns a string payload that becomes tool_result content. Errors
// are returned as plain text (Anthropic supports is_error=true but the
// V1 keeps it simple — the LLM treats any error message as a signal
// to try a different approach).
func executeGammaTool(store pkgstore.Reader, name string, inputJSON json.RawMessage) string {
	var args map[string]any
	if err := json.Unmarshal(inputJSON, &args); err != nil {
		return fmt.Sprintf("tool input parse error: %v", err)
	}
	switch name {
	case "find_symbol":
		q, _ := args["name"].(string)
		nodes, err := store.FindSymbol(q, false, pkgstore.FindSymbolOptions{})
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return jsonString(nodes)
	case "find_callers":
		qname, _ := args["qname"].(string)
		nodes, _, err := store.NeighborhoodByQname(qname, gammaDepth, true)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return jsonString(nodes)
	case "find_callees":
		qname, _ := args["qname"].(string)
		nodes, _, err := store.NeighborhoodByQname(qname, gammaDepth, false)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return jsonString(nodes)
	case "get_subgraph":
		qname, _ := args["qname"].(string)
		depth := gammaDepth
		if d, ok := args["depth"].(float64); ok && d > 0 {
			depth = int(d)
		}
		nodes, edges, err := store.SubgraphByQname(qname, depth)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return "Nodes:\n" + jsonString(nodes) + "\nEdges:\n" + jsonString(edges)
	case "search_text":
		q, _ := args["query"].(string)
		nodes, err := store.Search(q, gammaSearchLimit)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return jsonString(nodes)
	}
	return fmt.Sprintf("unknown tool: %s", name)
}
