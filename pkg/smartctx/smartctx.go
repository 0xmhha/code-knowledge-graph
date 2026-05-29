// Package smartctx is the shared "smart 1-shot retrieval" implementation
// used by both internal/mcp.get_context_for_task and internal/eval's δ
// baseline. Before this package existed the two callers had separate
// algorithms — the MCP path was a 50-line BM25/PR/usage fusion, the eval
// δ was `SearchFTS top-10 dump`. The asymmetry meant eval H1/H2 hypotheses
// did not measure what MCP actually returns to LLMs.
//
// BuildContext is now the single source of truth. Callers serialize the
// returned Pack however they prefer (mcp wraps it in mcp.NewToolResult;
// eval encodes to JSON and embeds in the LLM prompt).
//
// Citation Enforcement (warn mode): every body/summary/subgraph node
// includes file_path + start_line. Nodes that lack either are kept in
// the response (to preserve recall) but recorded under
// `metadata.warnings` with code "missing-citation". A future strict mode
// will drop those nodes outright once the warn-mode signal proves stable.
package smartctx

import (
	"sort"
	"time"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/bm25"
	"github.com/0xmhha/code-knowledge-graph/pkg/impact"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Options bundles the tunable knobs of BuildContext. Zero values are
// resolved to documented defaults inside BuildContext so callers can
// pass an empty struct for the typical case.
//
// The IncludePRs / IncludeImpact flags drive P0 #2 — the 1-shot
// retrieval surface from docs/PROJECT-BLUEPRINT-ALIGNMENT.md §4.2.
// Both default to false so existing callers (eval δ baseline, MCP
// pre-merge consumers) see the same output shape they had before;
// the new keys (recent_prs / impact) only appear when the caller
// explicitly opts in.
//
// CandidateLimit / RankedCap / MaxSummaries (P0 #3) expose the
// pipeline width knobs that were previously baked into private
// package constants. Bumping them widens recall at a roughly linear
// O(N) cost — the Stage B 2026-05-29 measurement found that the
// 30-candidate cap was leaving δ score at 76% of β's. The new
// defaults (100 / 50 / 25) close most of that gap on the eval
// fixture without changing the algorithm itself.
type Options struct {
	BudgetTokens int  // default 8000
	IncludeBlobs bool // mcp default true; eval may set false
	MaxBodies    int  // default 5

	// CandidateLimit caps the initial Search() top-N. Bumped from
	// 30 to 100 in P0 #3 — the prior cap was the dominant recall
	// bottleneck in the Stage B 2026-05-29 measurement. Override
	// to a smaller value when callers want a tight, fast response.
	CandidateLimit int // default 100

	// RankedCap caps the size of the per-query ranked slice that
	// reaches the packer. Must be >= MaxBodies + a small headroom
	// so the summary tier has rows to choose from. Default 50.
	RankedCap int // default 50

	// MaxSummaries caps signature/doc entries emitted alongside the
	// MaxBodies full sources. Default 25.
	MaxSummaries int

	// HopDepth controls how many edge hops the expand stage walks
	// out from the seed candidate set. 1 (default) preserves the
	// original O(candidates · avgFanout) cost. 2 enables a second
	// BFS hop — useful when the query's relevant context lives two
	// edges away (caller-of-caller, type-of-field-type). Bounded
	// at HopFrontierCap per hop to prevent fan-out explosions on
	// hub nodes. P2 #7 follow-up to the P0 #3 widening.
	HopDepth int

	// HopFrontierCap caps the per-hop frontier size when HopDepth
	// > 1. Without it a hub node like a generic "Logger" symbol
	// could pull in tens of thousands of callers at hop 2 and
	// blow the rank/pack budget. Default 200 — wide enough that
	// the BM25 re-ranker still has a meaningful set to discriminate
	// from, narrow enough that the per-hop store.NodesByIDs round
	// trip stays bounded.
	HopFrontierCap int

	// IncludePRs attaches up to PRsPerNode breadcrumbs to each body
	// entry (the "왜" history landed in P0 #1). Off by default so
	// the eval δ baseline's measurement remains comparable to its
	// pre-2026-05-29 runs.
	IncludePRs bool
	PRsPerNode int       // default 3
	PRCutoff   time.Time // zero = no cutoff (return full history)

	// IncludeImpact runs pkg/impact.Compute against the highest-
	// scoring kept node (rows[0]) so the agent gets reverse-deps in
	// the same response. Off by default — adds an O(impact.groups
	// × depth) traversal that's only worth paying for when the
	// caller actually wants impact info.
	IncludeImpact bool
	ImpactDepth   int // default 1; clamped by pkg/impact internally
}

func (o Options) withDefaults() Options {
	if o.BudgetTokens <= 0 {
		o.BudgetTokens = 8000
	}
	if o.MaxBodies <= 0 {
		o.MaxBodies = 5
	}
	if o.CandidateLimit <= 0 {
		o.CandidateLimit = defaultCandidateLimit
	}
	if o.RankedCap <= 0 {
		o.RankedCap = defaultRankedCap
	}
	if o.MaxSummaries <= 0 {
		o.MaxSummaries = defaultMaxSummaries
	}
	if o.HopDepth <= 0 {
		o.HopDepth = defaultHopDepth
	}
	if o.HopDepth > maxHopDepth {
		o.HopDepth = maxHopDepth
	}
	if o.HopFrontierCap <= 0 {
		o.HopFrontierCap = defaultHopFrontierCap
	}
	if o.PRsPerNode <= 0 {
		o.PRsPerNode = 3
	}
	if o.ImpactDepth <= 0 {
		o.ImpactDepth = 1
	}
	return o
}

// Pipeline tuning knobs. Defaults are exposed as Options fields
// (CandidateLimit / RankedCap / MaxSummaries) so callers can shrink
// the pipeline for tight responses; the constants below are the
// values that withDefaults applies when the Options field is unset.
//
// 2026-05-29 (P0 #3): the defaults were bumped from the historical
// 30/30/15 values found by the Stage B measurement to leave δ score
// at 0.335 — 76% of β's 0.441. Per docs/PROJECT-BLUEPRINT-ALIGNMENT.md
// §4.2 the recall ceiling was the 30-candidate cap; widening to 100
// and letting more rows reach packing closes most of that gap on
// the eval fixture without changing the algorithm itself.
const (
	defaultCandidateLimit = 100 // (a) Search top-N from FTS+CJK router
	defaultRankedCap      = 50  // (d) Diversify cap on the ranked set
	defaultMaxSummaries   = 25  // (e) Pack cap on signature/doc entries

	// Hop-depth knobs for (b) Expand. defaultHopDepth=1 preserves
	// the historical 1-hop behaviour. maxHopDepth=3 clamps over-
	// eager callers since hop ≥ 4 quickly degenerates into "the
	// whole reachable graph" on dense codebases and provides no
	// useful retrieval signal beyond what BM25 ranking already
	// surfaces. defaultHopFrontierCap=200 caps the per-hop
	// expansion so a single hub node (e.g. a Logger) can't pull
	// in tens of thousands of callers at hop 2.
	defaultHopDepth       = 1
	maxHopDepth           = 3
	defaultHopFrontierCap = 200
)

// scoredNode pairs a node with its composite relevance score.
// Internal to BuildContext; kept package-scoped so the staged
// helpers below share a type without re-declaring.
type scoredNode struct {
	node  types.Node
	score float64
}

// BuildContext is the shared smart-retrieval algorithm:
//
//	(a) Search   — top opt.CandidateLimit via the store's smart router.
//	(b) Expand   — opt.HopDepth-hop BFS via QueryEdgesForNodes, each
//	               hop capped at opt.HopFrontierCap new nodes.
//	(c) Score    — 0.5 BM25 + 0.3 PageRank + 0.2 Usage.
//	(d) Diversify — V0: opt.RankedCap. Per-cluster diversity is V1+.
//	(e) Pack     — top MaxBodies get full source; next ≤MaxSummaries get
//	               sig+doc.
//	(f) Cite     — every emitted item gets file_path + start_line.
//	               Items missing either generate a warning record.
func BuildContext(store persist.StoreReader, query string, opt Options) (map[string]any, error) {
	opt = opt.withDefaults()

	cands, err := store.Search(query, opt.CandidateLimit)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return emptyResult(query), nil
	}

	expanded, edges := expandToDepth(store, cands, opt.HopDepth, opt.HopFrontierCap)
	rows := rankCandidates(query, expanded)
	if len(rows) > opt.RankedCap {
		rows = rows[:opt.RankedCap]
	}

	bodies, summaries, warnings, tokens := packWithinBudget(store, rows, query, opt)
	subgraph := buildSubgraphView(rows, edges)

	out := map[string]any{
		"task_description": query,
		"subgraph":         subgraph,
		"bodies":           bodies,
		"summaries":        summaries,
		"tokens_estimated": tokens,
		"trimmed":          tokens >= opt.BudgetTokens,
		"metadata": map[string]any{
			"warnings": warnings,
		},
	}

	if opt.IncludePRs {
		out["recent_prs"] = attachRecentPRs(store, bodies, opt)
	}
	if opt.IncludeImpact && len(rows) > 0 {
		out["impact"] = computeImpactForPrimary(store, rows[0].node, opt)
	}
	return out, nil
}

