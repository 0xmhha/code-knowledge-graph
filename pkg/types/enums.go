package types

// NodeType enumerates the 35 node kinds (spec §5.1; v0.2 schema 1.1 added
// Mutex; schema 1.3 appended Endpoint + MessageType for CKS G5 Distributed;
// schema 1.4 appended Commit for CKS G6 Temporal — git history nodes;
// schema 1.8 appended Hunk for CKS G6 Temporal Hunk-graph — one block of
// changed lines per (commit, file); schema 1.10 appended AwaitPoint for
// within-language semantics Phase 4 — W-B TS async/await suspension points
// (slot reserved; detector lands in Phase 5)).
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
	// Schema 1.10 (within-language semantics Phase 4 — W-B TS async/await):
	// statement-level node emitted at each `await` expression in TypeScript
	// source. Marks an async suspension point so graph queries can answer
	// "where does control yield, and to which AsyncCallSite?". Slot
	// reserved 2026-05-11; detector lands in Phase 5 (W-B W2). See
	// docs/design/ts-async-await-and-interface.md §2.1 + §3.2 and
	// docs/DISPATCH-WITHIN-LANG-SEMANTICS.md §2 Phase 4. Appended at the
	// end so existing positional indices stay stable
	// (TestAllNodeTypes_Stable).
	NodeAwaitPoint NodeType = "AwaitPoint"

	// NodePolicy (schema 1.14, P1 #4) — governance/protocol policy
	// metadata loaded from an external YAML rather than the parsed
	// source tree. Surfaces "why does this code exist?" — fork blocks,
	// gas schedules, consensus parameters, security policies — so an
	// LLM can answer policy-driven questions ("which fork activated
	// this?", "what gas schedule governs this?") without first
	// searching the code itself. See docs/PROJECT-BLUEPRINT-ALIGNMENT.md
	// §4.2 P1 #4 and pkg/policy for the YAML loader. FilePath /
	// StartLine cite the YAML entry's source location so citations
	// stay grounded.
	NodePolicy NodeType = "Policy"

	// NodeSecurityPattern (schema 1.15, P1 #5) — security risk
	// pattern annotations loaded from an external YAML. Captures
	// "this symbol is reachable in a reentrancy / access-control /
	// Byzantine / overflow scenario" so an LLM modifying the code
	// can see the risk surface at retrieval time instead of having
	// to run a separate static analyser. SubKind carries the
	// category (reentrancy / access-control / …), the attrs JSON
	// blob carries severity (info / low / medium / high / critical)
	// and an optional remediation hint. See pkg/security for the
	// YAML loader and PROJECT-BLUEPRINT-ALIGNMENT.md §4.2 P1 #5.
	NodeSecurityPattern NodeType = "SecurityPattern"
)

// AllNodeTypes returns all 35 node types in a stable order.
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
// NodeAwaitPoint (schema 1.10, W-B) is appended at index 34 — TS async
// suspension family, slot reserved before the Phase 5 detector lands.
// NodePolicy (schema 1.14, P1 #4) is appended at index 35 — domain
// governance/protocol policy metadata loaded from an external YAML.
// NodeSecurityPattern (schema 1.15, P1 #5) is appended at index 36 —
// security risk pattern annotations loaded from an external YAML.
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
		NodeAwaitPoint,
		NodePolicy,
		NodeSecurityPattern,
	}
}

// IsSymbol reports whether t is a "symbol-level" node — a code unit
// (function, type, field, package, file, endpoint, …) that a coding
// agent's keyword search would normally want to surface. Returns
// false for:
//
//   - Statement nodes (IfStmt, LoopStmt, CallSite, ReturnStmt,
//     SwitchStmt, AwaitPoint) — control-flow markers whose qname
//     carries the enclosing function's prefix, which makes them
//     false-positive FTS hits on a keyword that names the enclosing
//     symbol.
//   - Meta nodes (Commit, Hunk) — git-history rows surfaced via
//     evidence_for_intent, not the symbol search path.
//   - Path-only nodes (Import, Export) — their FTS columns carry
//     module paths, so a query like "Vault" matches every import of
//     contracts/Vault even when the consumer wanted the class itself.
//
// search_text (pkg/mcphandlers + internal/persist.SearchFTS) uses
// IsSymbol as the default whitelist when SearchFTSOptions.NodeKinds
// is nil. Callers that want the full surface (statement nodes
// included) pass an explicit NodeKinds slice — typically
// types.AllNodeTypes().
func (t NodeType) IsSymbol() bool {
	switch t {
	case NodeIfStmt, NodeLoopStmt, NodeCallSite, NodeReturnStmt, NodeSwitchStmt,
		NodeAwaitPoint,
		NodeCommit, NodeHunk,
		NodeImport, NodeExport:
		return false
	default:
		return true
	}
}

// SymbolNodeTypes returns the symbol-level subset of AllNodeTypes —
// the default whitelist for search_text when no explicit NodeKinds
// is supplied. Stable ordering tracks AllNodeTypes (callers must not
// key on positional indices; see TestAllNodeTypes_Stable for the
// invariant).
func SymbolNodeTypes() []NodeType {
	all := AllNodeTypes()
	out := make([]NodeType, 0, len(all))
	for _, t := range all {
		if t.IsSymbol() {
			out = append(out, t)
		}
	}
	return out
}

