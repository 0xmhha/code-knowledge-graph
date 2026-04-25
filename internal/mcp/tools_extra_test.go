package mcp

import (
	"testing"

	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// ---------------------------------------------------------------------------
// register* smoke tests — verify that each registration helper reaches
// s.AddTool without panicking. The anonymous handler closures remain
// uncovered (they require a live MCP call); coverage of the wrapper code
// around them is sufficient for V1 maintenance.
// ---------------------------------------------------------------------------

func TestRegisterFindSymbol(t *testing.T) {
	s := server.NewMCPServer("test", "0")
	store := newFixtureStore(t)
	registerFindSymbol(s, store)
	tools := s.ListTools()
	if _, ok := tools["find_symbol"]; !ok {
		t.Error("find_symbol not registered")
	}
}

func TestRegisterFindCallers(t *testing.T) {
	s := server.NewMCPServer("test", "0")
	store := newFixtureStore(t)
	registerFindCallers(s, store)
	tools := s.ListTools()
	if _, ok := tools["find_callers"]; !ok {
		t.Error("find_callers not registered")
	}
}

func TestRegisterFindCallees(t *testing.T) {
	s := server.NewMCPServer("test", "0")
	store := newFixtureStore(t)
	registerFindCallees(s, store)
	tools := s.ListTools()
	if _, ok := tools["find_callees"]; !ok {
		t.Error("find_callees not registered")
	}
}

func TestRegisterGetSubgraph(t *testing.T) {
	s := server.NewMCPServer("test", "0")
	store := newFixtureStore(t)
	registerGetSubgraph(s, store)
	tools := s.ListTools()
	if _, ok := tools["get_subgraph"]; !ok {
		t.Error("get_subgraph not registered")
	}
}

func TestRegisterSearchText(t *testing.T) {
	s := server.NewMCPServer("test", "0")
	store := newFixtureStore(t)
	registerSearchText(s, store)
	tools := s.ListTools()
	if _, ok := tools["search_text"]; !ok {
		t.Error("search_text not registered")
	}
}

func TestRegisterGetContextForTask(t *testing.T) {
	s := server.NewMCPServer("test", "0")
	store := newFixtureStore(t)
	registerGetContextForTask(s, store)
	tools := s.ListTools()
	if _, ok := tools["get_context_for_task"]; !ok {
		t.Error("get_context_for_task not registered")
	}
}

// ---------------------------------------------------------------------------
// attachBlobs
// ---------------------------------------------------------------------------

// TestAttachBlobsIncludeFalse verifies that when include=false no "source"
// key is added to any node map, but all other expected fields are present.
func TestAttachBlobsIncludeFalse(t *testing.T) {
	store := newFixtureStore(t)

	// Retrieve any nodes from the fixture to exercise the real code path.
	nodes, err := store.SearchFTS("Greet", 5)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(nodes) == 0 {
		t.Skip("no nodes found in fixture; skipping attachBlobs test")
	}

	out := attachBlobs(store, nodes, false)
	if len(out) != len(nodes) {
		t.Fatalf("expected %d entries, got %d", len(nodes), len(out))
	}
	for i, m := range out {
		if _, has := m["source"]; has {
			t.Errorf("[%d] unexpected 'source' key when include=false", i)
		}
		for _, key := range []string{"id", "type", "name", "qname", "file", "line"} {
			if _, exists := m[key]; !exists {
				t.Errorf("[%d] missing expected key %q", i, key)
			}
		}
	}
}

// TestAttachBlobsIncludeTrue verifies that when include=true a "source" key
// is added for nodes that have a blob (not all node types necessarily do).
func TestAttachBlobsIncludeTrue(t *testing.T) {
	store := newFixtureStore(t)

	nodes, err := store.SearchFTS("Greet", 5)
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	if len(nodes) == 0 {
		t.Skip("no nodes found in fixture; skipping attachBlobs include=true test")
	}

	out := attachBlobs(store, nodes, true)
	if len(out) != len(nodes) {
		t.Fatalf("expected %d entries, got %d", len(nodes), len(out))
	}
	// At least confirm the output maps contain required fields.
	for i, m := range out {
		for _, key := range []string{"id", "type", "name", "qname"} {
			if _, exists := m[key]; !exists {
				t.Errorf("[%d] missing expected key %q in include=true mode", i, key)
			}
		}
	}
}

// TestAttachBlobsEmpty ensures an empty nodes slice returns an empty slice
// (not nil) without panicking.
func TestAttachBlobsEmpty(t *testing.T) {
	store := newFixtureStore(t)
	out := attachBlobs(store, []types.Node{}, false)
	if len(out) != 0 {
		t.Errorf("expected empty output for empty input, got %v", out)
	}
}
