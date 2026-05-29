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
type Options struct {
	BudgetTokens int  // default 8000
	IncludeBlobs bool // mcp default true; eval may set false
	MaxBodies    int  // default 5

	// IncludePRs attaches up to PRsPerNode breadcrumbs to each body
	// entry (the "왜" history landed in P0 #1). Off by default so
	// the eval δ baseline's measurement remains comparable to its
	// pre-2026-05-29 runs.
	IncludePRs  bool
	PRsPerNode  int       // default 3
	PRCutoff    time.Time // zero = no cutoff (return full history)

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
	if o.PRsPerNode <= 0 {
		o.PRsPerNode = 3
	}
	if o.ImpactDepth <= 0 {
		o.ImpactDepth = 1
	}
	return o
}

// Pipeline tuning knobs. Extracted so call sites and tests can reason
// about each stage's bound independently.
const (
	candidateSearchLimit = 30 // (a) Search top-N from FTS+CJK router
	maxRowsAfterRank     = 30 // (d) Diversify cap on the ranked set
	maxSummaries         = 15 // (e) Pack cap on signature/doc entries
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
//	(a) Search   — top candidateSearchLimit via the store's smart router.
//	(b) Expand   — 1-hop neighbours via QueryEdgesForNodes.
//	(c) Score    — 0.5 BM25 + 0.3 PageRank + 0.2 Usage.
//	(d) Diversify — V0: maxRowsAfterRank cap. Per-cluster diversity is V1+.
//	(e) Pack     — top MaxBodies get full source; next ≤maxSummaries get
//	               sig+doc.
//	(f) Cite     — every emitted item gets file_path + start_line.
//	               Items missing either generate a warning record.
func BuildContext(store persist.StoreReader, query string, opt Options) (map[string]any, error) {
	opt = opt.withDefaults()

	cands, err := store.Search(query, candidateSearchLimit)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return emptyResult(query), nil
	}

	expanded, edges := expandOneHop(store, cands)
	rows := rankCandidates(query, expanded)
	if len(rows) > maxRowsAfterRank {
		rows = rows[:maxRowsAfterRank]
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

// expandOneHop performs the (b) Expand step: gather every node id
// touched by an edge incident on the candidate set, then re-fetch the
// full node payloads. Returns the expanded node slice plus the edges
// (needed later by buildSubgraphView).
func expandOneHop(store persist.StoreReader, cands []types.Node) ([]types.Node, []types.Edge) {
	ids := make([]string, 0, len(cands))
	for _, n := range cands {
		ids = append(ids, n.ID)
	}
	edges, _ := store.QueryEdgesForNodes(ids)
	expSet := make(map[string]struct{}, len(ids)+2*len(edges))
	for _, id := range ids {
		expSet[id] = struct{}{}
	}
	for _, e := range edges {
		expSet[e.Src] = struct{}{}
		expSet[e.Dst] = struct{}{}
	}
	expanded, _ := store.NodesByIDs(setKeys(expSet))
	return expanded, edges
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

		// (e2) Fall back to signature+doc summary, capped at maxSummaries.
		if len(summaries) >= maxSummaries {
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
