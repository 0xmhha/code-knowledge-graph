package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	pkgstore "github.com/0xmhha/code-knowledge-graph/pkg/store"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// γ tool-use loop — V3 (2026-05-28). Prompt-based pseudo-tool-use with
// size-bounded results.
//
// V2 → V3 changes:
//   - Tool results are summarized (qname/type/file/line) instead of
//     full JSON nodes, so multi-turn prompts don't blow up
//   - Per-tool result caps (top-N) prevent runaway expansion
//   - Empty results get a "no match" hint so the LLM tries a different
//     name instead of repeating the same call
//   - System prompt clarifies final-answer semantics
//   - gammaResult tracks cumulative user-msg bytes for honest H1 measure

const (
	gammaMaxTurns      = 8
	gammaResultLimit   = 20 // top-N nodes per tool result
	gammaEdgesLimit    = 30 // max edges in get_subgraph result
	gammaSearchLimit   = 20
	gammaSubgraphDepth = 2
)

const gammaToolInstructions = `You have access to retrieval tools to explore a code knowledge graph. Use them to gather the information needed to answer the user's question.

Available tools:
- find_symbol(name): Find symbols by exact or partial name.
- find_callers(qname): Find callers of a symbol (use the full qualified name including package).
- find_callees(qname): Find callees of a symbol.
- get_subgraph(qname, depth): BFS expansion around a symbol.
- search_text(query): Full-text search across names, qnames, signatures, docs.

To call tools, emit XML blocks. Each block must be exactly:

<tool_call name="TOOL_NAME">
{"arg": "value"}
</tool_call>

You may call multiple tools per turn — all of them execute in parallel and the results appear in the next turn. Argument JSON must be a single-line object; no code fences.

FINAL ANSWER PROTOCOL: When you have enough information, respond with your final answer as plain text containing NO <tool_call> tags. The absence of any <tool_call> tag is the signal that you are done. Cite specific symbols and file:line locations.

Tips:
- If a tool returns an empty result, try a different name (e.g. with or without the package prefix).
- Be efficient — each tool call costs tokens and time. Plan your queries before calling.`

