package solidity_test

import (
	"os"
	"path/filepath"
	"testing"

	sol "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestSolSubKind_AbstractLibrary covers Sol W4 — the SubKind field on
// NodeContract for plain / abstract / library declarations.
//
// Spec: docs/design/solidity-inheritance-and-interface-dispatch.md §2.1, §4.4
// Dispatch: docs/DISPATCH-WITHIN-LANG-SEMANTICS.md §2 Phase 3
//
// The Sol grammar exposes three distinct top-level decls; the detector
// must distinguish them via:
//   - `contract_declaration` + leading `abstract` keyword → "abstract"
//   - `contract_declaration` (no `abstract`) → "contract"
//   - `library_declaration`  → "library"
func TestSolSubKind_AbstractLibrary(t *testing.T) {
	cases := []struct {
		file        string
		contractNm  string
		wantSubKind string
		// methodPrefix is the qualified prefix expected on the contract's
		// methods (e.g. "Base.foo"). Empty means skip the method-qname
		// check — useful for fixtures with no methods.
		methodPrefix string
	}{
		{file: "plain.sol", contractNm: "Simple", wantSubKind: "contract", methodPrefix: "Simple."},
		{file: "abstract.sol", contractNm: "Base", wantSubKind: "abstract", methodPrefix: "Base."},
		{file: "library.sol", contractNm: "SafeMath", wantSubKind: "library", methodPrefix: "SafeMath."},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			dir := filepath.Join("testdata", "subkind")
			src, err := os.ReadFile(filepath.Join(dir, tc.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			p := sol.New(dir)
			res, err := p.ParseFile(filepath.Join(dir, tc.file), src)
			if err != nil {
				t.Fatalf("ParseFile: %v", err)
			}

			// Locate the Contract node by Name and assert SubKind.
			var found *types.Node
			for i := range res.Nodes {
				n := &res.Nodes[i]
				if n.Type == types.NodeContract && n.Name == tc.contractNm {
					found = n
					break
				}
			}
			if found == nil {
				t.Fatalf("no NodeContract named %q in %s; got %d nodes",
					tc.contractNm, tc.file, len(res.Nodes))
			}
			if found.SubKind != tc.wantSubKind {
				t.Errorf("SubKind: got %q, want %q", found.SubKind, tc.wantSubKind)
			}

			// Method qname prefix — ensures nearestContractName resolves
			// library/abstract bodies the same way as plain contracts.
			if tc.methodPrefix != "" {
				sawPrefixed := false
				for _, n := range res.Nodes {
					if n.Type != types.NodeFunction {
						continue
					}
					if len(n.QualifiedName) >= len(tc.methodPrefix) &&
						n.QualifiedName[:len(tc.methodPrefix)] == tc.methodPrefix {
						sawPrefixed = true
						break
					}
				}
				if !sawPrefixed {
					var fnNames []string
					for _, n := range res.Nodes {
						if n.Type == types.NodeFunction {
							fnNames = append(fnNames, n.QualifiedName)
						}
					}
					t.Errorf("expected at least one function qualified with %q; got function qnames %v",
						tc.methodPrefix, fnNames)
				}
			}
		})
	}
}

// TestSolSubKind_VaultRegression confirms the existing vault.sol fixture
// (single plain `contract`) emits SubKind="contract" — guards against
// the W4 detector changing meaning for the pre-existing testdata baseline.
func TestSolSubKind_VaultRegression(t *testing.T) {
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
	var got *types.Node
	for i := range res.Nodes {
		n := &res.Nodes[i]
		if n.Type == types.NodeContract && n.Name == "Vault" {
			got = n
			break
		}
	}
	if got == nil {
		t.Fatalf("no NodeContract named Vault in vault.sol")
	}
	if got.SubKind != "contract" {
		t.Errorf("Vault SubKind: got %q, want %q", got.SubKind, "contract")
	}
}
