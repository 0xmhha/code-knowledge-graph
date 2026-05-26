// # Qualified Name (qname) conventions for external consumers
//
// CKG stores every symbol with a qualified_name (qname) field whose
// format is language-specific but follows a consistent hierarchy:
//
//	Go:         "package.Type.Method"   e.g. "vault.Vault.Deposit"
//	TypeScript: "module.Class.method"   e.g. "auth.AuthService.login"
//	Solidity:   "Contract.function"     e.g. "GovStaking.deposit"
//
// The qname is the primary lookup key for traversal methods:
//
//	Reader.FindSymbol(name, exact, opts)        — exact or suffix match
//	Reader.NeighborhoodByQname(qname, depth, reverse, edgeTypes...)
//	Reader.SubgraphByQname(qname, depth)
//
// # Canonical-helper pattern for cks/ckv
//
// External consumers (cks, ckv) typically receive user input that does
// not match the stored qname exactly — partial names, mixed case, or
// receiver-style prefixes (*pkg.Type). The recommended wrapping pattern
// uses FindSymbol with exact=false (suffix LIKE match) as the
// canonical resolution step, then passes the resolved qname to
// traversal methods:
//
//	r, _ := store.OpenReadOnly("/path/to/graph.db")
//	defer r.Close()
//
//	// Step 1: resolve user input → canonical qname(s).
//	// exact=false matches "%.Deposit" so "Deposit" finds
//	// "vault.Vault.Deposit" without the caller knowing the package.
//	nodes, _ := r.FindSymbol("Deposit", false, store.FindSymbolOptions{
//	    Language: "go",
//	    Kinds:    []string{"Function", "Method"},
//	})
//
//	// Step 2: use the resolved qname for traversal.
//	for _, n := range nodes {
//	    callers, edges, _ := r.NeighborhoodByQname(
//	        n.QualifiedName, 2, true,  // reverse=true → callers
//	    )
//	    // ... use callers, edges
//	}
//
// # Normalisation rules
//
// Before calling FindSymbol, strip these prefixes that leak from AST
// representations but are not stored in the graph:
//
//   - Go pointer receiver: "*pkg.Type.Method" → "pkg.Type.Method"
//   - Go address-of:       "&pkg.New"         → "pkg.New"
//
// CKG's internal extractSymbols already applies these rules (see
// internal/eval/runner.go); external consumers should replicate the
// same strip before lookup to avoid zero-result queries.
//
// # SearchFTS vs FindSymbol
//
// Use FindSymbol when you have an identifier (exact or suffix).
// Use SearchFTS / SearchWithOpts when you have natural-language
// keywords or need BM25 ranking across the full corpus.
// The two paths are complementary — cks's typical flow is:
//
//	ckv (vocab bridge) → exact keywords → ckg FindSymbol or SearchFTS
package store
