package golang

import (
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	gotypes "go/types"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// declVisitor walks the AST and emits Pass 1 nodes and edges.
//
// typesInfo, when non-nil, enables go/types-aware extraction (used by the
// concurrency pass to resolve sync.Mutex receivers via *types.Object
// identity rather than name matching). Stays nil when the parser was
// invoked in AST-only mode (no SetPackages call) — callers must check
// before dereferencing.
type declVisitor struct {
	fset      *token.FileSet
	relPath   string
	pkgName   string
	pkgID     string
	fileID    string
	nodes     []types.Node
	edges     []types.Edge
	pending   []parse.PendingRef
	typesInfo *gotypes.Info
	// mutexNodeIDs maps a *types.Object (the var/field declaration of a
	// sync.Mutex/RWMutex) to the Mutex node ID emitted by emitConcurrencyDecls.
	// Populated during decl walk; consumed by Lock/Unlock detection so
	// acquires_lock edges resolve to the same Mutex node the field declared.
	mutexNodeIDs map[gotypes.Object]string
	// fieldNodeIDs maps a *types.Object (a struct Field declaration) to its
	// NodeField ID. Populated during emitFields when typesInfo is available.
	// Consumed by the accessed_under_lock pass (B1 Phase 4 / G8) to translate
	// `x.field` references inside locked functions into edges anchored at the
	// owning Field node.
	fieldNodeIDs map[gotypes.Object]string
	// endpointNodeIDs maps an Endpoint qname (e.g. "http:GET /users" or
	// "http:* /users") to its node ID, deduping repeat HandleFunc calls on
	// the same (method, route) pair within a file. E3 (G5 Distributed).
	// Schema 1.9 §6.2 — cross-language qname format shared with the TS parser.
	endpointNodeIDs map[string]string
	// messageNodeIDs maps a MessageType qname (e.g. "pkg.Args" or
	// "rpc:Service.Method") to its node ID, deduping handles_message and
	// rpc_calls targets that resolve to the same logical message. E3.
	messageNodeIDs map[string]string
	// httpClientPlaceholderIDs maps an HTTP-client-call target qname
	// ("http:METHOD /path") to the AMBIGUOUS placeholder Endpoint ID emitted
	// in upsertHTTPClientPlaceholder. Distinct from endpointNodeIDs because
	// placeholder Endpoints use Language="external" — they live in a separate
	// ID space until the link pass (internal/link/http_match.go) either
	// rewires the http_calls edge to a real Endpoint or leaves the placeholder
	// in place as an external-API marker (W2, schema 1.9 §6.3 (B), §6.9).
	httpClientPlaceholderIDs map[string]string
	// grpcServerImpls tracks which (file, service) pairs have already had
	// grpc_listens_on edges emitted, so duplicate `pb.RegisterXXXServer(s, ...)`
	// calls for the same service in one file don't multiply the edge count.
	// Key = "<relPath>::<service>". W3b (schema 1.9, CKS G5 Distributed).
	grpcServerImpls map[string]struct{}
	// grpcClientStubs maps a local variable name (within the current function
	// scope) to the gRPC service name extracted from its
	// `pb.NewXXXClient(...)` RHS assignment. Populated by
	// scanFuncBodyForGRPCStubs (which runs before the CallExpr walk for the
	// same body) and consumed by maybeEmitGRPCClientCall when typesInfo
	// cannot resolve the stub's receiver type directly. Re-initialized per
	// function scope. W3b (schema 1.9).
	grpcClientStubs map[string]string
	// grpcClientPlaceholderIDs maps a gRPC-client-call target qname
	// ("grpc:Service.Method") to the AMBIGUOUS placeholder Endpoint ID
	// emitted in upsertGRPCClientPlaceholder. Mirrors httpClientPlaceholderIDs
	// — language="external" placeholder Endpoints live in a separate ID
	// space from real `language="go"` Endpoints minted by the server-side
	// RegisterXXXServer pass. W3b (schema 1.9).
	grpcClientPlaceholderIDs map[string]string
	// chanVarIDs maps a channel variable name (within the current function scope)
	// to the Channel node ID emitted by emitChannelFromMake. Used to wire
	// sends_to/recvs_from edges to the actual Channel node instead of an
	// anonymous CallSite. Key = variable name string (AST-level, not qualified).
	// Re-initialized per function scope in emitFunctionBodyPos.
	chanVarIDs map[string]string
	// funcFieldTouches records, for each Function/Method node ID emitted by
	// visitFuncDecl, the set of struct-Field node IDs whose values are
	// read or written anywhere inside the body. Populated even when the
	// function holds NO lock — buildpipe's cross-function lock propagation
	// pass (W-A, opt-in via --lock-propagation) walks the call graph from
	// lock-holding callers into their callees and uses this map to discover
	// "what fields does this callee touch?" without re-parsing.
	//
	// Stored as a set (map[string]struct{}) per func so DFS dedup is O(1)
	// and accidental double-emit from repeated field references inside the
	// same body (e.g. `x.f++; x.f++`) collapses to a single entry.
	//
	// Nil-safe: callers must check map presence before iteration. Only
	// populated when typesInfo != nil — AST-only mode has no reliable
	// way to distinguish field references from method receivers.
	funcFieldTouches map[string]map[string]struct{}
}

func newDeclVisitor(fset *token.FileSet, relPath, pkgName string) *declVisitor {
	v := &declVisitor{fset: fset, relPath: relPath, pkgName: pkgName}
	pkgQ := pkgName
	v.pkgID = MakeID(pkgQ, "go", 0)
	v.nodes = append(v.nodes, types.Node{
		ID: v.pkgID, Type: types.NodePackage,
		Name: pkgName, QualifiedName: pkgQ,
		FilePath: relPath, StartLine: 1, EndLine: 1,
		Language: "go", Confidence: types.ConfExtracted,
	})
	fileQ := pkgQ + "/" + relPath
	v.fileID = MakeID(fileQ, "go", 0)
	v.nodes = append(v.nodes, types.Node{
		ID: v.fileID, Type: types.NodeFile,
		Name: relPath, QualifiedName: fileQ,
		FilePath: relPath, StartLine: 1, EndLine: 1,
		Language: "go", Confidence: types.ConfExtracted,
	})
	v.edges = append(v.edges, types.Edge{
		Src: v.pkgID, Dst: v.fileID,
		Type: types.EdgeContains, Count: 1, Confidence: types.ConfExtracted,
	})
	return v
}

func (v *declVisitor) Visit(n ast.Node) ast.Visitor {
	switch d := n.(type) {
	case *ast.GenDecl:
		v.visitGenDecl(d)
	case *ast.FuncDecl:
		v.visitFuncDecl(d)
	}
	return v
}

func (v *declVisitor) pos(p token.Pos) (line, byteOff int) {
	pos := v.fset.Position(p)
	return pos.Line, pos.Offset
}

func (v *declVisitor) visitGenDecl(d *ast.GenDecl) {
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			v.emitTypeSpec(s, d.Doc)
		case *ast.ValueSpec:
			v.emitValueSpec(s, d.Tok)
		case *ast.ImportSpec:
			v.emitImportSpec(s)
		}
	}
}

