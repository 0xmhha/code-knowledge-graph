package eval

import (
	"testing"
)

func TestExtractCitations_ColonFormat(t *testing.T) {
	text := `See eth/backend.go:227 for the constructor and core/blockchain.go:45 for the chain init.`
	got := ExtractCitations(text)
	if len(got) != 2 {
		t.Fatalf("got %d citations, want 2: %+v", len(got), got)
	}
	if got[0].File != "eth/backend.go" || got[0].Line != 227 {
		t.Errorf("citation 0: got %+v", got[0])
	}
	if got[1].File != "core/blockchain.go" || got[1].Line != 45 {
		t.Errorf("citation 1: got %+v", got[1])
	}
}

func TestExtractCitations_HashLFormat(t *testing.T) {
	text := `The function is at service/vault.go#L11.`
	got := ExtractCitations(text)
	if len(got) != 1 {
		t.Fatalf("got %d citations, want 1", len(got))
	}
	if got[0].File != "service/vault.go" || got[0].Line != 11 {
		t.Errorf("got %+v", got[0])
	}
}

func TestExtractCitations_Dedup(t *testing.T) {
	text := `See api/handler.go:17 and also api/handler.go:17 again.`
	got := ExtractCitations(text)
	if len(got) != 1 {
		t.Errorf("duplicates not deduped: got %d", len(got))
	}
}

func TestExtractCitations_NoCitations(t *testing.T) {
	text := `This response has no file:line references at all.`
	got := ExtractCitations(text)
	if len(got) != 0 {
		t.Errorf("got %d citations from no-citation text", len(got))
	}
}

func TestExtractCitations_IgnoresURLs(t *testing.T) {
	text := `Visit https://example.com/path:443 for details.`
	got := ExtractCitations(text)
	for _, c := range got {
		if c.File == "https://example.com/path" {
			t.Errorf("should not extract URL as citation: %+v", c)
		}
	}
}

func TestValidateCitations_NilStore(t *testing.T) {
	result, err := ValidateCitations("see file.go:10", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 {
		t.Errorf("total: got %d want 1", result.Total)
	}
	if result.FileExists != 0 {
		t.Errorf("file_exists should be 0 with nil store")
	}
}

func TestValidateCitations_Integration(t *testing.T) {
	store, _ := newEvalFixtureStore(t)

	// resolve fixture has a/a.go and b/b.go.
	pathIndex, err := buildPathIndex(store)
	if err != nil {
		t.Fatal(err)
	}
	var goFiles []string
	for p := range pathIndex {
		goFiles = append(goFiles, p)
	}
	if len(goFiles) < 2 {
		t.Fatalf("expected ≥2 file paths, got %d: %v", len(goFiles), goFiles)
	}
	// Cite line 3 in each — should be within the package declaration at minimum
	text := goFiles[0] + ":3 and " + goFiles[1] + ":3"
	result, err := ValidateCitations(text, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 2 {
		t.Fatalf("total: got %d want 2", result.Total)
	}
	if result.FileExists < 2 {
		t.Errorf("file_exists: got %d want 2, paths used: %v", result.FileExists, goFiles[:2])
	}
	if result.LineInNode < 1 {
		t.Errorf("line_in_node: got %d want ≥1", result.LineInNode)
	}
}

func TestValidateCitations_NonexistentFile(t *testing.T) {
	store, _ := newEvalFixtureStore(t)

	text := `See nonexistent/fake.go:999 for details.`
	result, err := ValidateCitations(text, store)
	if err != nil {
		t.Fatal(err)
	}
	if result.FileExists != 0 {
		t.Errorf("file_exists: got %d want 0", result.FileExists)
	}
	if len(result.Hallucinated) != 1 {
		t.Errorf("hallucinated: got %d want 1", len(result.Hallucinated))
	}
}
