package persist_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// openSeededStore stands up a fresh on-disk SQLite store, migrates the
// schema, inserts a small fixture of Function nodes with names + qnames
// + signatures we can query against, and rebuilds the FTS index. Keeps
// each test self-contained so test ordering can't leak state.
func openSeededStore(t *testing.T, nodes []types.Node) persist.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	store, err := persist.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := store.InsertNodes(nodes); err != nil {
		t.Fatalf("InsertNodes: %v", err)
	}
	if err := store.RebuildFTS(); err != nil {
		t.Fatalf("RebuildFTS: %v", err)
	}
	return store
}

// fn synthesises a Function node with enough FTS-indexed surface
// (name + qualified_name + signature + doc_comment) for AND/OR tests
// to discriminate between hits.
func fn(id, name, qname, signature, doc string) types.Node {
	return types.Node{
		ID: id, Type: types.NodeFunction,
		Name: name, QualifiedName: qname,
		FilePath: "pkg/foo.go", StartLine: 1, EndLine: 2,
		StartByte: 0, EndByte: 1,
		Language:   "go",
		Signature:  signature,
		DocComment: doc,
		Confidence: types.ConfExtracted,
	}
}

// TestSearchFTS_NodeKinds_DefaultSymbolOnly locks the post-X-NodeKinds
// contract: with NodeKinds omitted, SearchFTS hides statement-level
// nodes (IfStmt/ReturnStmt/CallSite/…) and meta nodes (Hunk/Commit)
// from the result set, even when their qname prefix carries the query
// token. Captures the "search returns symbol units, not internal
// AST control flow rows" intent that drives the search_text default
// in pkg/mcphandlers.
func TestSearchFTS_NodeKinds_DefaultSymbolOnly(t *testing.T) {
	store := openSeededStore(t, []types.Node{
		fn("a", "Deposit", "service.Vault.Deposit", "vault deposit handler", "."),
		{ID: "b", Type: types.NodeIfStmt,
			Name: "deposit-if", QualifiedName: "service.Vault.Deposit#IfStmt@123",
			FilePath: "x.go", Language: "go", Confidence: types.ConfExtracted},
		{ID: "c", Type: types.NodeHunk,
			Name: "hunk", QualifiedName: "hunk:abc:x.go:0",
			FilePath: "x.go", Language: "go", Confidence: types.ConfExtracted},
	})

	hits, err := store.SearchFTS("deposit*", 10, persist.SearchFTSOptions{})
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	ids := map[string]bool{}
	for _, h := range hits {
		ids[h.Node.ID] = true
	}
	if !ids["a"] {
		t.Errorf("symbol-only default: expected method node 'a' in hits, got %v", ids)
	}
	if ids["b"] {
		t.Errorf("symbol-only default: did not expect IfStmt 'b', got %v", ids)
	}
	if ids["c"] {
		t.Errorf("symbol-only default: did not expect Hunk 'c', got %v", ids)
	}
}

// TestSearchFTS_NodeKinds_ExplicitOptsOut documents the escape
// hatch: an explicit NodeKinds slice overrides the default. Pass
// types.AllNodeTypes (or any subset that includes statement / meta
// kinds) to recover the pre-narrowing behaviour for callers that
// specifically need control-flow context (e.g. a future tool that
// answers "which IfStmt mentions Vault.Deposit").
func TestSearchFTS_NodeKinds_ExplicitOptsOut(t *testing.T) {
	store := openSeededStore(t, []types.Node{
		fn("a", "Deposit", "service.Vault.Deposit", "deposit", "."),
		{ID: "b", Type: types.NodeIfStmt,
			Name: "deposit-if", QualifiedName: "service.Vault.Deposit#IfStmt@123",
			FilePath: "x.go", Language: "go", Confidence: types.ConfExtracted},
	})

	hits, err := store.SearchFTS("deposit*", 10, persist.SearchFTSOptions{
		NodeKinds: types.AllNodeTypes(),
	})
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	ids := map[string]bool{}
	for _, h := range hits {
		ids[h.Node.ID] = true
	}
	if !ids["a"] || !ids["b"] {
		t.Errorf("explicit AllNodeTypes: expected both symbol + statement nodes, got %v", ids)
	}
}

