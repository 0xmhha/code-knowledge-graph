// Package mcp wires CKG's read-only SQLite store to the Model Context
// Protocol via stdio. The toolbox is now seven tools: the original six
// (spec §8.3) plus impact_of_change (P1a, dogfood plan) — all sharing
// the same Store instance.
package mcp

import (
	"context"
	"fmt"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// Run starts a stdio MCP server bound to store. Returns when stdin closes.
//
// The store is wrapped in newLLMSafeStoreReader so every read surface a
// tool touches has the §11.3 H3 retrieval boundary applied — AMBIGUOUS
// Hunk/Commit nodes (the unreachable-history track) never leak to LLM
// consumers regardless of which tool the agent calls.
func Run(ctx context.Context, store persist.StoreReader) error {
	s := server.NewMCPServer("ckg", "0.1.0")

	safe := newLLMSafeStoreReader(store)
	registerFindSymbol(s, safe)
	registerFindCallers(s, safe)
	registerFindCallees(s, safe)
	registerGetSubgraph(s, safe)
	registerSearchText(s, safe)
	registerGetContextForTask(s, safe)
	registerImpactOfChange(s, safe)

	if err := server.ServeStdio(s); err != nil {
		return fmt.Errorf("mcp serve stdio: %w", err)
	}
	return nil
}

// helper for tool result envelope
func textResult(payload any) *mcp.CallToolResult {
	return mcp.NewToolResultStructured(payload, "")
}