// emptyResult is the canonical "search returned no candidates" payload.
// Kept as a helper so the empty-path shape stays consistent with the
// success-path shape (same keys, zero-valued where applicable).
func emptyResult(query string) map[string]any {
	return map[string]any{
		"task_description": query,
		"subgraph":         nil,
		"bodies":           nil,
		"summaries":        nil,
		"tokens_estimated": estimateTokens(query),
		"trimmed":          false,
		"not_found":        true,
		"metadata":         map[string]any{"warnings": []map[string]any{}},
	}
}

// expandToDepth performs the (b) Expand step over `depth` BFS hops.
// depth=1 is the historical behaviour: one round of QueryEdgesForNodes
// over the seed candidates, then a NodesByIDs round-trip to re-hydrate
// the touched nodes. depth>1 takes the newly-discovered ids from the
// previous hop's edges and runs QueryEdgesForNodes against them,
// repeating until depth or until no new ids appear.
//
// frontierCap bounds the per-hop frontier so a hub node (Logger,
// common error type, …) can't pull tens of thousands of callers in
// at hop 2 and starve the rank/pack stage. The cap is applied
// AFTER the seen-set dedupe — already-visited ids never enter the
// frontier — so the bound is on "new nodes introduced this hop",
// not on raw edge count.
//
// The accumulated edges slice carries every edge touched across all
// hops, in the order they were discovered, so buildSubgraphView's
// keptIDs filter naturally prunes the cross-hop noise without an
// extra dedupe pass.
func expandToDepth(store persist.StoreReader, cands []types.Node, depth, frontierCap int) ([]types.Node, []types.Edge) {
	seen := make(map[string]struct{}, len(cands))
	frontier := make([]string, 0, len(cands))
	for _, n := range cands {
		if _, dup := seen[n.ID]; dup {
			continue
		}
		seen[n.ID] = struct{}{}
		frontier = append(frontier, n.ID)
	}
	var allEdges []types.Edge

	for hop := 0; hop < depth && len(frontier) > 0; hop++ {
		edges, _ := store.QueryEdgesForNodes(frontier)
		allEdges = append(allEdges, edges...)

		next := make([]string, 0, len(edges))
		for _, e := range edges {
			for _, id := range [2]string{e.Src, e.Dst} {
				if _, dup := seen[id]; dup {
					continue
				}
				seen[id] = struct{}{}
				next = append(next, id)
			}
		}
		if len(next) > frontierCap {
			next = next[:frontierCap]
		}
		frontier = next
	}

	expanded, _ := store.NodesByIDs(setKeys(seen))
	return expanded, allEdges
}

