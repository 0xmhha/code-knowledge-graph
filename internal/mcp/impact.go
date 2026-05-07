// Package mcp — impact_of_change tool (P1a, dogfood plan).
//
// Given a symbol qname (or a file path), returns the set of nodes that would
// need to be examined when the seed changes — i.e. reverse dependency closure
// over a SUPERSET of the call edges that find_callers walks.
//
// Why a separate tool from find_callers?
//   - find_callers restricts to {calls, invokes} so it returns ONLY the call
//     graph. That is intentional and stays as-is.
//   - "impact" is a broader question: anyone who imports the type, implements
//     the interface, reads/writes a field, listens on the same endpoint, etc.
//     should also be examined when the seed changes.
//
// The algorithm body lives in pkg/impact so the same code path is shared
// with internal/server's HTTP /api/impact handler — mirrors the smartctx
// pattern (pkg/smartctx is shared by mcp.get_context_for_task and the eval
// δ baseline). This file is now the MCP request envelope only.
package mcp

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/impact"
)

// registerImpactOfChange wires the impact_of_change tool. Either seed_qname
// or seed_file must be set; if both are set, seed_qname wins (less ambiguous,
// and the qname path returns a single seed node for the response envelope).
func registerImpactOfChange(s *server.MCPServer, store persist.StoreReader) {
	tool := mcp.NewTool("impact_of_change",
		mcp.WithDescription(
			"Reverse-dependency closure for a symbol or file. Returns nodes/edges grouped by impact category "+
				"(callers, interface_impact, type_users, distributed, concurrent, other_refs). "+
				"If results look empty for a Go concrete-method seed, retry with the interface method qname "+
				"(Go's call graph binds invocations to the interface, not the concrete receiver).",
		),
		mcp.WithString("seed_qname"),
		mcp.WithString("seed_file"),
		mcp.WithNumber("depth", mcp.DefaultNumber(2)),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		seedQ := req.GetString("seed_qname", "")
		seedF := req.GetString("seed_file", "")
		out, err := impact.Compute(store, seedQ, seedF, impact.Options{
			Depth:        int(req.GetFloat("depth", 2)),
			IncludeBlobs: req.GetBool("include_blobs", false),
		})
		if err != nil {
			return nil, err
		}
		return textResult(out), nil
	})
}

// computeImpact is a thin pass-through retained so existing tests (which
// call computeImpact directly to sidestep the MCP request envelope) keep
// working without churn. New callers should use pkg/impact.Compute instead.
func computeImpact(store persist.StoreReader, seedQname, seedFile string, depth int, includeBlobs bool) (map[string]any, error) {
	return impact.Compute(store, seedQname, seedFile, impact.Options{
		Depth:        depth,
		IncludeBlobs: includeBlobs,
	})
}

// impactDepthCap re-exports the shared cap so the existing
// TestImpact_DepthCap assertion (which compares against this constant)
// continues to work without referencing pkg/impact directly.
const impactDepthCap = impact.DepthCap
