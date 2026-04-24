package persist_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

func TestExportChunked(t *testing.T) {
	src := t.TempDir()
	if _, err := buildpipe.Run(buildpipe.Options{
		SrcRoot: "../parse/golang/testdata/resolve", OutDir: src,
		Languages: []string{"auto"}, CKGVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}
	store, _ := persist.OpenReadOnly(filepath.Join(src, "graph.db"))
	defer store.Close()
	dst := t.TempDir()
	if err := store.ExportChunked(dst, 5000, 10000); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"manifest.json", "hierarchy/pkg_tree.json"} {
		if _, err := os.Stat(filepath.Join(dst, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	// Spot-check a node chunk
	files, _ := filepath.Glob(filepath.Join(dst, "nodes", "chunk_*.json"))
	if len(files) == 0 {
		t.Fatalf("no node chunks emitted")
	}
	b, _ := os.ReadFile(files[0])
	var nodes []map[string]any
	_ = json.Unmarshal(b, &nodes)
	if len(nodes) == 0 {
		t.Errorf("first chunk is empty")
	}
}