// rankCandidates performs the (c) Score step: compute the composite
// 0.5·BM25 + 0.3·PageRank + 0.2·Usage score per node, normalise PR and
// Usage to their per-set max, then sort by score desc with ID as
// tiebreak. Returns the full ranked slice; the cap at maxRowsAfterRank
// happens in the caller so tests can inspect the full ranking.
func rankCandidates(query string, expanded []types.Node) []scoredNode {
	bm25Norm := scoreWithBM25(query, expanded)
	maxPR, maxUS := 1e-9, 1e-9
	for _, n := range expanded {
		if n.PageRank > maxPR {
			maxPR = n.PageRank
		}
		if n.UsageScore > maxUS {
			maxUS = n.UsageScore
		}
	}
	rows := make([]scoredNode, 0, len(expanded))
	for _, n := range expanded {
		s := 0.5*bm25Norm[n.ID] + 0.3*(n.PageRank/maxPR) + 0.2*(n.UsageScore/maxUS)
		rows = append(rows, scoredNode{node: n, score: s})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		return rows[i].node.ID < rows[j].node.ID
	})
	return rows
}

// packWithinBudget performs (e) Pack + (f) Cite: top opt.MaxBodies
// rows get full source bodies (subject to budget), the next up to
// maxSummaries get signature+doc summaries. Every emitted item is
// citation-augmented; rows lacking file_path/start_line produce a
// "missing-citation" warning instead of dropping silently.
func packWithinBudget(store persist.StoreReader, rows []scoredNode, query string, opt Options) (
	bodies []map[string]any,
	summaries []map[string]any,
	warnings []map[string]any,
	tokens int,
) {
	bodies = []map[string]any{}
	summaries = []map[string]any{}
	warnings = []map[string]any{}
	tokens = estimateTokens(query)

	for i, r := range rows {
		cite, hasCite := citationFor(r.node)
		if !hasCite {
			warnings = append(warnings, map[string]any{
				"code":    "missing-citation",
				"node_id": r.node.ID,
				"qname":   r.node.QualifiedName,
				"message": "node lacks file_path or start_line; LLM cannot cite this snippet",
			})
		}

		// (e1) Try a full-source body first if we're under MaxBodies.
		if i < opt.MaxBodies && opt.IncludeBlobs {
			b, err := store.GetBlob(r.node.ID)
			if err == nil {
				cost := estimateTokens(string(b))
				if tokens+cost > opt.BudgetTokens {
					break
				}
				bodies = append(bodies, bodyEntry(r.node, string(b), cite, hasCite))
				tokens += cost
				continue
			}
		}

		// (e2) Fall back to signature+doc summary, capped at MaxSummaries.
		if len(summaries) >= opt.MaxSummaries {
			continue
		}
		cost := estimateTokens(r.node.Signature + " " + r.node.DocComment)
		if tokens+cost > opt.BudgetTokens {
			continue
		}
		summaries = append(summaries, summaryEntry(r.node, cite, hasCite))
		tokens += cost
	}
	return
}