func (v *declVisitor) emitTypeSpec(s *ast.TypeSpec, doc *ast.CommentGroup) {
	qname := v.pkgName + "." + s.Name.Name
	startLine, startByte := v.pos(s.Pos())
	endLine, endByte := v.pos(s.End())
	id := MakeID(qname, "go", startByte)
	// canonical id for the type itself: <importpath>.<Type> (symbol-identity
	// Phase 1). Empty in AST-only mode. Passed down so fields and interface
	// methods qualify against the type's canonical id, not the short pkg name.
	var cid string
	if v.typesInfo != nil {
		cid = goCanonicalID(v.typesInfo.ObjectOf(s.Name))
	}
	var nodeType types.NodeType
	switch t := s.Type.(type) {
	case *ast.StructType:
		nodeType = types.NodeStruct
		v.appendNode(id, nodeType, s.Name.Name, qname, startLine, endLine, startByte, endByte, exported(s.Name.Name), commentText(doc), "")
		v.setLastCanonicalID(cid)
		for _, f := range t.Fields.List {
			v.emitFields(id, qname, cid, f)
		}
	case *ast.InterfaceType:
		nodeType = types.NodeInterface
		v.appendNode(id, nodeType, s.Name.Name, qname, startLine, endLine, startByte, endByte, exported(s.Name.Name), commentText(doc), "")
		v.setLastCanonicalID(cid)
		for _, f := range t.Methods.List {
			v.emitInterfaceMethod(id, qname, cid, f)
		}
	default:
		nodeType = types.NodeTypeAlias
		v.appendNode(id, nodeType, s.Name.Name, qname, startLine, endLine, startByte, endByte, exported(s.Name.Name), commentText(doc), "")
		v.setLastCanonicalID(cid)
	}
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
	})
}

