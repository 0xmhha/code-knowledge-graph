package types

// Node mirrors the SQLite nodes row plus runtime fields (spec §5.3).
type Node struct {
	ID            string     `json:"id"             validate:"required,len=16"`
	Type          NodeType   `json:"type"           validate:"required"`
	Name          string     `json:"name"           validate:"required"`
	QualifiedName string     `json:"qualified_name" validate:"required"`
	FilePath      string     `json:"file_path"      validate:"required"`
	StartLine     int        `json:"start_line"     validate:"min=1"`
	EndLine       int        `json:"end_line"       validate:"min=1"`
	StartByte     int        `json:"start_byte"     validate:"min=0"`
	EndByte       int        `json:"end_byte"       validate:"gtfield=StartByte"`
	Language      string     `json:"language"       validate:"required,oneof=go ts sol proto"`
	Visibility    string     `json:"visibility,omitempty"`
	Signature     string     `json:"signature,omitempty"`
	DocComment    string     `json:"doc_comment,omitempty"`
	Complexity    int        `json:"complexity,omitempty"`
	InDegree      int        `json:"in_degree"`
	OutDegree     int        `json:"out_degree"`
	PageRank      float64    `json:"pagerank"`
	UsageScore    float64    `json:"usage_score"`
	Confidence    Confidence `json:"confidence"     validate:"required"`
	SubKind       string     `json:"sub_kind,omitempty"`
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
}
