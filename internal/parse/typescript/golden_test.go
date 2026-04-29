package typescript_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// updateGolden, when true, rewrites the golden file from the live parser
// output instead of asserting equality. Refresh after intentional schema
// changes via:
//
//	go test ./internal/parse/typescript/... -run TestGolden_TypeScript -update
//
// The flag is parser-package-local so refreshing TS golden does not also
// rewrite Sol golden by accident.
var updateGolden = flag.Bool("update", false, "rewrite golden file from live parser output")

// TestGolden_TypeScript locks in the post-A1 (smacker → tree-sitter) shape
// of the TS parser. Walks testdata/synthetic/ts-frontend/src, parses every
// .ts/.tsx/.js file, runs Resolve, marshals the union to a deterministic
// JSON form, and diffs against testdata/ts_frontend_golden.json.
//
// Stability strategy:
//   - Sort nodes by ID, edges by (Src,Dst,Type).
//   - Marshal Node/Edge with the parser-layer fields only — pagerank /
//     usage_score / in_degree / out_degree are zero at this layer (scored
//     in buildpipe). The test asserts they ARE zero rather than omitting
//     them, so a future leak from buildpipe into the parser would fail loudly.
func TestGolden_TypeScript(t *testing.T) {
	fixture := filepath.Join("..", "..", "..", "testdata", "synthetic", "ts-frontend", "src")
	goldenPath := filepath.Join("testdata", "ts_frontend_golden.json")

	got, err := parseDirTS(fixture)
	if err != nil {
		t.Fatalf("parseDirTS: %v", err)
	}
	assertParserLayerInvariants(t, got)

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
		first := firstDivergence(string(want), string(out))
		t.Fatalf("golden mismatch (rerun with -update to refresh):\n  first divergence near: %s\n  golden: %s\n  parser size: %d bytes; golden size: %d bytes",
			first.context, goldenPath, len(out), len(want))
	}
}

// parseDirTS walks fixture, parses every file the TS parser claims, runs
// Resolve, and returns a deterministic snapshot. Built as a test-local
// helper rather than parser API extension — the golden test is the only
// caller, and the parser interface stays focused on per-file Pass 1.
func parseDirTS(fixture string) (*goldenSnapshot, error) {
	p := tsp.New(fixture)
	exts := extSet(p.Extensions())
	files, err := walkExt(fixture, exts)
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
	return snapshot(g.Nodes, g.Edges), nil
}

// goldenSnapshot is the JSON shape committed to disk. Nodes and Edges are
// sorted; the slices use the parser's existing types so any field-level
// schema change shows up as a diff.
type goldenSnapshot struct {
	Nodes []types.Node `json:"nodes"`
	Edges []types.Edge `json:"edges"`
}

// snapshot returns nodes/edges in canonical order:
//   - Nodes sorted by ID (16-char hex, deterministic from content+offset).
//   - Edges sorted by (Type, Src, Dst, Line) — covers the case where the
//     same (Src,Dst) pair has multiple distinct edge types.
//
// Edge.ID is zeroed because it's a SQLite rowid assigned at persist time
// (always 0 in parser output, but explicit zeroing protects against future
// leakage of in-memory counters).
func snapshot(nodes []types.Node, edges []types.Edge) *goldenSnapshot {
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
	return &goldenSnapshot{Nodes: nCopy, Edges: eCopy}
}

// assertParserLayerInvariants verifies the parser does NOT populate the
// scoring fields (PageRank, UsageScore, InDegree, OutDegree) — those are
// computed in buildpipe. A regression here means scoring leaked into the
// parser layer, which would make goldens unstable across builds.
func assertParserLayerInvariants(t *testing.T, g *goldenSnapshot) {
	t.Helper()
	for _, n := range g.Nodes {
		if n.PageRank != 0 || n.UsageScore != 0 || n.InDegree != 0 || n.OutDegree != 0 {
			t.Errorf("parser-layer node has non-zero scoring field: %s pagerank=%v usage=%v in=%d out=%d",
				n.ID, n.PageRank, n.UsageScore, n.InDegree, n.OutDegree)
		}
	}
}

// walkExt returns every regular file under root whose lowercased extension
// is in exts. Directory traversal is depth-first; ordering does not matter
// because snapshot() re-sorts the per-file results.
func walkExt(root string, exts map[string]bool) ([]string, error) {
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

func extSet(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[strings.ToLower(x)] = true
	}
	return m
}

// divergence captures the first character offset where two strings differ
// plus a small surrounding window for the failure message.
type divergence struct {
	offset  int
	context string
}

// firstDivergence reports the first byte offset where want and got differ
// and a 60-character window around it. Used in the failure message so the
// developer knows which field of which node/edge changed.
func firstDivergence(want, got string) divergence {
	n := len(want)
	if len(got) < n {
		n = len(got)
	}
	for i := 0; i < n; i++ {
		if want[i] != got[i] {
			return divergence{offset: i, context: contextWindow(got, i, 60)}
		}
	}
	if len(want) != len(got) {
		return divergence{offset: n, context: "length differs (truncation or extension)"}
	}
	return divergence{}
}

func contextWindow(s string, at, radius int) string {
	lo := at - radius
	if lo < 0 {
		lo = 0
	}
	hi := at + radius
	if hi > len(s) {
		hi = len(s)
	}
	return strings.ReplaceAll(s[lo:hi], "\n", `\n`)
}
