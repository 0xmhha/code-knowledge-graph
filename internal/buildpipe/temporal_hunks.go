// Package buildpipe — temporal_hunks.go wires the CKS G6 Hunk-graph H1
// stage (schema 1.8) on top of emitTemporalEdges. Per design
// docs/design/hunk-graph.md (decisions finalised 2026-05-09):
//
//   - Encoding (§11.1):  gzip stdlib, ~70% size reduction on diff text.
//   - Dedup    (§11.2):  none in H1; keep chronology of rebased hunks.
//   - Reach    (§11.3):  H1 only collects HEAD-reachable hunks
//     (Confidence='EXTRACTED'). A future PR adds
//     unreachable collection (Confidence='AMBIGUOUS')
//     via reflog/fsck — H3's EvidencePack assembler
//     MUST filter to EXTRACTED so the LLM never sees
//     force-pushed-away code paths.
//   - Lang     (§11.4):  hunk inherits its target file's extension when
//     in {go, ts, sol}; everything else becomes 'git'.
//   - Cap      (§11.6):  64KB patch cap. Larger patches are stored as
//     first 32KB + truncation marker + last 32KB.
//     Compression is applied AFTER truncation.
//   - Manifest (§11.8):  Hunk node IDs are NOT recorded in the per-file
//     manifest entries (they live outside file-level
//     cache invalidation; emitTemporalEdges runs them
//     wholesale on every build). isMetaNodeType is the
//     single source of truth that buildFileEntries +
//     computeColdFileEntries + extractBlobs share.
package buildpipe

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/internal/temporal"
	"github.com/0xmhha/code-knowledge-graph/pkg/hunkmodifies"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// hunkPatchCap is the byte cap on the unified-diff text PRE-compression.
// 64 KB is the §11.6 decision: large enough that real human-authored
// hunks fit verbatim (the 95th percentile in the self-graph is < 4 KB),
// small enough that a worst-case generated-code regeneration doesn't
// blow up the viewer side-panel render. Patches larger than this are
// truncated to first/last 32 KB chunks with a marker between them.
const hunkPatchCap = 64 * 1024

// hunkLanguageWhitelist is the §11.4 source-of-truth for which target
// file extensions get a language label other than the 'git' sentinel.
// Mirrors the parser-discovery extension set; future language additions
// (e.g. python, rust) extend this map AND the discovery / parser layer.
var hunkLanguageWhitelist = map[string]string{
	".go":  "go",
	".ts":  "ts",
	".tsx": "ts",
	".sol": "sol",
}

// isMetaNodeType reports whether t is a "meta" node — one that lives
// outside the file-level cache (Commit, Hunk). Meta nodes:
//
//   - are emitted wholesale by emitTemporalEdges on every build, so
//     recording them in per-file FileEntry.NodeIDs would inflate the
//     manifest and trigger spurious cache invalidations.
//   - are skipped by extractBlobs (they have no on-disk source range;
//     their blob, if any, is materialised by emitHunkGraph from the
//     git diff and merged into InsertBlobs separately).
func isMetaNodeType(t types.NodeType) bool {
	return t == types.NodeCommit || t == types.NodeHunk
}

