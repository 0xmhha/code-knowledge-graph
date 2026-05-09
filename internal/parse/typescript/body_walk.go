// Package typescript — body_walk.go implements P3 of the TS parser:
// statement-level call extraction inside function/method bodies.
//
// Before P3 the TS parser only emitted file-level declarations (Class /
// Interface / Function / Method / Decorator / TypeAlias / Enum / Import).
// Function bodies were unparsed, so cross-symbol call edges were absent
// from the TS portion of the graph — the 2026-05-09 graphify-comparison
// audit flagged this as the largest accuracy gap on the TS axis.
//
// V0 scope: emit `calls` Pending refs anchored on the smallest enclosing
// Function/Method node for each call_expression. Pass-2 Resolve unions
// these by callee Name (same idiom Go's pending_refs queue uses pre-1.7).
//
// Out of scope (deferred):
//   - statement-level node emission (CallSite / IfStmt / LoopStmt /
//     ReturnStmt / SwitchStmt) — Go has these via tree-sitter walks too;
//     a follow-up TS pass can mirror that without changing the resolution
//     contract here.
//   - type-aware dispatch classification (invokes + dispatch_kind) — would
//     require a TS LSP server embedded in CKG. Track C did this for Go via
//     go/packages.Load; TS has no equivalent in-process surface today.
//   - field reads/writes — captured by a separate `member_expression`
//     query if the audit shows demand.
package typescript

import (
	"sort"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// (Note: the original runBodyCalls function lived here in earlier
// schema-1.8 work — it walked call_expression nodes and emitted
// PendingRefs anchored on the enclosing Function/Method. It has been
// replaced by runBodyStatements in statements.go, which performs the
// same call walk plus four other statement kinds (IfStmt / LoopStmt /
// SwitchStmt / ReturnStmt) and re-anchors PendingRefs on a fresh
// CallSite node, mirroring the canonical Go-parser pattern. The helpers
// below — fnInterval, collectFnIntervalsFromTree, visitTreeForFnDecls,
// findEnclosingFn, fnKey, itoa — are still used by statements.go and
// kept here untouched.)

// fnInterval is one Function/Method's byte range + ID + start line.
// Sorted (by start asc, end desc) so a linear scan from the start can
// terminate early when the call's position falls before the next
// interval's start.
type fnInterval struct {
	start, end int
	fnID       string
}

// collectFnIntervalsFromTree re-walks the parse tree to find the FULL
// byte spans of every function_declaration / method_definition /
// arrow_function and matches them back to the Function/Method nodes
// already in v.nodes by (name, start-line) tuple.
//
// Why we can't reuse v.nodes' StartByte/EndByte directly: the
// declarations.go runQuery() stores the IDENTIFIER's byte range (just
// the function name), not the full declaration. A call_expression
// inside the function body would never overlap that range. The fix
// is local to body walk — we'd rather not alter declarations.go's
// existing semantics (the identifier range feeds blob extraction
// elsewhere) just to support this pass.
//
// Ambiguity: if two top-level functions share a name (legal in JS at
// runtime via var hoisting; legal in TS only for overload signatures),
// the first match wins. The cost is one missed pending ref per
// duplicate name — acceptable until an audit shows the V0 resolution
// path needs disambiguation here.
func collectFnIntervalsFromTree(v *declVisitor) []fnInterval {
	// (name, startLine) → node ID, for the Function/Method nodes the
	// declarations pass already emitted.
	idByKey := map[string]string{}
	for _, n := range v.nodes {
		if n.Type != types.NodeFunction && n.Type != types.NodeMethod {
			continue
		}
		idByKey[fnKey(n.Name, n.StartLine)] = n.ID
	}
	if len(idByKey) == 0 {
		return nil
	}
	out := make([]fnInterval, 0, len(idByKey))
	visitTreeForFnDecls(v, v.root, &out, idByKey)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].start != out[j].start {
			return out[i].start < out[j].start
		}
		return out[i].end > out[j].end
	})
	return out
}

// visitTreeForFnDecls recurses through the parse tree, matching every
// function_declaration / method_definition node against the (name,
// line) → ID map and emitting an interval per match.
func visitTreeForFnDecls(v *declVisitor, n *sitter.Node, out *[]fnInterval, idByKey map[string]string) {
	if n == nil {
		return
	}
	kind := n.Kind()
	if kind == "function_declaration" || kind == "method_definition" {
		var nameNode *sitter.Node
		switch kind {
		case "function_declaration":
			nameNode = n.ChildByFieldName("name")
		case "method_definition":
			nameNode = n.ChildByFieldName("name")
		}
		if nameNode != nil {
			name := nameNode.Utf8Text(v.src)
			line := int(nameNode.StartPosition().Row) + 1
			if id, ok := idByKey[fnKey(name, line)]; ok {
				*out = append(*out, fnInterval{
					start: int(n.StartByte()),
					end:   int(n.EndByte()),
					fnID:  id,
				})
			}
		}
	}
	count := int(n.ChildCount())
	for i := 0; i < count; i++ {
		visitTreeForFnDecls(v, n.Child(uint(i)), out, idByKey)
	}
}

// fnKey is the (name, line) join key used to map tree-sitter
// function_declaration / method_definition nodes back to the
// declarations pass's node IDs.
func fnKey(name string, line int) string {
	return name + "@" + itoa(line)
}

// findEnclosingFn returns the ID of the smallest interval that contains
// pos. Walks the sorted interval list and tracks the tightest match.
// O(N) per call; for files with <50 functions and <500 calls this is
// well under the noise floor compared to tree-sitter parse time. If a
// future audit shows a hot-path file with thousands of functions, swap
// for an interval tree.
func findEnclosingFn(intervals []fnInterval, pos int) (string, bool) {
	var bestID string
	bestSize := -1
	for _, iv := range intervals {
		if iv.start > pos {
			break // sorted asc by start; nothing later can contain pos
		}
		if pos >= iv.end {
			continue
		}
		size := iv.end - iv.start
		if bestID == "" || size < bestSize {
			bestID = iv.fnID
			bestSize = size
		}
	}
	return bestID, bestID != ""
}

// itoa is a small helper to avoid importing strconv just for an int→
// string conversion in the dedup key. Intentionally minimal — no
// negative-number handling because line numbers are always positive.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
