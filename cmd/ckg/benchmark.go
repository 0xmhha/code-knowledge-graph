// cmd/ckg/benchmark.go — `ckg benchmark` measures the token-reduction
// payoff of using the graph instead of feeding the agent the raw corpus.
// Inspired by graphify's benchmark.run_benchmark (benchmark.py:64-110)
// but adapted to CKG's richer node/edge schema:
//
//   - Corpus baseline:  sum of source-file bytes for the indexed languages,
//     converted to tokens via the standard ~4 chars-per-
//     token heuristic (matches OpenAI/Anthropic averages
//     for English+code; fine for ratio reporting).
//
//   - Graph-query cost: pick the top-N god nodes as proxy "questions",
//     BFS k=3 hops from each, render the subgraph as
//     a compact text answer (`Symbol → Symbol` chains),
//     and count the resulting tokens.
//
//   - Reduction ratio:  corpus_tokens / avg(query_tokens). A 100x ratio
//     means the graph reaches the same answer using
//     1% of the tokens a naive grep-everything pass
//     would consume.
//
// The numbers are approximate by design — point estimates for the
// "is the graph worth the build cost" decision, not precision metrics.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// charsPerToken is the standard rule-of-thumb for English/code tokenisers
// (GPT-style BPE averages 3.5-4.5 chars/token on natural language, ~3
// chars/token on code). 4 keeps the math conservative — it overestimates
// the corpus a bit and underestimates the query, which biases toward
// not-overselling the graph's reduction ratio.
const charsPerToken = 4

