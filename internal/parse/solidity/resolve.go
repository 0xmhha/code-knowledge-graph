package solidity

import (
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
			case types.NodeInterface:
				add(types.NodeInterface, n.Name, n.ID)
			}
		}
	}

	for _, r := range results {
		for _, pr := range r.Pending {
			// W1 inheritance branch — handled separately from the legacy
			// emits_event / has_modifier / writes_mapping path because
			// (a) it needs to consult both Contract and Interface tables,
			// (b) it reclassifies the EdgeType after resolution.
			if pr.DispatchKind == dispatchKindInherit {
				if edge, ok := resolveInheritanceRef(pr, byName, nodeFile, nodeType); ok {
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
