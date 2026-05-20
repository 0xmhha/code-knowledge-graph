// Package link — http_match.go implements W2 of schema 1.9 (cross-language
// HTTP client → server matching).
//
// The per-language parsers (Go: internal/parse/golang/distributed.go,
// TS: internal/parse/typescript/http_client.go) detect client-side call
// sites (fetch/axios in TS, http.Get / http.Client.Get / http.NewRequest
// in Go) and emit:
//
//   - An AMBIGUOUS placeholder Endpoint with Language="external", qname
//     "http:METHOD /path" (or "http:* /path" when the verb is unknown).
//   - An http_calls edge from the caller Function/Method to that placeholder.
//
// MatchHTTPClients runs ONCE after graph.Build and all per-language parsers
// have produced their nodes/edges. It walks every placeholder Endpoint and
// either:
//
//  1. Rewires the http_calls edge to a matching real Endpoint (Language="go"
//     or "ts" — emitted by W1 server-side detection) using the 2-stage
//     cascade specified in schema-1.9-spec §6.9:
//
//     a) Specific-verb lookup: `http:METHOD /path` exact match.
//     b) Miss → wildcard fallback: `http:* /path` exact match.
//
//     The matcher uses EXACT path matching (§3.3 decision: V0 chooses
//     exact-match over suffix-match because false-positives across distinct
//     services sharing path suffixes — e.g. two microservices each exposing
//     /api/users — are far worse than the false-negatives exact-match
//     incurs in well-curated monorepos).
//
//  2. Leaves the placeholder + edge alone (§6.3 (B)) when no match is
//     found — surfacing the call as an external-API dependency for audit.
//
// Rewired placeholders are deleted from the graph (they have no remaining
// inbound edges that matter — the `defines` edge from the parser-emitting
// file is also deleted since the placeholder no longer represents a node
// in the file). This keeps the final graph tidy: a successful match leaves
// no AMBIGUOUS Endpoint residue.
package link

