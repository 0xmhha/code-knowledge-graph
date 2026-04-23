package golang

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// declVisitor walks the AST and emits Pass 1 nodes and edges.
type declVisitor struct {
	fset    *token.FileSet
	relPath string
	pkgName string
	pkgID   string
	fileID  string
	nodes   []types.Node
	edges   []types.Edge
	pending []parse.PendingRef
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
			v.emitFields(qname, f)
		}
	case *ast.InterfaceType:
		nodeType = types.NodeInterface
		v.appendNode(id, nodeType, s.Name.Name, qname, startLine, endLine, startByte, endByte, exported(s.Name.Name), commentText(doc), "")
		for _, f := range t.Methods.List {
			v.emitInterfaceMethod(qname, f)
		}
	default:
		nodeType = types.NodeTypeAlias
		v.appendNode(id, nodeType, s.Name.Name, qname, startLine, endLine, startByte, endByte, exported(s.Name.Name), commentText(doc), "")
	}
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
	})
}

func (v *declVisitor) emitFields(parentQname string, f *ast.Field) {
	for _, name := range f.Names {
		qname := parentQname + "." + name.Name
		startLine, startByte := v.pos(f.Pos())
		endLine, endByte := v.pos(f.End())
		id := MakeID(qname, "go", startByte)
		v.appendNode(id, types.NodeField, name.Name, qname,
			startLine, endLine, startByte, endByte,
			exported(name.Name), commentText(f.Doc), "")
		parentID := MakeID(parentQname, "go", v.lookupStartByte(parentQname))
		v.edges = append(v.edges, types.Edge{
			Src: parentID, Dst: id, Type: types.EdgeDefines, Count: 1, Confidence: types.ConfExtracted,
		})
	}
}

func (v *declVisitor) emitInterfaceMethod(parentQname string, f *ast.Field) {
	for _, name := range f.Names {
		qname := parentQname + "." + name.Name
		startLine, startByte := v.pos(f.Pos())
		endLine, endByte := v.pos(f.End())
		id := MakeID(qname, "go", startByte)
		v.appendNode(id, types.NodeMethod, name.Name, qname,
			startLine, endLine, startByte, endByte,
			exported(name.Name), commentText(f.Doc), "")
		parentID := MakeID(parentQname, "go", v.lookupStartByte(parentQname))
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

func (v *declVisitor) lookupStartByte(qname string) int {
	for _, n := range v.nodes {
		if n.QualifiedName == qname {
			return n.StartByte
		}
	}
	return 0
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