// emitHunkGraph runs `git log -p` over the repoRoot, parses the diff
// stream into HunkInfo records, and appends:
//
//   - one NodeHunk per hunk (target file remapped from repoRoot to
//     srcRoot-relative — same idiom as emitTemporalEdges' commit pass),
//   - one EdgeHasHunk per (commit, hunk) pair,
//   - one EdgeAdjacent per consecutive (commit, file)-grouped hunk pair.
//
// Returns the gzip-compressed patch blobs keyed by Hunk node ID; caller
// merges them into the existing extractBlobs map before InsertBlobs so
// hunks live alongside CodeNode source slices in the same blobs table.
//
// Skips silently when:
//   - LoadHunks returns no hunks (non-git tree, empty subtree, or
//     repos with only merge commits under --no-merges).
//   - The hunk's file falls outside srcRoot's subtree (orphaned by
//     remapping; same filter rule as remapToSrcRel).
//   - The hunk's commit isn't in commitIDByteSHA — the commit pass
//     either dropped it (e.g. malformed header) or filtered it out
//     because it touched no in-srcRoot files.
func emitHunkGraph(g *graph.Graph, srcRel string, commitIDByteSHA map[string]string,
	repoRoot string) (map[string][]byte, error) {
	hunks, err := temporal.LoadHunks(repoRoot, 0)
	if err != nil {
		return nil, fmt.Errorf("temporal LoadHunks: %w", err)
	}
	if len(hunks) == 0 {
		return nil, nil
	}
	relHunks, prefix := remapHunksToSrcRel(hunks, srcRel)
	if len(relHunks) == 0 {
		return nil, nil
	}
	subjectBySHA := commitSubjectMapFromGraph(g)
	nodes, hasHunkEdges, adjacentEdges, blobs := buildHunkNodes(relHunks, prefix, commitIDByteSHA, subjectBySHA, types.ConfExtracted)
	g.Nodes = append(g.Nodes, nodes...)
	g.Edges = append(g.Edges, hasHunkEdges...)
	g.Edges = append(g.Edges, adjacentEdges...)
	return blobs, nil
}

// commitSubjectMapFromGraph extracts SHA → subject from the Commit
// nodes already present in g (emitted by buildCommitNodes earlier in
// emitTemporalEdges). The subject lives in Node.Signature as
// "<unix>: <subject>" — we strip the timestamp prefix to recover the
// bare subject text H4 needs for issue-ID extraction.
//
// Returns an empty (non-nil) map when no Commit nodes exist yet —
// callers can pass directly to buildHunkNodes which treats nil and
// empty maps the same way (no H4 enrichment for those hunks).
func commitSubjectMapFromGraph(g *graph.Graph) map[string]string {
	out := make(map[string]string, 256)
	for _, n := range g.Nodes {
		if n.Type != types.NodeCommit {
			continue
		}
		sha := strings.TrimPrefix(n.QualifiedName, "commit:")
		// Signature shape: "<unix>: <subject>".
		idx := strings.IndexByte(n.Signature, ':')
		if idx < 0 {
			out[sha] = n.Signature
			continue
		}
		out[sha] = strings.TrimSpace(n.Signature[idx+1:])
	}
	return out
}

// emitUnreachableHunkGraph adds Commit + Hunk nodes for SHAs reachable
// only via reflog or fsck-unreachable (i.e. force-pushed-away history).
// All nodes + edges get confidence='AMBIGUOUS' per docs/design/hunk-
// graph.md §11.3. The H3 retrieval layer (future) MUST filter to
// EXTRACTED so the LLM never sees code paths that were rolled back.
//
// Commit nodes for unreachable SHAs are emitted here (not by
// buildCommitNodes, which only sees the HEAD-reachable hist.Commits).
// They share the same shape as reachable Commits — only confidence
// differs — so viewer/MCP code that already knows how to render
// NodeCommit doesn't need a parallel "unreachable commit" path.
//
// Returns the gzipped patch blobs map; caller merges into InsertBlobs.
func emitUnreachableHunkGraph(g *graph.Graph, srcRel, repoRoot string,
	existingCommitIDs map[string]string) (map[string][]byte, error) {
	commits, hunks, err := temporal.LoadUnreachableHunks(repoRoot, 0)
	if err != nil {
		return nil, fmt.Errorf("LoadUnreachableHunks: %w", err)
	}
	if len(commits) == 0 || len(hunks) == 0 {
		return nil, nil
	}
	// Build Commit nodes for any unreachable SHA we don't already have.
	// (HEAD-reachable Commits were materialised earlier; the unreachable
	// set is supposed to be disjoint, but defensive de-dup costs nothing.)
	filePath := srcRel
	if filePath == "" || filePath == "." {
		filePath = ".git"
	}
	commitNodes := make([]types.Node, 0, len(commits))
	commitIDByteSHA := make(map[string]string, len(commits))
	maps.Copy(commitIDByteSHA, existingCommitIDs)
	for _, ci := range commits {
		if _, present := commitIDByteSHA[ci.SHA]; present {
			continue
		}
		node := makeCommitNode(ci.SHA, ci, filePath)
		// Override confidence on the Commit row so downstream queries
		// can spot recovery-track commits without a join through Hunk.
		node.Confidence = types.ConfAmbiguous
		commitNodes = append(commitNodes, node)
		commitIDByteSHA[ci.SHA] = node.ID
	}
	g.Nodes = append(g.Nodes, commitNodes...)

	// Reuse the existing hunk-build helper so binary / large-patch /
	// adjacency semantics stay consistent across the EXTRACTED and
	// AMBIGUOUS paths. Pass the unreachable hunks directly — they
	// already have repo-rooted file paths since git show emits the
	// same shape git log -p does.
	relHunks, prefix := remapHunksToSrcRel(hunks, srcRel)
	if len(relHunks) == 0 {
		return nil, nil
	}
	// Build subjectBySHA from BOTH the previously-emitted commits and
	// the newly-discovered unreachable ones — the latter were just
	// appended to g.Nodes via the commitNodes loop above, so they're
	// already visible.
	subjectBySHA := commitSubjectMapFromGraph(g)
	nodes, hasHunkEdges, adjacentEdges, blobs := buildHunkNodes(relHunks, prefix, commitIDByteSHA, subjectBySHA, types.ConfAmbiguous)
	g.Nodes = append(g.Nodes, nodes...)
	g.Edges = append(g.Edges, hasHunkEdges...)
	g.Edges = append(g.Edges, adjacentEdges...)
	return blobs, nil
}

