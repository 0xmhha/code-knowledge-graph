// Package buildpipe — temporal.go wires CKS G6 Temporal edges (E4) into
// the cold-rebuild path. Conceptually:
//
//   - Run a single `git log --raw` over the repo root containing srcRoot.
//   - Translate repo-rooted paths into srcRoot-relative slash paths so they
//     align with the rel paths the parsers stamped on Node.FilePath.
//   - For every distinct commit, append a NodeCommit (one per SHA).
//   - For every (file, commit) the log surfaces, emit `changed_in` from
//     EVERY symbol in that file → that commit (file-level heuristic; line-
//     level blame is deferred — see EdgeChangedIn doc-comment).
//   - For every file, emit ONE `blame` edge from its File node → its most
//     recent commit (V0 simplification of `file:line → commit`).
//
// Skips silently (no error) when srcRoot isn't inside a git checkout, so
// non-git source trees still build cleanly without temporal edges.
package buildpipe

import (
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/internal/temporal"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// temporalDepthDefault bounds per-file commit count when callers don't pass
// an explicit Options.TemporalDepth. 10 is the spec recommendation in the
// E4 task brief; keeps storage bounded for large monorepos while still
// surfacing enough churn signal for the viewer.
const temporalDepthDefault = 10

// emitTemporalEdges adds NodeCommit + NodeHunk nodes and the corresponding
// G6 Temporal edges (changed_in / blame for the file-level pass; has_hunk /
// adjacent for the Hunk-graph H1 pass) to g. Returns the gzip-compressed
// hunk patch blobs keyed by Hunk node ID — caller merges them into the
// InsertBlobs map alongside CodeNode source slices.
//
// Behavior:
//   - srcRoot not in a git checkout → no-op, log debug, return (nil, nil).
//   - git history empty for srcRoot subtree → no-op, log debug, return (nil, nil).
//   - Otherwise: append nodes/edges in place. Caller MUST re-run
//     graph.Validate after to catch any dangling refs introduced.
//
// The maxPerFile parameter caps per-file commit count for the changed_in /
// blame pass; pass 0 to use the E4 default (10). The hunk pass uses its
// own commit cap (temporal.LoadHunks default) since hunks are bounded by
// total commits walked, not per-file.
func emitTemporalEdges(g *graph.Graph, srcRoot string, log *slog.Logger, maxPerFile int) (map[string][]byte, error) {
	if maxPerFile <= 0 {
		maxPerFile = temporalDepthDefault
	}
	repoRoot, srcRel, ok := gitRepoRel(srcRoot)
	if !ok {
		log.Debug("temporal: srcRoot is not a git checkout; skipping G6 edges", "src", srcRoot)
		return nil, nil
	}
	hist, err := temporal.LoadHistory(repoRoot, maxPerFile)
	if err != nil {
		return nil, fmt.Errorf("temporal LoadHistory: %w", err)
	}
	if len(hist.Commits) == 0 || len(hist.Files) == 0 {
		log.Debug("temporal: empty git history under srcRoot; no G6 edges emitted",
			"repo", repoRoot, "rel", srcRel)
		return nil, nil
	}
	// Translate repo-rooted paths → srcRoot-relative slash paths. The
	// parsers stamp Node.FilePath using filepath.Rel(srcRoot, file) which
	// is the OS separator on disk; we normalise to forward slashes so the
	// match is portable across darwin/linux/windows.
	relHist := remapToSrcRel(hist.Files, srcRel)
	if len(relHist) == 0 {
		log.Debug("temporal: no overlap between srcRoot subtree and git history",
			"repo", repoRoot, "rel", srcRel)
		return nil, nil
	}

	commitNodes := buildCommitNodes(hist.Commits, srcRel, relHist)
	g.Nodes = append(g.Nodes, commitNodes...)

	nodesByPath, fileByPath := indexNodesByPath(g.Nodes)
	commitIDByteSHA := commitIDs(commitNodes)
	changedIn, blame := buildTemporalEdges(relHist, nodesByPath, fileByPath, commitIDByteSHA)
	g.Edges = append(g.Edges, changedIn...)
	g.Edges = append(g.Edges, blame...)

	// Hunk-graph H1: append NodeHunk + has_hunk + adjacent edges, returning
	// the gzip-compressed patch blobs. Hunks reuse the same commitIDByteSHA
	// map so they anchor on the commits we just emitted (and only those —
	// hunks for commits the file-overlap test dropped are skipped).
	hunkBlobs, err := emitHunkGraph(g, srcRel, commitIDByteSHA, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("temporal hunk graph: %w", err)
	}

	// Hunk-graph §11.3 follow-up: append AMBIGUOUS-confidence Commit +
	// Hunk nodes for SHAs reachable only via reflog or fsck-unreachable.
	// These represent force-pushed-away history that the LLM retrieval
	// layer (H3, future) must filter out — but a human "Recovery"
	// workflow can browse them in the viewer when an agent has
	// overwritten code that needs to come back.
	unreachableBlobs, err := emitUnreachableHunkGraph(g, srcRel, repoRoot, commitIDByteSHA)
	if err != nil {
		return nil, fmt.Errorf("temporal unreachable hunk graph: %w", err)
	}
	maps.Copy(hunkBlobs, unreachableBlobs)

	// Hunk-graph H2: every Hunk gets `modifies` edges to whitelisted
	// CodeNodes in the same file whose line range overlaps the hunk's.
	// Runs after both EXTRACTED and AMBIGUOUS hunks have been added so
	// recovery-track hunks also get their AST overlap edges (still
	// confidence-stamped per their parent hunk so H3 retrieval boundary
	// can filter consistently).
	emitModifiesEdges(g)

	hunkCount, hunkEdgeCount := 0, 0
	for _, n := range g.Nodes {
		if n.Type == types.NodeHunk {
			hunkCount++
		}
	}
	for _, e := range g.Edges {
		if e.Type == types.EdgeHasHunk || e.Type == types.EdgeAdjacent {
			hunkEdgeCount++
		}
	}
	log.Info("temporal G6 emitted",
		"commit_nodes", len(commitNodes),
		"hunk_nodes", hunkCount,
		"changed_in_edges", len(changedIn),
		"blame_edges", len(blame),
		"hunk_edges", hunkEdgeCount,
		"hunk_blobs", len(hunkBlobs),
		"max_per_file", maxPerFile)
	return hunkBlobs, nil
}

// remapToSrcRel filters out files outside srcRoot's subtree and rewrites
// their paths to be relative to srcRoot (slash form). srcRel is the
// repoRoot-relative slash path of srcRoot (or "." when srcRoot == repoRoot).
//
// Example: srcRel = "tools/cks", path = "tools/cks/internal/foo.go" →
// "internal/foo.go". A path outside srcRoot's subtree (e.g. "docs/bar.md"
// when srcRel = "tools/cks") is dropped.
func remapToSrcRel(files map[string][]string, srcRel string) map[string][]string {
	out := make(map[string][]string, len(files))
	prefix := ""
	if srcRel != "." && srcRel != "" {
		prefix = strings.TrimSuffix(srcRel, "/") + "/"
	}
	for path, shas := range files {
		var rel string
		switch {
		case prefix == "":
			rel = path
		case strings.HasPrefix(path, prefix):
			rel = path[len(prefix):]
		default:
			continue // outside srcRoot subtree
		}
		if rel == "" {
			continue
		}
		out[rel] = shas
	}
	return out
}

// buildCommitNodes constructs one NodeCommit per distinct SHA referenced
// from relHist, in stable-sorted SHA order so the graph snapshot is
// deterministic across builds. Only commits actually used by surviving
// (in-srcRoot) files are emitted — keeps the node count tight for
// sub-directory builds inside a large monorepo.
func buildCommitNodes(commits map[string]temporal.CommitInfo,
	srcRel string, relHist map[string][]string) []types.Node {
	wanted := map[string]bool{}
	for _, shas := range relHist {
		for _, sha := range shas {
			wanted[sha] = true
		}
	}
	out := make([]types.Node, 0, len(wanted))
	// Use srcRel as the node FilePath sentinel — commits don't live in any
	// real file, but FilePath is `validate:"required"` on Node and Pass-2
	// reload code paths expect non-empty. Convention: the rel path of the
	// build root, which is stable across builds.
	filePath := srcRel
	if filePath == "" || filePath == "." {
		filePath = ".git"
	}
	for sha := range wanted {
		info, ok := commits[sha]
		if !ok {
			continue
		}
		out = append(out, makeCommitNode(sha, info, filePath))
	}
	// Stable order (sort by SHA) so build snapshots compare cleanly.
	sortNodesByID(out)
	return out
}

// makeCommitNode produces a NodeCommit for one CommitInfo. ID derives from
// (`commit:<sha>`, "git", 0) so the same commit consistently hashes to the
// same node ID across rebuilds + across srcRoots inside the same repo.
func makeCommitNode(sha string, info temporal.CommitInfo, filePath string) types.Node {
	qname := "commit:" + sha
	displayName := sha
	if len(displayName) > 12 {
		displayName = displayName[:12]
	}
	signature := fmt.Sprintf("%d: %s", info.Timestamp, info.Subject)
	if len(signature) > 100 {
		signature = signature[:100]
	}
	return types.Node{
		ID:            parse.MakeID(qname, "git", 0),
		Type:          types.NodeCommit,
		Name:          displayName,
		QualifiedName: qname,
		FilePath:      filePath,
		StartLine:     1,
		EndLine:       1,
		StartByte:     0,
		EndByte:       1,
		Language:      "git", // sentinel for meta nodes (no source language). Picked specifically so DistinctFilePaths(language=go|ts|sol) audit queries don't see commit nodes — keeps the audit's per-language file-set diff clean.
		SubKind:       "git",
		Signature:     signature,
		Confidence:    types.ConfExtracted,
	}
}

// sortNodesByID sorts in place by ID for deterministic snapshots.
func sortNodesByID(ns []types.Node) {
	for i := 1; i < len(ns); i++ {
		for j := i; j > 0 && ns[j-1].ID > ns[j].ID; j-- {
			ns[j-1], ns[j] = ns[j], ns[j-1]
		}
	}
}

// commitIDs maps SHA → node ID for built commit nodes.
func commitIDs(commitNodes []types.Node) map[string]string {
	out := make(map[string]string, len(commitNodes))
	for _, n := range commitNodes {
		// QualifiedName is "commit:<sha>"; strip prefix for the SHA key.
		sha := strings.TrimPrefix(n.QualifiedName, "commit:")
		out[sha] = n.ID
	}
	return out
}

// indexNodesByPath returns (nodesByFile, fileNodeByFile) where keys are
// slash-normalised file paths. fileNodeByFile points at the single
// NodeFile per path (when present) — used as the `blame` edge source.
func indexNodesByPath(nodes []types.Node) (map[string][]string, map[string]string) {
	nodesByPath := map[string][]string{}
	fileByPath := map[string]string{}
	for _, n := range nodes {
		if n.FilePath == "" {
			continue
		}
		key := filepath.ToSlash(n.FilePath)
		nodesByPath[key] = append(nodesByPath[key], n.ID)
		if n.Type == types.NodeFile {
			fileByPath[key] = n.ID
		}
	}
	return nodesByPath, fileByPath
}

// buildTemporalEdges emits the two edge populations:
//
//   - changed_in: every symbol in a touched file → every commit that
//     touched that file (within the per-file cap).
//   - blame:      File node → most-recent commit touching that file.
//
// Files with no matching nodes (e.g. binaries, test fixtures the parser
// skipped) contribute nothing — silent miss is the correct behaviour.
func buildTemporalEdges(relHist map[string][]string, nodesByPath map[string][]string,
	fileByPath map[string]string, commitIDByteSHA map[string]string) ([]types.Edge, []types.Edge) {
	var changedIn, blame []types.Edge
	for path, shas := range relHist {
		nodeIDs, ok := nodesByPath[path]
		if !ok || len(nodeIDs) == 0 {
			continue
		}
		// changed_in for every (symbol, commit) pair on this file.
		for _, sha := range shas {
			cid, ok := commitIDByteSHA[sha]
			if !ok {
				continue
			}
			for _, nid := range nodeIDs {
				changedIn = append(changedIn, types.Edge{
					Src:        nid,
					Dst:        cid,
					Type:       types.EdgeChangedIn,
					FilePath:   path,
					Count:      1,
					Confidence: types.ConfExtracted,
				})
			}
		}
		// blame: File node → most recent commit (first entry by construction).
		if fileID, ok := fileByPath[path]; ok && len(shas) > 0 {
			if cid, ok := commitIDByteSHA[shas[0]]; ok {
				blame = append(blame, types.Edge{
					Src:        fileID,
					Dst:        cid,
					Type:       types.EdgeBlame,
					FilePath:   path,
					Count:      1,
					Confidence: types.ConfExtracted,
				})
			}
		}
	}
	return changedIn, blame
}
