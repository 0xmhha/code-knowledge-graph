package proto

// decls.go — recursive-descent productions for the grammar-heavy parts of a
// `.proto` file: service / rpc / message / field / oneof / map / enum and the
// shared parseTypeRef + signatureForRPC helpers. Split out of visitor.go to
// keep each file under the soft size cap; both files share the *visitor
// receiver and the production graph is recursive across the split.

import (
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// parseService emits an Interface node + Method nodes per rpc.
func (v *visitor) parseService() {
	svcTok := v.consume() // 'service'
	if v.peek().Kind != tkIdent {
		v.skipToTopLevel()
		return
	}
	nameTok := v.consume()
	if !v.expectPunct("{") {
		v.skipToTopLevel()
		return
	}
	qname := "proto:" + v.qualify(nameTok.Value)
	svcID := parse.MakeID(qname, languageTag, nameTok.StartByte)
	v.nodes = append(v.nodes, types.Node{
		ID: svcID, Type: types.NodeInterface,
		Name:          nameTok.Value,
		QualifiedName: qname,
		FilePath:      v.rel,
		StartLine:     svcTok.Line, EndLine: svcTok.Line,
		StartByte: nameTok.StartByte, EndByte: nameTok.EndByte,
		Language: languageTag, Confidence: types.ConfExtracted,
		SubKind: "service",
	})
	// File → Service via `defines`.
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: svcID, Type: types.EdgeDefines,
		Count: 1, Confidence: types.ConfExtracted, FilePath: v.rel,
	})
	for v.peek().Kind != tkEOF {
		t := v.peek()
		if t.Kind == tkPunct && t.Value == "}" {
			v.consume()
			return
		}
		if t.Kind == tkIdent {
			switch t.Value {
			case "rpc":
				v.parseRPC(svcID, nameTok.Value)
				continue
			case "option":
				v.skipOptionStmt()
				continue
			case "reserved":
				v.skipUntilSemi()
				continue
			}
		}
		if t.Kind == tkPunct && t.Value == ";" {
			v.consume()
			continue
		}
		// Unknown — skip one token to make progress.
		v.consume()
	}
}

// parseRPC parses one `rpc Foo(Req) returns (Resp) [ { ... } | ; ]` line and
// emits a Method node + defines + uses_type pending refs.
func (v *visitor) parseRPC(svcID, svcName string) {
	rpcTok := v.consume() // 'rpc'
	if v.peek().Kind != tkIdent {
		v.skipUntilSemi()
		return
	}
	nameTok := v.consume()
	if !v.expectPunct("(") {
		v.skipUntilSemi()
		return
	}
	reqStream := v.acceptIdent("stream")
	reqType := v.parseTypeRef()
	if !v.expectPunct(")") {
		v.skipUntilSemi()
		return
	}
	if !v.acceptIdent("returns") {
		v.skipUntilSemi()
		return
	}
	if !v.expectPunct("(") {
		v.skipUntilSemi()
		return
	}
	respStream := v.acceptIdent("stream")
	respType := v.parseTypeRef()
	if !v.expectPunct(")") {
		v.skipUntilSemi()
		return
	}
	// Body: either `{ ... }` (option statements) or `;`.
	if v.acceptPunct("{") {
		depth := 1
		for v.peek().Kind != tkEOF && depth > 0 {
			t := v.consume()
			if t.Kind == tkPunct && t.Value == "{" {
				depth++
			} else if t.Kind == tkPunct && t.Value == "}" {
				depth--
			}
		}
	} else {
		v.acceptPunct(";")
	}

	qname := "proto:" + v.qualify(svcName+"."+nameTok.Value)
	id := parse.MakeID(qname, languageTag, nameTok.StartByte)
	sig := signatureForRPC(reqType, reqStream, respType, respStream)
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: types.NodeMethod,
		Name:          nameTok.Value,
		QualifiedName: qname,
		FilePath:      v.rel,
		StartLine:     rpcTok.Line, EndLine: rpcTok.Line,
		StartByte: nameTok.StartByte, EndByte: nameTok.EndByte,
		Language: languageTag, Confidence: types.ConfExtracted,
		Signature: sig,
		SubKind:   "rpc",
	})
	// Service → Method (defines).
	v.edges = append(v.edges, types.Edge{
		Src: svcID, Dst: id, Type: types.EdgeDefines,
		Count: 1, Confidence: types.ConfExtracted, FilePath: v.rel, Line: rpcTok.Line,
	})
	// Pending uses_type refs — Resolve will match against MessageType nodes
	// in this file OR cross-file (cross-file -> INFERRED).
	if reqType != "" {
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:       id,
			EdgeType:    types.EdgeUsesType,
			TargetQName: v.candidateForType(reqType),
			Line:        rpcTok.Line,
		})
	}
	if respType != "" && respType != reqType {
		v.pending = append(v.pending, parse.PendingRef{
			SrcID:       id,
			EdgeType:    types.EdgeUsesType,
			TargetQName: v.candidateForType(respType),
			Line:        rpcTok.Line,
		})
	}
}

