package golang

import (
	"go/ast"
	"go/token"
	gotypes "go/types"

	"golang.org/x/tools/go/packages"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// field_access.go (defect E): emits `writes_field` edges from a
// Function/Method to a struct Field node when the function body assigns to
// that field — `x.F = v`, `x.F op= v`, or `x.F++` / `x.F--`.
//
// Why a post-Resolve pass (like uses_type / instantiates): the writer and the
// field declaration frequently live in different files or packages (e.g.
// core.applyTransaction writes core/types.Receipt.EffectiveGasPrice), so the
// edge can only be resolved against the union of package scopes + the union of
// emitted Field nodes. go/types' Selections map classifies each selector as a
// field access and yields the field's owning type, which we map to the Field
// node's qname "pkgleaf.Type.Field".
//
// Scope (V0): WRITES only. reads_field is intentionally not emitted — a read
// edge per `x.F` use would explode the edge count for little marginal value;
// "who mutates this field" is the high-signal half (it answers the data-flow
// "which writers feed field X" question). Promoted-field writes resolve to the
// embedding type's qname (from Selection.Recv()) which has no Field node, so
// they are skipped rather than mis-attributed.
//
// Returns nil when pkgs is empty.
func EmitFieldWriteEdges(pkgs []*packages.Package, nodes []types.Node) []types.Edge {
	if len(pkgs) == 0 {
		return nil
	}
	qnameToFieldID := make(map[string]string, len(nodes))
	qnameToFuncID := make(map[string]string, len(nodes))
	for _, n := range nodes {
		switch n.Type {
		case types.NodeField:
			if _, ok := qnameToFieldID[n.QualifiedName]; !ok {
				qnameToFieldID[n.QualifiedName] = n.ID
			}
		case types.NodeFunction, types.NodeMethod:
			if _, ok := qnameToFuncID[n.QualifiedName]; !ok {
				qnameToFuncID[n.QualifiedName] = n.ID
			}
		}
	}

	emitted := make(map[edgeDedupKey]struct{}, 128)
	var edges []types.Edge

	emit := func(srcID string, sel *ast.SelectorExpr, info *gotypes.Info) {
		fieldID := fieldNodeID(sel, info, qnameToFieldID)
		if fieldID == "" {
			return
		}
		k := edgeDedupKey{src: srcID, dst: fieldID}
		if _, dup := emitted[k]; dup {
			return
		}
		emitted[k] = struct{}{}
		edges = append(edges, types.Edge{
			Src: srcID, Dst: fieldID, Type: types.EdgeWritesField,
			Count: 1, Confidence: types.ConfExtracted,
		})
	}

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
					case *ast.AssignStmt:
						// `=` and op-assigns (`+=` etc.) write their LHS;
						// `:=` (DEFINE) introduces new idents, never a field.
						if x.Tok == token.DEFINE {
							return true
						}
						for _, lhs := range x.Lhs {
							if sel, ok := lhs.(*ast.SelectorExpr); ok {
								emit(srcID, sel, pkg.TypesInfo)
							}
						}
					case *ast.IncDecStmt:
						if sel, ok := x.X.(*ast.SelectorExpr); ok {
							emit(srcID, sel, pkg.TypesInfo)
						}
					}
					return true
				})
			}
		}
	}
	return edges
}

// fieldNodeID resolves a selector expression to the ID of the Field node it
// writes, or "" when sel is not a field selection, the owning type isn't an
// in-module named struct, or no Field node exists for that qname.
func fieldNodeID(sel *ast.SelectorExpr, info *gotypes.Info, qnameToFieldID map[string]string) string {
	if info == nil {
		return ""
	}
	s := info.Selections[sel]
	if s == nil || s.Kind() != gotypes.FieldVal {
		return ""
	}
	recv := s.Recv()
	if ptr, ok := recv.(*gotypes.Pointer); ok {
		recv = ptr.Elem()
	}
	named, ok := recv.(*gotypes.Named)
	if !ok || named.Obj().Pkg() == nil {
		return ""
	}
	qname := named.Obj().Pkg().Name() + "." + named.Obj().Name() + "." + s.Obj().Name()
	return qnameToFieldID[qname]
}
