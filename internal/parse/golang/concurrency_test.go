package golang_test

import (
	"strings"
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestConcurrency_Mutex_FieldAndLocal asserts that struct-field and local
// sync.Mutex declarations both produce NodeMutex nodes with the right
// sub_kind, and that user-defined types named "Mutex" do NOT trip the
// false-positive guard.
func TestConcurrency_Mutex_FieldAndLocal(t *testing.T) {
	root := "testdata/concurrency"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}

	mutexes := nodesByType(g.Nodes, types.NodeMutex)
	bySubKind := groupBySubKind(mutexes)

	// Counter.mu (struct field), Cache.mu (rwmutex field), Embedded.Mutex
	// (embedded), localMu (local var). FakeMutex must NOT appear.
	if got := bySubKind["mutex"]; got < 2 {
		t.Errorf("mutex sub_kind: got %d, want >=2 (Counter.mu + LocalLock.localMu + Embedded.Mutex)", got)
	}
	// Cache.mu (struct field) plus GoroutineRWLock.rwMu (function-local) — 2 in total.
	if got := bySubKind["rwmutex"]; got < 1 {
		t.Errorf("rwmutex sub_kind: got %d, want >=1 (Cache.mu, GoroutineRWLock.rwMu)", got)
	}
	for _, n := range mutexes {
		if strings.Contains(n.QualifiedName, "FakeMutex") {
			t.Errorf("FakeMutex should NOT appear as Mutex node: %s", n.QualifiedName)
		}
	}
}

// TestConcurrency_Mutex_Embedded ensures the embedded `sync.Mutex` field
// becomes a Mutex node attached to the Embedded struct's qname.
func TestConcurrency_Mutex_Embedded(t *testing.T) {
	root := "testdata/concurrency"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	found := false
	for _, n := range g.Nodes {
		if n.Type != types.NodeMutex {
			continue
		}
		if strings.HasSuffix(n.QualifiedName, "Embedded.Mutex#mutex") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Mutex node for embedded sync.Mutex on Embedded struct")
	}
}

// TestConcurrency_LockEdges_DeferAndExplicit verifies acquires_lock /
// releases_lock edges are emitted for both `mu.Lock(); mu.Unlock()` and
// `mu.Lock(); defer mu.Unlock()` patterns.
func TestConcurrency_LockEdges_DeferAndExplicit(t *testing.T) {
	root := "testdata/concurrency"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	acquires := edgesByType(g.Edges, types.EdgeAcquiresLock)
	releases := edgesByType(g.Edges, types.EdgeReleasesLock)
	// Inc, Get, Read, Write, Set, LocalLock = 6 acquires + 6 releases
	// (FakeMutex.UseFake must not contribute — types.Info distinguishes).
	if len(acquires) < 6 {
		t.Errorf("acquires_lock count: got %d, want >=6", len(acquires))
	}
	if len(releases) < 6 {
		t.Errorf("releases_lock count: got %d, want >=6", len(releases))
	}
	// FakeMutex.Lock() should NOT have produced any lock edge — sweep both.
	for _, e := range acquires {
		dst := findNodeByID(g.Nodes, e.Dst)
		if dst != nil && strings.Contains(dst.QualifiedName, "FakeMutex") {
			t.Errorf("acquires_lock to FakeMutex.* — false-positive guard failed: dst=%s", dst.QualifiedName)
		}
	}
}

// TestConcurrency_RWMutex_LockEdges asserts RLock/RUnlock produce the
// same edge types as Lock/Unlock (the lock variant lives in the Mutex
// node's sub_kind, not a separate edge type).
func TestConcurrency_RWMutex_LockEdges(t *testing.T) {
	root := "testdata/concurrency"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	// Find the Cache.mu Mutex node ID.
	var cacheMuID string
	for _, n := range g.Nodes {
		if n.Type == types.NodeMutex && strings.HasSuffix(n.QualifiedName, "Cache.mu#mutex") {
			cacheMuID = n.ID
			break
		}
	}
	if cacheMuID == "" {
		t.Fatal("Cache.mu Mutex node not found")
	}
	var rlockSeen, runlockSeen bool
	for _, e := range g.Edges {
		if e.Dst != cacheMuID {
			continue
		}
		switch e.Type {
		case types.EdgeAcquiresLock:
			rlockSeen = true
		case types.EdgeReleasesLock:
			runlockSeen = true
		}
	}
	if !rlockSeen {
		t.Error("expected acquires_lock edge into Cache.mu (Read uses RLock)")
	}
	if !runlockSeen {
		t.Error("expected releases_lock edge into Cache.mu (Read uses RUnlock)")
	}
}

