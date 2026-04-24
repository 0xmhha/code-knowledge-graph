// Package mcp — five "granular" MCP tool registrations.
//
// Each tool wraps a Store query with a thin schema declaration and a closure
// that unpacks the request and returns a structured result. The smart
// `get_context_for_task` lives in get_context.go.
package mcp

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// registerFindSymbol resolves an exact-or-suffix qname / name to nodes.
func registerFindSymbol(s *server.MCPServer, store *persist.Store) {
	tool := mcp.NewTool("find_symbol",
		mcp.WithDescription("Find symbols by name or qualified name."),
		mcp.WithString("name", mcp.Required()),
		mcp.WithString("language"),
		mcp.WithBoolean("exact", mcp.DefaultBool(true)),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := req.GetString("name", "")
		lang := req.GetString("language", "")
		exact := req.GetBool("exact", true)
		incl := req.GetBool("include_blobs", false)
		out, err := store.FindSymbol(name, lang, exact)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{"nodes": attachBlobs(store, out, incl)}), nil
	})
}

// registerFindCallers returns functions that call the seed symbol (reverse call graph).
func registerFindCallers(s *server.MCPServer, store *persist.Store) {
	tool := mcp.NewTool("find_callers",
		mcp.WithDescription("Functions that call the symbol (reverse call graph)."),
		mcp.WithString("qname", mcp.Required()),
		mcp.WithNumber("depth", mcp.DefaultNumber(1)),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("qname", "")
		d := int(req.GetFloat("depth", 1))
		incl := req.GetBool("include_blobs", false)
		nodes, edges, err := store.NeighborhoodByQname(q, d, true /*reverse*/)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{
			"nodes": attachBlobs(store, nodes, incl),
			"edges": edges,
		}), nil
	})
}

// registerFindCallees returns functions called by the seed symbol (forward call graph).
func registerFindCallees(s *server.MCPServer, store *persist.Store) {
	tool := mcp.NewTool("find_callees",
		mcp.WithDescription("Functions called by the symbol (forward call graph)."),
		mcp.WithString("qname", mcp.Required()),
		mcp.WithNumber("depth", mcp.DefaultNumber(1)),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("qname", "")
		d := int(req.GetFloat("depth", 1))
		incl := req.GetBool("include_blobs", false)
		nodes, edges, err := store.NeighborhoodByQname(q, d, false)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{
			"nodes": attachBlobs(store, nodes, incl),
			"edges": edges,
		}), nil
	})
}

// registerGetSubgraph returns the BFS bidirectional subgraph rooted at qname.
func registerGetSubgraph(s *server.MCPServer, store *persist.Store) {
	tool := mcp.NewTool("get_subgraph",
		mcp.WithDescription("Subgraph rooted at qname, expanded by depth (both directions)."),
		mcp.WithString("seed_qname", mcp.Required()),
		mcp.WithNumber("depth", mcp.DefaultNumber(2)),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("seed_qname", "")
		d := int(req.GetFloat("depth", 2))
		incl := req.GetBool("include_blobs", false)
		nodes, edges, err := store.SubgraphByQname(q, d)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{
			"nodes": attachBlobs(store, nodes, incl),
			"edges": edges,
		}), nil
	})
}

// registerSearchText runs a BM25 query over the FTS5 index of name+qname+signature+doc.
func registerSearchText(s *server.MCPServer, store *persist.Store) {
	tool := mcp.NewTool("search_text",
		mcp.WithDescription("Full-text search over name + qualified_name + signature + doc_comment."),
		mcp.WithString("query", mcp.Required()),
		mcp.WithNumber("top_k", mcp.DefaultNumber(10)),
		mcp.WithString("language"),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("query", "")
		top := int(req.GetFloat("top_k", 10))
		hits, err := store.SearchFTS(q, top)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{"nodes": hits}), nil
	})
}

// attachBlobs returns a slice of map representations of nodes, optionally
// inlining the source blob from the blobs table when include is true.
// Errors from GetBlob are silently swallowed (nodes like Package have no
// blob — sql.ErrNoRows is expected and harmless).
func attachBlobs(store *persist.Store, nodes []types.Node, include bool) []map[string]any {
	out := make([]map[string]any, 0, len(nodes))
	for _, n := range nodes {
		m := map[string]any{
			"id":          n.ID,
			"type":        n.Type,
			"name":        n.Name,
			"qname":       n.QualifiedName,
			"file":        n.FilePath,
			"line":        n.StartLine,
			"confidence":  n.Confidence,
			"signature":   n.Signature,
			"usage_score": n.UsageScore,
		}
		if include {
			if b, err := store.GetBlob(n.ID); err == nil {
				m["source"] = string(b)
			}
		}
		out = append(out, m)
	}
	return out
}