func newBenchmarkCmd() *cobra.Command {
	var graph string
	var questions int
	var depth int
	var format string
	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Measure token reduction of graph queries vs reading the raw corpus",
		Long: `Estimate how many tokens an agent saves by querying the graph
instead of grepping the source corpus. Picks the top-N god nodes
as proxy "questions", runs a k-hop BFS from each, renders the
subgraph as a compact text answer, and divides the corpus token
count by the average query token count.

The numbers are rough — the corpus baseline uses 4-chars-per-token
and the query renderer is intentionally simple. Treat the ratio
as a directional indicator ("100x ratio = graph is doing real
work") rather than a precision benchmark.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			db := filepath.Join(graph, "graph.db")
			store, err := persist.OpenReadOnly(db)
			if err != nil {
				return fmt.Errorf("open graph: %w", err)
			}
			defer func() { _ = store.Close() }()

			manifest, err := store.GetManifest()
			if err != nil {
				return fmt.Errorf("manifest: %w", err)
			}
			nodes, err := store.AllNodes()
			if err != nil {
				return fmt.Errorf("nodes: %w", err)
			}
			edges, err := store.AllEdges()
			if err != nil {
				return fmt.Errorf("edges: %w", err)
			}

			corpusTokens := estimateCorpusTokens(manifest.SrcRoot, nodes)
			gods := topPageRank(nodes, questions)
			if len(gods) == 0 {
				_, _ = fmt.Fprintln(os.Stderr,
					"benchmark: no PageRank-ranked nodes found — was the graph built without scoring?")
				return nil
			}

			adj := buildAdjacency(edges)
			perQ := make([]queryStat, 0, len(gods))
			totalQT := 0
			for _, gn := range gods {
				qt := queryTokensForNode(gn, nodes, adj, depth)
				perQ = append(perQ, queryStat{
					name: gn.QualifiedName, tokens: qt, reduction: ratio(corpusTokens, qt),
				})
				totalQT += qt
			}
			avgQT := 0
			if len(perQ) > 0 {
				avgQT = totalQT / len(perQ)
			}
			switch format {
			case "json":
				return emitBenchmarkJSON(os.Stdout, manifest, corpusTokens, avgQT, perQ)
			case "text", "":
				printBenchmark(os.Stdout, manifest, corpusTokens, avgQT, perQ)
				return nil
			default:
				return fmt.Errorf("unknown --format %q (want text|json)", format)
			}
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().IntVar(&questions, "questions", 5,
		"number of top-PageRank god nodes to sample as proxy queries")
	cmd.Flags().IntVar(&depth, "depth", 3,
		"BFS hop depth per query (graphify defaults to 3 — wider blows up the subgraph; narrower under-represents the agent's typical traversal)")
	cmd.Flags().StringVar(&format, "format", "text",
		"output format: text|json. json is the machine-readable form consumed by 'make eval' for baseline diffing.")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}

// benchmarkReport is the JSON-serialisable shape of the benchmark output.
// Stable field names because eval/baseline/*.json diffs against this
// shape across runs — renaming a field would break the regression gate.
type benchmarkReport struct {
	SrcRoot      string             `json:"src_root"`
	SrcCommit    string             `json:"src_commit,omitempty"`
	CorpusTokens int                `json:"corpus_tokens"`
	AvgQueryTok  int                `json:"avg_query_tokens"`
	Reduction    float64            `json:"reduction_ratio"`
	PerQuery     []benchmarkPerQRow `json:"per_query"`
}

type benchmarkPerQRow struct {
	QualifiedName string  `json:"qualified_name"`
	Tokens        int     `json:"tokens"`
	Reduction     float64 `json:"reduction_ratio"`
}

func emitBenchmarkJSON(w *os.File, m persist.Manifest, corpusTokens, avgQT int, perQ []queryStat) error {
	rows := make([]benchmarkPerQRow, len(perQ))
	for i, q := range perQ {
		rows[i] = benchmarkPerQRow{QualifiedName: q.name, Tokens: q.tokens, Reduction: q.reduction}
	}
	report := benchmarkReport{
		SrcRoot:      m.SrcRoot,
		SrcCommit:    m.SrcCommit,
		CorpusTokens: corpusTokens,
		AvgQueryTok:  avgQT,
		Reduction:    ratio(corpusTokens, avgQT),
		PerQuery:     rows,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

type queryStat struct {
	name      string
	tokens    int
	reduction float64
}

// estimateCorpusTokens sums on-disk source bytes for every File node
// whose path is reachable inside SrcRoot. Falls back to summing
// file-anchored Node.QualifiedName lengths × 50 (rough proxy for
// average symbol body) when a file can't be read — keeps the benchmark
// usable on detached graph.db files where SrcRoot moved or vanished.
func estimateCorpusTokens(srcRoot string, nodes []types.Node) int {
	totalBytes := 0
	read := 0
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeFile || n.FilePath == "" {
			continue
		}
		if seen[n.FilePath] {
			continue
		}
		seen[n.FilePath] = true
		full := filepath.Join(srcRoot, n.FilePath)
		st, err := os.Stat(full)
		if err == nil && !st.IsDir() {
			totalBytes += int(st.Size())
			read++
		}
	}
	if read == 0 {
		// Fallback: estimate from symbol cardinality. ~1KB average per
		// non-meta non-file symbol is a coarse but bounded substitute.
		for _, n := range nodes {
			if godNodeFilter(n.Type, n.FilePath) {
				continue
			}
			totalBytes += 1024
		}
	}
	return totalBytes / charsPerToken
}

// buildAdjacency returns id → []neighbourID, undirected. We build it
// once per benchmark invocation and share across all queries — the
// per-question BFS is the hot loop.
func buildAdjacency(edges []types.Edge) map[string][]string {
	adj := make(map[string][]string, len(edges)*2)
	for _, e := range edges {
		adj[e.Src] = append(adj[e.Src], e.Dst)
		adj[e.Dst] = append(adj[e.Dst], e.Src)
	}
	return adj
}

// queryTokensForNode simulates the cost of an "explain X" agent query:
// BFS k hops from the seed, render each visited node + each traversed
// edge as a compact line ("Symbol → Symbol [type] qname"), count tokens.
//
// Visited cap: god nodes can have thousands of immediate neighbours
// (e.g. `Errorf` with ~3500 inbound calls); a depth-3 BFS from such
// a seed without a cap fans out into hundreds of thousands of nodes
// and the "graph query" comes out larger than the original corpus.
// 200 is the empirical sweet spot — enough surface to answer a typical
// "explain X" prompt, small enough that the ratio actually reflects
// real agent navigation budget rather than worst-case fan-out.
func queryTokensForNode(seed types.Node, nodes []types.Node,
	adj map[string][]string, depth int) int {
	const visitedCap = 200
	idx := make(map[string]types.Node, len(nodes))
	for _, n := range nodes {
		idx[n.ID] = n
	}
	visited := map[string]bool{seed.ID: true}
	frontier := []string{seed.ID}
	var rendered strings.Builder
	rendered.WriteString(seed.Name)
	rendered.WriteString(" — ")
	rendered.WriteString(seed.QualifiedName)
	rendered.WriteString("\n")

bfs:
	for d := 0; d < depth && len(frontier) > 0; d++ {
		next := make([]string, 0, len(frontier))
		for _, id := range frontier {
			cur := idx[id]
			for _, nb := range adj[id] {
				if visited[nb] {
					continue
				}
				visited[nb] = true
				other := idx[nb]
				if other.ID == "" {
					continue
				}
				rendered.WriteString("  ")
				rendered.WriteString(cur.Name)
				rendered.WriteString(" → ")
				rendered.WriteString(other.Name)
				rendered.WriteString(" [")
				rendered.WriteString(string(other.Type))
				rendered.WriteString("] ")
				rendered.WriteString(other.QualifiedName)
				rendered.WriteString("\n")
				next = append(next, nb)
				if len(visited) >= visitedCap {
					break bfs
				}
			}
		}
		frontier = next
	}
	return rendered.Len() / charsPerToken
}

func ratio(corpus, query int) float64 {
	if query <= 0 {
		return 0
	}
	return float64(corpus) / float64(query)
}

func printBenchmark(w *os.File, m persist.Manifest, corpusTokens, avgQT int, perQ []queryStat) {
	_, _ = fmt.Fprintln(w, "ckg token-reduction benchmark")
	_, _ = fmt.Fprintf(w, "  Source:    %s\n", m.SrcRoot)
	_, _ = fmt.Fprintf(w, "  Corpus:    ~%s tokens (sum of indexed source files)\n",
		humanInt(corpusTokens))
	_, _ = fmt.Fprintf(w, "  Avg query: ~%s tokens (top-%d god nodes, k-hop BFS)\n",
		humanInt(avgQT), len(perQ))
	if avgQT > 0 {
		_, _ = fmt.Fprintf(w, "  Reduction: %.1fx fewer tokens per query\n",
			ratio(corpusTokens, avgQT))
	}
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Per-question breakdown:")
	for i, q := range perQ {
		_, _ = fmt.Fprintf(w, "  %d. `%s` → ~%s tokens (%.1fx reduction)\n",
			i+1, q.name, humanInt(q.tokens), q.reduction)
	}
}

// humanInt formats large integers with commas for the benchmark output.
// Saved here rather than depending on a third-party fmt humaniser to
// keep the binary lean.
func humanInt(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 1000 {
		return s
	}
	var out []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out)
}