// remapHunksToSrcRel filters hunks whose FilePath falls outside srcRoot's
// subtree and rewrites the surviving paths to be srcRoot-relative. Returns
// (filtered hunks, normalized prefix) so buildHunkNodes can stash the prefix
// for non-{go,ts,sol} language inference.
func remapHunksToSrcRel(hunks []temporal.HunkInfo, srcRel string) ([]temporal.HunkInfo, string) {
	prefix := ""
	if srcRel != "." && srcRel != "" {
		prefix = strings.TrimSuffix(srcRel, "/") + "/"
	}
	out := make([]temporal.HunkInfo, 0, len(hunks))
	for _, h := range hunks {
		var rel string
		switch {
		case prefix == "":
			rel = h.FilePath
		case strings.HasPrefix(h.FilePath, prefix):
			rel = h.FilePath[len(prefix):]
		default:
			continue
		}
		if rel == "" {
			continue
		}
		h.FilePath = rel
		out = append(out, h)
	}
	return out, prefix
}

// buildHunkNodes converts the post-remap HunkInfo slice into NodeHunk +
// EdgeHasHunk + EdgeAdjacent + gzip-compressed blob map. Stable ID hash
// derived from MakeID(qname, "git", 0) where qname encodes
// (sha, file, idx) — multiple hunks per (commit, file) get distinct IDs.
//
// adjacent edges connect within-(commit, file) hunks ordered by their
// new-file start line. Output is sorted by hunk node ID for deterministic
// graph snapshots, but the adjacency relation is computed BEFORE sorting
// so the line-order semantics are preserved.
//
// confidence stamps both the Hunk node and the has_hunk/adjacent edges:
// EXTRACTED for the HEAD-reachable pass, AMBIGUOUS for the unreachable
// follow-up pass per docs/design/hunk-graph.md §11.3.
//
// subjectBySHA maps each commit SHA → its subject line; used by H4
// (§10.4) to extract issue/ticket IDs into the Hunk node's
// doc_comment column with the `issues:ID1;ID2` prefix. Pass nil for
// the empty map — H4 enrichment silently no-ops.
func buildHunkNodes(hunks []temporal.HunkInfo, _ string,
	commitIDByteSHA map[string]string,
	subjectBySHA map[string]string,
	confidence types.Confidence) ([]types.Node, []types.Edge, []types.Edge, map[string][]byte) {
	// Group by (sha, file) so adjacent edges only fire within one commit-
	// file pair, in line-order. We sort each group by NewStart so the
	// "next-in-this-file" semantics match human reading order.
	type key struct{ sha, file string }
	groups := map[key][]*temporal.HunkInfo{}
	order := []key{}
	for i := range hunks {
		h := &hunks[i]
		k := key{h.SHA, h.FilePath}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], h)
	}

	nodes := make([]types.Node, 0, len(hunks))
	hasHunk := make([]types.Edge, 0, len(hunks))
	var adjacent []types.Edge
	blobs := map[string][]byte{}

	for _, k := range order {
		grp := groups[k]
		// Sort within the group by NewStart so adjacent edges follow
		// reading order. NewStart=0 (deletion-only hunks) comes first;
		// ties break on Index to keep parse order as the secondary key.
		sort.SliceStable(grp, func(i, j int) bool {
			if grp[i].NewStart != grp[j].NewStart {
				return grp[i].NewStart < grp[j].NewStart
			}
			return grp[i].Index < grp[j].Index
		})

		commitID, ok := commitIDByteSHA[k.sha]
		if !ok {
			// Commit wasn't materialised — either the file overlap test
			// dropped it or the patch stream had a header we couldn't
			// parse. Skip silently; the hunks have no anchor.
			continue
		}

		var prevHunkID string
		// Pre-compute the parent commit's issue IDs once per (commit, file)
		// group — every hunk in the group inherits the same encoding
		// since the source is the commit subject (§10.4).
		commitDocComment := ""
		if subjectBySHA != nil {
			if subj, ok := subjectBySHA[k.sha]; ok && subj != "" {
				commitDocComment = temporal.EncodeIssueIDs(temporal.ExtractIssueIDs(subj))
			}
		}
		for _, h := range grp {
			node := makeHunkNode(*h, confidence)
			if commitDocComment != "" {
				node.DocComment = commitDocComment
			}
			nodes = append(nodes, node)
			hasHunk = append(hasHunk, types.Edge{
				Src:        commitID,
				Dst:        node.ID,
				Type:       types.EdgeHasHunk,
				FilePath:   h.FilePath,
				Count:      1,
				Confidence: confidence,
			})
			if prevHunkID != "" {
				adjacent = append(adjacent, types.Edge{
					Src:        prevHunkID,
					Dst:        node.ID,
					Type:       types.EdgeAdjacent,
					FilePath:   h.FilePath,
					Count:      1,
					Confidence: confidence,
				})
			}
			prevHunkID = node.ID
			if !h.Binary && len(h.Patch) > 0 {
				gz, err := gzipPatch(capPatchText(h.Patch))
				if err == nil && len(gz) > 0 {
					blobs[node.ID] = gz
				}
			}
		}
	}
	sortNodesByID(nodes)
	return nodes, hasHunk, adjacent, blobs
}