// EdgeType enumerates the 41 edge kinds (spec §5.2; v0.2 schema 1.1 added 3
// lock edges; schema 1.3 appended listens_on / handles_message / rpc_calls
// for CKS G5 Distributed; schema 1.4 appended changed_in / blame for CKS
// G6 Temporal — git history derived; schema 1.6 appended timeout_path /
// cancellation_path for CKS G3 dogfood P2 — Go context.With* propagation;
// schema 1.8 appended has_hunk / adjacent for the Hunk-graph H1 stage,
// then `modifies` for the H2 AST-overlap stage; schema 1.9 W2 appended
// `http_calls` — caller Function → Endpoint (HTTP client call sites);
// schema 1.9 W3b appended `grpc_listens_on` + `grpc_calls` — Go gRPC
// server/client detection; schema 1.10 appended `awaits` (W-B, TS async
// suspension flow) + `overrides` (W-C, Solidity virtual/override semantics)
// for within-language semantics Phase 4 — slots reserved 2026-05-11,
// detectors land in Phase 5).
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
	// Schema 1.10 (within-language semantics Phase 4 — slots reserved
	// 2026-05-11; detectors land in Phase 5):
	//   awaits   (W-B): Function/Method → AwaitPoint, and AwaitPoint →
	//                   AsyncCallSite. Marks async suspension flow in
	//                   TypeScript source so graph queries can answer
	//                   "where does control yield, and what call awaits
	//                   here?". See docs/design/ts-async-await-and-interface.md
	//                   §2.1 + §3.2.
	//   overrides (W-C): Method → Method between a child contract's method
	//                   that overrides a parent's `virtual` method in
	//                   Solidity. Direction = child → parent (Q4 decision
	//                   in solidity-inheritance spec §5.0). Distinct from
	//                   `implements` (interface satisfaction) because
	//                   `overrides` is concrete-to-concrete virtual
	//                   dispatch resolution. See
	//                   docs/design/solidity-inheritance-and-interface-dispatch.md
	//                   §2.1 + §3.3.
	//   using_for (W-C W6): Contract → Library binding emitted from
	//                   `using SafeMath for uint256` directives. First-class
	//                   so binding queries can run as a single edge-type
	//                   filter (matches the extends/implements/overrides/
	//                   has_modifier idiom — every other Sol-specific
	//                   semantic relation is already first-class). Q9-1 (b)
	//                   decision (2026-05-12). Direction = contract →
	//                   library; the call site of `balance.add(...)` still
	//                   produces a separate EdgeCalls into the library
	//                   function. See solidity-inheritance-and-interface-
	//                   dispatch.md §4.6.
	//
	// Appended at the end so existing edge-type hash positions / test
	// snapshots stay stable.
	EdgeAwaits    EdgeType = "awaits"
	EdgeOverrides EdgeType = "overrides"
	EdgeUsesFor   EdgeType = "using_for"
	// Schema 1.14 (P1 #4 — policy metadata): code symbol → Policy node.
	// Emitted from pkg/policy.Resolve when a YAML policy entry's
	// governs[].qname matches an existing Function/Method/Field/Struct/
	// Constant/Variable in the parsed graph. Direction = governed node
	// → policy that constrains it, matching the natural query "what
	// rules apply to this symbol?". Multiple policies can govern the
	// same symbol; the Policy node side fans in.
	EdgeGovernedBy EdgeType = "governed_by"
	// Schema 1.15 (P1 #5 — security pattern annotations): code symbol
	// → SecurityPattern node. Emitted from pkg/security.Resolve when
	// a YAML security pattern's matches[].qname hits an existing
	// Function/Method/Field/Struct/Modifier in the parsed graph.
	// Direction = at-risk code symbol → pattern label so an LLM
	// modifying X can read "what security patterns does X exhibit?"
	// as a single edge-type lookup on src.
	EdgeHasSecurityPattern EdgeType = "has_security_pattern"
)

// AllEdgeTypes returns all 43 edge types in stable order.
// Append-only: existing positions are load-bearing for hash-derived IDs.
// EdgeAwaits (W-B) + EdgeOverrides (W-C) are appended at indices 38-39
// for schema 1.10 (slot-only; detectors land in Phase 5).
// EdgeUsesFor (W-C W6) is appended at index 40 (schema 1.10 W6 — using
// For library extension; first-class binding edge per Q9-1 (b) 2026-05-12).
// EdgeGovernedBy (P1 #4) is appended at index 41 (schema 1.14 — code
// symbol → external Policy node loaded from YAML; first-class so
// policy queries can run as a single edge-type filter).
// EdgeHasSecurityPattern (P1 #5) is appended at index 42 (schema 1.15 —
// code symbol → SecurityPattern node loaded from YAML).
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
		EdgeAwaits, EdgeOverrides,
		EdgeUsesFor,
		EdgeGovernedBy,
		EdgeHasSecurityPattern,
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
