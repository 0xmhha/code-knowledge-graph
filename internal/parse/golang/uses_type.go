package golang

import (
	"go/ast"
	gotypes "go/types"

	"golang.org/x/tools/go/packages"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// uses_type.go (Track C P0): emits `uses_type` edges from a Function /
// Method / Struct field declaration to the named Type it references.
//
// Granularity (Track C open question q1=B):
//   - Function arg type    : each parameter's named type → Function uses_type Type
//   - Function return type : each result's named type   → Function uses_type Type
//   - Struct field type    : every field's named type   → Struct   uses_type Type
//   - Var declaration type : (skipped in this initial pass — captured by the
//                             field/param paths above for the common cases)
//
// Why a post-Resolve pass (mirroring EmitImplementsEdges):
//   - Cross-package satisfaction needs the union of every package's Scope to
//     resolve a *gotypes.Named to its declaring package's qname.
//   - Per-file Pass 1 doesn't have the typed package handle for stdlib /
//     foreign-package types — those types resolve only against the post-load
//     packages slice.
//   - Running here lets us emit a `pending_refs` row for unresolved cross-package
//     references (Track C open question q4=A) so the partial-cache rebuild
//     replays the same input set the cold path saw.
//
// Skip rules (avoids edge-explosion / pure noise):
//   - Builtin primitives (int, string, error, etc.) — not nodes in the graph.
//   - Anonymous types (struct{...}, interface{...}, slice/map literals) —
//     no qname → no node.
//   - Self-references (Function uses_type its own receiver Type, etc.).

// EmitUsesTypeEdges scans every loaded package's top-level declarations and
// emits `uses_type` edges from Function/Method/Struct nodes to the named
// types they reference (params, results, fields).
//
// Returns:
//   - edges: the resolved uses_type edges
//   - pending: cross-package PendingRefs for types not present in the node
//     index (q4=A — pending_refs row so the next partial build replays the
//     same input set).
//
// Idempotent — safe to call multiple times against the same nodes/pkgs.
func EmitUsesTypeEdges(pkgs []*packages.Package, nodes []types.Node) ([]types.Edge, []parse.PendingRef) {
	if len(pkgs) == 0 {
		return nil, nil
	}

	// Build qname → emitted node ID map for every type that can legitimately
	// appear as a uses_type DST. We index Struct / Interface / TypeAlias /
	// Enum nodes (the same set EmitImplementsEdges uses) so post-pass
	// resolution matches.
	qnameToTypeID := make(map[string]string, len(nodes))
	for _, n := range nodes {
		switch n.Type {
		case types.NodeStruct, types.NodeInterface, types.NodeTypeAlias, types.NodeEnum:
			if _, exists := qnameToTypeID[n.QualifiedName]; !exists {
				qnameToTypeID[n.QualifiedName] = n.ID
			}
		}
	}

	// Build qname → emitted node ID map for symbols that can be a uses_type
	// SRC: Function, Method (callers using it as param/return), Struct
	// (caller using it as field type's container).
	qnameToSrcID := make(map[string]string, len(nodes))
	for _, n := range nodes {
		switch n.Type {
		case types.NodeFunction, types.NodeMethod, types.NodeStruct:
			if _, exists := qnameToSrcID[n.QualifiedName]; !exists {
				qnameToSrcID[n.QualifiedName] = n.ID
			}
		}
	}

	emitted := make(map[edgeDedupKey]struct{}, len(qnameToSrcID)*2)
	var edges []types.Edge
	var pending []parse.PendingRef

	addEdge := func(srcID, dstQname string, line int, hintFile string) {
		if srcID == "" || dstQname == "" {
			return
		}
		if dstID, ok := qnameToTypeID[dstQname]; ok {
			k := edgeDedupKey{src: srcID, dst: dstID}
			if _, dup := emitted[k]; dup {
				return
			}
			emitted[k] = struct{}{}
			edges = append(edges, types.Edge{
				Src: srcID, Dst: dstID, Type: types.EdgeUsesType,
				Count: 1, Confidence: types.ConfExtracted,
				Line: line, FilePath: hintFile,
			})
			return
		}
		// Unresolved cross-package type → q4=A: emit pending_ref so the
		// incremental partial-cache rebuild can replay this resolution
		// against a future build's full type universe.
		pending = append(pending, parse.PendingRef{
			SrcID:       srcID,
			EdgeType:    types.EdgeUsesType,
			TargetQName: dstQname,
			HintFile:    hintFile,
			Line:        line,
		})
	}

	for _, pkg := range pkgs {
		if pkg == nil || pkg.Types == nil {
			continue
		}
		pkgName := pkg.Types.Name()
		scope := pkg.Types.Scope()
		if scope == nil {
			continue
		}
		// Functions and methods (Function/Method nodes): walk param + result
		// signatures.
		emitUsesTypeForFunctions(pkg, pkgName, qnameToSrcID, addEdge)
		// Struct field types.
		emitUsesTypeForStructs(scope, pkgName, qnameToSrcID, addEdge)
	}

	return edges, pending
}

// emitUsesTypeForFunctions walks every FuncDecl in pkg.Syntax and emits
// edges for parameters / results whose type is a *gotypes.Named.
//
// Why pkg.Syntax (not scope.Names()): scope iterates package-scope objects
// only. Methods are scope-less from the package's point of view (they hang
// off types). FuncDecls in the syntax trees give us the AST positions for
// edge.line and the same qname construction declarations.go uses.
func emitUsesTypeForFunctions(
	pkg *packages.Package, pkgName string,
	qnameToSrcID map[string]string,
	addEdge func(srcID, dstQname string, line int, hintFile string),
) {
	if pkg.TypesInfo == nil || pkg.Fset == nil {
		return
	}
	for fileIdx, f := range pkg.Syntax {
		if f == nil {
			continue
		}
		var hintFile string
		if fileIdx < len(pkg.CompiledGoFiles) {
			hintFile = pkg.CompiledGoFiles[fileIdx]
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Type == nil {
				continue
			}
			// Construct the function's qname (mirrors visitFuncDecl in
			// declarations.go). For methods, the receiver type slot uses
			// exprName which strips pointer/selector.
			var srcQname string
			if fd.Recv != nil && len(fd.Recv.List) > 0 {
				srcQname = pkgName + "." + exprName(fd.Recv.List[0].Type) + "." + fd.Name.Name
			} else {
				srcQname = pkgName + "." + fd.Name.Name
			}
			srcID, ok := qnameToSrcID[srcQname]
			if !ok {
				continue
			}
			line := pkg.Fset.Position(fd.Pos()).Line
			// Parameters
			if fd.Type.Params != nil {
				for _, field := range fd.Type.Params.List {
					forEachNamedTypeInExpr(pkg.TypesInfo, field.Type, func(qname string) {
						if qname != srcQname {
							addEdge(srcID, qname, line, hintFile)
						}
					})
				}
			}
			// Results
			if fd.Type.Results != nil {
				for _, field := range fd.Type.Results.List {
					forEachNamedTypeInExpr(pkg.TypesInfo, field.Type, func(qname string) {
						if qname != srcQname {
							addEdge(srcID, qname, line, hintFile)
						}
					})
				}
			}
		}
	}
}

// emitUsesTypeForStructs walks every Struct type at package scope and
// emits a uses_type edge for every field's named type.
func emitUsesTypeForStructs(
	scope *gotypes.Scope, pkgName string,
	qnameToSrcID map[string]string,
	addEdge func(srcID, dstQname string, line int, hintFile string),
) {
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		tn, ok := obj.(*gotypes.TypeName)
		if !ok {
			continue
		}
		named, ok := tn.Type().(*gotypes.Named)
		if !ok {
			continue
		}
		st, ok := named.Underlying().(*gotypes.Struct)
		if !ok {
			continue
		}
		srcQname := pkgName + "." + tn.Name()
		srcID, ok := qnameToSrcID[srcQname]
		if !ok {
			continue
		}
		// scope.Lookup returns an obj whose Pos is the type-decl token.
		line := 0 // line is populated only for func-anchored edges
		hintFile := ""
		for i := 0; i < st.NumFields(); i++ {
			field := st.Field(i)
			forEachNamedTypeInType(field.Type(), func(qname string) {
				if qname != srcQname {
					addEdge(srcID, qname, line, hintFile)
				}
			})
		}
	}
}

