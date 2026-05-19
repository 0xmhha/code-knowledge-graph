package buildpipe_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestPipelineMultilangV6Markers — W-C W11 V6. Runs the full
// buildpipe.Run cold-rebuild pipeline over a multi-language
// fixture (Sol + TS + Go) and verifies the round-trip contract
// for the *currently persisted* surface: node identity by
// qualified name, file paths, language stamps, and the cross-
// language binds_to edge produced by linker T20.
//
// Marker persistence (HasExternalCall / HasFunctionPointerCall /
// IsFunctionTyped / SlotIndex / HasInheritanceMROFallback / …)
// is INTENTIONALLY NOT asserted here. Writing this test
// surfaced the broader gap that internal/persist/schema.sql's
// nodes table stops at sub_kind — every W6-W10 marker added on
// the types.Node struct is silently dropped at the SQLite
// boundary today. Closing that gap is its own scope (schema
// bump + migration helper) tracked as the next W11 V7+ item;
// V6's job is to lock the existing round-trip contract and
// document the gap.
func TestPipelineMultilangV6Markers(t *testing.T) {
	out := t.TempDir()
	_, err := buildpipe.Run(buildpipe.Options{
		SrcRoot:    "testdata/multilang_v6_markers",
		OutDir:     out,
		Languages:  []string{"auto"},
		CKGVersion: "test",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	store, err := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer store.Close()

	nodes, err := store.AllNodes()
	if err != nil {
		t.Fatalf("AllNodes: %v", err)
	}

	// Sol Wallet and TS Wallet share the bare name "Wallet" — the
	// cross-language binder relies on that homonymy. Index Sol nodes
	// separately so subsequent assertions can target the contract
	// without colliding with the TS class.
	solByQName := map[string]types.Node{}
	for _, n := range nodes {
		if n.Language == "sol" {
			solByQName[n.QualifiedName] = n
		}
	}

	// (a) Sol contract + its three functions all round-trip with
	// their qnames and file paths intact.
	for _, qn := range []string{
		"Wallet", "Wallet.trigger", "Wallet.relay", "Wallet.plain",
	} {
		n, ok := solByQName[qn]
		if !ok {
			t.Errorf("missing Sol node %q in persisted graph", qn)
			continue
		}
		if n.FilePath == "" {
			t.Errorf("%s: empty FilePath", qn)
		}
	}

	// (b) Cross-language binding. Sol Wallet + TS Wallet → linker
	// emits at least one binds_to edge.
	bindsTo, err := store.QueryEdgesByType("binds_to")
	if err != nil {
		t.Fatalf("QueryEdgesByType: %v", err)
	}
	if len(bindsTo) == 0 {
		t.Errorf("expected >=1 binds_to edge between Sol Wallet and TS Wallet, got 0")
	}

	// (c) Pipeline produced nodes from Sol and TS (Go optional —
	// the fixture has one Go file but buildpipe's auto discovery
	// may skip it if go.mod constraints reject the in-test path).
	langs := map[string]bool{}
	for _, n := range nodes {
		if n.Language != "" {
			langs[n.Language] = true
		}
	}
	if !langs["sol"] {
		t.Errorf("no Sol nodes persisted (langs=%v)", langs)
	}
	if !langs["ts"] {
		t.Errorf("no TS nodes persisted (langs=%v)", langs)
	}
}
