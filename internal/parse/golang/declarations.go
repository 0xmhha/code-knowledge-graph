package golang

import (
	"fmt"
	"go/ast"
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
	// chanVarIDs maps a channel variable name (within the current function scope)
	// to the Channel node ID emitted by emitChannelFromMake. Used to wire
	// sends_to/recvs_from edges to the actual Channel node instead of an
	// anonymous CallSite. Key = variable name string (AST-level, not qualified).
	// Re-initialized per function scope in emitFunctionBodyPos.
	chanVarIDs map[string]string
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
	var nodeType types.NodeType
	switch t := s.Type.(type) {
	case *ast.StructType:
		nodeType = types.NodeStruct
		v.appendNode(id, nodeType, s.Name.Name, qname, startLine, endLine, startByte, endByte, exported(s.Name.Name), commentText(doc), "")
		for _, f := range t.Fields.List {
			v.emitFields(id, qname, f)
		}
	case *ast.InterfaceType:
		nodeType = types.NodeInterface
		v.appendNode(id, nodeType, s.Name.Name, qname, startLine, endLine, startByte, endByte, exported(s.Name.Name), commentText(doc), "")
		for _, f := range t.Methods.List {
			v.emitInterfaceMethod(id, qname, f)
		}
	default:
		nodeType = types.NodeTypeAlias
		v.appendNode(id, nodeType, s.Name.Name, qname, startLine, endLine, startByte, endByte, exported(s.Name.Name), commentText(doc), "")
	}
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
	})
}

func (v *declVisitor) emitFields(parentID, parentQname string, f *ast.Field) {
	for _, name := range f.Names {
		qname := parentQname + "." + name.Name
		startLine, startByte := v.pos(f.Pos())
		endLine, endByte := v.pos(f.End())
		id := MakeID(qname, "go", startByte)
		v.appendNode(id, types.NodeField, name.Name, qname,
			startLine, endLine, startByte, endByte,
			exported(name.Name), commentText(f.Doc), "")
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

func (v *declVisitor) emitInterfaceMethod(parentID, parentQname string, f *ast.Field) {
	for _, name := range f.Names {
		qname := parentQname + "." + name.Name
		startLine, startByte := v.pos(f.Pos())
		endLine, endByte := v.pos(f.End())
		id := MakeID(qname, "go", startByte)
		v.appendNode(id, types.NodeMethod, name.Name, qname,
			startLine, endLine, startByte, endByte,
			exported(name.Name), commentText(f.Doc), "")
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
	sig := formatSignature(d)
	v.appendNode(id, nt, d.Name.Name, qname, startLine, endLine, startByte, endByte,
		exported(d.Name.Name), commentText(d.Doc), sig)
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
	})
	v.emitFunctionBodyPos(qname, id, d.Body)
	// G8 (B1 Phase 4): emit accessed_under_lock(field, mutex) for fields
	// referenced inside a function that holds at least one lock. No-op when
	// typesInfo is nil or the body holds no lock — keeps AST-only mode safe.
	v.emitAccessedUnderLock(id, d.Body)
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

func formatSignature(d *ast.FuncDecl) string {
	var b strings.Builder
	b.WriteString("func ")
	if d.Recv != nil && len(d.Recv.List) > 0 {
		fmt.Fprintf(&b, "(%s) ", exprName(d.Recv.List[0].Type))
	}
	b.WriteString(d.Name.Name)
	b.WriteString("(...)")
	if d.Type.Results != nil {
		b.WriteString(" ...")
	}
	return b.String()
}
