package solidity_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	sol "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// updateGolden, when true, rewrites the golden file from the live parser
// output instead of asserting equality. Refresh after intentional schema
// changes via:
//
//	go test ./internal/parse/solidity/... -run TestGolden_Solidity -update
//
// The flag is parser-package-local so refreshing Sol golden does not also
// rewrite TS golden by accident.
var updateGolden = flag.Bool("update", false, "rewrite golden file from live parser output")

// TestGolden_Solidity locks in the post-A2 (smacker → tree-sitter) shape of
// the Sol parser. Walks testdata/synthetic/sol-contract/contracts, parses
// every .sol file, runs Resolve, marshals the union to a deterministic
// JSON form, and diffs against testdata/sol_contract_golden.json.
func TestGolden_Solidity(t *testing.T) {
	fixture := filepath.Join("..", "..", "..", "testdata", "synthetic", "sol-contract", "contracts")
	goldenPath := filepath.Join("testdata", "sol_contract_golden.json")

	got, err := parseDirSol(fixture)
	if err != nil {
		t.Fatalf("parseDirSol: %v", err)
	}
	assertParserLayerInvariantsSol(t, got)

	out, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out = append(out, '\n')

	if *updateGolden {
		if err := os.WriteFile(goldenPath, out, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s (%d bytes)", goldenPath, len(out))
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (rerun with -update to create): %v", err)
	}
	if string(want) != string(out) {
		first := firstDivergenceSol(string(want), string(out))
		t.Fatalf("golden mismatch (rerun with -update to refresh):\n  first divergence near: %s\n  golden: %s\n  parser size: %d bytes; golden size: %d bytes",
			first, goldenPath, len(out), len(want))
	}
}

// parseDirSol walks fixture, parses every .sol file, runs Resolve, returns
// a deterministic snapshot. Test-local helper — same rationale as TS path.
func parseDirSol(fixture string) (*goldenSnapshotSol, error) {
	p := sol.New(fixture)
	exts := extSetSol(p.Extensions())
	files, err := walkExtSol(fixture, exts)
	if err != nil {
		return nil, err
	}
	results := make([]*parse.ParseResult, 0, len(files))
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		r, err := p.ParseFile(f, src)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	g, err := p.Resolve(results)
	if err != nil {
		return nil, err
	}
	return snapshotSol(g.Nodes, g.Edges), nil
}

// goldenSnapshotSol — same shape as TS, redeclared per-package to avoid
// cross-package test coupling (and circular imports).
type goldenSnapshotSol struct {
	Nodes []types.Node `json:"nodes"`
	Edges []types.Edge `json:"edges"`
}

func snapshotSol(nodes []types.Node, edges []types.Edge) *goldenSnapshotSol {
	nCopy := append([]types.Node(nil), nodes...)
	eCopy := append([]types.Edge(nil), edges...)
	sort.Slice(nCopy, func(i, j int) bool { return nCopy[i].ID < nCopy[j].ID })
	sort.Slice(eCopy, func(i, j int) bool {
		if eCopy[i].Type != eCopy[j].Type {
			return eCopy[i].Type < eCopy[j].Type
		}
		if eCopy[i].Src != eCopy[j].Src {
			return eCopy[i].Src < eCopy[j].Src
		}
		if eCopy[i].Dst != eCopy[j].Dst {
			return eCopy[i].Dst < eCopy[j].Dst
		}
		return eCopy[i].Line < eCopy[j].Line
	})
	for i := range eCopy {
		eCopy[i].ID = 0
	}
	return &goldenSnapshotSol{Nodes: nCopy, Edges: eCopy}
}

// assertParserLayerInvariantsSol confirms scoring fields are still zero at
// parser output (those get populated in buildpipe). Regression here would
// make goldens drift across builds.
func assertParserLayerInvariantsSol(t *testing.T, g *goldenSnapshotSol) {
	t.Helper()
	for _, n := range g.Nodes {
		if n.PageRank != 0 || n.UsageScore != 0 || n.InDegree != 0 || n.OutDegree != 0 {
			t.Errorf("parser-layer node has non-zero scoring field: %s pagerank=%v usage=%v in=%d out=%d",
				n.ID, n.PageRank, n.UsageScore, n.InDegree, n.OutDegree)
		}
	}
}

func walkExtSol(root string, exts map[string]bool) ([]string, error) {
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if exts[strings.ToLower(filepath.Ext(path))] {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func extSetSol(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[strings.ToLower(x)] = true
	}
	return m
}

// firstDivergenceSol returns a 60-char window around the first byte where
// want and got differ. Surfaced in the failure message so the developer
// can find the changed field without diffing the full golden by hand.
func firstDivergenceSol(want, got string) string {
	n := len(want)
	if len(got) < n {
		n = len(got)
	}
	for i := 0; i < n; i++ {
		if want[i] != got[i] {
			lo, hi := i-60, i+60
			if lo < 0 {
				lo = 0
			}
			if hi > len(got) {
				hi = len(got)
			}
			return strings.ReplaceAll(got[lo:hi], "\n", `\n`)
		}
	}
	if len(want) != len(got) {
		return "length differs (truncation or extension)"
	}
	return ""
}
