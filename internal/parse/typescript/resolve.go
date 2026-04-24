package typescript

import (
	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Resolve unions per-file results. V0 cross-file resolution is name-based:
// a CallSite/Reference referencing identifier X resolves to the definition
// node whose Name == X, with INFERRED confidence when match is non-unique.
func (p *Parser) Resolve(results []*parse.ParseResult) (*parse.ResolvedGraph, error) {
	out := &parse.ResolvedGraph{}
	byName := map[string][]string{}
	for _, r := range results {
		out.Nodes = append(out.Nodes, r.Nodes...)
		out.Edges = append(out.Edges, r.Edges...)
		for _, n := range r.Nodes {
			if n.Type == types.NodeFunction || n.Type == types.NodeMethod || n.Type == types.NodeClass {
				byName[n.Name] = append(byName[n.Name], n.ID)
			}
		}
	}
	for _, r := range results {
		for _, pr := range r.Pending {
			ids := byName[pr.TargetQName]
			if len(ids) == 0 {
				continue
			}
			out.Edges = append(out.Edges, types.Edge{
				Src: pr.SrcID, Dst: ids[0], Type: pr.EdgeType,
				Line: pr.Line, Count: 1, Confidence: types.ConfInferred,
			})
		}
	}
	return out, nil
}
