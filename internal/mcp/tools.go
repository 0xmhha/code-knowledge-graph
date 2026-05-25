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
func registerFindSymbol(s *server.MCPServer, store persist.StoreReader) {
	tool := mcp.NewTool("find_symbol",
		// Description rewritten 2026-05-11 (VERIFICATION_REPORT §3.1 B2):
		// the prior phrasing "Find symbols by name or qualified name" implied
		// either could be passed with exact=true, but the underlying
		// FindSymbol always matches qualified_name — bare names work ONLY
		// with exact=false (suffix match). Spelling that contract out so
		// LLM agents don't false-empty on `{"name":"NewBlockChain","exact":true}`.
		mcp.WithDescription("Find symbols by qualified_name. With exact=true (default), the input must match qualified_name exactly (e.g. \"core.NewBlockChain\"). With exact=false, the input is treated as a suffix — a bare symbol name (\"NewBlockChain\") matches every qualified_name ending in that segment. Use exact=false when you only know the symbol's short name."),
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
		out, err := store.FindSymbol(name, exact, persist.FindSymbolOptions{Language: lang})
		if err != nil {
			return nil, err
		}
		out = filterLLMSafe(out) // §11.3 H3 retrieval boundary
		return textResult(map[string]any{"nodes": attachBlobs(store, out, incl)}), nil
	})
}

// callEdgeTypes are the edge types find_callers / find_callees consider
// "calls". Without this filter the BFS would also follow `contains`,
// `defines`, etc., and return the file that holds a method as one of
// its "callers" — semantically wrong and noisy for LLM consumers. See
// docs/VIEWER-ROADMAP.md (L4? — V0+ findings) for the audit.
var callEdgeTypes = []string{"calls", "invokes"}

// registerFindCallers returns functions that call the seed symbol (reverse call graph).
func registerFindCallers(s *server.MCPServer, store persist.StoreReader) {
	tool := mcp.NewTool("find_callers",
		mcp.WithDescription("Functions that call the symbol (reverse call graph). Filters to calls/invokes edges only. Default depth=2 — see docs/ckg5-depth-sweep-report-2026-05-20.md for the latency justification."),
		mcp.WithString("qname", mcp.Required()),
		mcp.WithNumber("depth", mcp.DefaultNumber(2)),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("qname", "")
		d := int(req.GetFloat("depth", 2))
		incl := req.GetBool("include_blobs", false)
		nodes, edges, err := store.NeighborhoodByQname(q, d, true /*reverse*/, callEdgeTypes...)
		if err != nil {
			return nil, err
		}
		nodes = filterLLMSafe(nodes) // §11.3 H3 retrieval boundary
		edges = filterLLMSafeEdges(edges, nodeIDSet(nodes))
		return textResult(map[string]any{
			"nodes": attachBlobs(store, nodes, incl),
			"edges": edges,
		}), nil
	})
}

// registerFindCallees returns functions called by the seed symbol (forward call graph).
func registerFindCallees(s *server.MCPServer, store persist.StoreReader) {
	tool := mcp.NewTool("find_callees",
		mcp.WithDescription("Functions called by the symbol (forward call graph). Filters to calls/invokes edges only. Default depth=2 — see docs/ckg5-depth-sweep-report-2026-05-20.md for the latency justification."),
		mcp.WithString("qname", mcp.Required()),
		mcp.WithNumber("depth", mcp.DefaultNumber(2)),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("qname", "")
		d := int(req.GetFloat("depth", 2))
		incl := req.GetBool("include_blobs", false)
		nodes, edges, err := store.NeighborhoodByQname(q, d, false, callEdgeTypes...)
		if err != nil {
			return nil, err
		}
		nodes = filterLLMSafe(nodes) // §11.3 H3 retrieval boundary
		edges = filterLLMSafeEdges(edges, nodeIDSet(nodes))
		return textResult(map[string]any{
			"nodes": attachBlobs(store, nodes, incl),
			"edges": edges,
		}), nil
	})
}

// registerGetSubgraph returns the BFS bidirectional subgraph rooted at qname.
func registerGetSubgraph(s *server.MCPServer, store persist.StoreReader) {
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
		nodes = filterLLMSafe(nodes) // §11.3 H3 retrieval boundary
		edges = filterLLMSafeEdges(edges, nodeIDSet(nodes))
		return textResult(map[string]any{
			"nodes": attachBlobs(store, nodes, incl),
			"edges": edges,
		}), nil
	})
}

// registerSearchText runs the smart Search router (FTS5 with auto-prefix
// for ASCII, LIKE substring fallback for CJK). Goes through attachBlobs
// so the response shape matches find_symbol / find_callers / get_subgraph
// — LLM clients can parse one schema across the toolbox.
//
// mode = "or" (default) joins multi-token queries with FTS5 OR so any one
// match surfaces a candidate, then BM25 + PageRank + usage rerank.
// mode = "and" tightens to "every token must appear in the hit's name +
// qualified_name + signature + doc_comment" — the AND fixture work
// (eval/retrieval/R06-R10) lives on this branch. CJK input ignores mode;
// substring matching is single-pattern by design.
//
// language pushes a `WHERE language = ?` filter into the FTS5 query when
// non-empty (CKG-2). Empty string preserves prior behaviour.
func registerSearchText(s *server.MCPServer, store persist.StoreReader) {
	tool := mcp.NewTool("search_text",
		mcp.WithDescription("Full-text search over name + qualified_name + signature + doc_comment. Auto-prefix on short ASCII queries; substring fallback on CJK input. mode=\"or\" (default) ORs multi-token queries; mode=\"and\" requires every token to appear in each hit's indexed columns. language filters the result set to a single source language (go|ts|sol)."),
		mcp.WithString("query", mcp.Required()),
		mcp.WithNumber("top_k", mcp.DefaultNumber(10)),
		mcp.WithString("language"),
		mcp.WithString("mode"),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("query", "")
		top := int(req.GetFloat("top_k", 10))
		incl := req.GetBool("include_blobs", false)
		lang := req.GetString("language", "")
		mode := req.GetString("mode", "")
		hits, err := store.SearchWithOpts(q, top, persist.SearchFTSOptions{
			Language: lang,
			Mode:     mode,
		})
		if err != nil {
			return nil, err
		}
		hits = filterLLMSafe(hits) // §11.3 H3 retrieval boundary
		return textResult(map[string]any{"nodes": attachBlobs(store, hits, incl)}), nil
	})
}

// attachBlobs returns a slice of map representations of nodes, optionally
// inlining the source blob from the blobs table when include is true.
// Errors from GetBlob are silently swallowed (nodes like Package have no
// blob — sql.ErrNoRows is expected and harmless).
func attachBlobs(store persist.StoreReader, nodes []types.Node, include bool) []map[string]any {
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
