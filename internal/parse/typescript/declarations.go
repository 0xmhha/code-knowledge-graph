package typescript

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

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
	// Walk function/method bodies for statement-level nodes (IfStmt,
	// LoopStmt, SwitchStmt, ReturnStmt, CallSite) + emit cross-file
	// PendingRefs anchored on each CallSite. Runs after the declaration
	// queries so the Function/Method byte intervals (collected via
	// collectFnIntervalsFromTree) are populated for the enclosing-fn
	// lookup. Replaces the earlier P3 runBodyCalls — see
	// internal/parse/typescript/statements.go for the full schema.
	v.runBodyStatements()
}

func (v *declVisitor) runQuery(q string, nt types.NodeType) {
	query, qErr := sitter.NewQuery(v.lang, q)
	if qErr != nil {
		return
	}
	defer query.Close()
	cur := sitter.NewQueryCursor()
	defer cur.Close()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		for _, c := range m.Captures {
			if names[c.Index] != "name" {
				continue
			}
			node := c.Node
			ident := node.Utf8Text(v.src)
			startByte := int(node.StartByte())
			endByte := int(node.EndByte())
			startLine := int(node.StartPosition().Row) + 1
			endLine := int(node.EndPosition().Row) + 1
			qname := ident
			if nt == types.NodeMethod {
				if className := nearestClassName(&node, v.src); className != "" {
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
	query, qErr := sitter.NewQuery(v.lang, queryImport)
	if qErr != nil {
		return
	}
	defer query.Close()
	cur := sitter.NewQueryCursor()
	defer cur.Close()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		for _, c := range m.Captures {
			if names[c.Index] != "path" {
				continue
			}
			node := c.Node
			path := trimQuotes(node.Utf8Text(v.src))
			qname := "import:" + path
			startByte := int(node.StartByte())
			endByte := int(node.EndByte())
			id := makeID(qname, "ts", startByte)
			v.nodes = append(v.nodes, types.Node{
				ID: id, Type: types.NodeImport, Name: path, QualifiedName: qname,
				FilePath: v.rel, StartLine: int(node.StartPosition().Row) + 1,
				EndLine:   int(node.EndPosition().Row) + 1,
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
		if cur.Kind() == "class_declaration" {
			id := cur.ChildByFieldName("name")
			if id != nil {
				return id.Utf8Text(src)
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
