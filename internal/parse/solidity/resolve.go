package solidity

import (
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W6 V1.0 binding-map types (2026-05-12). The per-contract using-for
// binding info reaches Pass 2 method-call resolution through two
// intermediate maps; declaring them as package types lets helpers
// keep narrow signatures. The third lookup (funcID → contractID) reuses
// the existing containerIDByFuncID map from W-C W2 review M1+M3.
//
//   bindingMap:    contractID → (typeName | "*") → libraryName
//   stateVarTypes: contractID → varName → typeName (NodeField.Signature)
type bindingMap map[string]map[string]string
type stateVarTypeMap map[string]map[string]string

// Resolve unions per-file results. V0 cross-file resolution is name-based:
// pending edges (emits_event, has_modifier, writes_mapping) are matched
// against any node whose Name (or QualifiedName for mappings) equals the
// pending TargetQName. Cross-file matches are tagged INFERRED; same-file
// matches stay EXTRACTED. Mirrors the TypeScript resolver.
//
// W1 (Sol inheritance, 2026-05-11): adds resolution for `is`-clause
// PendingRefs (DispatchKind="inherit"). The detector (inheritance.go)
// emits these with a provisional EdgeType=EdgeExtends; this resolver
// reclassifies to EdgeImplements when the resolved parent is an
// Interface node. Contract / Interface lookups share the byName index
// keyed by NodeType.
//
// W2 (Sol virtual/override, 2026-05-11): adds resolution for
// override-specifier PendingRefs (DispatchKind="override" /
// "override_explicit"). Bare overrides walk the inheritance graph emitted
// in W1 to find every parent that declares a same-name function; explicit
// `override(A, B)` looks up "Parent.method" directly. Both paths emit
// EdgeOverrides (child → parent) at ConfExtracted (same-file) or
// ConfInferred (cross-file).
//
// W3 (Sol interface dispatch, 2026-05-11): adds resolution for
// `IFoo(addr).bar()` PendingRefs (DispatchKind="interface_dispatch").
// Two-step lookup: (1) TypeName must resolve to a NodeInterface; (2)
// `TypeName.MethodName` must resolve to a Function declared on that
// interface. Both hits → emit EdgeInvokes at ConfAmbiguous (§5.0 Q5 —
// confidence is constant regardless of file boundary; runtime dispatch
// makes the resolved target an over-approximation). Any miss → drop
// (V0 strict-purge — keeps the AMBIGUOUS bucket scoped to real
// interface dispatch only).
func (p *Parser) Resolve(results []*parse.ParseResult) (*parse.ResolvedGraph, error) {
	out := &parse.ResolvedGraph{}

	// nodeFile maps node ID -> source file, so we can mark cross-file
	// resolutions as INFERRED.
	nodeFile := map[string]string{}
	// nodeType maps node ID -> declared type — used by W1 inheritance
	// resolution to reclassify EdgeExtends → EdgeImplements when the
	// target is an Interface.
	nodeType := map[string]types.NodeType{}
	// byName indexes resolvable nodes by their unqualified Name.
	byName := map[types.NodeType]map[string][]string{}
	add := func(nt types.NodeType, key, id string) {
		if byName[nt] == nil {
			byName[nt] = map[string][]string{}
		}
		byName[nt][key] = append(byName[nt][key], id)
	}

	// W2 indexes — populated in the same single pass over all per-file
	// results below. The three maps work together so bare-override
	// resolution can walk Function → enclosing Container → parent
	// Containers → parent Function in O(1) per hop:
	//
	//   - funcByQName: "Container.func" → []nodeID — explicit override
	//     lookup ("Parent.foo" TargetQName resolves here). The list is
	//     plural because real-world Sol builds can contain duplicate
	//     contract names across files (e.g. test fixtures with a shared
	//     `Base` name in two unrelated subtrees); resolveOverridesRef
	//     disambiguates by file path against the source function.
	//   - containerNameByID: containerID → unqualified name. ID-keyed
	//     reverse-direction map used to label parent contract IDs from
	//     the inheritance index when constructing the "Parent.method"
	//     qname for funcByQName lookup. Sol allows three container kinds
	//     (Contract / Interface / Library) — V0 W2 indexes the first two
	//     (Library has no override semantics in Sol).
	//   - containerIDByFuncID: funcID → enclosing containerID. Pre-built
	//     reverse index that replaces the O(N) scan over funcByQName +
	//     reverse scan over containerNameByID that bare-override
	//     resolution used to do per PendingRef. Population is two-step
	//     because Function ↔ Container association requires both nodes
	//     loaded first (same-file + name-prefix match); see the second
	//     loop below.
	funcByQName := map[string][]string{}
	containerNameByID := map[string]string{}
	containerIDByFuncID := map[string]string{}

	// containerByNameFile is a transient lookup map for the second pass —
	// keyed by (name + file) so a Function's enclosing container can be
	// resolved without scanning every container in the build. Sol
	// functions cannot span files, so file-scoping is a complete
	// disambiguator (two `Base` contracts in different files won't
	// shadow each other).
	containerByNameFile := map[string]string{}

	for _, r := range results {
		out.Nodes = append(out.Nodes, r.Nodes...)
		out.Edges = append(out.Edges, r.Edges...)
		for _, n := range r.Nodes {
			nodeFile[n.ID] = n.FilePath
			nodeType[n.ID] = n.Type
			switch n.Type {
			case types.NodeEvent:
				add(types.NodeEvent, n.Name, n.ID)
			case types.NodeModifier:
				add(types.NodeModifier, n.Name, n.ID)
			case types.NodeMapping:
				add(types.NodeMapping, n.QualifiedName, n.ID)
			// W1: index Contracts and Interfaces by Name so inheritance
			// PendingRefs (which only know the parent's unqualified name)
			// can resolve cross-file.
			case types.NodeContract:
				add(types.NodeContract, n.Name, n.ID)
				containerNameByID[n.ID] = n.Name
				containerByNameFile[n.Name+"|"+n.FilePath] = n.ID
			case types.NodeInterface:
				add(types.NodeInterface, n.Name, n.ID)
				containerNameByID[n.ID] = n.Name
				containerByNameFile[n.Name+"|"+n.FilePath] = n.ID
			case types.NodeFunction:
				// W2: explicit override `override(A,B)` queues a
				// TargetQName of "Parent.method", so we index every Sol
				// function by its qualified name. Bare-override resolution
				// uses the same index, scoped by parent contract name.
				funcByQName[n.QualifiedName] = append(funcByQName[n.QualifiedName], n.ID)
			}
		}
	}

	// Pass 1.5 — build containerIDByFuncID and the W6 V1.0 state-variable
	// type index. Both require Function / Container / Field nodes already
	// indexed (above), so they run as a separate loop.
	//
	//   containerIDByFuncID: funcID → enclosing containerID (W2 reverse
	//                        index, reused by W6 V1.0 for call→contract
	//                        recovery).
	//   stateVarTypes:       contractID → varName → declared typeName
	//                        (NodeField.Signature, set by runStateVarDecl
	//                        via extractTypeNameText). Drives method-call
	//                        receiver type lookup in Pass 2c.
	//
	// Both indexes derive the enclosing container from the node's
	// QualifiedName prefix (`<Container>.<member>`) — emitted by
	// runFunctionDecl and (since W6 V1.0) by runStateVarDecl. file-level
	// helpers without a container prefix are skipped (Sol allows free
	// functions but not free state vars; either way override / using-for
	// semantics don't apply).
	stateVarTypes := stateVarTypeMap{}
	for _, r := range results {
		for _, n := range r.Nodes {
			if n.Type != types.NodeFunction && n.Type != types.NodeField {
				continue
			}
			dot := strings.IndexByte(n.QualifiedName, '.')
			if dot < 0 {
				continue
			}
			containerName := n.QualifiedName[:dot]
			cid, ok := containerByNameFile[containerName+"|"+n.FilePath]
			if !ok {
				continue
			}
			if n.Type == types.NodeFunction {
				containerIDByFuncID[n.ID] = cid
				continue
			}
			// NodeField: stash typeName under the same container ID.
			if n.Signature == "" {
				continue // extraction-failed shapes
			}
			if stateVarTypes[cid] == nil {
				stateVarTypes[cid] = map[string]string{}
			}
			stateVarTypes[cid][n.Name] = n.Signature
		}
	}

	// Pass 2a — resolve W1 inheritance edges first, before W2 needs them.
	// W2 bare-override resolution walks the EdgeExtends/EdgeImplements
	// adjacency built in buildInheritanceIndex; that index must include
	// both same-file (already in out.Edges from Pass 1) and cross-file
	// (resolved here) inheritance. Splitting the pending iteration into
	// two sub-passes keeps the dependency explicit instead of relying on
	// per-result ordering.
	for _, r := range results {
		for _, pr := range r.Pending {
			if pr.DispatchKind != dispatchKindInherit {
				continue
			}
			if edge, ok := resolveInheritanceRef(pr, byName, nodeFile, nodeType); ok {
				out.Edges = append(out.Edges, edge)
			}
		}
	}

	// W2 inheritance edge index — contractID → []parentContractID. Built
	// from the (now fully-resolved) EdgeExtends + EdgeImplements edges
	// after Pass 2a above. Order is preserved (one entry per `is`-clause
	// parent in source order), so `override` against a multi-parent class
	// hits parents in the declared order.
	parents := buildInheritanceIndex(out.Edges)

	// W6 V1.0 (2026-05-12) — pre-build the (contractID, typeName) →
	// libraryName binding map by sweeping all dispatchKindUsingForTypeBind
	// PendingRefs across results. Done before Pass 2b so the using-for-call
	// branch can consume it without ordering surprises.
	bindings := bindingMap{}
	for _, r := range results {
		for _, pr := range r.Pending {
			if pr.DispatchKind != dispatchKindUsingForTypeBind {
				continue
			}
			// TargetQName encoding from runUsingFor: `libraryName|typeName`.
			sep := strings.IndexByte(pr.TargetQName, '|')
			if sep < 0 {
				continue
			}
			libName := pr.TargetQName[:sep]
			typeName := pr.TargetQName[sep+1:]
			if bindings[pr.SrcID] == nil {
				bindings[pr.SrcID] = map[string]string{}
			}
			bindings[pr.SrcID][typeName] = libName
		}
	}

	// Pass 2b — everything except W1 inheritance (already done) and any
	// future detector-specific branches go through this loop. W2 overrides
	// rely on the `parents` index built between the two sub-passes.
	for _, r := range results {
		for _, pr := range r.Pending {
			if pr.DispatchKind == dispatchKindInherit {
				continue // already handled in Pass 2a
			}
			// W2 override branch — fans out one EdgeOverrides per resolved
			// parent. Bare `override` consults the inheritance index; the
			// explicit form does a direct qname lookup.
			if pr.DispatchKind == dispatchKindOverride ||
				pr.DispatchKind == dispatchKindOverrideExplicit {
				edges := resolveOverridesRef(
					pr, funcByQName, containerNameByID,
					containerIDByFuncID, parents, nodeFile,
				)
				out.Edges = append(out.Edges, edges...)
				continue
			}
			// W3 interface-dispatch branch — resolves IFoo(addr).bar()
			// against the Interface index. Confidence is fixed at
			// ConfAmbiguous per §5.0 Q5 regardless of cross-file boundary.
			if pr.DispatchKind == dispatchKindInterfaceDispatch {
				if edge, ok := resolveInterfaceDispatchRef(
					pr, byName, funcByQName, nodeFile,
				); ok {
					out.Edges = append(out.Edges, edge)
				}
				continue
			}
			// W6 using-for branch — resolves `using LibName for ...` to
			// EdgeUsesFor (Container → Library). Library is emitted by W4
			// as NodeContract + SubKind="library", so we use the same
			// byName[NodeContract] index as inheritance resolution but
			// further filter to library-subkind nodes via the existing
			// containerNameByID map. Same-file → ConfExtracted, cross-file
			// → ConfInferred, unresolved → drop (V0 strict-purge).
			if pr.DispatchKind == dispatchKindUsingFor {
				if edge, ok := resolveUsingForRef(
					pr, byName, nodeFile,
				); ok {
					out.Edges = append(out.Edges, edge)
				}
				continue
			}
			// W6 V1.0 typebind — already consumed before this loop into
			// the `bindings` map. Skip silently here so the default
			// switch doesn't try to emit a graph edge for it.
			if pr.DispatchKind == dispatchKindUsingForTypeBind {
				continue
			}
			// W6 V1.0 using-for method-call branch — resolves
			// `<stateVar>.<method>(...)` to an EdgeCalls into the
			// library function bound for stateVar's type. Drops when
			// any link in the chain fails (no state var of that name,
			// no binding for that type, no library function with that
			// method) — strict-purge same as the other Sol resolvers.
			if pr.DispatchKind == dispatchKindUsingForCall {
				if edge, ok := resolveUsingForCallRef(
					pr, bindings, stateVarTypes,
					containerIDByFuncID, funcByQName, nodeFile,
				); ok {
					out.Edges = append(out.Edges, edge)
				}
				continue
			}
			var targetType types.NodeType
			switch pr.EdgeType {
			case types.EdgeEmitsEvent:
				targetType = types.NodeEvent
			case types.EdgeHasModifier:
				targetType = types.NodeModifier
			case types.EdgeWritesMapping:
				targetType = types.NodeMapping
			default:
				continue
			}
			ids := byName[targetType][pr.TargetQName]
			if len(ids) == 0 {
				continue
			}
			conf := types.ConfExtracted
			if nodeFile[pr.SrcID] != "" && nodeFile[ids[0]] != "" && nodeFile[pr.SrcID] != nodeFile[ids[0]] {
				conf = types.ConfInferred
			}
			out.Edges = append(out.Edges, types.Edge{
				Src: pr.SrcID, Dst: ids[0], Type: pr.EdgeType,
				Line: pr.Line, Count: 1, Confidence: conf,
			})
		}
	}
	return out, nil
}

// buildInheritanceIndex collects every EdgeExtends / EdgeImplements edge
// into a child → []parent adjacency map. Order is preserved in append
// order, matching the source-order semantics W1 emits (parents listed
// left-to-right in the `is`-clause). Bare-override resolution iterates
// this list to find every parent that declares a same-name virtual
// function, so order stability matters for deterministic edge ordering.
func buildInheritanceIndex(edges []types.Edge) map[string][]string {
	out := map[string][]string{}
	for _, e := range edges {
		if e.Type != types.EdgeExtends && e.Type != types.EdgeImplements {
			continue
		}
		out[e.Src] = append(out[e.Src], e.Dst)
	}
	return out
}

// resolveOverridesRef fans one W2 PendingRef out into zero or more
// EdgeOverrides edges. Two dispatch kinds:
//
//   - dispatchKindOverrideExplicit: TargetQName is "Parent.method".
//     Direct lookup in funcByQName; no inheritance walk.
//   - dispatchKindOverride: TargetQName is the bare method name.
//     We use the source function's already-resolved EdgeExtends /
//     EdgeImplements parents (via the `parents` adjacency keyed by the
//     enclosing contract ID), and emit one EdgeOverrides per parent that
//     declares a same-name function. Unresolved (no parent declares it)
//     → zero edges.
//
// The child's enclosing contract is recovered via the pre-built
// containerIDByFuncID reverse index (single hop, O(1)). The earlier
// implementation scanned funcByQName to extract the qname prefix and
// then scanned containerNameByID by (name + file) to recover the ID —
// both passes are subsumed by the index. M1 + M3 (W-C W2 review,
// 2026-05-12).
//
// Confidence policy mirrors W1: same-file → ConfExtracted, cross-file →
// ConfInferred. Multiple parents in a single bare override fan out into
// multiple edges, each independently scored.
func resolveOverridesRef(
	pr parse.PendingRef,
	funcByQName map[string][]string,
	containerNameByID map[string]string,
	containerIDByFuncID map[string]string,
	parents map[string][]string,
	nodeFile map[string]string,
) []types.Edge {
	switch pr.DispatchKind {
	case dispatchKindOverrideExplicit:
		ids := funcByQName[pr.TargetQName]
		if len(ids) == 0 {
			return nil
		}
		// Disambiguate when multiple candidates share the same "P.method"
		// qname (rare but legal — homonymous contracts across files).
		// Prefer a same-file match; fall back to the first candidate so
		// genuine cross-file explicit overrides still resolve.
		srcFile := nodeFile[pr.SrcID]
		dstID := ids[0]
		for _, candidate := range ids {
			if nodeFile[candidate] == srcFile {
				dstID = candidate
				break
			}
		}
		conf := types.ConfExtracted
		if srcFile != "" && nodeFile[dstID] != "" && srcFile != nodeFile[dstID] {
			conf = types.ConfInferred
		}
		return []types.Edge{{
			Src: pr.SrcID, Dst: dstID, Type: types.EdgeOverrides,
			Line: pr.Line, Count: 1, Confidence: conf,
		}}

	case dispatchKindOverride:
		// Recover the enclosing contract of pr.SrcID via the pre-built
		// reverse index. Sol functions can't span files, so the index
		// pairs each funcID with exactly one containerID; missing entries
		// represent file-level functions (no override semantics — drop).
		contractID, ok := containerIDByFuncID[pr.SrcID]
		if !ok {
			return nil
		}
		parentIDs := parents[contractID]
		if len(parentIDs) == 0 {
			return nil
		}
		srcFile := nodeFile[pr.SrcID]
		method := pr.TargetQName
		var out []types.Edge
		for _, pid := range parentIDs {
			parentName := containerNameByID[pid]
			if parentName == "" {
				continue
			}
			ids := funcByQName[parentName+"."+method]
			if len(ids) == 0 {
				continue
			}
			// When multiple containers share a name (rare but legal across
			// files), funcByQName[parent+"."+method] may carry several IDs.
			// Pick the one whose container ID matches the actual parent
			// id we're processing — every other candidate is a homonym
			// declared elsewhere. We compare by file (the parent
			// container's file must match the function's file).
			parentFile := nodeFile[pid]
			var dstID string
			for _, fid := range ids {
				if nodeFile[fid] == parentFile {
					dstID = fid
					break
				}
			}
			if dstID == "" {
				continue
			}
			conf := types.ConfExtracted
			if srcFile != "" && parentFile != "" && srcFile != parentFile {
				conf = types.ConfInferred
			}
			out = append(out, types.Edge{
				Src: pr.SrcID, Dst: dstID, Type: types.EdgeOverrides,
				Line: pr.Line, Count: 1, Confidence: conf,
			})
		}
		return out
	}
	return nil
}

// resolveUsingForRef resolves one W6 PendingRef (`using LibName for ...`)
// to a single EdgeUsesFor edge. The library reference uses bare name
// matching against the NodeContract index (libraries are emitted by W4 as
// NodeContract + SubKind="library").
//
// Confidence policy mirrors W1 / W2: same-file → ConfExtracted, cross-file
// → ConfInferred, unresolved → ok=false (caller drops the edge).
//
// We don't filter byName[NodeContract] hits to library-subkind only —
// rationale: Sol's `using` is grammar-permissive (the compiler enforces
// "for libraries only", but the AST has no such restriction). When a
// fixture genuinely binds against a non-library contract, the resolved
// EdgeUsesFor still lands; the graph consumer can filter by Library
// subkind downstream. Strict pre-filter would introduce a silent drop
// path that's hard to diagnose if the library declaration gets missed by
// W4 (real bug surface).
//
// Multiple homonymous libraries across files: prefer same-file via
// pickSameFileCandidate (same idiom as W1 / W2 explicit-override path).
func resolveUsingForRef(
	pr parse.PendingRef,
	byName map[types.NodeType]map[string][]string,
	nodeFile map[string]string,
) (types.Edge, bool) {
	ids := byName[types.NodeContract][pr.TargetQName]
	if len(ids) == 0 {
		return types.Edge{}, false
	}
	srcFile := nodeFile[pr.SrcID]
	dstID := pickSameFileCandidate(ids, srcFile, nodeFile)
	conf := types.ConfExtracted
	if srcFile != "" && nodeFile[dstID] != "" && srcFile != nodeFile[dstID] {
		conf = types.ConfInferred
	}
	return types.Edge{
		Src: pr.SrcID, Dst: dstID, Type: types.EdgeUsesFor,
		Line: pr.Line, Count: 1, Confidence: conf,
	}, true
}

// resolveUsingForCallRef resolves one W6 V1.0 method-call PendingRef
// (`<stateVar>.<method>(...)`) to a single EdgeCalls edge. Four-step
// chain — any step's failure drops the edge (V0 strict-purge):
//
//  1. funcID → enclosing containerID via containerIDByFuncID.
//  2. (containerID, receiverName) → typeName via stateVarTypes.
//  3. (containerID, typeName) → libraryName via bindings — falls back to
//     wildcard "*" binding when no specific binding exists (Q9-3 (a)
//     specific-first decision).
//  4. `<libraryName>.<methodName>` → libraryFunctionID via funcByQName.
//
// Confidence: ConfExtracted when both endpoints are in the same file
// AND came through the same-file binding; ConfInferred when the chain
// crosses files (library declared in another file). Sol's library
// dispatch is statically determinable once the binding is known, so we
// don't downgrade to AMBIGUOUS the way W3 (interface dispatch) does —
// the call resolution is concrete.
func resolveUsingForCallRef(
	pr parse.PendingRef,
	bindings bindingMap,
	stateVarTypes stateVarTypeMap,
	containerIDByFuncID map[string]string,
	funcByQName map[string][]string,
	nodeFile map[string]string,
) (types.Edge, bool) {
	// TargetQName encoding from runUsingForCalls: `receiverName|methodName`.
	sep := strings.IndexByte(pr.TargetQName, '|')
	if sep < 0 {
		return types.Edge{}, false
	}
	receiverName := pr.TargetQName[:sep]
	methodName := pr.TargetQName[sep+1:]

	contractID, ok := containerIDByFuncID[pr.SrcID]
	if !ok {
		return types.Edge{}, false
	}
	varMap := stateVarTypes[contractID]
	if varMap == nil {
		return types.Edge{}, false
	}
	typeName, ok := varMap[receiverName]
	if !ok {
		return types.Edge{}, false
	}
	bindMap := bindings[contractID]
	if bindMap == nil {
		return types.Edge{}, false
	}
	libName, hit := bindMap[typeName]
	if !hit {
		// Wildcard fallback per Q9-3 (a).
		libName, hit = bindMap["*"]
		if !hit {
			return types.Edge{}, false
		}
	}
	ids := funcByQName[libName+"."+methodName]
	if len(ids) == 0 {
		return types.Edge{}, false
	}
	srcFile := nodeFile[pr.SrcID]
	dstID := pickSameFileCandidate(ids, srcFile, nodeFile)
	conf := types.ConfExtracted
	if srcFile != "" && nodeFile[dstID] != "" && srcFile != nodeFile[dstID] {
		conf = types.ConfInferred
	}
	return types.Edge{
		Src: pr.SrcID, Dst: dstID, Type: types.EdgeCalls,
		Line: pr.Line, Count: 1, Confidence: conf,
	}, true
}

// pickSameFileCandidate returns the candidate ID whose file matches srcFile
// when one exists, otherwise the first candidate. Used by W1 inheritance and
// W2 explicit-override resolution to disambiguate homonymous targets across
// files: a same-file resolution is structurally more likely correct (a
// child won't usually inherit from a parent in an unrelated file with the
// same name), and falling back to the first ID keeps cross-file resolution
// working for genuine multi-file hierarchies. ids must be non-empty.
func pickSameFileCandidate(ids []string, srcFile string, nodeFile map[string]string) string {
	if srcFile != "" {
		for _, id := range ids {
			if nodeFile[id] == srcFile {
				return id
			}
		}
	}
	return ids[0]
}

// resolveInheritanceRef resolves one W1 PendingRef (a single `is X` parent
// reference) against the indexed Contract / Interface tables.
//
// Edge-type classification (§2.1 / §3.1) depends on *both* child and
// parent NodeType — Solidity allows three meaningful combinations:
//
//	child=Contract,  parent=Contract  → EdgeExtends    (`contract C is Base`)
//	child=Contract,  parent=Interface → EdgeImplements (`contract C is IFoo`)
//	child=Interface, parent=Interface → EdgeExtends    (`interface IB is IA`)
//
// (child=Interface, parent=Contract is syntactically invalid in Solidity
// — interfaces can only `is` other interfaces — so it's not handled
// here; solc rejects such code before our parser sees it.)
//
// Resolution order: prefer Interface first when both tables have a hit
// for the same name. Real codebases keep contract / interface namespaces
// disjoint, but solc itself uses the same lookup space — so this matches
// Solidity's own resolution semantics.
//
// Confidence policy (§2.2):
//   - same-file resolution  → ConfExtracted
//   - cross-file resolution → ConfInferred
//   - unresolved            → returns ok=false (caller drops the edge)
//
// Returns (edge, true) on success or (zero, false) when the parent name
// matches no known Contract / Interface in the build set.
func resolveInheritanceRef(
	pr parse.PendingRef,
	byName map[types.NodeType]map[string][]string,
	nodeFile map[string]string,
	nodeType map[string]types.NodeType,
) (types.Edge, bool) {
	// Locate the parent node — prefer Interface first to match solc's
	// name-resolution behaviour (interfaces and contracts share one
	// global namespace in Solidity). Within the matching type bucket,
	// prefer a same-file candidate so homonymous parents declared across
	// files don't shadow the locally-resolvable one. M2 (W-C W2 review,
	// 2026-05-12) — explicit override path already does this; the
	// inheritance path was the missing half.
	srcFile := nodeFile[pr.SrcID]
	var dstID string
	var parentType types.NodeType
	if ids := byName[types.NodeInterface][pr.TargetQName]; len(ids) > 0 {
		dstID = pickSameFileCandidate(ids, srcFile, nodeFile)
		parentType = types.NodeInterface
	} else if ids := byName[types.NodeContract][pr.TargetQName]; len(ids) > 0 {
		dstID = pickSameFileCandidate(ids, srcFile, nodeFile)
		parentType = types.NodeContract
	} else {
		return types.Edge{}, false
	}

	// Classify based on (child, parent). The only combination that maps
	// to EdgeImplements is (Contract, Interface) — a contract realising
	// an interface. Interface-to-interface inheritance is EdgeExtends
	// (interface IB is IA → IB extends IA, not implements).
	childType := nodeType[pr.SrcID]
	edgeType := types.EdgeExtends
	if childType == types.NodeContract && parentType == types.NodeInterface {
		edgeType = types.EdgeImplements
	}

	conf := types.ConfExtracted
	if nodeFile[pr.SrcID] != "" && nodeFile[dstID] != "" && nodeFile[pr.SrcID] != nodeFile[dstID] {
		conf = types.ConfInferred
	}
	return types.Edge{
		Src: pr.SrcID, Dst: dstID, Type: edgeType,
		Line: pr.Line, Count: 1, Confidence: conf,
	}, true
}

// resolveInterfaceDispatchRef resolves one W3 PendingRef (an
// `IFoo(addr).bar(...)` invocation) into a single EdgeInvokes edge.
//
// pr.TargetQName carries `TypeName.MethodName` (set in dispatch.go).
// Two predicates must hold for emission:
//
//  1. The leading `TypeName` must be a known NodeInterface in the build —
//     filters out plain identifier casts (`address(addr).foo`,
//     `MyContract(addr).foo`) which are not interface dispatch by the
//     spec definition.
//  2. The fully-qualified `TypeName.MethodName` must resolve to a
//     Function node — i.e. the interface declares a `bar(...)` method.
//     Unknown methods on a known interface (typos, evolving APIs across
//     branch builds) drop, matching W1/W2's strict-purge policy.
//
// Confidence is *constant* ConfAmbiguous (§5.0 Q5). This differs from
// W1 (file-boundary tagged) and W2 (file-boundary tagged) because the
// runtime address determines actual dispatch — the resolver can only
// identify the interface-method declaration, never the live target.
// The `llmSafeStoreReader` wrapper (hunk-graph §11.3) filters
// AMBIGUOUS edges from LLM-facing queries automatically.
//
// When multiple Function nodes share the same `TypeName.MethodName`
// qname (rare — would require duplicate interface declarations across
// files), pick the first candidate. Disambiguation could be sharpened
// by preferring same-file or the interface's own file, but with no
// real-world impact in the V0 corpus (validated against
// testdata/dispatch fixtures).
func resolveInterfaceDispatchRef(
	pr parse.PendingRef,
	byName map[types.NodeType]map[string][]string,
	funcByQName map[string][]string,
	nodeFile map[string]string,
) (types.Edge, bool) {
	// Split TargetQName on the first "." into (typeName, methodName).
	// dispatch.go always emits exactly one "." — no qualified parent
	// names in V0 (matches W1's known limitation: leading-identifier
	// only).
	dot := strings.IndexByte(pr.TargetQName, '.')
	if dot <= 0 || dot == len(pr.TargetQName)-1 {
		return types.Edge{}, false
	}
	typeName := pr.TargetQName[:dot]
	// Predicate 1: the leading type must be a NodeInterface. Plain
	// identifiers (variables, free functions, primitive type tokens
	// like `address`) miss the index and drop.
	if ids := byName[types.NodeInterface][typeName]; len(ids) == 0 {
		return types.Edge{}, false
	}
	// Predicate 2: the interface must declare the named method. Look
	// up by fully-qualified name (`Interface.method`).
	candidates := funcByQName[pr.TargetQName]
	if len(candidates) == 0 {
		return types.Edge{}, false
	}
	// Disambiguation: when multiple interfaces share the same name
	// across files (homonym across fixtures), prefer the candidate in
	// the source function's file, then any same-file as one of the
	// interface IDs. Fall back to candidates[0] when neither rule fires.
	srcFile := nodeFile[pr.SrcID]
	dstID := candidates[0]
	for _, fid := range candidates {
		if nodeFile[fid] == srcFile {
			dstID = fid
			break
		}
	}
	// AMBIGUOUS is the *fixed* confidence — see preamble + §5.0 Q5.
	return types.Edge{
		Src: pr.SrcID, Dst: dstID, Type: types.EdgeInvokes,
		Line: pr.Line, Count: 1, Confidence: types.ConfAmbiguous,
	}, true
}
