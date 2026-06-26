package mcphandlers

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/pkg/store"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
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
	reader = safeReader(reader) // enforce the §11.3 H3 boundary regardless of caller
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
	reader = safeReader(reader) // enforce the §11.3 H3 boundary regardless of caller
	tool := mcp.NewTool("find_callers",
		mcp.WithDescription("Functions that call the symbol (reverse call graph). qname may be a full qualified_name (\"core.NewBlockChain\") or a bare short name (\"NewBlockChain\") — a bare name is resolved by suffix, and an ambiguous bare name returns its candidate qnames instead of an empty result. Filters to calls/invokes edges only. Default depth=2 — see docs/ckg5-depth-sweep-report-2026-05-20.md for the latency justification."),
		mcp.WithString("qname", mcp.Required()),
		mcp.WithNumber("depth", mcp.DefaultNumber(2)),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("qname", "")
		d := int(req.GetFloat("depth", 2))
		incl := req.GetBool("include_blobs", false)
		resolved, cands, ambiguous, ok := resolveSeed(reader, q, "")
		if ambiguous {
			return textResult(seedAmbiguousResult(q, cands)), nil
		}
		if !ok {
			return textResult(seedNotFoundResult(q)), nil
		}
		// Union the concrete seed with the interface methods it satisfies, so
		// interface-dispatched callers (recorded as `invokes` to the interface
		// method, not the concrete method) are not missed. See reverseCallersUnion.
		nodes, edges, err := reverseCallersUnion(reader, resolved, d)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{
			"seed_qname": resolved,
			"nodes":      attachBlobs(reader, nodes, incl),
			"edges":      edges,
		}), nil
	})
}

// RegisterFindCallees walks the forward call graph from the seed.
// Same edge-type filter as RegisterFindCallers for symmetry.
func RegisterFindCallees(s *server.MCPServer, reader store.Reader) {
	reader = safeReader(reader) // enforce the §11.3 H3 boundary regardless of caller
	tool := mcp.NewTool("find_callees",
		mcp.WithDescription("Functions called by the symbol (forward call graph). qname may be a full qualified_name or a bare short name (resolved by suffix; an ambiguous bare name returns candidate qnames instead of an empty result). Filters to calls/invokes edges only. Default depth=2 — see docs/ckg5-depth-sweep-report-2026-05-20.md for the latency justification."),
		mcp.WithString("qname", mcp.Required()),
		mcp.WithNumber("depth", mcp.DefaultNumber(2)),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("qname", "")
		d := int(req.GetFloat("depth", 2))
		incl := req.GetBool("include_blobs", false)
		resolved, cands, ambiguous, ok := resolveSeed(reader, q, "")
		if ambiguous {
			return textResult(seedAmbiguousResult(q, cands)), nil
		}
		if !ok {
			return textResult(seedNotFoundResult(q)), nil
		}
		nodes, edges, err := reader.NeighborhoodByQname(resolved, d, false, callEdgeTypes...)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{
			"seed_qname": resolved,
			"nodes":      attachBlobs(reader, nodes, incl),
			"edges":      edges,
		}), nil
	})
}

// RegisterGetSubgraph returns the BFS bidirectional subgraph rooted
// at qname. Unlike find_callers / find_callees this DOES follow every
// edge type — the caller asked for a neighbourhood, not a call graph.
func RegisterGetSubgraph(s *server.MCPServer, reader store.Reader) {
	reader = safeReader(reader) // enforce the §11.3 H3 boundary regardless of caller
	tool := mcp.NewTool("get_subgraph",
		mcp.WithDescription("Subgraph rooted at qname, expanded by depth (both directions). seed_qname may be a full qualified_name or a bare short name (resolved by suffix; an ambiguous bare name returns candidate qnames instead of an empty result)."),
		mcp.WithString("seed_qname", mcp.Required()),
		mcp.WithNumber("depth", mcp.DefaultNumber(2)),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("seed_qname", "")
		d := int(req.GetFloat("depth", 2))
		incl := req.GetBool("include_blobs", false)
		resolved, cands, ambiguous, ok := resolveSeed(reader, q, "")
		if ambiguous {
			return textResult(seedAmbiguousResult(q, cands)), nil
		}
		if !ok {
			return textResult(seedNotFoundResult(q)), nil
		}
		nodes, edges, err := reader.SubgraphByQname(resolved, d)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{
			"seed_qname": resolved,
			"nodes":      attachBlobs(reader, nodes, incl),
			"edges":      edges,
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
//
// node_kinds is the optional whitelist of node types to return. The
// default (omitted / empty) applies a symbol-only filter that strips
// statement-level rows (IfStmt/LoopStmt/CallSite/ReturnStmt/SwitchStmt/
// AwaitPoint), meta rows (Commit/Hunk), and path-only rows
// (Import/Export). Pass an explicit array — typically the full set
// produced by types.AllNodeTypes — to disable the default narrowing
// and surface every FTS match.
func RegisterSearchText(s *server.MCPServer, reader store.Reader) {
	reader = safeReader(reader) // enforce the §11.3 H3 boundary regardless of caller
	tool := mcp.NewTool("search_text",
		mcp.WithDescription("Full-text search over name + qualified_name + signature + doc_comment. Auto-prefix on short ASCII queries; substring fallback on CJK input. mode=\"or\" (default) ORs multi-token queries; mode=\"and\" requires every token to appear in each hit's indexed columns. language filters the result set to a single source language (go|ts|sol). node_kinds is an optional whitelist of node types; omit it to apply the default symbol-only filter (statement / Commit / Hunk / Import / Export rows are stripped because their FTS hits are typically noise from the enclosing symbol's qname prefix)."),
		mcp.WithString("query", mcp.Required()),
		mcp.WithNumber("top_k", mcp.DefaultNumber(10)),
		mcp.WithString("language"),
		mcp.WithString("mode"),
		mcp.WithArray("node_kinds"),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("query", "")
		top := int(req.GetFloat("top_k", 10))
		incl := req.GetBool("include_blobs", false)
		lang := req.GetString("language", "")
		mode := req.GetString("mode", "")
		opts := store.SearchFTSOptions{
			Language: lang,
			Mode:     mode,
		}
		if raw := req.GetStringSlice("node_kinds", nil); raw != nil {
			opts.NodeKinds = make([]types.NodeType, 0, len(raw))
			for _, k := range raw {
				opts.NodeKinds = append(opts.NodeKinds, types.NodeType(k))
			}
		}
		hits, err := reader.SearchWithOpts(q, top, opts)
		if err != nil {
			return nil, err
		}
		return textResult(map[string]any{"nodes": attachBlobs(reader, hits, incl)}), nil
	})
}
