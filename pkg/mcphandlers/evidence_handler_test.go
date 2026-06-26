package mcphandlers

import (
	"context"
	"testing"

	mcp "github.com/mark3labs/mcp-go/mcp"
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/evidence"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestRegisterEvidenceForIntent_ToolListed mirrors the other register*
// smokes — confirms the tool name shows up in the MCP server registry.
// Pairs with server_test.go's TestRunRegistersAllEightTools (static
// scan of Run()) for end-to-end registration coverage.
func TestRegisterEvidenceForIntent_ToolListed(t *testing.T) {
	s := server.NewMCPServer("test", "0")
	RegisterEvidenceForIntent(s, &emptyStore{}, evidence.NewCache())
	tools := s.ListTools()
	if _, ok := tools["evidence_for_intent"]; !ok {
		t.Error("evidence_for_intent not registered")
	}
}

// TestRegisterEvidenceForIntent_ModeArgPropagated covers the
// surface-fan-out gap flagged in the verification checklist: the MCP
// tool exposes a `mode` string param, and the handler must pass it
// through to evidence.Options.Mode. We invoke the handler with
// mode="and" + a query that, in OR mode, would match a doc lacking
// some tokens; AND must drop it. The drop is the tell-tale that the
// param actually reached BuildPack rather than getting silently
// defaulted.
func TestRegisterEvidenceForIntent_ModeArgPropagated(t *testing.T) {
	store := &fakeMCPEvidenceStore{
		nodes: []types.Node{
			{ID: "c1", Type: types.NodeCommit, QualifiedName: "commit:aaaa",
				Signature: "1700000100: panel only", Confidence: types.ConfExtracted},
			{ID: "h1", Type: types.NodeHunk, QualifiedName: "hunk:aaaa:Panel.tsx:0",
				FilePath: "Panel.tsx", Confidence: types.ConfExtracted},
		},
		blobs: map[string][]byte{"h1": gz("panel mounts")},
	}
	s := server.NewMCPServer("test", "0")
	RegisterEvidenceForIntent(s, store, evidence.NewCache())
	tool := s.GetTool("evidence_for_intent")
	if tool == nil || tool.Handler == nil {
		t.Fatal("evidence_for_intent not registered or has no handler")
	}

	// OR (default): "panel jitter" matches h1 via the "panel" token.
	orPack := callEvidence(t, tool.Handler, map[string]any{"intent": "panel jitter"})
	if len(orPack.Hits) == 0 {
		t.Errorf("OR mode should surface aaaa via partial match; got 0 hits")
	}

	// AND: same query, but h1 lacks "jitter" → dropped → no hits.
	andPack := callEvidence(t, tool.Handler, map[string]any{
		"intent": "panel jitter", "mode": "and",
	})
	if len(andPack.Hits) != 0 {
		t.Errorf("AND mode should drop aaaa (no 'jitter' in doc); got %d hits", len(andPack.Hits))
	}
}

// callEvidence invokes the registered handler in-process and unwraps
// the structured EvidencePack from the CallToolResult. textResult
// (server.go) wraps payload via mcp.NewToolResultStructured, so the
// real value lives at res.StructuredContent — same pattern impact's
// test uses.
func callEvidence(t *testing.T, handler server.ToolHandlerFunc, args map[string]any) *evidence.Pack {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Name = "evidence_for_intent"
	req.Params.Arguments = args
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res == nil || res.StructuredContent == nil {
		t.Fatalf("handler returned no StructuredContent (res=%v)", res)
	}
	pack, ok := res.StructuredContent.(*evidence.Pack)
	if !ok {
		t.Fatalf("StructuredContent type = %T, want *evidence.Pack", res.StructuredContent)
	}
	return pack
}

// emptyStore is the zero-method stub used by TestRegisterEvidenceForIntent_ToolListed —
// the listing test never invokes the handler so no method needs a real
// implementation.
type emptyStore struct{ persist.StoreReader }

// fakeMCPEvidenceStore is the read-only StoreReader subset that
// evidence.BuildPack actually exercises (AllNodes / AllEdges /
// GetBlob / GetManifest). Local to this test file so the AMBIGUOUS
// fakeStore in h3_filter_test.go stays tightly scoped to that test.
type fakeMCPEvidenceStore struct {
	persist.StoreReader
	nodes []types.Node
	blobs map[string][]byte
}

func (f *fakeMCPEvidenceStore) AllNodes() ([]types.Node, error) {
	return f.nodes, nil
}

func (f *fakeMCPEvidenceStore) AllEdges() ([]types.Edge, error) {
	return nil, nil
}

func (f *fakeMCPEvidenceStore) GetBlob(id string) ([]byte, error) {
	return f.blobs[id], nil
}

// NodesByIDs backs the llmSafeReader.GetBlob backstop, which the handler now
// routes through (every Register* wraps its reader). Returns the matching
// seeded nodes so the safety filter can classify them.
func (f *fakeMCPEvidenceStore) NodesByIDs(ids []string) ([]types.Node, error) {
	var out []types.Node
	for _, id := range ids {
		for _, n := range f.nodes {
			if n.ID == id {
				out = append(out, n)
			}
		}
	}
	return out, nil
}

func (f *fakeMCPEvidenceStore) GetManifest() (persist.Manifest, error) {
	return persist.Manifest{BuildTimestamp: "test", SrcCommit: "test"}, nil
}

// gz mirrors pkg/evidence/evidence_test.go's helper — gzip-compresses
// a blob payload because emitHunkGraph stores patches gzipped and
// pkg/evidence's gunzipIfNeeded checks the magic bytes on egress.
func gz(s string) []byte {
	// Reusing the same compression as evidence_test.go so the test
	// data round-trips through pkg/evidence's gunzipIfNeeded
	// identically. The body is small enough that gzip isn't a
	// realistic perf concern.
	return gzipBytes([]byte(s))
}