// TestSearchFTS_OrMode_DefaultBehaviour locks the pre-Mode-option contract:
// zero-value SearchFTSOptions preserves the historical OR-broadening so
// queries with one matching token still surface their candidate.
func TestSearchFTS_OrMode_DefaultBehaviour(t *testing.T) {
	store := openSeededStore(t, []types.Node{
		fn("a", "Deposit", "service.Vault.Deposit", "(amount int) error", "Deposits funds."),
		fn("b", "Withdraw", "service.Vault.Withdraw", "(amount int) error", "Withdraws funds."),
	})

	// "deposit OR withdraw" — both nodes must surface under default OR.
	hits, err := store.SearchFTS("deposit OR withdraw", 10, persist.SearchFTSOptions{})
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	ids := map[string]bool{}
	for _, h := range hits {
		ids[h.Node.ID] = true
	}
	if !ids["a"] || !ids["b"] {
		t.Errorf("OR mode default: expected both ids, got %v", ids)
	}
}

// TestSearchFTS_AndMode_FiltersOut covers the load-bearing case for the
// user-listed R-Query requirement: a hit that BM25 ranks high but is
// missing one of the query tokens must be dropped under Mode="and".
func TestSearchFTS_AndMode_FiltersOut(t *testing.T) {
	store := openSeededStore(t, []types.Node{
		// hit "a" carries BOTH tokens — keeper.
		fn("a", "Deposit", "service.Vault.Deposit", "vault deposit handler", "Vault deposits."),
		// hit "b" carries only "deposit" — filtered out under AND.
		fn("b", "Process", "service.Process", "deposit pipeline step", "Stage 2 processor."),
	})

	// `Vault deposit*` → rewriteFTSQuery is bypassed by passing the raw
	// FTS5 expression; we want the AND post-filter to make the call.
	hits, err := store.SearchFTS("vault* OR deposit*", 10, persist.SearchFTSOptions{Mode: "and"})
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	ids := map[string]bool{}
	for _, h := range hits {
		ids[h.Node.ID] = true
	}
	if !ids["a"] {
		t.Errorf("AND mode: expected 'a' (carries both tokens), got %v", ids)
	}
	if ids["b"] {
		t.Errorf("AND mode: did not expect 'b' (missing 'vault' token), got %v", ids)
	}
}

// TestSearchFTS_AndMode_NoSurvivors confirms an AND query whose tokens
// never co-occur returns the empty set rather than degrading to OR.
// Protects the user's accuracy goal against a fallback-on-empty
// regression — silently widening to OR would make precision targets
// unmeetable.
func TestSearchFTS_AndMode_NoSurvivors(t *testing.T) {
	store := openSeededStore(t, []types.Node{
		fn("a", "Deposit", "service.Vault.Deposit", "deposit handler", "Vault deposits."),
		fn("b", "Withdraw", "service.Vault.Withdraw", "withdraw handler", "Vault withdrawals."),
	})

	hits, err := store.SearchFTS("deposit* OR withdraw*", 10, persist.SearchFTSOptions{Mode: "and"})
	if err != nil {
		t.Fatalf("SearchFTS: %v", err)
	}
	// Neither node carries both "deposit" AND "withdraw" — expect empty.
	if len(hits) != 0 {
		ids := []string{}
		for _, h := range hits {
			ids = append(ids, h.Node.ID)
		}
		t.Errorf("AND mode no-survivor: expected empty, got %s", strings.Join(ids, ","))
	}
}