// setLastCanonicalID assigns cid to the most recently appended node when cid is
// non-empty. Centralises the "set canonical id on the node we just appended"
// pattern used across the decl emitters.
func (v *declVisitor) setLastCanonicalID(cid string) {
	if cid != "" && len(v.nodes) > 0 {
		v.nodes[len(v.nodes)-1].CanonicalID = cid
	}
}

func (v *declVisitor) emitFields(parentID, parentQname, parentCanonical string, f *ast.Field) {
	for _, name := range f.Names {
		qname := parentQname + "." + name.Name
		startLine, startByte := v.pos(f.Pos())
		endLine, endByte := v.pos(f.End())
		id := MakeID(qname, "go", startByte)
		v.appendNode(id, types.NodeField, name.Name, qname,
			startLine, endLine, startByte, endByte,
			exported(name.Name), commentText(f.Doc), "")
		// field canonical id: <importpath>.<Type>.<Field>, derived from the
		// owning type's canonical id (a field *types.Var carries no receiver).
		// Skip the blank identifier `_` (struct padding fields) — see B1.
		if parentCanonical != "" && name.Name != "_" {
			v.setLastCanonicalID(parentCanonical + "." + name.Name)
		}
		v.edges = append(v.edges, types.Edge{
			Src: parentID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
		})
		// G8: index by *types.Object so the accessed_under_lock pass can
		// resolve `recv.field` references back to this NodeField. Empty when
		// typesInfo is nil — that path emits nothing in G8 (avoids false
		// positives on AST-only mode where field receivers are ambiguous).
		if v.typesInfo != nil {
			if obj := v.typesInfo.Defs[name]; obj != nil {
				if v.fieldNodeIDs == nil {
					v.fieldNodeIDs = map[gotypes.Object]string{}
				}
				v.fieldNodeIDs[obj] = id
			}
		}
	}
}

func (v *declVisitor) emitInterfaceMethod(parentID, parentQname, parentCanonical string, f *ast.Field) {
	for _, name := range f.Names {
		qname := parentQname + "." + name.Name
		startLine, startByte := v.pos(f.Pos())
		endLine, endByte := v.pos(f.End())
		id := MakeID(qname, "go", startByte)
		v.appendNode(id, types.NodeMethod, name.Name, qname,
			startLine, endLine, startByte, endByte,
			exported(name.Name), commentText(f.Doc), "")
		// interface-method canonical id: <importpath>.<Interface>.<Method>,
		// distinct from any concrete impl's <importpath>.(*T).<Method>.
		if parentCanonical != "" && name.Name != "_" {
			v.setLastCanonicalID(parentCanonical + "." + name.Name)
		}
		v.edges = append(v.edges, types.Edge{
			Src: parentID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
		})
	}
}

