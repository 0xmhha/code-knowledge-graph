// Package persist — node_attrs.go (W-C W11 V7, 2026-05-19) bridges
// the per-node JSON-blob `attrs` column to the type fields on
// types.Node that have no dedicated SQLite column. Adding a new
// marker on types.Node only requires a new field in this struct
// — the schema stays put.
//
// Marshalling rule: every nodeAttrs field is annotated `,omitempty`
// so a Node with no markers serialises to `{}` (or NULL if the
// column was never touched) and consumes minimum space. Empty
// attrs strings produced by old (pre-1.9) writers parse back to
// the zero-valued struct, which makes incremental DB upgrades
// safe.
package persist

import (
	"encoding/json"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// nodeAttrs mirrors every types.Node field that doesn't have its
// own SQLite column. Tags match the JSON tags on types.Node so
// downstream tooling can deserialise the column directly.
type nodeAttrs struct {
	SlotIndex                     int      `json:"slot_index,omitempty"`
	HasAssembly                   bool     `json:"has_assembly,omitempty"`
	HasLowLevelCall               bool     `json:"has_low_level_call,omitempty"`
	HasValueTransfer              bool     `json:"has_value_transfer,omitempty"`
	YulBuiltins                   []string `json:"yul_builtins,omitempty"`
	IsFunctionTyped               bool     `json:"is_function_typed,omitempty"`
	HasFunctionTypedVar           bool     `json:"has_function_typed_var,omitempty"`
	HasFunctionPointerCall        bool     `json:"has_function_pointer_call,omitempty"`
	HasExternalCall               bool     `json:"has_external_call,omitempty"`
	HasInheritanceMROFallback     bool     `json:"has_inheritance_mro_fallback,omitempty"`
	HasFunctionPointerPropagation bool     `json:"has_function_pointer_propagation,omitempty"`
	HasSelfReentrantCall          bool     `json:"has_self_reentrant_call,omitempty"`
	HasSelfDelegatecallDead       bool     `json:"has_self_delegatecall_dead,omitempty"`
}

// marshalNodeAttrs serialises a Node's marker surface into the
// JSON blob stored in nodes.attrs. Returns "" when every marker is
// at its zero value so the column stays NULL — keeps DB diffs
// minimal for the common case where a node has no markers.
func marshalNodeAttrs(n *types.Node) string {
	a := nodeAttrs{
		SlotIndex:                     n.SlotIndex,
		HasAssembly:                   n.HasAssembly,
		HasLowLevelCall:               n.HasLowLevelCall,
		HasValueTransfer:              n.HasValueTransfer,
		YulBuiltins:                   n.YulBuiltins,
		IsFunctionTyped:               n.IsFunctionTyped,
		HasFunctionTypedVar:           n.HasFunctionTypedVar,
		HasFunctionPointerCall:        n.HasFunctionPointerCall,
		HasExternalCall:               n.HasExternalCall,
		HasInheritanceMROFallback:     n.HasInheritanceMROFallback,
		HasFunctionPointerPropagation: n.HasFunctionPointerPropagation,
		HasSelfReentrantCall:          n.HasSelfReentrantCall,
		HasSelfDelegatecallDead:       n.HasSelfDelegatecallDead,
	}
	if isZeroAttrs(a) {
		return ""
	}
	buf, err := json.Marshal(&a)
	if err != nil {
		return ""
	}
	return string(buf)
}

// unmarshalNodeAttrs populates the Node's marker fields from the
// JSON blob stored in nodes.attrs. Empty / missing blobs leave the
// fields at their zero values (no-op).
func unmarshalNodeAttrs(blob string, n *types.Node) {
	if blob == "" {
		return
	}
	var a nodeAttrs
	if err := json.Unmarshal([]byte(blob), &a); err != nil {
		return
	}
	n.SlotIndex = a.SlotIndex
	n.HasAssembly = a.HasAssembly
	n.HasLowLevelCall = a.HasLowLevelCall
	n.HasValueTransfer = a.HasValueTransfer
	n.YulBuiltins = a.YulBuiltins
	n.IsFunctionTyped = a.IsFunctionTyped
	n.HasFunctionTypedVar = a.HasFunctionTypedVar
	n.HasFunctionPointerCall = a.HasFunctionPointerCall
	n.HasExternalCall = a.HasExternalCall
	n.HasInheritanceMROFallback = a.HasInheritanceMROFallback
	n.HasFunctionPointerPropagation = a.HasFunctionPointerPropagation
	n.HasSelfReentrantCall = a.HasSelfReentrantCall
	n.HasSelfDelegatecallDead = a.HasSelfDelegatecallDead
}

func isZeroAttrs(a nodeAttrs) bool {
	return a.SlotIndex == 0 &&
		!a.HasAssembly &&
		!a.HasLowLevelCall &&
		!a.HasValueTransfer &&
		len(a.YulBuiltins) == 0 &&
		!a.IsFunctionTyped &&
		!a.HasFunctionTypedVar &&
		!a.HasFunctionPointerCall &&
		!a.HasExternalCall &&
		!a.HasInheritanceMROFallback &&
		!a.HasFunctionPointerPropagation &&
		!a.HasSelfReentrantCall &&
		!a.HasSelfDelegatecallDead
}
