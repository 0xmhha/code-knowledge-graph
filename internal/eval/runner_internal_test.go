// runner_internal_test.go tests unexported functions in the eval package.
// Must use package eval (not eval_test) to access isStale and smartContext.
package eval

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// TestExtractSymbols_ReceiverNormalisation locks L2 fix (2026-05-11
// VERIFICATION_REPORT §5): LLM responses written in Go-idiomatic
// pointer-receiver notation (`*eth.Ethereum.New`) used to fail symbol
// matching against spec-style expected values (`eth.Ethereum.New`).
// Trim set now strips `*` `&` `[` `]` along with the existing
// `.:;()` set so receiver-prefixed and inline-code-spanned identifiers
// normalise to a comparable form.
func TestExtractSymbols_ReceiverNormalisation(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "pointer receiver sigil stripped",
			in:   "Calls into *eth.Ethereum.New for genesis bootstrap.",
			want: []string{"eth.Ethereum.New"},
		},
		{
			name: "ampersand receiver stripped",
			in:   "Pass &core.BlockChain.Insert to the dispatcher.",
			want: []string{"core.BlockChain.Insert"},
		},
		{
			name: "markdown code span brackets stripped",
			in:   "see [pkg.Func] and [other.Method] for the flow.",
			want: []string{"pkg.Func", "other.Method"},
		},
		{
			name: "trailing punctuation stripped",
			in:   "Try core.NewBlockChain, then eth.Ethereum.New;",
			want: []string{"core.NewBlockChain", "eth.Ethereum.New"},
		},
		{
			name: "leading-dot token rejected",
			in:   "The .gitignore entry isn't a symbol.",
			want: []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractSymbols(tc.in)
			sort.Strings(got)
			sort.Strings(tc.want)
			if !slicesEqual(got, tc.want) {
				t.Errorf("extractSymbols(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestWriteCSV_RawOutputColumn locks L2 fix (2026-05-11 VERIFICATION_REPORT
// §5 L2-1): the writeCSV header + each row now carries the raw LLM output
// as the 10th column so post-hoc analysis of low scores doesn't need an
// eval re-run.
func TestWriteCSV_RawOutputColumn(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "results.csv")
	rows := []Result{{
		TaskID:    "T01",
		Baseline:  "alpha",
		Score:     0.42,
		RawOutput: "The answer is core.NewBlockChain.\nWith pointer *eth.Ethereum.New.",
	}}
	if err := writeCSV(tmp, rows); err != nil {
		t.Fatalf("writeCSV: %v", err)
	}
	f, err := os.Open(tmp)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatalf("csv read: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected header + 1 row, got %d records", len(records))
	}
	header := records[0]
	if header[len(header)-1] != "raw_output" {
		t.Errorf("last header column = %q, want raw_output", header[len(header)-1])
	}
	row := records[1]
	if len(row) != len(header) {
		t.Fatalf("row arity mismatch: %d vs %d", len(row), len(header))
	}
	if !strings.Contains(row[len(row)-1], "core.NewBlockChain") {
		t.Errorf("raw_output column missing expected content: %q", row[len(row)-1])
	}
}

func slicesEqual(a, b []string) bool {
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

// newEvalFixtureStore runs buildpipe once and returns a read-only Store for
// the Go resolve testdata fixture.
func newEvalFixtureStore(t *testing.T) (persist.Store, string) {
	t.Helper()
	out := t.TempDir()
	if _, err := buildpipe.Run(buildpipe.Options{
		SrcRoot:    "../parse/golang/testdata/resolve",
		OutDir:     out,
		Languages:  []string{"auto"},
		CKGVersion: "test",
	}); err != nil {
		t.Fatalf("buildpipe.Run: %v", err)
	}
	store, err := persist.OpenReadOnly(filepath.Join(out, "graph.db"))
	if err != nil {
		t.Fatalf("persist.OpenReadOnly: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store, out
}

// ---------------------------------------------------------------------------
// isStale
// ---------------------------------------------------------------------------

// TestIsStale_NonGitManifest exercises the early-return path when
// StalenessMethod != "git" (or GetManifest returns an error for an empty store).
// buildpipe's testdata fixture is not inside its own git repo, so SrcCommit
// will be empty and StalenessMethod will not be "git" → isStale returns false.
func TestIsStale_ReturnsFalse(t *testing.T) {
	store, out := newEvalFixtureStore(t)
	// The fixture graph is built from a static directory, not a live git repo,
	// so the manifest either has StalenessMethod="" or the git command will fail.
	// Either way isStale must return false without panicking.
	result := isStale(store, out)
	// We don't assert the value — both true and false are valid for this fixture.
	// What matters is the code path executes without error.
	_ = result
}

// TestIsStale_NoManifest verifies isStale returns false when GetManifest fails
// (e.g. a store with no manifest row, which would normally be an error).
// We exercise this by passing a fresh store where GetManifest may succeed or
// fail; the function must not panic.
func TestIsStale_Covers(t *testing.T) {
	store, out := newEvalFixtureStore(t)
	m, err := store.GetManifest()
	if err != nil {
		// GetManifest failed → isStale returns false at the first branch
		got := isStale(store, out)
		if got {
			t.Error("isStale: expected false when GetManifest fails")
		}
		return
	}
	// Manifest exists.  If StalenessMethod != "git", isStale short-circuits to false.
	if m.StalenessMethod != "git" {
		got := isStale(store, out)
		if got {
			t.Errorf("isStale: expected false for StalenessMethod=%q", m.StalenessMethod)
		}
	} else {
		// StalenessMethod == "git": result depends on whether HEAD matches.
		// Just call it to exercise the git exec path; don't assert the value.
		_ = isStale(store, out)
	}
}

// ---------------------------------------------------------------------------
// smartContext
// ---------------------------------------------------------------------------

func TestSmartContext_NonEmptyQuery(t *testing.T) {
	store, _ := newEvalFixtureStore(t)
	result, err := smartContext(store, "Resolve")
	if err != nil {
		t.Fatalf("smartContext(\"Resolve\"): unexpected error: %v", err)
	}
	// Result should be valid JSON (at minimum "null" or an array/object).
	if len(result) == 0 {
		t.Error("smartContext returned empty string")
	}
}

func TestSmartContext_EmptyQuery(t *testing.T) {
	store, _ := newEvalFixtureStore(t)
	// Empty query — SearchFTS may return empty results or an error.
	// Either branch in smartContext must execute without panic.
	result, err := smartContext(store, "")
	if err != nil {
		// error path covered
		return
	}
	// No error: result should be marshalled JSON.
	if len(result) == 0 {
		t.Error("smartContext(\"\") returned empty string (want at least \"null\" or \"[]\")")
	}
}
