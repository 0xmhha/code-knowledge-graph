// Package proto implements the CKG parser for `.proto` files (schema 1.9 W3a).
//
// Design (schema-1.9-spec §6.4): dependency-free hand-rolled lexer + recursive-
// descent parser. The proto3 grammar is small enough that pulling in tree-sitter
// is overkill, but we cross-checked our token set + production list against the
// community grammar at github.com/coder3101/tree-sitter-proto/blob/main/grammar.js
// so production names and token boundaries align with mainstream tooling.
//
// Scope (W3a): emit nodes/edges for service/rpc/message/enum/field/package.
// gRPC server/client detection in Go/TS lives in W3b/W3c — out of scope here.
// The parser produces parse.ParseResult exactly like every other language
// parser; the cross-language linker can later wire up rpc_calls/listens_on
// against these proto nodes by qualified-name suffix match.
package proto

import (
	"unicode"
	"unicode/utf8"
)

// tokenKind enumerates lexer outputs. We only carve out the tokens the
// recursive-descent parser actually inspects — anything else (e.g. `[a=b]`
// field options, full option statement payloads) is consumed by tokens
// without ever leaving the lexer.
type tokenKind int

const (
	tkEOF tokenKind = iota
	tkIdent
	tkNumber
	tkString
	tkPunct
)

// token is a single lexeme. Value is the raw source slice (sans quotes for
// strings). StartByte/EndByte are absolute offsets into the original source
// so node IDs (parse.MakeID) carry stable positional information.
type token struct {
	Kind      tokenKind
	Value     string
	StartByte int
	EndByte   int
	Line      int // 1-based, of the first byte of the token
}

// lexer is a single-pass scanner. Errors are intentionally absent: malformed
// input degrades to an unknown punctuation token (returned verbatim) which
// the parser then surfaces via the recovery path. This matches the
// "best-effort INFERRED" handling spec §6.4 calls for on edge-case syntax.
type lexer struct {
	src    []byte
	offset int // byte cursor
	line   int // 1-based
}

func newLexer(src []byte) *lexer {
	return &lexer{src: src, line: 1}
}

// next returns the next token. Comments and whitespace are skipped.
// Returns tkEOF once the source is exhausted.
func (l *lexer) next() token {
	l.skipWhitespaceAndComments()
	if l.offset >= len(l.src) {
		return token{Kind: tkEOF, StartByte: l.offset, EndByte: l.offset, Line: l.line}
	}
	start := l.offset
	startLine := l.line
	ch, size := utf8.DecodeRune(l.src[l.offset:])

	// String literal: '"' or "'" delimited. Proto3 spec only specifies
	// double-quoted strings, but real-world files mix both — accept either.
	if ch == '"' || ch == '\'' {
		quote := ch
		l.offset += size
		for l.offset < len(l.src) {
			c, sz := utf8.DecodeRune(l.src[l.offset:])
			if c == '\\' && l.offset+1 < len(l.src) {
				// Skip the escape and the next byte (best-effort — we
				// don't care about decoded value, only token boundary).
				l.offset += sz
				_, sz2 := utf8.DecodeRune(l.src[l.offset:])
				l.offset += sz2
				continue
			}
			if c == quote {
				l.offset += sz
				return token{
					Kind:      tkString,
					Value:     string(l.src[start+1 : l.offset-1]),
					StartByte: start, EndByte: l.offset, Line: startLine,
				}
			}
			if c == '\n' {
				l.line++
			}
			l.offset += sz
		}
		// Unterminated string — return what we have so the parser can
		// abort gracefully on a bad file.
		return token{
			Kind:      tkString,
			Value:     string(l.src[start+1:]),
			StartByte: start, EndByte: l.offset, Line: startLine,
		}
	}

	// Number: leading digit, optionally with decimal point or hex prefix.
	// Proto3 uses field numbers (integers) and option values (numbers); we
	// don't need to decode the value, just consume the run.
	if ch >= '0' && ch <= '9' {
		for l.offset < len(l.src) {
			c, sz := utf8.DecodeRune(l.src[l.offset:])
			if c != '.' && c != 'x' && c != 'X' && c != '-' && c != '+' &&
				(c < '0' || c > '9') &&
				(c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				break
			}
			l.offset += sz
		}
		return token{
			Kind:      tkNumber,
			Value:     string(l.src[start:l.offset]),
			StartByte: start, EndByte: l.offset, Line: startLine,
		}
	}

	// Identifier / keyword: leading letter or underscore.
	if isIdentStart(ch) {
		for l.offset < len(l.src) {
			c, sz := utf8.DecodeRune(l.src[l.offset:])
			if !isIdentCont(c) {
				break
			}
			l.offset += sz
		}
		return token{
			Kind:      tkIdent,
			Value:     string(l.src[start:l.offset]),
			StartByte: start, EndByte: l.offset, Line: startLine,
		}
	}

	// Punctuation / single-byte symbol. proto3 has no multi-byte operators
	// inside declarations the parser cares about (the lexer treats `.` as a
	// distinct punct so the parser can build fully-qualified type refs by
	// joining `pkg . sub . Type`).
	l.offset += size
	return token{
		Kind:      tkPunct,
		Value:     string(l.src[start:l.offset]),
		StartByte: start, EndByte: l.offset, Line: startLine,
	}
}

// skipWhitespaceAndComments advances past spaces, tabs, newlines, and both
// `// line` + `/* block */` comments. Mirrors tree-sitter-proto's extras
// rules (`comment`, `_whitespace`).
func (l *lexer) skipWhitespaceAndComments() {
	for l.offset < len(l.src) {
		c, sz := utf8.DecodeRune(l.src[l.offset:])
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			l.offset += sz
		case c == '\n':
			l.line++
			l.offset += sz
		case c == '/' && l.offset+1 < len(l.src) && l.src[l.offset+1] == '/':
			// line comment until newline
			l.offset += 2
			for l.offset < len(l.src) && l.src[l.offset] != '\n' {
				l.offset++
			}
		case c == '/' && l.offset+1 < len(l.src) && l.src[l.offset+1] == '*':
			// block comment until '*/'
			l.offset += 2
			for l.offset+1 < len(l.src) {
				if l.src[l.offset] == '*' && l.src[l.offset+1] == '/' {
					l.offset += 2
					break
				}
				if l.src[l.offset] == '\n' {
					l.line++
				}
				l.offset++
			}
		default:
			return
		}
	}
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentCont(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
