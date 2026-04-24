// Package mcp wires CKG's read-only SQLite store to the Model Context
// Protocol via stdio. All six tools (spec §8.3) share the same Store.
package mcp

import (
	"context"
	"fmt"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// Run starts a stdio MCP server bound to store. Returns when stdin closes.
func Run(ctx context.Context, store *persist.Store) error {
	s := server.NewMCPServer("ckg", "0.1.0")

	registerFindSymbol(s, store)
	registerFindCallers(s, store)
	registerFindCallees(s, store)
	registerGetSubgraph(s, store)
	registerSearchText(s, store)
	registerGetContextForTask(s, store)

	if err := server.ServeStdio(s); err != nil {
		return fmt.Errorf("mcp serve stdio: %w", err)
	}
	return nil
}

// helper for tool result envelope
func textResult(payload any) *mcp.CallToolResult {
	return mcp.NewToolResultStructured(payload, "")
}
