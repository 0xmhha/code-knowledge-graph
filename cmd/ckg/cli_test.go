package main

import (
	"sort"
	"testing"
)

func TestSubcommandsRegistered(t *testing.T) {
	root := newRootCmd()
	want := []string{"build", "eval", "export-static", "mcp", "serve"}
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