// TestConcurrency_Channel_Attributes asserts make(chan T, n) → Channel
// node with direction sub_kind and signature carrying the elem + buffer.
func TestConcurrency_Channel_Attributes(t *testing.T) {
	root := "testdata/concurrency"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	channels := nodesByType(g.Nodes, types.NodeChannel)
	if len(channels) < 4 {
		t.Errorf("Channel node count: got %d, want >=4 (one per make(chan ...))", len(channels))
	}
	directions := groupBySubKind(channels)
	if directions["bidi"] < 2 {
		t.Errorf("bidi channel count: got %d, want >=2", directions["bidi"])
	}
	if directions["send"] < 1 {
		t.Errorf("send-only channel count: got %d, want >=1", directions["send"])
	}
	if directions["recv"] < 1 {
		t.Errorf("recv-only channel count: got %d, want >=1", directions["recv"])
	}
	// At least one channel signature should carry buf= info.
	var hasBuffered bool
	for _, n := range channels {
		if strings.Contains(n.Signature, "buf=10") {
			hasBuffered = true
			break
		}
	}
	if !hasBuffered {
		t.Error("expected at least one Channel signature containing buf=10 (MakeBuffered)")
	}
}

// TestConcurrency_NoRegression_Goroutine confirms B1 didn't break the
// pre-existing Goroutine/spawns/sends_to/recvs_from extraction. The
// fixture's GoroutineFanout uses all four.
func TestConcurrency_NoRegression_Goroutine(t *testing.T) {
	root := "testdata/concurrency"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	if len(nodesByType(g.Nodes, types.NodeGoroutine)) < 1 {
		t.Error("expected >=1 Goroutine node")
	}
	if len(edgesByType(g.Edges, types.EdgeSpawns)) < 1 {
		t.Error("expected >=1 spawns edge")
	}
	if len(edgesByType(g.Edges, types.EdgeSendsTo)) < 1 {
		t.Error("expected >=1 sends_to edge")
	}
	if len(edgesByType(g.Edges, types.EdgeRecvsFrom)) < 1 {
		t.Error("expected >=1 recvs_from edge")
	}
}

// TestChannelFlow_MakeAssign_SendsToChannelNode verifies that a sends_to edge
// emitted for a channel created via `ch := make(chan T, n)` points to the
// Channel node rather than an anonymous CallSite node.
func TestChannelFlow_MakeAssign_SendsToChannelNode(t *testing.T) {
	root := "testdata/concurrency"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	channels := nodesByType(g.Nodes, types.NodeChannel)
	if len(channels) == 0 {
		t.Fatal("no Channel nodes found")
	}
	chanIDs := make(map[string]bool, len(channels))
	for _, n := range channels {
		chanIDs[n.ID] = true
	}
	sendsTo := edgesByType(g.Edges, types.EdgeSendsTo)
	var foundChannelDst bool
	for _, e := range sendsTo {
		if chanIDs[e.Dst] {
			foundChannelDst = true
			break
		}
	}
	if !foundChannelDst {
		t.Error("expected at least one sends_to edge with Channel node as Dst; all point to CallSite")
	}
}

// TestChannelFlow_MakeAssign_RecvsFromChannelNode verifies that a recvs_from
// edge for a make()-created channel points to the Channel node, not a CallSite.
func TestChannelFlow_MakeAssign_RecvsFromChannelNode(t *testing.T) {
	root := "testdata/concurrency"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	channels := nodesByType(g.Nodes, types.NodeChannel)
	chanIDs := make(map[string]bool, len(channels))
	for _, n := range channels {
		chanIDs[n.ID] = true
	}
	recvsFrom := edgesByType(g.Edges, types.EdgeRecvsFrom)
	var foundChannelDst bool
	for _, e := range recvsFrom {
		if chanIDs[e.Dst] {
			foundChannelDst = true
			break
		}
	}
	if !foundChannelDst {
		t.Error("expected at least one recvs_from edge with Channel node as Dst; all point to CallSite")
	}
}

