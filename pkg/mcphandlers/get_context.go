package mcphandlers

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/pkg/smartctx"
	"github.com/0xmhha/code-knowledge-graph/pkg/store"
)

// RegisterGetContextForTask wires the smart 1-shot retrieval tool:
// BM25 retrieve -> 1-hop expand -> score-fuse -> diversify -> pack
// within token budget. The body lives in pkg/smartctx so the eval
// δ baseline measures exactly the same algorithm the LLM consumer
// runs at request time.
func RegisterGetContextForTask(s *server.MCPServer, reader store.Reader) {
	tool := mcp.NewTool("get_context_for_task",
		mcp.WithDescription("Smart 1-shot retrieval: BM25 -> 1-hop expand -> score -> diversify -> pack."),
		mcp.WithString("task_description", mcp.Required()),
		mcp.WithNumber("budget_tokens", mcp.DefaultNumber(8000)),
		mcp.WithString("language"),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(true)),
		mcp.WithNumber("max_bodies", mcp.DefaultNumber(5)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		desc := req.GetString("task_description", "")
		opt := smartctx.Options{
			BudgetTokens: int(req.GetFloat("budget_tokens", 8000)),
			IncludeBlobs: req.GetBool("include_blobs", true),
			MaxBodies:    int(req.GetFloat("max_bodies", 5)),
		}
		out, err := smartctx.BuildContext(reader, desc, opt)
		if err != nil {
			return nil, err
		}
		return textResult(out), nil
	})
}
