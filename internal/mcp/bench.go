// Package mcp — bench.go exposes the registered tool handlers for
// in-process latency measurement. Production stdio serving still
// goes through Run; this entrypoint exists so cmd/ckg/bench-mcp can
// invoke each handler directly (no subprocess spawn, no JSON-RPC
// framing) and surface the graph layer's contribution to MCP tool
// latency separately from the stdio transport's.
//
// The §11.3 retrieval boundary is preserved: mcphandlers.RegisterAll
// wraps the store before threading. AMBIGUOUS Hunk/Commit nodes never
// reach the bench callers.
package mcp

import (
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/mcphandlers"
)

// BenchToolNames is the canonical ordering of the eight tools, used
// by cmd/ckg/bench-mcp so probe output stays stable across runs.
var BenchToolNames = []string{
	"find_symbol",
	"find_callers",
	"find_callees",
	"get_subgraph",
	"search_text",
	"get_context_for_task",
	"impact_of_change",
	"evidence_for_intent",
}

// NewBenchHandlers builds a fresh MCPServer with all eight tools
// registered through the safe wrapper, then returns the handler map
// keyed by tool name. The MCPServer is returned alongside so the
// caller can cleanly tear down (the current mcp-go API doesn't need
// explicit close, but holding the reference avoids GC pressure on
// long-running benches).
func NewBenchHandlers(store persist.StoreReader) (*server.MCPServer, map[string]server.ToolHandlerFunc) {
	s := server.NewMCPServer("ckg-bench", "0.0.0")
	mcphandlers.RegisterAll(s, store)
	handlers := make(map[string]server.ToolHandlerFunc, len(BenchToolNames))
	for _, name := range BenchToolNames {
		t := s.GetTool(name)
		if t == nil || t.Handler == nil {
			continue
		}
		handlers[name] = t.Handler
	}
	return s, handlers
}
