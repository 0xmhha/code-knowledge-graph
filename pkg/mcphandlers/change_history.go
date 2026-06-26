package mcphandlers

import (
	"context"
	"sort"
	"time"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/pkg/store"
)

const defaultChangeHistoryK = 20

// RegisterChangeHistory wires the change_history tool: the merged pull
// requests that touched a symbol (number, title, summary, merged-at), newest
// first. It exposes the graph's existing per-node PR breadcrumbs
// (Reader.GetNodePRs) over MCP so an agent can answer "when/why did this code
// change" from a single HEAD graph — no pre-fix re-index or git fallback.
func RegisterChangeHistory(s *server.MCPServer, reader store.Reader) {
	reader = safeReader(reader) // enforce the §11.3 H3 boundary regardless of caller
	tool := mcp.NewTool("change_history",
		mcp.WithDescription(
			"PR history for a symbol: the merged pull requests (number, title, summary, merged_at) that changed it, newest first. "+
				"qname may be a full qualified_name or a bare short name (suffix-resolved; an ambiguous bare name returns candidates). "+
				"Optional `cutoff` (RFC3339) returns only PRs merged strictly before it; `k` caps the count (default 20). "+
				"Use this to answer \"when/why did this code change\" without reading git — e.g. which PR introduced or fixed a behaviour.",
		),
		mcp.WithString("qname", mcp.Required()),
		mcp.WithString("cutoff"),
		mcp.WithNumber("k", mcp.DefaultNumber(defaultChangeHistoryK)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("qname", "")
		k := int(req.GetFloat("k", defaultChangeHistoryK))
		var cutoff time.Time
		if cs := req.GetString("cutoff", ""); cs != "" {
			if t, err := time.Parse(time.RFC3339, cs); err == nil {
				cutoff = t
			}
		}
		res, err := changeHistory(reader, q, cutoff, k)
		if err != nil {
			return nil, err
		}
		return textResult(res), nil
	})
}

// changeHistory resolves qname and aggregates the PR breadcrumbs across the
// resolved node(s), deduped by PR number and sorted newest-first (capped at
// k). Seed resolution mirrors the traversal tools — bare names resolve by
// suffix, multi-match returns `ambiguous` + candidates, no match returns
// `not_found` — so callers handle resolution one way across the tool set.
func changeHistory(reader store.Reader, qname string, cutoff time.Time, k int) (map[string]any, error) {
	resolved, cands, ambiguous, ok := resolveSeed(reader, qname, "")
	if ambiguous {
		return map[string]any{
			"seed_qname": qname,
			"ambiguous":  true,
			"candidates": cands,
			"prs":        []map[string]any{},
		}, nil
	}
	if !ok {
		return map[string]any{
			"seed_qname": qname,
			"not_found":  true,
			"prs":        []map[string]any{},
		}, nil
	}

	nodes, err := reader.FindSymbol(resolved, true, store.FindSymbolOptions{})
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	prs := []map[string]any{}
	for _, n := range nodes {
		refs, err := reader.GetNodePRs(n.ID, cutoff)
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			if seen[r.Number] {
				continue
			}
			seen[r.Number] = true
			prs = append(prs, map[string]any{
				"number":    r.Number,
				"title":     r.Title,
				"summary":   r.Summary,
				"merged_at": r.MergedAtUTC.Format(time.RFC3339),
				"repo":      r.Repo,
				"head_sha":  r.HeadSHA,
				"base_sha":  r.BaseSHA,
			})
		}
	}
	// Aggregating across nodes can interleave PRs; re-sort newest-first.
	// RFC3339-UTC strings sort lexicographically == chronologically.
	sort.SliceStable(prs, func(i, j int) bool {
		return prs[i]["merged_at"].(string) > prs[j]["merged_at"].(string)
	})
	if k > 0 && len(prs) > k {
		prs = prs[:k]
	}
	return map[string]any{"seed_qname": resolved, "prs": prs}, nil
}
