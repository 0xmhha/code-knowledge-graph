// Package impact is the shared "impact_of_change" implementation used by both
// internal/mcp.impact_of_change and internal/server's HTTP /api/impact handler.
// Before this package existed the algorithm lived inside internal/mcp; the
// HTTP viewer surface had to either re-implement reverse-dependency closure
// (drift risk) or reach across packages into private mcp helpers (layering
// inversion). Mirrors the pkg/smartctx pattern: callers serialise the
// returned map however they prefer (mcp wraps it in mcp.NewToolResult, server
// json-encodes it directly).
//
// Citation Enforcement (warn mode): every node in any bucket includes
// file_path + start_line. Nodes that lack either are kept in the response
// (preserve recall) but recorded under metadata.warnings with code
// "missing-citation" — same contract as smartctx.
package impact

import (
	"sort"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// DepthCap caps user-supplied depth so a misconfigured client (or LLM that
// asks for depth=99) cannot blow up the BFS. Five hops is already more than
// enough for human review; deeper transitive closures dilute signal more
// than they help.
const DepthCap = 5

// Edge filters per output bucket. Excludes structural (contains/defines —
// would resolve every method back to its package) and lock/temporal edges
// (runtime / historical state, not change-impact). The six groups line up
// 1:1 with the response keys so each edge has exactly one home category.
//
// Intentionally excluded:
//   - acquires_lock / releases_lock / accessed_under_lock — runtime locking
//     state. Touching a function under a lock does not mean changing it
//     impacts the lock owner; that's a separate concern.
//   - changed_in / blame — git history. A commit that previously touched the
//     seed does not need to be "examined" when the seed changes again.
var (
	edgesCallers     = []string{"calls", "invokes"}
	edgesInterface   = []string{"implements", "extends"}
	edgesTypeUsers   = []string{"uses_type", "instantiates", "reads_field", "writes_field", "reads_mapping", "writes_mapping"}
	edgesDistributed = []string{"listens_on", "handles_message", "rpc_calls", "binds_to"}
	edgesConcurrent  = []string{"spawns", "sends_to", "recvs_from"}
	// otherRefs absorbs the long tail. imports/exports (TS module edges) land
	// here rather than getting their own group: a change to the seed triggers
	// a "go re-read this importer" examination just like `references` does,
	// and a dedicated `module` bucket would be empty for every Go-only graph.
	edgesOtherRefs = []string{"references", "emits_event", "has_modifier", "has_decorator", "imports", "exports"}
)

type group struct {
	key   string
	edges []string
}

func groups() []group {
	return []group{
		{key: "callers", edges: edgesCallers},
		{key: "interface_impact", edges: edgesInterface},
		{key: "type_users", edges: edgesTypeUsers},
		{key: "distributed", edges: edgesDistributed},
		{key: "concurrent", edges: edgesConcurrent},
		{key: "other_refs", edges: edgesOtherRefs},
	}
}

// allImpactEdges is the deduped union of every group's edge filter. The single
// reverse-neighborhood query uses it so one traversal per seed covers every
// bucket; the per-group split then happens in memory.
func allImpactEdges() []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range groups() {
		for _, t := range g.edges {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// Options bundles the tunable knobs of Compute. Zero values are resolved to
// documented defaults inside Compute so callers can pass an empty struct
// for the typical case.
type Options struct {
	Depth        int  // default 2; clamped to [1, DepthCap]
	IncludeBlobs bool // default false; opt-in source bodies for LLMs
}

// Compute is the shared algorithm body. seed_qname wins when both seedQname
// and seedFile are set (less ambiguous, and the qname path returns a single
// seed node for the response envelope).
//
// The output is fully deterministic for a fixed (store, seed, depth) tuple:
// per-group node slices are sorted by qname (tiebreak id), edge triples by
// (type, src, dst, line), warnings by node_id, and the multi-seed echo by id.
// Go map iteration randomness is therefore boundary-only — it never leaks
// into the marshalled response, which keeps the LLM context cache stable.
//
// Response shape (always present):
//
//	depth        int                — actual depth used (post-clamp)
//	seed         object?            — single-seed envelope (qname mode)
//	seeds        []object?          — multi-seed envelope (file mode)
//	seed_file    string?            — echoed file path (file mode)
//	seed_qname   string?            — echoed seed qname when not_found
//	not_found    bool?              — true when the seed did not resolve
//	impact       map[string][]node  — keys: callers, interface_impact,
//	                                  type_users, distributed, concurrent,
//	                                  other_refs (always all six, possibly
//	                                  empty so consumers don't nil-check)
//	edges        [][]any            — [src, dst, type, line] triples
//	totals       object             — { nodes, edges, by_group }
//	metadata     object             — { warnings: [...] }
func Compute(store persist.StoreReader, seedQname, seedFile string, opt Options) (map[string]any, error) {
	depth := opt.Depth
	if depth < 1 {
		depth = 1
	}
	if depth > DepthCap {
		depth = DepthCap
	}

	seeds, primary, err := resolveSeeds(store, seedQname, seedFile)
	if err != nil {
		return nil, err
	}
	if len(seeds) == 0 {
		// Echo whichever seed identifier the caller supplied so a client
		// can confirm what was attempted without re-reading the request.
		out := map[string]any{
			"not_found": true,
			"depth":     depth,
		}
		if seedQname != "" {
			out["seed_qname"] = seedQname
		}
		if seedFile != "" {
			out["seed_file"] = seedFile
		}
		return out, nil
	}

	// Reverse traversal. Fetch the union reverse-neighborhood once per seed
	// (all impact edge classes at once), then split the returned subgraph into
	// buckets by replaying a per-group BFS in memory. Bucket attribution stays
	// edge-class-exact — a node lands in a bucket only if a path of that class
	// alone reaches it — but we pay one store traversal per seed instead of one
	// per (seed, group). The union neighborhood is a superset of every group's,
	// so the in-memory replay reaches exactly the same nodes and edges.
	type groupResult struct {
		nodes []map[string]any
		count int
	}
	groupOut := make(map[string]groupResult, len(groups()))

	dedupNodes := map[string]types.Node{}
	dedupEdges := map[string]types.Edge{}
	warnings := []map[string]any{}

	// Track seed IDs so we can exclude them from the impact buckets — the
	// seed itself is reported in `seed`/`seeds`, not as its own dependent.
	seedIDs := map[string]bool{}
	for _, s := range seeds {
		seedIDs[s.ID] = true
	}

	// Per-group reached node sets, accumulated across seeds.
	groupReached := make(map[string]map[string]types.Node, len(groups()))
	for _, g := range groups() {
		groupReached[g.key] = map[string]types.Node{}
	}

	unionEdges := allImpactEdges()
	for _, seed := range seeds {
		if seed.QualifiedName == "" {
			continue
		}
		nbNodes, nbEdges, err := store.NeighborhoodByQname(seed.QualifiedName, depth, true /*reverse*/, unionEdges...)
		if err != nil {
			return nil, err
		}
		nodeByID := make(map[string]types.Node, len(nbNodes))
		for _, n := range nbNodes {
			nodeByID[n.ID] = n
		}
		// Reverse adjacency over the returned subgraph: edges keyed by the
		// node they point to, so a reverse BFS walks src ← dst.
		inEdges := make(map[string][]types.Edge, len(nbNodes))
		for _, e := range nbEdges {
			inEdges[e.Dst] = append(inEdges[e.Dst], e)
		}
		// Roots are the nodes the store resolved this qname to (exact qname
		// match), mirroring NeighborhoodByQname's own FindSymbol seeding.
		var roots []string
		for _, n := range nbNodes {
			if n.QualifiedName == seed.QualifiedName {
				roots = append(roots, n.ID)
			}
		}

		for _, g := range groups() {
			edgeSet := make(map[string]bool, len(g.edges))
			for _, t := range g.edges {
				edgeSet[t] = true
			}
			seen := make(map[string]bool, len(roots))
			frontier := make([]string, 0, len(roots))
			for _, r := range roots {
				if !seen[r] {
					seen[r] = true
					frontier = append(frontier, r)
				}
			}
			for d := 0; d < depth; d++ {
				if len(frontier) == 0 {
					break
				}
				var next []string
				for _, fid := range frontier {
					for _, e := range inEdges[fid] {
						if !edgeSet[string(e.Type)] {
							continue
						}
						dedupEdges[edgeKey(e)] = e
						if !seen[e.Src] {
							seen[e.Src] = true
							next = append(next, e.Src)
						}
					}
				}
				frontier = next
			}
			reached := groupReached[g.key]
			for id := range seen {
				if seedIDs[id] {
					continue
				}
				if n, ok := nodeByID[id]; ok {
					reached[id] = n
					dedupNodes[id] = n
				}
			}
		}
	}

	for _, g := range groups() {
		reached := groupReached[g.key]
		// Sort reached nodes by qname (tiebreak id) BEFORE projecting into the
		// response shape — Go map iteration is random, and without this the
		// same seed yields different bucket ordering across calls, breaking
		// prompt cache reuse.
		sortedNodes := make([]types.Node, 0, len(reached))
		for _, n := range reached {
			sortedNodes = append(sortedNodes, n)
		}
		sort.Slice(sortedNodes, func(i, j int) bool {
			if sortedNodes[i].QualifiedName != sortedNodes[j].QualifiedName {
				return sortedNodes[i].QualifiedName < sortedNodes[j].QualifiedName
			}
			return sortedNodes[i].ID < sortedNodes[j].ID
		})

		bucket := make([]map[string]any, 0, len(sortedNodes))
		for _, n := range sortedNodes {
			bucket = append(bucket, nodeToImpactEntry(store, n, opt.IncludeBlobs, &warnings))
		}
		groupOut[g.key] = groupResult{nodes: bucket, count: len(bucket)}
	}

	// Edge triples — keep them compact (Type, Src, Dst, Line) to match
	// the existing find_callers / get_subgraph envelope without bloating
	// the response. Sort by (type, src, dst, line) so the same graph
	// always produces the same JSON.
	sortedEdges := make([]types.Edge, 0, len(dedupEdges))
	for _, e := range dedupEdges {
		sortedEdges = append(sortedEdges, e)
	}
	sort.Slice(sortedEdges, func(i, j int) bool {
		a, b := sortedEdges[i], sortedEdges[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Src != b.Src {
			return a.Src < b.Src
		}
		if a.Dst != b.Dst {
			return a.Dst < b.Dst
		}
		return a.Line < b.Line
	})
	edgeTriples := make([][]any, 0, len(sortedEdges))
	for _, e := range sortedEdges {
		edgeTriples = append(edgeTriples, []any{e.Src, e.Dst, string(e.Type), e.Line})
	}

	// Sort warnings by node_id so the metadata block is also stable.
	sort.Slice(warnings, func(i, j int) bool {
		ai, _ := warnings[i]["node_id"].(string)
		bj, _ := warnings[j]["node_id"].(string)
		return ai < bj
	})

	byGroup := map[string]int{}
	impact := map[string]any{}
	for _, g := range groups() {
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

	if primary != nil {
		resp["seed"] = seedSummary(*primary)
	}
	if seedFile != "" && seedQname == "" {
		// Sort seeds by id so the multi-rooted echo is stable across
		// calls (NodesByFilePath ordering is implementation-defined).
		sortedSeeds := make([]types.Node, len(seeds))
		copy(sortedSeeds, seeds)
		sort.Slice(sortedSeeds, func(i, j int) bool {
			return sortedSeeds[i].ID < sortedSeeds[j].ID
		})
		seedList := make([]map[string]any, 0, len(sortedSeeds))
		for _, s := range sortedSeeds {
			seedList = append(seedList, seedSummary(s))
		}
		resp["seeds"] = seedList
		resp["seed_file"] = seedFile
	}

	return resp, nil
}

// resolveSeeds returns the seed node set and (when resolvable to a single
// primary node) a pointer to that node. seed_qname takes precedence; when
// only seed_file is given, every node in the file becomes a seed.
//
// Returns (nil, nil, nil) when nothing matched — the caller surfaces this
// as `not_found: true` in the response.
func resolveSeeds(store persist.StoreReader, seedQname, seedFile string) ([]types.Node, *types.Node, error) {
	if seedQname != "" {
		nodes, err := store.FindSymbol(seedQname, true, persist.FindSymbolOptions{})
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
// records a metadata warning so consumers still know the node exists but
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
// into one edge — that information is load-bearing for the consumer
// picking which line to read first.
func edgeKey(e types.Edge) string {
	return string(e.Type) + "|" + e.Src + "|" + e.Dst + "|" + itoa(e.Line)
}

// itoa is a tiny base-10 int formatter so we don't pull strconv into a hot
// path that already sits behind SQLite latency. Identical to the helper in
// smartctx (kept duplicated to avoid an internal-cross-package import for
// a one-liner).
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
