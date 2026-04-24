package mcp

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// TestBuildContextNotFound exercises the smart tool against an unrelated
// query. The fixture under parse/golang/testdata/resolve has no symbols
// matching "zzzzz_no_match" so SearchFTS returns zero candidates and
// buildContext should short-circuit with not_found=true.
func TestBuildContextNotFound(t *testing.T) {
	out := t.TempDir()
	if _, err := buildpipe.Run(buildpipe.Options{
		SrcRoot:    "../parse/golang/testdata/resolve",
		OutDir:     out,
		Languages:  []string{"auto"},
		CKGVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}
	store, err := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	res, err := buildContext(store, "zzzzz_no_match", 4000, true, 5)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := res["not_found"].(bool); !got {
		t.Errorf("expected not_found=true on unrelated query, got %+v", res)
	}
}
