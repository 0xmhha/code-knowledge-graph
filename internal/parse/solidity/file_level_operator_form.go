package solidity

import (
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Sol W-C W6 V2.5 (2026-05-19) — file-level operator-form using
// directive recovery.
//
// Sol 0.8.13+ file-level `using {f1 as +, f2 as -} for T [global];`
// (free-function or library-method form with user-defined operator
// bindings) is parsed by vendored tree-sitter-solidity v1.2.11 as an
// ERROR child of source_file. Unlike the contract-scope misparse
// V2.20 handles (which becomes a state_variable_declaration with
// recoverable named children), the file-level shape exposes only:
//
//	source_file
//	  ERROR "using {f1 as +, f2 as -} for T [global];"
//	    type_name "using"        ← keyword reinterpreted
//	    type_name "T"            ← bound type
//	    identifier "global"      ← optional qualifier
//
// The braced body (`{f1 as +, f2 as -}`) is consumed by the ERROR
// node text but not surfaced as named children. V2.18's walker
// declines because its source-order extraction picks "global" as
// libName but doesn't see a following type_name (typeName already
// passed by). V2.5 fills the gap by parsing the ERROR text directly.
//
// Per Sol semantics, file-level using applies to every contract /
// interface in the file regardless of the `global` qualifier (the
// qualifier only controls cross-file binding scope, which V0
// cross-file resolution already handles via NodeFile). One emit per
// (container, bound function) pair, mirroring V2.18's fan-out.
//
// Multi-function form (`{add as +, sub as -}`) emits one pair per
// function. Library-method form (`{Lib.method as +}`) reduces to the
// library name (the same shape V2.20 emits for contract-scope), so
// downstream binding-map keys line up.
//
// Limitations:
//   - The `as +` operator association is not preserved on the edge —
//     V0 dispatch resolution does not yet detect operator usage at
//     call sites, so the operator metadata would be unused.
//   - Free-function form (no library prefix) creates an edge pointing
//     at the free-function NodeFunction; downstream binding map
//     resolution joins it with the type binding key (libName | type)
//     so dispatch through this form would need a separate pass to
//     match operator usage.

func (v *declVisitor) runFileLevelOperatorForm() {
	if v.root == nil {
		return
	}
	var containerIDs []string
	for _, n := range v.nodes {
		switch n.Type {
		case types.NodeContract:
			if n.SubKind != "library" {
				containerIDs = append(containerIDs, n.ID)
			}
		case types.NodeInterface:
			containerIDs = append(containerIDs, n.ID)
		}
	}
	if len(containerIDs) == 0 {
		return
	}
	for i := uint(0); i < v.root.NamedChildCount(); i++ {
		child := v.root.NamedChild(i)
		if child == nil || child.Kind() != "ERROR" {
			continue
		}
		text := child.Utf8Text(v.src)
		libNames, typeName, ok := parseFileLevelOperatorForm(text)
		if !ok {
			continue
		}
		line := int(child.StartPosition().Row) + 1
		byteOff := int(child.StartByte())
		for _, libName := range libNames {
			if v.namespaceAliases[libName] {
				continue
			}
			if orig, hit := v.importAliases[libName]; hit {
				libName = orig
			}
			for _, srcID := range containerIDs {
				v.pending = append(v.pending, parse.PendingRef{
					SrcID:        srcID,
					EdgeType:     types.EdgeUsesFor,
					TargetQName:  libName,
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
}

// parseFileLevelOperatorForm extracts (library/function names, bound
// type) from the raw ERROR text of a misparsed file-level operator-
// form using directive. Recognises both free-function shape and
// library-method shape (the dotted prefix becomes the library name,
// matching V2.20's reduction). Returns ok=false if any of the three
// structural markers (`{`, `}`, ` for `) is missing.
func parseFileLevelOperatorForm(text string) ([]string, string, bool) {
	if !strings.HasPrefix(text, "using ") {
		return nil, "", false
	}
	braceOpen := strings.Index(text, "{")
	braceClose := strings.Index(text, "}")
	forIdx := strings.Index(text, " for ")
	if braceOpen < 0 || braceClose < 0 || forIdx < 0 || braceClose <= braceOpen {
		return nil, "", false
	}
	body := text[braceOpen+1 : braceClose]
	var libNames []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(body, ",") {
		entry := strings.TrimSpace(raw)
		if asIdx := strings.Index(entry, " as "); asIdx >= 0 {
			entry = strings.TrimSpace(entry[:asIdx])
		}
		if dotIdx := strings.Index(entry, "."); dotIdx >= 0 {
			entry = entry[:dotIdx]
		}
		if entry == "" || seen[entry] {
			continue
		}
		seen[entry] = true
		libNames = append(libNames, entry)
	}
	if len(libNames) == 0 {
		return nil, "", false
	}
	rest := strings.TrimLeft(text[forIdx+len(" for "):], " \t")
	var typeName string
	for _, term := range []string{" global", ";"} {
		if idx := strings.Index(rest, term); idx >= 0 {
			typeName = rest[:idx]
			break
		}
	}
	if typeName == "" {
		typeName = rest
	}
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return nil, "", false
	}
	return libNames, typeName, true
}
