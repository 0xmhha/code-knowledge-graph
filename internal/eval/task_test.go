package eval_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/eval"
)

func TestLoadTasks(t *testing.T) {
	dir := t.TempDir()
	yaml := `
id: T01
corpus: synthetic
description: "find callers of foo"
expected_kind: symbol_set
expected:
  symbols: ["a.foo", "b.bar"]
scoring:
  type: precision_recall
  threshold: { precision: 0.8, recall: 0.8 }
`
	if err := os.WriteFile(filepath.Join(dir, "t.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	tasks, err := eval.LoadTasks(filepath.Join(dir, "*.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != "T01" || len(tasks[0].Expected.Symbols) != 2 {
		t.Errorf("unexpected: %+v", tasks)
	}
}
