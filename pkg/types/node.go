package types

// Node mirrors the SQLite nodes row plus runtime fields (spec §5.3).
type Node struct {
	ID            string   `json:"id"             validate:"required,len=16"`
	Type          NodeType `json:"type"           validate:"required"`
	Name          string   `json:"name"           validate:"required"`
	QualifiedName string   `json:"qualified_name" validate:"required"`
	// CanonicalID is the globally-unique, import-path-qualified identity of the
	// symbol (e.g. "github.com/ethereum/go-ethereum/core/vm.(*EVM).Call"),
	// receiver- and signature-aware, used for exact resolution. QualifiedName
	// stays the short, suffix-searchable display form. Empty when type info is
	// unavailable (AST-only mode) or for node kinds not yet wired. See
	// code-knowledge-system docs/symbol-identity-design.md.
	CanonicalID string     `json:"canonical_id,omitempty"`
	FilePath    string     `json:"file_path"      validate:"required"`
	StartLine   int        `json:"start_line"     validate:"min=1"`
	EndLine     int        `json:"end_line"       validate:"min=1"`
	StartByte   int        `json:"start_byte"     validate:"min=0"`
	EndByte     int        `json:"end_byte"       validate:"gtfield=StartByte"`
	Language    string     `json:"language"       validate:"required,oneof=go ts sol proto"`
	Visibility  string     `json:"visibility,omitempty"`
	Signature   string     `json:"signature,omitempty"`
	DocComment  string     `json:"doc_comment,omitempty"`
	Complexity  int        `json:"complexity,omitempty"`
	InDegree    int        `json:"in_degree"`
	OutDegree   int        `json:"out_degree"`
	PageRank    float64    `json:"pagerank"`
	UsageScore  float64    `json:"usage_score"`
	Confidence  Confidence `json:"confidence"     validate:"required"`
	SubKind     string     `json:"sub_kind,omitempty"`
	// SlotIndex (W-C W9 V0, 2026-05-18): EVM storage slot index for
	// Solidity state variables (NodeField). V0 is per-contract
	// declaration-order index (0, 1, 2, ...) — bit-packing and
	// inheritance offsets are deferred to V1+. Omitted from JSON for
	// non-state-var nodes and for NodeField rows where the value is
	// the zero default.
	SlotIndex int `json:"slot_index,omitempty"`
	// HasAssembly (W-C W10 V0, 2026-05-18): true when a Solidity
	// callable (function / modifier / constructor / fallback) contains
	// at least one `assembly { ... }` block in its body. Lets
	// downstream consumers run a basic "show me all functions with
	// inline assembly" query without re-parsing source. V0 detects
	// presence only; Yul-internal op detection (delegatecall, sstore,
	// selfdestruct, …) and receiver resolution are deferred to V1+.
	HasAssembly bool `json:"has_assembly,omitempty"`
	// HasLowLevelCall (W-C W8 V1, 2026-05-18): true when a Solidity
	// callable contains at least one `.call` / `.delegatecall` /
	// `.staticcall` invocation, regardless of whether the receiver
	// resolves to a concrete contract / interface. W7.1 V0 emits an
	// EdgeInvokes only when the receiver is a state-var / parameter
	// typed as Contract or Interface; this marker additionally surfaces
	// dynamic-address receivers (e.g. `address(target).call(...)`)
	// where no static target exists.
	HasLowLevelCall bool `json:"has_low_level_call,omitempty"`
	// HasValueTransfer (W-C W8 V1, 2026-05-18): true when a Solidity
	// callable contains at least one `.send` or `.transfer` value-
	// transfer. Distinct from low-level method calls — Sol semantics
	// for send/transfer are ETH transfer with limited gas, not method
	// dispatch. Security tooling commonly differentiates these.
	HasValueTransfer bool `json:"has_value_transfer,omitempty"`
	// YulBuiltins (W-C W10 V1.1, 2026-05-18): security-relevant EVM
	// opcodes that appear inside the callable's `assembly { ... }`
	// blocks. Sorted, deduped, lower-case identifiers — the slice is
	// the canonical set of Yul builtin names tree-sitter exposes
	// under `yul_evm_builtin` (e.g. "delegatecall", "sstore", "sload",
	// "selfdestruct", "call", "staticcall"). Empty for callables with
	// no assembly or only non-critical Yul ops.
	YulBuiltins []string `json:"yul_builtins,omitempty"`
	// IsFunctionTyped (W-C W8 V2, 2026-05-18): true when a NodeField
	// is a Solidity state variable declared with a function type
	// (e.g. `function(uint256) external returns (uint256) handler;`).
	// V0 marker only — call-site resolution `stored(args)` against
	// function-typed state vars is deferred. Empty for non-field
	// nodes and for fields whose type is anything but a function.
	IsFunctionTyped bool `json:"is_function_typed,omitempty"`
	// HasFunctionTypedVar (W-C W8 V3, 2026-05-19): true when a
	// Solidity callable (NodeFunction / NodeModifier) has at least
	// one parameter or local variable declared with a function type.
	// Indirect dispatch through function pointers is a control-flow
	// integrity signal — security tooling commonly flags callables
	// that load and invoke caller-supplied callbacks. The marker is
	// presence-only; the V0 dispatch path does not resolve the
	// concrete target since function-typed locals can be reassigned
	// across paths.
	HasFunctionTypedVar bool `json:"has_function_typed_var,omitempty"`
	// HasFunctionPointerCall (W-C W8 V4, 2026-05-19): true when a
	// callable invokes a function pointer — a call_expression whose
	// callee identifier resolves to a function-typed parameter or
	// local variable in the same callable. Complements
	// HasFunctionTypedVar: a function may declare a function-typed
	// var without invoking it, or invoke a function-typed pointer
	// passed from another scope. Together the two markers locate
	// every callable involved in indirect dispatch.
	HasFunctionPointerCall bool `json:"has_function_pointer_call,omitempty"`
	// HasExternalCall (W-C W10 V4, 2026-05-19): true when a callable
	// performs at least one low-level call (Sol .call /
	// .delegatecall / .staticcall or the Yul equivalents) whose
	// receiver resolves to an address-typed Sol scope variable
	// rather than a Contract / Interface. Distinguishes "arbitrary-
	// address dispatch" from the resolved-receiver shape that lands
	// as a concrete EdgeInvokes, which security tooling commonly
	// flags for re-entrancy / external-call risk analysis.
	HasExternalCall bool `json:"has_external_call,omitempty"`
	// HasInheritanceMROFallback (W-C W9 V8, 2026-05-19): true when
	// a NodeContract / NodeInterface declared an inheritance graph
	// that has no consistent C3 linearization. Sol's reference
	// compiler rejects such hierarchies; the parser falls back to a
	// deterministic depth-first walk so layout stays computable,
	// but downstream tooling should surface the diagnostic so the
	// developer notices the would-be-rejected hierarchy.
	HasInheritanceMROFallback bool `json:"has_inheritance_mro_fallback,omitempty"`
	// HasFunctionPointerPropagation (W-C W8 V8, 2026-05-19): true
	// when a callable propagates a function-typed value without
	// invoking it — assigning it to a state variable, passing it
	// as an argument to another call, or both. Distinct from
	// HasFunctionPointerCall (which marks invocation) and from
	// HasFunctionTypedVar (which marks declaration). Security
	// tooling tracking indirect-dispatch surfaces uses all three
	// together: declaration + propagation + invocation = the full
	// life-cycle of a function pointer through the contract.
	HasFunctionPointerPropagation bool `json:"has_function_pointer_propagation,omitempty"`
	// HasSelfReentrantCall (W-C W10 V8, 2026-05-19): true when a
	// callable performs a low-level call whose receiver is a self
	// cast — `payable(this).call(...)` or `address(this).call(...)`.
	// The receiver is the same contract, so this isn't arbitrary-
	// address dispatch (HasExternalCall would mislead); it IS a
	// reentrancy surface because the call re-enters the contract's
	// fallback() / receive() path. Security tooling that scans for
	// the two surfaces separately consumes both markers.
	HasSelfReentrantCall bool `json:"has_self_reentrant_call,omitempty"`
	// HasSelfDelegatecallDead (W-C W10 V9, 2026-05-19): true when a
	// callable performs `address(this).delegatecall(...)` or
	// `payable(this).delegatecall(...)`. Sol semantics make this
	// effectively dead — delegatecall executes the target's code
	// against the caller's storage, and the target IS the caller,
	// so the operation reduces to re-running the contract's own
	// dispatch with the same calldata. Almost always a bug or a
	// confused re-implementation of an internal call. The marker
	// rides alongside HasSelfReentrantCall (the cast still re-
	// enters fallback / receive on certain edge paths) so security
	// tooling can pick either signal.
	HasSelfDelegatecallDead bool `json:"has_self_delegatecall_dead,omitempty"`
	// HasHighLevelSelfCall (W-C W10 V19, 2026-05-21): true when a
	// callable performs a *typed* self-call — `this.foo()`,
	// `MyContract(address(this)).foo()`, or `IFoo(address(this)).bar()`
	// (and equivalents through cast chains). Unlike
	// HasSelfReentrantCall (which keys on low-level `.call /
	// .delegatecall / .transfer / .send` to a self-cast receiver),
	// this marker surfaces *high-level* dispatch self-calls: typed
	// invocations through the EVM message-call boundary that still
	// allow re-entrancy. The two markers are independent — a callable
	// can have either, both, or neither — and security tooling
	// consumes both to characterise the full re-entrancy surface.
	HasHighLevelSelfCall bool `json:"has_high_level_self_call,omitempty"`
	// RecentPRs (ckg-NEW-2, schema 1.12, 2026-05-26): build-time-
	// derived list of pull requests whose merge commit touched lines
	// overlapping this node's [StartLine, EndLine] range. Populated
	// on demand — the canonical fetch path is
	// store.Reader.GetNodePRs (ckg-NEW-4) with an explicit cutoff,
	// not eager joining on every nodes read. JSON omits the field
	// when nil so existing payloads (HTTP / MCP / chunked export)
	// stay byte-identical until a caller asks for breadcrumbs.
	RecentPRs []PRRef `json:"recent_prs,omitempty"`
}