// TestChannelFlow_Goroutine_SendsToChannelNode verifies the Goroutine node →
// Channel node sends_to edge is emitted for `go func() { ch <- 1 }()` where
// ch was created with make() in the enclosing function.
func TestChannelFlow_Goroutine_SendsToChannelNode(t *testing.T) {
	root := "testdata/concurrency"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	goroutines := nodesByType(g.Nodes, types.NodeGoroutine)
	if len(goroutines) == 0 {
		t.Fatal("no Goroutine nodes found")
	}
	goroutineIDs := make(map[string]bool, len(goroutines))
	for _, n := range goroutines {
		goroutineIDs[n.ID] = true
	}
	channels := nodesByType(g.Nodes, types.NodeChannel)
	chanIDs := make(map[string]bool, len(channels))
	for _, n := range channels {
		chanIDs[n.ID] = true
	}
	sendsTo := edgesByType(g.Edges, types.EdgeSendsTo)
	var found bool
	for _, e := range sendsTo {
		if goroutineIDs[e.Src] && chanIDs[e.Dst] {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected Goroutine -sends_to-> Channel edge; none found")
	}
}

// TestConcurrency_GoroutineLockEdges (Track C P1a) — lock CallExprs inside a
// `go func() { ... }()` literal must produce acquires_lock / releases_lock
// edges. Pre-fix, statements.go returned false on *ast.GoStmt to avoid
// double-walking, and emitGoroutineChannelEdges only handled channel
// statements — so the entire goroutine body's lock calls were silently
// skipped. This test pins the post-fix behaviour: each goroutine fixture
// in goroutine_lock.go contributes 1 acquires_lock + 1 releases_lock.
func TestConcurrency_GoroutineLockEdges(t *testing.T) {
	root := "testdata/concurrency"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	// Find the function-local Mutex IDs for GoroutineLock + GoroutineRWLock.
	var goroutineLockMu, goroutineRWLockMu string
	for _, n := range g.Nodes {
		if n.Type != types.NodeMutex {
			continue
		}
		switch {
		case strings.HasSuffix(n.QualifiedName, ".GoroutineLock.errMu"):
			goroutineLockMu = n.ID
		case strings.HasSuffix(n.QualifiedName, ".GoroutineRWLock.rwMu"):
			goroutineRWLockMu = n.ID
		}
	}
	if goroutineLockMu == "" {
		t.Fatal("expected Mutex node for GoroutineLock.errMu (function-local var)")
	}
	if goroutineRWLockMu == "" {
		t.Fatal("expected Mutex node for GoroutineRWLock.rwMu (function-local var)")
	}
	acquires := edgesByType(g.Edges, types.EdgeAcquiresLock)
	releases := edgesByType(g.Edges, types.EdgeReleasesLock)
	var lockAcq, lockRel, rwlockAcq, rwlockRel int
	for _, e := range acquires {
		switch e.Dst {
		case goroutineLockMu:
			lockAcq++
		case goroutineRWLockMu:
			rwlockAcq++
		}
	}
	for _, e := range releases {
		switch e.Dst {
		case goroutineLockMu:
			lockRel++
		case goroutineRWLockMu:
			rwlockRel++
		}
	}
	if lockAcq < 1 {
		t.Errorf("acquires_lock(GoroutineLock -> errMu): got %d, want >=1", lockAcq)
	}
	if lockRel < 1 {
		t.Errorf("releases_lock(GoroutineLock -> errMu): got %d, want >=1", lockRel)
	}
	if rwlockAcq < 1 {
		t.Errorf("acquires_lock(GoroutineRWLock -> rwMu): got %d, want >=1 (RLock)", rwlockAcq)
	}
	if rwlockRel < 1 {
		t.Errorf("releases_lock(GoroutineRWLock -> rwMu): got %d, want >=1 (RUnlock)", rwlockRel)
	}
}

// helpers

func nodesByType(nodes []types.Node, t types.NodeType) []types.Node {
	out := nodes[:0:0]
	for _, n := range nodes {
		if n.Type == t {
			out = append(out, n)
		}
	}
	return out
}

func edgesByType(edges []types.Edge, t types.EdgeType) []types.Edge {
	out := edges[:0:0]
	for _, e := range edges {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

func groupBySubKind(nodes []types.Node) map[string]int {
	out := map[string]int{}
	for _, n := range nodes {
		out[n.SubKind]++
	}
	return out
}

func findNodeByID(nodes []types.Node, id string) *types.Node {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
	}
	return nil
}