// forEachNamedTypeInExpr walks an *ast.Expr (a parameter / result type
// expression) using types.Info to resolve identifiers / selectors to
// *gotypes.Named, then yields each distinct (pkgName + "." + typeName)
// qname encountered. Pointer / slice / map / chan layers are stripped to
// reach the underlying named element.
func forEachNamedTypeInExpr(info *gotypes.Info, e ast.Expr, yield func(qname string)) {
	if info == nil || e == nil {
		return
	}
	t := info.TypeOf(e)
	if t == nil {
		return
	}
	forEachNamedTypeInType(t, yield)
}

// forEachNamedTypeInType walks a *gotypes.Type to find every named-type
// reference and yields its package-prefixed qname.
//
// Stripping layers:
//
//	*T          → recurse into T
//	[]T, [N]T   → recurse into element
//	map[K]V     → recurse into K and V
//	chan T      → recurse into element
//	struct{...} → recurse into each field
//	(no special case for interface — interface methods aren't "uses_type"
//	 in the Track C scope; they're already covered by `implements`).
//
// Yield is called once per distinct top-level *types.Named encountered.
// Builtin / unnamed types yield nothing.
func forEachNamedTypeInType(t gotypes.Type, yield func(qname string)) {
	if t == nil {
		return
	}
	switch tt := t.(type) {
	case *gotypes.Pointer:
		forEachNamedTypeInType(tt.Elem(), yield)
	case *gotypes.Slice:
		forEachNamedTypeInType(tt.Elem(), yield)
	case *gotypes.Array:
		forEachNamedTypeInType(tt.Elem(), yield)
	case *gotypes.Map:
		forEachNamedTypeInType(tt.Key(), yield)
		forEachNamedTypeInType(tt.Elem(), yield)
	case *gotypes.Chan:
		forEachNamedTypeInType(tt.Elem(), yield)
	case *gotypes.Named:
		obj := tt.Obj()
		if obj == nil || obj.Pkg() == nil {
			return // builtin (error / comparable) — no node
		}
		yield(obj.Pkg().Name() + "." + obj.Name())
	case *gotypes.Struct:
		for i := 0; i < tt.NumFields(); i++ {
			forEachNamedTypeInType(tt.Field(i).Type(), yield)
		}
	}
}

// edgeDedupKey collapses (src, dst) pairs across the uses_type pass so a
// function with two `pkg.T` params produces only one edge.
type edgeDedupKey struct {
	src, dst string
}
