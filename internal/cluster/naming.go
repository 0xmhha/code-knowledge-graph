package cluster

import (
	"sort"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// LabelCommunity computes a human-readable label using the 3-tuple heuristic
// from spec §5.5.4: "<dominant_pkg> — <common_substring>* + <top_pagerank_node>".
// `members` is the slice of nodes belonging to one community.
func LabelCommunity(members []types.Node) string {
	if len(members) == 0 {
		return "(empty)"
	}
	pkg := dominantPackage(members)
	prefix := commonNamePrefix(members, 3)
	top := topPageRankName(members)

	var parts []string
	if pkg != "" {
		parts = append(parts, pkg)
	}
	right := ""
	if len(prefix) >= 3 {
		right = prefix + "*"
	}
	if top != "" {
		if right != "" {
			right += " + " + top
		} else {
			right = top
		}
	}
	if right != "" {
		parts = append(parts, right)
	}
	if len(parts) == 0 {
		return "(unnamed)"
	}
	return strings.Join(parts, " — ")
}

func dominantPackage(members []types.Node) string {
	count := map[string]int{}
	for _, n := range members {
		// First segment(s) before the last "." form the package portion.
		q := n.QualifiedName
		if i := strings.LastIndex(q, "."); i > 0 {
			count[q[:i]]++
		}
	}
	best, max := "", 0
	for k, v := range count {
		if v > max || (v == max && k < best) {
			best, max = k, v
		}
	}
	return best
}

func commonNamePrefix(members []types.Node, minOccur int) string {
	// shortest common prefix across at least minOccur names
	if len(members) < minOccur {
		return ""
	}
	names := make([]string, len(members))
	for i, n := range members {
		names[i] = n.Name
	}
	sort.Strings(names)
	// candidate = LCP of first minOccur sorted names
	cand := names[0]
	for i := 1; i < minOccur && i < len(names); i++ {
		cand = lcp(cand, names[i])
	}
	if len(cand) < 3 {
		return ""
	}
	return cand
}

func lcp(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[:i]
		}
	}
	return a[:n]
}

func topPageRankName(members []types.Node) string {
	if len(members) == 0 {
		return ""
	}
	best := members[0]
	for _, n := range members[1:] {
		if n.PageRank > best.PageRank {
			best = n
		}
	}
	return best.Name
}
