package typescript_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestTSAsync_FixtureMatrix — W-B W2 acceptance check (schema 1.10,
// docs/design/ts-async-await-and-interface.md §3.2, §5.0). For each fixture:
//
//   - count of NodeAwaitPoint nodes (== count of EdgeAwaits — pair invariant)
//   - set of (parentFnName → []awaitDisplayName) for content correctness
//   - SubKind="async" on the expected async Function/Method nodes only
func TestTSAsync_FixtureMatrix(t *testing.T) {
	type want struct {
		file         string
		awaitByFn    map[string][]string // function name → sorted AwaitPoint Name list
		asyncFnNames []string            // Function/Method names with SubKind="async"
	}
	cases := []want{
		{
			file: "testdata/async/async_function.ts",
			awaitByFn: map[string][]string{
				"fetchUser": {"await fetch", "await json"},
			},
			asyncFnNames: []string{"fetchUser"},
		},
		{
			file: "testdata/async/async_method.ts",
			awaitByFn: map[string][]string{
				"load": {"await query"},
				"save": {"await write"},
			},
			asyncFnNames: []string{"load", "save"},
		},
		{
			file: "testdata/async/multi_awaits.ts",
			awaitByFn: map[string][]string{
				"pipeline": {"await stepA", "await stepB", "await stepC"},
			},
			asyncFnNames: []string{"pipeline", "stepA", "stepB", "stepC"},
		},
		{
			file:         "testdata/async/non_async.ts",
			awaitByFn:    nil, // no awaits
			asyncFnNames: nil, // no async functions
		},
		{
			file: "testdata/async/await_in_branch.ts",
			awaitByFn: map[string][]string{
				"branchy": {"await checkA", "await finalize", "await loopBody"},
			},
			asyncFnNames: []string{"branchy", "checkA", "finalize", "loopBody"},
		},
	}
	for _, c := range cases {
		t.Run(filepath.Base(c.file), func(t *testing.T) {
			src, err := os.ReadFile(c.file)
			if err != nil {
				t.Fatalf("read %s: %v", c.file, err)
			}
			p := tsp.New(".")
			r, err := p.ParseFile(c.file, src)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}
			byID := map[string]types.Node{}
			for _, n := range r.Nodes {
				byID[n.ID] = n
			}

			// 1. Pair invariant: every AwaitPoint has exactly one inbound EdgeAwaits.
			awaitPoints := map[string]types.Node{}
			for _, n := range r.Nodes {
				if n.Type == types.NodeAwaitPoint {
					awaitPoints[n.ID] = n
				}
			}
			awaitsEdges := 0
			for _, e := range r.Edges {
				if e.Type == types.EdgeAwaits {
					awaitsEdges++
					if _, ok := awaitPoints[e.Dst]; !ok {
						t.Errorf("EdgeAwaits points to non-AwaitPoint dst=%s", e.Dst)
					}
				}
			}
			if awaitsEdges != len(awaitPoints) {
				t.Errorf("pair invariant: %d AwaitPoint nodes, %d EdgeAwaits edges",
					len(awaitPoints), awaitsEdges)
			}

			// 2. Group AwaitPoints by parent fn name.
			gotAwait := map[string][]string{}
			for _, e := range r.Edges {
				if e.Type != types.EdgeAwaits {
					continue
				}
				parent := byID[e.Src]
				ap := byID[e.Dst]
				gotAwait[parent.Name] = append(gotAwait[parent.Name], ap.Name)
			}
			for k := range gotAwait {
				sort.Strings(gotAwait[k])
			}
			wantSorted := map[string][]string{}
			for k, v := range c.awaitByFn {
				cp := append([]string(nil), v...)
				sort.Strings(cp)
				wantSorted[k] = cp
			}
			if len(wantSorted) != len(gotAwait) {
				t.Errorf("await groups: got=%v want=%v", gotAwait, wantSorted)
			}
			for fn, awaits := range wantSorted {
				if !equalStrSlice(gotAwait[fn], awaits) {
					t.Errorf("await[%s]: got=%v want=%v", fn, gotAwait[fn], awaits)
				}
			}
			for fn := range gotAwait {
				if _, ok := wantSorted[fn]; !ok {
					t.Errorf("unexpected await group %q with %v", fn, gotAwait[fn])
				}
			}

			// 3. Async SubKind on declared Function/Method nodes.
			gotAsync := map[string]bool{}
			for _, n := range r.Nodes {
				if n.Type != types.NodeFunction && n.Type != types.NodeMethod {
					continue
				}
				if n.SubKind == "async" {
					gotAsync[n.Name] = true
				}
			}
			for _, fn := range c.asyncFnNames {
				if !gotAsync[fn] {
					t.Errorf("expected %s to carry SubKind=\"async\" (got=%v)", fn, gotAsync)
				}
				delete(gotAsync, fn)
			}
			for fn := range gotAsync {
				t.Errorf("unexpected SubKind=\"async\" on %s", fn)
			}
		})
	}
}

