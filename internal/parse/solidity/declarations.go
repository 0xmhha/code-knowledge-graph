package solidity

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// declVisitor walks tree-sitter query matches and emits Pass 1 nodes/edges.
// Mirrors the TypeScript declVisitor structure for consistency.
type declVisitor struct {
	rel     string
	src     []byte
	lang    *sitter.Language
	root    *sitter.Node
	fileID  string
	nodes   []types.Node
	edges   []types.Edge
	pending []parse.PendingRef
	abi     map[string][]ABISig
}

func newDeclVisitor(rel string, src []byte, lang *sitter.Language, root *sitter.Node, abi map[string][]ABISig) *declVisitor {
	v := &declVisitor{rel: rel, src: src, lang: lang, root: root, abi: abi}
	fileQ := "file:" + rel
	v.fileID = parse.MakeID(fileQ, "sol", 0)
	v.nodes = append(v.nodes, types.Node{
		ID: v.fileID, Type: types.NodeFile, Name: rel, QualifiedName: fileQ,
		FilePath: rel, StartLine: 1, EndLine: 1,
		Language: "sol", Confidence: types.ConfExtracted,
	})
	return v
}

func (v *declVisitor) visit() {
	v.runDecl(queryContract, types.NodeContract)
	v.runDecl(queryFunction, types.NodeFunction)
	v.runDecl(queryModifier, types.NodeModifier)
	v.runDecl(queryEvent, types.NodeEvent)
	v.runDecl(queryStruct, types.NodeStruct)
	v.runDecl(queryEnum, types.NodeEnum)
	v.runStateVarDecl()
	v.runEmits()
	v.runHasModifier()
	v.collectABI()
}

func (v *declVisitor) runDecl(q string, nt types.NodeType) {
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
			if query.CaptureNameForId(c.Index) != "name" {
				continue
			}
			ident := c.Node.Content(v.src)
			startByte := int(c.Node.StartByte())
			endByte := int(c.Node.EndByte())
			qname := ident
			if nt == types.NodeFunction {
				if cn := nearestContractName(c.Node, v.src); cn != "" {
					qname = cn + "." + ident
				}
			}
			id := parse.MakeID(qname, "sol", startByte)
			v.nodes = append(v.nodes, types.Node{
				ID: id, Type: nt, Name: ident, QualifiedName: qname,
				FilePath: v.rel, StartLine: int(c.Node.StartPoint().Row) + 1,
				EndLine:   int(c.Node.EndPoint().Row) + 1,
				StartByte: startByte, EndByte: endByte,
				Language: "sol", Confidence: types.ConfExtracted,
			})
			v.edges = append(v.edges, types.Edge{
				Src: v.fileID, Dst: id, Type: types.EdgeDefines,
				Count: 1, Confidence: types.ConfExtracted,
			})
		}
	}
}

// runStateVarDecl walks all state_variable_declaration nodes once. Non-mapping
// state vars become Field nodes; declarations whose type_name has key_type +
// value_type fields are emitted as Mapping nodes. Unifying both kinds in one
// pass lets us avoid a separate queryMappingDecl (which the grammar doesn't
// expose as a distinct node type) and keeps mapping detection adjacent to its
// type-introspection logic.
func (v *declVisitor) runStateVarDecl() {
	query, err := sitter.NewQuery([]byte(queryStateVarAll), v.lang)
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
			if query.CaptureNameForId(c.Index) != "decl" {
				continue
			}
			nameNode := c.Node.ChildByFieldName("name")
			typeNode := c.Node.ChildByFieldName("type")
			if nameNode == nil {
				continue
			}
			name := nameNode.Content(v.src)
			startByte := int(nameNode.StartByte())
			endByte := int(nameNode.EndByte())
			line := int(nameNode.StartPoint().Row) + 1
			isMapping := typeNode != nil && typeNameIsMapping(typeNode, v.src)
			var nt types.NodeType
			var qname string
			if isMapping {
				nt = types.NodeMapping
				qname = name + ":mapping"
			} else {
				nt = types.NodeField
				qname = name
			}
			id := parse.MakeID(qname, "sol", startByte)
			v.nodes = append(v.nodes, types.Node{
				ID: id, Type: nt, Name: name, QualifiedName: qname,
				FilePath: v.rel, StartLine: line, EndLine: line,
				StartByte: startByte, EndByte: endByte,
				Language: "sol", Confidence: types.ConfExtracted,
			})
			v.edges = append(v.edges, types.Edge{
				Src: v.fileID, Dst: id, Type: types.EdgeDefines,
				Count: 1, Confidence: types.ConfExtracted,
			})
			if isMapping {
				// TODO(T19+): pass `id` here once writes_mapping can be emitted as
				// a same-file resolved edge directly (skip pending pipeline).
				v.queueMappingWrites(name)
			}
		}
	}
}

// queueMappingWrites scans every function in the current root for an
// augmented_assignment_expression whose LHS array_access targets the given
// mapping name, and queues a pending writes_mapping edge. V0 simplification:
// we treat any `name[...] = ...` or `name[...] += ...` as a write.
func (v *declVisitor) queueMappingWrites(mappingName string) {
	q := `(augmented_assignment_expression
	         (expression (array_access (expression (identifier) @arr))))
	      @stmt`
	query, err := sitter.NewQuery([]byte(q), v.lang)
	if err != nil {
		// Fallback: try plain assignment_expression too.
		return
	}
	cur := sitter.NewQueryCursor()
	cur.Exec(query, v.root)
	for {
		m, ok := cur.NextMatch()
		if !ok {
			break
		}
		var arrName string
		var stmtNode *sitter.Node
		for _, c := range m.Captures {
			cap := query.CaptureNameForId(c.Index)
			if cap == "arr" {
				arrName = c.Node.Content(v.src)
			} else if cap == "stmt" {
				stmtNode = c.Node
			}
		}
		if arrName != mappingName || stmtNode == nil {
			continue
		}
		fnQ, fnStart, ok := nearestFunctionQnameAndStart(stmtNode, v.src)
		if !ok {
			continue
		}
		// SrcID must match the function node ID emitted in runDecl, which
		// hashes (qname, "sol", name-node startByte). Using offset 0 here would
		// produce an ID that never resolves to a real node and graph.Validate
		// would reject the resulting edge as dangling.
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:       parse.MakeID(fnQ, "sol", fnStart),
			EdgeType:    types.EdgeWritesMapping,
			TargetQName: mappingName + ":mapping",
			Line:        int(stmtNode.StartPoint().Row) + 1,
		})
	}
}

