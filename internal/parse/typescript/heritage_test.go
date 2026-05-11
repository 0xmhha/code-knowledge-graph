package typescript_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestTSHeritage_FixtureMatrix — W-B W1 acceptance check (schema 1.10 §4.1,
// docs/design/ts-async-await-and-interface.md §3.1). Each fixture parses +
// resolves to a fixed (extends, implements) edge set with ConfExtracted
// (same-file) and child → parent direction.
//
// The matrix covers four shapes the spec calls out:
//   - simple_extends:        class extends class (single parent)
//   - class_implements:      class implements (multi-parent, no extends)
//   - interface_extends:     interface extends (single + multi-parent)
//   - multiple_implements:   class extends + implements (3 interfaces)
func TestTSHeritage_FixtureMatrix(t *testing.T) {
	type want struct {
		file              string
		extendsByChild    map[string][]string
		implementsByChild map[string][]string
	}
	cases := []want{
		{
			file: "testdata/heritage/simple_extends.ts",
			extendsByChild: map[string][]string{
				"Child": {"Parent"},
			},
		},
		{
			file: "testdata/heritage/class_implements.ts",
			implementsByChild: map[string][]string{
				"Service": {"ILogger", "IService"},
			},
		},
		{
			file: "testdata/heritage/interface_extends.ts",
			extendsByChild: map[string][]string{
				"IChild": {"IBase"},
				"IUnion": {"IBar", "IFoo"},
			},
		},
		{
			file: "testdata/heritage/multiple_implements.ts",
			extendsByChild: map[string][]string{
				"Combined": {"Base"},
			},
			implementsByChild: map[string][]string{
				"Combined": {"IAlpha", "IBeta", "IGamma"},
			},
		},
	}
	for _, c := range cases {
		t.Run(filepath.Base(c.file), func(t *testing.T) {
			g := parseResolveHeritage(t, c.file)
			byID := map[string]types.Node{}
			for _, n := range g.Nodes {
				byID[n.ID] = n
			}
			gotExt := collectHeritage(g.Edges, byID, types.EdgeExtends)
			gotImpl := collectHeritage(g.Edges, byID, types.EdgeImplements)
			assertHeritage(t, "extends", c.extendsByChild, gotExt)
			assertHeritage(t, "implements", c.implementsByChild, gotImpl)
			// Same-file invariant: every heritage edge must be ConfExtracted.
			for _, e := range g.Edges {
				if e.Type != types.EdgeExtends && e.Type != types.EdgeImplements {
					continue
				}
				if e.Confidence != types.ConfExtracted {
					t.Errorf("same-file heritage edge %s→%s: conf=%q, want EXTRACTED",
						byID[e.Src].Name, byID[e.Dst].Name, e.Confidence)
				}
			}
		})
	}
}

// TestTSHeritage_CrossFile — parent declared in a separate file from the
// child. Resolve must emit the edge with ConfInferred per the cross-file
// rule in resolve.go::resolveHeritageRef.
func TestTSHeritage_CrossFile(t *testing.T) {
	p := tsp.New(".")
	files := []string{
		"testdata/heritage/cross_file_base.ts",
		"testdata/heritage/cross_file_child.ts",
	}
	results := make([]*parse.ParseResult, 0, len(files))
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		r, err := p.ParseFile(f, src)
		if err != nil {
			t.Fatalf("ParseFile %s: %v", f, err)
		}
		results = append(results, r)
	}
	g, err := p.Resolve(results)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	byID := map[string]types.Node{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	var hit *types.Edge
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.Type != types.EdgeExtends {
			continue
		}
		if byID[e.Src].Name == "CrossChild" && byID[e.Dst].Name == "CrossBase" {
			hit = e
			break
		}
	}
	if hit == nil {
		t.Fatal("expected CrossChild → CrossBase EdgeExtends across files")
	}
	if hit.Confidence != types.ConfInferred {
		t.Errorf("cross-file extends conf=%q, want INFERRED", hit.Confidence)
	}
	// Negative: the resolver must NOT also attach CrossChild to itself or to
	// the file node (regression guard for SrcID byte-offset collisions).
	if byID[hit.Src].FilePath == byID[hit.Dst].FilePath {
		t.Errorf("cross-file edge endpoints in same file: src=%s dst=%s",
			byID[hit.Src].FilePath, byID[hit.Dst].FilePath)
	}
}

