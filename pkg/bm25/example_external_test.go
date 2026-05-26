package bm25_test

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/bm25"
)

// TestExternalConsumer_IndexAndQuery exercises the public API from an
// external package — the same import path ckv/cks will use. If this
// file stops compiling, the external contract is broken.
func TestExternalConsumer_IndexAndQuery(t *testing.T) {
	var scorer bm25.Scorer = bm25.NewOkapi()

	docs := []bm25.Document{
		{ID: "node-1", Tokens: bm25.Tokenize("HandleDeposit")},
		{ID: "node-2", Tokens: bm25.Tokenize("processWithdraw")},
		{ID: "node-3", Tokens: bm25.Tokenize("validateDepositAmount")},
	}
	scorer.Index(docs)

	hits := scorer.TopK(bm25.Tokenize("deposit"), 10)
	if len(hits) == 0 {
		t.Fatal("expected at least one hit for 'deposit'")
	}
	for _, h := range hits {
		if h.ID == "node-2" {
			t.Error("processWithdraw should not match 'deposit'")
		}
		if h.Score <= 0 {
			t.Errorf("hit %s has non-positive score %f", h.ID, h.Score)
		}
	}

	if s := scorer.Score(bm25.Tokenize("deposit"), "node-1"); s <= 0 {
		t.Errorf("direct Score for node-1 should be positive, got %f", s)
	}
}

// TestExternalConsumer_DefaultHyperparams verifies the exported
// constants match what NewOkapi returns — external callers may
// reference these for documentation or override detection.
func TestExternalConsumer_DefaultHyperparams(t *testing.T) {
	o := bm25.NewOkapi()
	if o.K1 != bm25.DefaultK1 {
		t.Errorf("K1: got %f want %f", o.K1, bm25.DefaultK1)
	}
	if o.B != bm25.DefaultB {
		t.Errorf("B: got %f want %f", o.B, bm25.DefaultB)
	}
}