// parseMessage parses `message Name { body }` and emits a MessageType node.
// parentQual is the dotted prefix for nested messages ("" at top level,
// "Outer" inside an Outer's body, "Outer.Inner" two levels deep, etc.).
// Recurses into nested message/enum/oneof bodies.
func (v *visitor) parseMessage(parentQual string) {
	msgTok := v.consume() // 'message'
	if v.peek().Kind != tkIdent {
		v.skipToTopLevel()
		return
	}
	nameTok := v.consume()
	if !v.expectPunct("{") {
		v.skipToTopLevel()
		return
	}
	localName := nameTok.Value
	dotted := localName
	if parentQual != "" {
		dotted = parentQual + "." + localName
	}
	qname := "proto:" + v.qualify(dotted)
	msgID := parse.MakeID(qname, languageTag, nameTok.StartByte)
	v.nodes = append(v.nodes, types.Node{
		ID: msgID, Type: types.NodeMessageType,
		Name:          localName,
		QualifiedName: qname,
		FilePath:      v.rel,
		StartLine:     msgTok.Line, EndLine: msgTok.Line,
		StartByte: nameTok.StartByte, EndByte: nameTok.EndByte,
		Language: languageTag, Confidence: types.ConfExtracted,
		SubKind: "message",
	})
	// Parent → Message via `defines`. For top-level messages, parent is the
	// File node; for nested messages, the enclosing Message node id is
	// looked up via the dotted name — we don't have direct access here, so
	// we re-derive the parent qname.
	parentID := v.fileID
	if parentQual != "" {
		parentQ := "proto:" + v.qualify(parentQual)
		// Find the parent node we just emitted by qname (linear scan over
		// nodes — small N per file).
		for _, n := range v.nodes {
			if n.QualifiedName == parentQ && n.Type == types.NodeMessageType {
				parentID = n.ID
				break
			}
		}
	}
	v.edges = append(v.edges, types.Edge{
		Src: parentID, Dst: msgID, Type: types.EdgeDefines,
		Count: 1, Confidence: types.ConfExtracted, FilePath: v.rel, Line: msgTok.Line,
	})

	for v.peek().Kind != tkEOF {
		t := v.peek()
		if t.Kind == tkPunct && t.Value == "}" {
			v.consume()
			return
		}
		if t.Kind == tkIdent {
			switch t.Value {
			case "message":
				v.parseMessage(dotted)
				continue
			case "enum":
				v.parseEnum(dotted)
				continue
			case "option":
				v.skipOptionStmt()
				continue
			case "reserved", "extensions":
				v.skipUntilSemi()
				continue
			case "oneof":
				v.parseOneof(msgID, dotted)
				continue
			case "map":
				v.parseMapField(msgID, dotted)
				continue
			case "group":
				// proto2 `group MyGroup = N { ... }` — best-effort skip.
				// Reviewer (W3a Important #2) caught that without this branch
				// parseField consumes the `group` token as a type ref, the
				// group name as the field name, then bails on the missing `;`
				// — but the trailing `{ body }` then leaks into the enclosing
				// message body parse, producing noisy garbage fields. proto3
				// dropped group so this is purely proto2 protection.
				// skipBlock() handles: consume the `group` keyword, advance
				// to the opening `{`, then balance-skip the body.
				v.skipBlock()
				continue
			}
		}
		if t.Kind == tkPunct && t.Value == ";" {
			v.consume()
			continue
		}
		// Otherwise, treat as a normal field declaration.
		if !v.parseField(msgID, dotted) {
			// Field parse failed — advance one token so we don't loop.
			v.consume()
		}
	}
}

