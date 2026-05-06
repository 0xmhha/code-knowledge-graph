package bm25

import (
	"math"
	"reflect"
	"testing"
)

func TestTokenize_CamelCase(t *testing.T) {
	got := Tokenize("parseFile")
	want := []string{"parsefile", "parse", "file"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseFile: got %v want %v", got, want)
	}
}

func TestTokenize_AllCapsThenWord(t *testing.T) {
	got := Tokenize("URLParser")
	want := []string{"urlparser", "url", "parser"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("URLParser: got %v want %v", got, want)
	}
}

func TestTokenize_SnakeCase(t *testing.T) {
	got := Tokenize("read_file")
	want := []string{"read_file", "read", "file"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("read_file: got %v want %v", got, want)
	}
}

func TestTokenize_QName(t *testing.T) {
	got := Tokenize("pkg.Type.Method")
	want := []string{"pkg", "type", "method"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pkg.Type.Method: got %v want %v", got, want)
	}
}

func TestTokenize_DigitBoundary(t *testing.T) {
	got := Tokenize("v1Beta2")
	// "v1Beta2" → split: v|1|Beta|2 ; lowered: ["v1beta2", "beta"]
	// Note: "v" and "1" and "2" all have len<2 → filtered
	want := []string{"v1beta2", "beta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("v1Beta2: got %v want %v", got, want)
	}
}

func TestTokenize_Empty(t *testing.T) {
	if got := Tokenize(""); got != nil {
		t.Errorf("empty input: got %v want nil", got)
	}
}

// TestOkapi_Sanity exercises the documented monotonicity: for a fixed
// query, a doc that contains the query term scores higher than one that
// does not, and a longer doc with the same TF scores lower than a
// shorter one (length normalization at B=0.75).
func TestOkapi_Sanity(t *testing.T) {
	o := NewOkapi()
	o.Index([]Document{
		{ID: "match-short", Tokens: []string{"alpha", "beta"}},
		{ID: "match-long", Tokens: []string{"alpha", "beta", "gamma", "delta", "epsilon"}},
		{ID: "no-match", Tokens: []string{"omega", "psi"}},
	})

	hits := o.TopK([]string{"alpha"}, 0)
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits (no-match excluded), got %d: %+v", len(hits), hits)
	}
	if hits[0].ID != "match-short" {
		t.Errorf("expected match-short to rank first (length norm), got %s", hits[0].ID)
	}
	if hits[1].ID != "match-long" {
		t.Errorf("expected match-long to rank second, got %s", hits[1].ID)
	}
}

// TestOkapi_TFSaturation: a doc with two matches should outscore a doc
// with one match (TF effect), but the gap is sub-linear because of K1.
func TestOkapi_TFSaturation(t *testing.T) {
	o := NewOkapi()
	o.Index([]Document{
		{ID: "single", Tokens: []string{"alpha", "x", "y", "z"}},
		{ID: "double", Tokens: []string{"alpha", "alpha", "y", "z"}},
	})
	hits := o.TopK([]string{"alpha"}, 0)
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].ID != "double" {
		t.Errorf("doc with TF=2 should outscore TF=1, got %s first", hits[0].ID)
	}
	// Scores should not exactly double — TF saturation must compress.
	if hits[0].Score >= 2*hits[1].Score {
		t.Errorf("TF saturation failed: tf=2 score %.3f >= 2*tf=1 score %.3f",
			hits[0].Score, hits[1].Score)
	}
}

// TestOkapi_EmptyAndUnknown — robustness: empty corpus, unknown doc,
// empty query string. None should panic, all should return zero values.
func TestOkapi_EmptyAndUnknown(t *testing.T) {
	o := NewOkapi()
	if got := o.Score([]string{"x"}, "missing"); got != 0 {
		t.Errorf("score on empty corpus: got %v want 0", got)
	}
	o.Index(nil)
	if got := o.TopK([]string{"x"}, 5); len(got) != 0 {
		t.Errorf("empty corpus topk: got %v want []", got)
	}

	o.Index([]Document{{ID: "a", Tokens: []string{"foo"}}})
	if got := o.Score(nil, "a"); got != 0 {
		t.Errorf("empty query: got %v want 0", got)
	}
	if got := o.Score([]string{""}, "a"); got != 0 {
		t.Errorf("empty term: got %v want 0", got)
	}
	if got := o.Score([]string{"foo"}, "ghost"); got != 0 {
		t.Errorf("unknown doc: got %v want 0", got)
	}
}

// TestOkapi_IDFCommonTerms — the modern smoothing IDF should yield a
// non-negative score even when the query term appears in every document.
// The classic Robertson IDF would go negative; the +1 inside log fixes it.
func TestOkapi_IDFCommonTerms(t *testing.T) {
	o := NewOkapi()
	docs := make([]Document, 10)
	for i := range docs {
		docs[i] = Document{ID: string(rune('a' + i)), Tokens: []string{"common"}}
	}
	o.Index(docs)
	idf := o.idf("common")
	if idf < 0 {
		t.Errorf("IDF for ubiquitous term went negative: %v", idf)
	}
	// math.Log(1 + 0.5/10.5) ≈ 0.0465
	want := math.Log(1 + 0.5/10.5)
	if math.Abs(idf-want) > 1e-9 {
		t.Errorf("IDF formula drift: got %v want %v", idf, want)
	}
}
