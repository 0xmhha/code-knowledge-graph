package eval

import (
	"fmt"
	"strings"
)

// FilterHallucinations rewrites `text` so each symbol mention that
// HallucinationResult flagged as Hallucinated is replaced with an
// inline marker. The function does NOT remove sentences or
// paragraphs — that decision belongs to the consumer (a model
// answer can carry useful prose around a single bad symbol; full
// strip would throw the useful prose away too).
//
// The returned `warnings` slice carries a short human-readable
// summary of every replacement plus the qname-diverged mentions
// (the latter are not replaced — they resolved by bare name and
// might still be correct — but they warrant a flag).
//
// Axis 4 of the 4-axis evaluation roadmap (2026-05-22). Sits
// downstream of T-04 ValidateMentions: the hallucination metric
// only *measures*, the filter *acts on the measurement* before the
// text reaches a consumer. Together they give 0%-error a two-step
// path: prompt-engineering reduces the rate at the source, the
// filter scrubs whatever leaks through.
//
// Design rules:
//   - Replacement only: `[unverified: <symbol>]`. No deletion.
//   - Word-boundary aware: `Token.transferFrom` is not rewritten
//     when `Token.transfer` is flagged. strings.Replace with a
//     boundary check covers the common Go/Sol/TS dotted-identifier
//     shape.
//   - Idempotent: re-filtering an already-filtered text is a no-op
//     because the marker `[unverified: ...]` does not contain a
//     bare dotted identifier the validator would re-flag.
//   - Empty hallucination set → returns text unchanged + nil
//     warnings.
func FilterHallucinations(text string, hallu HallucinationResult) (filtered string, warnings []string) {
	filtered = text
	for _, sym := range hallu.Hallucinated {
		replacement := fmt.Sprintf("[unverified: %s]", sym)
		filtered = replaceWholeToken(filtered, sym, replacement)
		warnings = append(warnings, fmt.Sprintf("rewrote unverified symbol %q", sym))
	}
	for _, sym := range hallu.QnameDiverged {
		warnings = append(warnings,
			fmt.Sprintf("qualified-name mismatch on %q (bare name resolved, but the dotted form did not match any stored qname; verify before relying on this)", sym))
	}
	return filtered, warnings
}

// replaceWholeToken replaces every word-boundary-bounded occurrence
// of `old` in `s` with `new`. Word boundary is defined as either
// end-of-string or a non-identifier rune (anything that is not a
// letter, digit, dot, or underscore). The check on both ends
// prevents `Token.transfer` from accidentally rewriting inside
// `Token.transferFrom` or `xToken.transfer`.
func replaceWholeToken(s, old, repl string) string {
	if old == "" {
		return s
	}
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(s[i:], old)
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		j += i
		// Boundary check on the left.
		left := j == 0 || !isIdentRune(rune(s[j-1]))
		// Boundary check on the right.
		end := j + len(old)
		right := end == len(s) || !isIdentRune(rune(s[end]))
		b.WriteString(s[i:j])
		if left && right {
			b.WriteString(repl)
		} else {
			b.WriteString(old)
		}
		i = end
		if i >= len(s) {
			break
		}
	}
	return b.String()
}

// isIdentRune reports whether r continues a dotted-identifier
// token. Used by replaceWholeToken's word-boundary check.
//
// `.` is intentionally NOT an ident rune here even though
// dotted identifiers contain it: when `.` follows the end of a
// match it could be either (a) the period of a continuing
// dotted identifier (`Token.transfer.foo`) or (b) the period
// of a sentence-ending punctuation (`...Token.transfer.`). Case
// (a) is rare for the kind of mention we replace (the validator
// already saw the symbol as `Token.transfer`, not
// `Token.transfer.foo`); case (b) is the common one. Treating
// `.` as non-ident lets sentence-ending periods match cleanly.
func isIdentRune(r rune) bool {
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	return r == '_'
}
