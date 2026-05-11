package proto

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// readFixture loads a single .proto from testdata and returns it with its
// repo-relative path stamped as the source root would record it.
func readFixture(t *testing.T, sub, name string) ([]byte, string, string) {
	t.Helper()
	root := filepath.Join("testdata", sub)
	full := filepath.Join(root, name)
	b, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b, root, full
}

// nodeNames returns every node name with the given type, sorted, so test
// assertions don't depend on emission order.
func nodeNames(nodes []types.Node, nt types.NodeType) []string {
	var out []string
	for _, n := range nodes {
		if n.Type == nt {
			out = append(out, n.Name)
		}
	}
	sort.Strings(out)
	return out
}

// hasNode finds the first node whose (Type, QualifiedName) matches.
func hasNode(nodes []types.Node, nt types.NodeType, qname string) *types.Node {
	for i := range nodes {
		if nodes[i].Type == nt && nodes[i].QualifiedName == qname {
			return &nodes[i]
		}
	}
	return nil
}

// edgeCount returns how many edges of `et` originate from src node ID.
func edgeCount(edges []types.Edge, src string, et types.EdgeType) int {
	n := 0
	for _, e := range edges {
		if e.Src == src && e.Type == et {
			n++
		}
	}
	return n
}

func TestParseFile_Simple(t *testing.T) {
	src, root, full := readFixture(t, "simple", "echo.proto")
	p := New(root)
	r, err := p.ParseFile(full, src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Package node
	if hasNode(r.Nodes, types.NodePackage, "proto:simple") == nil {
		t.Errorf("missing Package node proto:simple; got nodes: %s", debugNodes(r.Nodes))
	}
	// Service → Interface
	svc := hasNode(r.Nodes, types.NodeInterface, "proto:simple.EchoService")
	if svc == nil {
		t.Fatalf("missing Service Interface node; got nodes: %s", debugNodes(r.Nodes))
	}
	if svc.SubKind != "service" {
		t.Errorf("Service SubKind=%q, want service", svc.SubKind)
	}
	// RPC → Method
	rpc := hasNode(r.Nodes, types.NodeMethod, "proto:simple.EchoService.Echo")
	if rpc == nil {
		t.Fatalf("missing rpc Method node")
	}
	if rpc.SubKind != "rpc" {
		t.Errorf("rpc SubKind=%q, want rpc", rpc.SubKind)
	}
	if !strings.Contains(rpc.Signature, "EchoRequest") ||
		!strings.Contains(rpc.Signature, "EchoResponse") {
		t.Errorf("rpc Signature=%q, missing request/response types", rpc.Signature)
	}
	// Service → defines → Method
	if edgeCount(r.Edges, svc.ID, types.EdgeDefines) != 1 {
		t.Errorf("Service should `defines` 1 method, got %d",
			edgeCount(r.Edges, svc.ID, types.EdgeDefines))
	}
	// Messages
	for _, want := range []string{"proto:simple.EchoRequest", "proto:simple.EchoResponse"} {
		if hasNode(r.Nodes, types.NodeMessageType, want) == nil {
			t.Errorf("missing MessageType %s", want)
		}
	}
	// Fields under EchoResponse: message + received_at
	resp := hasNode(r.Nodes, types.NodeMessageType, "proto:simple.EchoResponse")
	if resp == nil {
		t.Fatal("missing EchoResponse")
	}
	if got := edgeCount(r.Edges, resp.ID, types.EdgeDefines); got != 2 {
		t.Errorf("EchoResponse should define 2 fields, got %d", got)
	}
	// Confidence: every emitted node should be EXTRACTED.
	for _, n := range r.Nodes {
		if n.Confidence != types.ConfExtracted {
			t.Errorf("node %s confidence=%s, want EXTRACTED",
				n.QualifiedName, n.Confidence)
		}
	}
	// Language tag.
	for _, n := range r.Nodes {
		if n.Language != languageTag {
			t.Errorf("node %s language=%s, want %s",
				n.QualifiedName, n.Language, languageTag)
		}
	}
}

func TestResolve_UsesType_SameFile(t *testing.T) {
	src, root, full := readFixture(t, "simple", "echo.proto")
	p := New(root)
	r, err := p.ParseFile(full, src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	rg, err := p.Resolve([]*parse.ParseResult{r})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// rpc Echo should `uses_type` EchoRequest and EchoResponse.
	rpc := hasNode(rg.Nodes, types.NodeMethod, "proto:simple.EchoService.Echo")
	if rpc == nil {
		t.Fatal("missing rpc Method")
	}
	uses := 0
	for _, e := range rg.Edges {
		if e.Src == rpc.ID && e.Type == types.EdgeUsesType {
			uses++
			// Same-file → EXTRACTED.
			if e.Confidence != types.ConfExtracted {
				t.Errorf("same-file uses_type confidence=%s, want EXTRACTED",
					e.Confidence)
			}
		}
	}
	if uses != 2 {
		t.Errorf("rpc should have 2 uses_type edges, got %d", uses)
	}
}

func TestParseFile_MultiService(t *testing.T) {
	src, root, full := readFixture(t, "multi_service", "orders.proto")
	p := New(root)
	r, err := p.ParseFile(full, src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	services := nodeNames(r.Nodes, types.NodeInterface)
	wantSvcs := []string{"InventoryService", "OrderService"}
	if !equalStrings(services, wantSvcs) {
		t.Errorf("services=%v, want %v", services, wantSvcs)
	}
	// stream return type retained in signature.
	stream := hasNode(r.Nodes, types.NodeMethod, "proto:shop.orders.OrderService.Stream")
	if stream == nil {
		t.Fatal("missing OrderService.Stream")
	}
	if !strings.Contains(stream.Signature, "stream StreamResponse") {
		t.Errorf("Stream rpc signature missing 'stream StreamResponse': %q",
			stream.Signature)
	}
	// Package qname uses dotted form.
	if hasNode(r.Nodes, types.NodePackage, "proto:shop.orders") == nil {
		t.Errorf("missing dotted Package node proto:shop.orders")
	}
}

func TestParseFile_Nested(t *testing.T) {
	src, root, full := readFixture(t, "nested_messages", "nested.proto")
	p := New(root)
	r, err := p.ParseFile(full, src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// Nested message Inner should have qname Outer.Inner.
	if hasNode(r.Nodes, types.NodeMessageType, "proto:nested.Outer.Inner") == nil {
		t.Errorf("missing nested MessageType proto:nested.Outer.Inner; got: %s",
			debugNodes(r.Nodes))
	}
	// Nested enum Status: proto:nested.Outer.Status
	if hasNode(r.Nodes, types.NodeEnum, "proto:nested.Outer.Status") == nil {
		t.Errorf("missing nested Enum proto:nested.Outer.Status")
	}
	// Top-level enum
	if hasNode(r.Nodes, types.NodeEnum, "proto:nested.TopLevelEnum") == nil {
		t.Errorf("missing top-level Enum proto:nested.TopLevelEnum")
	}
	// Enum values become Field nodes.
	enumVals := 0
	for _, n := range r.Nodes {
		if n.Type == types.NodeField && n.SubKind == "enum_value" {
			enumVals++
		}
	}
	// Outer.Status has 3 + TopLevelEnum has 2 = 5
	if enumVals != 5 {
		t.Errorf("enum values: got %d, want 5", enumVals)
	}
}

func TestParseFile_EdgeCases(t *testing.T) {
	src, root, full := readFixture(t, "edge_cases", "edge.proto")
	p := New(root)
	r, err := p.ParseFile(full, src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// Service + 1 rpc
	if hasNode(r.Nodes, types.NodeInterface, "proto:edge.EdgeService") == nil {
		t.Errorf("missing EdgeService Interface")
	}
	if hasNode(r.Nodes, types.NodeMethod, "proto:edge.EdgeService.Run") == nil {
		t.Errorf("missing rpc Run despite option-laden body")
	}
	// Oneof fields surface as Field nodes (under synthetic oneof_payload prefix).
	for _, want := range []string{
		"proto:edge.RunRequest.oneof_payload.text",
		"proto:edge.RunRequest.oneof_payload.binary",
	} {
		if hasNode(r.Nodes, types.NodeField, want) == nil {
			t.Errorf("missing oneof Field %s", want)
		}
	}
	// Map field surfaces with map<> signature.
	counters := hasNode(r.Nodes, types.NodeField, "proto:edge.RunRequest.counters")
	if counters == nil {
		t.Fatal("missing counters Field")
	}
	if !strings.HasPrefix(counters.Signature, "map<") {
		t.Errorf("counters signature=%q, want map<...>", counters.Signature)
	}
	// optional field still emitted.
	if hasNode(r.Nodes, types.NodeField, "proto:edge.RunRequest.trace_id") == nil {
		t.Errorf("missing optional Field trace_id")
	}
	// Reserved should NOT produce Field nodes (it's a directive, not a field).
	if hasNode(r.Nodes, types.NodeField, "proto:edge.RunRequest.deprecated_field") != nil {
		t.Errorf("reserved name leaked into Field nodes")
	}
}

func TestExtensions(t *testing.T) {
	p := New(".")
	exts := p.Extensions()
	if len(exts) != 1 || exts[0] != ".proto" {
		t.Errorf("Extensions()=%v, want [.proto]", exts)
	}
}

func TestParseFile_MalformedRecoversToEOF(t *testing.T) {
	// Truncated message — parser should not panic, should still produce a
	// File node and the salvageable top-level decls.
	src := []byte(`syntax = "proto3";

package broken;

service Half {
  rpc Oops(InRequest returns OutResponse;
}

message Salvage {
  string id = 1;
}
`)
	p := New(".")
	r, err := p.ParseFile("broken.proto", src)
	if err != nil {
		t.Fatalf("ParseFile must not error on recoverable input: %v", err)
	}
	if hasNode(r.Nodes, types.NodeMessageType, "proto:broken.Salvage") == nil {
		t.Errorf("recovery should preserve later top-level Salvage; nodes=%s",
			debugNodes(r.Nodes))
	}
}

// helpers ──────────────────────────────────────────────────────────────────

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func debugNodes(ns []types.Node) string {
	var sb strings.Builder
	for _, n := range ns {
		sb.WriteString(string(n.Type))
		sb.WriteString("/")
		sb.WriteString(n.QualifiedName)
		sb.WriteString(" ")
	}
	return sb.String()
}

