package typescript

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// declVisitor walks tree-sitter query matches and emits Pass 1 nodes/edges.
type declVisitor struct {
	rel     string
	src     []byte
	lang    *sitter.Language
	root    *sitter.Node
	fileID  string
	nodes   []types.Node
	edges   []types.Edge
	pending []parse.PendingRef
}

func newDeclVisitor(rel string, src []byte, lang *sitter.Language, root *sitter.Node) *declVisitor {
	v := &declVisitor{rel: rel, src: src, lang: lang, root: root}
	fileQ := "file:" + rel
	v.fileID = makeID(fileQ, "ts", 0)
	v.nodes = append(v.nodes, types.Node{
		ID: v.fileID, Type: types.NodeFile, Name: rel, QualifiedName: fileQ,
		FilePath: rel, StartLine: 1, EndLine: 1,
		Language: "ts", Confidence: types.ConfExtracted,
	})
	return v
}

func (v *declVisitor) visit() {
	v.runQuery(queryClass, types.NodeClass)
	v.runQuery(queryInterface, types.NodeInterface)
	v.runQuery(queryFunction, types.NodeFunction)
	v.runQuery(queryMethod, types.NodeMethod)
	v.runQuery(queryTypeAlias, types.NodeTypeAlias)
	v.runQuery(queryEnum, types.NodeEnum)
	v.runQuery(queryDecorator, types.NodeDecorator)
	v.runImports()
}

func (v *declVisitor) runQuery(q string, nt types.NodeType) {
	query, err := sitter.NewQuery([]byte(q), v.lang)
	if err != nil {
		return
	}
	cur := sitter.NewQueryCursor()
	cur.Exec(query, v.root)
	for {
		m, ok := cur.NextMatch()
		if !ok {
			break
		}
		for _, c := range m.Captures {
			name := query.CaptureNameForId(c.Index)
			if name != "name" {
				continue
			}
			ident := c.Node.Content(v.src)
			startByte := int(c.Node.StartByte())
			endByte := int(c.Node.EndByte())
			startLine := int(c.Node.StartPoint().Row) + 1
			endLine := int(c.Node.EndPoint().Row) + 1
			qname := ident
			if nt == types.NodeMethod {
				if className := nearestClassName(c.Node, v.src); className != "" {
					qname = className + "." + ident
				}
			}
			id := makeID(qname, "ts", startByte)
			v.nodes = append(v.nodes, types.Node{
				ID: id, Type: nt, Name: ident, QualifiedName: qname,
				FilePath: v.rel, StartLine: startLine, EndLine: endLine,
				StartByte: startByte, EndByte: endByte,
				Language: "ts", Confidence: types.ConfExtracted,
			})
			v.edges = append(v.edges, types.Edge{
				Src: v.fileID, Dst: id, Type: types.EdgeDefines,
				Count: 1, Confidence: types.ConfExtracted,
			})
		}
	}
}

func (v *declVisitor) runImports() {
	query, err := sitter.NewQuery([]byte(queryImport), v.lang)
	if err != nil {
		return
	}
	cur := sitter.NewQueryCursor()
	cur.Exec(query, v.root)
	for {
		m, ok := cur.NextMatch()
		if !ok {
			break
		}
		for _, c := range m.Captures {
			if query.CaptureNameForId(c.Index) != "path" {
				continue
			}
			path := trimQuotes(c.Node.Content(v.src))
			qname := "import:" + path
			startByte := int(c.Node.StartByte())
			endByte := int(c.Node.EndByte())
			id := makeID(qname, "ts", startByte)
			v.nodes = append(v.nodes, types.Node{
				ID: id, Type: types.NodeImport, Name: path, QualifiedName: qname,
				FilePath: v.rel, StartLine: int(c.Node.StartPoint().Row) + 1,
				EndLine:   int(c.Node.EndPoint().Row) + 1,
				StartByte: startByte, EndByte: endByte,
				Language: "ts", Confidence: types.ConfExtracted,
			})
			v.edges = append(v.edges, types.Edge{
				Src: v.fileID, Dst: id, Type: types.EdgeImports,
				Count: 1, Confidence: types.ConfExtracted,
			})
		}
	}
}

// nearestClassName walks the parent chain looking for an enclosing
// class_declaration and returns its name (empty if none).
func nearestClassName(n *sitter.Node, src []byte) string {
	for cur := n; cur != nil; cur = cur.Parent() {
		if cur.Type() == "class_declaration" {
			id := cur.ChildByFieldName("name")
			if id != nil {
				return id.Content(src)
			}
		}
	}
	return ""
}

func trimQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'' || s[0] == '`') {
		return s[1 : len(s)-1]
	}
	return s
}

// makeID is a thin wrapper over the shared parse.MakeID, kept local for
// ergonomic call sites within this package.
func makeID(qname, lang string, startByte int) string {
	return parse.MakeID(qname, lang, startByte)
}