// makeHunkNode produces one NodeHunk row. Node ID is stable across rebuilds
// for the same (sha, file, idx) tuple via MakeID. FilePath is the target
// file (post-remap), so viewer queries "show me the hunks that touched
// main.go" can filter by file_path directly. doc_comment / signature stay
// empty in H1 — they're reserved for H4's issue-id extraction.
//
// confidence is stamped on the row so the §11.3 hybrid (EXTRACTED for
// HEAD-reachable, AMBIGUOUS for reflog/fsck-collected unreachable) can
// be distinguished by H3 retrieval queries via a single SQL filter.
func makeHunkNode(h temporal.HunkInfo, confidence types.Confidence) types.Node {
	qname := fmt.Sprintf("hunk:%s:%s:%d", h.SHA, h.FilePath, h.Index)
	displayName := h.SHA
	if len(displayName) > 12 {
		displayName = displayName[:12]
	}
	displayName = fmt.Sprintf("%s:%s:%d", displayName, h.FilePath, h.Index)
	startLine, endLine := h.NewStart, h.NewStart
	// NewLines == 0 happens for deletion-only hunks; keep StartLine valid
	// (>=1 per Node.StartLine validate tag) by clamping to 1.
	if startLine < 1 {
		startLine = 1
	}
	if h.NewLines > 0 {
		endLine = h.NewStart + h.NewLines - 1
	}
	if endLine < startLine {
		endLine = startLine
	}
	return types.Node{
		ID:            parse.MakeID(qname, "git", 0),
		Type:          types.NodeHunk,
		Name:          displayName,
		QualifiedName: qname,
		FilePath:      h.FilePath,
		StartLine:     startLine,
		EndLine:       endLine,
		StartByte:     0,
		EndByte:       1, // sentinel — patch text is in blobs.source, not a byte slice
		Language:      hunkLanguageFor(h.FilePath),
		SubKind:       "git",
		Confidence:    confidence,
	}
}

