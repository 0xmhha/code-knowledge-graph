package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pkgstore "github.com/0xmhha/code-knowledge-graph/pkg/store"
)

// alphaFileDump returns the contents of files containing the task's
// expected symbols. For symbol_set / code_patch tasks, uses
// Expected.Symbols (or Expected.MustUseSymbols). For rubric tasks
// without symbol hints, falls back to FTS search on the description.
//
// Up to maxAlphaFiles files are emitted, each in full (no truncation),
// matching the user-stated intent: "정답 코드가 포함된 파일의 전체 내용".
const maxAlphaFiles = 5

func alphaFileDump(store pkgstore.Reader, t Task) string {
	var seeds []string
	seeds = append(seeds, t.Expected.Symbols...)
	seeds = append(seeds, t.Expected.MustUseSymbols...)

	fileSet := map[string]struct{}{}
	for _, sym := range seeds {
		name := lastDotSegment(sym)
		if name == "" {
			continue
		}
		nodes, err := store.FindSymbol(name, false, pkgstore.FindSymbolOptions{})
		if err != nil {
			continue
		}
		for _, n := range nodes {
			if n.FilePath == "" {
				continue
			}
			if strings.EqualFold(n.QualifiedName, sym) || isQnameSuffix(n.QualifiedName, sym) {
				fileSet[n.FilePath] = struct{}{}
			}
		}
	}

	// Rubric fallback: keyword search on task description.
	if len(fileSet) == 0 {
		hits, err := store.Search(t.Description, 20)
		if err == nil {
			for _, n := range hits {
				if n.FilePath != "" {
					fileSet[n.FilePath] = struct{}{}
				}
				if len(fileSet) >= maxAlphaFiles {
					break
				}
			}
		}
	}

	files := setKeysStr(fileSet)
	sort.Strings(files)
	if len(files) > maxAlphaFiles {
		files = files[:maxAlphaFiles]
	}

	var b strings.Builder
	for _, rel := range files {
		path := filepath.Join(t.CorpusPath, rel)
		content, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(&b, "\n=== %s ===\n[read error: %v]\n", rel, err)
			continue
		}
		fmt.Fprintf(&b, "\n=== %s ===\n%s\n", rel, content)
	}
	return b.String()
}

// betaSubgraphDump returns the task-relevant subgraph: search task
// description → top candidates → 1-hop neighborhood → emit all nodes
// (full type/qname/signature/doc) + edges in the relevant region.
//
// Distinct from δ (smartContext): β supplies the raw subgraph without
// re-ranking, budget packing, or body trimming. The size signal
// matters — β is meant to be the "graph 통째 dump" upper bound.
const betaSeedTopK = 30

func betaSubgraphDump(store pkgstore.Reader, t Task) string {
	hits, err := store.Search(t.Description, betaSeedTopK)
	if err != nil || len(hits) == 0 {
		return "[no candidates]"
	}

	seedIDs := make([]string, 0, len(hits))
	nodeSet := map[string]struct{}{}
	for _, n := range hits {
		seedIDs = append(seedIDs, n.ID)
		nodeSet[n.ID] = struct{}{}
	}

	edges, err := store.QueryEdgesForNodes(seedIDs)
	if err == nil {
		for _, e := range edges {
			nodeSet[e.Src] = struct{}{}
			nodeSet[e.Dst] = struct{}{}
		}
	}

	allNodes, err := store.NodesByIDs(setKeysStr(nodeSet))
	if err != nil {
		allNodes = hits
	}

	return "Nodes:\n" + jsonString(allNodes) +
		"\nEdges:\n" + jsonString(edges)
}

func setKeysStr(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
