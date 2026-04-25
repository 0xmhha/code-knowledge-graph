package eval

// Baseline determines what tools the LLM may call and how raw context is
// supplied (α only). See spec §9.1.
type Baseline string

const (
	BaselineAlpha Baseline = "alpha" // raw file dump, no tools
	BaselineBeta  Baseline = "beta"  // get_subgraph(root, depth=99), 1 tool
	BaselineGamma Baseline = "gamma" // 5 granular tools
	BaselineDelta Baseline = "delta" // get_context_for_task only (smart)
)

// AllowedTools maps a baseline to the set of MCP tool names the LLM may call.
// α returns nil (no tools).
func AllowedTools(b Baseline) []string {
	switch b {
	case BaselineAlpha:
		return nil
	case BaselineBeta:
		return []string{"get_subgraph"}
	case BaselineGamma:
		return []string{"find_symbol", "find_callers", "find_callees", "get_subgraph", "search_text"}
	case BaselineDelta:
		return []string{"get_context_for_task"}
	}
	return nil
}

// SystemPrompt returns the system prompt fragment that primes the LLM about
// what's available. α also receives raw file dumps appended to user content.
func SystemPrompt(b Baseline) string {
	switch b {
	case BaselineAlpha:
		return "You are a coding assistant. Raw source files are appended below the task description. Use them to answer."
	case BaselineBeta:
		return "You are a coding assistant. Call get_subgraph once to retrieve the entire graph, then answer."
	case BaselineGamma:
		return "You are a coding assistant. Use find_symbol/find_callers/find_callees/get_subgraph/search_text as needed to gather context, then answer."
	case BaselineDelta:
		return "You are a coding assistant. Call get_context_for_task ONCE with the user's task description, then answer."
	}
	return ""
}