func (v *declVisitor) runEmits() {
	query, err := sitter.NewQuery([]byte(queryEmit), v.lang)
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
		var event string
		var fnQ string
		var fnStart int
		var fnOK bool
		var line int
		for _, c := range m.Captures {
			if query.CaptureNameForId(c.Index) == "event" {
				event = c.Node.Content(v.src)
				fnQ, fnStart, fnOK = nearestFunctionQnameAndStart(c.Node, v.src)
				line = int(c.Node.StartPoint().Row) + 1
			}
		}
		if event == "" || !fnOK {
			continue
		}
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:       parse.MakeID(fnQ, "sol", fnStart),
			EdgeType:    types.EdgeEmitsEvent,
			TargetQName: event,
			Line:        line,
		})
	}
}

func (v *declVisitor) runHasModifier() {
	query, err := sitter.NewQuery([]byte(queryHasModifier), v.lang)
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
		var mod string
		var fnQ string
		var fnStart int
		var fnOK bool
		var line int
		for _, c := range m.Captures {
			if query.CaptureNameForId(c.Index) == "mod" {
				mod = c.Node.Content(v.src)
				fnQ, fnStart, fnOK = nearestFunctionQnameAndStart(c.Node, v.src)
				line = int(c.Node.StartPoint().Row) + 1
			}
		}
		if mod == "" || !fnOK {
			continue
		}
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:       parse.MakeID(fnQ, "sol", fnStart),
			EdgeType:    types.EdgeHasModifier,
			TargetQName: mod,
			Line:        line,
		})
	}
}

// collectABI populates p.abi from the discovered Contract / Function nodes.
// Iteration order matches v.nodes (which is append order from visit()), so
// Contract nodes are seen before their methods because runDecl(Contract)
// runs before runDecl(Function). For nested contracts we'd need a smarter
// scope-tracking pass; V0 is single-level.
func (v *declVisitor) collectABI() {
	currentContract := ""
	for _, n := range v.nodes {
		switch n.Type {
		case types.NodeContract:
			currentContract = n.Name
		case types.NodeFunction:
			if currentContract == "" {
				continue
			}
			v.abi[currentContract] = append(v.abi[currentContract], ABISig{
				ContractName: currentContract,
				FunctionName: n.Name,
				ParamTypes:   nil, // V0 placeholder — name-match is sufficient.
			})
		}
	}
}

// helpers

// nearestContractName walks the parent chain looking for an enclosing
// contract_declaration and returns its name (empty if none).
func nearestContractName(n *sitter.Node, src []byte) string {
	for cur := n; cur != nil; cur = cur.Parent() {
		if cur.Type() == "contract_declaration" {
			id := cur.ChildByFieldName("name")
			if id != nil {
				return id.Content(src)
			}
		}
	}
	return ""
}

// nearestFunctionQnameAndStart walks the parent chain to the enclosing
// function_definition and returns its qualified name (Contract.Func or just
// Func) plus the StartByte of the function's name identifier — the same
// (qname, startByte) pair that runDecl(NodeFunction) uses to mint the
// function node ID. Pending refs that build SrcID via parse.MakeID(fnQ,
// "sol", fnStart) will therefore resolve to a real node, avoiding dangling
// edges in graph.Validate.
//
// Returns ok=false when no enclosing function_definition exists or its
// name field is missing (defensive — every emit / modifier_invocation /
// mapping write in valid Solidity sits inside a function with a name).
func nearestFunctionQnameAndStart(n *sitter.Node, src []byte) (string, int, bool) {
	cn := nearestContractName(n, src)
	for cur := n; cur != nil; cur = cur.Parent() {
		if cur.Type() == "function_definition" {
			id := cur.ChildByFieldName("name")
			if id == nil {
				return "", 0, false
			}
			ident := id.Content(src)
			qname := ident
			if cn != "" {
				qname = cn + "." + ident
			}
			return qname, int(id.StartByte()), true
		}
	}
	return "", 0, false
}

// nearestFunctionQname is the qname-only form, retained for callers (like
// future statement-level extractors) that don't need the start byte.
func nearestFunctionQname(n *sitter.Node, src []byte) string {
	q, _, ok := nearestFunctionQnameAndStart(n, src)
	if ok {
		return q
	}
	return strings.TrimSpace(string(src[n.StartByte():n.EndByte()]))
}

// typeNameIsMapping reports whether a type_name node represents a mapping
// declaration. The grammar models mappings as a hidden _mapping rule inlined
// into type_name, so we detect them by the presence of `key_type` /
// `value_type` fields, falling back to a textual `mapping(` prefix check.
func typeNameIsMapping(n *sitter.Node, src []byte) bool {
	if n == nil {
		return false
	}
	if n.ChildByFieldName("key_type") != nil || n.ChildByFieldName("value_type") != nil {
		return true
	}
	txt := strings.TrimSpace(string(src[n.StartByte():n.EndByte()]))
	return strings.HasPrefix(txt, "mapping")
}
