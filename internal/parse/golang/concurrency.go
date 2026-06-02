package golang

import (
	"fmt"
	"go/ast"
	"go/token"
	gotypes "go/types"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// concurrency.go implements B1 Stage 1 (spec §2): emit Mutex nodes for
// sync.Mutex / sync.RWMutex declarations, acquires_lock / releases_lock
// edges for Lock/Unlock/RLock/RUnlock calls, and improve Channel
// emission with direction + buffer + elem-type attributes (in Signature).
//
// Stage 2 (SSA-based cross-function lock chain analysis) is D1's scope.
// accessed_under_lock detection is deferred to a B1 follow-up — the lexical
// scope analysis required for it is non-trivial and the value delivered by
// just Mutex nodes + lock edges (currently zero in the production DB) is
// already substantial.
//
// Receiver resolution: when typesInfo is non-nil, every detection uses
// types.Info.ObjectOf / types.Info.TypeOf to confirm receivers belong to
// sync.Mutex or sync.RWMutex (handles embedded mutexes, type aliases,
// pointer-vs-value distinction). When typesInfo is nil, falls back to
// name-based matching with INFERRED confidence; this is the AST-only
// path used by tests that call ParseFile directly without a *packages.Package.

// emitConcurrencyDecls is the per-file entry point for the B1 concurrency
// pass. Walks the file's top-level declarations to find sync.Mutex /
// sync.RWMutex fields and locals, emits Mutex nodes, and records
// var/field → node-ID mapping (consumed later by maybeEmitLockEdge).
//
// Idempotent: repeated invocation overwrites mutexNodeIDs, leaks no
// additional nodes (caller controls v.nodes lifetime).
func (v *declVisitor) emitConcurrencyDecls(f *ast.File) {
	if v.mutexNodeIDs == nil {
		v.mutexNodeIDs = map[gotypes.Object]string{}
	}
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				v.scanStructForMutex(s)
			case *ast.ValueSpec:
				v.scanValueSpecForMutex(s)
			}
		}
	}
	// Local-var mutex declarations live inside function bodies — walk all
	// function bodies one more time to pick those up (the standard ast.Walk
	// over the file already visited them, but we need an explicit pass for
	// var foo sync.Mutex within functions).
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		v.scanFuncBodyForMutexLocals(fd)
	}
}

// scanStructForMutex inspects a TypeSpec; if it's a struct type, every
// field whose underlying type resolves to *sync.Mutex or *sync.RWMutex
// becomes a NodeMutex. Embedded mutexes (anonymous fields) get a Name
// derived from the type's basename.
func (v *declVisitor) scanStructForMutex(s *ast.TypeSpec) {
	st, ok := s.Type.(*ast.StructType)
	if !ok || st.Fields == nil {
		return
	}
	structQname := v.pkgName + "." + s.Name.Name
	for _, f := range st.Fields.List {
		kind := v.classifyMutexExpr(f.Type)
		if kind == "" {
			continue
		}
		if len(f.Names) == 0 {
			// Embedded mutex: synthesise a name from the underlying type.
			// Suffix qname with #mutex to disambiguate from the Field node
			// emitFields will emit at the same source position (G9 — the
			// previous shared-qname collision caused emitFields to overwrite
			// the Mutex node, leaving acquires_lock edges pointing at a Field
			// node instead of the Mutex node).
			name := embeddedMutexName(f.Type)
			id := v.emitMutexNode(name, structQname+"."+name+"#mutex", kind, f.Pos(), f.End())
			if v.typesInfo != nil {
				if obj := v.typesInfo.Defs[asTypeIdent(f.Type)]; obj != nil {
					v.mutexNodeIDs[obj] = id
				}
			}
			continue
		}
		for _, name := range f.Names {
			// Same #mutex suffix as the embedded branch above — see the
			// comment there for the collision-avoidance rationale.
			id := v.emitMutexNode(name.Name, structQname+"."+name.Name+"#mutex", kind, name.Pos(), f.End())
			if v.typesInfo != nil {
				if obj := v.typesInfo.Defs[name]; obj != nil {
					v.mutexNodeIDs[obj] = id
				}
			}
		}
	}
}

