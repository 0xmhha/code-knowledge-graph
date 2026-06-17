package mcphandlers

import (
	"sort"

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

// resolveSeed maps a user-supplied seed — which may be a fully-qualified
// name ("a.Greet") or a bare short name ("Greet") — to a single canonical
// qualified_name for the graph-traversal tools (find_callers /
// find_callees / get_subgraph). It mirrors find_symbol's exact-or-suffix
// logic so an agent that located a symbol via find_symbol(exact=false) can
// feed the same bare name to the traversal tools instead of silently
// getting an empty result (those tools resolve seeds with exact=true).
//
// Resolution order:
//  0. exact match on canonical_id — globally unique (ADR-0001), so a hit is
//     unambiguous by construction; resolves to that node's qualified_name.
//  1. exact match on qualified_name — the common, unambiguous case.
//  2. suffix match (bare name) when exact found nothing.
//
// Return contract:
//   - ok=true, qname set            exactly one distinct qname matched.
//   - ambiguous=true, candidates set the suffix matched >1 distinct qname;
//     the caller should surface them so the agent can retry with a full
//     qname (ADR-0001: a multi-match is reported, never silently picked).
//   - all-zero (ok=false)           nothing matched (not_found).
//
// Multiple nodes sharing ONE qualified_name (e.g. the same symbol across
// languages) is not ambiguous: every such node becomes a traversal root
// inside NeighborhoodByQname/SubgraphByQname. To traverse one exact node
// regardless of qname collisions, seed with its canonical_id (step 0).
func resolveSeed(reader store.Reader, seed, lang string) (qname string, candidates []string, ambiguous bool, ok bool) {
	if seed == "" {
		return "", nil, false, false
	}
	opts := store.FindSymbolOptions{Language: lang}

	// 0. exact canonical_id — unambiguous; resolve to the node's qualified_name.
	if n, found, err := reader.FindByCanonicalID(seed); err == nil && found {
		return n.QualifiedName, nil, false, true
	}

	// 1. exact qualified_name.
	if nodes, err := reader.FindSymbol(seed, true, opts); err == nil && len(nodes) > 0 {
		return seed, nil, false, true
	}

	// 2. suffix fallback for bare names.
	nodes, err := reader.FindSymbol(seed, false, opts)
	if err != nil || len(nodes) == 0 {
		return "", nil, false, false
	}
	seen := map[string]struct{}{}
	distinct := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if _, dup := seen[n.QualifiedName]; dup {
			continue
		}
		seen[n.QualifiedName] = struct{}{}
		distinct = append(distinct, n.QualifiedName)
	}
	if len(distinct) == 1 {
		return distinct[0], nil, false, true
	}
	sort.Strings(distinct) // deterministic order keeps cks's prompt cache stable
	return "", distinct, true, false
}

// seedNotFoundResult is the uniform payload the traversal tools return
// when a seed resolves to nothing. nodes/edges are empty (not omitted) so
// the consumer's shape stays stable, and not_found distinguishes "the
// symbol has no neighbours" from "the symbol name did not resolve".
func seedNotFoundResult(seed string) map[string]any {
	return map[string]any{
		"seed_qname": seed,
		"not_found":  true,
		"nodes":      []map[string]any{},
		"edges":      []types.Edge{},
	}
}

// seedAmbiguousResult is returned when a bare seed name matches more than
// one distinct qualified_name. The agent is expected to retry with one of
// the candidates rather than receive a silently-picked (and possibly
// wrong) neighbourhood.
func seedAmbiguousResult(seed string, candidates []string) map[string]any {
	return map[string]any{
		"seed_qname": seed,
		"ambiguous":  true,
		"candidates": candidates,
		"nodes":      []map[string]any{},
		"edges":      []types.Edge{},
	}
}

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
		// canonical_id (ADR-0001): the globally-unique, import-path-qualified
		// identity. Included only when present (empty for builtins, AST-only
		// mode, and not-yet-wired languages) so the agent can feed it back as an
		// unambiguous seed to the traversal tools. Omitted when empty.
		if n.CanonicalID != "" {
			m["canonical_id"] = n.CanonicalID
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
