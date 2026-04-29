package types_test

import (
	"reflect"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func TestAllNodeTypes_Count(t *testing.T) {
	if got, want := len(types.AllNodeTypes()), 30; got != want {
		t.Fatalf("AllNodeTypes count = %d, want %d", got, want)
	}
}

func TestAllEdgeTypes_Count(t *testing.T) {
	if got, want := len(types.AllEdgeTypes()), 25; got != want {
		t.Fatalf("AllEdgeTypes count = %d, want %d", got, want)
	}
}

func TestAllNodeTypes_Contains(t *testing.T) {
	want := types.NodeMutex
	for _, n := range types.AllNodeTypes() {
		if n == want {
			return
		}
	}
	t.Fatalf("AllNodeTypes missing %q", want)
}

func TestAllEdgeTypes_Contains(t *testing.T) {
	wants := []types.EdgeType{
		types.EdgeAcquiresLock, types.EdgeReleasesLock, types.EdgeAccessedUnderLock,
	}
	have := make(map[types.EdgeType]struct{}, len(types.AllEdgeTypes()))
	for _, e := range types.AllEdgeTypes() {
		have[e] = struct{}{}
	}
	for _, w := range wants {
		if _, ok := have[w]; !ok {
			t.Errorf("AllEdgeTypes missing %q", w)
		}
	}
}

// TestAllNodeTypes_Stable locks down the order of the existing entries so a
// future schema bump can't accidentally reorder them and invalidate
// hash-derived IDs / cached test snapshots. Append-only is the contract;
// NodeMutex was inserted right after NodeChannel (concurrency family).
func TestAllNodeTypes_Stable(t *testing.T) {
	want := []types.NodeType{
		types.NodePackage, types.NodeFile, types.NodeStruct, types.NodeInterface, types.NodeClass,
		types.NodeTypeAlias, types.NodeEnum, types.NodeContract, types.NodeMapping, types.NodeEvent,
		types.NodeFunction, types.NodeMethod, types.NodeModifier, types.NodeConstructor,
		types.NodeConstant, types.NodeVariable, types.NodeField, types.NodeParameter, types.NodeLocalVariable,
		types.NodeImport, types.NodeExport, types.NodeDecorator,
		types.NodeGoroutine, types.NodeChannel, types.NodeMutex,
		types.NodeIfStmt, types.NodeLoopStmt, types.NodeCallSite, types.NodeReturnStmt, types.NodeSwitchStmt,
	}
	if !reflect.DeepEqual(types.AllNodeTypes(), want) {
		t.Fatalf("AllNodeTypes order changed:\n got=%v\nwant=%v", types.AllNodeTypes(), want)
	}
}

// TestAllEdgeTypes_Stable mirrors the node-stability check for edges.
// Lock edges are appended at the end — never interleaved.
func TestAllEdgeTypes_Stable(t *testing.T) {
	want := []types.EdgeType{
		types.EdgeContains, types.EdgeDefines, types.EdgeCalls, types.EdgeInvokes, types.EdgeUsesType,
		types.EdgeInstantiates, types.EdgeReferences, types.EdgeReadsField, types.EdgeWritesField,
		types.EdgeImports, types.EdgeExports, types.EdgeImplements, types.EdgeExtends,
		types.EdgeHasModifier, types.EdgeEmitsEvent, types.EdgeReadsMapping, types.EdgeWritesMapping,
		types.EdgeHasDecorator, types.EdgeSpawns, types.EdgeSendsTo, types.EdgeRecvsFrom, types.EdgeBindsTo,
		types.EdgeAcquiresLock, types.EdgeReleasesLock, types.EdgeAccessedUnderLock,
	}
	if !reflect.DeepEqual(types.AllEdgeTypes(), want) {
		t.Fatalf("AllEdgeTypes order changed:\n got=%v\nwant=%v", types.AllEdgeTypes(), want)
	}
}

func TestConfidenceValid(t *testing.T) {
	for _, c := range []types.Confidence{types.ConfExtracted, types.ConfInferred, types.ConfAmbiguous} {
		if !c.Valid() {
			t.Errorf("Confidence(%q) should be valid", c)
		}
	}
	if types.Confidence("BOGUS").Valid() {
		t.Error("Confidence(BOGUS) should be invalid")
	}
}
