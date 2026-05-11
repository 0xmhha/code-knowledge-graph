package types_test

import (
	"reflect"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func TestAllNodeTypes_Count(t *testing.T) {
	if got, want := len(types.AllNodeTypes()), 34; got != want {
		t.Fatalf("AllNodeTypes count = %d, want %d", got, want)
	}
}

func TestAllEdgeTypes_Count(t *testing.T) {
	if got, want := len(types.AllEdgeTypes()), 38; got != want {
		t.Fatalf("AllEdgeTypes count = %d, want %d", got, want)
	}
}

func TestAllNodeTypes_Contains(t *testing.T) {
	wants := []types.NodeType{
		types.NodeMutex, types.NodeEndpoint, types.NodeMessageType,
		types.NodeCommit, types.NodeHunk,
	}
	have := make(map[types.NodeType]struct{}, len(types.AllNodeTypes()))
	for _, n := range types.AllNodeTypes() {
		have[n] = struct{}{}
	}
	for _, w := range wants {
		if _, ok := have[w]; !ok {
			t.Errorf("AllNodeTypes missing %q", w)
		}
	}
}

func TestAllEdgeTypes_Contains(t *testing.T) {
	wants := []types.EdgeType{
		types.EdgeAcquiresLock, types.EdgeReleasesLock, types.EdgeAccessedUnderLock,
		types.EdgeListensOn, types.EdgeHandlesMessage, types.EdgeRPCCalls,
		types.EdgeChangedIn, types.EdgeBlame,
		types.EdgeTimeoutPath, types.EdgeCancellationPath,
		types.EdgeHasHunk, types.EdgeAdjacent, types.EdgeModifies,
		types.EdgeHTTPCalls,
		types.EdgeGRPCListensOn, types.EdgeGRPCCalls,
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
// hash-derived IDs / cached test snapshots. Identifier names are append-only
// in spirit; NodeMutex was inserted at index 24 to keep the concurrency
// family contiguous, which shifted statement nodes from 24-28 to 25-29 —
// the test snapshot below records the post-A5 reality. Future additions
// should prefer append over insert when no grouping argument applies.
// E3 appended NodeEndpoint + NodeMessageType at indices 30-31.
// E4 appended NodeCommit at index 32 (CKS G6 Temporal — git history).
// H1 appended NodeHunk at index 33 (CKS G6 Temporal extension — schema 1.8).
func TestAllNodeTypes_Stable(t *testing.T) {
	want := []types.NodeType{
		types.NodePackage, types.NodeFile, types.NodeStruct, types.NodeInterface, types.NodeClass,
		types.NodeTypeAlias, types.NodeEnum, types.NodeContract, types.NodeMapping, types.NodeEvent,
		types.NodeFunction, types.NodeMethod, types.NodeModifier, types.NodeConstructor,
		types.NodeConstant, types.NodeVariable, types.NodeField, types.NodeParameter, types.NodeLocalVariable,
		types.NodeImport, types.NodeExport, types.NodeDecorator,
		types.NodeGoroutine, types.NodeChannel, types.NodeMutex,
		types.NodeIfStmt, types.NodeLoopStmt, types.NodeCallSite, types.NodeReturnStmt, types.NodeSwitchStmt,
		types.NodeEndpoint, types.NodeMessageType,
		types.NodeCommit,
		types.NodeHunk,
	}
	if !reflect.DeepEqual(types.AllNodeTypes(), want) {
		t.Fatalf("AllNodeTypes order changed:\n got=%v\nwant=%v", types.AllNodeTypes(), want)
	}
}

// TestAllEdgeTypes_Stable mirrors the node-stability check for edges.
// Lock edges, distributed edges, temporal edges, context-path edges,
// hunk-graph edges, the W2 http_calls edge, and the W3b grpc_listens_on /
// grpc_calls edges are appended at the end — never interleaved.
func TestAllEdgeTypes_Stable(t *testing.T) {
	want := []types.EdgeType{
		types.EdgeContains, types.EdgeDefines, types.EdgeCalls, types.EdgeInvokes, types.EdgeUsesType,
		types.EdgeInstantiates, types.EdgeReferences, types.EdgeReadsField, types.EdgeWritesField,
		types.EdgeImports, types.EdgeExports, types.EdgeImplements, types.EdgeExtends,
		types.EdgeHasModifier, types.EdgeEmitsEvent, types.EdgeReadsMapping, types.EdgeWritesMapping,
		types.EdgeHasDecorator, types.EdgeSpawns, types.EdgeSendsTo, types.EdgeRecvsFrom, types.EdgeBindsTo,
		types.EdgeAcquiresLock, types.EdgeReleasesLock, types.EdgeAccessedUnderLock,
		types.EdgeListensOn, types.EdgeHandlesMessage, types.EdgeRPCCalls,
		types.EdgeChangedIn, types.EdgeBlame,
		types.EdgeTimeoutPath, types.EdgeCancellationPath,
		types.EdgeHasHunk, types.EdgeAdjacent, types.EdgeModifies,
		types.EdgeHTTPCalls,
		types.EdgeGRPCListensOn, types.EdgeGRPCCalls,
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
