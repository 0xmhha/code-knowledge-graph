package types

// NodeType enumerates the 34 node kinds (spec §5.1; v0.2 schema 1.1 added
// Mutex; schema 1.3 appended Endpoint + MessageType for CKS G5 Distributed;
// schema 1.4 appended Commit for CKS G6 Temporal — git history nodes;
// schema 1.8 appended Hunk for CKS G6 Temporal Hunk-graph — one block of
// changed lines per (commit, file)).
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
	// NodeMutex: schema 1.1 slot — emitted by B1 Phase 1 of the Go
	// concurrency pass for sync.Mutex / sync.RWMutex fields, package-level
	// vars, and function-local vars. See
	// internal/parse/golang/concurrency.go:emitMutexNode. Cross-function
	// lock chain propagation (caller holds X, callee touches field) is
	// deferred to D1 — see
	// docs/design/go-cross-function-lock-propagation.md (decisions resolved
	// 2026-05-11). Kept adjacent to the other concurrency nodes
	// (Goroutine/Channel) for grouping.
	NodeMutex      NodeType = "Mutex"
	NodeIfStmt     NodeType = "IfStmt"
	NodeLoopStmt   NodeType = "LoopStmt"
	NodeCallSite   NodeType = "CallSite"
	NodeReturnStmt NodeType = "ReturnStmt"
	NodeSwitchStmt NodeType = "SwitchStmt"
	// Schema 1.3 (E3 — CKS G5 Distributed): handler/route topology entries.
	// NodeEndpoint  : an HTTP/RPC route literal. Qname follows protocol-
	//                  specific format (schema 1.9 §6.2):
	//                    - http  : `http:METHOD /route`   (METHOD=`*` for any)
	//                    - rpc   : `rpc:Service.Method`
	//                    - grpc  : `grpc:pkg.Service.Method`  (W3)
	//                    - ws    : `ws:/route[#msg]`          (later)
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
	// Schema 1.8 (Hunk-graph H1 — CKS G6 Temporal extension): one contiguous
	// block of changed lines in one file in one commit, as defined by
	// unified-diff `@@` headers. Name = "<sha12>:<file>:<idx>",
	// QualifiedName = `hunk:<full-sha>:<file>:<idx>` (idx = 0-based per-commit
	// hunk position so multiple hunks per commit get distinct IDs via MakeID).
	// SubKind = "git". StartLine/EndLine = the hunk's @@ header new-file
	// line range; StartByte = 0 / EndByte = 1 sentinels (the patch text
	// lives in blobs.source, gzip-compressed; see hunk-graph.md §2.2-2.3).
	// Confidence semantics (hunk-graph.md §11.3 — finalised 2026-05-09):
	//   - EXTRACTED: HEAD-reachable hunks (the only kind H1 collects).
	//   - AMBIGUOUS: reserved for unreachable hunks that a future PR will
	//                collect via reflog/fsck. The H3 EvidencePack assembler
	//                MUST filter to confidence='EXTRACTED' so the LLM never
	//                sees code paths that were rolled back by force-push.
	// Appended at the end so existing positional indices stay stable
	// (TestAllNodeTypes_Stable).
	NodeHunk NodeType = "Hunk"
)

// AllNodeTypes returns all 34 node types in a stable order.
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
// distinct family from everything above. NodeHunk (schema 1.8, Hunk-graph
// H1) is appended at index 33 — same temporal family as NodeCommit but
// finer-grained (one block of changed lines, not a whole commit).
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
		NodeHunk,
	}
}

