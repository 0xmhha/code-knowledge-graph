package concurrency_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/concurrency"
	"github.com/0xmhha/code-knowledge-graph/pkg/store"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// fakeReader implements store.Reader by embedding the interface (nil) and
// overriding only the two methods Analyze calls. Any other method would
// panic, proving Analyze depends on exactly FindSymbol + NeighborhoodByQname.
type fakeReader struct {
	store.Reader
	nodes map[string]types.Node
	edges []types.Edge
}

func (f *fakeReader) FindSymbol(name string, _ bool, _ store.FindSymbolOptions) ([]types.Node, error) {
	var out []types.Node
	for _, n := range f.nodes {
		if n.QualifiedName == name {
			out = append(out, n)
		}
	}
	return out, nil
}

// NeighborhoodByQname faithfully mirrors the sqlite reader: resolve roots via
// FindSymbol (include them), BFS to depth in one direction, filter by edge
// type, return the union node set + every traversed edge.
func (f *fakeReader) NeighborhoodByQname(qname string, depth int, reverse bool, edgeTypes ...string) ([]types.Node, []types.Edge, error) {
	roots, _ := f.FindSymbol(qname, true, store.FindSymbolOptions{})
	if len(roots) == 0 {
		return nil, nil, nil
	}
	want := map[string]bool{}
	for _, t := range edgeTypes {
		want[t] = true
	}
	allow := func(t types.EdgeType) bool { return len(want) == 0 || want[string(t)] }

	seen := map[string]types.Node{}
	frontier := map[string]bool{}
	for _, r := range roots {
		seen[r.ID] = r
		frontier[r.ID] = true
	}
	var allEdges []types.Edge
	for d := 0; d < depth; d++ {
		if len(frontier) == 0 {
			break
		}
		next := map[string]bool{}
		for _, e := range f.edges {
			if !allow(e.Type) {
				continue
			}
			from, to := e.Src, e.Dst
			if reverse {
				from, to = e.Dst, e.Src
			}
			if !frontier[from] {
				continue
			}
			allEdges = append(allEdges, e)
			if _, ok := seen[to]; !ok {
				if n, ok := f.nodes[to]; ok {
					seen[n.ID] = n
					next[to] = true
				}
			}
		}
		frontier = next
	}
	out := make([]types.Node, 0, len(seen))
	for _, n := range seen {
		out = append(out, n)
	}
	return out, allEdges, nil
}

func node(id, name, qname, file string, line int, kind types.NodeType) types.Node {
	return types.Node{ID: id, Name: name, QualifiedName: qname, FilePath: file, StartLine: line, Type: kind}
}

func edge(src, dst string, t types.EdgeType, line int) types.Edge {
	return types.Edge{Src: src, Dst: dst, Type: t, Line: line, Count: 1, Confidence: types.ConfInferred}
}

// fixture mirrors the REAL go-stablenet concurrency topology (verified against
// a 255k-node build): goroutine/channel/lock edges are anchored on the
// ENCLOSING FUNCTION, and channel ops point to a per-site CallSite node
// (Function -> sends_to/recvs_from -> CallSite), NOT Goroutine -> Channel.
//
//	Worker --spawns--> g1
//	Worker --sends_to--> cs_send (CallSite)
//	Peer   --spawns--> g2
//	Peer   --recvs_from--> cs_recv (CallSite)
//	Touch  --acquires_lock--> mu
//	Touch  --accessed_under_lock--> balance
//	Touch  --releases_lock--> mu        (must NOT surface)
func fixture() *fakeReader {
	nodes := []types.Node{
		node("n_worker", "Worker", "pkg.Worker", "a.go", 10, types.NodeFunction),
		node("n_g1", "goroutine", "pkg.Worker#Goroutine@12", "a.go", 12, types.NodeGoroutine),
		node("n_cs_send", "ch<-", "pkg.Worker#CallSite@13", "a.go", 13, types.NodeCallSite),
		node("n_peer", "Peer", "pkg.Peer", "b.go", 20, types.NodeFunction),
		node("n_g2", "goroutine", "pkg.Peer#Goroutine@22", "b.go", 22, types.NodeGoroutine),
		node("n_cs_recv", "<-ch", "pkg.Peer#CallSite@23", "b.go", 23, types.NodeCallSite),
		node("n_mu", "mu", "pkg.State.mu", "c.go", 30, types.NodeMutex),
		node("n_bal", "balance", "pkg.State.balance", "c.go", 31, types.NodeField),
		node("n_touch", "Touch", "pkg.Touch", "c.go", 35, types.NodeFunction),
	}
	m := map[string]types.Node{}
	for _, n := range nodes {
		m[n.ID] = n
	}
	return &fakeReader{
		nodes: m,
		edges: []types.Edge{
			edge("n_worker", "n_g1", types.EdgeSpawns, 12),
			edge("n_worker", "n_cs_send", types.EdgeSendsTo, 13), // Function -> CallSite (real)
			edge("n_peer", "n_g2", types.EdgeSpawns, 22),
			edge("n_peer", "n_cs_recv", types.EdgeRecvsFrom, 23), // Function -> CallSite (real)
			edge("n_touch", "n_mu", types.EdgeAcquiresLock, 36),
			edge("n_touch", "n_bal", types.EdgeAccessedUnderLock, 37),
			edge("n_touch", "n_mu", types.EdgeReleasesLock, 38), // excluded by design
		},
	}
}

