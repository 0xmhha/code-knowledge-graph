package golang_test

import (
	"strings"
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestUsesType_FuncParamsAndResults asserts uses_type edges flow from a
// function's signature to every distinct named type it references.
func TestUsesType_FuncParamsAndResults(t *testing.T) {
	root := "testdata/uses_type"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	usesType := edgesByType(g.Edges, types.EdgeUsesType)
	if len(usesType) == 0 {
		t.Fatal("expected uses_type edges; got 0")
	}
	// Find Process function ID + the named type IDs it should reference.
	idByQname := map[string]string{}
	for _, n := range g.Nodes {
		idByQname[n.QualifiedName] = n.ID
	}
	processID := idByQname["usestype_fixture.Process"]
	if processID == "" {
		t.Fatal("Process function node not found")
	}
	wantTargets := []string{
		"usestype_fixture.Config",
		"usestype_fixture.Logger",
		"usestype_fixture.Result",
	}
	for _, target := range wantTargets {
		targetID := idByQname[target]
		if targetID == "" {
			t.Errorf("expected node for %s; missing", target)
			continue
		}
		var found bool
		for _, e := range usesType {
			if e.Src == processID && e.Dst == targetID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected uses_type(Process -> %s); not emitted", target)
		}
	}
}

// TestUsesType_StructFieldTypes asserts struct field types produce uses_type
// edges from the Struct node to the field's type.
func TestUsesType_StructFieldTypes(t *testing.T) {
	root := "testdata/uses_type"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	usesType := edgesByType(g.Edges, types.EdgeUsesType)
	idByQname := map[string]string{}
	for _, n := range g.Nodes {
		idByQname[n.QualifiedName] = n.ID
	}
	configID := idByQname["usestype_fixture.Config"]
	counterID := idByQname["usestype_fixture.Counter"]
	if configID == "" || counterID == "" {
		t.Fatal("Config / Counter nodes missing")
	}
	var found bool
	for _, e := range usesType {
		if e.Src == configID && e.Dst == counterID {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected uses_type(Config -> Counter) for the *Counter field")
	}
}

// TestInvokes_DispatchKind asserts that interface dispatch / func values /
// closures emit `invokes` edges with the right dispatch_kind metadata.
func TestInvokes_DispatchKind(t *testing.T) {
	root := "testdata/uses_type"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	invokes := edgesByType(g.Edges, types.EdgeInvokes)
	if len(invokes) == 0 {
		t.Fatal("expected invokes edges; got 0")
	}
	byKind := map[string]int{}
	for _, e := range invokes {
		byKind[e.DispatchKind]++
	}
	// At minimum we expect:
	//   interface_method: 1 (Process calls l.Log)
	//   closure:         1 (invokeClosure)
	//   method_value:    1 (FireCallback dispatches via h.cb)
	//   func_value:      1 (callFuncValue dispatches fn)
	for _, want := range []string{"interface_method", "closure", "method_value", "func_value"} {
		if byKind[want] < 1 {
			t.Errorf("dispatch_kind=%q: got %d invokes edges, want >=1", want, byKind[want])
		}
	}
}

// TestInstantiates_CompositeAndNew asserts that struct literals and new(T)
// emit instantiates edges from the enclosing function to the target type.
func TestInstantiates_CompositeAndNew(t *testing.T) {
	root := "testdata/uses_type"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	inst := edgesByType(g.Edges, types.EdgeInstantiates)
	if len(inst) == 0 {
		t.Fatal("expected instantiates edges; got 0")
	}
	idByQname := map[string]string{}
	for _, n := range g.Nodes {
		idByQname[n.QualifiedName] = n.ID
	}
	processID := idByQname["usestype_fixture.Process"]
	resultID := idByQname["usestype_fixture.Result"]
	makeCounterID := idByQname["usestype_fixture.MakeCounter"]
	counterID := idByQname["usestype_fixture.Counter"]
	if processID == "" || resultID == "" || makeCounterID == "" || counterID == "" {
		t.Fatal("expected Process / Result / MakeCounter / Counter nodes")
	}
	var processInstResult, makeCounterInst bool
	for _, e := range inst {
		if e.Src == processID && e.Dst == resultID {
			processInstResult = true
		}
		if e.Src == makeCounterID && e.Dst == counterID {
			makeCounterInst = true
		}
	}
	if !processInstResult {
		t.Error("expected instantiates(Process -> Result) for `Result{OK: true}` literal")
	}
	if !makeCounterInst {
		t.Error("expected instantiates(MakeCounter -> Counter) for `new(Counter)`")
	}
}

// TestUsesType_NoSelfLoop asserts a function's uses_type edges never point
// back at the function's own enclosing type (self-reference guard).
func TestUsesType_NoSelfLoop(t *testing.T) {
	root := "testdata/uses_type"
	g, err := gop.LoadAndResolve(root)
	if err != nil {
		t.Fatalf("LoadAndResolve: %v", err)
	}
	usesType := edgesByType(g.Edges, types.EdgeUsesType)
	for _, e := range usesType {
		if e.Src == e.Dst {
			n := findNodeByID(g.Nodes, e.Src)
			qn := ""
			if n != nil {
				qn = n.QualifiedName
			}
			t.Errorf("self-loop uses_type edge: src=dst=%s (%s)", e.Src, qn)
		}
	}
	// Sanity: at least one struct-field-typed edge should exist (sanity that
	// the test corpus actually exercises both branches).
	var anyStructSrc bool
	for _, e := range usesType {
		n := findNodeByID(g.Nodes, e.Src)
		if n != nil && strings.HasSuffix(n.QualifiedName, ".Config") {
			anyStructSrc = true
			break
		}
	}
	if !anyStructSrc {
		t.Error("expected at least one uses_type edge with a Struct (Config) as src")
	}
}
