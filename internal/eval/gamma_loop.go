package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	pkgstore "github.com/0xmhha/code-knowledge-graph/pkg/store"
)

// γ tool-use loop — V2 (2026-05-28). Prompt-based pseudo-tool-use:
// the LLM is instructed to emit <tool_call name="...">{json}</tool_call>
// blocks; the runner parses these, executes each call against the
// local pkgstore.Reader, and feeds the results back as tool_result
// markup in the next user turn. Loops until the LLM responds without
// any tool_call blocks (final answer) or gammaMaxTurns is hit.
//
// V2 replaces V1's native Anthropic tool_use protocol so γ works with
// any LLM backend, including the Claude CLI subprocess.

const (
	gammaMaxTurns    = 8
	gammaSearchLimit = 20
	gammaDepth       = 2
)

const gammaToolInstructions = `You have access to retrieval tools to explore a code knowledge graph. Use them to gather the information needed to answer the user's question.

Available tools:
- find_symbol(name): Find symbols by exact or partial name. Returns matching nodes with file paths.
- find_callers(qname): Find callers of a symbol. Returns caller nodes.
- find_callees(qname): Find callees of a symbol. Returns callee nodes.
- get_subgraph(qname, depth): BFS expansion around a symbol. Returns nodes + edges.
- search_text(query): Full-text search across names, qnames, signatures, docs.

To call one or more tools, emit XML blocks in your response. Each block must use this exact format:

<tool_call name="TOOL_NAME">
{"arg": "value"}
</tool_call>

Multiple tool_call blocks per turn are allowed; they are all executed and the results are appended to the next user turn. Argument JSON must be on a single line or properly formatted; do not include code fences.

When you have enough information, respond with your final answer in plain text — no <tool_call> tags. Reference specific symbols and file:line locations.`

// runGammaPromptLoop is the entry point γ uses for both LLM backends.
// It piggybacks on llm.Complete (single-turn text completion) and
// implements the multi-turn loop on top via prompt-based tool calls.
func runGammaPromptLoop(ctx context.Context, llm LLMClient, system, user string,
	store pkgstore.Reader) (LLMResult, error) {

	fullSystem := system + "\n\n" + gammaToolInstructions

	// userMsg accumulates the original task + each turn's tool results
	// so the LLM sees the full conversation history on every Complete.
	userMsg := user

	var totalInput, totalOutput, totalCacheRead, totalCacheCreate int
	var totalToolCalls int
	var lastOutput string

	for turn := 0; turn < gammaMaxTurns; turn++ {
		res, err := llm.Complete(ctx, fullSystem, userMsg)
		if err != nil {
			return LLMResult{}, fmt.Errorf("turn %d: %w", turn, err)
		}

		totalInput += res.InputTokens
		totalOutput += res.OutputTokens
		totalCacheRead += res.CacheReadTokens
		totalCacheCreate += res.CacheCreateTokens
		lastOutput = res.OutputText

		calls := parseToolCalls(res.OutputText)
		if len(calls) == 0 {
			// Final answer reached.
			return LLMResult{
				OutputText:        res.OutputText,
				InputTokens:       totalInput,
				OutputTokens:      totalOutput,
				CacheReadTokens:   totalCacheRead,
				CacheCreateTokens: totalCacheCreate,
				NumToolCalls:      totalToolCalls,
			}, nil
		}

		// Execute every parsed call, build a tool_result blob.
		var results strings.Builder
		results.WriteString("\n\n--- tool results ---\n")
		for _, c := range calls {
			totalToolCalls++
			out := executeGammaTool(store, c.Name, c.Args)
			fmt.Fprintf(&results, "<tool_result name=%q>\n%s\n</tool_result>\n", c.Name, out)
		}

		// Append the assistant's tool_call output and the tool_result
		// blob to userMsg so the next Complete sees the full history.
		userMsg = userMsg + "\n\n--- assistant ---\n" + res.OutputText + results.String()
	}

	// Loop exited without a clean final answer.
	return LLMResult{
		OutputText:        lastOutput + "\n[gamma: max turns reached]",
		InputTokens:       totalInput,
		OutputTokens:      totalOutput,
		CacheReadTokens:   totalCacheRead,
		CacheCreateTokens: totalCacheCreate,
		NumToolCalls:      totalToolCalls,
	}, nil
}

// gammaCall is one parsed tool invocation from the LLM's response.
type gammaCall struct {
	Name string
	Args json.RawMessage
}

var (
	gammaOpenRe = regexp.MustCompile(`<tool_call\s+name="([^"]+)"\s*>`)
	gammaArgsRe = regexp.MustCompile(`(?s)\{.*?\}`)
)

// parseToolCalls extracts every <tool_call name="...">...</tool_call>
// block from the LLM response. Bodies are searched for the first JSON
// object; malformed blocks are skipped silently.
func parseToolCalls(text string) []gammaCall {
	var calls []gammaCall
	rest := text
	for {
		open := gammaOpenRe.FindStringSubmatchIndex(rest)
		if open == nil {
			break
		}
		name := rest[open[2]:open[3]]
		blockStart := open[1]
		closeIdx := strings.Index(rest[blockStart:], "</tool_call>")
		if closeIdx < 0 {
			break
		}
		body := rest[blockStart : blockStart+closeIdx]
		args := gammaArgsRe.FindString(body)
		if args == "" {
			args = "{}"
		}
		calls = append(calls, gammaCall{Name: name, Args: json.RawMessage(args)})
		rest = rest[blockStart+closeIdx+len("</tool_call>"):]
	}
	return calls
}

// executeGammaTool dispatches one tool call against the local store.
// Returns a string payload that becomes tool_result content. Errors
// are returned as plain text so the LLM can react and try again.
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
