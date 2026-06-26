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
//
// 1-shot enrichment (P0 #2, docs/PROJECT-BLUEPRINT-ALIGNMENT.md
// §4.2): opt-in flags include_recent_prs and include_impact fold
// the PR breadcrumb and reverse-deps closures into the same
// response, so a coding-agent can answer "what is this, why did
// it change, what depends on it?" in a single tool call.
func RegisterGetContextForTask(s *server.MCPServer, reader store.Reader) {
	reader = safeReader(reader) // enforce the §11.3 H3 boundary regardless of caller
	tool := mcp.NewTool("get_context_for_task",
		mcp.WithDescription("Smart 1-shot retrieval: BM25 -> 1-hop expand -> score -> diversify -> pack. "+
			"Optionally folds PR breadcrumbs (include_recent_prs) and reverse-dep impact "+
			"(include_impact) into the same response so coding agents avoid extra round-trips."),
		mcp.WithString("task_description", mcp.Required()),
		mcp.WithNumber("budget_tokens", mcp.DefaultNumber(8000)),
		mcp.WithString("language"),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(true)),
		mcp.WithNumber("max_bodies", mcp.DefaultNumber(5)),
		mcp.WithBoolean("include_recent_prs", mcp.DefaultBool(false)),
		mcp.WithNumber("prs_per_node", mcp.DefaultNumber(3)),
		mcp.WithBoolean("include_impact", mcp.DefaultBool(false)),
		mcp.WithNumber("impact_depth", mcp.DefaultNumber(1)),
		mcp.WithNumber("hop_depth", mcp.DefaultNumber(1)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		desc := req.GetString("task_description", "")
		opt := smartctx.Options{
			BudgetTokens:  int(req.GetFloat("budget_tokens", 8000)),
			IncludeBlobs:  req.GetBool("include_blobs", true),
			MaxBodies:     int(req.GetFloat("max_bodies", 5)),
			IncludePRs:    req.GetBool("include_recent_prs", false),
			PRsPerNode:    int(req.GetFloat("prs_per_node", 3)),
			IncludeImpact: req.GetBool("include_impact", false),
			ImpactDepth:   int(req.GetFloat("impact_depth", 1)),
			HopDepth:      int(req.GetFloat("hop_depth", 1)),
		}
		out, err := smartctx.BuildContext(reader, desc, opt)
		if err != nil {
			return nil, err
		}
		return textResult(out), nil
	})
}
