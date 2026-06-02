package main

import (
	"sort"
	"testing"
)

func TestSubcommandsRegistered(t *testing.T) {
	root := newRootCmd()
	want := []string{"audit", "bench-index", "bench-mcp", "bench-mcp-stdio", "bench-server", "benchmark", "build", "eval-retrieval", "evidence", "export-json", "export-postgres", "export-static",
		"mcp", "path <from> <to>", "query <question>", "quickstart", "report", "serve", "validate", "watch"}
	got := []string{}
	for _, c := range root.Commands() {
		got = append(got, c.Use)
	}
	sort.Strings(got)
	if len(got) != len(want) {
		t.Fatalf("subcommands = %v, want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("subcommands[%d] = %q, want %q", i, got[i], w)
		}
	}
}
