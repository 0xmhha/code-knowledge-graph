package golang

import (
	"go/ast"
	gotypes "go/types"

	"golang.org/x/tools/go/packages"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// instantiates.go (Track C P1c): emits `instantiates` edges from a
// Function/Method to a Type when the function body materialises a value of
// that type via:
//
//	Type{...}        — composite literal (struct / array / map literal whose
//	                   Type is a *ast.Ident or *ast.SelectorExpr resolving to
//	                   a named type)
//	&Type{...}       — same, addressed
//	new(Type)        — new builtin
//
// `make(...)` is intentionally NOT emitted in this pass:
//   - make(map[K]V) and make([]T) target unnamed slice/map types — there is
//     no Type node to point to, and the element type is already covered by
//     uses_type.
//   - make(chan T) is already represented by Channel nodes (concurrency.go).
//
// Like uses_type, this is a post-Resolve pass — same reasons (cross-package
// types resolve only against the union of package scopes). Cross-package
// instantiations whose target type isn't in the node graph are silently
// dropped; the resolution drift they'd produce is far smaller than uses_type
// (most struct literals point to types declared in the same package) so the
// pending_refs path isn't worth the complexity for this edge.

// EmitInstantiatesEdges scans every loaded package's function bodies for
// composite literals and new() calls, emitting instantiates edges from the
// enclosing Function/Method to the named target type.
func EmitInstantiatesEdges(pkgs []*packages.Package, nodes []types.Node) []types.Edge {
	if len(pkgs) == 0 {
		return nil
	}
	qnameToTypeID := make(map[string]string, len(nodes))
	for _, n := range nodes {
		switch n.Type {
		case types.NodeStruct, types.NodeInterface, types.NodeTypeAlias, types.NodeEnum:
			if _, exists := qnameToTypeID[n.QualifiedName]; !exists {
				qnameToTypeID[n.QualifiedName] = n.ID
			}
		}
	}
	qnameToFuncID := make(map[string]string, len(nodes))
	for _, n := range nodes {
		switch n.Type {
		case types.NodeFunction, types.NodeMethod:
			if _, exists := qnameToFuncID[n.QualifiedName]; !exists {
				qnameToFuncID[n.QualifiedName] = n.ID
			}
		}
	}

	emitted := make(map[edgeDedupKey]struct{}, 128)
	var edges []types.Edge

	for _, pkg := range pkgs {
		if pkg == nil || pkg.Types == nil || pkg.TypesInfo == nil {
			continue
		}
		pkgName := pkg.Types.Name()
		for _, f := range pkg.Syntax {
			if f == nil {
				continue
			}
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				var srcQname string
				if fd.Recv != nil && len(fd.Recv.List) > 0 {
					srcQname = pkgName + "." + exprName(fd.Recv.List[0].Type) + "." + fd.Name.Name
				} else {
					srcQname = pkgName + "." + fd.Name.Name
				}
				srcID, ok := qnameToFuncID[srcQname]
				if !ok {
					continue
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					switch x := n.(type) {
					case *ast.CompositeLit:
						emitInstantiatesForType(srcID, srcQname, x.Type, pkg.TypesInfo,
							qnameToTypeID, emitted, &edges)
					case *ast.CallExpr:
						// new(T)
						if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "new" && len(x.Args) == 1 {
							emitInstantiatesForType(srcID, srcQname, x.Args[0], pkg.TypesInfo,
								qnameToTypeID, emitted, &edges)
						}
					}
					return true
				})
			}
		}
	}
	return edges
}

// emitInstantiatesForType resolves a type-expression (composite literal's
// Type field or new()'s argument) to a *types.Named and emits one edge per
// distinct target. Anonymous / builtin types are skipped.
func emitInstantiatesForType(
	srcID, srcQname string, expr ast.Expr, info *gotypes.Info,
	qnameToTypeID map[string]string, emitted map[edgeDedupKey]struct{},
	out *[]types.Edge,
) {
	if expr == nil || info == nil {
		return
	}
	t := info.TypeOf(expr)
	if t == nil {
		return
	}
	// Strip pointer (for `&Type{}` cases the composite's Type is the bare
	// element; new(*T) is unusual but the caller's Args[0] is already the
	// bare T expression).
	if ptr, ok := t.(*gotypes.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*gotypes.Named)
	if !ok {
		return
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return
	}
	dstQname := obj.Pkg().Name() + "." + obj.Name()
	if dstQname == srcQname {
		return
	}
	dstID, ok := qnameToTypeID[dstQname]
	if !ok {
		return // cross-package type without a node in this graph
	}
	k := edgeDedupKey{src: srcID, dst: dstID}
	if _, dup := emitted[k]; dup {
		return
	}
	emitted[k] = struct{}{}
	*out = append(*out, types.Edge{
		Src: srcID, Dst: dstID, Type: types.EdgeInstantiates,
		Count: 1, Confidence: types.ConfExtracted,
	})
}
