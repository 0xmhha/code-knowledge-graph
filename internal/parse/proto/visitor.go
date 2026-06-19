package proto

import (
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// visitor drives the recursive-descent parse. One visitor per file. Mirrors
// the declVisitor shape used by the Solidity / TypeScript parsers.
//
// proto3 grammar productions (cross-checked against
// github.com/coder3101/tree-sitter-proto/blob/main/grammar.js):
//
//	proto_file       = syntax_stmt? package_stmt? top_level_def*
//	top_level_def    = service | message | enum | import | option | extend
//	service          = "service" ident "{" service_body* "}"
//	service_body     = rpc | option | reserved | empty_stmt
//	rpc              = "rpc" ident "(" "stream"? type_ref ")"
//	                   "returns" "(" "stream"? type_ref ")"
//	                   ( "{" rpc_body* "}" | ";" )
//	message          = "message" ident "{" message_body* "}"
//	message_body     = field | message | enum | oneof | map_field
//	                 | reserved | option | extensions | empty_stmt
//	field            = ("repeated"|"optional"|"required")? type_ref ident
//	                   "=" number field_options? ";"
//	map_field        = "map" "<" key_type "," value_type ">" ident
//	                   "=" number field_options? ";"
//	oneof            = "oneof" ident "{" oneof_field* "}"
//	enum             = "enum" ident "{" enum_value* "}"
//
// proto2 vs proto3: proto2 allows `required` / `optional` / extensions, which
// we accept-and-ignore (the field still surfaces as a Field node — proto2
// support is best-effort per W3a scope decision).
//
// File layout: this file holds the visitor type, token cursor helpers,
// recovery, top-level dispatch, and the trivial preamble productions
// (syntax/package/import). The grammar-heavy productions live in decls.go to
// keep each file under the coding-style soft cap.
type visitor struct {
	rel    string
	src    []byte
	tokens []token
	pos    int

	pkg     string // current `package` (empty before parse, set by parsePackage)
	fileID  string
	pkgID   string // empty until parsePackage emits it
	nodes   []types.Node
	edges   []types.Edge
	pending []parse.PendingRef
}

func newVisitor(rel string, src []byte) *visitor {
	v := &visitor{rel: rel, src: src}
	// Eagerly tokenize so the parser can peek/unget cheaply.
	lex := newLexer(src)
	for {
		t := lex.next()
		v.tokens = append(v.tokens, t)
		if t.Kind == tkEOF {
			break
		}
	}
	// File node — always emitted, even if the body is empty/invalid.
	fileQ := "file:" + rel
	v.fileID = parse.MakeID(fileQ, languageTag, 0)
	v.nodes = append(v.nodes, types.Node{
		ID: v.fileID, Type: types.NodeFile, Name: rel, QualifiedName: fileQ,
		FilePath: rel, StartLine: 1, EndLine: 1,
		Language: languageTag, Confidence: types.ConfExtracted,
	})
	return v
}

func (v *visitor) parse() {
	for v.peek().Kind != tkEOF {
		t := v.peek()
		if t.Kind != tkIdent {
			// Stray punctuation at top level — skip until next ; or }.
			v.skipToTopLevel()
			continue
		}
		switch t.Value {
		case "syntax":
			v.parseSyntax()
		case "package":
			v.parsePackage()
		case "import":
			v.parseImport()
		case "option":
			v.skipOptionStmt()
		case "service":
			v.parseService()
		case "message":
			v.parseMessage("")
		case "enum":
			v.parseEnum("")
		case "extend":
			// proto2 extension blocks — accept but don't emit. Skip the
			// block as a whole to avoid spurious top-level recovery.
			v.skipBlock()
		default:
			// Unknown top-level token — skip to next decl boundary.
			v.skipToTopLevel()
		}
	}
	// canonical id (ADR-0001): proto has no import path, so the relative file
	// path is the qualifier — <relpath>:<qualified_name>. Applied once here so
	// every emit site (service/rpc/message/field/enum) gets it uniformly. File
	// and import nodes are not symbols and are skipped.
	for i := range v.nodes {
		n := &v.nodes[i]
		if n.CanonicalID != "" || n.Type == types.NodeFile || n.Type == types.NodeImport {
			continue
		}
		// proto qnames already carry a "proto:" prefix; strip it (B4) so the
		// canonical id reads <relpath>:<pkg>.<Msg> rather than the doubled
		// <relpath>:proto:<pkg>.<Msg>.
		n.CanonicalID = v.rel + ":" + strings.TrimPrefix(n.QualifiedName, "proto:")
	}
}

// ── token cursor helpers ──────────────────────────────────────────────────

func (v *visitor) peek() token {
	return v.tokens[v.pos]
}

func (v *visitor) consume() token {
	t := v.tokens[v.pos]
	if t.Kind != tkEOF {
		v.pos++
	}
	return t
}

// acceptPunct consumes a punctuation token matching s; returns true if matched.
func (v *visitor) acceptPunct(s string) bool {
	t := v.peek()
	if t.Kind == tkPunct && t.Value == s {
		v.consume()
		return true
	}
	return false
}

// expectPunct is acceptPunct with the contract that the caller will recover
// (usually via skipToTopLevel / skipUntilSemi) when it returns false.
func (v *visitor) expectPunct(s string) bool {
	return v.acceptPunct(s)
}

// acceptIdent consumes ident if it matches name; returns true if matched.
func (v *visitor) acceptIdent(name string) bool {
	t := v.peek()
	if t.Kind == tkIdent && t.Value == name {
		v.consume()
		return true
	}
	return false
}

// ── recovery ──────────────────────────────────────────────────────────────

// skipToTopLevel advances tokens until we land on the start of a new
// top-level production (service/message/enum/syntax/package/import/option/
// extend) or EOF. Used after a parse failure inside a top-level def.
func (v *visitor) skipToTopLevel() {
	depth := 0
	for v.peek().Kind != tkEOF {
		t := v.peek()
		if t.Kind == tkPunct && t.Value == "{" {
			depth++
			v.consume()
			continue
		}
		if t.Kind == tkPunct && t.Value == "}" {
			if depth > 0 {
				depth--
			}
			v.consume()
			continue
		}
		if depth == 0 && t.Kind == tkIdent {
			switch t.Value {
			case "syntax", "package", "import", "option", "service",
				"message", "enum", "extend":
				return
			}
		}
		v.consume()
	}
}

// skipBlock consumes the current token (expected to be an ident) and the
// brace-delimited body that follows, balancing nested braces. Used to no-op
// past `extend` and other unrecognised but bracketed top-level blocks.
func (v *visitor) skipBlock() {
	v.consume() // the keyword
	// Skip until first `{` or `;`.
	for v.peek().Kind != tkEOF {
		t := v.peek()
		if t.Kind == tkPunct && t.Value == ";" {
			v.consume()
			return
		}
		if t.Kind == tkPunct && t.Value == "{" {
			break
		}
		v.consume()
	}
	depth := 0
	for v.peek().Kind != tkEOF {
		t := v.consume()
		if t.Kind == tkPunct && t.Value == "{" {
			depth++
			continue
		}
		if t.Kind == tkPunct && t.Value == "}" {
			depth--
			if depth == 0 {
				return
			}
		}
	}
}

// skipOptionStmt consumes `option name [= value]? ;`. The lexer treats `=`
// and `;` as punct tokens; we just walk until the semicolon.
func (v *visitor) skipOptionStmt() {
	for v.peek().Kind != tkEOF {
		t := v.consume()
		if t.Kind == tkPunct && t.Value == ";" {
			return
		}
		if t.Kind == tkPunct && t.Value == "{" {
			// `option (foo) = { nested = 1 };` — balance the braces.
			depth := 1
			for v.peek().Kind != tkEOF && depth > 0 {
				tt := v.consume()
				if tt.Kind == tkPunct && tt.Value == "{" {
					depth++
				} else if tt.Kind == tkPunct && tt.Value == "}" {
					depth--
				}
			}
		}
	}
}

// skipUntilSemi advances tokens until the next top-level `;` (depth 0). Used
// after a malformed rpc/field declaration to resume parsing safely.
func (v *visitor) skipUntilSemi() {
	depth := 0
	for v.peek().Kind != tkEOF {
		t := v.consume()
		if t.Kind == tkPunct && t.Value == "{" {
			depth++
			continue
		}
		if t.Kind == tkPunct && t.Value == "}" {
			if depth == 0 {
				return
			}
			depth--
			continue
		}
		if t.Kind == tkPunct && t.Value == ";" && depth == 0 {
			return
		}
	}
}

// ── preamble productions ──────────────────────────────────────────────────

// parseSyntax consumes `syntax = "proto3" ;` (or proto2). We only verify the
// shape — the proto2/3 distinction is purely informational at this layer.
func (v *visitor) parseSyntax() {
	v.consume() // 'syntax'
	v.acceptPunct("=")
	if v.peek().Kind == tkString {
		v.consume()
	}
	v.acceptPunct(";")
}

// parsePackage consumes `package a.b.c ;` and emits the Package node.
// The package name is recorded on v so subsequent decls can qualify
// their identifiers correctly.
func (v *visitor) parsePackage() {
	pkgTok := v.consume() // 'package'
	parts := []string{}
	startByte := pkgTok.StartByte
	endByte := pkgTok.EndByte
	startLine := pkgTok.Line
	for v.peek().Kind == tkIdent || (v.peek().Kind == tkPunct && v.peek().Value == ".") {
		t := v.consume()
		endByte = t.EndByte
		if t.Kind == tkIdent {
			parts = append(parts, t.Value)
		}
	}
	v.acceptPunct(";")
	if len(parts) == 0 {
		return
	}
	v.pkg = strings.Join(parts, ".")
	qname := "proto:" + v.pkg
	v.pkgID = parse.MakeID(qname, languageTag, startByte)
	v.nodes = append(v.nodes, types.Node{
		ID: v.pkgID, Type: types.NodePackage,
		Name:          v.pkg,
		QualifiedName: qname,
		FilePath:      v.rel,
		StartLine:     startLine, EndLine: startLine,
		StartByte: startByte, EndByte: endByte,
		Language: languageTag, Confidence: types.ConfExtracted,
	})
	// File → Package via `contains` (mirrors Go: file lives in package).
	v.edges = append(v.edges, types.Edge{
		Src: v.pkgID, Dst: v.fileID, Type: types.EdgeContains,
		Count: 1, Confidence: types.ConfExtracted,
	})
}

// parseImport consumes `import [public|weak]? "path" ;`. No nodes emitted —
// `.proto` imports are file-level and the cross-language linker resolves
// type refs by qualified-name regardless of import order. We could emit
// Import nodes for parity with TS, but that would require a NodeType
// addition; deferred to a follow-up.
func (v *visitor) parseImport() {
	v.consume() // 'import'
	// optional public/weak
	if t := v.peek(); t.Kind == tkIdent && (t.Value == "public" || t.Value == "weak") {
		v.consume()
	}
	if v.peek().Kind == tkString {
		v.consume()
	}
	v.acceptPunct(";")
}

// qualify returns name prefixed with the file's package, joined by ".".
// Returns the bare name when no package is declared (proto2 default).
func (v *visitor) qualify(name string) string {
	if v.pkg == "" {
		return name
	}
	return v.pkg + "." + name
}

// candidateForType normalises a type reference into the qualified name we
// expect the corresponding MessageType node to carry. Absolute refs
// (".pkg.Foo") drop the leading dot. Bare refs ("Foo") get the current
// package prefixed. The result is wrapped with the "proto:" qname prefix
// so Resolve can do an O(1) match against byQName.
func (v *visitor) candidateForType(t string) string {
	if t == "" {
		return ""
	}
	if strings.HasPrefix(t, ".") {
		return "proto:" + t[1:]
	}
	// If the type already contains a package-style dot, use it as-is.
	if strings.Contains(t, ".") {
		return "proto:" + t
	}
	if v.pkg != "" {
		return "proto:" + v.pkg + "." + t
	}
	return "proto:" + t
}
