package solidity_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sol "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

type golden struct {
	ExpectedNodeTypes []string `json:"expected_node_types"`
	ExpectedEdgeTypes []string `json:"expected_edge_types"`
}

func TestSolParseVault(t *testing.T) {
	dir := "testdata"
	src, err := os.ReadFile(filepath.Join(dir, "vault.sol"))
	if err != nil {
		t.Fatal(err)
	}
	p := sol.New(dir)
	res, err := p.ParseFile(filepath.Join(dir, "vault.sol"), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// Resolve runs Pass 2 to materialize the pending edges (emits_event,
	// has_modifier, writes_mapping). The test asserts the resolved graph,
	// not just Pass 1 output, so we union via Resolve.
	resolved, err := p.Resolve([]*parse.ParseResult{res})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	gb, err := os.ReadFile(filepath.Join(dir, "vault_golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(gb, &g); err != nil {
		t.Fatal(err)
	}

	haveNT := map[types.NodeType]bool{}
	for _, n := range resolved.Nodes {
		haveNT[n.Type] = true
	}
	for _, want := range g.ExpectedNodeTypes {
		if !haveNT[types.NodeType(want)] {
			t.Errorf("missing node type %s; have %v", want, haveNT)
		}
	}
	haveET := map[types.EdgeType]bool{}
	for _, e := range resolved.Edges {
		haveET[e.Type] = true
	}
	for _, want := range g.ExpectedEdgeTypes {
		if !haveET[types.EdgeType(want)] {
			t.Errorf("missing edge type %s; have %v", want, haveET)
		}
	}

	// Every edge.Src must reference a real node ID. This catches the class
	// of bug where pending refs are queued with a synthetic SrcID (e.g.
	// MakeID(qname, "sol", 0)) that never matches the function node minted
	// from runDecl with the real name-node startByte. graph.Validate enforces
	// the same invariant at build time, but checking here keeps the parser
	// honest in isolation.
	nodeIDs := make(map[string]struct{}, len(resolved.Nodes))
	for _, n := range resolved.Nodes {
		nodeIDs[n.ID] = struct{}{}
	}
	for _, e := range resolved.Edges {
		if _, ok := nodeIDs[e.Src]; !ok {
			t.Errorf("dangling edge.Src for %s edge: %s -> %s", e.Type, e.Src, e.Dst)
		}
	}

	// ABI side-product: the Vault contract should have at least one entry
	// (deposit) collected during ParseFile.
	abi := p.ABI()
	if sigs := abi["Vault"]; len(sigs) == 0 {
		t.Errorf("expected Vault ABI to be populated; got %#v", abi)
	} else {
		found := false
		for _, s := range sigs {
			if s.FunctionName == "deposit" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected Vault.deposit in ABI; got %#v", sigs)
		}
	}
	t.Logf("nodes: %d (types=%v); edges: %d (types=%v); abi=%v",
		len(resolved.Nodes), keysNT(haveNT), len(resolved.Edges), keysET(haveET), abi)
}

func keysNT(m map[types.NodeType]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, string(k))
	}
	return out
}

func keysET(m map[types.EdgeType]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, string(k))
	}
	return out
}

func TestSolParser_Extensions(t *testing.T) {
	p := sol.New(".")
	exts := p.Extensions()
	if len(exts) == 0 {
		t.Fatal("Extensions() returned empty slice")
	}
	found := false
	for _, e := range exts {
		if e == ".sol" {
			found = true
		}
	}
	if !found {
		t.Errorf("Extensions() does not contain \".sol\": %v", exts)
	}
}
