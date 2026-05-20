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

// W-C W8 V15 — modifier-scope function-typed local audit. The
// V3 marker query targets every (parameter)/(variable_declaration)
// node and walks up via nearestFunctionQnameAndStart, which W6
// V1.22 extended to recognise modifier_definition. runDecl emits
// NodeModifier with the same (qname, startByte) pair that
// parse.MakeID consumes, so the marker's affected[id] write
// should land on the NodeModifier row's HasFunctionTypedVar
// field.
//
// V15 locks the contract. A regression that excludes
// modifier_definition from nearestFunctionQnameAndStart, or that
// emits NodeModifier with a different startByte than the
// identifier's start, would silently drop coverage on every
// modifier body — a security-relevant blind spot since modifiers
// frequently route auth/reentrancy checks.
func TestFunctionTypedVar_ModifierScope(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var", "modifier_scope.sol")

	var mod types.Node
	for _, n := range nodes {
		if n.QualifiedName == "ModHolder.withFnLocal" && n.Type == types.NodeModifier {
			mod = n
			break
		}
	}
	if mod.ID == "" {
		t.Fatalf("ModHolder.withFnLocal not indexed as NodeModifier")
	}
	if !mod.HasFunctionTypedVar {
		t.Errorf("ModHolder.withFnLocal HasFunctionTypedVar: got false, want true")
	}
}

// W-C W8 V14 — function-of-function-pointer audit. A state-var
// typed as `function(...) returns (function(...) returns (...))`
// is itself a function pointer whose return value is another
// function pointer. typeNameIsFunctionTyped checks the outer
// type_name's direct children for parameter/return_parameter
// nodes, so the marker fires for the outer level regardless of
// what the return-parameter's inner type happens to be.
//
// V14 locks two properties:
//
//  1. The state-var IsFunctionTyped=true.
//  2. A function declaring such a local has HasFunctionTypedVar=true.
//
// Without this lock, a future tightening of typeNameIsFunctionTyped
// (e.g. requiring the return-parameter to NOT be a function type
// so the outer becomes a "value-returning function only" form)
// would silently drop coverage for higher-order-style Sol.
func TestFunctionTypedVar_NestedReturnTypeFunctionPointer(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var", "fn_pointer_nested.sol")

	var field types.Node
	for _, n := range nodes {
		if n.QualifiedName == "Nested.higherOrder" && n.Type == types.NodeField {
			field = n
			break
		}
	}
	if field.ID == "" {
		t.Fatalf("Nested.higherOrder not indexed")
	}
	if !field.IsFunctionTyped {
		t.Errorf("Nested.higherOrder IsFunctionTyped: got false, want true")
	}

	var fn types.Node
	for _, n := range nodes {
		if n.QualifiedName == "Nested.withNestedLocal" && n.Type == types.NodeFunction {
			fn = n
			break
		}
	}
	if fn.ID == "" {
		t.Fatalf("Nested.withNestedLocal not indexed")
	}
	if !fn.HasFunctionTypedVar {
		t.Errorf("Nested.withNestedLocal HasFunctionTypedVar: got false, want true")
	}
}

// W-C W8 V13 — mapping value as function pointer. The state-var
// `mapping(address => function(...)) handlers` is emitted as a
// NodeMapping (mapping's own node type) rather than NodeField,
// but the IsFunctionTyped flag still applies — V13 drops the
// V2 `!isMapping` guard so the same shared
// typeNameIsFunctionTyped check fires on the mapping outer
// type_name (its nested type_name child carries the function
// signature).
func TestFunctionTypedVar_MappingValueFunctionPointer(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var", "fn_pointer_mapping.sol")

	// NodeMapping uses a `<name>:mapping` QualifiedName shape;
	// match by Name + Type to avoid the shape detail.
	var mapping types.Node
	for _, n := range nodes {
		if n.Name == "handlers" && n.Type == types.NodeMapping {
			mapping = n
			break
		}
	}
	if mapping.ID == "" {
		t.Fatalf("handlers mapping not indexed as NodeMapping")
	}
	if !mapping.IsFunctionTyped {
		t.Errorf("IsFunctionTyped on fn-typed mapping: got false, want true")
	}
}

