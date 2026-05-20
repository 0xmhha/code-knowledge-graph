// Package buildpipe — lock_propagation.go implements D1 Stage B (W-A,
// Within-language semantics Phase 5): cross-function lock propagation for
// the Go concurrency pass. Extends the existing intra-function
// accessed_under_lock detector (internal/parse/golang/concurrency_underlock.go)
// by walking the call graph from lock-holding functions into their callees
// and emitting accessed_under_lock(field, mutex) edges for fields touched
// inside reachable callee bodies.
//
// Spec reference: docs/design/go-cross-function-lock-propagation.md
// (decisions resolved 2026-05-11, §5.0 — Stage B DFS depth=5, INFERRED
// confidence for all cross-function emits, calls+invokes traversal,
// goroutine bodies forced to INFERRED, opt-in flag, dedup with confidence
// priority).
//
// Opt-in only: gated by Options.LockPropagation (CLI: --lock-propagation,
// default false). When the flag is off, the pass is a structural no-op —
// the existing intra-function B1 Phase 4 emit is unchanged.
package buildpipe

import (
	"log/slog"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// lockPropagationMaxDepth caps the DFS depth from any lock-holding function.
// Spec §3.2 (W-A §5.0 Q1): user-decided 3-5 range; we pick 5 as the upper
// bound to maximize transitive reach while still bounding noise. Larger
// depths would chase helper chains into stdlib-flavoured utility methods
// that almost never share the caller's critical section. Cycle protection
// is handled by the visited set, so the limit only bounds depth not graph
// traversal.
const lockPropagationMaxDepth = 5

// propagateLockedFieldAccess emits cross-function accessed_under_lock edges
// in place on g.Edges. The algorithm (spec §3.2):
//
//  1. Build callgraph adjacency from calls + invokes edges (W-A §5.0 Q3).
//  2. Index each function/method by its acquires_lock targets (the mutexes
//     it directly locks; intra-function emits already exist for these).
//  3. For each lock-holding function f with held mutex set H:
//     - DFS from f up to lockPropagationMaxDepth via callgraph adjacency.
//     - At every reachable callee c, for every field in funcFields[c], emit
//     accessed_under_lock(field, mutex) for each mutex in H.
//  4. Dedup against existing edges and within this pass (W-A §5.0 Q6):
//     priority EXTRACTED > INFERRED > AMBIGUOUS. Existing intra-function
//     edges (INFERRED) survive — we only add new (field, mutex) pairs.
//
// funcFields is the per-Function/Method body field-access set produced by
// the Go parser (parse/golang.Parser.FuncFieldTouches). Nil/empty disables
// the pass (no work possible — every callee's field set is unknown).
//
// Confidence policy (W-A §5.0 Q2 + Q4): all cross-function emits are
// INFERRED. Goroutine bodies entered through the DFS chain also stay
// INFERRED — the spec marks them as the lowest-trust path because async
// scheduling separates the lock-acquiring caller from the callee's
// runtime execution.
//
// Returns the number of new edges emitted (for KPI logging). Zero when
// the flag is off, funcFields is empty, or no lock-holding function exists.
func propagateLockedFieldAccess(g *graph.Graph, funcFields map[string]map[string]struct{}, log *slog.Logger) int {
	if g == nil || len(funcFields) == 0 {
		return 0
	}
	// nodeIDSet: every Function/Method/Goroutine node in the graph. Used to
	// skip callees that resolve outside the build (stdlib, external deps).
	// Spec §3.3 noise control: a `calls` edge whose dst isn't a node in g
	// (e.g. fmt.Println, third-party helpers) is silently dropped, preventing
	// the propagator from inventing edges into bodies it never parsed.
	nodeKind := make(map[string]types.NodeType, len(g.Nodes))
	for _, n := range g.Nodes {
		nodeKind[n.ID] = n.Type
	}

	// callees adjacency: funcID → []calleeID via `calls` + `invokes` edges
	// (W-A §5.0 Q3 — future-proof for invokes once interface dispatch
	// resolution lands). Self-loops are kept; the visited set in DFS handles
	// recursive functions naturally.
	callees := buildCalleeAdjacency(g.Edges, nodeKind)

	// holdersByFunc: funcID → []mutexID acquired by this function. Derived
	// from acquires_lock edges (already emitted by the Go concurrency pass).
	// Only Function/Method/Goroutine nodes appear as Src on acquires_lock.
	holdersByFunc := buildLockHolders(g.Edges)
	if len(holdersByFunc) == 0 {
		return 0
	}

	// existing: (field, mutex) pairs already in g.Edges. We dedup against
	// these so the propagator never produces a duplicate of an intra-function
	// edge already emitted by concurrency_underlock.go. Existing edge wins
	// per W-A §5.0 Q6 — we never demote an INFERRED to a lower confidence.
	existing := make(map[edgePairKey]struct{}, 64)
	for _, e := range g.Edges {
		if e.Type != types.EdgeAccessedUnderLock {
			continue
		}
		existing[edgePairKey{src: e.Src, dst: e.Dst}] = struct{}{}
	}

	emitted := 0
	for funcID, mutexes := range holdersByFunc {
		// DFS from funcID. visited prevents cycle revisits (W-A §5.0 Q1 —
		// visited set is the only cycle defence; depth limit is independent).
		visited := map[string]struct{}{funcID: {}}
		stack := []dfsFrame{{id: funcID, depth: 0}}
		for len(stack) > 0 {
			cur := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			// At the lock-holding function itself (depth 0), intra-function
			// emit already handled this; skip the field walk. Depth > 0 means
			// we crossed at least one call edge — that's the W-A territory.
			if cur.depth > 0 {
				if touched, ok := funcFields[cur.id]; ok {
					for fieldID := range touched {
						for _, mutexID := range mutexes {
							k := edgePairKey{src: fieldID, dst: mutexID}
							if _, dup := existing[k]; dup {
								continue
							}
							existing[k] = struct{}{}
							g.Edges = append(g.Edges, types.Edge{
								Src: fieldID, Dst: mutexID,
								Type:       types.EdgeAccessedUnderLock,
								Count:      1,
								Confidence: types.ConfInferred,
							})
							emitted++
						}
					}
				}
			}
			if cur.depth >= lockPropagationMaxDepth {
				continue
			}
			for _, callee := range callees[cur.id] {
				if _, seen := visited[callee]; seen {
					continue
				}
				// Skip nodes outside this build (stdlib, vendored deps). This is
				// the W-A §3.3 stdlib guard — a callee with no funcFields entry
				// AND no graph node is almost certainly external; including it
				// would chase phantom edges. We still descend through nodes
				// known to the graph even if they have no field touches (they
				// may transitively call functions that do).
				if _, ok := nodeKind[callee]; !ok {
					continue
				}
				visited[callee] = struct{}{}
				stack = append(stack, dfsFrame{id: callee, depth: cur.depth + 1})
			}
		}
	}

	if emitted > 0 && log != nil {
		log.Info("lock propagation",
			"new_accessed_under_lock", emitted,
			"lock_holders", len(holdersByFunc),
			"max_depth", lockPropagationMaxDepth)
	}
	return emitted
}

// dfsFrame is a (nodeID, depth) tuple consumed by the DFS stack. depth is
// the number of call edges traversed from the original lock-holding root,
// bounded by lockPropagationMaxDepth.
type dfsFrame struct {
	id    string
	depth int
}

// edgePairKey dedupes (src, dst) accessed_under_lock pairs. Type and
// confidence are constant across the propagator's emits (EdgeAccessedUnderLock
// + ConfInferred per W-A §5.0 Q2), so they're not part of the key.
type edgePairKey struct {
	src, dst string
}

// buildCalleeAdjacency returns funcID → []calleeID derived from
// calls + invokes edges (W-A §5.0 Q3). Both edge types are merged into a
// single adjacency list; the propagator treats them identically because the
// downstream emit confidence is INFERRED for both cases.
//
// nodeKind is consulted only to confirm src and dst are real nodes —
// dangling endpoints (which validateAndSanitize would have already dropped
// in strict mode but may persist in lenient mode) are skipped here too.
func buildCalleeAdjacency(edges []types.Edge, nodeKind map[string]types.NodeType) map[string][]string {
	adj := make(map[string][]string, 64)
	// dedup repeated (src, dst) pairs from emit double-counting in either
	// edge type (e.g. multi-line call expressions). DFS would handle dupes
	// anyway via visited, but keeping the adjacency tight makes the log
	// numbers in tests less noisy.
	seen := make(map[edgePairKey]struct{}, len(edges))
	for _, e := range edges {
		if e.Type != types.EdgeCalls && e.Type != types.EdgeInvokes {
			continue
		}
		if _, ok := nodeKind[e.Src]; !ok {
			continue
		}
		if _, ok := nodeKind[e.Dst]; !ok {
			continue
		}
		k := edgePairKey{src: e.Src, dst: e.Dst}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		adj[e.Src] = append(adj[e.Src], e.Dst)
	}
	return adj
}

// buildLockHolders returns funcID → []mutexID held by funcID, derived from
// acquires_lock edges already emitted by the Go concurrency pass. Each
// (funcID, mutexID) pair is deduped because acquires_lock can fire multiple
// times for the same mutex inside one body (e.g. mu.Lock() in both branches
// of an if/else); for propagation the held set is what matters, not the
// per-call multiplicity.
func buildLockHolders(edges []types.Edge) map[string][]string {
	out := make(map[string][]string, 16)
	seen := make(map[edgePairKey]struct{}, 32)
	for _, e := range edges {
		if e.Type != types.EdgeAcquiresLock {
			continue
		}
		k := edgePairKey{src: e.Src, dst: e.Dst}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out[e.Src] = append(out[e.Src], e.Dst)
	}
	return out
}
