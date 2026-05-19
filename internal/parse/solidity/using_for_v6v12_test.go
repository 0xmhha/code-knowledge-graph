package solidity_test

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W6 V12 — wildcard target audit after V8-V11 hint encoding
// landed. `using Helpers for *;` should still emit a single
// EdgeUsesFor with the bare `using_for` DispatchKind (no
// "|<method>" suffix since the directive doesn't name a member)
// and no path-hint contamination from the V5/V6 alias path.
//
// The wildcard test fixture has been around since W6 V0; V12
// adds the explicit DispatchKind assertion so the V8 method-
// name encoding can't silently mis-fire on the wildcard form.
func TestUsingForV6V12_WildcardAuditAfterHintEncoding(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for", "wildcard_binding.sol")

	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	var got []types.Edge
	for _, e := range edges {
		if e.Type == types.EdgeUsesFor {
			got = append(got, e)
		}
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly one EdgeUsesFor, got %d", len(got))
	}
	e := got[0]

	// Source / target identity.
	src := byID[e.Src].Name
	dst := byID[e.Dst].Name
	if src != "Universal" || dst != "Helpers" {
		t.Errorf("EdgeUsesFor: got %s -> %s, want Universal -> Helpers", src, dst)
	}

	// DispatchKind must be the bare value — wildcard directives
	// don't name a specific method, so V8's `using_for|<method>`
	// suffix should NOT fire here.
	if e.DispatchKind != "using_for" {
		t.Errorf("DispatchKind: got %q, want %q (wildcard form has no method)", e.DispatchKind, "using_for")
	}
	if strings.Contains(e.DispatchKind, "|") {
		t.Errorf("DispatchKind contains a method suffix on the wildcard form: %q", e.DispatchKind)
	}
}
