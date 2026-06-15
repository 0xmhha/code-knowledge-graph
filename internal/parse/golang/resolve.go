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
			// E3: listens_on PendingRefs from the distributed pass encode
			// (endpoint, handler-name) — but the conventional edge direction
			// is handler → endpoint. Swap src↔dst so downstream consumers
			// don't have to special-case this edge type. Documented in
			// distributed.go's maybeEmitHTTPListensOn fallback comment.
			finalSrc, finalDst := src, id
			if pr.EdgeType == types.EdgeListensOn {
				finalSrc, finalDst = id, src
			}
			out.Edges = append(out.Edges, types.Edge{
				Src: finalSrc, Dst: finalDst, Type: pr.EdgeType,
				Line: pr.Line, Count: 1, Confidence: conf,
				DispatchKind: pr.DispatchKind,
			})
		}
	}
	return out, nil
}

// LoadAndResolve is a convenience for tests: walks Go files under root,
// runs Pass 1 on each, then Pass 2 across the union.
//
// Type-aware: registers the loaded packages with the parser via
// SetPackages so the concurrency pass (B1) gets EXTRACTED-confidence
// Mutex / lock-edge emission instead of falling back to AST-only INFERRED.
func LoadAndResolve(root string) (*parse.ResolvedGraph, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedDeps |
			packages.NeedImports | packages.NeedModule | packages.NeedCompiledGoFiles,
		Dir: root,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("packages.Load: %w", err)
	}
	p := New(root)
	p.SetPackages(pkgs)
	var results []*parse.ParseResult
	seen := map[string]struct{}{}
	for _, pkg := range pkgs {
		for _, path := range pkg.GoFiles {
			if _, dup := seen[path]; dup {
				continue
			}
			seen[path] = struct{}{}
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
	rg, err := p.Resolve(results)
	if err != nil || rg == nil {
		return rg, err
	}
	// P0: implements / extends edges are a post-Resolve pass — they need
	// (a) the union of nodes (qname → ID lookup) and (b) the loaded package
	// scopes (go/types satisfaction queries). Wired here so tests using
	// LoadAndResolve exercise the same code path as production buildpipe.
	rg.Edges = append(rg.Edges, EmitImplementsEdges(p.Pkgs(), rg.Nodes)...)
	// Track C P0: uses_type edges (Function/Method/Struct → Type). Same
	// post-Resolve location for the same reasons. Cross-package types that
	// don't have a node in this graph become PendingRefs (q4=A); LoadAndResolve
	// drops the pending side because tests don't replay them, but the
	// production buildpipe path persists them via runGoPipeline.
	usesEdges, _ := EmitUsesTypeEdges(p.Pkgs(), rg.Nodes)
	rg.Edges = append(rg.Edges, usesEdges...)
	// Track C P1c: instantiates edges (Function/Method → Type). Walks each
	// function body for composite literals and `new(T)` calls.
	rg.Edges = append(rg.Edges, EmitInstantiatesEdges(p.Pkgs(), rg.Nodes)...)
	// Defect C: promoted-method nodes (embedding type -> embedded method).
	// Runs after the per-file nodes are unioned so the declaring method nodes
	// exist for the in-module bound. Emits method nodes + defines edges.
	promNodes, promEdges := EmitPromotedMethods(p.Pkgs(), rg.Nodes)
	rg.Nodes = append(rg.Nodes, promNodes...)
	rg.Edges = append(rg.Edges, promEdges...)
	// Defect E: writes_field edges (function -> struct field it assigns).
	rg.Edges = append(rg.Edges, EmitFieldWriteEdges(p.Pkgs(), rg.Nodes)...)
	return rg, nil
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
