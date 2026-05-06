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
// The result is grouped by edge category so the LLM can prioritise where to
// look first (callers tend to dominate; distributed/binds_to is often the
// most surprising for an LLM that only knows the single-language slice).
package mcp

import (
	"context"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// impactDepthCap caps user-supplied depth so an LLM that asks for depth=99
// cannot blow up the BFS. Five hops is already more than enough for human
// review; deeper transitive closures dilute signal more than they help.
const impactDepthCap = 5

// impactEdgeTypes is the broader reverse-traversal edge set used by
// impact_of_change. It excludes structural edges (contains/defines) — those
// would resolve every method back to its file/package and drown the result
// — and excludes lock/concurrency/temporal edges, which describe runtime or
// historical state rather than a change-impact dependency.
//
// The five groups below intentionally line up with the output buckets so
// each edge has exactly one home category.
var (
	impactEdgesCallers     = []string{"calls", "invokes"}
	impactEdgesInterface   = []string{"implements", "extends"}
	impactEdgesTypeUsers   = []string{"uses_type", "instantiates", "reads_field", "writes_field", "reads_mapping", "writes_mapping"}
	impactEdgesDistributed = []string{"listens_on", "handles_message", "rpc_calls", "binds_to"}
	impactEdgesOtherRefs   = []string{"references", "emits_event", "has_modifier", "has_decorator"}
)

// impactGroup binds an output bucket name to its edge filter. Order matters
// for output stability but not for correctness.
type impactGroup struct {
	key   string
	edges []string
}

func impactGroups() []impactGroup {
	return []impactGroup{
		{key: "callers", edges: impactEdgesCallers},
		{key: "interface_impact", edges: impactEdgesInterface},
		{key: "type_users", edges: impactEdgesTypeUsers},
		{key: "distributed", edges: impactEdgesDistributed},
		{key: "other_refs", edges: impactEdgesOtherRefs},
	}
}

// registerImpactOfChange wires the impact_of_change tool. Either seed_qname
// or seed_file must be set; if both are set, seed_qname wins (less ambiguous,
// and the qname path returns a single seed node for the response envelope).
func registerImpactOfChange(s *server.MCPServer, store persist.StoreReader) {
	tool := mcp.NewTool("impact_of_change",
		mcp.WithDescription("Reverse-dependency closure for a symbol or file. Returns nodes/edges grouped by impact category (callers, interface_impact, type_users, distributed, other_refs)."),
		mcp.WithString("seed_qname"),
		mcp.WithString("seed_file"),
		mcp.WithNumber("depth", mcp.DefaultNumber(2)),
		mcp.WithBoolean("include_blobs", mcp.DefaultBool(false)),
	)
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		seedQ := req.GetString("seed_qname", "")
		seedF := req.GetString("seed_file", "")
		depth := int(req.GetFloat("depth", 2))
		if depth < 1 {
			depth = 1
		}
		if depth > impactDepthCap {
			depth = impactDepthCap
		}
		incl := req.GetBool("include_blobs", false)

		out, err := computeImpact(store, seedQ, seedF, depth, incl)
		if err != nil {
			return nil, err
		}
		return textResult(out), nil
	})
}

// computeImpact is the algorithm body, factored out for direct testing
// without going through the MCP request envelope. Returns the structured
// payload that the tool handler wraps in textResult.
func computeImpact(store persist.StoreReader, seedQname, seedFile string, depth int, includeBlobs bool) (map[string]any, error) {
	// Resolve seed(s). seed_qname wins when both are set.
	seeds, primary, err := resolveImpactSeeds(store, seedQname, seedFile)
	if err != nil {
		return nil, err
	}
	if len(seeds) == 0 {
		return map[string]any{
			"not_found": true,
			"depth":     depth,
		}, nil
	}

	// Per-group reverse traversal. We walk each group's edge filter
	// independently so we can attribute reached nodes to their bucket
	// without inspecting each edge after the fact. Yes, that means we
	// pay len(groups) * traversal cost — for the small graphs CKG
	// targets (sub-million nodes) this is well under the SQLite cost
	// floor and keeps the bucket attribution unambiguous.
	type groupResult struct {
		nodes []map[string]any
		count int
	}
	groupOut := make(map[string]groupResult, 5)

	dedupNodes := map[string]types.Node{}
	dedupEdges := map[string]types.Edge{}
	warnings := []map[string]any{}

	// Track seed IDs so we can exclude them from the impact buckets —
	// the seed itself is reported in `seed`/`seeds`, not as its own
	// dependent.
	seedIDs := map[string]bool{}
	for _, s := range seeds {
		seedIDs[s.ID] = true
	}

	for _, g := range impactGroups() {
		reached := map[string]types.Node{}
		for _, seed := range seeds {
			if seed.QualifiedName == "" {
				continue
			}
			nodes, edges, err := store.NeighborhoodByQname(seed.QualifiedName, depth, true /*reverse*/, g.edges...)
			if err != nil {
				return nil, err
			}
			for _, n := range nodes {
				if seedIDs[n.ID] {
					continue
				}
				reached[n.ID] = n
				dedupNodes[n.ID] = n
			}
			for _, e := range edges {
				k := edgeKey(e)
				dedupEdges[k] = e
			}
		}

		// Project reached nodes for this bucket into the LLM-facing shape.
		bucket := make([]map[string]any, 0, len(reached))
		for _, n := range reached {
			m := nodeToImpactEntry(store, n, includeBlobs, &warnings)
			bucket = append(bucket, m)
		}
		groupOut[g.key] = groupResult{nodes: bucket, count: len(bucket)}
	}

	// Edge triples — keep them compact (Type, Src, Dst, Line) to match
	// the existing find_callers / get_subgraph envelope without bloating
	// the response.
	edgeTriples := make([][]any, 0, len(dedupEdges))
	for _, e := range dedupEdges {
		edgeTriples = append(edgeTriples, []any{e.Src, e.Dst, string(e.Type), e.Line})
	}

	// Build totals.by_group ahead of the impact map so the response is
	// deterministic for tests.
	byGroup := map[string]int{}
	impact := map[string]any{}
	for _, g := range impactGroups() {
		gr := groupOut[g.key]
		impact[g.key] = gr.nodes
		byGroup[g.key] = gr.count
	}

	resp := map[string]any{
		"depth":  depth,
		"impact": impact,
		"edges":  edgeTriples,
		"totals": map[string]any{
			"nodes":    len(dedupNodes),
			"edges":    len(dedupEdges),
			"by_group": byGroup,
		},
		"metadata": map[string]any{
			"warnings": warnings,
		},
	}

	// Single-seed envelope (qname mode). For file mode we expose the
	// full seed list so the LLM can see which symbols were treated as
	// roots — file-level impact is multi-rooted by definition.
	if primary != nil {
		resp["seed"] = seedSummary(*primary)
	}
	if seedFile != "" && seedQname == "" {
		seedList := make([]map[string]any, 0, len(seeds))
		for _, s := range seeds {
			seedList = append(seedList, seedSummary(s))
		}
		resp["seeds"] = seedList
		resp["seed_file"] = seedFile
	}

	return resp, nil
}

