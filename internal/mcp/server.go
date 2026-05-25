// Package mcp wires CKG's read-only SQLite store to the Model Context
// Protocol via stdio. The eight tool handlers themselves live in
// pkg/mcphandlers — this package now only carries the stdio entry
// point (Run) and the bench-mode handler exposure used by
// cmd/ckg/bench-mcp.
package mcp

import (
	"context"
	"fmt"

	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/mcphandlers"
)

// Run starts a stdio MCP server bound to store. Returns when stdin
// closes. mcphandlers.RegisterAll wraps store with the §11.3 H3
// safety filter and registers every tool — no per-tool wiring here
// any more (the eight register* calls moved to pkg/mcphandlers).
func Run(ctx context.Context, store persist.StoreReader) error {
	s := server.NewMCPServer("ckg", "0.1.0")
	mcphandlers.RegisterAll(s, store)
	if err := server.ServeStdio(s); err != nil {
		return fmt.Errorf("mcp serve stdio: %w", err)
	}
	return nil
}
