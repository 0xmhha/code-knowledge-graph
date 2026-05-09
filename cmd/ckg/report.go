// cmd/ckg/report.go — generate a human-readable GRAPH_REPORT.md from a
// built graph.db. Inspired by graphify's GRAPH_REPORT.md (god nodes,
// surprising connections, suggested questions) but extended with CKG's
// 6-graph axis breakdown so the report carries the full picture of the
// codebase across structural / semantic / execution / concurrency /
// distributed / temporal axes.
//
// Use case: ship a single markdown alongside graph.db / graph.json so
// reviewers, agents, and managers have a quick primer on the codebase
// without booting the viewer. The report is purely derived — re-run any
// time without re-building.
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

func newReportCmd() *cobra.Command {
	var graph, out string
	var topGod int
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Generate GRAPH_REPORT.md (god nodes + axis distribution + suggested questions)",
		Long: `Generate a markdown summary of the graph: top-PageRank "god nodes"
that everything flows through, the 6-graph axis distribution (G1-G6),
the most-connected files, and a few heuristic-suggested questions the
graph is uniquely positioned to answer. Reads graph.db only — no LLM
calls, runs in seconds even on 220K-node graphs.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			db := filepath.Join(graph, "graph.db")
			store, err := persist.OpenReadOnly(db)
			if err != nil {
				return fmt.Errorf("open graph: %w", err)
			}
			defer store.Close()

			manifest, err := store.GetManifest()
			if err != nil {
				return fmt.Errorf("read manifest: %w", err)
			}
			nodes, err := store.AllNodes()
			if err != nil {
				return fmt.Errorf("load nodes: %w", err)
			}
			edges, err := store.AllEdges()
			if err != nil {
				return fmt.Errorf("load edges: %w", err)
			}

			report := buildReport(manifest, nodes, edges, topGod)
			if err := os.WriteFile(out, []byte(report), 0o644); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			fmt.Fprintf(os.Stderr, "ckg: wrote GRAPH_REPORT to %s (%d bytes)\n", out, len(report))
			return nil
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().StringVar(&out, "out", "GRAPH_REPORT.md", "output markdown path")
	cmd.Flags().IntVar(&topGod, "top-god", 25, "number of top-PageRank nodes to list")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}

// buildReport produces the markdown. Pure function (no I/O) so it's
// trivially testable; the caller owns reading graph.db and writing the
// output file.
func buildReport(m persist.Manifest, nodes []types.Node, edges []types.Edge, topGod int) string {
	var b strings.Builder

	// ── Header
	fmt.Fprintf(&b, "# GRAPH_REPORT\n\n")
	fmt.Fprintf(&b, "Generated from CKG schema **%s** • %s\n\n",
		m.SchemaVersion, m.BuildTimestamp)
	if m.SrcRoot != "" {
		fmt.Fprintf(&b, "- **Source**: `%s`", m.SrcRoot)
		if m.SrcCommit != "" {
			fmt.Fprintf(&b, " @ `%s`", shortSHA(m.SrcCommit))
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "- **Nodes**: %d • **Edges**: %d\n",
		len(nodes), len(edges))
	if len(m.Languages) > 0 {
		var langs []string
		for k, v := range m.Languages {
			langs = append(langs, fmt.Sprintf("%s=%d", k, v))
		}
		sort.Strings(langs)
		fmt.Fprintf(&b, "- **Languages** (files): %s\n", strings.Join(langs, ", "))
	}
	b.WriteString("\n")

	// ── 6-graph axis distribution
	b.WriteString("## 6-Graph axis distribution\n\n")
	axisCounts := axisDistribution(edges)
	for _, ax := range []string{"G1", "G2", "G3", "G4", "G5", "G6"} {
		c := axisCounts[ax]
		bar := bar(c, axisCounts["max"], 30)
		fmt.Fprintf(&b, "- **%s** %s — %d edges %s\n",
			ax, axisLabel(ax), c, bar)
	}
	b.WriteString("\n")

	// ── God nodes
	fmt.Fprintf(&b, "## God nodes (top-%d by PageRank)\n\n", topGod)
	b.WriteString("Symbols that everything else flows through. Removing or refactoring these has the highest blast radius.\n\n")
	gods := topPageRank(nodes, topGod)
	if len(gods) == 0 {
		b.WriteString("_(No PageRank values found — was the graph built without scoring?)_\n\n")
	} else {
		b.WriteString("| # | Type | Name | Qualified Name | PageRank | In/Out |\n")
		b.WriteString("|---|------|------|----------------|---------:|-------:|\n")
		for i, n := range gods {
			fmt.Fprintf(&b, "| %d | %s | `%s` | `%s` | %.5f | %d/%d |\n",
				i+1, n.Type, n.Name, n.QualifiedName,
				n.PageRank, n.InDegree, n.OutDegree)
		}
		b.WriteString("\n")
	}

	// ── Most-touched files
	b.WriteString("## Most-connected files\n\n")
	b.WriteString("Files whose contained nodes have the highest combined PageRank. Hubs of activity.\n\n")
	hotFiles := topConnectedFiles(nodes, 15)
	if len(hotFiles) > 0 {
		b.WriteString("| # | File | Symbols | Σ PageRank |\n")
		b.WriteString("|---|------|--------:|-----------:|\n")
		for i, f := range hotFiles {
			fmt.Fprintf(&b, "| %d | `%s` | %d | %.5f |\n",
				i+1, f.path, f.nodeCount, f.sumPR)
		}
		b.WriteString("\n")
	}

	// ── Suggested questions (heuristic)
	b.WriteString("## Suggested questions\n\n")
	b.WriteString("Questions the graph is uniquely positioned to answer.\n\n")
	for _, q := range suggestedQuestions(gods, hotFiles, axisCounts) {
		fmt.Fprintf(&b, "- %s\n", q)
	}
	b.WriteString("\n")

	// ── Confidence breakdown
	b.WriteString("## Confidence breakdown\n\n")
	confEdges := confidenceCounts(edges)
	for _, c := range []types.Confidence{types.ConfExtracted, types.ConfInferred, types.ConfAmbiguous} {
		fmt.Fprintf(&b, "- **%s**: %d edges\n", c, confEdges[c])
	}
	b.WriteString("\n")

	b.WriteString("---\n\n")
	b.WriteString("_Generated by `ckg report`. Re-run any time — derives only from `graph.db`._\n")
	return b.String()
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// axisDistribution sums edges per CKS axis. Map result also carries
// "max" so the bar visualisation has a denominator.
func axisDistribution(edges []types.Edge) map[string]int {
	out := map[string]int{"G1": 0, "G2": 0, "G3": 0, "G4": 0, "G5": 0, "G6": 0}
	for _, e := range edges {
		ax := edgeToAxis(e.Type)
		if ax != "" {
			out[ax]++
		}
	}
	max := 0
	for _, v := range out {
		if v > max {
			max = v
		}
	}
	out["max"] = max
	return out
}

// edgeToAxis maps backend EdgeType → CKS graph axis (G1..G6). Source of
// truth for the report's axis breakdown; mirrors viewer-next/src/lib/
// edges.ts GRAPH_GROUPS.
func edgeToAxis(t types.EdgeType) string {
	switch t {
	case types.EdgeContains, types.EdgeDefines, types.EdgeImports, types.EdgeExports:
		return "G1"
	case types.EdgeUsesType, types.EdgeInstantiates, types.EdgeReferences,
		types.EdgeImplements, types.EdgeExtends,
		types.EdgeReadsField, types.EdgeWritesField,
		types.EdgeReadsMapping, types.EdgeWritesMapping,
		types.EdgeEmitsEvent, types.EdgeHasModifier, types.EdgeHasDecorator:
		return "G2"
	case types.EdgeCalls, types.EdgeInvokes, types.EdgeTimeoutPath, types.EdgeCancellationPath:
		return "G3"
	case types.EdgeSpawns, types.EdgeSendsTo, types.EdgeRecvsFrom,
		types.EdgeAcquiresLock, types.EdgeReleasesLock, types.EdgeAccessedUnderLock:
		return "G4"
	case types.EdgeListensOn, types.EdgeHandlesMessage, types.EdgeRPCCalls, types.EdgeBindsTo:
		return "G5"
	case types.EdgeChangedIn, types.EdgeBlame, types.EdgeHasHunk, types.EdgeAdjacent:
		return "G6"
	}
	return ""
}

func axisLabel(ax string) string {
	return map[string]string{
		"G1": "Structural",
		"G2": "Semantic",
		"G3": "Execution",
		"G4": "Concurrency",
		"G5": "Distributed",
		"G6": "Temporal",
	}[ax]
}

// bar renders a unicode-block progress bar of width characters, scaled
// against max. Empty when max == 0.
func bar(value, max, width int) string {
	if max <= 0 {
		return ""
	}
	filled := value * width / max
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// topPageRank returns the top-N nodes by PageRank, descending. Meta
// nodes (Commit, Hunk) are excluded because their PageRank is zeroed
// by score.Compute (schema 1.8 §11.7) — including them would always
// slot "0.0" rows at the bottom.
func topPageRank(nodes []types.Node, n int) []types.Node {
	filtered := make([]types.Node, 0, len(nodes))
	for _, x := range nodes {
		if x.Type == types.NodeCommit || x.Type == types.NodeHunk {
			continue
		}
		filtered = append(filtered, x)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].PageRank > filtered[j].PageRank
	})
	if n > len(filtered) {
		n = len(filtered)
	}
	return filtered[:n]
}

type fileHub struct {
	path      string
	nodeCount int
	sumPR     float64
}

// topConnectedFiles ranks files by the sum of their contained nodes'
// PageRank. Captures "where does the codebase's gravity sit" without
// requiring blame/changed_in to be enabled.
func topConnectedFiles(nodes []types.Node, n int) []fileHub {
	agg := map[string]*fileHub{}
	for _, nd := range nodes {
		if nd.FilePath == "" || nd.Type == types.NodeCommit || nd.Type == types.NodeHunk {
			continue
		}
		h, ok := agg[nd.FilePath]
		if !ok {
			h = &fileHub{path: nd.FilePath}
			agg[nd.FilePath] = h
		}
		h.nodeCount++
		h.sumPR += nd.PageRank
	}
	hubs := make([]fileHub, 0, len(agg))
	for _, h := range agg {
		hubs = append(hubs, *h)
	}
	sort.SliceStable(hubs, func(i, j int) bool {
		return hubs[i].sumPR > hubs[j].sumPR
	})
	if n > len(hubs) {
		n = len(hubs)
	}
	return hubs[:n]
}

// confidenceCounts tallies edges by confidence label. Useful as a "how
// much of the graph was inferred vs extracted" quality indicator.
func confidenceCounts(edges []types.Edge) map[types.Confidence]int {
	out := map[types.Confidence]int{}
	for _, e := range edges {
		out[e.Confidence]++
	}
	return out
}

// suggestedQuestions emits heuristic-driven questions a reader can ask
// the graph. Hand-picked templates that pull the top god node + top
// hub file + dominant axis so the questions are concrete to this repo
// rather than generic boilerplate.
func suggestedQuestions(gods []types.Node, hubs []fileHub, axes map[string]int) []string {
	var out []string
	if len(gods) > 0 {
		out = append(out, fmt.Sprintf("What calls `%s`? (the top god node — its callers define a refactoring blast radius)",
			gods[0].QualifiedName))
	}
	if len(gods) >= 3 {
		out = append(out, fmt.Sprintf("How are `%s`, `%s`, and `%s` connected? (the three biggest hubs — paths between them are the load-bearing call chains)",
			gods[0].Name, gods[1].Name, gods[2].Name))
	}
	if len(hubs) > 0 {
		out = append(out, fmt.Sprintf("What lives in `%s` and what depends on it? (the file with the most concentrated PageRank)",
			hubs[0].path))
	}
	if axes["G4"] > 0 {
		out = append(out, "Which symbols are accessed under a lock without holding it? (run audit on `accessed_under_lock` edges)")
	}
	if axes["G5"] > 0 {
		out = append(out, "Which HTTP/RPC handlers exist and what message types do they dispatch on?")
	}
	if axes["G6"] > 0 {
		out = append(out, "Which files churn the most and what do they share? (high `changed_in` / `blame` density)")
	}
	if len(out) == 0 {
		out = append(out, "(No suggestions — graph appears to be empty or PageRank wasn't computed.)")
	}
	return out
}