func qnames(ms []concurrency.Module) map[string]concurrency.Module {
	out := map[string]concurrency.Module{}
	for _, m := range ms {
		out[m.Qname] = m
	}
	return out
}

func TestAnalyze_LockSeed_ExcludesReleasesLock(t *testing.T) {
	res, err := concurrency.Analyze(fixture(), "pkg.Touch", concurrency.Options{Depth: 2})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if res.NotFound {
		t.Fatal("seed pkg.Touch should resolve")
	}
	mods := qnames(res.Modules)
	mu, ok := mods["pkg.State.mu"]
	if !ok {
		t.Fatalf("expected pkg.State.mu in modules, got %+v", res.Modules)
	}
	if mu.Direction != "affected_by" {
		t.Errorf("mu direction = %q, want affected_by", mu.Direction)
	}
	if mu.Type != types.NodeMutex {
		t.Errorf("mu type = %q, want Mutex", mu.Type)
	}
	if _, ok := mods["pkg.State.balance"]; !ok {
		t.Errorf("expected pkg.State.balance in modules, got %+v", res.Modules)
	}
	if mu.Citation != "c.go:30" {
		t.Errorf("mu citation = %q, want c.go:30", mu.Citation)
	}
	for _, e := range res.Edges {
		if e[2].(string) == string(types.EdgeReleasesLock) {
			t.Errorf("releases_lock edge must NOT be surfaced, got %+v", e)
		}
	}
	var sawAcquire bool
	for _, e := range res.Edges {
		if e[2].(string) == string(types.EdgeAcquiresLock) {
			sawAcquire = true
		}
	}
	if !sawAcquire {
		t.Errorf("expected an acquires_lock edge, got %+v", res.Edges)
	}
}

func TestAnalyze_FieldSeed_ReverseFindsTouchers(t *testing.T) {
	// Byzantine query: "what touches this field under the same lock?"
	res, err := concurrency.Analyze(fixture(), "pkg.State.balance", concurrency.Options{Depth: 1})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	mods := qnames(res.Modules)
	touch, ok := mods["pkg.Touch"]
	if !ok {
		t.Fatalf("expected pkg.Touch (reverse) in modules, got %+v", res.Modules)
	}
	if touch.Direction != "affects" {
		t.Errorf("touch direction = %q, want affects", touch.Direction)
	}
}

func TestAnalyze_FunctionSeed_ForwardCallSiteTopology(t *testing.T) {
	// Real go-stablenet topology: a function reaches its own goroutine (spawns)
	// and the per-site CallSite of its channel op (sends_to), but NOT the peer
	// function across the channel (single-direction BFS; channel edges are
	// Function -> CallSite, not Goroutine -> Channel).
	res, err := concurrency.Analyze(fixture(), "pkg.Worker", concurrency.Options{Depth: 3})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	mods := qnames(res.Modules)
	if g := mods["pkg.Worker#Goroutine@12"]; g.Type != types.NodeGoroutine {
		t.Errorf("expected own Goroutine via spawns, got %+v", res.Modules)
	}
	cs, ok := mods["pkg.Worker#CallSite@13"]
	if !ok || cs.Type != types.NodeCallSite {
		t.Errorf("expected the send CallSite via sends_to, got %+v", res.Modules)
	}
	if _, ok := mods["pkg.Peer"]; ok {
		t.Errorf("function seed must NOT reach the peer across a CallSite, got %+v", res.Modules)
	}
}

func TestAnalyze_NotFound(t *testing.T) {
	res, err := concurrency.Analyze(fixture(), "pkg.DoesNotExist", concurrency.Options{Depth: 2})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !res.NotFound {
		t.Errorf("expected NotFound for unknown seed")
	}
	if len(res.Modules) != 0 {
		t.Errorf("expected no modules for unknown seed, got %+v", res.Modules)
	}
}

func TestAnalyze_MaxTotalAndDeterministicSort(t *testing.T) {
	res, err := concurrency.Analyze(fixture(), "pkg.Touch", concurrency.Options{Depth: 2, MaxTotal: 1})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(res.Modules) != 1 {
		t.Fatalf("MaxTotal=1 should cap to 1 module, got %d", len(res.Modules))
	}
	// Sorted by qname: "pkg.State.balance" < "pkg.State.mu".
	if got := res.Modules[0].Qname; got != "pkg.State.balance" {
		t.Errorf("first module qname = %q, want pkg.State.balance (deterministic sort)", got)
	}
}
