package solidity

import (
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

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

	// W2 indexes:
	//   - funcByQName: "Contract.func" → []nodeID — explicit override
	//     lookup ("Parent.foo" TargetQName resolves here). The list is
	//     plural because real-world Sol builds can contain duplicate
	//     contract names across files (e.g. test fixtures with a shared
	//     `Base` name in two unrelated subtrees); resolveOverridesRef
	//     disambiguates by file path against the source function.
	//   - containerNameByID: nodeID → unqualified name. Used by
	//     bare-override resolution to label parent IDs from the
	//     inheritance index. Reverse-direction map (not map[string][]ID)
	//     because we always go ID → name, never the inverse.
	funcByQName := map[string][]string{}
	containerNameByID := map[string]string{}

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
			case types.NodeInterface:
				add(types.NodeInterface, n.Name, n.ID)
				containerNameByID[n.ID] = n.Name
			case types.NodeFunction:
				// W2: explicit override `override(A,B)` queues a
				// TargetQName of "Parent.method", so we index every Sol
				// function by its qualified name. Bare-override resolution
				// uses the same index, scoped by parent contract name.
				funcByQName[n.QualifiedName] = append(funcByQName[n.QualifiedName], n.ID)
			}
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
					pr, funcByQName, containerNameByID, parents, nodeFile,
				)
				out.Edges = append(out.Edges, edges...)
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
// The child's enclosing contract id is derived from
// pr.SrcID directly — the source function's Src in
// the W1 EdgeExtends/EdgeImplements edges is the contract node, not the
// function. We instead walk every (childContractID, parentIDs) entry in
// `parents` looking for the contract that contains pr.SrcID; that lookup
// is keyed off the function's qname prefix, which runFunctionDecl
// guarantees is "Contract.func" when the function sits inside a
// contract / interface / library.
//
// Confidence policy mirrors W1: same-file → ConfExtracted, cross-file →
// ConfInferred. Multiple parents in a single bare override fan out into
// multiple edges, each independently scored.
func resolveOverridesRef(
	pr parse.PendingRef,
	funcByQName map[string][]string,
	containerNameByID map[string]string,
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
		// Recover the enclosing contract of pr.SrcID. The function's qname
		// is "Container.func", so we scan funcByQName for the qname whose
		// ID list contains pr.SrcID. We then look up the container ID
		// from the function's filePath + the qname prefix — but the prefix
		// alone is ambiguous if multiple files declare a container of the
		// same name. Disambiguate by matching on file: only the same-file
		// container is a valid candidate (Sol functions can't span files).
		container, ok := enclosingContainerName(pr.SrcID, funcByQName)
		if !ok {
			return nil
		}
		// Locate the contract ID for the same-file container. The
		// containerNameByID reverse map gives us a single hop without
		// scanning funcByQName, but we still need the file-disambiguation
		// step against the source function's file.
		srcFile := nodeFile[pr.SrcID]
		var contractID string
		for cid, name := range containerNameByID {
			if name == container && nodeFile[cid] == srcFile {
				contractID = cid
				break
			}
		}
		if contractID == "" {
			return nil
		}
		parentIDs := parents[contractID]
		if len(parentIDs) == 0 {
			return nil
		}
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

// enclosingContainerName recovers the unqualified name of the contract /
// interface / library that owns the given function ID. Returns the prefix
// of the function's qualified name (everything before the first ".").
//
// Implementation: scan funcByQName for the entry whose ID list contains
// funcID, then split on ".". O(N) over the number of distinct function
// qnames in the build — sub-millisecond at real-world scale. A pre-built
// reverse index would help only if W2 became the dominant Pass 2 cost
// (it isn't — emits / has_modifier dominate).
//
// Returns ok=false when:
//   - the function isn't in funcByQName (shouldn't happen — every Function
//     node is indexed there in the same loop);
//   - the qname carries no "." (file-level Sol function without an
//     enclosing contract — V0 leaves these out of W2 scope, no resolver
//     hook for non-contract overrides exists in Sol semantics anyway).
func enclosingContainerName(
	funcID string,
	funcByQName map[string][]string,
) (string, bool) {
	for qname, ids := range funcByQName {
		hit := false
		for _, id := range ids {
			if id == funcID {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		dot := strings.IndexByte(qname, '.')
		if dot < 0 {
			return "", false
		}
		return qname[:dot], true
	}
	return "", false
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
	// global namespace in Solidity).
	var dstID string
	var parentType types.NodeType
	if ids := byName[types.NodeInterface][pr.TargetQName]; len(ids) > 0 {
		dstID = ids[0]
		parentType = types.NodeInterface
	} else if ids := byName[types.NodeContract][pr.TargetQName]; len(ids) > 0 {
		dstID = ids[0]
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