// parseField parses one `[repeated|optional|required]? type name = number;`
// declaration and emits a Field node + defines edge. Returns true if a field
// was emitted (or syntactically resembled one); false if the input didn't
// look like a field at all (caller skips the token).
func (v *visitor) parseField(msgID, parentQual string) bool {
	start := v.pos
	// Optional label.
	// Preserve label so downstream consumers (viewer / link / W3b grpc
	// matching) can distinguish `repeated string tags = 2` from a plain
	// scalar — reviewer (W3a Important #3) caught that the prior
	// implementation discarded the label, leaving signatures identical for
	// `string tags = 2` vs `repeated string tags = 2`.
	label := ""
	if t := v.peek(); t.Kind == tkIdent &&
		(t.Value == "repeated" || t.Value == "optional" || t.Value == "required") {
		label = t.Value
		v.consume()
	}
	// proto2 group inside a field position — `[label] group Name = N { body }`.
	// The message body switch already handles label-less `group`, but proto2
	// commonly attaches a label, so we re-check here after label consume to
	// avoid the body tokens leaking into the enclosing message parse.
	// Reviewer (W3a Important #2) caught the desync on labelled groups.
	// skipBlock() consumes the `group` keyword + balance-skips the body.
	if t := v.peek(); t.Kind == tkIdent && t.Value == "group" {
		v.skipBlock()
		return true
	}
	// Type ref.
	typeRef := v.parseTypeRef()
	if typeRef == "" {
		v.pos = start
		return false
	}
	if v.peek().Kind != tkIdent {
		v.pos = start
		return false
	}
	nameTok := v.consume()
	if !v.expectPunct("=") {
		v.skipUntilSemi()
		return true
	}
	numberStr := ""
	if t := v.peek(); t.Kind == tkNumber {
		numberStr = t.Value
		v.consume()
	}
	// Skip optional field options `[ ... ]`.
	if v.acceptPunct("[") {
		depth := 1
		for v.peek().Kind != tkEOF && depth > 0 {
			t := v.consume()
			if t.Kind == tkPunct && t.Value == "[" {
				depth++
			} else if t.Kind == tkPunct && t.Value == "]" {
				depth--
			}
		}
	}
	v.acceptPunct(";")

	qname := "proto:" + v.qualify(parentQual+"."+nameTok.Value)
	id := parse.MakeID(qname, languageTag, nameTok.StartByte)
	sig := typeRef
	if label != "" {
		sig = label + " " + typeRef
	}
	if numberStr != "" {
		sig = sig + " " + numberStr
	}
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: types.NodeField,
		Name:          nameTok.Value,
		QualifiedName: qname,
		FilePath:      v.rel,
		StartLine:     nameTok.Line, EndLine: nameTok.Line,
		StartByte: nameTok.StartByte, EndByte: nameTok.EndByte,
		Language: languageTag, Confidence: types.ConfExtracted,
		Signature: sig,
		SubKind:   "field",
	})
	v.edges = append(v.edges, types.Edge{
		Src: msgID, Dst: id, Type: types.EdgeDefines,
		Count: 1, Confidence: types.ConfExtracted, FilePath: v.rel, Line: nameTok.Line,
	})
	return true
}

