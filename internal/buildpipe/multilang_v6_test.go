package buildpipe_test

import (
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestPipelineMultilangV6Markers — W-C W11 V6 / V7. Runs the full
// buildpipe.Run cold-rebuild pipeline over a multi-language
// fixture (Sol + TS + Go) and verifies the round-trip contract
// for both:
//
//   - Identity surface: qualified name, file path, language stamp,
//     cross-language binds_to edges (T20 linker output).
//   - Marker surface: HasFunctionPointerCall, IsFunctionTyped,
//     HasExternalCall, and the rest survive the SQLite write-
//     then-read cycle via the nodes.attrs JSON-blob column added
//     in schema 1.9 (W11 V7). V6 originally documented this as a
//     gap; V7 closed it and this test gained the marker
//     assertions.
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
	defer func() { _ = store.Close() }()

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

	// (a.1) W-C W11 V7: marker round-trip. The Sol parser stamps
	// HasFunctionPointerCall on Wallet.trigger (W8 V5) and
	// HasExternalCall on Wallet.relay (W10 V5). The fn-typed
	// state-var onAction lights up IsFunctionTyped on the
	// corresponding NodeField. All three markers MUST survive the
	// SQLite write-then-read cycle now that schema 1.9 (nodes.attrs)
	// is in place.
	if n, ok := solByQName["Wallet.trigger"]; ok {
		if !n.HasFunctionPointerCall {
			t.Errorf("Wallet.trigger HasFunctionPointerCall: got false, want true (after V7 attrs roundtrip)")
		}
	}
	if n, ok := solByQName["Wallet.relay"]; ok {
		if !n.HasExternalCall {
			t.Errorf("Wallet.relay HasExternalCall: got false, want true (after V7 attrs roundtrip)")
		}
	}
	if n, ok := solByQName["Wallet.onAction"]; ok {
		if !n.IsFunctionTyped {
			t.Errorf("Wallet.onAction IsFunctionTyped: got false, want true (after V7 attrs roundtrip)")
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