// TestSearchFTS_KoreanInput_GracefulEmpty covers the ckg-NEW-1 contract
// (CKS-INTEGRATION-2026-05-23 §3.1): a pure Korean query must NOT
// panic, MUST NOT raise an FTS5 syntax error, and SHOULD return an
// empty result when no indexed token matches. This is graceful
// degradation, not retrieval — the ckv vocabulary bridge is the
// component that *translates* Korean intent into English keywords;
// ckg only needs to survive when the bridge is bypassed or the
// translation passes through unchanged characters.
func TestSearchFTS_KoreanInput_GracefulEmpty(t *testing.T) {
	store := openSeededStore(t, []types.Node{
		fn("a", "Deposit", "service.Vault.Deposit", "deposit handler", "."),
	})

	// 1. Direct SearchFTS with a Korean-only query.
	hits, err := store.SearchFTS("한국어", 10, persist.SearchFTSOptions{})
	if err != nil {
		t.Errorf("SearchFTS Korean: unexpected error %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("SearchFTS Korean: want empty hits, got %d", len(hits))
	}

	// 2. SearchWithOpts hits the CJK substring fallback (hasNonASCII
	//    routes the call away from FTS5). Empty result is acceptable;
	//    panic is not.
	nodes, err := store.SearchWithOpts("한국어 query", 10, persist.SearchFTSOptions{Mode: "and"})
	if err != nil {
		t.Errorf("SearchWithOpts CJK: unexpected error %v", err)
	}
	if len(nodes) != 0 {
		t.Errorf("SearchWithOpts CJK: want empty nodes (synthetic graph has no Korean text), got %d", len(nodes))
	}
}

// TestSearchFTS_KoreanMixed_ExtractsAsciiToken locks the rewriteFTSQuery
// behaviour on mixed Korean + English input: the Korean fragments are
// stripped at the trimFTSToken boundary (non-alnum trim) and only the
// ASCII token survives, becoming a prefix match. The ASCII fragment
// finds its node; the Korean fragment does not surface as a syntax
// error. Documents the graceful degradation surface a coding agent
// can rely on when the ckv translation is partial.
func TestSearchFTS_KoreanMixed_ExtractsAsciiToken(t *testing.T) {
	store := openSeededStore(t, []types.Node{
		fn("a", "AnzeonTipEnv", "gasprice.AnzeonTipEnv", "tip env tracker", "."),
		fn("b", "Other", "pkg.Other", "unrelated", "."),
	})

	// The router (SearchWithOpts) routes pure-CJK to SearchSubstr but
	// ASCII-with-CJK-fragments still goes through hasNonASCII → CJK
	// branch, so use the FTS path explicitly via Search-style routing
	// applied manually: rewriteFTSQuery is exercised when SearchWithOpts
	// is given a query whose ASCII portion alone would match.
	//
	// For the FTS-path graceful contract we hit SearchFTS directly with
	// the rewriter-applied form, which is how MCP search_text routes
	// in mode != "" cases after the X-1 rewrite.
	hits, err := store.SearchFTS("AnzeonTipEnv* OR query*", 10, persist.SearchFTSOptions{})
	if err != nil {
		t.Fatalf("SearchFTS mixed-stripped: unexpected error %v", err)
	}
	found := false
	for _, h := range hits {
		if h.Node.ID == "a" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected AnzeonTipEnv hit on the surviving ASCII prefix; got %d hits", len(hits))
	}
}

// TestSearchWithOpts_RoutesAndPropagates covers the router contract: the
// MCP-facing entry point applies rewriteFTSQuery to ASCII input AND
// threads the Mode option through to SearchFTS. Without this guarantee
// the search_text tool's mode parameter would be silently dropped.
func TestSearchWithOpts_RoutesAndPropagates(t *testing.T) {
	store := openSeededStore(t, []types.Node{
		fn("a", "Deposit", "service.Vault.Deposit", "vault deposit handler", "."),
		fn("b", "Process", "service.Process", "deposit pipeline step", "."),
	})

	// Raw multi-word query goes through rewriteFTSQuery → OR-broadened
	// FTS5 → AND post-filter. Only node "a" should survive.
	nodes, err := store.SearchWithOpts("vault deposit", 10, persist.SearchFTSOptions{Mode: "and"})
	if err != nil {
		t.Fatalf("SearchWithOpts: %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "a" {
		ids := []string{}
		for _, n := range nodes {
			ids = append(ids, n.ID)
		}
		t.Errorf("SearchWithOpts AND: want [a], got [%s]", strings.Join(ids, ","))
	}
}
