package solidity

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

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

// newDeclVisitor allocates a per-file visitor with a local abi map. The
// caller (ParseFile) merges v.abi into the shared Parser.abi under lock
// after visit() returns — this keeps collectABI race-free under the
// concurrent ParseFile dispatch buildpipe now uses.
func newDeclVisitor(rel string, src []byte, lang *sitter.Language, root *sitter.Node) *declVisitor {
	v := &declVisitor{rel: rel, src: src, lang: lang, root: root, abi: map[string][]ABISig{}}
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
	// W4: contract & library decls use SubKind-aware emit paths so the
	// graph distinguishes plain / abstract / library variants. See
	// abstract_library.go and docs/design/solidity-inheritance-and-
	// interface-dispatch.md §2.1 / §4.4.
	//
	// W1: interface_declaration gets its own emit path so it lands as
	// NodeInterface (Q1: reuse the existing NodeInterface enum, shared
	// with TS/Go idiom). Inheritance / implementation detection runs
	// after contract / interface decls have been emitted — the W1
	// PendingRefs are name-resolved in Pass 2 (resolveInheritance) so
	// they can see nodes from other files.
	v.runContractDecl()
	v.runLibraryDecl()
	v.runInterfaceDecl()
	// W2: function emit is SubKind-aware (virtual / override / virtual_override
	// / function). The generic runDecl path is bypassed for functions so the
	// modifier scan and EdgeOverrides PendingRef emission share a single AST
	// walk. Node IDs and the `defines` edge stay identical to runDecl, so
	// downstream consumers (ABI, mapping writes, emits, modifier_invocation)
	// continue to resolve against the same Function node.
	v.runFunctionDecl()
	v.runDecl(queryModifier, types.NodeModifier)
	v.runDecl(queryEvent, types.NodeEvent)
	v.runDecl(queryStruct, types.NodeStruct)
	v.runDecl(queryEnum, types.NodeEnum)
	v.runStateVarDecl()
	v.runEmits()
	v.runHasModifier()
	v.runInheritance()
	// W3 (interface dispatch): walks body member_expression nodes that
	// fit the `IFoo(addr).bar()` shape and queues EdgeInvokes PendingRefs
	// resolved in Pass 2 (resolveInterfaceDispatch). Confidence is always
	// ConfAmbiguous per §5.0 Q5 — see dispatch.go preamble.
	v.runDispatch()
	// W6 (using For): emits EdgeUsesFor PendingRefs from `using LibName for
	// TypeName;` directives nested inside a contract / library / interface
	// body. Q9-1 (b) decision (2026-05-12). V0 scope: binding declaration.
	// V1.0 (same call) additionally emits typebind PendingRefs (for the
	// per-contract binding map) and method-call PendingRefs (via the
	// separate runUsingForCalls below).
	v.runUsingFor()
	// W6 V1.0 (2026-05-12): method-call dispatch detector. Walks every
	// member_expression that fits `<identifier>.<identifier>(...)` and
	// queues a PendingRef that Pass 2 resolves through the binding map.
	// State-variable receivers only (Q9-2 (a) V0 limit); parameter
	// receivers added in V1.1 via emitParameterMetaPending in overrides.go.
	v.runUsingForCalls()
	v.collectABI()
}

