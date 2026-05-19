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
		entries, typeName, ok := parseFileLevelOperatorForm(text)
		if !ok {
			continue
		}
		line := int(child.StartPosition().Row) + 1
		byteOff := int(child.StartByte())
		// Dedup on the *resolved* libName: a library-method form
		// `{Math.add, Math.sub}` reduces both entries to libName=Math
		// and should emit one binding pair, not two. Namespace-
		// aliased forms `{M.add, M.sub}` resolve to distinct
		// libNames (add, sub) and stay separate.
		seenLib := map[string]bool{}
		for _, entry := range entries {
			libName, ok := v.resolveUsingBindingLeading(entry)
			if !ok || seenLib[libName] {
				continue
			}
			seenLib[libName] = true
			// W-C W6 V5 / V6 (2026-05-19): when the entry's leading
			// identifier is a namespace alias (V5) or named-import
			// alias (V6) with a recorded source path, attach the
			// path to the binding PendingRef as a homonym
			// disambiguation hint. resolveUsingForRef prefers
			// candidates whose file path matches the hint before
			// falling back to pickSameFileCandidate.
			//
			// W-C W6 V8 (2026-05-19): also append the method name (the
			// tail's leaf segment when present, e.g. `add` from
			// `M.SubMath.add`) encoded with the RFC record separator
			// so resolveUsingForRef can surface it on Edge.DispatchKind.
			target := libName
			if path, has := v.namespacePaths[entry.leading]; has {
				target = libName + "||" + path
			} else if path, has := v.importPaths[entry.leading]; has {
				target = libName + "||" + path
			}
			if methodName := leafSegment(entry.tail); methodName != "" && methodName != libName {
				target = target + "\x1e" + methodName
			}
			for _, srcID := range containerIDs {
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
}

// leafSegment returns the last dot-segment of a dotted path. For
// `SubMath.addOne` it returns `addOne`; for bare `mul` it returns
// `mul`; for empty input it returns `""`. Used by the file-level
// operator-form walker (W6 V8) to extract the method name when
// the entry was a 3+ segment path like `M.SubMath.addOne`.
func leafSegment(path string) string {
	if path == "" {
		return ""
	}
	if idx := strings.LastIndex(path, "."); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

// resolveUsingBindingLeading normalises a using-for binding entry's
// leading identifier through importAliases / namespaceAliases. Three
// cases:
//
//   - leading is a namespace alias (`import "..." as M`) — the
//     intent is to reference a symbol in that module. The tail is
//     the actual function name; return the tail as libName.
//
//   - leading is an import alias (`import {Orig as Alias}`) — the
//     intent is to reference Orig under the alias. Return Orig.
//
//   - bare identifier — the intent is the library or free function
//     of that name. Return leading unchanged.
//
// Returns ok=false when the leading is a namespace alias with no
// tail (the entry was bare `M`, which can't bind to anything).
func (v *declVisitor) resolveUsingBindingLeading(e usingBindingEntry) (string, bool) {
	if v.namespaceAliases[e.leading] {
		if e.tail == "" {
			return "", false
		}
		// V0 only follows the immediate sub-name; deeper paths
		// (`M.SubMod.fn`) fall back to the first segment after the
		// namespace, matching the existing extractTypeNameText
		// "trailing identifier" convention.
		if dotIdx := strings.Index(e.tail, "."); dotIdx >= 0 {
			return e.tail[:dotIdx], true
		}
		return e.tail, true
	}
	if orig, hit := v.importAliases[e.leading]; hit {
		return orig, true
	}
	return e.leading, true
}

// usingBindingEntry captures one entry of a using-for directive's
// braced body. `leading` is the first dot-separated segment (the
// candidate library / namespace name); `tail` is the rest of the
// dotted path (`""` when the entry was a bare identifier, otherwise
// the method or sub-name following the first dot). The walker uses
// these together to route through importAliases / namespaceAliases
// before committing to a libName for the binding emit.
type usingBindingEntry struct {
	leading string
	tail    string
}

// parseFileLevelOperatorForm extracts (entries, bound type) from
// the raw ERROR text of a misparsed file-level operator-form using
// directive. Each entry preserves both the leading identifier (the
// candidate library / namespace) and the dotted tail (the method
// name when the entry was qualified like `M.mul`), so the caller
// can distinguish library-method form from namespace-aliased free-
// function form. Returns ok=false if any structural marker is
// missing.
func parseFileLevelOperatorForm(text string) ([]usingBindingEntry, string, bool) {
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
	var entries []usingBindingEntry
	seen := map[string]bool{}
	for _, raw := range strings.Split(body, ",") {
		entry := strings.TrimSpace(raw)
		if asIdx := strings.Index(entry, " as "); asIdx >= 0 {
			entry = strings.TrimSpace(entry[:asIdx])
		}
		leading := entry
		tail := ""
		if dotIdx := strings.Index(entry, "."); dotIdx >= 0 {
			leading = entry[:dotIdx]
			tail = entry[dotIdx+1:]
		}
		if leading == "" || seen[entry] {
			continue
		}
		seen[entry] = true
		entries = append(entries, usingBindingEntry{leading: leading, tail: tail})
	}
	if len(entries) == 0 {
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
	return entries, typeName, true
}
