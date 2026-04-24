package link_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/link"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

func TestLinkBindsToContract(t *testing.T) {
	tsClass := types.Node{
		ID: "ts000000000000aa", Type: types.NodeClass,
		Name: "Vault", QualifiedName: "Vault",
		FilePath: "src/contracts/Vault.ts", Language: "ts",
		Confidence: types.ConfExtracted,
	}
	solContract := types.Node{
		ID: "sol00000000000bb", Type: types.NodeContract,
		Name: "Vault", QualifiedName: "Vault",
		FilePath: "contracts/Vault.sol", Language: "sol",
		Confidence: types.ConfExtracted,
	}
	abi := map[string][]link.ABISig{"Vault": {{ContractName: "Vault", FunctionName: "deposit"}}}

	edges := link.SolToTS(
		[]types.Node{tsClass, solContract},
		abi,
	)
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(edges))
	}
	e := edges[0]
	if e.Type != types.EdgeBindsTo || e.Src != solContract.ID || e.Dst != tsClass.ID {
		t.Errorf("unexpected edge: %+v", e)
	}
	if e.Confidence != types.ConfInferred {
		t.Errorf("confidence = %s, want INFERRED", e.Confidence)
	}
}