import (
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// HTTPMatchResult captures aggregate counts so callers can log the matching
// outcome without re-walking the graph.
type HTTPMatchResult struct {
	// Rewired counts http_calls edges whose Dst was successfully redirected
	// from a placeholder to a real Endpoint.
	Rewired int
	// SpecificHits counts rewires that matched on the specific verb (stage 1).
	SpecificHits int
	// WildcardHits counts rewires that matched on the wildcard fallback (stage 2).
	WildcardHits int
	// AmbiguousRetained counts placeholder Endpoints that remained in the
	// graph because no server-side Endpoint matched (external-API calls).
	AmbiguousRetained int
	// PlaceholdersDropped counts placeholder Endpoint nodes (and their
	// inbound `defines` edges) removed because every http_calls edge using
	// them got rewired.
	PlaceholdersDropped int
}

// MatchHTTPClients applies the §6.9 cascade matching pass to nodes/edges,
// returning the new node/edge slices plus a result summary. The function
// is pure (no side effects beyond constructing the return values) so the
// caller can apply the result atomically.
//
// Behaviour:
//   - Real Endpoint = NodeEndpoint with SubKind="http" and Language != "external".
//   - Placeholder Endpoint = NodeEndpoint with SubKind="http" and Language="external".
//   - For each http_calls edge whose Dst is a placeholder:
//     try cascade match. On hit: rewrite Dst; mark placeholder for removal
//     IF no other http_calls edge still points at it. On miss: leave alone.
//   - Placeholder Endpoint with NO inbound http_calls edges after matching:
//     dropped (shouldn't normally occur — every placeholder is created
//     alongside its http_calls edge).
func MatchHTTPClients(nodes []types.Node, edges []types.Edge) ([]types.Node, []types.Edge, HTTPMatchResult) {
	result := HTTPMatchResult{}

	// (1) Bucket Endpoint nodes by qname split into real vs placeholder.
	realByQName := map[string]string{}   // qname -> real Endpoint ID
	placeholderByID := map[string]bool{} // placeholder Endpoint IDs
	placeholderQNameByID := map[string]string{}
	for i := range nodes {
		n := &nodes[i]
		if n.Type != types.NodeEndpoint || n.SubKind != "http" {
			continue
		}
		if n.Language == "external" {
			placeholderByID[n.ID] = true
			placeholderQNameByID[n.ID] = n.QualifiedName
			continue
		}
		// Real Endpoint — first writer wins on qname collision (deterministic
		// because nodes are sorted by ID before this pass runs).
		if _, exists := realByQName[n.QualifiedName]; !exists {
			realByQName[n.QualifiedName] = n.ID
		}
	}

	// (2) Walk http_calls edges; try cascade match per §6.9 for those pointing
	// at a placeholder. Build new edge slice with rewired Dst.
	newEdges := make([]types.Edge, 0, len(edges))
	// Placeholders that retained at least one inbound http_calls edge after
	// matching — those stay in the graph as AMBIGUOUS markers.
	retained := map[string]bool{}
	for _, e := range edges {
		if e.Type != types.EdgeHTTPCalls || !placeholderByID[e.Dst] {
			newEdges = append(newEdges, e)
			continue
		}
		placeholderQName := placeholderQNameByID[e.Dst]
		method, path, ok := parseHTTPQName(placeholderQName)
		if !ok {
			// Malformed placeholder qname — keep edge as-is.
			retained[e.Dst] = true
			newEdges = append(newEdges, e)
			continue
		}
		// Stage 1: specific verb lookup (skipped when method is "*" — go
		// straight to wildcard).
		var matchID string
		matchStage := 0
		if method != "*" {
			if rid, hit := realByQName["http:"+method+" "+path]; hit {
				matchID = rid
				matchStage = 1
			}
		}
		// Stage 2: wildcard fallback.
		if matchID == "" {
			if rid, hit := realByQName["http:* "+path]; hit {
				matchID = rid
				matchStage = 2
			}
		}
		if matchID == "" {
			// Both miss — keep placeholder.
			retained[e.Dst] = true
			// Reset ID so the incremental persist step inserts a fresh row.
			// Background: incremental.go's reloadCachedEdges loads cached
			// http_calls edges with non-zero ID; the persist step's INSERT
			// skips ID!=0 to avoid duplicating. Combined with the DB-side
			// DeleteEdgesByType("http_calls") in incremental.go step 3, the
			// reset lets us re-INSERT every http_calls edge on each pass —
			// keeping DB in sync with in-memory rewire/retain decisions.
			e.ID = 0
			newEdges = append(newEdges, e)
			result.AmbiguousRetained++
			continue
		}
		// Rewire.
		rewired := e
		rewired.Dst = matchID
		// Reset ID for the same reason as the retain branch above.
		rewired.ID = 0
		// On successful match, lift confidence: client knew the method/path
		// and a real server-side Endpoint exists for it — INFERRED is
		// honest about static-analysis source but the matching itself is
		// EXTRACTED-quality. Keep INFERRED so consumers can still filter on
		// "exact handler resolution" semantics if desired.
		newEdges = append(newEdges, rewired)
		result.Rewired++
		switch matchStage {
		case 1:
			result.SpecificHits++
		case 2:
			result.WildcardHits++
		}
	}

	// (3) Drop placeholder nodes that no longer have an inbound http_calls
	// edge AND drop their inbound `defines` edge from the parser-emitting
	// file. Placeholders retained as AMBIGUOUS markers stay.
	toDrop := map[string]bool{}
	for id := range placeholderByID {
		if !retained[id] {
			toDrop[id] = true
			result.PlaceholdersDropped++
		}
	}
	if len(toDrop) == 0 {
		return nodes, newEdges, result
	}

	// Filter the placeholder nodes out, and drop `defines` edges into them
	// (those came from the parser-emitting file; keeping them would dangle).
	filteredNodes := make([]types.Node, 0, len(nodes))
	for _, n := range nodes {
		if toDrop[n.ID] {
			continue
		}
		filteredNodes = append(filteredNodes, n)
	}
	filteredEdges := make([]types.Edge, 0, len(newEdges))
	for _, e := range newEdges {
		if toDrop[e.Dst] && e.Type == types.EdgeDefines {
			continue
		}
		filteredEdges = append(filteredEdges, e)
	}

	return filteredNodes, filteredEdges, result
}

// parseHTTPQName splits an Endpoint qname into (METHOD, path). The qname
// format (schema 1.9 §6.2) is `http:METHOD /path` with a single space
// between METHOD and /path. Returns ("", "", false) when the prefix or
// shape doesn't match.
//
// Tolerates the wildcard form `http:* /path`. METHOD is preserved
// verbatim (callers compare against the literal "*" sentinel).
func parseHTTPQName(qname string) (method, path string, ok bool) {
	const prefix = "http:"
	if len(qname) <= len(prefix) {
		return "", "", false
	}
	if qname[:len(prefix)] != prefix {
		return "", "", false
	}
	rest := qname[len(prefix):]
	// Find the FIRST space — METHOD has no spaces, path begins with /.
	for i := 0; i < len(rest); i++ {
		if rest[i] == ' ' {
			method = rest[:i]
			path = rest[i+1:]
			if method == "" || path == "" {
				return "", "", false
			}
			return method, path, true
		}
	}
	return "", "", false
}
