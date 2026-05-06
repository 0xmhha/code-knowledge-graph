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
func Run(ctx context.Context, store persist.StoreReader) error {
	s := server.NewMCPServer("ckg", "0.1.0")

	registerFindSymbol(s, store)
	registerFindCallers(s, store)
	registerFindCallees(s, store)
	registerGetSubgraph(s, store)
	registerSearchText(s, store)
	registerGetContextForTask(s, store)
	registerImpactOfChange(s, store)

	if err := server.ServeStdio(s); err != nil {
		return fmt.Errorf("mcp serve stdio: %w", err)
	}
	return nil
}

// helper for tool result envelope
func textResult(payload any) *mcp.CallToolResult {
	return mcp.NewToolResultStructured(payload, "")
}
