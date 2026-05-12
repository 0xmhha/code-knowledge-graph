package solidity_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	sol "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// usingForWant captures one EdgeUsesFor assertion as (contract.qname,
// library.qname). Direction = contract → library (§4.6.1, Q9-1 (b)).
type usingForWant struct {
	contract string
	library  string
}

// parseResolveOneSol — shared helper: parses a single fixture, resolves
// it in isolation (one-file ResolvedGraph), returns the union of nodes
// and edges. Same shape as parseResolveHeritage on the TS side.
func parseResolveOneSol(t *testing.T, dir, file string) ([]types.Node, []types.Edge) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	p := sol.New(dir)
	res, err := p.ParseFile(filepath.Join(dir, file), src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	resolved, err := p.Resolve([]*parse.ParseResult{res})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return resolved.Nodes, resolved.Edges
}

// collectUsingFor groups EdgeUsesFor edges into a sorted list of
// (contractName, libraryName) pairs so assertions are stable across
// map-iteration order.
func collectUsingFor(nodes []types.Node, edges []types.Edge) []usingForWant {
	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	var out []usingForWant
	for _, e := range edges {
		if e.Type != types.EdgeUsesFor {
			continue
		}
		out = append(out, usingForWant{
			contract: byID[e.Src].Name,
			library:  byID[e.Dst].Name,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].contract != out[j].contract {
			return out[i].contract < out[j].contract
		}
		return out[i].library < out[j].library
	})
	return out
}

// TestUsingFor_SpecificBinding — W-C W6 baseline. `using SafeMath for
// uint256` in a single contract yields exactly one EdgeUsesFor (Vault →
// SafeMath) at ConfExtracted (same-file).
func TestUsingFor_SpecificBinding(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for", "specific_binding.sol")
	got := collectUsingFor(nodes, edges)
	want := []usingForWant{
		{contract: "Vault", library: "SafeMath"},
	}
	if !equalUsingFor(got, want) {
		t.Errorf("EdgeUsesFor mismatch: got=%v want=%v", got, want)
	}
	// Confidence: same-file → ConfExtracted.
	for _, e := range edges {
		if e.Type != types.EdgeUsesFor {
			continue
		}
		if e.Confidence != types.ConfExtracted {
			t.Errorf("same-file EdgeUsesFor conf=%q, want EXTRACTED", e.Confidence)
		}
	}
}

// TestUsingFor_WildcardForm — `using Lib for *` shape recognition. V0
// emits the same edge shape as specific binding (typeName not surfaced).
func TestUsingFor_WildcardForm(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for", "wildcard_binding.sol")
	got := collectUsingFor(nodes, edges)
	want := []usingForWant{
		{contract: "Universal", library: "Helpers"},
	}
	if !equalUsingFor(got, want) {
		t.Errorf("EdgeUsesFor mismatch: got=%v want=%v", got, want)
	}
}

// TestUsingFor_MultiLibrary — one contract binds two different libraries
// to two different types. Both edges land independently; no dedup at the
// (contract, library) level (a single contract with 2 distinct libraries
// produces 2 edges).
func TestUsingFor_MultiLibrary(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for", "multi_library.sol")
	got := collectUsingFor(nodes, edges)
	want := []usingForWant{
		{contract: "Combined", library: "AddressTwo"},
		{contract: "Combined", library: "SafeMathTwo"},
	}
	if !equalUsingFor(got, want) {
		t.Errorf("EdgeUsesFor mismatch: got=%v want=%v", got, want)
	}
}

// TestUsingFor_ContractScoped — two contracts each binding the same
// library yield two distinct edges sharing the Dst (SharedLib) but with
// distinct Src. Demonstrates that bindings are contract-scoped — the
// graph encodes each contract's relationship independently rather than
// deduping at the library level.
func TestUsingFor_ContractScoped(t *testing.T) {
	nodes, edges := parseResolveOneSol(t, "testdata/using_for", "cross_contract.sol")
	got := collectUsingFor(nodes, edges)
	want := []usingForWant{
		{contract: "VaultA", library: "SharedLib"},
		{contract: "VaultB", library: "SharedLib"},
	}
	if !equalUsingFor(got, want) {
		t.Errorf("EdgeUsesFor mismatch: got=%v want=%v", got, want)
	}
	// Cross-contract should NOT deduplicate to a single edge: two
	// distinct binding declarations even if they target the same library.
	byID := map[string]types.Node{}
	for _, n := range nodes {
		byID[n.ID] = n
	}
	srcSet := map[string]bool{}
	for _, e := range edges {
		if e.Type != types.EdgeUsesFor {
			continue
		}
		srcSet[byID[e.Src].Name] = true
	}
	if len(srcSet) != 2 {
		t.Errorf("expected 2 distinct contract sources, got %d (%v)", len(srcSet), srcSet)
	}
}

// TestUsingFor_NegativeNoBinding — defensive guard. A contract with
// method-call syntax but no `using` directive must produce zero
// EdgeUsesFor edges. Catches a future regression where the detector
// false-positives on plain `obj.foo()` without a directive present.
func TestUsingFor_NegativeNoBinding(t *testing.T) {
	_, edges := parseResolveOneSol(t, "testdata/using_for", "no_binding_negative.sol")
	for _, e := range edges {
		if e.Type == types.EdgeUsesFor {
			t.Errorf("unexpected EdgeUsesFor in no_binding_negative.sol: %+v", e)
		}
	}
}

func equalUsingFor(a, b []usingForWant) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