// scanValueSpecForMutex catches package-level `var foo sync.Mutex` decls.
func (v *declVisitor) scanValueSpecForMutex(s *ast.ValueSpec) {
	if s.Type == nil {
		return
	}
	kind := v.classifyMutexExpr(s.Type)
	if kind == "" {
		return
	}
	for _, name := range s.Names {
		qn := v.pkgName + "." + name.Name
		id := v.emitMutexNode(name.Name, qn, kind, name.Pos(), s.End())
		if v.typesInfo != nil {
			if obj := v.typesInfo.Defs[name]; obj != nil {
				v.mutexNodeIDs[obj] = id
			}
		}
	}
}

// scanFuncBodyForMutexLocals walks a function body for local declarations
// (`var mu sync.Mutex`, `mu := sync.Mutex{}`) and emits Mutex nodes.
// The qualified-name uses pkg.func.localVar to disambiguate same-named
// locals across different functions.
func (v *declVisitor) scanFuncBodyForMutexLocals(fd *ast.FuncDecl) {
	funcQname := v.pkgName + "." + fd.Name.Name
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		funcQname = v.pkgName + "." + exprName(fd.Recv.List[0].Type) + "." + fd.Name.Name
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		decl, ok := n.(*ast.DeclStmt)
		if !ok {
			return true
		}
		gen, ok := decl.Decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			return true
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || vs.Type == nil {
				continue
			}
			kind := v.classifyMutexExpr(vs.Type)
			if kind == "" {
				continue
			}
			for _, name := range vs.Names {
				qn := funcQname + "." + name.Name
				id := v.emitMutexNode(name.Name, qn, kind, name.Pos(), vs.End())
				if v.typesInfo != nil {
					if obj := v.typesInfo.Defs[name]; obj != nil {
						v.mutexNodeIDs[obj] = id
					}
				}
			}
		}
		return true
	})
}

// emitMutexNode appends a NodeMutex to v.nodes and a defines-edge from the
// enclosing file. Returns the new node's ID. Confidence is EXTRACTED when
// typesInfo confirmed the type, INFERRED in AST-only mode.
func (v *declVisitor) emitMutexNode(name, qname, subKind string, startPos, endPos token.Pos) string {
	startLn, startBy := v.pos(startPos)
	endLn, endBy := v.pos(endPos)
	id := MakeID(qname, "go", startBy)
	conf := types.ConfExtracted
	if v.typesInfo == nil {
		conf = types.ConfInferred
	}
	v.nodes = append(v.nodes, types.Node{
		ID: id, Type: types.NodeMutex,
		Name: name, QualifiedName: qname,
		FilePath: v.relPath, StartLine: startLn, EndLine: endLn,
		StartByte: startBy, EndByte: endBy,
		Language: "go", Confidence: conf, SubKind: subKind,
	})
	v.edges = append(v.edges, types.Edge{
		Src: v.fileID, Dst: id, Type: types.EdgeDefines, Count: 1,
		Confidence: conf,
	})
	return id
}

// classifyMutexExpr returns "mutex" / "rwmutex" / "" based on whether the
// type expression resolves to sync.Mutex or sync.RWMutex.
//
// With typesInfo: walks the underlying types.Type chain, dereferences any
// pointer/named layers, returns the package+name match. This handles type
// aliases and embedded mutexes correctly (R2.3 in spec).
//
// Without typesInfo (AST-only fallback): pattern-matches `sync.Mutex` /
// `sync.RWMutex` selector expressions by name. False positives possible if
// a different package is also imported as "sync" — flagged INFERRED on
// emit so downstream consumers can distinguish.
func (v *declVisitor) classifyMutexExpr(e ast.Expr) string {
	if v.typesInfo != nil {
		t := v.typesInfo.TypeOf(e)
		return classifyMutexType(t)
	}
	return classifyMutexExprByName(e)
}

