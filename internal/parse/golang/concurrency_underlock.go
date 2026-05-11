package golang

import (
	"go/ast"
	gotypes "go/types"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// concurrency_underlock.go — B1 Phase 4 (WORK-PLAN G8). Emits
// accessed_under_lock(field, mutex) edges for struct field accesses that
// occur inside a function holding a lock.
//
// V0 simplification (documented in commit message + WORK-PLAN G8):
// any field access inside a function that performs ANY Lock/RLock at any
// point is treated as "accessed under" all mutexes that function locks.
// This over-emits relative to a precise lexical scope analysis (a field
// touched BEFORE the Lock or AFTER the matching Unlock will still get an
// edge) but never miss-emits — important property for the schema slot
// (downstream consumers can be conservative about edges that exist; they
// can't recover edges that don't).
//
// Cross-function lock propagation (caller holds X, callee accesses field)
// is OUT OF SCOPE — that's D1 SSA territory. A function whose body holds
// no lock contributes zero edges here even if its callers all hold a lock.
//
// Requires typesInfo: without it we can't reliably distinguish a field
// access (`s.counter`) from a method call's receiver (`s.Lock()`). When
// typesInfo is nil this pass is a no-op (returns early).

// emitAccessedUnderLock walks fnBody after emitFunctionBodyPos has run.
// If the body contains at least one Lock/RLock call resolving to a known
// Mutex node, every distinct Field reference inside the body produces an
// accessed_under_lock(field, mutex) edge — one per (field, mutex) pair.
//
// Idempotency: dedup is per-function. Repeat calls overwrite no state and
// would produce duplicate edges; callers must invoke at most once per
// function-decl walk (visitFuncDecl wires it that way).
func (v *declVisitor) emitAccessedUnderLock(parentFuncID string, body *ast.BlockStmt) {
	if v.typesInfo == nil || body == nil || len(v.fieldNodeIDs) == 0 {
		return
	}
	mutexes := v.collectHeldMutexes(body)
	if len(mutexes) == 0 {
		return
	}
	fields := v.collectFieldAccesses(body)
	if len(fields) == 0 {
		return
	}
	emitted := make(map[edgeKey]struct{}, len(fields)*len(mutexes))
	for fieldID := range fields {
		for _, mutexID := range mutexes {
			k := edgeKey{src: fieldID, dst: mutexID}
			if _, dup := emitted[k]; dup {
				continue
			}
			emitted[k] = struct{}{}
			v.edges = append(v.edges, types.Edge{
				Src: fieldID, Dst: mutexID, Type: types.EdgeAccessedUnderLock,
				Count: 1, Confidence: types.ConfInferred,
				FilePath: v.relPath,
			})
		}
	}
	_ = parentFuncID // reserved: future precision pass may anchor on the function
}

// edgeKey dedupes (src, dst) pairs within a single function. Edge.Type is
// constant in this pass (accessed_under_lock) so it's not part of the key.
type edgeKey struct {
	src, dst string
}

// collectHeldMutexes returns Mutex node IDs that this function body locks
// at least once. Walks every CallExpr; for each Lock/RLock call whose
// receiver resolves to a known Mutex (via mutexNodeIDs or embedded-mutex
// chain), records the node ID.
//
// Returns a slice (not a set) because callers iterate it; the slice is
// already deduped by tracking node IDs in the seen-set during collection.
func (v *declVisitor) collectHeldMutexes(body *ast.BlockStmt) []string {
	seen := map[string]struct{}{}
	var out []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		method, ok := lockMethodName(call)
		if !ok {
			return true
		}
		// Only acquisitions count — Unlock/RUnlock signals release, not
		// "function holds the lock".
		if method == "Unlock" || method == "RUnlock" {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		mutexID, _ := v.resolveMutexReceiver(sel.X)
		if mutexID == "" {
			return true
		}
		if _, dup := seen[mutexID]; !dup {
			seen[mutexID] = struct{}{}
			out = append(out, mutexID)
		}
		return true
	})
	return out
}

// recordFuncFieldTouches stashes the funcID → set-of-field-IDs touched by
// body, regardless of whether body holds a lock. Consumed by buildpipe's
// W-A cross-function lock propagation pass (opt-in --lock-propagation).
//
// No-op when typesInfo is nil (AST-only mode) or fieldNodeIDs is empty
// (no struct fields declared in this file's scope — see emitFields).
//
// Idempotent per funcID: callers should invoke at most once per FuncDecl
// (visitFuncDecl wires it that way). Re-invocation merges into the existing
// set so deferred passes don't lose data.
func (v *declVisitor) recordFuncFieldTouches(funcID string, body *ast.BlockStmt) {
	if v.typesInfo == nil || body == nil || len(v.fieldNodeIDs) == 0 {
		return
	}
	fields := v.collectFieldAccesses(body)
	if len(fields) == 0 {
		return
	}
	if v.funcFieldTouches == nil {
		v.funcFieldTouches = map[string]map[string]struct{}{}
	}
	dst := v.funcFieldTouches[funcID]
	if dst == nil {
		dst = make(map[string]struct{}, len(fields))
		v.funcFieldTouches[funcID] = dst
	}
	for fid := range fields {
		dst[fid] = struct{}{}
	}
}

// collectFieldAccesses returns the set of Field node IDs referenced
// anywhere inside body. Reference detection is intentionally permissive:
// any *ast.SelectorExpr whose Sel resolves (via typesInfo.ObjectOf) to an
// object in fieldNodeIDs counts.
//
// Why a set (not slice): a field can be touched many times in a function
// body (`x.counter++; x.counter++`), but accessed_under_lock collapses
// that to one edge per (field, mutex) pair.
func (v *declVisitor) collectFieldAccesses(body *ast.BlockStmt) map[string]struct{} {
	out := map[string]struct{}{}
	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		obj := v.typesInfo.ObjectOf(sel.Sel)
		if obj == nil {
			return true
		}
		// Skip method receivers — `s.Lock()` resolves Sel to a *types.Func,
		// not a *types.Var. fieldNodeIDs is keyed on *types.Var (struct
		// fields), so this rejects naturally, but we early-out for clarity.
		if _, isVar := obj.(*gotypes.Var); !isVar {
			return true
		}
		if id, ok := v.fieldNodeIDs[obj]; ok {
			out[id] = struct{}{}
		}
		return true
	})
	return out
}