// TestTSAsync_AwaitPointSchemaInvariants — AwaitPoint nodes carry the
// position metadata the viewer/MCP needs. This guards against silent
// drift if a future edit forgets to set StartLine/EndLine or the
// QualifiedName naming convention.
func TestTSAsync_AwaitPointSchemaInvariants(t *testing.T) {
	src, err := os.ReadFile("testdata/async/multi_awaits.ts")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	p := tsp.New(".")
	r, err := p.ParseFile("testdata/async/multi_awaits.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	var awaits []types.Node
	for _, n := range r.Nodes {
		if n.Type == types.NodeAwaitPoint {
			awaits = append(awaits, n)
		}
	}
	if len(awaits) != 3 {
		t.Fatalf("AwaitPoint count: got %d, want 3", len(awaits))
	}
	for _, n := range awaits {
		if n.StartLine <= 0 || n.EndLine < n.StartLine {
			t.Errorf("AwaitPoint %s: invalid line range %d..%d",
				n.QualifiedName, n.StartLine, n.EndLine)
		}
		if n.StartByte < 0 || n.EndByte < n.StartByte {
			t.Errorf("AwaitPoint %s: invalid byte range %d..%d",
				n.QualifiedName, n.StartByte, n.EndByte)
		}
		if n.Language != "ts" {
			t.Errorf("AwaitPoint %s: language=%q, want \"ts\"", n.QualifiedName, n.Language)
		}
		if n.Confidence != types.ConfExtracted {
			t.Errorf("AwaitPoint %s: confidence=%q, want EXTRACTED", n.QualifiedName, n.Confidence)
		}
		if !strings.Contains(n.QualifiedName, "#AwaitPoint@") {
			t.Errorf("AwaitPoint qname %q missing #AwaitPoint@<offset> marker", n.QualifiedName)
		}
		if !strings.HasPrefix(n.Name, "await") {
			t.Errorf("AwaitPoint name=%q, want \"await\" prefix", n.Name)
		}
	}
}

// TestTSAsync_TopLevelAwaitDropped — module-scope `await` (no enclosing
// Function/Method) is dropped in V0. The fixture deliberately uses a
// raw `await` at module scope; the detector must NOT emit an AwaitPoint
// for it.
func TestTSAsync_TopLevelAwaitDropped(t *testing.T) {
	// Synthesised inline — keeping it out of testdata/ so we don't pollute
	// the fixture matrix with a deliberately ill-formed (but parseable) file.
	src := []byte(`// top-level await — V0 drops because no enclosing Function/Method.
const x = await someProm()
`)
	p := tsp.New(".")
	r, err := p.ParseFile("inline_top_level.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	for _, n := range r.Nodes {
		if n.Type == types.NodeAwaitPoint {
			t.Errorf("unexpected top-level AwaitPoint: %+v", n)
		}
	}
	for _, e := range r.Edges {
		if e.Type == types.EdgeAwaits {
			t.Errorf("unexpected top-level EdgeAwaits: %+v", e)
		}
	}
}
