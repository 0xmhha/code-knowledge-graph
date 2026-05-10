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
		mcp.WithString("intent",
			mcp.Description("Free-text task description. Tokenised with the bm25 splitter (camelCase + snake_case + qname-aware). Required unless issue_id is set.")),
		mcp.WithString("seed_qname",
			mcp.Description("Optional. Restrict to hunks whose modifies edges reach this CodeNode or its callers/callees (1-hop).")),
		mcp.WithString("issue_id",
			mcp.Description("Optional. Restrict to hunks whose parent commit cites this H4-extracted ticket (e.g. GH-42, INGEST-789). Use alone to browse a ticket's full footprint sorted by recency, or combine with intent for BM25-rank within the ticket subset.")),
		mcp.WithNumber("k", mcp.DefaultNumber(5),
			mcp.Description("Top-K commits to return. Each commit may contain multiple hunks (the adjacent edge means the Agent reads the full change).")),
		mcp.WithNumber("budget_tokens", mcp.DefaultNumber(6000),
			mcp.Description("Stop emitting commits once cumulative patch text exceeds this many tokens (4 chars/token approx).")),
		mcp.WithNumber("offset", mcp.DefaultNumber(0),
			mcp.Description("Skip the first N commits in the recency-sorted result. Used for paging through a large ticket without raising budget_tokens; pair with `k` to walk back through history page by page.")),
		mcp.WithString("mode",
			mcp.Description("Term-match strategy applied on top of BM25. 'or' (default) keeps BM25's any-term-match — fuzzy semantic search. 'and' drops hits whose virtual document is missing any query token — useful for precise queries like \"hunks mentioning RetryPolicy AND Backoff\".")),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		opt := evidence.Options{
			Intent:       req.GetString("intent", ""),
			SeedQname:    req.GetString("seed_qname", ""),
			IssueID:      req.GetString("issue_id", ""),
			K:            int(req.GetFloat("k", 5)),
			BudgetTokens: int(req.GetFloat("budget_tokens", 6000)),
			Offset:       int(req.GetFloat("offset", 0)),
			Mode:         req.GetString("mode", ""),
		}
		pack, err := cache.BuildPack(store, opt)
		if err != nil {
			return nil, err
		}
		return textResult(pack), nil
	})
}
