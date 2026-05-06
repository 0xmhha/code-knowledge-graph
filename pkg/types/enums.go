package types

// NodeType enumerates the 33 node kinds (spec §5.1; v0.2 schema 1.1 added
// Mutex; schema 1.3 appended Endpoint + MessageType for CKS G5 Distributed;
// schema 1.4 appended Commit for CKS G6 Temporal — git history nodes).
type NodeType string

const (
	NodePackage       NodeType = "Package"
	NodeFile          NodeType = "File"
	NodeStruct        NodeType = "Struct"
	NodeInterface     NodeType = "Interface"
	NodeClass         NodeType = "Class"
	NodeTypeAlias     NodeType = "TypeAlias"
	NodeEnum          NodeType = "Enum"
	NodeContract      NodeType = "Contract"
	NodeMapping       NodeType = "Mapping"
	NodeEvent         NodeType = "Event"
	NodeFunction      NodeType = "Function"
	NodeMethod        NodeType = "Method"
	NodeModifier      NodeType = "Modifier"
	NodeConstructor   NodeType = "Constructor"
	NodeConstant      NodeType = "Constant"
	NodeVariable      NodeType = "Variable"
	NodeField         NodeType = "Field"
	NodeParameter     NodeType = "Parameter"
	NodeLocalVariable NodeType = "LocalVariable"
	NodeImport        NodeType = "Import"
	NodeExport        NodeType = "Export"
	NodeDecorator     NodeType = "Decorator"
	NodeGoroutine     NodeType = "Goroutine"
	NodeChannel       NodeType = "Channel"
	// NodeMutex: schema 1.1 slot reservation (spec v0.2 §2). B1 (Wave 5) will
	// emit; the parser does not produce Mutex nodes yet. Kept adjacent to
	// the other concurrency nodes (Goroutine/Channel) for grouping.
	NodeMutex      NodeType = "Mutex"
	NodeIfStmt     NodeType = "IfStmt"
	NodeLoopStmt   NodeType = "LoopStmt"
	NodeCallSite   NodeType = "CallSite"
	NodeReturnStmt NodeType = "ReturnStmt"
	NodeSwitchStmt NodeType = "SwitchStmt"
	// Schema 1.3 (E3 — CKS G5 Distributed): handler/route topology entries.
	// NodeEndpoint  : an HTTP/RPC route literal (`http:/users`, `rpc:Foo.Bar`).
	// NodeMessageType: a request/response message type a handler dispatches on
	//                  (e.g. `pkg.MyRequest`). Appended at the end so existing
	//                  positional indices stay stable (see TestAllNodeTypes_Stable).
	NodeEndpoint    NodeType = "Endpoint"
	NodeMessageType NodeType = "MessageType"
	// Schema 1.4 (E4 — CKS G6 Temporal): a git commit that touched one or
	// more source files. Name = first 12 chars of SHA, QualifiedName =
	// `commit:<full-sha>`. SubKind = "git". StartLine/EndLine = 1 (commits
	// have no source range). Appended at the end so existing positional
	// indices stay stable (TestAllNodeTypes_Stable).
	NodeCommit NodeType = "Commit"
)

// AllNodeTypes returns all 33 node types in a stable order.
// NOTE: identifier names are stable; positional indices are load-bearing
// only for tests that snapshot the full slice (TestAllNodeTypes_Stable).
// NodeMutex was inserted at index 24 to keep the concurrency family
// (Goroutine/Channel/Mutex) contiguous, which shifted the statement
// nodes (NodeIfStmt..NodeSwitchStmt) from indices 24-28 to 25-29 — no
// callers key on those indices, so the shift is safe; future additions
// should prefer append over insert when no grouping reason argues
// otherwise. NodeEndpoint + NodeMessageType (schema 1.3, E3) are appended
// (indices 30-31) — distributed topology is a distinct family from
// concurrency / statements, no grouping argument applied. NodeCommit
// (schema 1.4, E4) is appended at index 32 — temporal/git history is a
// distinct family from everything above.
func AllNodeTypes() []NodeType {
	return []NodeType{
		NodePackage, NodeFile, NodeStruct, NodeInterface, NodeClass,
		NodeTypeAlias, NodeEnum, NodeContract, NodeMapping, NodeEvent,
		NodeFunction, NodeMethod, NodeModifier, NodeConstructor,
		NodeConstant, NodeVariable, NodeField, NodeParameter, NodeLocalVariable,
		NodeImport, NodeExport, NodeDecorator,
		NodeGoroutine, NodeChannel, NodeMutex,
		NodeIfStmt, NodeLoopStmt, NodeCallSite, NodeReturnStmt, NodeSwitchStmt,
		NodeEndpoint, NodeMessageType,
		NodeCommit,
	}
}

// EdgeType enumerates the 32 edge kinds (spec §5.2; v0.2 schema 1.1 added 3
// lock edges; schema 1.3 appended listens_on / handles_message / rpc_calls
// for CKS G5 Distributed; schema 1.4 appended changed_in / blame for CKS
// G6 Temporal — git history derived; schema 1.6 appended timeout_path /
// cancellation_path for CKS G3 dogfood P2 — Go context.With* propagation).
type EdgeType string

