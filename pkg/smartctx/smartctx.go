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

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/bm25"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Options bundles the tunable knobs of BuildContext. Zero values are
// resolved to documented defaults inside BuildContext so callers can
// pass an empty struct for the typical case.
type Options struct {
	BudgetTokens int  // default 8000
	IncludeBlobs bool // mcp default true; eval may set false
	MaxBodies    int  // default 5
}

func (o Options) withDefaults() Options {
	if o.BudgetTokens <= 0 {
		o.BudgetTokens = 8000
	}
	if o.MaxBodies <= 0 {
		o.MaxBodies = 5
	}
	return o
}

// BuildContext is the shared smart-retrieval algorithm:
//   (a) Search   — top 30 candidates via the store's smart router.
//   (b) Expand   — 1-hop neighbours via QueryEdgesForNodes.
//   (c) Score    — 0.5 BM25 + 0.3 PageRank + 0.2 Usage. BM25 uses pkg/bm25
//                  Okapi with code-aware tokenization (replaces the old
//                  1/(rank+1) placeholder).
//   (d) Diversify — V0: top-30 cap. Per-cluster diversity is V1+.
//   (e) Pack     — top MaxBodies get full source; next ≤15 get sig+doc.
//   (f) Cite     — every emitted item gets file_path + start_line. Items
//                  missing either generate a warning record so callers
//                  can audit citation coverage.
func BuildContext(store persist.StoreReader, query string, opt Options) (map[string]any, error) {
	opt = opt.withDefaults()

	cands, err := store.Search(query, 30)
	if err != nil {
		return nil, err
	}
	if len(cands) == 0 {
		return map[string]any{
			"task_description": query,
			"subgraph":         nil,
			"bodies":           nil,
			"summaries":        nil,
			"tokens_estimated": estimateTokens(query),
			"trimmed":          false,
			"not_found":        true,
			"metadata":         map[string]any{"warnings": []map[string]any{}},
		}, nil
	}

	// (b) Expand: 1-hop traversal
	ids := make([]string, 0, len(cands))
	for _, n := range cands {
		ids = append(ids, n.ID)
	}
	moreEdges, _ := store.QueryEdgesForNodes(ids)
	expSet := map[string]struct{}{}
	for _, e := range moreEdges {
		expSet[e.Src] = struct{}{}
		expSet[e.Dst] = struct{}{}
	}
	for _, id := range ids {
		expSet[id] = struct{}{}
	}
	expanded, _ := store.NodesByIDs(setKeys(expSet))

	// (c) Score
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
	type row struct {
		n     types.Node
		score float64
	}
	rows := make([]row, 0, len(expanded))
	for _, n := range expanded {
		s := 0.5*bm25Norm[n.ID] + 0.3*(n.PageRank/maxPR) + 0.2*(n.UsageScore/maxUS)
		rows = append(rows, row{n: n, score: s})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		return rows[i].n.ID < rows[j].n.ID
	})

	// (d) Diversify (V0 simple cap)
	if len(rows) > 30 {
		rows = rows[:30]
	}

	// (e) Pack within budget + (f) Cite
	warnings := []map[string]any{}
	bodies := []map[string]any{}
	summaries := []map[string]any{}
	tokens := estimateTokens(query)

	for i, r := range rows {
		cite, ok := citationFor(r.n)
		if !ok {
			warnings = append(warnings, map[string]any{
				"code":    "missing-citation",
				"node_id": r.n.ID,
				"qname":   r.n.QualifiedName,
				"message": "node lacks file_path or start_line; LLM cannot cite this snippet",
			})
		}
		if i < opt.MaxBodies && opt.IncludeBlobs {
			b, err := store.GetBlob(r.n.ID)
			if err == nil {
				cost := estimateTokens(string(b))
				if tokens+cost > opt.BudgetTokens {
					break
				}
				body := map[string]any{
					"id":     r.n.ID,
					"qname":  r.n.QualifiedName,
					"source": string(b),
				}
				if ok {
					body["citation"] = cite
					body["file_path"] = r.n.FilePath
					body["start_line"] = r.n.StartLine
				}
				bodies = append(bodies, body)
				tokens += cost
				continue
			}
		}
		if len(summaries) >= 15 {
			continue
		}
		summary := map[string]any{
			"id":        r.n.ID,
			"qname":     r.n.QualifiedName,
			"signature": r.n.Signature,
			"doc":       r.n.DocComment,
		}
		if ok {
			summary["citation"] = cite
			summary["file_path"] = r.n.FilePath
			summary["start_line"] = r.n.StartLine
		}
		cost := estimateTokens(r.n.Signature + " " + r.n.DocComment)
		if tokens+cost > opt.BudgetTokens {
			continue
		}
		summaries = append(summaries, summary)
		tokens += cost
	}

	keptIDs := map[string]struct{}{}
	for _, r := range rows {
		keptIDs[r.n.ID] = struct{}{}
	}
	adj := [][]string{}
	for _, e := range moreEdges {
		if _, ok := keptIDs[e.Src]; !ok {
			continue
		}
		if _, ok := keptIDs[e.Dst]; !ok {
			continue
		}
		adj = append(adj, []string{e.Src, e.Dst, string(e.Type)})
	}
	subgraphNodes := make([]map[string]any, len(rows))
	for i, r := range rows {
		entry := map[string]any{
			"id":    r.n.ID,
			"name":  r.n.Name,
			"type":  r.n.Type,
			"qname": r.n.QualifiedName,
			"score": r.score,
		}
		if cite, ok := citationFor(r.n); ok {
			entry["citation"] = cite
			entry["file_path"] = r.n.FilePath
			entry["start_line"] = r.n.StartLine
		}
		subgraphNodes[i] = entry
	}

	return map[string]any{
		"task_description": query,
		"subgraph": map[string]any{
			"nodes": subgraphNodes,
			"edges": adj,
		},
		"bodies":           bodies,
		"summaries":        summaries,
		"tokens_estimated": tokens,
		"trimmed":          tokens >= opt.BudgetTokens,
		"metadata": map[string]any{
			"warnings": warnings,
		},
	}, nil
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