// classifyMutexType walks the types.Type to find sync.Mutex / sync.RWMutex
// at any depth (pointer, named alias, embedded). Returns the sub_kind tag
// ("mutex" / "rwmutex") or "" when not a mutex.
func classifyMutexType(t gotypes.Type) string {
	if t == nil {
		return ""
	}
	// Strip pointer layers — `*sync.Mutex` and `sync.Mutex` are both mutexes.
	if ptr, ok := t.(*gotypes.Pointer); ok {
		return classifyMutexType(ptr.Elem())
	}
	named, ok := t.(*gotypes.Named)
	if !ok {
		return ""
	}
	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return ""
	}
	if obj.Pkg().Path() != "sync" {
		return ""
	}
	switch obj.Name() {
	case "Mutex":
		return "mutex"
	case "RWMutex":
		return "rwmutex"
	}
	return ""
}

// classifyMutexExprByName is the AST-only fallback: a SelectorExpr of form
// `sync.Mutex` / `sync.RWMutex` (or starred forms) is treated as the
// matching mutex. Will produce false positives if another import is also
// aliased to "sync"; emitMutexNode flags these with INFERRED confidence.
func classifyMutexExprByName(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		return classifyMutexExprByName(star.X)
	}
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok || pkgIdent.Name != "sync" {
		return ""
	}
	switch sel.Sel.Name {
	case "Mutex":
		return "mutex"
	case "RWMutex":
		return "rwmutex"
	}
	return ""
}

