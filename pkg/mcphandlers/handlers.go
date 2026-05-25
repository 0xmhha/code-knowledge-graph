package mcphandlers

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/pkg/store"
)

// RegisterFindSymbol resolves an exact-or-suffix qname / name to nodes.
//
// Description text was rewritten 2026-05-11 (go-stablenet
// VERIFICATION_REPORT §3.1 B2): the prior phrasing implied a bare
// name worked with exact=true, but FindSymbol always matches
// qualified_name — bare names work only with exact=false (suffix
// match). The schema spells that contract out so LLM agents don't
// false-empty on `{"name":"NewBlockChain","exact":true}`.
func RegisterFindSymbol(s *server.MCPServer, reader store.Reader) {
	tool := mcp.NewTool("find_symbol",
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
		out, err := reader.FindSymbol(name, exact, store.FindSymbolOptions{Language: lang})
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{"nodes": attachBlobs(reader, out, incl)}), nil
	})
}

// RegisterFindCallers walks the reverse call graph from the seed.
// Filters to calls/invokes edges so the BFS only follows real
// invocation links (see callEdgeTypes for the rationale). Default
// depth=2 — see docs/ckg5-depth-sweep-report-2026-05-20.md for the
// latency justification.
func RegisterFindCallers(s *server.MCPServer, reader store.Reader) {
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
		nodes, edges, err := reader.NeighborhoodByQname(q, d, true /*reverse*/, callEdgeTypes...)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{
			"nodes": attachBlobs(reader, nodes, incl),
			"edges": edges,
		}), nil
	})
}

// RegisterFindCallees walks the forward call graph from the seed.
// Same edge-type filter as RegisterFindCallers for symmetry.
func RegisterFindCallees(s *server.MCPServer, reader store.Reader) {
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
		nodes, edges, err := reader.NeighborhoodByQname(q, d, false, callEdgeTypes...)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{
			"nodes": attachBlobs(reader, nodes, incl),
			"edges": edges,
		}), nil
	})
}

// RegisterGetSubgraph returns the BFS bidirectional subgraph rooted
// at qname. Unlike find_callers / find_callees this DOES follow every
// edge type — the caller asked for a neighbourhood, not a call graph.
func RegisterGetSubgraph(s *server.MCPServer, reader store.Reader) {
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
		nodes, edges, err := reader.SubgraphByQname(q, d)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{
			"nodes": attachBlobs(reader, nodes, incl),
			"edges": edges,
		}), nil
	})
}

// RegisterSearchText runs the smart Search router (FTS5 with
// auto-prefix for ASCII, LIKE substring fallback for CJK). Routes
// through attachBlobs so the response shape matches
// find_symbol / find_callers / get_subgraph — LLM clients can parse
// one schema across the toolbox.
//
// mode = "or" (default) ORs multi-token queries via rewriteFTSQuery
// (any token match surfaces the candidate, BM25 + PageRank + usage
// rerank). mode = "and" requires every token to appear in the hit's
// FTS-indexed columns; useful for precise multi-keyword retrieval.
// language pushes a `WHERE language = ?` filter into the SQL when
// non-empty (CKG-2).
func RegisterSearchText(s *server.MCPServer, reader store.Reader) {
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
		hits, err := reader.SearchWithOpts(q, top, store.SearchFTSOptions{
			Language: lang,
			Mode:     mode,
		})
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{"nodes": attachBlobs(reader, hits, incl)}), nil
	})
}