func (v *declVisitor) emitValueSpec(s *ast.ValueSpec, tok token.Token) {
	for _, name := range s.Names {
		qname := v.pkgName + "." + name.Name
		startLine, startByte := v.pos(name.Pos())
		endLine, endByte := v.pos(s.End())
		id := MakeID(qname, "go", startByte)
		nt := types.NodeVariable
		if tok == token.CONST {
			nt = types.NodeConstant
		}
		v.appendNode(id, nt, name.Name, qname, startLine, endLine, startByte, endByte,
			exported(name.Name), commentText(s.Doc), "")
		// canonical id only for PACKAGE-LEVEL const/var (B2): the AST walk also
		// reaches `var x = …` declared inside function bodies, whose id
		// <importpath>.<name> is neither unique (many funcs declare `gspec`,
		// `err`, …) nor a useful retrieval target. go/types tells them apart:
		// a package-level object's parent scope is the package scope.
		if v.typesInfo != nil {
			if obj := v.typesInfo.ObjectOf(name); obj != nil && obj.Pkg() != nil &&
				obj.Parent() == obj.Pkg().Scope() {
				v.setLastCanonicalID(goCanonicalID(obj))
			}
		}
		v.edges = append(v.edges, types.Edge{
			Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
		})
	}
}

func (v *declVisitor) emitImportSpec(s *ast.ImportSpec) {
	pathLit := strings.Trim(s.Path.Value, "\"")
	qname := "import:" + pathLit
	startLine, startByte := v.pos(s.Pos())
	endLine, endByte := v.pos(s.End())
	id := MakeID(qname, "go", startByte)
	v.appendNode(id, types.NodeImport, pathLit, qname,
		startLine, endLine, startByte, endByte, "", "", "")
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeImports, Count: 1, Confidence: types.ConfExtracted,
	})
}

func (v *declVisitor) visitFuncDecl(d *ast.FuncDecl) {
	var qname string
	var nt types.NodeType
	if d.Recv != nil && len(d.Recv.List) > 0 {
		recvType := exprName(d.Recv.List[0].Type)
		qname = v.pkgName + "." + recvType + "." + d.Name.Name
		nt = types.NodeMethod
	} else {
		qname = v.pkgName + "." + d.Name.Name
		nt = types.NodeFunction
	}
	startLine, startByte := v.pos(d.Pos())
	endLine, endByte := v.pos(d.End())
	id := MakeID(qname, "go", startByte)
	sig := v.formatSignature(d)
	v.appendNode(id, nt, d.Name.Name, qname, startLine, endLine, startByte, endByte,
		exported(d.Name.Name), commentText(d.Doc), sig)
	if v.typesInfo != nil {
		v.setLastCanonicalID(goCanonicalID(v.typesInfo.ObjectOf(d.Name)))
	}
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
	})
	v.emitFunctionBodyPos(qname, id, d.Body)
	// G8 (B1 Phase 4): emit accessed_under_lock(field, mutex) for fields
	// referenced inside a function that holds at least one lock. No-op when
	// typesInfo is nil or the body holds no lock — keeps AST-only mode safe.
	v.emitAccessedUnderLock(id, d.Body)
	// W-A (D1 Stage 2, opt-in via --lock-propagation): record funcID → fields
	// touched for ALL functions (lock-holding or not). buildpipe's
	// propagateLockedFieldAccessDFS reads this map after Pass 2 to walk the
	// call graph and emit accessed_under_lock for callee field accesses under
	// caller-held locks. No-op when typesInfo is nil — declarations.go's
	// fieldNodeIDs is empty in AST-only mode and collectFieldAccesses would
	// return an empty set anyway.
	v.recordFuncFieldTouches(id, d.Body)
}

// helpers

func (v *declVisitor) appendNode(id string, t types.NodeType, name, qname string,
	startLine, endLine, startByte, endByte int, vis, doc, sig string) {
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: t, Name: name, QualifiedName: qname,
		FilePath: v.relPath, StartLine: startLine, EndLine: endLine,
		StartByte: startByte, EndByte: endByte,
		Language: "go", Visibility: vis, DocComment: doc, Signature: sig,
		Confidence: types.ConfExtracted,
	})
}

