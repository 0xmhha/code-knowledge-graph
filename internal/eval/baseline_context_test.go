package eval

import (
	"strings"
	"testing"
)

func TestAlphaFileDump_FromExpectedSymbols(t *testing.T) {
	store, _ := newEvalFixtureStore(t)
	srcRoot := "../parse/golang/testdata/resolve"

	task := Task{
		ID:          "test-alpha",
		CorpusPath:  srcRoot,
		Description: "find usages of Greet",
		Expected:    Expected{Symbols: []string{"a.Greet"}},
	}
	dump := alphaFileDump(store, task)
	if !strings.Contains(dump, "Greet") {
		t.Errorf("dump should contain Greet, got: %q", dump[:min(200, len(dump))])
	}
	if !strings.Contains(dump, "=== a/a.go ===") && !strings.Contains(dump, "a.go") {
		t.Errorf("dump should reference a.go, got: %q", dump[:min(200, len(dump))])
	}
}

func TestAlphaFileDump_FallbackToDescription(t *testing.T) {
	store, _ := newEvalFixtureStore(t)
	srcRoot := "../parse/golang/testdata/resolve"

	// Rubric task: no symbols, only description
	task := Task{
		ID:          "test-alpha-rubric",
		CorpusPath:  srcRoot,
		Description: "Greet function implementation",
		Expected:    Expected{Rubric: []string{"greeting"}},
	}
	dump := alphaFileDump(store, task)
	if dump == "" {
		t.Error("dump should not be empty for rubric fallback")
	}
}

func TestBetaSubgraphDump_TaskAware(t *testing.T) {
	store, _ := newEvalFixtureStore(t)
	srcRoot := "../parse/golang/testdata/resolve"

	task := Task{
		ID:          "test-beta",
		CorpusPath:  srcRoot,
		Description: "Greet function",
	}
	dump := betaSubgraphDump(store, task)
	if !strings.Contains(dump, "Nodes:") {
		t.Errorf("dump should contain Nodes section")
	}
	if !strings.Contains(dump, "Edges:") {
		t.Errorf("dump should contain Edges section")
	}
	if !strings.Contains(dump, "Greet") {
		t.Errorf("dump should reference Greet, got first 300: %q", dump[:min(300, len(dump))])
	}
}

func TestBetaSubgraphDump_NoMatch(t *testing.T) {
	store, _ := newEvalFixtureStore(t)
	srcRoot := "../parse/golang/testdata/resolve"

	task := Task{
		CorpusPath:  srcRoot,
		Description: "totally unrelated zzzzz xxxxx",
	}
	dump := betaSubgraphDump(store, task)
	// Should not panic and return either nodes or "[no candidates]"
	if dump == "" {
		t.Error("dump should not be empty string")
	}
}
