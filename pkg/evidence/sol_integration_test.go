package evidence

import (
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestBuildPack_SolGraphRegression — W-C W11 V0 (2026-05-18) regression
// safety net for the Sol parser's evolving Node / Edge shape.
//
// Background: the W-C series (W6 using-for, W7 storage/modifier, W8
// contract-type cast, W9 storage slot, W10 inline-assembly marker)
// has added several Sol-specific fields on Node and Edge:
//
//	Node.SubKind       — W4 contract subkind + W7.2 storage location
//	                      on NodeField ("storage_public", "immutable", ...)
//	Node.SlotIndex     — W9 V0 per-contract slot counter on NodeField
//	Node.HasAssembly   — W10 V0 presence flag on NodeFunction / NodeModifier
//	Edge.DispatchKind  — W3 / W7.1 / W8 / etc. dispatch metadata
//	                      ("interface_method", "low_level_call",
//	                       "contract_cast", ...)
//	Edge.Order         — W7.3 multi-modifier source order
//
// None of these reach the EvidencePack output (BuildPack assembles
// Commit / Hunk subset, not Function / Field / Edge). But the evidence
// layer still consumes the upstream graph and any shape drift could
// cause silent serialization changes or assembly failures.
//
// This test stages a small Sol-shaped fakeStore carrying every new
// field (with realistic non-zero values) plus a known-issue commit
// subject, then runs BuildPack and locks the surface invariants:
//
//  1. BuildPack succeeds without panic when W-C fields are populated.
//  2. AMBIGUOUS leak check holds (existing §11.3 boundary).
//  3. Sol-shaped commit subjects flow through H4 issue extraction
//     end to end (the Pack still surfaces the underlying hunks for
//     commits with extractable issue IDs).
//  4. The Pack's deterministic ordering by commit timestamp survives
//     a heterogeneous mix of EXTRACTED / INFERRED / AMBIGUOUS upstream
//     confidences (only the first two land in hits).
func TestBuildPack_SolGraphRegression(t *testing.T) {
	store := &fakeStore{
		nodes: []types.Node{
			// Two non-AMBIGUOUS commits, one AMBIGUOUS that must be filtered.
			{
				ID: "c1", Type: types.NodeCommit,
				QualifiedName: "commit:1111111111111111111111111111111111111111",
				Signature:     "1700000100: feat(parse-sol): W-C W7.1 V0 — low-level call (#101)",
				Confidence:    types.ConfExtracted,
			},
			{
				ID: "c2", Type: types.NodeCommit,
				QualifiedName: "commit:2222222222222222222222222222222222222222",
				Signature:     "1700000200: feat(parse-sol): W-C W9 V0 slot index [ENG-42]",
				Confidence:    types.ConfExtracted,
			},
			{
				ID: "cA", Type: types.NodeCommit,
				QualifiedName: "commit:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
				Signature:     "1700000300: ambiguous: should never reach Pack",
				Confidence:    types.ConfAmbiguous,
			},
			// Hunks under each commit, with Sol-shaped graph emit.
			// Note: a Hunk's confidence is independent of the function/
			// field it ranks against — Pack uses commit confidence as
			// the leak gate per §11.3.
			{
				ID:            "h1",
				Type:          types.NodeHunk,
				QualifiedName: "hunk:1111111111111111111111111111111111111111:contract.sol:0",
				Signature:     "low-level call dispatch in proxy contract",
				Confidence:    types.ConfExtracted,
			},
			{
				ID:            "h2",
				Type:          types.NodeHunk,
				QualifiedName: "hunk:2222222222222222222222222222222222222222:layout.sol:0",
				Signature:     "storage slot index for state variables",
				Confidence:    types.ConfExtracted,
			},
			{
				ID:            "hA",
				Type:          types.NodeHunk,
				QualifiedName: "hunk:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa:bad.sol:0",
				Signature:     "ambiguous hunk should drop",
				Confidence:    types.ConfAmbiguous,
			},
			// Sol-shaped support nodes carrying the new W-C fields.
			// These don't appear in Pack hits (Pack is Commit / Hunk
			// only), but their presence in the store exercises every
			// AllNodes consumer that BuildPack chains through.
			{
				ID:            "fn1",
				Type:          types.NodeFunction,
				QualifiedName: "Proxy.delegate",
				FilePath:      "contract.sol",
				StartLine:     10, EndLine: 30,
				StartByte: 100, EndByte: 800,
				Language:    "sol",
				Confidence:  types.ConfExtracted,
				SubKind:     "function",
				HasAssembly: true, // W10
			},
			{
				ID:            "fld1",
				Type:          types.NodeField,
				QualifiedName: "Proxy.target",
				FilePath:      "contract.sol",
				StartLine:     5, EndLine: 5,
				StartByte:  20,
				EndByte:    50,
				Language:   "sol",
				Confidence: types.ConfExtracted,
				Signature:  "address",
				SubKind:    "storage_public", // W7.2
				SlotIndex:  0,                // W9
			},
		},
		edges: []types.Edge{
			// Sol-shaped edges carrying the new W-C metadata.
			{
				Src: "fn1", Dst: "fn1", Type: types.EdgeInvokes,
				Count: 1, Confidence: types.ConfAmbiguous,
				DispatchKind: "low_level_call", // W7.1
			},
			{
				Src: "fn1", Dst: "fld1", Type: types.EdgeHasModifier,
				Count: 1, Confidence: types.ConfExtracted,
				Order: 2, // W7.3
			},
			// modifies edge from hunk → file-level field (the H2
			// §11 path that BuildPack walks).
			{
				Src: "h1", Dst: "fn1", Type: types.EdgeModifies,
				Count: 1, Confidence: types.ConfExtracted,
			},
			{
				Src: "h2", Dst: "fld1", Type: types.EdgeModifies,
				Count: 1, Confidence: types.ConfExtracted,
			},
		},
		blobs: map[string][]byte{},
	}

	pack, err := BuildPack(store, Options{
		Intent:       "Sol storage layout slot",
		K:            10,
		BudgetTokens: 4000,
	})
	if err != nil {
		t.Fatalf("BuildPack: %v", err)
	}

	// (1) Pack assembled without crashing on the new fields.
	if pack == nil {
		t.Fatalf("BuildPack returned nil pack")
	}

	// (2) §11.3 AMBIGUOUS leak — no commit / hunk with AMBIGUOUS confidence
	//     may surface in hits.
	for _, hit := range pack.Hits {
		if strings.Contains(hit.Commit.SHA, "aaaa") {
			t.Errorf("AMBIGUOUS commit leaked: %+v", hit)
		}
	}

	// (3) Sol commit subjects flow into the Pack so downstream
	//     issue-ID extraction sees them. Asserting >=1 hit is the
	//     loose "didn't break" gate; tighter checks belong in unit
	//     tests of evidence ranking.
	if len(pack.Hits) == 0 {
		t.Errorf("expected >=1 hit on Sol-shaped graph; got 0 (intent=%q)", "Sol storage layout slot")
	}

	// (4) Deterministic ordering by commit timestamp. The fixture
	//     stages c1=1700000100 < c2=1700000200; any hits surfacing
	//     both commits must respect that order.
	var sawC1, sawC2 int
	for i, h := range pack.Hits {
		if strings.Contains(h.Commit.SHA, "1111") {
			sawC1 = i + 1
		}
		if strings.Contains(h.Commit.SHA, "2222") {
			sawC2 = i + 1
		}
	}
	if sawC1 > 0 && sawC2 > 0 && sawC1 < sawC2 {
		// Newer commits should rank first (DESC by timestamp); c2 > c1.
		t.Errorf("hit ordering inverted: c1 at position %d, c2 at position %d (want c2 before c1)", sawC1, sawC2)
	}
}
