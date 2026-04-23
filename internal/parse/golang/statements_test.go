package golang_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

type stmtGolden struct {
	LogicBlockCounts map[string]int `json:"logic_block_counts"`
	GoroutineCount   int            `json:"goroutine_count"`
}

func TestParseStatements(t *testing.T) {
	dir := "testdata/statements"
	src, err := os.ReadFile(filepath.Join(dir, "control_flow.go"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := gop.New(dir).ParseFile(filepath.Join(dir, "control_flow.go"), src)
	if err != nil {
		t.Fatal(err)
	}
	gb, _ := os.ReadFile(filepath.Join(dir, "control_flow_golden.json"))
	var g stmtGolden
	_ = json.Unmarshal(gb, &g)

	counts := map[string]int{}
	for _, n := range res.Nodes {
		switch n.Type {
		case types.NodeIfStmt, types.NodeLoopStmt, types.NodeSwitchStmt,
			types.NodeReturnStmt, types.NodeCallSite:
			counts[string(n.Type)]++
		}
	}
	for k, want := range g.LogicBlockCounts {
		if got := counts[k]; got != want {
			t.Errorf("logic-block %s = %d, want %d", k, got, want)
		}
	}

	gor := 0
	for _, n := range res.Nodes {
		if n.Type == types.NodeGoroutine {
			gor++
		}
	}
	if gor != g.GoroutineCount {
		t.Errorf("Goroutine count = %d, want %d", gor, g.GoroutineCount)
	}
}
