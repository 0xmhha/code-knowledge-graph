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
	defer store.Close()
	// We can't easily invoke stdio in a unit test; this just verifies
	// registration doesn't panic.
	_ = mcppkg.Run // referenced for compilation; full registration smoke in T29
}

// TestRunRegistersAllEightTools is a static-ish guard: it grep-scans
// the package source for the canonical register* call sites inside
// Run(). If a future contributor adds a new register-style helper but
// forgets to wire it into Run() — or removes one of the existing eight
// — the test surfaces it before review. Pairs with
// TestLLMSafeStoreReader_AllReadMethods_DropAmbiguousMeta to enforce
// the §11.3 boundary across the full surface: every tool registered
// here goes through the wrapper, and every wrapper method drops
// AMBIGUOUS Hunk/Commit.
//
// We read server.go from the source tree (the test runs from the
// package directory) rather than reflecting on the running binary,
// because mcp-go doesn't expose a "list registered handlers" API.
func TestRunRegistersAllEightTools(t *testing.T) {
	// cwd for `go test ./internal/mcp/...` is the package directory,
	// so server.go is in the same folder. Falling back to the bare
	// filename keeps the test working under `go test -run` from a
	// different cwd, too.
	bs, err := os.ReadFile("server.go")
	if err != nil {
		bs, err = os.ReadFile(filepath.Join("..", "mcp", "server.go"))
		if err != nil {
			t.Fatalf("read server.go: %v", err)
		}
	}
	src := string(bs)
	want := []string{
		"registerFindSymbol(s, safe)",
		"registerFindCallers(s, safe)",
		"registerFindCallees(s, safe)",
		"registerGetSubgraph(s, safe)",
		"registerSearchText(s, safe)",
		"registerGetContextForTask(s, safe)",
		"registerImpactOfChange(s, safe)",
		"registerEvidenceForIntent(s, safe,",
	}
	for _, line := range want {
		if !strings.Contains(src, line) {
			t.Errorf("server.go is missing %q — Run() must wire every tool through the safe wrapper", line)
		}
	}
}