// runGammaPromptLoop is the entry point γ uses for both LLM backends.
// It piggybacks on llm.Complete (single-turn text completion) and
// implements the multi-turn loop on top via prompt-based tool calls.
func runGammaPromptLoop(ctx context.Context, llm LLMClient, system, user string,
	store pkgstore.Reader) (LLMResult, error) {

	fullSystem := system + "\n\n" + gammaToolInstructions
	userMsg := user

	var totalInput, totalOutput, totalCacheRead, totalCacheCreate int
	var totalToolCalls int
	var maxUserBytes int // peak cumulative user-msg size across turns
	var lastOutput string

	for turn := 0; turn < gammaMaxTurns; turn++ {
		if len(userMsg) > maxUserBytes {
			maxUserBytes = len(userMsg)
		}

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
			return LLMResult{
				OutputText:        res.OutputText,
				InputTokens:       totalInput,
				OutputTokens:      totalOutput,
				CacheReadTokens:   totalCacheRead,
				CacheCreateTokens: totalCacheCreate,
				NumToolCalls:      totalToolCalls,
				UserPromptBytes:   maxUserBytes,
			}, nil
		}

		var results strings.Builder
		results.WriteString("\n\n--- tool results ---\n")
		for _, c := range calls {
			totalToolCalls++
			out := executeGammaTool(store, c.Name, c.Args)
			fmt.Fprintf(&results, "<tool_result name=%q>\n%s\n</tool_result>\n", c.Name, out)
		}
		userMsg = userMsg + "\n\n--- assistant ---\n" + res.OutputText + results.String()
	}

	if len(userMsg) > maxUserBytes {
		maxUserBytes = len(userMsg)
	}
	return LLMResult{
		OutputText:        lastOutput + "\n[gamma: max turns reached]",
		InputTokens:       totalInput,
		OutputTokens:      totalOutput,
		CacheReadTokens:   totalCacheRead,
		CacheCreateTokens: totalCacheCreate,
		NumToolCalls:      totalToolCalls,
		UserPromptBytes:   maxUserBytes,
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

// nodeBrief is the compact node form used inside tool_result blocks.
// Keeps the LLM-visible payload small so multi-turn prompts don't blow
// up after a few hops. Sufficient for the LLM to decide whether to
// drill further or finalize an answer.
type nodeBrief struct {
	Qname string `json:"qname"`
	Type  string `json:"type"`
	File  string `json:"file,omitempty"`
	Line  int    `json:"line,omitempty"`
	Sig   string `json:"sig,omitempty"`
}

func briefNodes(ns []types.Node, limit int) []nodeBrief {
	if len(ns) > limit {
		ns = ns[:limit]
	}
	out := make([]nodeBrief, 0, len(ns))
	for _, n := range ns {
		nb := nodeBrief{
			Qname: n.QualifiedName,
			Type:  string(n.Type),
			File:  n.FilePath,
			Line:  n.StartLine,
		}
		if len(n.Signature) > 120 {
			nb.Sig = n.Signature[:120] + "…"
		} else {
			nb.Sig = n.Signature
		}
		out = append(out, nb)
	}
	return out
}

type edgeBrief struct {
	Src  string `json:"src"`
	Dst  string `json:"dst"`
	Type string `json:"type"`
}

func briefEdges(es []types.Edge, limit int) []edgeBrief {
	if len(es) > limit {
		es = es[:limit]
	}
	out := make([]edgeBrief, 0, len(es))
	for _, e := range es {
		out = append(out, edgeBrief{
			Src:  e.Src,
			Dst:  e.Dst,
			Type: string(e.Type),
		})
	}
	return out
}

// formatToolResult wraps a result with an empty-result hint when needed.
func formatToolResult(nodes []nodeBrief, originalCount int, query string) string {
	if len(nodes) == 0 {
		return fmt.Sprintf("no matches for %q. Try a different name (with/without package prefix) or use search_text for keyword search.", query)
	}
	payload := jsonString(nodes)
	if originalCount > len(nodes) {
		return fmt.Sprintf("(showing top %d of %d results)\n%s", len(nodes), originalCount, payload)
	}
	return payload
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
		return formatToolResult(briefNodes(nodes, gammaResultLimit), len(nodes), q)

	case "find_callers":
		qname, _ := args["qname"].(string)
		nodes, _, err := store.NeighborhoodByQname(qname, gammaSubgraphDepth, true)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return formatToolResult(briefNodes(nodes, gammaResultLimit), len(nodes), qname)

	case "find_callees":
		qname, _ := args["qname"].(string)
		nodes, _, err := store.NeighborhoodByQname(qname, gammaSubgraphDepth, false)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return formatToolResult(briefNodes(nodes, gammaResultLimit), len(nodes), qname)

	case "get_subgraph":
		qname, _ := args["qname"].(string)
		depth := gammaSubgraphDepth
		if d, ok := args["depth"].(float64); ok && d > 0 {
			depth = int(d)
		}
		nodes, edges, err := store.SubgraphByQname(qname, depth)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		if len(nodes) == 0 {
			return formatToolResult(nil, 0, qname)
		}
		return fmt.Sprintf("Nodes (top %d of %d):\n%s\nEdges (top %d of %d):\n%s",
			min(len(nodes), gammaResultLimit), len(nodes),
			jsonString(briefNodes(nodes, gammaResultLimit)),
			min(len(edges), gammaEdgesLimit), len(edges),
			jsonString(briefEdges(edges, gammaEdgesLimit)))

	case "search_text":
		q, _ := args["query"].(string)
		nodes, err := store.Search(q, gammaSearchLimit)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return formatToolResult(briefNodes(nodes, gammaResultLimit), len(nodes), q)
	}
	return fmt.Sprintf("unknown tool: %s", name)
}
