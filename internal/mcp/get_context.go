package mcp

import (
	"context"
	"sort"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// scoredNode is the per-candidate row produced by buildContext after fusing
// BM25 rank, PageRank and usage score. Defined at file level (rather than as
// an anonymous struct inside buildContext) so rowsToNodeRefs has a clean
// parameter type.
type scoredNode struct {
	n     types.Node
	score float64
}

// registerGetContextForTask is the single "smart" tool: BM25 retrieve → 1-hop
// expand → score-fuse → diversify (V0: simple cap) → pack within token budget.
func registerGetContextForTask(s *server.MCPServer, store *persist.Store) {
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
		budget := int(req.GetFloat("budget_tokens", 8000))
		incl := req.GetBool("include_blobs", true)
		maxBodies := int(req.GetFloat("max_bodies", 5))
		out, err := buildContext(store, desc, budget, incl, maxBodies)
		if err != nil {
			return nil, err
		}
		return textResult(out), nil
	})
}

// buildContext is the algorithm body, separated from the tool registration so
// it can be unit-tested without spinning up the MCP server.
//
// Steps:
//   (a) Retrieve  — BM25 top 30 via FTS5
//   (b) Expand    — 1-hop neighbours via QueryEdgesForNodes
//   (c) Score     — 0.5 * BM25_rank_norm + 0.3 * PageRank_norm + 0.2 * Usage_norm
//   (d) Diversify — V0: simple cap of top-30 (no per-cluster cap)
//   (e) Pack      — top maxBodies get full source; next ≤15 get sig+doc summary
func buildContext(store *persist.Store, query string, budget int, includeBlobs bool, maxBodies int) (map[string]any, error) {
	// (a) Retrieve: BM25 top 30
	cands, err := store.SearchFTS(query, 30)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return map[string]any{
			"task_description": query,
			"subgraph":         nil,
			"bodies":           nil,
			"summaries":        nil,
			"tokens_estimated": estimateTokens(query),
			"trimmed":          false,
			"not_found":        true,
		}, nil
	}
	// (b) Expand: 1-hop traversal
	ids := make([]string, 0, len(cands))
	for _, n := range cands {
		ids = append(ids, n.ID)
	}
	moreEdges, _ := store.QueryEdgesForNodes(ids)
	expIDs := map[string]struct{}{}
	for _, e := range moreEdges {
		expIDs[e.Src] = struct{}{}
		expIDs[e.Dst] = struct{}{}
	}
	for _, id := range ids {
		expIDs[id] = struct{}{}
	}
	expanded, _ := store.NodesByIDs(setKeys(expIDs))

	// (c) Score: 0.5 BM25 + 0.3 PageRank + 0.2 usage. BM25 is approximated by
	// candidate rank reciprocal — full BM25 ranks are hidden by the FTS5 API
	// surface we expose, but rank order is preserved.
	bm25Rank := map[string]float64{}
	for i, n := range cands {
		bm25Rank[n.ID] = 1.0 / float64(i+1)
	}
	// 1e-9 floor avoids divide-by-zero when no node has nonzero PR or usage.
	maxPR, maxUS := 1e-9, 1e-9
	for _, n := range expanded {
		if n.PageRank > maxPR {
			maxPR = n.PageRank
		}
		if n.UsageScore > maxUS {
			maxUS = n.UsageScore
		}
	}
	rows := make([]scoredNode, 0, len(expanded))
	for _, n := range expanded {
		s := 0.5*bm25Rank[n.ID] + 0.3*(n.PageRank/maxPR) + 0.2*(n.UsageScore/maxUS)
		rows = append(rows, scoredNode{n: n, score: s})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].score > rows[j].score })

	// (d) Diversify (V0: simple cap of top-30, no per-cluster cap)
	if len(rows) > 30 {
		rows = rows[:30]
	}

	// (e) Pack within budget: top maxBodies get blobs, next ≤15 get summaries
	bodies := []map[string]any{}
	summaries := []map[string]any{}
	tokens := estimateTokens(query)
	for i, r := range rows {
		if i < maxBodies && includeBlobs {
			b, err := store.GetBlob(r.n.ID)
			if err == nil {
				cost := estimateTokens(string(b))
				if tokens+cost > budget {
					break
				}
				bodies = append(bodies, map[string]any{
					"id":     r.n.ID,
					"qname":  r.n.QualifiedName,
					"source": string(b),
				})
				tokens += cost
				continue
			}
		}
		if len(summaries) >= 15 {
			continue
		}
		summary := map[string]any{
			"id":        r.n.ID,
			"qname":     r.n.QualifiedName,
			"signature": r.n.Signature,
			"doc":       r.n.DocComment,
		}
		cost := estimateTokens(r.n.Signature + " " + r.n.DocComment)
		if tokens+cost > budget {
			continue
		}
		summaries = append(summaries, summary)
		tokens += cost
	}

	// Subgraph adjacency (compact triples), filtered to surviving nodes.
	keptIDs := map[string]struct{}{}
	for _, r := range rows {
		keptIDs[r.n.ID] = struct{}{}
	}
	adj := [][]string{}
	for _, e := range moreEdges {
		if _, ok := keptIDs[e.Src]; !ok {
			continue
		}
		if _, ok := keptIDs[e.Dst]; !ok {
			continue
		}
		adj = append(adj, []string{e.Src, e.Dst, string(e.Type)})
	}

	return map[string]any{
		"task_description": query,
		"subgraph": map[string]any{
			"nodes": rowsToNodeRefs(rows),
			"edges": adj,
		},
		"bodies":           bodies,
		"summaries":        summaries,
		"tokens_estimated": tokens,
		"trimmed":          tokens >= budget,
	}, nil
}

func rowsToNodeRefs(rows []scoredNode) []map[string]any {
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		out[i] = map[string]any{
			"id":    r.n.ID,
			"name":  r.n.Name,
			"type":  r.n.Type,
			"qname": r.n.QualifiedName,
			"score": r.score,
		}
	}
	return out
}

// estimateTokens is the standard chars/4 heuristic. Used by buildContext to
// approximate token cost without round-tripping to a tokenizer.
func estimateTokens(s string) int { return (len(s) + 3) / 4 }

// setKeys returns the keys of a map[string]V as a slice. Local to the mcp
// package so we don't widen persist's exported surface for one call site.
func setKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
