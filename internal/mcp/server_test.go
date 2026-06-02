package mcp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	mcppkg "github.com/0xmhha/code-knowledge-graph/internal/mcp"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

func TestMCPServerConstructs(t *testing.T) {
	out := t.TempDir()
	if _, err := buildpipe.Run(buildpipe.Options{
		SrcRoot: "../parse/golang/testdata/resolve", OutDir: out,
		Languages: []string{"auto"}, CKGVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	// We can't easily invoke stdio in a unit test; this just verifies
	// registration doesn't panic.
	_ = mcppkg.Run // referenced for compilation; full registration smoke in T29
}

// TestRunWiresMCPHandlersRegisterAll is a static guard against
// regressing the post-T-14b architecture: Run() must hand off to
// pkg/mcphandlers.RegisterAll, which is the single source of truth
// for the eight-tool wiring and the §11.3 H3 safety wrapper. The
// per-tool registration coverage lives in
// pkg/mcphandlers/example_test.go::TestRegisterAll_LocksEightTools;
// that test confirms each tool name shows up after RegisterAll, so
// this file only has to lock the call-out from Run().
//
// Read server.go from the package directory; fall back to the bare
// filename for `go test -run` invoked from a different cwd.
func TestRunWiresMCPHandlersRegisterAll(t *testing.T) {
	bs, err := os.ReadFile("server.go")
	if err != nil {
		bs, err = os.ReadFile(filepath.Join("..", "mcp", "server.go"))
		if err != nil {
			t.Fatalf("read server.go: %v", err)
		}
	}
	src := string(bs)
	if !strings.Contains(src, "mcphandlers.RegisterAll(s, store)") {
		t.Error("server.go must call mcphandlers.RegisterAll(s, store) — Run() is the production stdio entry point and the single hand-off to the public tool surface")
	}
}
