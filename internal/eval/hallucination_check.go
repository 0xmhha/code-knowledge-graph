// Package eval — Hallucination validator (T-04 prototype).
//
// HANDOFF.md T-04 (2026-05-11) flagged hallucination measurement as a
// P0 missing piece of the four-baseline LLM evaluation: LLM responses
// that mention nonexistent symbols (`eth.NewBlockchain` instead of
// `core.NewBlockChain`) flow through scoreTask as if they were valid
// answers, inflating recall when the symbol set extraction happens to
// miss the wrong-but-plausible token. Hallucination measurement is
// the per-response counter that surfaces these.
//
// This V0 layer reuses extractSymbols (the same tokenizer scoreTask
// uses, so we measure exactly the symbols that influence the score),
// looks each unique mention up in the store via FindSymbol, and
// reports both the count and the literal list of unknown symbols.
// The list matters more than the count for triaging: 3 hallucinated
// symbols labelled `eth.X / eth.Y / eth.Z` is one bug ("LLM is
// confusing the eth package"), not three.
//
// V0 scope (call out as separate V1+ work):
//   - FindSymbol matches the last dot-segment as the symbol name.
//     A response that mentions `eth.NewBlockchain` (wrong) when the
//     real symbol is `core.NewBlockChain` (right) lands as a Found
//     entry because the bare name `NewBlockchain` / `NewBlockChain`
//     case-differs but doesn't qname-collide. V0 records the bare
//     match as Found and exposes a separate QnameDiverged list for
//     the manual triage step — the call site decides whether to
//     count divergences against hallucination rate.
//   - Case-insensitive comparison. Sol/Go/TS conventions all
//     case-fold differently; V0 picks lowercase as the comparison
//     key and accepts that `MyContract` vs `mycontract` from a
//     prose response both reduce to the same key.
//   - No fuzzy matching. `NewBlockchain` vs `NewBlockChain` (the
//     real stablenet example) lands in Hallucinated unless the
//     QnameDiverged hit catches it via a partial qname.

package eval

import (
	"strings"

	pkgstore "github.com/0xmhha/code-knowledge-graph/pkg/store"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// HallucinationResult is the per-response classification of the symbol
// mentions extracted from an LLM output.
//
// Rate = len(Hallucinated) / Total. Total = 0 (no mentions extracted)
// produces Rate = 0; the call site that wants to distinguish "answer
// had no symbols" from "answer had only valid symbols" reads Total
// directly.
type HallucinationResult struct {
	// Total mentions extracted from the output, after lowercase
	// deduplication. A response that mentions "core.NewBlockChain"
	// three times contributes 1 to Total.
	Total int

	// Found is the subset of mentions whose bare name (last
	// dot-segment) resolves in the store, *case-insensitively*.
	Found []string

	// QnameDiverged is the subset of Found where the bare name
	// resolved but no candidate node had a qualified name matching
	// the mentioned dotted form. The mention is plausible but the
	// package prefix is wrong (`eth.NewBlockChain` vs
	// `core.NewBlockChain`). V0 surfaces these for manual triage
	// without counting them against Rate.
	QnameDiverged []string

	// Hallucinated is the subset of mentions where the bare name
	// did not resolve at all.
	Hallucinated []string

	// Rate is the hallucination fraction: len(Hallucinated) / Total.
	Rate float64
}

// ValidateMentions classifies every symbol mention in `output` as
// Found, QnameDiverged, or Hallucinated by looking each up in `store`.
// The tokenizer is extractSymbols (the same path scoreTask uses).
//
// store may be nil — in which case every mention is recorded as
// Found with Rate = 0. This keeps call sites that lack a graph (e.g.
// the rubric-only scoring path) from short-circuiting on a nil
// dereference; hallucination measurement is opt-in.
func ValidateMentions(output string, store pkgstore.Reader) (HallucinationResult, error) {
	mentions := extractSymbols(output)
	unique := dedupCaseInsensitive(mentions)
	result := HallucinationResult{Total: len(unique)}

	if store == nil {
		result.Found = append(result.Found, unique...)
		return result, nil
	}

	for _, m := range unique {
		name := lastDotSegment(m)
		if name == "" {
			result.Hallucinated = append(result.Hallucinated, m)
			continue
		}
		nodes, err := store.FindSymbol(name, true, pkgstore.FindSymbolOptions{})
		if err != nil {
			return result, err
		}
		if len(nodes) == 0 {
			// Bare name didn't resolve — try a case-insensitive sweep
			// before giving up. FindSymbol exact=true uses an
			// SQL-level equality; symbol identifiers in mixed-language
			// codebases often case-differ between idiomatic prose and
			// the actual declaration ("vault.deposit" vs
			// "Vault.Deposit"). The case-insensitive sweep prevents a
			// stylistic case mismatch from being charged as
			// hallucination.
			altNodes, alterr := store.FindSymbol(name, false, pkgstore.FindSymbolOptions{})
			if alterr != nil {
				return result, alterr
			}
			ciMatch := false
			for _, n := range altNodes {
				if strings.EqualFold(n.Name, name) {
					ciMatch = true
					nodes = altNodes
					break
				}
			}
			if !ciMatch {
				result.Hallucinated = append(result.Hallucinated, m)
				continue
			}
		}
		result.Found = append(result.Found, m)
		// Qname check: was there at least one candidate whose
		// QualifiedName matches the dotted form the response used?
		// If not, the mention is plausible-but-misqualified.
		if !anyQnameMatch(nodes, m) {
			result.QnameDiverged = append(result.QnameDiverged, m)
		}
	}
	if result.Total > 0 {
		result.Rate = float64(len(result.Hallucinated)) / float64(result.Total)
	}
	return result, nil
}

func dedupCaseInsensitive(xs []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(xs))
	for _, x := range xs {
		key := strings.ToLower(x)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, x)
	}
	return out
}

func lastDotSegment(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 && i+1 < len(s) {
		return s[i+1:]
	}
	return s
}

func anyQnameMatch(nodes []types.Node, mention string) bool {
	for _, n := range nodes {
		if strings.EqualFold(n.QualifiedName, mention) {
			return true
		}
	}
	return false
}