// W-C W8 V12 — function pointer array marker. typeNameIsFunctionTyped
// now recurses into array_type wrappers so
// `function(uint256)[] handlers` lights up the same markers as
// the scalar form. The state-var gets IsFunctionTyped=true and a
// callable declaring an array-of-fn local lights up
// HasFunctionTypedVar.
func TestFunctionTypedVar_FunctionPointerArray(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var", "fn_pointer_array.sol")

	// (a) state-var field carries IsFunctionTyped.
	var field types.Node
	for _, n := range nodes {
		if n.QualifiedName == "ArrayHolder.handlers" && n.Type == types.NodeField {
			field = n
			break
		}
	}
	if field.ID == "" {
		t.Fatalf("ArrayHolder.handlers not indexed")
	}
	if !field.IsFunctionTyped {
		t.Errorf("ArrayHolder.handlers IsFunctionTyped: got false, want true")
	}

	// (b) function declaring the array-of-fn local lights up
	// HasFunctionTypedVar.
	var fn types.Node
	for _, n := range nodes {
		if n.QualifiedName == "ArrayHolder.captureArray" && n.Type == types.NodeFunction {
			fn = n
			break
		}
	}
	if fn.ID == "" {
		t.Fatalf("ArrayHolder.captureArray not indexed")
	}
	if !fn.HasFunctionTypedVar {
		t.Errorf("ArrayHolder.captureArray HasFunctionTypedVar: got false, want true")
	}
}

// W-C W8 V11 — try/catch with fn-typed returns parameter
// propagation. `try provider.getCallback() returns (function(...)
// cb) { handler = cb; }` extracts a function pointer from a try-
// block's returns clause and propagates it to a state variable.
// V3 already marks HasFunctionTypedVar (the cb declaration goes
// through emitTryReturnsBinding and the parameter node lives in
// the function's body), and the V8 propagation walker catches
// the `handler = cb` assignment. V11 audits the full
// declaration -> propagation chain through the try-block scope.
func TestFunctionTypedVar_TryCatchPropagation(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var", "try_catch_prop.sol")

	want := map[string]struct {
		typedVar, propagation, invocation bool
	}{
		"Caller.captureCallback": {typedVar: true, propagation: true, invocation: false},
	}
	got := map[string]struct {
		typedVar, propagation, invocation bool
	}{}
	for _, n := range nodes {
		if n.Type != types.NodeFunction {
			continue
		}
		if _, ok := want[n.QualifiedName]; ok {
			got[n.QualifiedName] = struct {
				typedVar, propagation, invocation bool
			}{n.HasFunctionTypedVar, n.HasFunctionPointerPropagation, n.HasFunctionPointerCall}
		}
	}
	for qn, w := range want {
		g := got[qn]
		if g.typedVar != w.typedVar {
			t.Errorf("%s HasFunctionTypedVar: got %v want %v", qn, g.typedVar, w.typedVar)
		}
		if g.propagation != w.propagation {
			t.Errorf("%s HasFunctionPointerPropagation: got %v want %v", qn, g.propagation, w.propagation)
		}
		if g.invocation != w.invocation {
			t.Errorf("%s HasFunctionPointerCall: got %v want %v", qn, g.invocation, w.invocation)
		}
	}
}

// W-C W8 V10 — emit-statement propagation. `emit Event(handler)`
// logs the fn-typed value to chain — propagation surface that
// the V8 assignment / argument and V9 return passes don't cover.
// HasFunctionPointerPropagation=true on the emitting function;
// HasFunctionPointerCall=false (no invocation).
func TestFunctionTypedVar_EmitPropagation(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var", "emit_prop.sol")

	want := map[string]struct {
		propagation bool
		invocation  bool
	}{
		"EventEmitter.logHandler": {propagation: true, invocation: false},
		"EventEmitter.logPlain":   {propagation: false, invocation: false},
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

// W-C W8 V9 — return-position propagation. `return cb;` where cb
// is a fn-typed param/local/state-var counts as propagation
// (HasFunctionPointerPropagation=true), parallel to the V8
// assignment / call-argument paths. The function does not invoke
// the pointer so HasFunctionPointerCall stays false.
func TestFunctionTypedVar_ReturnPropagation(t *testing.T) {
	nodes, _ := parseResolveOneSol(t, "testdata/function_typed_var", "return_prop.sol")

	want := map[string]struct {
		propagation bool
		invocation  bool
	}{
		"ReturnProp.returnCb":     {propagation: true, invocation: false},
		"ReturnProp.returnStored": {propagation: true, invocation: false},
		"ReturnProp.noReturn":     {propagation: false, invocation: false},
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