// bodyEntry builds the "full source body" payload for a packed row.
func bodyEntry(n types.Node, source, cite string, hasCite bool) map[string]any {
	body := map[string]any{
		"id":     n.ID,
		"qname":  n.QualifiedName,
		"source": source,
	}
	if hasCite {
		body["citation"] = cite
		body["file_path"] = n.FilePath
		body["start_line"] = n.StartLine
	}
	return body
}

// summaryEntry builds the "signature + doc" payload for a packed row.
func summaryEntry(n types.Node, cite string, hasCite bool) map[string]any {
	summary := map[string]any{
		"id":        n.ID,
		"qname":     n.QualifiedName,
		"signature": n.Signature,
		"doc":       n.DocComment,
	}
	if hasCite {
		summary["citation"] = cite
		summary["file_path"] = n.FilePath
		summary["start_line"] = n.StartLine
	}
	return summary
}

// attachRecentPRs is the P0 #2 "왜" history side-channel: for each
// packed body entry, fetch up to opt.PRsPerNode PR breadcrumbs and
// return them keyed by node id. Results are intentionally NOT counted
// against opt.BudgetTokens — the PR summary text is capped at 2 KB
// per row (see internal/buildpipe.bodyExcerptMaxBytes) and the per-
// node cap of PRsPerNode bounds the worst case at 3 × 2 KB = 6 KB
// per body, well under the practical request size.
//
// Why bodies only (no summaries): summaries are the fallback when the
// budget can't afford a full source body — attaching PR breadcrumbs
// to them would inflate the lower-tier rows past their original cost.
// The most valuable "왜" signal is paired with the source the LLM is
// reading anyway.
//
// On store errors (transient SQLite, missing node_prs table on pre-
// 1.12 DBs) we skip the offending row silently — PR breadcrumbs are
// strictly additive metadata; an outage here must not break the main
// retrieval response.
func attachRecentPRs(store persist.StoreReader, bodies []map[string]any, opt Options) map[string][]types.PRRef {
	out := map[string][]types.PRRef{}
	for _, b := range bodies {
		id, _ := b["id"].(string)
		if id == "" {
			continue
		}
		refs, err := store.GetNodePRs(id, opt.PRCutoff)
		if err != nil || len(refs) == 0 {
			continue
		}
		if len(refs) > opt.PRsPerNode {
			refs = refs[:opt.PRsPerNode]
		}
		out[id] = refs
	}
	return out
}

