package golang

import (
	"fmt"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Resolve unions per-file results and uses go/types to resolve PendingRefs.
// V0 implementation: resolves call-target qnames to existing function/method
// nodes by qname suffix match. Unresolved pending refs are dropped (V0
// simplification — emitting AMBIGUOUS edges would violate the schema's
// foreign-key constraint on edges.dst, so full AMBIGUOUS handling is
// deferred until edge persistence supports nullable dst).
func (p *Parser) Resolve(results []*parse.ParseResult) (*parse.ResolvedGraph, error) {
	out := &parse.ResolvedGraph{}
	qIndex := map[string]string{} // qname -> nodeID (Function/Method only)
	// callSiteParent maps a CallSite node ID to its enclosing Function/Method ID,
	// so pending refs originating from a CallSite can be lifted to the function
	// that actually performs the call (spec §4.6.1 — edges are between named
	// entities, CallSite nodes model the syntactic site).
	callSiteParent := map[string]string{}
	for _, r := range results {
		out.Nodes = append(out.Nodes, r.Nodes...)
		out.Edges = append(out.Edges, r.Edges...)
		for _, n := range r.Nodes {
			if n.Type == types.NodeFunction || n.Type == types.NodeMethod {
				qIndex[n.QualifiedName] = n.ID
				// also index trailing simple name and pkg.Name for partial matches
				suffix := simpleName(n.QualifiedName)
				qIndex[suffix] = n.ID
			}
		}
		// Second pass over this file's nodes: once qIndex is populated for
		// this file's functions, derive CallSite → parent function mapping
		// from the CallSite qname prefix (declarations.go/statements.go
		// encode it as "<parentQname>#<Kind>@<offset>").
		for _, n := range r.Nodes {
			if n.Type != types.NodeCallSite {
				continue
			}
			hashIdx := strings.Index(n.QualifiedName, "#")
			if hashIdx <= 0 {
				continue
			}
			parentQ := n.QualifiedName[:hashIdx]
			if parentID, ok := qIndex[parentQ]; ok {
				callSiteParent[n.ID] = parentID
			}
		}
	}
	for _, r := range results {
		for _, pr := range r.Pending {
			id, ok := qIndex[pr.TargetQName]
			conf := types.ConfExtracted
			if !ok {
				// try suffix match
				for q, nid := range qIndex {
					if strings.HasSuffix(q, "."+pr.TargetQName) || q == pr.TargetQName {
						id, ok = nid, true
						break
					}
				}
			}
			if !ok {
				continue // V0: drop unresolved edges to avoid foreign-key violations.
			}
			src := pr.SrcID
			if parentID, ok := callSiteParent[src]; ok {
				src = parentID
			}
			out.Edges = append(out.Edges, types.Edge{
				Src: src, Dst: id, Type: pr.EdgeType,
				Line: pr.Line, Count: 1, Confidence: conf,
			})
		}
	}
	return out, nil
}

// LoadAndResolve is a convenience for tests: walks Go files under root,
// runs Pass 1 on each, then Pass 2 across the union.
func LoadAndResolve(root string) (*parse.ResolvedGraph, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps |
			packages.NeedImports | packages.NeedModule,
		Dir: root,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("packages.Load: %w", err)
	}
	p := New(root)
	var results []*parse.ParseResult
	for _, pkg := range pkgs {
		for _, path := range pkg.GoFiles {
			src, err := readFile(path)
			if err != nil {
				return nil, err
			}
			r, err := p.ParseFile(path, src)
			if err != nil {
				return nil, err
			}
			results = append(results, r)
		}
	}
	return p.Resolve(results)
}

func simpleName(qname string) string {
	i := strings.LastIndex(qname, ".")
	if i < 0 {
		return qname
	}
	return qname[i+1:]
}

func readFile(path string) ([]byte, error) {
	// indirection for testability
	return readFileOS(path)
}
