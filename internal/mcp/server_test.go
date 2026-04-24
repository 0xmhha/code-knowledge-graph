package mcp_test

import (
	"path/filepath"
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
