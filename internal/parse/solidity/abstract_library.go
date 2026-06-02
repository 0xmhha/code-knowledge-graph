package solidity

import (
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Sol W4 — SubKind detection for `abstract contract` and `library`.
//
// Spec: docs/design/solidity-inheritance-and-interface-dispatch.md §2.1, §4.4
// Dispatch index: docs/DISPATCH-WITHIN-LANG-SEMANTICS.md §2 Phase 3
//
// The tree-sitter-solidity grammar exposes three distinct top-level nodes
// for contract-like declarations (verified against binding/parser.c symbol
// table):
//
//   - `contract_declaration` — both `contract Foo` and `abstract contract Foo`.
//     The `abstract` keyword (anon_sym_abstract) appears as an anonymous
//     leading token child of the contract_declaration node.
//   - `library_declaration`   — `library Foo`.
//   - `interface_declaration` — `interface IFoo`. (Reserved for W1.)
//
// W4 scope is narrow: emit `NodeContract` with `SubKind` populated so
// downstream consumers can distinguish abstract/library from plain
// contracts. No new node or edge types. No schema bump.

// runContractDecl replaces the generic runDecl(queryContract, NodeContract)
// path. Like runDecl it walks every `contract_declaration` query match and
// emits a Contract node + defines edge, but additionally inspects the
// declaration's anonymous keyword children to set SubKind to "abstract"
// when an `abstract` token precedes `contract`. Plain contracts get
// SubKind="contract" (explicit value — see §2.1 SubKind extension table).
func (v *declVisitor) runContractDecl() {
	query, qErr := sitter.NewQuery(v.lang, queryContract)
	if qErr != nil {
		return
	}
	defer func() { query.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		var nameNode *sitter.Node
		var declNode *sitter.Node
		for _, c := range m.Captures {
			switch names[c.Index] {
			case "name":
				n := c.Node
				nameNode = &n
			case "decl":
				n := c.Node
				declNode = &n
			}
		}
		if nameNode == nil || declNode == nil {
			continue
		}
		subKind := "contract"
		if hasAbstractKeyword(declNode, v.src) {
			subKind = "abstract"
		}
		v.emitContractLikeNode(*nameNode, *declNode, types.NodeContract, subKind)
	}
}

// runInterfaceDecl walks every `interface_declaration` match and emits
// an Interface node (Q1: reuse the existing pkg/types.NodeInterface enum
// — same idiom as Go/TS, no new node type, no schema bump).
//
// Interfaces in Solidity carry function signatures but no bodies; the
// existing queryFunction matches them globally, and nearestContractName
// (already extended in W4 to walk interface_declaration) supplies the
// qualified "IFoo.bar" prefix so interface methods are addressable as
// PendingRef targets in W3 (interface dispatch — separate dispatch).
//
// Per §2.1 SubKind extension table, plain interfaces get
// SubKind="interface" (mirrors the explicit "contract" / "abstract" /
// "library" labelling so consumers can filter by SubKind without
// inferring from NodeType).
func (v *declVisitor) runInterfaceDecl() {
	query, qErr := sitter.NewQuery(v.lang, queryInterface)
	if qErr != nil {
		return
	}
	defer func() { query.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		var nameNode *sitter.Node
		var declNode *sitter.Node
		for _, c := range m.Captures {
			switch names[c.Index] {
			case "name":
				n := c.Node
				nameNode = &n
			case "decl":
				n := c.Node
				declNode = &n
			}
		}
		if nameNode == nil || declNode == nil {
			continue
		}
		v.emitContractLikeNode(*nameNode, *declNode, types.NodeInterface, "interface")
	}
}

// runLibraryDecl walks every `library_declaration` match and emits a
// Contract node with SubKind="library". Per §5.0 Q2: libraries are a
// syntactic variant of contracts, so we re-use NodeContract instead of
// introducing a NodeLibrary type (schema impact = zero).
//
// Library bodies contain function_definitions just like contracts; the
// existing queryFunction matches them globally, and nearestContractName
// (updated to also recognise library_declaration) supplies the qualified
// "Library.func" prefix so library functions appear under their library.
func (v *declVisitor) runLibraryDecl() {
	query, qErr := sitter.NewQuery(v.lang, queryLibrary)
	if qErr != nil {
		return
	}
	defer func() { query.Close() }()
	cur := sitter.NewQueryCursor()
	defer func() { cur.Close() }()
	matches := cur.Matches(query, v.root, v.src)
	names := query.CaptureNames()
	for {
		m := matches.Next()
		if m == nil {
			break
		}
		var nameNode *sitter.Node
		var declNode *sitter.Node
		for _, c := range m.Captures {
			switch names[c.Index] {
			case "name":
				n := c.Node
				nameNode = &n
			case "decl":
				n := c.Node
				declNode = &n
			}
		}
		if nameNode == nil || declNode == nil {
			continue
		}
		v.emitContractLikeNode(*nameNode, *declNode, types.NodeContract, "library")
	}
}

// emitContractLikeNode is the shared emit path for contract / library
// declarations. Mirrors runDecl's emit but additionally stamps SubKind.
// Kept in this file (rather than declarations.go) to keep the W4 detector
// logic localised — easier to extend in W1 (interface_declaration) without
// re-touching declarations.go.
func (v *declVisitor) emitContractLikeNode(
	nameNode, declNode sitter.Node,
	nt types.NodeType,
	subKind string,
) {
	ident := nameNode.Utf8Text(v.src)
	startByte := int(nameNode.StartByte())
	endByte := int(nameNode.EndByte())
	id := parse.MakeID(ident, "sol", startByte)
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: nt, Name: ident, QualifiedName: ident,
		FilePath:  v.rel,
		StartLine: int(nameNode.StartPosition().Row) + 1,
		EndLine:   int(nameNode.EndPosition().Row) + 1,
		StartByte: startByte, EndByte: endByte,
		Language: "sol", Confidence: types.ConfExtracted,
		SubKind: subKind,
	})
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines,
		Count: 1, Confidence: types.ConfExtracted,
	})
	_ = declNode // reserved for future use (e.g. inheritance specifier in W1)
}

// hasAbstractKeyword reports whether a contract_declaration node carries
// a leading `abstract` keyword. The tree-sitter-solidity grammar models
// `abstract` as an anonymous keyword token (anon_sym_abstract) preceding
// the `contract` keyword inside contract_declaration. We detect it via
// the declaration's source text prefix — robust to whitespace/comments
// between `abstract` and `contract` because the grammar guarantees the
// declaration's first non-whitespace token is one of those two keywords.
func hasAbstractKeyword(decl *sitter.Node, src []byte) bool {
	if decl == nil {
		return false
	}
	start := int(decl.StartByte())
	end := int(decl.EndByte())
	if start < 0 || end > len(src) || start >= end {
		return false
	}
	// Skip leading whitespace (tree-sitter trims most, but be defensive).
	i := start
	for i < end && isSolWhitespace(src[i]) {
		i++
	}
	const abstractKw = "abstract"
	if end-i < len(abstractKw) {
		return false
	}
	if string(src[i:i+len(abstractKw)]) != abstractKw {
		return false
	}
	// Ensure the match is a whole token, not the prefix of an identifier
	// such as `abstractor`. The next char must be whitespace or `{`/`(`
	// — but in practice it is always whitespace before the `contract`
	// keyword.
	next := i + len(abstractKw)
	if next >= end {
		return false
	}
	return isSolWhitespace(src[next])
}

// isSolWhitespace reports whether b is whitespace per Solidity grammar
// (space, tab, CR, LF). Newlines between keywords are legal.
func isSolWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}
