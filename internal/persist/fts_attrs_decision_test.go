package persist

import (
	"strings"
	"testing"
)

// W-C W11 V10 — FTS5 attrs indexing decision lock.
//
// Background: W11 V7 added the nodes.attrs JSON-blob column.
// A natural follow-up would index attrs into nodes_fts so a
// security reviewer typing "external call" could find callables
// with HasExternalCall=true. V10 audited that option and
// declined it for two reasons:
//
//  1. JSON noise. The blob contains keys like
//     "has_external_call" alongside the boolean values. Adding
//     attrs to FTS would expose those keys to user-facing
//     search results — typing "external" would match every
//     node with HasExternalCall=false too (the key is present
//     in the JSON whether the value is true or false until the
//     omitempty optimisation kicks in for false). Worse,
//     typing "function" matches every fn-typed node AND every
//     node whose JSON mentions "has_function_*" keys.
//
//  2. Marker queries want boolean semantics. "Show me every
//     callable with HasExternalCall=true" is a filter, not a
//     relevance-ranked search. A direct SQL query
//     (json_extract(attrs, '$.has_external_call') = 1) serves
//     it better than FTS5 ranking would.
//
// This audit locks the decision by asserting the schema's FTS5
// virtual table only carries the four original columns. If a
// future change adds attrs to nodes_fts, this test fails — the
// regression should be conscious, with the noise / semantics
// trade-off re-evaluated.
func TestFTS5_DoesNotIndexAttrs(t *testing.T) {
	// schemaSQL is the embedded schema.sql contents. We grep
	// the CREATE VIRTUAL TABLE block for its column list.
	idx := strings.Index(schemaSQL, "CREATE VIRTUAL TABLE IF NOT EXISTS nodes_fts")
	if idx < 0 {
		t.Fatalf("schemaSQL: nodes_fts CREATE statement not found")
	}
	endIdx := strings.Index(schemaSQL[idx:], ");")
	if endIdx < 0 {
		t.Fatalf("schemaSQL: nodes_fts statement not terminated")
	}
	fts := schemaSQL[idx : idx+endIdx]
	if strings.Contains(fts, "attrs") {
		t.Errorf("nodes_fts indexes attrs — review the V10 decision and remove this test or update the rationale")
	}
	// Affirm the intended four columns ARE there so the audit
	// also catches a silent FTS column rename.
	for _, col := range []string{"name", "qualified_name", "signature", "doc_comment"} {
		if !strings.Contains(fts, col) {
			t.Errorf("nodes_fts missing expected column %q", col)
		}
	}
}