// EdgeType enumerates the 38 edge kinds (spec §5.2; v0.2 schema 1.1 added 3
// lock edges; schema 1.3 appended listens_on / handles_message / rpc_calls
// for CKS G5 Distributed; schema 1.4 appended changed_in / blame for CKS
// G6 Temporal — git history derived; schema 1.6 appended timeout_path /
// cancellation_path for CKS G3 dogfood P2 — Go context.With* propagation;
// schema 1.8 appended has_hunk / adjacent for the Hunk-graph H1 stage,
// then `modifies` for the H2 AST-overlap stage; schema 1.9 W2 appended
// `http_calls` — caller Function → Endpoint (HTTP client call sites);
// schema 1.9 W3b appended `grpc_listens_on` + `grpc_calls` — Go gRPC
// server/client detection).
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
	// Schema 1.1 (concurrency lock semantics) — emitted by the Go
	// concurrency pass:
	//   acquires_lock / releases_lock: from
	//     internal/parse/golang/concurrency.go:maybeEmitLockEdge — matches
	//     sync.Mutex.Lock/Unlock/RLock/RUnlock by object-identity on the
	//     receiver (types.Info path, EXTRACTED) or by name match (AST-only,
	//     INFERRED). False-positive guarded against user-defined Lock() on
	//     non-mutex types (spec §2 R2.1).
	//   accessed_under_lock: from
	//     internal/parse/golang/concurrency_underlock.go — intra-function
	//     lexical heuristic (any field access in a body that holds any lock
	//     gets one edge per (field, mutex) pair). Cross-function chain
	//     propagation is deferred to D1 — see
	//     docs/design/go-cross-function-lock-propagation.md (decisions
	//     resolved 2026-05-11, --lock-propagation opt-in flag).
	// Appended (not interleaved) so existing edge-type hash positions /
	// test snapshots stay stable.
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
	// Schema 1.8 (Hunk-graph H1 — CKS G6 Temporal extension):
	//   has_hunk: Commit → Hunk. One per Hunk; "this commit produced this
	//             block of changed lines". Confidence mirrors the Hunk's
	//             own (EXTRACTED for HEAD-reachable, AMBIGUOUS for
	//             unreachable hunks added by a future reflog-collection PR).
	//   adjacent: Hunk → Hunk between same-commit, same-file hunks
	//             ordered by their @@ header start line. Provides a
	//             deterministic "next-in-this-file" traversal so the
	//             EvidencePack assembler can stitch a multi-hunk view
	//             of a commit's edits without a separate ORDER BY query.
	//             Emitted only between hunks within one (commit, file)
	//             pair — never across commits or files. Out-of-scope edges:
	//             modifies (Hunk → CodeNode interval overlap) lands in H2;
	//             same_logical_change clustering across commits is out of
	//             scope (see hunk-graph.md §11.5 decision).
	// Appended (not interleaved) so existing edge-type hash positions /
	// test snapshots stay stable.
	EdgeHasHunk  EdgeType = "has_hunk"
	EdgeAdjacent EdgeType = "adjacent"
	// Schema 1.8 (Hunk-graph H2 — CKS G6 Temporal extension):
	//   modifies: Hunk → CodeNode (Function/Method/Struct/Interface/Field
	//             /etc) when the hunk's [start_line, end_line] interval
	//             overlaps the CodeNode's interval inside the same file.
	//             Whitelisted to "FunctionLike + TypeLike + Field-ish" so
	//             noise-level statement nodes (CallSite / IfStmt / ...)
	//             don't blow up the edge count without retrieval signal.
	//             See docs/design/hunk-graph.md §4.
	// Appended at the end so existing hash positions stay stable.
	EdgeModifies EdgeType = "modifies"
	// Schema 1.9 W2 (CKS G5 Distributed cross-language interop expansion):
	//   http_calls: caller Function/Method → Endpoint when the function
	//               invokes an HTTP client (TS: fetch / axios / useSWR /
	//               useQuery; Go: http.Get / http.Post / http.NewRequest /
	//               (*http.Client).Get/Post/Do).
	//
	// Target resolution uses 2-stage cascade (schema-1.9-spec §6.9):
	//   1. Specific-verb lookup: `http:METHOD /path` exact match.
	//   2. Wildcard fallback: `http:* /path` exact match.
	// On miss the matcher synthesises an AMBIGUOUS placeholder Endpoint
	// (schema-1.9-spec §6.3 (B)) so the call site stays surfaceable for
	// monorepo external-API audits. Path matching is EXACT (schema-1.9-spec
	// §3.3 decision: V0 chooses exact-match over suffix-match because
	// false-positives across distinct services with overlapping path
	// suffixes are far worse than the false-negatives exact-match incurs
	// in well-curated monorepos).
	//
	// Appended at the end so existing edge-type hash positions / test
	// snapshots stay stable.
	EdgeHTTPCalls EdgeType = "http_calls"
	// Schema 1.9 W3b (CKS G5 Distributed cross-language interop expansion —
	// Go gRPC server/client detection):
	//   grpc_listens_on: server impl Method → Endpoint when the file calls
	//                    `pb.RegisterXXXServer(s, &impl{})`. Each method on
	//                    the impl receiver type whose name matches an rpc
	//                    method on the generated XServer interface emits
	//                    one edge to a `grpc:Service.Method` Endpoint
	//                    (language="go", sub_kind="grpc").
	//   grpc_calls:      caller Function/Method → Endpoint when the body
	//                    calls `<stub>.RpcMethod(ctx, req)` where `stub`
	//                    was assigned from `pb.NewXXXClient(conn)`. Like
	//                    http_calls, on miss the matcher synthesises an
	//                    AMBIGUOUS placeholder Endpoint (language="external")
	//                    so external-API call sites stay surfaceable.
	//
	// Confidence split (schema-1.9-spec §6.5 (C) Both with split confidence):
	//   - typesInfo available + method matches generated XServer interface
	//     → EXTRACTED.
	//   - AST-only (no typesInfo) suffix-matcher on RegisterXXXServer →
	//     INFERRED.
	//   - Miss / unresolved stub var type → AMBIGUOUS placeholder.
	//
	// Appended at the end so existing edge-type hash positions / test
	// snapshots stay stable.
	EdgeGRPCListensOn EdgeType = "grpc_listens_on"
	EdgeGRPCCalls     EdgeType = "grpc_calls"
)

// AllEdgeTypes returns all 38 edge types in stable order.
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
		EdgeHasHunk, EdgeAdjacent, EdgeModifies,
		EdgeHTTPCalls,
		EdgeGRPCListensOn, EdgeGRPCCalls,
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
