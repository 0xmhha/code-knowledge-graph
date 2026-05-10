// Package mcp — evidence.go registers the H3 evidence_for_intent tool
// (docs/design/hunk-graph.md §5). Returns an EvidencePack JSON for an
// intent string + optional seed_qname, ranked by BM25 over the
// (commit subject || patch || modifies-qnames) virtual document and
// grouped by parent commit.
//
// The §11.3 retrieval boundary is enforced two ways:
//
//   - This tool is registered against the llmSafeStoreReader (server.go
//     wraps before threading), so AllNodes/AllEdges/GetBlob filter
//     AMBIGUOUS Hunks/Commits at the read layer.
//   - pkg/evidence.indexCorpus also filters confidence='EXTRACTED' as
//     defense in depth — even if a future change accidentally bypasses
//     the wrapper, the EvidencePack assembler still hides the
//     unreachable-history track from LLM consumers.
package mcp

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/evidence"
)

// registerEvidenceForIntent adds the evidence_for_intent tool. Schema
// matches design §5.1; defaults match the package constants.
//
// The cache parameter is shared across the lifetime of one MCP Run
// invocation so the BM25 corpus is built once and reused — sub-second
// query latency on graphs that take ~4s for a cold rebuild. The cache
// invalidates itself when the underlying graph.db's manifest drifts
// (a `ckg build` while the server is running).
func registerEvidenceForIntent(s *server.MCPServer, store persist.StoreReader, cache *evidence.Cache) {
	tool := mcp.NewTool("evidence_for_intent",
		mcp.WithDescription("EvidencePack: BM25-rank past commit hunks against an intent, return top-K with patches + modifies neighbours. Filters AMBIGUOUS unreachable-history per §11.3."),
		mcp.WithString("intent", mcp.Required(),
			mcp.Description("Free-text task description. Tokenised with the bm25 splitter (camelCase + snake_case + qname-aware).")),
		mcp.WithString("seed_qname",
			mcp.Description("Optional. Restrict to hunks whose modifies edges reach this CodeNode or its callers/callees (1-hop).")),
		mcp.WithNumber("k", mcp.DefaultNumber(5),
			mcp.Description("Top-K commits to return. Each commit may contain multiple hunks (the adjacent edge means the Agent reads the full change).")),
		mcp.WithNumber("budget_tokens", mcp.DefaultNumber(6000),
			mcp.Description("Stop emitting commits once cumulative patch text exceeds this many tokens (4 chars/token approx).")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		opt := evidence.Options{
			Intent:       req.GetString("intent", ""),
			SeedQname:    req.GetString("seed_qname", ""),
			K:            int(req.GetFloat("k", 5)),
			BudgetTokens: int(req.GetFloat("budget_tokens", 6000)),
		}
		pack, err := cache.BuildPack(store, opt)
		if err != nil {
			return nil, err
		}
		return textResult(pack), nil
	})
}
