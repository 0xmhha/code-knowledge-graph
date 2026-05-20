package persist

import (
	"testing"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// W-C W11 V12 — Sol-only marker payload decision lock.
//
// Background: every field on types.Node serialised through
// nodeAttrs originates in a Sol parser pass (W6-W10 markers
// around using-for, function pointers, storage layout, low-
// level call surfaces, etc.). Go and TS parsers leave the
// marker fields at their zero values, so marshalNodeAttrs
// returns "" for Go/TS nodes — the attrs column stays NULL,
// keeping the DB diff minimal.
//
// V12 audits and locks the design choice. Two alternative
// directions exist:
//
//  1. Promote a subset of markers to language-agnostic
//     semantics. e.g. HasAssembly could mirror Go's `import
//     "unsafe"` users or TS's `//@ts-ignore` populations.
//     Each markers carries Sol-specific semantics today and
//     cross-language reuse would need fresh per-language
//     detection — out of scope for V12.
//
//  2. Add language-specific markers under a polymorphic
//     payload (e.g. attrs JSON with a "language" tag).
//     Increases payload size and adds branch logic at every
//     read site for marginal gain. Better served by separate
//     Node fields with their own omitempty tags.
//
// V12 keeps the current design: nodeAttrs fields are Sol
// markers; Go/TS nodes serialise to "" and round-trip cleanly
// as zero-valued markers on the read side.
//
// The test asserts:
//   - A Go node with no markers serialises to "".
//   - A TS node with no markers serialises to "".
//   - A Sol node with a marker serialises to a non-empty JSON.
func TestAttrs_LanguageScopeRemainsSolOnly(t *testing.T) {
	// Go-language node, no markers — must serialise to "".
	goNode := types.Node{
		Type: types.NodeFunction, Language: "go",
		Name: "doThing", QualifiedName: "pkg/doThing",
	}
	if blob := marshalNodeAttrs(&goNode); blob != "" {
		t.Errorf("Go node with no markers should serialise to empty; got %q", blob)
	}

	// TS-language node, no markers — must serialise to "".
	tsNode := types.Node{
		Type: types.NodeFunction, Language: "ts",
		Name: "doThing", QualifiedName: "src/doThing",
	}
	if blob := marshalNodeAttrs(&tsNode); blob != "" {
		t.Errorf("TS node with no markers should serialise to empty; got %q", blob)
	}

	// Sol-language node with a marker — must serialise to JSON.
	solNode := types.Node{
		Type: types.NodeFunction, Language: "sol",
		Name: "withdraw", QualifiedName: "Wallet.withdraw",
		HasExternalCall: true,
	}
	if blob := marshalNodeAttrs(&solNode); blob == "" {
		t.Errorf("Sol node with HasExternalCall=true should serialise to non-empty JSON")
	}
}