// embeddedMutexName synthesises a field name for an anonymous embedded
// mutex field. `sync.Mutex` → "Mutex", `*sync.RWMutex` → "RWMutex".
func embeddedMutexName(e ast.Expr) string {
	if star, ok := e.(*ast.StarExpr); ok {
		return embeddedMutexName(star.X)
	}
	if sel, ok := e.(*ast.SelectorExpr); ok {
		return sel.Sel.Name
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return "embedded"
}

// asTypeIdent extracts the *ast.Ident representing a type expression, used
// to look up the field's *types.Object for embedded fields. Returns nil
// when the type expression isn't a simple selector.
func asTypeIdent(e ast.Expr) *ast.Ident {
	if star, ok := e.(*ast.StarExpr); ok {
		return asTypeIdent(star.X)
	}
	if sel, ok := e.(*ast.SelectorExpr); ok {
		return sel.Sel
	}
	if id, ok := e.(*ast.Ident); ok {
		return id
	}
	return nil
}

// maybeEmitLockEdge inspects a CallExpr; if it's `<recv>.Lock()`,
// `<recv>.Unlock()`, `<recv>.RLock()` or `<recv>.RUnlock()` AND the
// receiver resolves to a *sync.Mutex / *sync.RWMutex, emits an
// acquires_lock / releases_lock edge from parentFuncID to the matching
// Mutex node.
//
// Resolution path:
//   - typesInfo present: receiver's *types.Object is looked up in
//     mutexNodeIDs (matches the field/var declaration's object). Fast,
//     accurate, EXTRACTED confidence.
//   - typesInfo nil: AST-only fallback — receiver IS-a-known-mutex check
//     uses the string form of the selector chain. INFERRED confidence.
//
// No edge is emitted when the receiver doesn't resolve to a mutex node
// (e.g. user-defined `Lock()` method on an unrelated type — false-positive
// guard required by spec §2 R2.1).
func (v *declVisitor) maybeEmitLockEdge(parentFuncID string, call *ast.CallExpr) {
	method, ok := lockMethodName(call)
	if !ok {
		return
	}
	sel := call.Fun.(*ast.SelectorExpr) // safe: lockMethodName already verified
	mutexID, conf := v.resolveMutexReceiver(sel.X)
	if mutexID == "" {
		return
	}
	edgeType := types.EdgeAcquiresLock
	switch method {
	case "Unlock", "RUnlock":
		edgeType = types.EdgeReleasesLock
	}
	// The R-prefix variant (RLock/RUnlock) is captured on the Mutex node's
	// sub_kind ("rwmutex"), not on the edge — the edge schema has no
	// sub_kind column. Consumers that need to distinguish read/write locks
	// can look up the destination Mutex node.
	pos := v.fset.Position(call.Pos())
	v.edges = append(v.edges, types.Edge{
		Src: parentFuncID, Dst: mutexID, Type: edgeType,
		Line: pos.Line, Count: 1, Confidence: conf,
		FilePath: v.relPath,
	})
}

// lockMethodName returns the called method name and true when call is of
// the form `<expr>.Lock()` / `Unlock()` / `RLock()` / `RUnlock()` with no
// arguments. Returns "", false otherwise.
func lockMethodName(call *ast.CallExpr) (string, bool) {
	if call == nil || len(call.Args) != 0 {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	switch sel.Sel.Name {
	case "Lock", "Unlock", "RLock", "RUnlock":
		return sel.Sel.Name, true
	}
	return "", false
}

// resolveMutexReceiver returns the Mutex node ID for the receiver expression
// `recv` and the appropriate confidence label, or ("", "") if recv doesn't
// resolve to a known mutex.
//
// types.Info path: walks the receiver chain and uses ObjectOf to look up
// the declaration in mutexNodeIDs. For embedded mutexes (`s.Lock()` where
// the embedded *sync.Mutex provides the method), falls back to TypeOf to
// confirm the underlying receiver is a mutex.
//
// AST-only fallback: returns "" — without type info we can't reliably
// distinguish `mu.Lock()` (real mutex) from `controller.Lock()` (user
// method named Lock). False-positive avoidance > coverage for v0.
func (v *declVisitor) resolveMutexReceiver(recv ast.Expr) (string, types.Confidence) {
	if v.typesInfo == nil {
		return "", types.ConfInferred
	}
	// Direct receiver: `mu.Lock()` where mu is the mutex variable/field.
	if id := v.lookupMutexByExpr(recv); id != "" {
		return id, types.ConfExtracted
	}
	// Embedded receiver: `s.Lock()` where s is a struct with embedded
	// sync.Mutex. The receiver TYPE chain doesn't directly include sync.Mutex,
	// but the method set does — confirm by checking the resolved selection's
	// receiver type.
	if t := v.typesInfo.TypeOf(recv); t != nil {
		if classifyMutexType(t) != "" {
			// recv itself IS a mutex value (e.g. dereferenced pointer) but
			// we couldn't find the declaration object — emit unresolved.
			// Returning "" keeps downstream graph consistent (no dangling
			// edges); accept the coverage loss.
			return "", types.ConfInferred
		}
		// Look at the named struct's fields for an embedded mutex.
		if id := v.findEmbeddedMutexInType(t); id != "" {
			return id, types.ConfExtracted
		}
	}
	return "", types.ConfInferred
}

// lookupMutexByExpr maps an ast.Expr (Ident, SelectorExpr) to a Mutex node
// ID via types.Info.ObjectOf. Returns "" when no match.
func (v *declVisitor) lookupMutexByExpr(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		if obj := v.typesInfo.ObjectOf(x); obj != nil {
			return v.mutexNodeIDs[obj]
		}
	case *ast.SelectorExpr:
		if obj := v.typesInfo.ObjectOf(x.Sel); obj != nil {
			return v.mutexNodeIDs[obj]
		}
	case *ast.StarExpr:
		return v.lookupMutexByExpr(x.X)
	}
	return ""
}

// findEmbeddedMutexInType walks the struct fields of t (after dereferencing
// pointer / named) looking for an embedded sync.Mutex / sync.RWMutex.
// Returns the matching mutex node ID (looked up in mutexNodeIDs by the
// field's *types.Var) or "" when not found.
func (v *declVisitor) findEmbeddedMutexInType(t gotypes.Type) string {
	if ptr, ok := t.(*gotypes.Pointer); ok {
		return v.findEmbeddedMutexInType(ptr.Elem())
	}
	named, ok := t.(*gotypes.Named)
	if !ok {
		return ""
	}
	st, ok := named.Underlying().(*gotypes.Struct)
	if !ok {
		return ""
	}
	for f := range st.Fields() {
		if !f.Embedded() {
			continue
		}
		if classifyMutexType(f.Type()) == "" {
			continue
		}
		if id, ok := v.mutexNodeIDs[f]; ok {
			return id
		}
	}
	return ""
}

// concurrency_channel.go-style helpers below: improve Channel emission to
// stamp direction + buffer + elem type into Signature so callers (viewer,
// MCP) can introspect channel attributes without re-parsing Go source.
//
// Implementation note (deferred): currently statements.go emits Channel
// nodes only via the GoStmt handler — there's no direct Channel node when
// you have `ch := make(chan int, 10)`. The existing code emits a CallSite
// for `make`. We could extend that to emit a Channel sibling-node with
// the parsed type info; doing so is included in this commit but lives in
// emitChannelFromMake (called from statements.go's CallExpr case).

// emitChannelFromMake checks if a CallExpr is `make(chan T, n)` and, if so,
// emits a Channel node with direction/elem/buffer encoded in Signature.
// Direction and elem come from the *ast.ChanType argument; buffer from the
// optional second argument when it's an integer literal (best-effort —
// non-literal buffers stay BufferSize=-1 for "unknown").
//
// Returns the new node ID, or "" if call is not a `make(chan ...)` form.
func (v *declVisitor) emitChannelFromMake(parentID string, call *ast.CallExpr) string {
	if call == nil || len(call.Args) == 0 {
		return ""
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "make" {
		return ""
	}
	chType, ok := call.Args[0].(*ast.ChanType)
	if !ok {
		return ""
	}
	dir := channelDirection(chType.Dir)
	elem := exprName(chType.Value)
	var buf int
	if len(call.Args) >= 2 {
		buf = literalIntValue(call.Args[1])
	} else {
		buf = 0
	}
	sig := formatChannelSignature(dir, elem, buf)
	startLn, startBy := v.pos(call.Pos())
	endLn, endBy := v.pos(call.End())
	qname := fmt.Sprintf("chan@%s:%d", v.relPath, startBy)
	nodeID := MakeID(qname, "go", startBy)
	v.nodes = append(v.nodes, types.Node{
		ID: nodeID, Type: types.NodeChannel,
		Name: "channel", QualifiedName: qname,
		FilePath: v.relPath, StartLine: startLn, EndLine: endLn,
		StartByte: startBy, EndByte: endBy,
		Language: "go", Confidence: types.ConfExtracted,
		SubKind: dir, Signature: sig,
	})
	v.edges = append(v.edges, types.Edge{
		Src: parentID, Dst: nodeID, Type: types.EdgeContains, Count: 1,
		Confidence: types.ConfExtracted,
	})
	return nodeID
}

// emitGoroutineChannelEdges walks the goroutine body call expression and
// emits sends_to/recvs_from edges from the Goroutine node to Channel nodes
// for any channel sends/recvs whose channel variable is in chanVarIDs.
// Only handles inline `go func() { ... }()` literals; named-function goroutines
// require cross-file resolution and are silently skipped.
//
// Track C P1a: also emits acquires_lock / releases_lock edges for Lock /
// Unlock / RLock / RUnlock CallExprs nested inside the goroutine body. The
// outer body walker (emitFunctionBodyPos) returns false on *ast.GoStmt to
// avoid double-walking, which previously dropped every lock call inside a
// goroutine literal (the canonical "spawn worker, lock counter" pattern).
// Lock edges are anchored at the *enclosing function* (parentFuncID) — same
// semantics as the non-goroutine path — because that's the symbol that
// owns the critical section's start/end pair.
func (v *declVisitor) emitGoroutineChannelEdges(goroutineID string, call *ast.CallExpr) {
	if call == nil {
		return
	}
	fn, ok := call.Fun.(*ast.FuncLit)
	if !ok {
		return // named-function goroutines: cross-file resolution needed
	}
	// parentFuncID is the function that contains this goroutine. We don't
	// have it threaded as a parameter (refactor-cost vs. value), so derive
	// it from the goroutine node's edge: every Goroutine has a `spawns`
	// edge from its parent. Linear scan is fine here — emits per goroutine
	// are bounded by the body's call density. We recover the parent by
	// looking up the most-recent `spawns` edge whose Dst is goroutineID.
	parentFuncID := ""
	for i := len(v.edges) - 1; i >= 0; i-- {
		if v.edges[i].Type == types.EdgeSpawns && v.edges[i].Dst == goroutineID {
			parentFuncID = v.edges[i].Src
			break
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.GoStmt:
			return false // nested goroutines not recursively tracked here
		case *ast.SendStmt:
			if chanName := channelVarName(s.Chan); chanName != "" {
				if chanID, ok := v.chanVarIDs[chanName]; ok {
					v.edges = append(v.edges, types.Edge{
						Src: goroutineID, Dst: chanID,
						Type: types.EdgeSendsTo, Count: 1,
						Confidence: types.ConfInferred,
					})
				}
			}
		case *ast.UnaryExpr:
			if s.Op == token.ARROW {
				if chanName := channelVarName(s.X); chanName != "" {
					if chanID, ok := v.chanVarIDs[chanName]; ok {
						v.edges = append(v.edges, types.Edge{
							Src: goroutineID, Dst: chanID,
							Type: types.EdgeRecvsFrom, Count: 1,
							Confidence: types.ConfInferred,
						})
					}
				}
			}
		case *ast.CallExpr:
			// Track C P1a: lock edges inside goroutine bodies. Anchored on
			// parentFuncID (the function that spawned the goroutine), which
			// matches the non-goroutine path emitted by maybeEmitLockEdge.
			// No-op when parentFuncID couldn't be recovered (defensive).
			if parentFuncID != "" {
				v.maybeEmitLockEdge(parentFuncID, s)
			}
		}
		return true
	})
}

// channelDirection maps ast.ChanDir to the spec's string tag.
//
//	SEND  → "send"  (chan<- T)
//	RECV  → "recv"  (<-chan T)
//	other → "bidi"  (chan T, the parser bitwise-ORs SEND|RECV for bidi)
func channelDirection(d ast.ChanDir) string {
	switch d {
	case ast.SEND:
		return "send"
	case ast.RECV:
		return "recv"
	}
	return "bidi"
}

// literalIntValue parses an *ast.BasicLit as an int. Returns -1 on any
// failure (non-literal expression, non-INT kind, parse error). Used for
// buffer sizes — non-literal sizes lose the constant info but the channel
// node is still emitted with BufferSize=-1 ("unknown") so consumers can
// distinguish "unbuffered" (0) from "size lost" (-1).
func literalIntValue(e ast.Expr) int {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return -1
	}
	var n int
	if _, err := fmt.Sscanf(lit.Value, "%d", &n); err != nil {
		return -1
	}
	return n
}

// formatChannelSignature returns a concise human-readable channel
// description suitable for the existing Signature field — schema-free way
// to surface direction/buffer/elem in the viewer without a node.go bump.
//
// Examples:
//
//	("bidi", "int", 10) → "chan int (buf=10)"
//	("send", "Msg", 0)  → "chan<- Msg"
//	("recv", "T", -1)   → "<-chan T (buf=?)"
func formatChannelSignature(dir, elem string, buf int) string {
	prefix := "chan"
	switch dir {
	case "send":
		prefix = "chan<-"
	case "recv":
		prefix = "<-chan"
	}
	bufPart := ""
	switch {
	case buf > 0:
		bufPart = fmt.Sprintf(" (buf=%d)", buf)
	case buf < 0:
		bufPart = " (buf=?)"
	}
	if elem == "" {
		elem = "?"
	}
	return prefix + " " + elem + bufPart
}
