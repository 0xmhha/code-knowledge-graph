package solidity

import (
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Sol W-C W6 V2.20 (2026-05-18) — operator-form using directive
// ERROR-tolerant recovery.
//
// Sol 0.8.19+ `using {f as +} for T;` is parsed by vendored
// tree-sitter-solidity v1.2.11 as an ERROR-wrapped state_variable_
// declaration with no surrounding using_directive node. V2.17's
// AST probe captured the exact misparse shape:
//
//   contract_declaration (★HasError)
//     identifier "Calc"
//     ERROR "{ using" (★HasError)
//     contract_body (★HasError)
//       state_variable_declaration (★HasError)
//         type_name
//           user_defined_type
//             identifier "Math"     ← library name (recoverable)
//             identifier "add"      ← method name (not consumed by V0)
//         identifier "as"           ← discriminator: real state-var
//                                     declarations never have a bare
//                                     "as" identifier child
//         ERROR "+} for uint256"    ← contains the bound type
//       function_definition (...)
//
// The walker pattern-matches this shape and emits the same PendingRef
// pair runUsingFor produces for type-alias form directives, so the
// downstream binding map and dispatch resolution paths reuse existing
// infrastructure unchanged.
//
// V2.5 file-level operator-form keeps its 0-edge lock — its misparse
// surfaces at source_file scope where state_variable_declaration is
// not a valid child, and V2.18's file-level walker rejects the
// directive at the libName-extraction step (no clean identifier).
//
// V2.7 / V2.14 IOp / V2.17 locks flip from 0 → 1 in the same V-cycle.

// matchOperatorFormStateVar inspects a state_variable_declaration
// node for the operator-form misparse signature. Returns
// (libName, boundType, true) when the shape matches; the bound type
// is extracted from the trailing ERROR text by stripping the leading
// operator characters and the " for " separator.
func matchOperatorFormStateVar(node *sitter.Node, src []byte) (string, string, bool) {
	lib, boundType, _, ok := matchOperatorFormStateVarFull(node, src)
	return lib, boundType, ok
}

// matchOperatorFormStateVarFull is the W-C W6 V8 (2026-05-19)
// variant that also returns the method name embedded in the
// misparse (e.g. `add` from `using {Math.add as +}`). The legacy
// 3-tuple wrapper above keeps the original callers compiling
// without forcing them to consume the method when they don't need
// it.
func matchOperatorFormStateVarFull(node *sitter.Node, src []byte) (string, string, string, bool) {
	if node == nil || node.Kind() != "state_variable_declaration" || !node.HasError() {
		return "", "", "", false
	}
	var libName, methodName string
	var hasAsIdent bool
	var trailingError string
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "type_name":
			// Look for user_defined_type child. First identifier is
			// the library name; second identifier (if present) is
			// the method name in the `Lib.method` form.
			udt := child.NamedChild(0)
			if udt != nil && udt.Kind() == "user_defined_type" {
				if id := udt.NamedChild(0); id != nil && id.Kind() == "identifier" {
					libName = id.Utf8Text(src)
				}
				if id := udt.NamedChild(1); id != nil && id.Kind() == "identifier" {
					methodName = id.Utf8Text(src)
				}
			}
		case "identifier":
			if child.Utf8Text(src) == "as" {
				hasAsIdent = true
			}
		case "ERROR":
			text := child.Utf8Text(src)
			if strings.Contains(text, " for ") {
				trailingError = strings.TrimSpace(text)
			}
		}
	}
	if !hasAsIdent || libName == "" || trailingError == "" {
		return "", "", "", false
	}
	idx := strings.Index(trailingError, " for ")
	if idx < 0 {
		return "", "", "", false
	}
	rest := strings.TrimSpace(trailingError[idx+len(" for "):])
	rest = strings.TrimRight(rest, ";} \t\n")
	if rest == "" {
		return "", "", "", false
	}
	return libName, rest, methodName, true
}

// runOperatorFormRecovery walks every state_variable_declaration
// node and applies matchOperatorFormStateVar. On match, finds the
// enclosing container (contract / library / interface) via
// nearestContractName and emits the same PendingRef pair runUsingFor
// produces for type-alias form using directives.
func (v *declVisitor) runOperatorFormRecovery() {
	const q = `(state_variable_declaration) @sv`
	query, qErr := sitter.NewQuery(v.lang, q)
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
		for _, c := range m.Captures {
			if names[c.Index] != "sv" {
				continue
			}
			node := c.Node
			libName, typeName, methodName, ok := matchOperatorFormStateVarFull(&node, v.src)
			if !ok {
				continue
			}
			containerName := nearestContractName(&node, v.src)
			if containerName == "" {
				continue
			}
			// Find the container node's name-position byte to compute
			// the SrcID. nearestContractName returned the name text;
			// we need the identifier node for MakeID consistency with
			// runContractDecl / runLibraryDecl / runInterfaceDecl.
			containerStart, ok := nearestContractNameStart(&node, v.src)
			if !ok {
				continue
			}
			srcID := parse.MakeID(containerName, "sol", containerStart)
			line := int(node.StartPosition().Row) + 1
			byteOff := int(node.StartByte())

			// Apply namespace / import alias normalisation (matches
			// runUsingFor / runFileLevelUsingFor).
			if v.namespaceAliases[libName] {
				continue
			}
			if orig, hit := v.importAliases[libName]; hit {
				libName = orig
			}

			// W-C W6 V8 (2026-05-19): encode the method name with the
			// RFC record separator so resolveUsingForRef can decode it
			// without colliding with V5's `||<path>` hint syntax.
			target := libName
			if methodName != "" {
				target = libName + "\x1e" + methodName
			}
			v.pending = append(v.pending, parse.PendingRef{
				SrcID:        srcID,
				EdgeType:     types.EdgeUsesFor,
				TargetQName:  target,
				Line:         line,
				ByteOffset:   byteOff,
				DispatchKind: dispatchKindUsingFor,
			})
			v.pending = append(v.pending, parse.PendingRef{
				SrcID:        srcID,
				EdgeType:     types.EdgeUsesFor,
				TargetQName:  libName + "|" + typeName,
				Line:         line,
				ByteOffset:   byteOff,
				DispatchKind: dispatchKindUsingForTypeBind,
			})
		}
	}
}

// nearestContractNameStart walks the parent chain like
// nearestContractName but returns the StartByte of the container's
// name identifier so callers can mint a SrcID matching the container
// node emitted by runContractDecl / runLibraryDecl / runInterfaceDecl.
func nearestContractNameStart(n *sitter.Node, src []byte) (int, bool) {
	for cur := n; cur != nil; cur = cur.Parent() {
		switch cur.Kind() {
		case "contract_declaration", "library_declaration", "interface_declaration":
			id := cur.ChildByFieldName("name")
			if id != nil {
				return int(id.StartByte()), true
			}
		}
	}
	return 0, false
}
