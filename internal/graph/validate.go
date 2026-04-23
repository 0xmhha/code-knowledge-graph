package graph

import (
	"fmt"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Validate enforces the CKG invariants:
//   - every edge.src and edge.dst references an existing node ID (no dangling refs)
//   - every node and edge has a valid Confidence label
//   - every node has a known NodeType
//   - every edge has a known EdgeType
//
// Returns the FIRST violation; callers may iterate further if useful.
func Validate(g *Graph) error {
	ids := make(map[string]struct{}, len(g.Nodes))
	validNT := make(map[types.NodeType]struct{})
	for _, t := range types.AllNodeTypes() {
		validNT[t] = struct{}{}
	}
	validET := make(map[types.EdgeType]struct{})
	for _, t := range types.AllEdgeTypes() {
		validET[t] = struct{}{}
	}
	for _, n := range g.Nodes {
		if _, ok := validNT[n.Type]; !ok {
			return fmt.Errorf("node %s: unknown type %q", n.ID, n.Type)
		}
		if !n.Confidence.Valid() {
			return fmt.Errorf("node %s: invalid confidence %q", n.ID, n.Confidence)
		}
		ids[n.ID] = struct{}{}
	}
	for _, e := range g.Edges {
		if _, ok := validET[e.Type]; !ok {
			return fmt.Errorf("edge %s->%s: unknown type %q", e.Src, e.Dst, e.Type)
		}
		if !e.Confidence.Valid() {
			return fmt.Errorf("edge %s->%s: invalid confidence %q", e.Src, e.Dst, e.Confidence)
		}
		if _, ok := ids[e.Src]; !ok {
			return fmt.Errorf("dangling src on edge of type %s: %s -> %s", e.Type, e.Src, e.Dst)
		}
		if _, ok := ids[e.Dst]; !ok {
			return fmt.Errorf("dangling dst on edge of type %s: %s -> %s", e.Type, e.Src, e.Dst)
		}
	}
	return nil
}