// hunkLanguageFor returns the language label for a hunk's target file.
// Files outside the {go, ts, sol} whitelist get the 'git' sentinel —
// matches the language enum already used by NodeCommit and lets the
// viewer's language filter naturally exclude documentation/config hunks
// from language-specific views.
func hunkLanguageFor(filePath string) string {
	for ext, lang := range hunkLanguageWhitelist {
		if strings.HasSuffix(filePath, ext) {
			return lang
		}
	}
	return "git"
}

// capPatchText enforces the §11.6 64KB cap on raw patch text before
// gzip compression. Patches under the cap pass through unchanged. Larger
// patches are truncated to first 32 KB + a single-line marker + last
// 32 KB so retrieval still sees both ends of the change while the
// middle (typically a regeneration's bulk content) is summarized.
func capPatchText(b []byte) []byte {
	if len(b) <= hunkPatchCap {
		return b
	}
	// Reserve room for the marker so the final length stays under cap.
	const markerTmpl = "\n[... truncated, %d bytes ...]\n"
	dropped := len(b) - 2*32*1024 // about hunkPatchCap-2*32KB
	marker := []byte(fmt.Sprintf(markerTmpl, dropped))
	half := (hunkPatchCap - len(marker)) / 2
	if half <= 0 || half >= len(b) {
		return b // pathological — fall back to passthrough
	}
	out := make([]byte, 0, hunkPatchCap)
	out = append(out, b[:half]...)
	out = append(out, marker...)
	out = append(out, b[len(b)-half:]...)
	return out
}

// gzipPatch compresses raw patch bytes with the stdlib gzip writer at
// default level. The empirical 70% size reduction on real diff text
// (high-entropy code interleaved with low-entropy `+`/`-` markers and
// repeated context lines) is plenty given the §11.6 64KB pre-cap; level
// tuning is a non-goal for H1.
func gzipPatch(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write(b); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// emitModifiesEdges (H2) walks every Hunk node and emits a `modifies`
// edge for each whitelisted CodeNode in the same file whose
// [start_line, end_line] interval overlaps the hunk's. Confidence
// follows the hunk: EXTRACTED for HEAD-reachable hunks, AMBIGUOUS for
// the unreachable-history track populated by emitUnreachableHunkGraph.
//
// Algorithm (mirrors design §4.1):
//
//	for hunk in hunks(g):
//	    for codeNode in nodesByFile[hunk.file_path]:
//	        if !modifiesNodeWhitelist[codeNode.Type] continue
//	        if overlap(hunk_lines, codeNode_lines) emit modifies(hunk -> codeNode)
//
// Determinism: hunks iterate in g.Nodes order (already deterministic
// from buildHunkNodes' sortNodesByID); candidates iterate in their
// original parser-emission order (file-path bucket preserves insertion).
//
// Performance: per design §4.3, 700 hunks × ~50 candidates × O(1) test
// ≈ 35K comparisons, < 50 ms. Real go-stablenet (~9K hunks × ~190
// candidates) ~1.7M ops — still sub-second.
func emitModifiesEdges(g *graph.Graph) {
	newEdges := BuildModifiesEdges(g.Nodes)
	if len(newEdges) > 0 {
		g.Edges = append(g.Edges, newEdges...)
	}
}

// BuildModifiesEdges retained as a buildpipe-internal alias for
// historical callers and tests; the canonical implementation lives
// in pkg/hunkmodifies so consumers outside this repository's
// internal tree can compose the H2 join without crossing the
// internal boundary. Both forms call the same underlying function.
func BuildModifiesEdges(nodes []types.Node) []types.Edge {
	return hunkmodifies.BuildEdges(nodes)
}
