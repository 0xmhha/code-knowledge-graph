package mcphandlers

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/pkg/impact"
	"github.com/0xmhha/code-knowledge-graph/pkg/store"
)

// RegisterImpactOfChange wires the impact_of_change tool. Either
// seed_qname or seed_file must be set; if both are set, seed_qname
// wins (less ambiguous, and the qname path returns a single seed
// node for the response envelope).
//
// The algorithm body lives in pkg/impact so the same code path is
// shared with internal/server's HTTP /api/impact handler — mirrors
// the smartctx pattern (pkg/smartctx is shared by
// get_context_for_task and the eval δ baseline). This file is the
// MCP request envelope only.
func RegisterImpactOfChange(s *server.MCPServer, reader store.Reader) {
	reader = safeReader(reader) // enforce the §11.3 H3 boundary regardless of caller
	tool := mcp.NewTool("impact_of_change",
		mcp.WithDescription(
			"Reverse-dependency closure for a symbol or file. Returns nodes/edges grouped by impact category "+
				"(callers, interface_impact, type_users, distributed, concurrent, other_refs). "+
				"If results look empty for a Go concrete-method seed, retry with the interface method qname "+
				"(Go's call graph binds invocations to the interface, not the concrete receiver).",
		),
		mcp.WithString("seed_qname"),
		mcp.WithString("seed_file"),
		mcp.WithNumber("depth", mcp.DefaultNumber(2)),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		seedQ := req.GetString("seed_qname", "")
		seedF := req.GetString("seed_file", "")
		out, err := impact.Compute(reader, seedQ, seedF, impact.Options{
			Depth:        int(req.GetFloat("depth", 2)),
			IncludeBlobs: req.GetBool("include_blobs", false),
		})
		if err != nil {
			return nil, err
		}
		return textResult(out), nil
	})
}
