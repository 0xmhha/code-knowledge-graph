package link_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/link"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestPickBestPrefersHighScore drives the score/containsFold internals via
// multiple TS candidates with the same Vault name but different file paths,
// asserting SolToTS picks the candidate whose path matches the most hints
// ("typechain", "contracts", "abi") — case-insensitive.
func TestPickBestPrefersHighScore(t *testing.T) {
	abi := map[string][]link.ABISig{
		"Vault": {{ContractName: "Vault", FunctionName: "deposit"}},
	}
	sol := types.Node{
		ID: "sol00000000000bb", Type: types.NodeContract,
		Name: "Vault", QualifiedName: "Vault",
		FilePath: "contracts/Vault.sol", Language: "sol",
		Confidence: types.ConfExtracted,
	}

	cases := []struct {
		name      string
		candidates []types.Node
		wantDst   string
	}{
		{
			name: "single_contracts_match",
			candidates: []types.Node{
				{ID: "ts0001", Type: types.NodeClass, Name: "Vault", QualifiedName: "Vault",
					FilePath: "src/random/Vault.ts", Language: "ts", Confidence: types.ConfExtracted},
				{ID: "ts0002", Type: types.NodeClass, Name: "Vault", QualifiedName: "Vault",
					FilePath: "src/contracts/Vault.ts", Language: "ts", Confidence: types.ConfExtracted},
			},
			wantDst: "ts0002",
		},
		{
			name: "typechain_abi_beats_contracts",
			candidates: []types.Node{
				{ID: "ts0010", Type: types.NodeClass, Name: "Vault", QualifiedName: "Vault",
					FilePath: "src/contracts/Vault.ts", Language: "ts", Confidence: types.ConfExtracted},
				{ID: "ts0011", Type: types.NodeClass, Name: "Vault", QualifiedName: "Vault",
					FilePath: "src/typechain/abi/Vault.ts", Language: "ts", Confidence: types.ConfExtracted},
			},
			wantDst: "ts0011",
		},
		{
			name: "case_insensitive_TYPECHAIN",
			candidates: []types.Node{
				{ID: "ts0020", Type: types.NodeClass, Name: "Vault", QualifiedName: "Vault",
					FilePath: "src/random/Vault.ts", Language: "ts", Confidence: types.ConfExtracted},
				{ID: "ts0021", Type: types.NodeClass, Name: "Vault", QualifiedName: "Vault",
					FilePath: "src/TYPECHAIN/Vault.ts", Language: "ts", Confidence: types.ConfExtracted},
			},
			wantDst: "ts0021",
		},
		{
			name: "all_zero_score_picks_first",
			candidates: []types.Node{
				{ID: "ts0030", Type: types.NodeClass, Name: "Vault", QualifiedName: "Vault",
					FilePath: "src/foo/Vault.ts", Language: "ts", Confidence: types.ConfExtracted},
				{ID: "ts0031", Type: types.NodeClass, Name: "Vault", QualifiedName: "Vault",
					FilePath: "src/bar/Vault.ts", Language: "ts", Confidence: types.ConfExtracted},
			},
			wantDst: "ts0030",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodes := append([]types.Node{sol}, tc.candidates...)
			edges := link.SolToTS(nodes, abi)
			if len(edges) != 1 {
				t.Fatalf("got %d edges, want 1", len(edges))
			}
			if edges[0].Dst != tc.wantDst {
				t.Errorf("dst = %s, want %s", edges[0].Dst, tc.wantDst)
			}
		})
	}
}

// TestSolToTSNoMatch covers the path where no TS candidate exists for the
// Sol contract — SolToTS should return zero edges.
func TestSolToTSNoMatch(t *testing.T) {
	abi := map[string][]link.ABISig{
		"Token": {{ContractName: "Token", FunctionName: "transfer"}},
	}
	sol := types.Node{
		ID: "solxxxx0000000xx", Type: types.NodeContract,
		Name: "Token", QualifiedName: "Token",
		FilePath: "contracts/Token.sol", Language: "sol",
		Confidence: types.ConfExtracted,
	}
	// Only an unrelated TS class
	tsOther := types.Node{
		ID: "tsxxxx0000000yy", Type: types.NodeClass,
		Name: "Vault", QualifiedName: "Vault",
		FilePath: "src/contracts/Vault.ts", Language: "ts",
		Confidence: types.ConfExtracted,
	}
	edges := link.SolToTS([]types.Node{sol, tsOther}, abi)
	if len(edges) != 0 {
		t.Errorf("got %d edges, want 0", len(edges))
	}
}

// TestContainsFoldEmptySub indirectly: an ABI hint of empty string in score
// would always match, but score uses fixed hints. This test instead verifies
// the case-insensitive substring against a path with mixed case.
func TestSolToTSCaseInsensitivePath(t *testing.T) {
	abi := map[string][]link.ABISig{
		"Vault": {{ContractName: "Vault", FunctionName: "deposit"}},
	}
	sol := types.Node{
		ID: "sol_aaaaaaaaaaaa", Type: types.NodeContract,
		Name: "Vault", QualifiedName: "Vault",
		FilePath: "contracts/Vault.sol", Language: "sol",
		Confidence: types.ConfExtracted,
	}
	ts := types.Node{
		ID: "ts_aaaaaaaaaaaa", Type: types.NodeClass,
		Name: "Vault", QualifiedName: "Vault",
		// Mixed case "Contracts" — containsFold should still match "contracts".
		FilePath: "src/Contracts/Vault.ts", Language: "ts",
		Confidence: types.ConfExtracted,
	}
	edges := link.SolToTS([]types.Node{sol, ts}, abi)
	if len(edges) != 1 || edges[0].Dst != ts.ID {
		t.Errorf("expected 1 edge to %s, got %+v", ts.ID, edges)
	}
}
