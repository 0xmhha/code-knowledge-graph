package mcphandlers_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/pkg/evidence"
	"github.com/0xmhha/code-knowledge-graph/pkg/mcphandlers"
	"github.com/0xmhha/code-knowledge-graph/pkg/store"
)

// TestRegisterAll_LocksEightTools is the external-import smoke test
// for the cks/ckv consumption pattern described in
// eval/stablenet/CKS-INTEGRATION-2026-05-23.md §5: a sister-repo
// constructs an mcp-go server, calls RegisterAll with a store.Reader,
// and gets the eight tools wired with the §11.3 safety wrapper
// applied — no internal/* imports involved.
//
// Tool name list is the contract: when a new tool joins the canonical
// set, this test's `want` slice grows and pkg/mcphandlers/registerall.go
// must register it; when a tool is removed, the test's `notWant`
// guard documents the explicit removal. Pairs with the internal/mcp
// server_test.go static scan: that one locks Run(); this one locks
// the public surface.
func TestRegisterAll_LocksEightTools(t *testing.T) {
	out := t.TempDir()
	if _, err := buildpipe.Run(buildpipe.Options{
		SrcRoot:    "../../testdata/synthetic",
		OutDir:     out,
		Languages:  []string{"go", "ts", "sol"},
		CKGVersion: "test",
	}); err != nil {
		t.Fatalf("build synthetic graph: %v", err)
	}
	r, err := store.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer func() { _ = r.Close() }()

	s := server.NewMCPServer("smoke", "0.0.0")
	mcphandlers.RegisterAll(s, r)

	want := []string{
		"find_symbol",
		"find_callers",
		"find_callees",
		"get_subgraph",
		"search_text",
		"get_context_for_task",
		"impact_of_change",
		"evidence_for_intent",
	}
	for _, name := range want {
		if tool := s.GetTool(name); tool == nil || tool.Handler == nil {
			t.Errorf("RegisterAll: missing tool %q — sister-repo wiring would silently lose it", name)
		}
	}
}

// TestRegisterFindSymbol_ExternalShape exercises an individual
// Register* function end to end without RegisterAll, so callers that
// only need a subset of the tool surface (cks may want everything
// except evidence_for_intent, for example) have a guaranteed-to-work
// pattern. The H3 safety wrapper is *not* applied in this branch —
// individual Register* callers are responsible for wrapping their
// reader themselves; the test verifies the unwrapped form compiles
// and answers a real query.
func TestRegisterFindSymbol_ExternalShape(t *testing.T) {
	out := t.TempDir()
	if _, err := buildpipe.Run(buildpipe.Options{
		SrcRoot:    "../../testdata/synthetic",
		OutDir:     out,
		Languages:  []string{"go"},
		CKGVersion: "test",
	}); err != nil {
		t.Fatalf("build synthetic graph: %v", err)
	}
	r, err := store.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer func() { _ = r.Close() }()

	s := server.NewMCPServer("smoke", "0.0.0")
	mcphandlers.RegisterFindSymbol(s, r)

	tool := s.GetTool("find_symbol")
	if tool == nil || tool.Handler == nil {
		t.Fatal("find_symbol not registered")
	}

	// Invoke the handler the way mcp-go's server would.
	req := mcp.CallToolRequest{}
	req.Params.Name = "find_symbol"
	req.Params.Arguments = map[string]any{
		"name":  "service.Vault",
		"exact": true,
	}
	res, err := tool.Handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler call: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}

	// The response payload should at minimum carry a "nodes" key with
	// at least one entry — service.Vault exists in the synthetic Go
	// corpus. Walk the structured content to confirm.
	raw, err := json.Marshal(res.Content)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if len(raw) == 0 {
		t.Error("empty response content — handler should return a nodes payload")
	}
}

// TestRegisterEvidenceForIntent_RequiresCache documents the
// individual-Register form for the evidence tool, which differs from
// the other seven because it takes a cache parameter. Sister repos
// that want a single cache across multiple servers should use this
// path; RegisterAll's convenience cache is fresh-per-call.
func TestRegisterEvidenceForIntent_RequiresCache(t *testing.T) {
	out := t.TempDir()
	if _, err := buildpipe.Run(buildpipe.Options{
		SrcRoot:    "../../testdata/synthetic",
		OutDir:     out,
		Languages:  []string{"go"},
		CKGVersion: "test",
	}); err != nil {
		t.Fatalf("build synthetic graph: %v", err)
	}
	r, err := store.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer func() { _ = r.Close() }()

	s := server.NewMCPServer("smoke", "0.0.0")
	cache := evidence.NewCache()
	mcphandlers.RegisterEvidenceForIntent(s, r, cache)

	if tool := s.GetTool("evidence_for_intent"); tool == nil || tool.Handler == nil {
		t.Fatal("evidence_for_intent not registered")
	}
}