func exported(name string) string {
	if name == "" {
		return "private"
	}
	if name[0] >= 'A' && name[0] <= 'Z' {
		return "exported"
	}
	return "private"
}

func commentText(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}
	return strings.TrimSpace(g.Text())
}

// goCanonicalID builds the globally-unique import-path-qualified identity of a
// declared symbol from its go/types object, per docs/symbol-identity-design.md:
//
//	function: <importpath>.<Func>
//	method:   <importpath>.(<*?RecvType>).<Method>   (pointer star preserved)
//	type/const/var: <importpath>.<Name>
//
// Returns "" when the object has no package (builtins) so callers leave the
// node's CanonicalID empty rather than emitting an ambiguous id.
func goCanonicalID(obj gotypes.Object) string {
	if obj == nil || obj.Pkg() == nil {
		return ""
	}
	// The blank identifier `_` is not an addressable symbol — many package-level
	// `var _ = ...` / `_ "import"` declarations share the name, so an id like
	// `<pkg>._` is intentionally non-unique. Leave it empty (B1).
	if obj.Name() == "_" {
		return ""
	}
	pkgPath := obj.Pkg().Path()
	if fn, ok := obj.(*gotypes.Func); ok {
		if sig, ok := fn.Type().(*gotypes.Signature); ok && sig.Recv() != nil {
			rt := sig.Recv().Type()
			star := ""
			if ptr, ok := rt.(*gotypes.Pointer); ok {
				rt = ptr.Elem()
				star = "*"
			}
			if named, ok := rt.(*gotypes.Named); ok && named.Obj() != nil {
				return pkgPath + ".(" + star + named.Obj().Name() + ")." + fn.Name()
			}
		}
		return pkgPath + "." + fn.Name()
	}
	return pkgPath + "." + obj.Name()
}

func exprName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return exprName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

// formatSignature renders a one-line Go signature with the real parameter
// and result lists, e.g. "func (Core) handle(x int) (bool, error)". The
// parameter/result lists are printed from the AST via go/printer (accurate
// for variadics, multi-name fields, qualified types) and then whitespace-
// collapsed so a multi-line source signature still yields a single line.
//
// The signature is the highest-signal per-symbol field a retrieval consumer
// reads to answer "what does this check"; the historical
// "func name(...) ..." placeholder hid every parameter (e.g. isJustified's
// targetView), which defeated that purpose.
func (v *declVisitor) formatSignature(d *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")
	if d.Recv != nil && len(d.Recv.List) > 0 {
		_, _ = fmt.Fprintf(&b, "(%s) ", exprName(d.Recv.List[0].Type))
	}
	b.WriteString(d.Name.Name)

	var ft strings.Builder
	if err := printer.Fprint(&ft, v.fset, d.Type); err != nil {
		// Fall back to the old placeholder if the type fails to print
		// (should not happen for a well-formed FuncDecl).
		b.WriteString("(...)")
		if d.Type.Results != nil {
			b.WriteString(" ...")
		}
		return strings.Join(strings.Fields(b.String()), " ")
	}
	// printer renders the FuncType as "func(params) results" — drop the
	// leading "func" so it joins onto the receiver+name we already wrote.
	b.WriteString(strings.TrimPrefix(ft.String(), "func"))

	// Collapse any newlines/tabs/runs of spaces (multi-line source
	// signatures, gofmt alignment) into single spaces, then trim the
	// artifacts a multi-line field list leaves behind: a space just inside
	// the parens and the trailing comma the printer keeps before a
	// newline-preceded close paren.
	s := strings.Join(strings.Fields(b.String()), " ")
	s = strings.ReplaceAll(s, "( ", "(")
	s = strings.ReplaceAll(s, " )", ")")
	s = strings.ReplaceAll(s, ",)", ")")
	return s
}
