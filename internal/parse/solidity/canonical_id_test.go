package solidity_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	sol "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestCanonicalID_SolidityOverloads guards symbol-identity Phase 1 (ADR-0001)
// for Solidity: two same-named functions in one contract must get distinct
// canonical ids, separated by their parameter-type signature, while the short
// qualified_name (Over.foo) collides. The relative file path is the qualifier
// (no import path in Solidity).
func TestCanonicalID_SolidityOverloads(t *testing.T) {
	src := []byte(`// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Over {
    function foo(uint256 x) public pure returns (uint256) { return x; }
    function foo(address a) public pure returns (address) { return a; }
}
`)
	root := t.TempDir()
	path := filepath.Join(root, "Over.sol")
	p := sol.New(root)
	r, err := p.ParseFile(path, src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	g, err := p.Resolve([]*parse.ParseResult{r})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	var cids []string
	for _, n := range g.Nodes {
		if n.Type == types.NodeFunction && n.QualifiedName == "Over.foo" {
			cids = append(cids, n.CanonicalID)
		}
	}
	if len(cids) != 2 {
		t.Fatalf("expected 2 Over.foo function nodes, got %d (%v)", len(cids), cids)
	}
	if cids[0] == cids[1] {
		t.Fatalf("overloaded foo share canonical id %q — overloads not separated", cids[0])
	}
	want := map[string]bool{
		"Over.sol:Over.foo(uint256)": true,
		"Over.sol:Over.foo(address)": true,
	}
	for _, c := range cids {
		if !want[c] {
			t.Errorf("unexpected overload canonical id %q (want one of %v)", c, want)
		}
	}
}
