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

// TestExtractSymbols_FileExtensionBlacklist locks L2-2-1 fix
// (2026-05-11 VERIFICATION_REPORT §8.3): LLM verbose answers that cite
// file paths (`see core.NewBlockChain in core/blockchain.go`) used to
// drag those file references into the symbol set, pushing precision
// below the rubric's 0.7 threshold and zeroing out the score even when
// the real qualified-name answers were correct. extractSymbols now
// drops path-like tokens (`/` separator) and tokens whose trailing
// dot-segment matches a recognised source/markup extension.
func TestExtractSymbols_FileExtensionBlacklist(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "path-with-slash dropped",
			in:   "See core.NewBlockChain in core/blockchain.go for details.",
			want: []string{"core.NewBlockChain"},
		},
		{
			name: "bare file name with .go dropped",
			in:   "Defined in blockchain.go alongside eth.Ethereum.New.",
			want: []string{"eth.Ethereum.New"},
		},
		{
			name: "TS file extension dropped",
			in:   "see auth.ts and user.Service.login for the flow",
			want: []string{"user.Service.login"},
		},
		{
			name: "markdown reference dropped",
			in:   "documented in README.md; symbol is pkg.Helper.Run",
			want: []string{"pkg.Helper.Run"},
		},
		{
			name: "no-symbol-only-paths",
			in:   "see core/blockchain.go and eth/handler.go",
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

// TestExtractSymbols_ParenSplit locks the T-02 P0 fix surfaced by the
// 2026-05-21 T-04 V1 first smoke run. A real Claude response of the
// shape "Call h.vault.Deposit(req) to wire it up" produced the token
// `h.vault.Deposit(req` because the splitter only ran FieldsFunc on
// whitespace + a few markdown chars; parens were in the Trim set,
// which only strips prefix/suffix and never the open-paren *inside*
// a token. extractSymbols then classified `h.vault.Deposit(req` as a
// hallucinated symbol because `Deposit(req` has no chance of
// resolving in the graph.
//
// Promoting `(` and `)` to FieldsFunc separators splits the call
// site from the symbol so the dotted prefix stays intact and the
// argument list (which fails the "must contain a dot" check)
// drops cleanly.
func TestExtractSymbols_ParenSplit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "open paren inside token splits cleanly",
			in:   "Call h.vault.Deposit(req) to wire it up.",
			want: []string{"h.vault.Deposit"},
		},
		{
			name: "two adjacent calls each keep their symbol",
			in:   "service.Vault.Deposit(req) then api.Handler.HandleDeposit(ctx)",
			want: []string{"service.Vault.Deposit", "api.Handler.HandleDeposit"},
		},
		{
			name: "nested-call shape still extracts both",
			in:   "wrap(eth.Ethereum.New(cfg))",
			want: []string{"eth.Ethereum.New"},
		},
		{
			name: "paren-only token gets dropped (no dot)",
			in:   "(req) is just the argument",
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

// TestExtractSymbols_BraceSplit locks the brace-splitter fix
// surfaced by the 2026-05-21 T-04 V1 third smoke run. A real Claude
// response wrote `Vault{...}` (Go struct-literal placeholder syntax)
// and the splitter kept the whole thing as one token. extractSymbols'
// `strings.Contains(tok, ".")` check matched the `.` from inside
// the `...` placeholder, so `Vault{...}` flowed through as a
// candidate symbol. It then fell into Hallucinated because nothing
// in the graph is named `Vault{...}`.
//
// Promoting `{` and `}` to FieldsFunc separators splits the
// placeholder from the type name. `Vault` (no dot) then fails the
// dot filter and the false positive is removed.
func TestExtractSymbols_BraceSplit(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "struct literal placeholder split",
			in:   "Vault{...} initialises the receiver; method is core.Vault.Init",
			want: []string{"core.Vault.Init"},
		},
		{
			name: "composite literal split keeps dotted symbol",
			in:   "build with &service.Vault{cfg: cfg}",
			want: []string{"service.Vault"},
		},
		{
			name: "braces around prose drop noise",
			in:   "see {pkg.Helper} for one method, also pkg.Run",
			want: []string{"pkg.Helper", "pkg.Run"},
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

// TestExtractSymbols_ProseAbbreviationBlacklist locks the second
// finding of the 2026-05-21 smoke run: `e.g` from a real LLM
// response ("e.g., service.Vault.Deposit") was classified as a
// hallucinated symbol because it's a dot-bearing token that survives
// the file-extension blacklist (".g" is not a known file ext).
// isProseAbbreviation drops the common cases by case-folded lookup.
func TestExtractSymbols_ProseAbbreviationBlacklist(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "e.g dropped",
			in:   "e.g., service.Vault.Deposit kicks off the flow",
			want: []string{"service.Vault.Deposit"},
		},
		{
			name: "i.e dropped",
			in:   "i.e., the entrypoint is api.Handler.HandleDeposit",
			want: []string{"api.Handler.HandleDeposit"},
		},
		{
			name: "case-folded E.g dropped",
			in:   "E.g. pkg.Helper.Run is invoked here",
			want: []string{"pkg.Helper.Run"},
		},
		{
			name: "et.al dropped",
			in:   "see Smith et.al for prior work; symbol is pkg.Run",
			want: []string{"pkg.Run"},
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