// parseOneof parses `oneof name { oneof_field* }`. Fields inside a oneof
// surface as Field nodes under a synthetic `oneof_<name>` parent segment so
// the resulting qualified names are unambiguous. Best-effort; oneof
// semantics beyond field enumeration are V0-out-of-scope.
func (v *visitor) parseOneof(msgID, parentQual string) {
	v.consume() // 'oneof'
	if v.peek().Kind != tkIdent {
		v.skipToTopLevel()
		return
	}
	oneofName := v.consume().Value
	if !v.expectPunct("{") {
		v.skipToTopLevel()
		return
	}
	for v.peek().Kind != tkEOF {
		t := v.peek()
		if t.Kind == tkPunct && t.Value == "}" {
			v.consume()
			return
		}
		if t.Kind == tkPunct && t.Value == ";" {
			v.consume()
			continue
		}
		if t.Kind == tkIdent && t.Value == "option" {
			v.skipOptionStmt()
			continue
		}
		if !v.parseField(msgID, parentQual+".oneof_"+oneofName) {
			v.consume()
		}
	}
}

// parseMapField parses `map<keyT, valT> name = number;`. Emits one Field
// node with the map type encoded in Signature.
func (v *visitor) parseMapField(msgID, parentQual string) {
	v.consume() // 'map'
	if !v.expectPunct("<") {
		v.skipUntilSemi()
		return
	}
	keyT := v.parseTypeRef()
	v.acceptPunct(",")
	valT := v.parseTypeRef()
	if !v.expectPunct(">") {
		v.skipUntilSemi()
		return
	}
	if v.peek().Kind != tkIdent {
		v.skipUntilSemi()
		return
	}
	nameTok := v.consume()
	v.acceptPunct("=")
	number := ""
	if t := v.peek(); t.Kind == tkNumber {
		number = t.Value
		v.consume()
	}
	if v.acceptPunct("[") {
		depth := 1
		for v.peek().Kind != tkEOF && depth > 0 {
			t := v.consume()
			if t.Kind == tkPunct && t.Value == "[" {
				depth++
			} else if t.Kind == tkPunct && t.Value == "]" {
				depth--
			}
		}
	}
	v.acceptPunct(";")

	qname := "proto:" + v.qualify(parentQual+"."+nameTok.Value)
	id := parse.MakeID(qname, languageTag, nameTok.StartByte)
	sig := "map<" + keyT + "," + valT + ">"
	if number != "" {
		sig = sig + " " + number
	}
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: types.NodeField,
		Name:          nameTok.Value,
		QualifiedName: qname,
		FilePath:      v.rel,
		StartLine:     nameTok.Line, EndLine: nameTok.Line,
		StartByte: nameTok.StartByte, EndByte: nameTok.EndByte,
		Language: languageTag, Confidence: types.ConfExtracted,
		Signature: sig,
		SubKind:   "field",
	})
	v.edges = append(v.edges, types.Edge{
		Src: msgID, Dst: id, Type: types.EdgeDefines,
		Count: 1, Confidence: types.ConfExtracted, FilePath: v.rel, Line: nameTok.Line,
	})
}