// TestTSHeritage_UnresolvedDropped — heritage PendingRefs whose parent name
// matches no Class/Interface in the resolved graph are dropped (graph.Validate
// rejects dangling edges, and the existing TS `calls` resolver does the same).
// Achieved by ParseFile-only on the child fixture (parent is in a *different*
// file we deliberately skip), then asserting zero heritage edges.
func TestTSHeritage_UnresolvedDropped(t *testing.T) {
	// Only parse the child — base.ts is intentionally omitted.
	g := parseResolveHeritage(t, "testdata/heritage/cross_file_child.ts")
	for _, e := range g.Edges {
		if e.Type == types.EdgeExtends || e.Type == types.EdgeImplements {
			t.Errorf("unexpected heritage edge with missing parent: %+v", e)
		}
	}
}

// TestTSHeritage_EdgeDirection — child → parent direction is load-bearing
// (downstream PageRank, blast-radius queries assume this convention).
// Asserts on every fixture that no edge points from a parent back to a child.
func TestTSHeritage_EdgeDirection(t *testing.T) {
	cases := []struct {
		file    string
		parents map[string]bool
	}{
		{"testdata/heritage/simple_extends.ts", map[string]bool{"Parent": true}},
		{"testdata/heritage/class_implements.ts", map[string]bool{"IService": true, "ILogger": true}},
		{"testdata/heritage/interface_extends.ts", map[string]bool{"IBase": true, "IFoo": true, "IBar": true}},
		{"testdata/heritage/multiple_implements.ts", map[string]bool{"Base": true, "IAlpha": true, "IBeta": true, "IGamma": true}},
	}
	for _, c := range cases {
		t.Run(filepath.Base(c.file), func(t *testing.T) {
			g := parseResolveHeritage(t, c.file)
			byID := map[string]types.Node{}
			for _, n := range g.Nodes {
				byID[n.ID] = n
			}
			for _, e := range g.Edges {
				if e.Type != types.EdgeExtends && e.Type != types.EdgeImplements {
					continue
				}
				srcName := byID[e.Src].Name
				if c.parents[srcName] {
					t.Errorf("inverted edge: parent %s appears as Src in %s edge → %s",
						srcName, e.Type, byID[e.Dst].Name)
				}
			}
		})
	}
}

// --- helpers ---------------------------------------------------------------

func parseResolveHeritage(t *testing.T, file string) *parse.ResolvedGraph {
	t.Helper()
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	p := tsp.New(".")
	r, err := p.ParseFile(file, src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	g, err := p.Resolve([]*parse.ParseResult{r})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return g
}

// collectHeritage groups edges of one type into childName → []parentName,
// sorting each parent list so comparisons don't depend on map iteration order.
func collectHeritage(edges []types.Edge, byID map[string]types.Node, et types.EdgeType) map[string][]string {
	out := map[string][]string{}
	for _, e := range edges {
		if e.Type != et {
			continue
		}
		src := byID[e.Src].Name
		dst := byID[e.Dst].Name
		out[src] = append(out[src], dst)
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

func assertHeritage(t *testing.T, label string, want, got map[string][]string) {
	t.Helper()
	if want == nil {
		want = map[string][]string{}
	}
	wantSorted := map[string][]string{}
	for k, v := range want {
		cp := append([]string(nil), v...)
		sort.Strings(cp)
		wantSorted[k] = cp
	}
	if len(wantSorted) != len(got) {
		t.Errorf("%s: child count got=%d want=%d (got=%v want=%v)",
			label, len(got), len(wantSorted), got, wantSorted)
	}
	for child, parents := range wantSorted {
		gp, ok := got[child]
		if !ok {
			t.Errorf("%s: missing child %q (got=%v)", label, child, got)
			continue
		}
		if !equalStrSlice(gp, parents) {
			t.Errorf("%s[%s]: got=%v want=%v", label, child, gp, parents)
		}
	}
	for child := range got {
		if _, ok := wantSorted[child]; !ok {
			t.Errorf("%s: unexpected child %q with parents %v", label, child, got[child])
		}
	}
}

func equalStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
