package buildpipe

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// ComputeGraphDigest returns a deterministic, build-invariant hex hash of the
// CODE graph — the coordinate pin anchor published in manifest.GraphDigest and
// consumed by CKV/CKS (docs/coordination-reindex-migration-2026-07-10.md § Q1).
//
// Definition (agreed cross-repo, 2026-07-10):
//
//	digest = sha256( <nodes-block> "\n--edges--\n" <edges-block> )
//	  node line = id \t canonical_id \t type \t qualified_name \t file_path \t
//	              start_line \t end_line          — sorted by id ascending
//	  edge line = type \t src \t dst \t line       — the canonical (Type,Src,Dst,
//	              Line) edge identity; exact duplicates collapsed; sorted
//
// Excluded, by design:
//   - Derived metrics (pagerank / in_degree / out_degree / usage_score) — they
//     are recomputed on any incremental dirt, so including them would make the
//     incremental digest differ from a cold one for the same logical graph.
//   - Index-derived / content columns (search_tokens, simple_name, attrs,
//     signature, doc_comment) and start_byte (already folded into id).
//   - Temporal nodes/edges (Commit/Hunk + changed_in/blame/has_hunk/adjacent/
//     modifies) — they depend on --temporal-depth + git state, orthogonal to the
//     code graph CKV/CKS pin. A temporal-only rebuild leaves this digest
//     unchanged, which is the point.
//
// Because every retained field is deterministic for a pinned source under
// ADR-0002 and the lines are sorted, the digest is identical across cold and
// incremental builds and across machines.
func ComputeGraphDigest(nodes []types.Node, edges []types.Edge) string {
	nodeLines := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if isMetaNodeType(n.Type) {
			continue // temporal (Commit/Hunk) excluded
		}
		nodeLines = append(nodeLines, strings.Join([]string{
			n.ID,
			n.CanonicalID,
			string(n.Type),
			n.QualifiedName,
			n.FilePath,
			strconv.Itoa(n.StartLine),
			strconv.Itoa(n.EndLine),
		}, "\t"))
	}
	sort.Strings(nodeLines)

	edgeSet := make(map[string]struct{}, len(edges))
	for _, e := range edges {
		if isTemporalEdgeType(e.Type) {
			continue // temporal edges excluded
		}
		edgeSet[strings.Join([]string{
			string(e.Type),
			e.Src,
			e.Dst,
			strconv.Itoa(e.Line),
		}, "\t")] = struct{}{}
	}
	edgeLines := make([]string, 0, len(edgeSet))
	for l := range edgeSet {
		edgeLines = append(edgeLines, l)
	}
	sort.Strings(edgeLines)

	h := sha256.New()
	h.Write([]byte(strings.Join(nodeLines, "\n")))
	h.Write([]byte("\n--edges--\n"))
	h.Write([]byte(strings.Join(edgeLines, "\n")))
	return hex.EncodeToString(h.Sum(nil))
}

// isTemporalEdgeType reports whether e belongs to the temporal (git-history)
// family excluded from the code-graph digest. Mirrors isMetaNodeType for edges.
func isTemporalEdgeType(t types.EdgeType) bool {
	switch t {
	case types.EdgeChangedIn, types.EdgeBlame, types.EdgeHasHunk,
		types.EdgeAdjacent, types.EdgeModifies:
		return true
	default:
		return false
	}
}