// parseEnum parses `enum Name { VALUE = number; ... }`. Enum values are
// emitted as Field nodes (NodeType reuse — there's no NodeEnumValue in the
// current schema, and Field is the closest semantic fit: a named constant
// belonging to a parent enumeration).
func (v *visitor) parseEnum(parentQual string) {
	enumTok := v.consume() // 'enum'
	if v.peek().Kind != tkIdent {
		v.skipToTopLevel()
		return
	}
	nameTok := v.consume()
	if !v.expectPunct("{") {
		v.skipToTopLevel()
		return
	}
	localName := nameTok.Value
	dotted := localName
	if parentQual != "" {
		dotted = parentQual + "." + localName
	}
	qname := "proto:" + v.qualify(dotted)
	enumID := parse.MakeID(qname, languageTag, nameTok.StartByte)
	v.nodes = append(v.nodes, types.Node{
		ID: enumID, Type: types.NodeEnum,
		Name:          localName,
		QualifiedName: qname,
		FilePath:      v.rel,
		StartLine:     enumTok.Line, EndLine: enumTok.Line,
		StartByte: nameTok.StartByte, EndByte: nameTok.EndByte,
		Language: languageTag, Confidence: types.ConfExtracted,
		SubKind: "enum",
	})
	parentID := v.fileID
	if parentQual != "" {
		parentQ := "proto:" + v.qualify(parentQual)
		for _, n := range v.nodes {
			if n.QualifiedName == parentQ && n.Type == types.NodeMessageType {
				parentID = n.ID
				break
			}
		}
	}
	v.edges = append(v.edges, types.Edge{
		Src: parentID, Dst: enumID, Type: types.EdgeDefines,
		Count: 1, Confidence: types.ConfExtracted, FilePath: v.rel, Line: enumTok.Line,
	})
	for v.peek().Kind != tkEOF {
		t := v.peek()
		if t.Kind == tkPunct && t.Value == "}" {
			v.consume()
			return
		}
		if t.Kind == tkIdent && t.Value == "option" {
			v.skipOptionStmt()
			continue
		}
		if t.Kind == tkIdent && t.Value == "reserved" {
			v.skipUntilSemi()
			continue
		}
		if t.Kind == tkPunct && t.Value == ";" {
			v.consume()
			continue
		}
		// enum_value = ident "=" number ";"
		if t.Kind != tkIdent {
			v.consume()
			continue
		}
		valTok := v.consume()
		if !v.expectPunct("=") {
			v.skipUntilSemi()
			continue
		}
		number := ""
		if num := v.peek(); num.Kind == tkNumber {
			number = num.Value
			v.consume()
		}
		// optional value options
		if v.acceptPunct("[") {
			depth := 1
			for v.peek().Kind != tkEOF && depth > 0 {
				tt := v.consume()
				if tt.Kind == tkPunct && tt.Value == "[" {
					depth++
				} else if tt.Kind == tkPunct && tt.Value == "]" {
					depth--
				}
			}
		}
		v.acceptPunct(";")
		valQ := "proto:" + v.qualify(dotted+"."+valTok.Value)
		valID := parse.MakeID(valQ, languageTag, valTok.StartByte)
		sig := number
		v.nodes = append(v.nodes, types.Node{
			ID: valID, Type: types.NodeField,
			Name:          valTok.Value,
			QualifiedName: valQ,
			FilePath:      v.rel,
			StartLine:     valTok.Line, EndLine: valTok.Line,
			StartByte: valTok.StartByte, EndByte: valTok.EndByte,
			Language: languageTag, Confidence: types.ConfExtracted,
			Signature: sig,
			SubKind:   "enum_value",
		})
		v.edges = append(v.edges, types.Edge{
			Src: enumID, Dst: valID, Type: types.EdgeDefines,
			Count: 1, Confidence: types.ConfExtracted, FilePath: v.rel, Line: valTok.Line,
		})
	}
}

// parseTypeRef reads a dotted type reference (e.g. `Foo`, `foo.Bar`,
// `.google.protobuf.Empty`) and returns the joined string. Returns "" if
// the next token isn't a type ref.
func (v *visitor) parseTypeRef() string {
	var parts []string
	// Leading dot allowed: ".pkg.Type" means absolute reference.
	leading := ""
	if v.peek().Kind == tkPunct && v.peek().Value == "." {
		v.consume()
		leading = "."
	}
	if v.peek().Kind != tkIdent {
		return ""
	}
	parts = append(parts, v.consume().Value)
	for v.peek().Kind == tkPunct && v.peek().Value == "." {
		v.consume()
		if v.peek().Kind != tkIdent {
			break
		}
		parts = append(parts, v.consume().Value)
	}
	return leading + strings.Join(parts, ".")
}

// signatureForRPC formats the rpc method's request/response signature in a
// stable shape consumed by viewers and string-search tooling. Mirrors the
// grpcurl convention.
func signatureForRPC(reqType string, reqStream bool, respType string, respStream bool) string {
	left := reqType
	if reqStream {
		left = "stream " + left
	}
	right := respType
	if respStream {
		right = "stream " + right
	}
	return "(" + left + ") returns (" + right + ")"
}