const (
	EdgeContains      EdgeType = "contains"
	EdgeDefines       EdgeType = "defines"
	EdgeCalls         EdgeType = "calls"
	EdgeInvokes       EdgeType = "invokes"
	EdgeUsesType      EdgeType = "uses_type"
	EdgeInstantiates  EdgeType = "instantiates"
	EdgeReferences    EdgeType = "references"
	EdgeReadsField    EdgeType = "reads_field"
	EdgeWritesField   EdgeType = "writes_field"
	EdgeImports       EdgeType = "imports"
	EdgeExports       EdgeType = "exports"
	EdgeImplements    EdgeType = "implements"
	EdgeExtends       EdgeType = "extends"
	EdgeHasModifier   EdgeType = "has_modifier"
	EdgeEmitsEvent    EdgeType = "emits_event"
	EdgeReadsMapping  EdgeType = "reads_mapping"
	EdgeWritesMapping EdgeType = "writes_mapping"
	EdgeHasDecorator  EdgeType = "has_decorator"
	EdgeSpawns        EdgeType = "spawns"
	EdgeSendsTo       EdgeType = "sends_to"
	EdgeRecvsFrom     EdgeType = "recvs_from"
	EdgeBindsTo       EdgeType = "binds_to"
	// Schema 1.1 slot reservations (spec v0.2 §2): concurrency lock semantics.
	// B1 (Wave 5) will emit these from the Go Mutex AST pass; the parser does
	// not produce them yet. Appended (not interleaved) so existing edge-type
	// hash positions / test snapshots stay stable.
	EdgeAcquiresLock      EdgeType = "acquires_lock"
	EdgeReleasesLock      EdgeType = "releases_lock"
	EdgeAccessedUnderLock EdgeType = "accessed_under_lock"
	// Schema 1.3 (E3 — CKS G5 Distributed): handler/RPC topology edges.
	// listens_on:      handler function/method → endpoint route
	// handles_message: handler function/method → message type it dispatches on
	// rpc_calls:       caller function → server method (or message-type placeholder)
	// Appended (not interleaved) so existing edge-type hash positions / test
	// snapshots stay stable.
	EdgeListensOn      EdgeType = "listens_on"
	EdgeHandlesMessage EdgeType = "handles_message"
	EdgeRPCCalls       EdgeType = "rpc_calls"
	// Schema 1.4 (E4 — CKS G6 Temporal): git-history derived edges.
	// changed_in: any symbol whose file was touched by a commit → that
	//             commit. Heuristic — file-level (not line-level). Bounded
	//             by Options.TemporalDepth (default 10) most-recent commits
	//             per file. Line-level blame is deferred (G6 Phase 2).
	// blame:      File node → most-recent commit touching that file
	//             (V0 simplification of `file:line → commit`).
	// Appended (not interleaved) so existing edge-type hash positions /
	// test snapshots stay stable.
	EdgeChangedIn EdgeType = "changed_in"
	EdgeBlame     EdgeType = "blame"
	// Schema 1.6 (P2 — CKS G3 control-flow context propagation): Go
	// context.With* creation sites. Self-loop edges anchored on the
	// enclosing Function/Method:
	//   timeout_path:      context.WithTimeout / context.WithDeadline call
	//                      site. Deadline is treated as a timeout variant —
	//                      both express "this work is bounded by a wall-clock
	//                      budget" and consumers (graph queries / viewer)
	//                      benefit from collapsing them.
	//   cancellation_path: context.WithCancel / context.WithCancelCause call
	//                      site (Go 1.20+ for the latter). Distinct from
	//                      timeout_path because cancellation is event-driven,
	//                      not deadline-driven.
	// TODO: retry_path is intentionally NOT emitted in V0 — the heuristic
	// (loops around RPC calls? error-handling branches?) is too noisy to
	// ship without false-positive cleanup. Reserved for a follow-up once
	// we have a typed retry pattern (e.g. detecting backoff libraries
	// like cenkalti/backoff or built-in `for { ...err... }` loops with
	// rpc_calls inside).
	// Appended (not interleaved) so existing edge-type hash positions /
	// test snapshots stay stable.
	EdgeTimeoutPath      EdgeType = "timeout_path"
	EdgeCancellationPath EdgeType = "cancellation_path"
)

// AllEdgeTypes returns all 32 edge types in stable order.
// Append-only: existing positions are load-bearing for hash-derived IDs.
func AllEdgeTypes() []EdgeType {
	return []EdgeType{
		EdgeContains, EdgeDefines, EdgeCalls, EdgeInvokes, EdgeUsesType,
		EdgeInstantiates, EdgeReferences, EdgeReadsField, EdgeWritesField,
		EdgeImports, EdgeExports, EdgeImplements, EdgeExtends,
		EdgeHasModifier, EdgeEmitsEvent, EdgeReadsMapping, EdgeWritesMapping,
		EdgeHasDecorator, EdgeSpawns, EdgeSendsTo, EdgeRecvsFrom, EdgeBindsTo,
		EdgeAcquiresLock, EdgeReleasesLock, EdgeAccessedUnderLock,
		EdgeListensOn, EdgeHandlesMessage, EdgeRPCCalls,
		EdgeChangedIn, EdgeBlame,
		EdgeTimeoutPath, EdgeCancellationPath,
	}
}

// Confidence labels (spec §4.8).
type Confidence string

const (
	ConfExtracted Confidence = "EXTRACTED"
	ConfInferred  Confidence = "INFERRED"
	ConfAmbiguous Confidence = "AMBIGUOUS"
)

// Valid reports whether c is one of the three known confidence labels.
func (c Confidence) Valid() bool {
	switch c {
	case ConfExtracted, ConfInferred, ConfAmbiguous:
		return true
	}
	return false
}