func (v *declVisitor) runDecl(q string, nt types.NodeType) {
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
			qname := ident
			if nt == types.NodeFunction {
				if cn := nearestContractName(&node, v.src); cn != "" {
					qname = cn + "." + ident
				}
			}
			id := parse.MakeID(qname, "sol", startByte)
			v.nodes = append(v.nodes, types.Node{
				ID: id, Type: nt, Name: ident, QualifiedName: qname,
				FilePath: v.rel, StartLine: int(node.StartPosition().Row) + 1,
				EndLine:   int(node.EndPosition().Row) + 1,
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
	query, qErr := sitter.NewQuery(v.lang, queryStateVarAll)
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
			if names[c.Index] != "decl" {
				continue
			}
			declNode := c.Node
			nameNode := declNode.ChildByFieldName("name")
			typeNode := declNode.ChildByFieldName("type")
			if nameNode == nil {
				continue
			}
			name := nameNode.Utf8Text(v.src)
			startByte := int(nameNode.StartByte())
			endByte := int(nameNode.EndByte())
			line := int(nameNode.StartPosition().Row) + 1
			isMapping := typeNode != nil && typeNameIsMapping(typeNode, v.src)
			var nt types.NodeType
			var qname string
			if isMapping {
				nt = types.NodeMapping
				qname = name + ":mapping"
			} else {
				nt = types.NodeField
				// W-C W6 V1.0 (2026-05-12): qualify NodeField qnames with
				// the enclosing container's name so Pass 2 can recover
				// the state-var → container relationship cheaply (same
				// idiom as runFunctionDecl's `Container.func` qname).
				// File-level state-var declarations are out of scope in
				// Solidity; nearestContractName returns "" for them and
				// the qname falls back to the bare name (extant V0
				// shape preserved for that edge case).
				qname = name
				if cn := nearestContractName(nameNode, v.src); cn != "" {
					qname = cn + "." + name
				}
			}
			id := parse.MakeID(qname, "sol", startByte)
			// W-C W6 V1.0 (2026-05-12): stash the declared type name on the
			// Field/Mapping node's Signature so Pass 2 can resolve
			// `<stateVar>.<method>(...)` callsites by looking up the
			// state-variable's type and matching it against the using-for
			// binding map. We use Signature (not SubKind) because the value
			// here is the raw user-written type expression — V0 graph
			// consumers already treat Signature as opaque metadata. Empty
			// typeNode (rare — degenerate parses) → Signature stays empty.
			signature := ""
			if typeNode != nil {
				signature = extractTypeNameText(typeNode, v.src)
			}
			v.nodes = append(v.nodes, types.Node{
				ID: id, Type: nt, Name: name, QualifiedName: qname,
				FilePath: v.rel, StartLine: line, EndLine: line,
				StartByte: startByte, EndByte: endByte,
				Language: "sol", Confidence: types.ConfExtracted,
				Signature: signature,
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
	query, qErr := sitter.NewQuery(v.lang, q)
	if qErr != nil {
		// Fallback: try plain assignment_expression too.
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
		var arrName string
		var stmtNode *sitter.Node
		for _, c := range m.Captures {
			capName := names[c.Index]
			node := c.Node
			if capName == "arr" {
				arrName = node.Utf8Text(v.src)
			} else if capName == "stmt" {
				// The capture's Node is a value type; we need a stable pointer
				// for the parent walk below. Take address of the local copy.
				stmtCopy := node
				stmtNode = &stmtCopy
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
			Line:        int(stmtNode.StartPosition().Row) + 1,
		})
	}
}

func (v *declVisitor) runEmits() {
	query, qErr := sitter.NewQuery(v.lang, queryEmit)
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
		var event string
		var fnQ string
		var fnStart int
		var fnOK bool
		var line int
		for _, c := range m.Captures {
			if names[c.Index] == "event" {
				node := c.Node
				event = node.Utf8Text(v.src)
				fnQ, fnStart, fnOK = nearestFunctionQnameAndStart(&node, v.src)
				line = int(node.StartPosition().Row) + 1
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
	query, qErr := sitter.NewQuery(v.lang, queryHasModifier)
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
		var mod string
		var fnQ string
		var fnStart int
		var fnOK bool
		var line int
		for _, c := range m.Captures {
			if names[c.Index] == "mod" {
				node := c.Node
				mod = node.Utf8Text(v.src)
				fnQ, fnStart, fnOK = nearestFunctionQnameAndStart(&node, v.src)
				line = int(node.StartPosition().Row) + 1
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
// contract-like declaration and returns its name (empty if none).
//
// W4: also recognises `library_declaration` (Sol libraries hold function
// definitions just like contracts do; their methods should be qualified
// as "Library.func" the same way contract methods are). Reserved for
// future extension: `interface_declaration` (W1 — interface methods).
func nearestContractName(n *sitter.Node, src []byte) string {
	for cur := n; cur != nil; cur = cur.Parent() {
		switch cur.Kind() {
		case "contract_declaration", "library_declaration", "interface_declaration":
			id := cur.ChildByFieldName("name")
			if id != nil {
				return id.Utf8Text(src)
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
		if cur.Kind() == "function_definition" {
			id := cur.ChildByFieldName("name")
			if id == nil {
				return "", 0, false
			}
			ident := id.Utf8Text(src)
			qname := ident
			if cn != "" {
				qname = cn + "." + ident
			}
			return qname, int(id.StartByte()), true
		}
	}
	return "", 0, false
}

// extractTypeNameText returns the user-written type expression for a
// state_variable_declaration's type_name child. Used by W-C W6 V1.0 to
// stamp NodeField.Signature with the declared type so Pass 2 can resolve
// `<stateVar>.<method>(...)` against the using-for binding map.
//
// Three shapes covered:
//   - primitive_type / user_defined_type → identifier text (`uint256`,
//     `MyContract`).
//   - mapping → returns "" (mapping receivers don't participate in
//     using-for dispatch; the resolver naturally drops them).
//   - other compound types (array_type, function_type) → raw subtree text
//     normalised by stripping outer whitespace. This is a permissive
//     fallback so future receiver shapes don't crash the parse; the V0
//     binding map lookup will simply miss when the typeName doesn't match
//     any directive's bound type.
func extractTypeNameText(typeNode *sitter.Node, src []byte) string {
	if typeNode == nil {
		return ""
	}
	if typeNameIsMapping(typeNode, src) {
		return ""
	}
	// First, look for a direct named child (primitive_type /
	// user_defined_type). The grammar wraps these in type_name; the inner
	// named child is the one carrying the identifier we care about.
	for i := uint(0); i < uint(typeNode.NamedChildCount()); i++ {
		c := typeNode.NamedChild(i)
		switch c.Kind() {
		case "primitive_type":
			return strings.TrimSpace(string(src[c.StartByte():c.EndByte()]))
		case "user_defined_type":
			// user_defined_type may be `Foo` or `Ns.Foo` — V0 takes the
			// trailing identifier (same idiom as TS heritage resolution).
			if id := lastIdentifier(c); id != nil {
				return id.Utf8Text(src)
			}
			return strings.TrimSpace(string(src[c.StartByte():c.EndByte()]))
		}
	}
	// Permissive fallback — raw subtree text. Whitespace-trimmed so
	// downstream string compares stay clean.
	return strings.TrimSpace(string(src[typeNode.StartByte():typeNode.EndByte()]))
}

// lastIdentifier returns the rightmost identifier-like named child under n,
// flattening `Ns.Foo`-style user_defined_type references to their final
// segment.
func lastIdentifier(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	for i := int(n.NamedChildCount()) - 1; i >= 0; i-- {
		c := n.NamedChild(uint(i))
		if c.Kind() == "identifier" {
			return c
		}
	}
	return nil
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
