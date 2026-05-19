package solidity_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W8 V8 — function pointer propagation marker. fires when a
// callable assigns a fn-typed value to a state var, or passes it
// as an argument to another call, WITHOUT invoking it.
// HasFunctionPointerCall covers invocation; V8 covers the
// orthogonal propagation axis. assignTo / forwardArg / register
// all propagate; invokeOnly invokes (so V8 marker stays false
// there).
func TestFunctionTypedVar_PropagationMarker(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var", "propagation.sol")

	want := map[string]struct {
		propagation bool
		invocation  bool
	}{
		"Propagator.assignTo":   {propagation: true, invocation: false},
		"Propagator.forwardArg": {propagation: true, invocation: false},
		"Propagator.register":   {propagation: true, invocation: false},
		"Propagator.invokeOnly": {propagation: false, invocation: true},
	}
	got := map[string]struct {
		propagation bool
		invocation  bool
	}{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = struct {
				propagation bool
				invocation  bool
			}{n.HasFunctionPointerPropagation, n.HasFunctionPointerCall}
		}
	}
	for qn, w := range want {
		g := got[qn]
		if g.propagation != w.propagation {
			t.Errorf("%s HasFunctionPointerPropagation: got %v want %v", qn, g.propagation, w.propagation)
		}
		if g.invocation != w.invocation {
			t.Errorf("%s HasFunctionPointerCall: got %v want %v", qn, g.invocation, w.invocation)
		}
	}
}

// W-C W8 V7 — inherited function-typed state-var invocation.
// Hub extends Base; Base declares `onAction`. Caller does
// `h.onAction(x)` where h is Hub-typed. Pre-V7 the lookup missed
// because fnTypedFields only had Base, not Hub. V7 walks Hub's
// C3 MRO so the marker fires.
func TestFunctionTypedVar_InheritedCrossContractCall(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var", "inherited_cross_contract.sol")

	var got bool
	for _, n := range nodes {
		if n.QualifiedName == "Caller.trigger" && n.Type == types.NodeFunction {
			got = n.HasFunctionPointerCall
			break
		}
	}
	if !got {
		t.Errorf("HasFunctionPointerCall on Caller.trigger: got false, want true (inherited fn-typed state-var)")
	}
}

// W-C W8 V6 — HasFunctionPointerCall fires on cross-contract
// function-pointer invocations: `h.onAction(x)` where `h` is a
// state-var of type Hub and Hub.onAction is a function-typed
// NodeField. Pass 2 walks the receiver type chain, finds the
// fn-typed field on the other contract, and marks the caller.
func TestFunctionTypedVar_CrossContractCall(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var", "cross_contract_call.sol")

	want := map[string]bool{
		"Caller.trigger": true,
		"Caller.noop":    false,
		// Hub.setHook declares a fn-typed param `cb` but only
		// assigns it to a state-var — never invokes it. V4/V5/V6
		// look for invocations, so HasFunctionPointerCall stays
		// false. (HasFunctionTypedVar would still be true.)
		"Hub.setHook": false,
	}
	got := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = n.HasFunctionPointerCall
		}
	}
	for qn, w := range want {
		if got[qn] != w {
			t.Errorf("HasFunctionPointerCall on %q: got %v want %v", qn, got[qn], w)
		}
	}
}

// W-C W8 V5 — HasFunctionPointerCall fires on calls to function-
// typed state variables (`onAction(x)` where onAction is a state-
// var of function type). Extends V4 (bare-identifier param/local
// invocations) to contract-scope state variables.
func TestFunctionTypedVar_StateVarPointerCall(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var", "state_var_call.sol")

	want := map[string]bool{
		"Hooked.trigger":     true,  // onAction(x) — fn-typed state var call
		"Hooked.passthrough": false, // no fn-pointer invocation
	}
	got := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = n.HasFunctionPointerCall
		}
	}
	for qn, w := range want {
		if got[qn] != w {
			t.Errorf("HasFunctionPointerCall on %q: got %v want %v", qn, got[qn], w)
		}
	}
}

// W-C W8 V4 — HasFunctionPointerCall marker. True when the callable
// invokes a function-typed parameter or local via a bare-identifier
// call_expression (`local(args)`). Complements HasFunctionTypedVar
// which marks declarations.
func TestFunctionTypedVar_PointerCallMarker(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var", "dispatcher.sol")

	want := map[string]bool{
		"Dispatcher.runWithCallback": true,  // cb(x)
		"Dispatcher.pickAndRun":      true,  // local(x)
		"Dispatcher.chooseFn":        false, // declares but never invokes
		"Dispatcher.plain":           false,
	}

	got := map[string]bool{}
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; !ok {
			continue
		}
		seen[n.QualifiedName] = true
		got[n.QualifiedName] = n.HasFunctionPointerCall
	}

	for qn, w := range want {
		if !seen[qn] {
			t.Errorf("missing NodeFunction %q", qn)
			continue
		}
		if got[qn] != w {
			t.Errorf("NodeFunction %q HasFunctionPointerCall: got %v, want %v", qn, got[qn], w)
		}
	}
}

// W-C W8 V3 — function-typed parameter / local marker. Extends W8 V2
// (state-var) to callables that own a function-typed parameter or
// local variable. The marker is presence-only: V0 dispatch resolution
// does not follow indirect calls through function pointers.
func TestFunctionTypedVar_CallableMarker(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var", "dispatcher.sol")

	want := map[string]bool{
		"Dispatcher.runWithCallback": true,
		"Dispatcher.pickAndRun":      true,
		"Dispatcher.chooseFn":        true,
		"Dispatcher.plain":           false,
	}

	got := map[string]bool{}
	seen := map[string]bool{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; !ok {
			continue
		}
		seen[n.QualifiedName] = true
		got[n.QualifiedName] = n.HasFunctionTypedVar
	}

	for qn, w := range want {
		if !seen[qn] {
			t.Errorf("missing NodeFunction %q", qn)
			continue
		}
		if got[qn] != w {
			t.Errorf("NodeFunction %q HasFunctionTypedVar: got %v, want %v", qn, got[qn], w)
		}
	}
}