// computeImpactForPrimary runs the impact algorithm against the
// top-ranked node — the closest match to the user's query and the
// most actionable seed for "what does changing this break?" without
// the agent having to make a second tool call. Depth defaults to 1
// (shallower than the standalone impact_of_change tool's depth-2
// default) because the 1-shot envelope already includes the local
// subgraph + source bodies; the impact field exists to surface the
// next ring out, not to repeat what the caller can already see.
//
// Returns the raw impact.Compute output map so callers get the full
// shape (by_group counts, edge triples, totals, metadata). On error
// we return a placeholder map with an error key — same fail-soft
// stance as attachRecentPRs.
func computeImpactForPrimary(store persist.StoreReader, primary types.Node, opt Options) map[string]any {
	if primary.QualifiedName == "" {
		return map[string]any{"skipped": "primary node has no qualified_name"}
	}
	res, err := impact.Compute(store, primary.QualifiedName, "", impact.Options{
		Depth:        opt.ImpactDepth,
		IncludeBlobs: false,
	})
	if err != nil {
		return map[string]any{"error": err.Error(), "seed_qname": primary.QualifiedName}
	}
	return res
}

// buildSubgraphView assembles the JSON subgraph (nodes + edges) for the
// kept row set. Edges crossing outside the kept set are dropped so the
// rendered subgraph is self-contained.
func buildSubgraphView(rows []scoredNode, allEdges []types.Edge) map[string]any {
	keptIDs := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		keptIDs[r.node.ID] = struct{}{}
	}
	adj := [][]string{}
	for _, e := range allEdges {
		if _, ok := keptIDs[e.Src]; !ok {
			continue
		}
		if _, ok := keptIDs[e.Dst]; !ok {
			continue
		}
		adj = append(adj, []string{e.Src, e.Dst, string(e.Type)})
	}
	nodes := make([]map[string]any, len(rows))
	for i, r := range rows {
		entry := map[string]any{
			"id":    r.node.ID,
			"name":  r.node.Name,
			"type":  r.node.Type,
			"qname": r.node.QualifiedName,
			"score": r.score,
		}
		if cite, ok := citationFor(r.node); ok {
			entry["citation"] = cite
			entry["file_path"] = r.node.FilePath
			entry["start_line"] = r.node.StartLine
		}
		nodes[i] = entry
	}
	return map[string]any{
		"nodes": nodes,
		"edges": adj,
	}
}

// citationFor returns "file_path:start_line" and true when the node has
// both fields. Some node kinds (Package, Commit, Endpoint, MessageType)
// legitimately have no file scope — they return false and the caller
// records a warning instead of a citation.
func citationFor(n types.Node) (string, bool) {
	if n.FilePath == "" || n.StartLine <= 0 {
		return "", false
	}
	return n.FilePath + ":" + itoa(n.StartLine), true
}

// estimateTokens is the standard chars/4 heuristic.
func estimateTokens(s string) int { return (len(s) + 3) / 4 }

// scoreWithBM25 builds an ad-hoc BM25 corpus from the expanded candidate
// nodes and returns a docID → normalized BM25 score map (range [0, 1]).
// Tokens combine qualified_name, name, signature, doc_comment, file_path
// so identifier and prose queries both surface relevant nodes.
func scoreWithBM25(query string, expanded []types.Node) map[string]float64 {
	out := make(map[string]float64, len(expanded))
	if len(expanded) == 0 {
		return out
	}
	docs := make([]bm25.Document, 0, len(expanded))
	for _, n := range expanded {
		toks := bm25.Tokenize(n.QualifiedName + " " + n.Name + " " +
			n.Signature + " " + n.DocComment + " " + n.FilePath)
		docs = append(docs, bm25.Document{ID: n.ID, Tokens: toks})
	}
	scorer := bm25.NewOkapi()
	scorer.Index(docs)
	qTokens := bm25.Tokenize(query)
	if len(qTokens) == 0 {
		return out
	}
	maxScore := 0.0
	for _, n := range expanded {
		s := scorer.Score(qTokens, n.ID)
		out[n.ID] = s
		if s > maxScore {
			maxScore = s
		}
	}
	if maxScore > 0 {
		for id, s := range out {
			out[id] = s / maxScore
		}
	}
	return out
}

func setKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
