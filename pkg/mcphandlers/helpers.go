package mcphandlers

import (
	mcp "github.com/mark3labs/mcp-go/mcp"

	"github.com/0xmhha/code-knowledge-graph/pkg/store"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// textResult wraps a payload in the mcp-go structured-result envelope.
// All handlers route their JSON payload through here so the response
// shape stays uniform across the tool set.
func textResult(payload any) *mcp.CallToolResult {
	return mcp.NewToolResultStructured(payload, "")
}

// callEdgeTypes are the edge types find_callers / find_callees follow.
// Without this filter the BFS would also walk contains/defines and
// return the file holding a method as one of its "callers" —
// semantically wrong and noisy for LLM consumers.
var callEdgeTypes = []string{"calls", "invokes"}

// attachBlobs converts a node slice into the JSON-friendly maps the
// MCP tools return, optionally inlining the source blob from the
// blobs table when include is true. GetBlob errors are silently
// swallowed: nodes like Package have no blob (sql.ErrNoRows is
// expected and harmless for them).
func attachBlobs(reader store.Reader, nodes []types.Node, include bool) []map[string]any {
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
			if b, err := reader.GetBlob(n.ID); err == nil {
				m["source"] = string(b)
			}
		}
		out = append(out, m)
	}
	return out
}
