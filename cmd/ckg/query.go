// cmd/ckg/query.go — `ckg query "<question>"` answers a free-form
// question by searching for matching symbols + their k-hop graph
// neighbourhood, then rendering a compact markdown answer with cited
// symbols and source locations. LLM-free — pure structural retrieval,
// suitable for piping into an agent that wants a token-bounded brief
// of the relevant code surface.
//
// Inspired by graphify's `graphify query "..."` (analyze.suggest_questions
// + manual BFS), but explicitly keyword-driven rather than NLU. The
// query engine: (1) tokenise the question, (2) score every node by
// keyword overlap on Name + QualifiedName, (3) BFS from the top-K seeds
// with a hop budget, (4) render the visited subgraph as markdown +
// citations.
//
// Token budget is rough — the renderer trims the visited set when the
// estimated token count would exceed --budget (graphify default 2000).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func newQueryCmd() *cobra.Command {
	var graph string
	var budget, depth, seeds int
	cmd := &cobra.Command{
		Use:   "query <question>",
		Short: "Answer a free-form question with graph-cited symbols (LLM-free)",
		Long: `Run a keyword search over node names + qualified names, BFS k hops
from the top-scoring seeds, and render the visited subgraph as
markdown with file:line citations. The answer is bounded by --budget
tokens so the output is suitable for piping into an agent prompt.

Examples:

  ckg query --graph /tmp/ckg-self "what calls NewBlockChain"
  ckg query --graph /tmp/ckg-self "how does the snap sync downloader handle missing trie nodes"

The retrieval is purely structural — the question's words are matched
against symbol identifiers, NOT semantic embeddings. Phrase questions
in terms of code symbols you want to navigate (function names, type
names, package fragments) for best results.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			question := strings.Join(args, " ")
			db := filepath.Join(graph, "graph.db")
			store, err := persist.OpenReadOnly(db)
			if err != nil {
				return fmt.Errorf("open graph: %w", err)
			}
			defer func() { _ = store.Close() }()

			nodes, err := store.AllNodes()
			if err != nil {
				return fmt.Errorf("nodes: %w", err)
			}
			edges, err := store.AllEdges()
			if err != nil {
				return fmt.Errorf("edges: %w", err)
			}

			tokens := tokeniseQuestion(question)
			if len(tokens) == 0 {
				return fmt.Errorf("question has no usable tokens (after stop-word strip)")
			}
			seedNodes := scoreAndPickSeeds(nodes, tokens, seeds)
			if len(seedNodes) == 0 {
				fmt.Println("No matching symbols found. Try rephrasing with literal type/function names.")
				return nil
			}
			adj := buildAdjacency(edges)
			edgeIdx := buildEdgeIndex(edges)
			renderQueryAnswer(os.Stdout, question, seedNodes, nodes, adj, edgeIdx, depth, budget)
			return nil
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().IntVar(&budget, "budget", 2000,
		"approximate token budget for the rendered answer (graphify default; chars/4)")
	cmd.Flags().IntVar(&depth, "depth", 2, "BFS hop depth from each seed (2 covers most call/define hubs without fan-out)")
	cmd.Flags().IntVar(&seeds, "seeds", 5, "number of top-scoring seed nodes to expand from")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}

// queryStopwords are the common English filler words that would dominate
// keyword scoring without contributing actual signal. Conservative list —
// retrieval still works on questions that contain them; this just stops
// "the / how / does" from outranking real symbol fragments.
var queryStopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "do": true, "does": true, "for": true, "from": true,
	"how": true, "i": true, "if": true, "in": true, "is": true, "it": true,
	"of": true, "on": true, "or": true, "the": true, "this": true, "to": true,
	"what": true, "when": true, "where": true, "which": true, "who": true,
	"why": true, "with": true, "you": true, "your": true,
}

// tokeniseQuestion lower-cases the input, splits on non-alphanumerics,
// and drops stopwords + 1-character tokens. The remaining tokens are the
// signal-carrying fragments scored against symbol names.
func tokeniseQuestion(q string) []string {
	q = strings.ToLower(q)
	out := []string{}
	cur := strings.Builder{}
	flush := func() {
		s := cur.String()
		cur.Reset()
		if len(s) <= 1 {
			return
		}
		if queryStopwords[s] {
			return
		}
		out = append(out, s)
	}
	for _, r := range q {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// seedNodeFilter is stricter than godNodeFilter — query seeds must be
// real symbol-level nodes (Function/Method/Type/Struct/Interface/...),
// not statement nodes that just happen to mention the keyword (CallSite
// inside `NewBlockChain` body would otherwise dominate the seed list).
//
// Returns true to EXCLUDE the node from seed selection.
func seedNodeFilter(t types.NodeType, filePath string) bool {
	if godNodeFilter(t, filePath) {
		return true
	}
	switch t {
	case types.NodeCallSite, types.NodeIfStmt, types.NodeLoopStmt,
		types.NodeReturnStmt, types.NodeSwitchStmt,
		types.NodeParameter, types.NodeLocalVariable:
		return true
	}
	return false
}

// noisyEdgeForQuery returns true when an edge type is metadata (not
// semantic flow) and should be excluded from the BFS neighbourhood
// rendering. Mirrors the spirit of edges.ts DEFAULT_EDGE_TYPES — G6
// Temporal edges (changed_in/blame/has_hunk/adjacent) are useful for
// the viewer's history pane but pure noise in an "explain X" query.
// `contains` is hidden too because it overwhelmingly traverses to
// statement-level children (IfStmt / CallSite / ...) which the same
// query would already de-noise via the seed filter.
func noisyEdgeForQuery(t types.EdgeType) bool {
	switch t {
	case types.EdgeChangedIn, types.EdgeBlame, types.EdgeHasHunk, types.EdgeAdjacent,
		types.EdgeContains:
		return true
	}
	return false
}

// scoreAndPickSeeds ranks nodes by keyword-overlap on Name +
// QualifiedName, then returns the top-N. Filters via seedNodeFilter so
// the seed surface is symbol-level, not statement-level.
func scoreAndPickSeeds(nodes []types.Node, tokens []string, n int) []types.Node {
	type scored struct {
		node  types.Node
		score int
	}
	matches := []scored{}
	for _, nd := range nodes {
		if seedNodeFilter(nd.Type, nd.FilePath) {
			continue
		}
		score := 0
		nameLow := strings.ToLower(nd.Name)
		qnLow := strings.ToLower(nd.QualifiedName)
		for _, t := range tokens {
			if strings.Contains(nameLow, t) {
				score += 3 // name match weighs heavier than qualifier
			}
			if strings.Contains(qnLow, t) {
				score++
			}
		}
		if score > 0 {
			matches = append(matches, scored{nd, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].node.PageRank > matches[j].node.PageRank
	})
	if n > len(matches) {
		n = len(matches)
	}
	out := make([]types.Node, n)
	for i := 0; i < n; i++ {
		out[i] = matches[i].node
	}
	return out
}

// edgeIndex maps (src, dst) → edge so the renderer can label the
// traversal with the actual relation type rather than guessing.
type edgeIndex map[string]types.Edge

func buildEdgeIndex(edges []types.Edge) edgeIndex {
	idx := make(edgeIndex, len(edges))
	for _, e := range edges {
		idx[e.Src+"\x00"+e.Dst] = e
		// Reverse lookup for undirected display.
		if _, ok := idx[e.Dst+"\x00"+e.Src]; !ok {
			idx[e.Dst+"\x00"+e.Src] = e
		}
	}
	return idx
}

func renderQueryAnswer(w *os.File, question string,
	seeds []types.Node, allNodes []types.Node,
	adj map[string][]string, eidx edgeIndex,
	depth, tokenBudget int) {
	idx := make(map[string]types.Node, len(allNodes))
	for _, n := range allNodes {
		idx[n.ID] = n
	}
	_, _ = fmt.Fprintf(w, "## Query: %s\n\n", question)
	_, _ = fmt.Fprintf(w, "Seeds (top-%d by keyword score):\n\n", len(seeds))
	for i, s := range seeds {
		_, _ = fmt.Fprintf(w, "%d. **`%s`** [%s] · `%s` · %s:%d\n",
			i+1, s.Name, s.Type, s.QualifiedName, s.FilePath, s.StartLine)
	}
	_, _ = fmt.Fprintln(w)

	visited := make(map[string]bool, 128)
	for _, s := range seeds {
		visited[s.ID] = true
	}
	frontier := make([]string, 0, len(seeds))
	for _, s := range seeds {
		frontier = append(frontier, s.ID)
	}
	tokens := 0
	rendered := strings.Builder{}
	rendered.WriteString("### Neighbourhood (BFS)\n\n")
bfs:
	for d := 0; d < depth && len(frontier) > 0; d++ {
		next := make([]string, 0, len(frontier))
		for _, src := range frontier {
			cur := idx[src]
			for _, nb := range adj[src] {
				if visited[nb] {
					continue
				}
				e := eidx[src+"\x00"+nb]
				if noisyEdgeForQuery(e.Type) {
					continue // skip G6 Temporal edges in the answer
				}
				visited[nb] = true
				other := idx[nb]
				if other.ID == "" {
					continue
				}
				line := fmt.Sprintf("- `%s` —[%s]→ `%s` · %s:%d\n",
					cur.Name, e.Type, other.Name, other.FilePath, other.StartLine)
				if (rendered.Len()+len(line))/charsPerToken > tokenBudget {
					rendered.WriteString("- _… token budget reached; rerun with --budget=N for more._\n")
					tokens = rendered.Len() / charsPerToken
					_, _ = w.WriteString(rendered.String())
					_, _ = fmt.Fprintf(w, "\n_~%d tokens, %d nodes visited_\n", tokens, len(visited))
					return
				}
				rendered.WriteString(line)
				next = append(next, nb)
			}
		}
		frontier = next
		if len(visited) >= 1000 {
			rendered.WriteString("- _… 1000-node visited cap reached._\n")
			break bfs
		}
	}
	tokens = rendered.Len() / charsPerToken
	_, _ = w.WriteString(rendered.String())
	_, _ = fmt.Fprintf(w, "\n_~%d tokens, %d nodes visited_\n", tokens, len(visited))
}
