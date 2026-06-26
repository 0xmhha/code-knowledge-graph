package mcphandlers

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/pkg/concurrency"
	"github.com/0xmhha/code-knowledge-graph/pkg/store"
)

// RegisterConcurrencyImpact wires the concurrency_impact tool — the
// concurrency blast radius of a symbol over the five contract edge types
// (spawns, sends_to, recvs_from, acquires_lock, accessed_under_lock;
// R1' contract C1, S1). The algorithm body lives in pkg/concurrency so cks
// consumes it in-process; this file is the MCP request envelope only (the
// dev-only ckg server, 00 §7), mirroring RegisterImpactOfChange.
func RegisterConcurrencyImpact(s *server.MCPServer, reader store.Reader) {
	reader = safeReader(reader) // enforce the §11.3 H3 boundary regardless of caller
	tool := mcp.NewTool("concurrency_impact",
		mcp.WithDescription(
			"Concurrency blast radius for a symbol: modules that affect or are affected by it via "+
				"goroutine/channel/lock edges (spawns, sends_to, recvs_from, acquires_lock, accessed_under_lock). "+
				"Seed a Channel/Mutex/Field node to recover producer<->consumer or lock-sharing peers; a function "+
				"seed reaches its own goroutine and the channel it writes, but not the peer across the channel.",
		),
		mcp.WithString("seed_qname"),
		mcp.WithNumber("depth", mcp.DefaultNumber(2)),
		mcp.WithNumber("max_total", mcp.DefaultNumber(0)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		out, err := concurrency.Analyze(reader, req.GetString("seed_qname", ""), concurrency.Options{
			Depth:    int(req.GetFloat("depth", 2)),
			MaxTotal: int(req.GetFloat("max_total", 0)),
		})
		if err != nil {
			return nil, err
		}
		return textResult(out), nil
	})
}
