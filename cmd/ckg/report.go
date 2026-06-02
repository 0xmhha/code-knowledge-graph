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
	"github.com/0xmhha/code-knowledge-graph/internal/score"
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
			defer func() { _ = store.Close() }()

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
			// Topic tree powers the Communities + cohesion section.
			// LoadHierarchy returns rows for every resolution; we read the
			// middle one (Level=1, γ=1.0) by default — the smallest
			// communities at γ=2.0 are too granular for an at-a-glance
			// report and the largest at γ=0.5 collapse the whole graph
			// into a few mega-clusters.
			topics, err := store.LoadHierarchy("topic")
			if err != nil {
				// Non-fatal: a graph built before the topic_tree was added
				// (or one where Leiden was skipped) still produces a useful
				// report without the Communities section.
				topics = nil
			}

			report := buildReport(manifest, nodes, edges, topics, topGod)
			if err := os.WriteFile(out, []byte(report), 0o644); err != nil {
				return fmt.Errorf("write report: %w", err)
			}
			_, _ = fmt.Fprintf(os.Stderr, "ckg: wrote GRAPH_REPORT to %s (%d bytes)\n", out, len(report))
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
//
// topics is the persisted topic_tree (LoadHierarchy("topic")) — empty
// is OK; the Communities + Knowledge Gaps sections degrade gracefully
// without it.
func buildReport(m persist.Manifest, nodes []types.Node, edges []types.Edge,
	topics []persist.HierarchyRow, topGod int) string {
	var b strings.Builder

	// ── Header
	_, _ = fmt.Fprintf(&b, "# GRAPH_REPORT\n\n")
	_, _ = fmt.Fprintf(&b, "Generated from CKG schema **%s** • %s\n\n",
		m.SchemaVersion, m.BuildTimestamp)
	if m.SrcRoot != "" {
		_, _ = fmt.Fprintf(&b, "- **Source**: `%s`", m.SrcRoot)
		if m.SrcCommit != "" {
			_, _ = fmt.Fprintf(&b, " @ `%s`", shortSHA(m.SrcCommit))
		}
		b.WriteString("\n")
	}
	_, _ = fmt.Fprintf(&b, "- **Nodes**: %d • **Edges**: %d\n",
		len(nodes), len(edges))
	if len(m.Languages) > 0 {
		var langs []string
		for k, v := range m.Languages {
			langs = append(langs, fmt.Sprintf("%s=%d", k, v))
		}
		sort.Strings(langs)
		_, _ = fmt.Fprintf(&b, "- **Languages** (files): %s\n", strings.Join(langs, ", "))
	}
	b.WriteString("\n")

	// ── 6-graph axis distribution
	b.WriteString("## 6-Graph axis distribution\n\n")
	axisCounts := axisDistribution(edges)
	for _, ax := range []string{"G1", "G2", "G3", "G4", "G5", "G6"} {
		c := axisCounts[ax]
		bar := bar(c, axisCounts["max"], 30)
		_, _ = fmt.Fprintf(&b, "- **%s** %s — %d edges %s\n",
			ax, axisLabel(ax), c, bar)
	}
	b.WriteString("\n")

	// ── God nodes
	_, _ = fmt.Fprintf(&b, "## God nodes (top-%d by PageRank)\n\n", topGod)
	b.WriteString("Symbols that everything else flows through. Removing or refactoring these has the highest blast radius.\n\n")
	gods := topPageRank(nodes, topGod)
	if len(gods) == 0 {
		b.WriteString("_(No PageRank values found — was the graph built without scoring?)_\n\n")
	} else {
		b.WriteString("| # | Type | Name | Qualified Name | PageRank | In/Out |\n")
		b.WriteString("|---|------|------|----------------|---------:|-------:|\n")
		for i, n := range gods {
			_, _ = fmt.Fprintf(&b, "| %d | %s | `%s` | `%s` | %.5f | %d/%d |\n",
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
			_, _ = fmt.Fprintf(&b, "| %d | `%s` | %d | %.5f |\n",
				i+1, f.path, f.nodeCount, f.sumPR)
		}
		b.WriteString("\n")
	}

	// ── Communities + cohesion (graphify-inspired, schema 1.8+)
	communities := communitiesAtLevel(topics, 1)
	cohesions := computeCohesions(communities, edges)
	if len(communities) > 0 {
		b.WriteString("## Communities (Leiden γ=1.0)\n\n")
		b.WriteString("Topics surfaced by the middle-resolution Leiden pass. Cohesion = intra-community edges ÷ max-possible undirected pairs.\n\n")
		commSummary := topCommunities(communities, cohesions, 12)
		b.WriteString("| Community | Size | Cohesion | Sample symbols |\n")
		b.WriteString("|-----------|-----:|---------:|----------------|\n")
		for _, c := range commSummary {
			samples := sampleCommunityNames(c.members, nodes, 5)
			_, _ = fmt.Fprintf(&b, "| `%s` | %d | %.3f | %s |\n",
				truncateRunes(c.parentID, 24), c.size, c.cohesion, samples)
		}
		b.WriteString("\n")
	}

	// ── Knowledge Gaps (graphify-inspired)
	gaps := findKnowledgeGaps(nodes, edges, communities, 3)
	if gaps.hasAny() {
		b.WriteString("## Knowledge Gaps\n\n")
		b.WriteString("Low-signal regions. Possible missing edges, undocumented components, or candidates for refactoring.\n\n")
		if len(gaps.isolated) > 0 {
			_, _ = fmt.Fprintf(&b, "- **%d isolated node(s)** (degree ≤ 1):", len(gaps.isolated))
			labels := []string{}
			for i, n := range gaps.isolated {
				if i >= 5 {
					break
				}
				labels = append(labels, fmt.Sprintf("`%s`", n.Name))
			}
			b.WriteString(" " + strings.Join(labels, ", "))
			if len(gaps.isolated) > 5 {
				_, _ = fmt.Fprintf(&b, " (+%d more)", len(gaps.isolated)-5)
			}
			b.WriteString("\n  These have ≤1 connection — possible missing edges or unused symbols.\n")
		}
		if len(gaps.thinComms) > 0 {
			_, _ = fmt.Fprintf(&b, "- **%d thin communities** (< 3 members) — may indicate stranded code or parser-coverage holes.\n",
				len(gaps.thinComms))
		}
		if gaps.ambiguousPct > 20 {
			_, _ = fmt.Fprintf(&b, "- **High ambiguity: %.0f%% of edges are AMBIGUOUS.** Review and disambiguate to improve graph quality.\n",
				gaps.ambiguousPct)
		}
		b.WriteString("\n")
	}

	// ── Suggested questions (5-category, graphify-inspired)
	b.WriteString("## Suggested questions\n\n")
	b.WriteString("Questions the graph is uniquely positioned to answer.\n\n")
	bc := score.ApproxBetweenness(nodes, edges, 100, 42)
	for _, q := range suggestedQuestionsV2(nodes, edges, gods, hotFiles, communities, cohesions, gaps, bc, axisCounts) {
		_, _ = fmt.Fprintf(&b, "- %s\n", q)
	}
	b.WriteString("\n")

	// ── Confidence breakdown
	b.WriteString("## Confidence breakdown\n\n")
	confEdges := confidenceCounts(edges)
	for _, c := range []types.Confidence{types.ConfExtracted, types.ConfInferred, types.ConfAmbiguous} {
		_, _ = fmt.Fprintf(&b, "- **%s**: %d edges\n", c, confEdges[c])
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

// truncateRunes is a UTF-8-safe truncation: bounds output by rune count
// rather than byte count so multi-byte characters (em-dash, kana, etc.)
// don't get sliced mid-byte. shortSHA stays byte-based because SHA hex is
// ASCII; community labels and other display strings should use this.
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		count++
		if count > maxRunes {
			return s[:i]
		}
	}
	return s
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
	case types.EdgeListensOn, types.EdgeHandlesMessage, types.EdgeRPCCalls,
		types.EdgeBindsTo, types.EdgeHTTPCalls,
		types.EdgeGRPCListensOn, types.EdgeGRPCCalls:
		return "G5"
	case types.EdgeChangedIn, types.EdgeBlame, types.EdgeHasHunk, types.EdgeAdjacent, types.EdgeModifies:
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

// topPageRank returns the top-N nodes by PageRank, descending. Filters
// out meta nodes + structural-hub kinds via godNodeFilter — see that
// function for the rationale.
func topPageRank(nodes []types.Node, n int) []types.Node {
	filtered := make([]types.Node, 0, len(nodes))
	for _, x := range nodes {
		if godNodeFilter(x.Type, x.FilePath) {
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

// godNodeFilter reports whether a node should be EXCLUDED from the
// "god nodes" ranking and the topPageRank-derived suggested questions.
// Filters two tiers:
//
//   - meta nodes (Commit, Hunk): no PageRank by §11.7 — including
//     them slots zero rows at the bottom of the table.
//   - structural-hub kinds (File, Package, Import, Export, Decorator,
//     Modifier): they accumulate `contains` / `defines` / `imports`
//     edges mechanically and don't represent the architectural
//     abstractions a reader of the report wants surfaced. graphify's
//     analyze.god_nodes excludes file-level nodes for the same reason
//     (analyze.py:69-77).
//   - concept-like (FilePath empty): nodes the parser couldn't anchor
//     to a real source location are usually placeholder pending refs,
//     not real abstractions.
func godNodeFilter(t types.NodeType, filePath string) bool {
	if filePath == "" {
		return true
	}
	switch t {
	case types.NodeCommit, types.NodeHunk:
		return true
	case types.NodeFile, types.NodePackage:
		return true
	case types.NodeImport, types.NodeExport, types.NodeDecorator, types.NodeModifier:
		return true
	}
	return false
}

// communitySummary captures one community's identity, size, and cohesion
// for the report's table. parentID is the topic_tree's parent node ID at
// the chosen resolution level.
type communitySummary struct {
	parentID string
	size     int
	cohesion float64
	members  []string
}

// communitiesAtLevel groups topic_tree HierarchyRow children by their
// community membership at the requested level. CKG's topic_tree stores
// the community identity in `topic_label` (a human-readable name picked
// by LabelCommunity from the highest-PageRank member at that resolution)
// rather than in `parent_id` (which is the upstream higher-resolution
// community link, often empty at level 0). We group by label so each
// returned key is a distinct community.
//
// Empty when the topic tree wasn't built (graphs from before E4 / no
// Leiden run).
func communitiesAtLevel(topics []persist.HierarchyRow, level int) map[string][]string {
	out := map[string][]string{}
	for _, r := range topics {
		if r.Level != level {
			continue
		}
		key := r.TopicLabel
		if key == "" {
			// Fall back to parent_id if the label is missing (older
			// graphs may not have populated TopicLabel).
			key = r.ParentID
		}
		if key == "" {
			// Still empty — bucket as a singleton under the child id
			// itself. Better than collapsing every unlabelled row into
			// one mega-community.
			key = "_orphan_" + r.ChildID
		}
		out[key] = append(out[key], r.ChildID)
	}
	return out
}

// computeCohesions returns intra-community-edges / max-possible-undirected-
// pairs for each community. Mirrors graphify's cohesion_score
// (cluster.py:138-146): values in [0, 1] where 1 = clique.
func computeCohesions(communities map[string][]string, edges []types.Edge) map[string]float64 {
	out := make(map[string]float64, len(communities))
	memberOf := make(map[string]string, 0)
	for cid, members := range communities {
		for _, m := range members {
			memberOf[m] = cid
		}
	}
	intra := make(map[string]int, len(communities))
	for _, e := range edges {
		if e.Src == e.Dst {
			continue
		}
		c1, ok1 := memberOf[e.Src]
		c2, ok2 := memberOf[e.Dst]
		if ok1 && ok2 && c1 == c2 {
			intra[c1]++
		}
	}
	for cid, members := range communities {
		n := len(members)
		if n <= 1 {
			out[cid] = 1.0
			continue
		}
		maxPairs := n * (n - 1) / 2
		out[cid] = float64(intra[cid]) / float64(maxPairs)
	}
	return out
}

// topCommunities returns the largest communities (by member count),
// capped at n entries. Used by the report to avoid dumping all 100s of
// communities — the long tail is summarised in Knowledge Gaps as
// "thin communities".
func topCommunities(communities map[string][]string, cohesions map[string]float64, n int) []communitySummary {
	out := make([]communitySummary, 0, len(communities))
	for cid, members := range communities {
		out = append(out, communitySummary{
			parentID: cid,
			size:     len(members),
			cohesion: cohesions[cid],
			members:  members,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].size != out[j].size {
			return out[i].size > out[j].size
		}
		return out[i].cohesion > out[j].cohesion
	})
	if n > len(out) {
		n = len(out)
	}
	return out[:n]
}

// sampleCommunityNames returns up to n node names from the community,
// preferring high-PageRank symbols so the sample reads as "what this
// community is about" rather than a random slice.
func sampleCommunityNames(memberIDs []string, allNodes []types.Node, n int) string {
	idx := make(map[string]types.Node, len(allNodes))
	for _, nd := range allNodes {
		idx[nd.ID] = nd
	}
	picked := make([]types.Node, 0, len(memberIDs))
	for _, id := range memberIDs {
		if nd, ok := idx[id]; ok && !godNodeFilter(nd.Type, nd.FilePath) {
			picked = append(picked, nd)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool {
		return picked[i].PageRank > picked[j].PageRank
	})
	if n > len(picked) {
		n = len(picked)
	}
	if n == 0 {
		return "_(file-level nodes only)_"
	}
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		parts[i] = "`" + picked[i].Name + "`"
	}
	return strings.Join(parts, ", ")
}

// knowledgeGaps captures low-signal regions of the graph: isolated
// nodes (degree ≤ 1), thin communities (member count < threshold),
// and edge-confidence stats. Mirrors graphify's report.py "Knowledge
// Gaps" section (report.py:158-181).
type knowledgeGaps struct {
	isolated     []types.Node
	thinComms    []communitySummary
	ambiguousPct float64
	ambiguousN   int
}

func (g knowledgeGaps) hasAny() bool {
	return len(g.isolated) > 0 || len(g.thinComms) > 0 || g.ambiguousPct > 20
}

// findKnowledgeGaps walks the graph for low-signal indicators. minComm
// sets the thin-community size threshold (graphify uses 3).
func findKnowledgeGaps(nodes []types.Node, edges []types.Edge,
	communities map[string][]string, minComm int) knowledgeGaps {
	gaps := knowledgeGaps{}
	for _, n := range nodes {
		if godNodeFilter(n.Type, n.FilePath) {
			continue
		}
		if n.InDegree+n.OutDegree <= 1 {
			gaps.isolated = append(gaps.isolated, n)
		}
	}
	// Sort isolated by PageRank (highest first) so the first few in the
	// report are the most surprising losses (a high-rank symbol that
	// somehow only has one connection is more interesting than a leaf).
	sort.SliceStable(gaps.isolated, func(i, j int) bool {
		return gaps.isolated[i].PageRank > gaps.isolated[j].PageRank
	})
	for cid, members := range communities {
		if len(members) > 0 && len(members) < minComm {
			gaps.thinComms = append(gaps.thinComms, communitySummary{
				parentID: cid, size: len(members), members: members,
			})
		}
	}
	confs := confidenceCounts(edges)
	gaps.ambiguousN = confs[types.ConfAmbiguous]
	if len(edges) > 0 {
		gaps.ambiguousPct = float64(gaps.ambiguousN) * 100.0 / float64(len(edges))
	}
	return gaps
}

// suggestedQuestionsV2 emits 5-category questions inspired by
// graphify's analyze.suggest_questions (analyze.py:370-489):
//
//  1. AMBIGUOUS edges → relationship-clarification questions
//  2. Bridge nodes (high betweenness) → cross-cutting concern questions
//  3. God nodes with INFERRED neighbours → verification questions
//     (CKG self-graph has near-zero INFERRED edges thanks to Track C;
//     this category falls back to god node call analysis when empty)
//  4. Isolated nodes → exploration questions
//  5. Low-cohesion communities → structural questions
//
// Plus a few CKG-specific axis questions kept from the v1 helper that
// surface only when the relevant 6-graph axis has data.
func suggestedQuestionsV2(
	nodes []types.Node, edges []types.Edge,
	gods []types.Node, hubs []fileHub,
	communities map[string][]string,
	cohesions map[string]float64,
	gaps knowledgeGaps,
	bc map[string]float64,
	axes map[string]int,
) []string {
	var out []string

	// 1. AMBIGUOUS edges
	for _, e := range edges {
		if e.Confidence == types.ConfAmbiguous && len(out) < 3 {
			srcName := nodeNameByID(nodes, e.Src)
			dstName := nodeNameByID(nodes, e.Dst)
			out = append(out, fmt.Sprintf(
				"What is the exact relationship between `%s` and `%s`? _(edge tagged AMBIGUOUS, relation: %s)_",
				srcName, dstName, e.Type))
		}
	}

	// 2. Bridge nodes (top betweenness, file-filtered)
	type bridge struct {
		id    string
		score float64
	}
	bridges := make([]bridge, 0, len(bc))
	idx := make(map[string]types.Node, len(nodes))
	for _, n := range nodes {
		idx[n.ID] = n
	}
	for id, s := range bc {
		nd, ok := idx[id]
		if !ok {
			continue
		}
		if godNodeFilter(nd.Type, nd.FilePath) {
			continue
		}
		bridges = append(bridges, bridge{id, s})
	}
	sort.SliceStable(bridges, func(i, j int) bool {
		return bridges[i].score > bridges[j].score
	})
	for i, br := range bridges {
		if i >= 3 {
			break
		}
		nd := idx[br.id]
		out = append(out, fmt.Sprintf(
			"Why does `%s` bridge so much of the graph? _(betweenness centrality %.4f — a cross-cutting concern)_",
			nd.Name, br.score))
	}

	// 3. God nodes — substitute "What touches X?" since CKG rarely has
	// INFERRED edges (Track C resolves most cross-file refs cleanly).
	for i, gn := range gods {
		if i >= 1 {
			break
		}
		out = append(out, fmt.Sprintf(
			"What calls `%s`? _(top-PageRank — its callers define the refactoring blast radius)_",
			gn.QualifiedName))
	}
	if len(gods) >= 3 {
		out = append(out, fmt.Sprintf(
			"How are `%s`, `%s`, and `%s` connected? _(the three biggest hubs — paths between them are the load-bearing call chains)_",
			gods[0].Name, gods[1].Name, gods[2].Name))
	}

	// 4. Isolated nodes
	if len(gaps.isolated) > 0 {
		labels := []string{}
		for i, n := range gaps.isolated {
			if i >= 3 {
				break
			}
			labels = append(labels, "`"+n.Name+"`")
		}
		out = append(out, fmt.Sprintf(
			"What connects %s to the rest of the system? _(degree ≤ 1 — possible documentation gaps)_",
			strings.Join(labels, ", ")))
	}

	// 5. Low-cohesion communities
	type lowComm struct {
		id       string
		size     int
		cohesion float64
	}
	var lows []lowComm
	for cid, score := range cohesions {
		if score >= 0.15 || len(communities[cid]) < 5 {
			continue
		}
		lows = append(lows, lowComm{cid, len(communities[cid]), score})
	}
	sort.SliceStable(lows, func(i, j int) bool {
		return lows[i].size > lows[j].size
	})
	for i, lc := range lows {
		if i >= 2 {
			break
		}
		out = append(out, fmt.Sprintf(
			"Should community `%s` (%d members, cohesion %.3f) be split into more focused modules?",
			truncateRunes(lc.id, 32), lc.size, lc.cohesion))
	}

	// CKG-specific axis questions (kept from v1)
	if hub := firstHub(hubs); hub != "" {
		out = append(out, fmt.Sprintf(
			"What lives in `%s` and what depends on it? _(highest concentrated PageRank)_", hub))
	}
	if axes["G4"] > 0 {
		out = append(out, "Which symbols are accessed under a lock without holding it? _(run audit on `accessed_under_lock`)_")
	}
	if axes["G5"] > 0 {
		out = append(out, "Which HTTP/RPC handlers exist and what message types do they dispatch on?")
	}
	if axes["G6"] > 0 {
		out = append(out, "Which files churn the most and what do they share? _(high `changed_in` / `blame` density)_")
	}

	if len(out) == 0 {
		out = append(out,
			"(Not enough signal to generate questions — try `ckg build` with more source files or enable a richer extractor.)")
	}
	return out
}

func firstHub(hubs []fileHub) string {
	if len(hubs) == 0 {
		return ""
	}
	return hubs[0].path
}

func nodeNameByID(nodes []types.Node, id string) string {
	for _, n := range nodes {
		if n.ID == id {
			return n.Name
		}
	}
	return id[:min(8, len(id))]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