// resolveImpactSeeds returns the seed node set and (when resolvable to a
// single primary node) a pointer to that node. seed_qname takes precedence;
// when only seed_file is given, every node in the file becomes a seed.
//
// Returns (nil, nil, nil) when nothing matched — the caller surfaces this
// as `not_found: true` in the response.
func resolveImpactSeeds(store persist.StoreReader, seedQname, seedFile string) ([]types.Node, *types.Node, error) {
	if seedQname != "" {
		nodes, err := store.FindSymbol(seedQname, "", true)
		if err != nil {
			return nil, nil, err
		}
		if len(nodes) == 0 {
			return nil, nil, nil
		}
		// FindSymbol can return multiple rows when the same qname exists
		// across languages (rare); pick the first as the primary for the
		// seed envelope and keep all as roots for traversal.
		primary := nodes[0]
		return nodes, &primary, nil
	}
	if seedFile != "" {
		nodes, err := store.NodesByFilePath(seedFile)
		if err != nil {
			return nil, nil, err
		}
		// Drop nodes without a qname (StartLine-anonymous fragments etc.)
		// — NeighborhoodByQname needs a qname to resolve roots.
		filtered := nodes[:0]
		for _, n := range nodes {
			if n.QualifiedName != "" {
				filtered = append(filtered, n)
			}
		}
		if len(filtered) == 0 {
			return nil, nil, nil
		}
		return filtered, nil, nil
	}
	return nil, nil, nil
}

// nodeToImpactEntry projects a Node into the per-bucket impact entry. It
// adds a citation when file_path + start_line are present; otherwise it
// records a metadata warning so the LLM still knows the node exists but
// can't be cited (Citation Enforcement, warn-mode contract).
func nodeToImpactEntry(store persist.StoreReader, n types.Node, includeBlobs bool, warnings *[]map[string]any) map[string]any {
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
	if cite, ok := citationFor(n); ok {
		m["citation"] = cite
	} else {
		*warnings = append(*warnings, map[string]any{
			"code":    "missing-citation",
			"node_id": n.ID,
			"qname":   n.QualifiedName,
		})
	}
	if includeBlobs {
		if b, err := store.GetBlob(n.ID); err == nil {
			m["source"] = string(b)
		}
	}
	return m
}

// seedSummary produces the small envelope used for `seed` / `seeds` keys.
func seedSummary(n types.Node) map[string]any {
	out := map[string]any{
		"id":         n.ID,
		"type":       n.Type,
		"name":       n.Name,
		"qname":      n.QualifiedName,
		"file_path":  n.FilePath,
		"start_line": n.StartLine,
	}
	if cite, ok := citationFor(n); ok {
		out["citation"] = cite
	}
	return out
}

// citationFor mirrors smartctx.citationFor — kept private here to avoid a
// cross-package import for a 4-line helper. Returns "file:line" when both
// fields are present.
func citationFor(n types.Node) (string, bool) {
	if n.FilePath == "" || n.StartLine <= 0 {
		return "", false
	}
	return n.FilePath + ":" + itoa(n.StartLine), true
}

// edgeKey is the dedup key for impact edges. We intentionally include Line
// so two distinct call sites in the same caller→callee pair don't collapse
// into one edge — that information is load-bearing for the LLM picking
// which line to read first.
func edgeKey(e types.Edge) string {
	return string(e.Type) + "|" + e.Src + "|" + e.Dst + "|" + itoa(e.Line)
}

// itoa is a tiny base-10 int formatter so we don't pull strconv into a hot
// path that already sits behind SQLite latency. Identical to the helper
// in smartctx (kept duplicated to avoid an internal-cross-package import
// for a one-liner).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
