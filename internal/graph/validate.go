package graph

import (
	"fmt"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// DanglingEdge captures an edge whose Src or Dst (or both) does not reference
// a node present in g.Nodes. Used by Inspect / Sanitize to report and drop
// orphaned references without aborting the whole build.
type DanglingEdge struct {
	Edge types.Edge
	Src  bool // true when edge.Src has no matching node
	Dst  bool // true when edge.Dst has no matching node
}

// ValidationReport summarizes the result of Inspect. Schema errors are
// fatal-worthy (unknown type / invalid confidence — these mean the emitter
// is broken). Dangling edges are recoverable: Sanitize drops them and the
// build proceeds with a warning. This separation lets dogfooding self-analysis
// proceed even when the parser emits an orphaned reference.
type ValidationReport struct {
	SchemaErrors  []error
	DanglingEdges []DanglingEdge
}

// HasSchemaErrors reports whether any unknown-type / invalid-confidence
// violations were found. These should always fail a build.
func (r *ValidationReport) HasSchemaErrors() bool { return len(r.SchemaErrors) > 0 }

// HasDangling reports whether at least one edge has a missing endpoint.
func (r *ValidationReport) HasDangling() bool { return len(r.DanglingEdges) > 0 }

// CountByEdgeType groups the dangling edges by their EdgeType. Useful for
// surfacing in build logs ("listens_on: 3, calls: 1").
func (r *ValidationReport) CountByEdgeType() map[types.EdgeType]int {
	out := make(map[types.EdgeType]int, len(r.DanglingEdges))
	for _, d := range r.DanglingEdges {
		out[d.Edge.Type]++
	}
	return out
}

// Inspect collects ALL validation issues in g without aborting on the first.
// Schema errors (unknown node/edge type, invalid confidence) and dangling
// references (edge.Src or edge.Dst not in g.Nodes) are reported separately.
//
// Inspect itself never returns an error — callers decide policy via the
// returned report. Use Validate for the legacy fatal-on-first-violation API.
func Inspect(g *Graph) *ValidationReport {
	report := &ValidationReport{}
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
			report.SchemaErrors = append(report.SchemaErrors,
				fmt.Errorf("node %s: unknown type %q", n.ID, n.Type))
			continue
		}
		if !n.Confidence.Valid() {
			report.SchemaErrors = append(report.SchemaErrors,
				fmt.Errorf("node %s: invalid confidence %q", n.ID, n.Confidence))
			continue
		}
		ids[n.ID] = struct{}{}
	}
	for _, e := range g.Edges {
		if _, ok := validET[e.Type]; !ok {
			report.SchemaErrors = append(report.SchemaErrors,
				fmt.Errorf("edge %s->%s: unknown type %q", e.Src, e.Dst, e.Type))
			continue
		}
		if !e.Confidence.Valid() {
			report.SchemaErrors = append(report.SchemaErrors,
				fmt.Errorf("edge %s->%s: invalid confidence %q", e.Src, e.Dst, e.Confidence))
			continue
		}
		_, srcOK := ids[e.Src]
		_, dstOK := ids[e.Dst]
		if !srcOK || !dstOK {
			report.DanglingEdges = append(report.DanglingEdges, DanglingEdge{
				Edge: e, Src: !srcOK, Dst: !dstOK,
			})
		}
	}
	return report
}

// Sanitize drops every edge in g flagged as dangling by report. In-place
// mutation; returns the number of edges dropped. Pre-condition: report was
// produced from Inspect(g) on this same graph (the dangling edge identity
// matches by all four (Type,Src,Dst,Line) fields).
func Sanitize(g *Graph, report *ValidationReport) int {
	if report == nil || len(report.DanglingEdges) == 0 {
		return 0
	}
	drop := make(map[edgeKey]struct{}, len(report.DanglingEdges))
	for _, d := range report.DanglingEdges {
		drop[edgeKey{d.Edge.Type, d.Edge.Src, d.Edge.Dst, d.Edge.Line}] = struct{}{}
	}
	kept := g.Edges[:0]
	dropped := 0
	for _, e := range g.Edges {
		if _, bad := drop[edgeKey{e.Type, e.Src, e.Dst, e.Line}]; bad {
			dropped++
			continue
		}
		kept = append(kept, e)
	}
	g.Edges = kept
	return dropped
}

// Validate enforces the CKG invariants strictly:
//   - every edge.src and edge.dst references an existing node ID
//   - every node and edge has a valid Confidence label
//   - every node has a known NodeType
//   - every edge has a known EdgeType
//
// Returns the FIRST violation as an error; preserved for backward
// compatibility with tests and strict-mode builds. New callers should
// prefer Inspect (which collects everything) plus Sanitize (which drops
// dangling edges) to enable lenient dogfooding flows.
func Validate(g *Graph) error {
	report := Inspect(g)
	if len(report.SchemaErrors) > 0 {
		return report.SchemaErrors[0]
	}
	if len(report.DanglingEdges) > 0 {
		d := report.DanglingEdges[0]
		if d.Src {
			return fmt.Errorf("dangling src on edge of type %s: %s -> %s",
				d.Edge.Type, d.Edge.Src, d.Edge.Dst)
		}
		return fmt.Errorf("dangling dst on edge of type %s: %s -> %s",
			d.Edge.Type, d.Edge.Src, d.Edge.Dst)
	}
	return nil
}
