package persist

import (
	"fmt"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// SearchFTS executes an FTS5 MATCH against nodes_fts and returns the joined
// node rows with relevance scores. Caller is responsible for forming a valid
// FTS5 query string.
//
// The projection is fully qualified with the n.* alias because nodes_fts
// shares column names (name, qualified_name, signature, doc_comment) with
// the nodes content table — bare references would be ambiguous.
//
// SQLite's bm25() returns a *negative* score where smaller (more negative)
// means a stronger match. We sign-flip via `-bm25(...)` so RawScore follows
// the "higher is better" convention shared with the PostgreSQL backend.
// Score is then min-max normalized to [0, 1] within the result set by
// normalizeSearchHits — see SearchHit doc for the consumer contract.
//
// opts.Language pushes a `n.language = ?` predicate into the WHERE clause
// when non-empty. Filtering at the SQL layer (rather than client-side after
// over-fetching) is the CKG-2 fix that removes cks's FilterOverfetchRatio=3
// workaround.
func (s *sqliteStore) SearchFTS(q string, limit int, opts SearchFTSOptions) ([]SearchHit, error) {
	// Mode="and" over-fetches before applying the per-token presence
	// filter so the post-filter doesn't starve recall on small pages.
	// The 3× ratio mirrors the cks workaround that CKG-2 retired
	// (FilterOverfetchRatio=3); the floor of 30 keeps tiny pages
	// (limit=1, 5) from collapsing to zero survivors when a single
	// hit happens to miss the AND set.
	effectiveLimit := limit
	if opts.Mode == "and" {
		effectiveLimit = max(limit*3, 30)
	}
	// NodeKinds: zero value (nil or empty) defaults to symbol-only.
	// Pushed down as a `WHERE n.type IN (...)` predicate so the FTS
	// index can shed statement/meta/path rows before BM25 scoring
	// even sees them — cheaper than a post-filter and keeps the
	// over-fetch math (Mode="and") honest because the filtered set
	// IS the candidate pool the post-filter then narrows further.
	kinds := opts.NodeKinds
	if len(kinds) == 0 {
		kinds = types.SymbolNodeTypes()
	}
	sql := `SELECT n.id, n.type, n.name, n.qualified_name, n.file_path,
		n.start_line, n.end_line, n.start_byte, n.end_byte, n.language,
		COALESCE(n.visibility,''), COALESCE(n.signature,''), COALESCE(n.doc_comment,''),
		COALESCE(n.complexity,0), n.in_degree, n.out_degree, n.pagerank, n.usage_score,
		n.confidence, COALESCE(n.sub_kind,''), COALESCE(n.attrs,''),
		-bm25(nodes_fts) AS raw_score
		FROM nodes_fts f
		JOIN nodes n ON n.rowid = f.rowid
		WHERE nodes_fts MATCH ?`
	args := []any{q}
	if opts.Language != "" {
		sql += ` AND n.language = ?`
		args = append(args, opts.Language)
	}
	sql += ` AND n.type IN (` + placeholders(len(kinds)) + `)`
	for _, k := range kinds {
		args = append(args, string(k))
	}
	sql += ` ORDER BY raw_score DESC LIMIT ?`
	args = append(args, effectiveLimit)

	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, fmt.Errorf("fts search %q: %w", q, err)
	}
	defer func() { _ = rows.Close() }()
	hits, err := scanSearchHits(rows)
	if err != nil {
		return nil, err
	}
	if opts.Mode == "and" {
		tokens := tokenizeAndQuery(q)
		if len(tokens) > 1 {
			hits = filterHitsByAllTokens(hits, tokens)
		}
		if len(hits) > limit {
			hits = hits[:limit]
		}
	}
	normalizeSearchHits(hits)
	return hits, nil
}

// Search is the smart search front door used by the HTTP /api/search
// handler AND the MCP `search_text` / `get_context_for_task` tools. It
// routes between FTS5 (English / token-aligned) and a substring fallback
// (CJK / non-tokenisable), and rewrites benign single-token ASCII queries
// to a prefix match (`gene` → `gene*`) so callers don't need to know
// FTS5 syntax. See docs/VIEWER-ROADMAP.md L1/L2 for the option matrix.
//
// Centralising the routing here removes the divergence between
// handleSearch (auto-prefix + CJK) and buildContext (raw FTS, prose
// queries silently `not_found`); both now call this and get the same
// behaviour.
func (s *sqliteStore) Search(q string, limit int) ([]types.Node, error) {
	return s.SearchWithOpts(q, limit, SearchFTSOptions{})
}

// SearchWithOpts threads SearchFTSOptions (Language filter, Mode for
// AND/OR multi-token combining) through the same router as Search.
// CJK input is still resolved by the substring fallback — opts is
// dropped on that branch because substring search has no notion of
// per-token AND/OR.
func (s *sqliteStore) SearchWithOpts(q string, limit int, opts SearchFTSOptions) ([]types.Node, error) {
	if hasNonASCII(q) {
		return s.SearchSubstr(q, limit)
	}
	hits, err := s.SearchFTS(rewriteFTSQuery(q), limit, opts)
	if err != nil {
		return nil, err
	}
	return nodesFromHits(hits), nil
}

// nodesFromHits unpacks SearchHit.Node so callers that don't care about
// scores can keep their existing []types.Node contract. Order is preserved
// from the input (SearchFTS already sorts by relevance).
func nodesFromHits(hits []SearchHit) []types.Node {
	out := make([]types.Node, len(hits))
	for i, h := range hits {
		out[i] = h.Node
	}
	return out
}

// hasNonASCII reports whether q contains any byte ≥ 0x80. Drives the
// CJK-routing branch in Search.
func hasNonASCII(q string) bool {
	for i := 0; i < len(q); i++ {
		if q[i] >= 0x80 {
			return true
		}
	}
	return false
}

// rewriteFTSQuery turns a casual user query into something FTS5 actually
// matches. The default FTS5 semantics on multi-token input is AND, so a
// prose description like "how does block validation work in consensus"
// returns zero hits because no doc contains all seven tokens. We instead:
//
//   - power-user mode: any sigil (* " ( ) :) → pass through verbatim.
//   - single token, length ≥ 2 → append `*` (prefix match: gene → gene*).
//   - multi-token: drop tokens shorter than 3 chars (stop-word heuristic),
//     prefix-tag tokens length ≥ 4, OR them together so any one match
//     surfaces a candidate. The downstream scoring (BM25 + PageRank +
//     usage) re-ranks the candidates so this OR-broadening doesn't
//     degrade quality on terms that are uniquely informative.
//
// Returning q unchanged when no useful tokens survive (`""`, `"a b"`)
// lets FTS5 surface its own no-hits behaviour.
func rewriteFTSQuery(q string) string {
	// Power-user passthrough — explicit FTS5 wildcard (`*`) or phrase quote
	// (`"`) signals a hand-crafted query that should bypass the prose
	// rewriter. The earlier sigil set included `()`/`:` as well, but those
	// punctuation marks routinely show up in natural-language task
	// descriptions (e.g. "Where does (NewBlockChain) get called:") and the
	// false-power-user classification caused FTS5 to choke on the raw
	// parenthesis as a "syntax error near 'does'". Reported 2026-05-11 in
	// go-stablenet VERIFICATION_REPORT §7.3 as B1's adjacent case. We now
	// route those queries through the same per-token sanitiser as plain
	// prose; `trimFTSToken` strips boundary `(` / `)` / `:` so the FTS5
	// expression stays well-formed. Hand-crafted FTS5 queries that rely
	// on grouping or column filters can still opt in by adding `*` or `"`.
	// Power-user passthrough — explicit FTS5 wildcard (`*`) or a
	// query that is *entirely* phrase-quoted ("foo bar baz") signals
	// a hand-crafted FTS5 expression. Prose containing embedded
	// quotes (`Like "Vault.deposit"`) does NOT qualify — those are
	// natural-language quotes around an identifier and must still
	// go through the rewriter. The earlier check
	// `strings.ContainsAny(q, "*\"")` flipped on any quote
	// anywhere in the query, which caused the post-V3+B smoke run
	// to bypass the rewriter on a task description that quoted
	// example qnames and to hand FTS5 the raw `Vault.deposit.`
	// — fts5 then rejected the trailing period with the original
	// "syntax error near '.'" surfaced as δ smartContext silent
	// failure (2026-05-22, second iteration).
	if strings.Contains(q, "*") {
		return q
	}
	if strings.HasPrefix(q, `"`) && strings.HasSuffix(q, `"`) && len(q) >= 2 {
		return q
	}
	// 2026-05-22 (smartContext audit): dotted identifiers in task
	// descriptions ("List functions that call Vault.deposit") used
	// to tokenise as a single field `Vault.deposit`. With length ≥4
	// the rewriter then appended `*` → `Vault.deposit*`, which
	// FTS5 rejects with `syntax error near "."`. The full call
	// path: smartctx.BuildContext → store.Search → SearchFTS →
	// rewriteFTSQuery → fts5 → error. δ baseline ran with no
	// smartContext context for the entire previous smoke run
	// because of this. Splitting on `.` (and `"`, after the gate
	// tightening above) lines up with the FTS5 tokeniser's own
	// behaviour — it indexes dotted identifiers as separate
	// tokens, so the rewriter has nothing to lose by matching that
	// Split on whitespace + punctuation that FTS5 would misinterpret:
	//   .  dotted identifiers (Vault.deposit → Vault, deposit)
	//   "  embedded quotes in prose
	//   -  FTS5 NOT operator (refresh-on-change → refresh, on, change)
	//   /  path separators (consensus/wbft/core → consensus, wbft, core)
	//   :  colon (port:443, file:line patterns)
	//   ,  comma-separated lists
	//   ;  semicolons in prose
	//   () [] {} grouping punctuation
	fields := strings.FieldsFunc(q, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '.', '"', '-', '/', ':', ',', ';',
			'(', ')', '[', ']', '{', '}', '<', '>':
			return true
		}
		return false
	})
	if len(fields) == 0 {
		return q
	}
	if len(fields) == 1 {
		t := trimFTSToken(fields[0])
		if len(t) >= 2 {
			return t + "*"
		}
		return q
	}
	parts := make([]string, 0, len(fields))
	for _, t := range fields {
		t = trimFTSToken(t)
		if len(t) < 4 {
			continue
		}
		if isFTS5Reserved(t) {
			continue
		}
		parts = append(parts, t+"*")
	}
	if len(parts) == 0 {
		return q
	}
	return strings.Join(parts, " OR ")
}

// isFTS5Reserved returns true for tokens that FTS5 interprets as operators
// or column names rather than search terms. Passing these through
// rewriteFTSQuery causes "no such column" or syntax errors.
func isFTS5Reserved(t string) bool {
	switch strings.ToLower(t) {
	case "and", "or", "not", "near":
		return true
	}
	return false
}

// trimFTSToken strips leading/trailing characters that would confuse FTS5's
// prefix syntax. Keeps alnum + `_`; everything else is treated as boundary
// punctuation. Reported 2026-05-11 in go-stablenet VERIFICATION_REPORT
// §3.1 B1: natural-language task_description ending in `.` would produce
// tokens like "validated." → after `+ "*"` → "validated.*", which FTS5
// rejects with `syntax error near "."`. Stripping trailing punctuation
// (the dominant trigger in prose) restores the well-formed `validated*`
// prefix while leaving identifier-internal characters alone — callers
// passing intentional sigils (`*`, `"`, `(`, `)`, `:`) still bypass
// rewriteFTSQuery entirely via the early-return at line 643.
func trimFTSToken(t string) string {
	return strings.TrimFunc(t, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_'
	})
}

// tokenizeAndQuery extracts the lowercased token set that
// SearchFTSOptions.Mode="and" requires to appear in every hit.
// Separator set matches rewriteFTSQuery (whitespace + "." + `"`) so
// the AND filter operates over the same surface FTS5 itself tokenises;
// boundary punctuation is stripped via trimFTSToken so prose tail
// punctuation ("Vault.deposit?") doesn't leak into the required set.
// Empty / single-token queries collapse the filter at the caller — they
// don't need an AND-of-one check.
func tokenizeAndQuery(q string) []string {
	fields := strings.FieldsFunc(q, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '.' || r == '"'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		t := trimFTSToken(f)
		if t == "" {
			continue
		}
		lower := strings.ToLower(t)
		// FTS5 reserved keywords are syntax, not search terms. Caller
		// may pass either the raw user query ("foo bar") or a power-
		// user / rewriteFTSQuery expression ("foo* OR bar*"); both
		// must yield the same AND token set {foo, bar}. Without this
		// filter the OR/AND/NOT tokens leak into containsAll and any
		// hit that happens to include the literal word "or" would
		// pass while real hits get dropped.
		switch lower {
		case "or", "and", "not":
			continue
		}
		out = append(out, lower)
	}
	return out
}

// filterHitsByAllTokens drops hits whose FTS-indexed columns (name +
// qualified_name + signature + doc_comment) miss any token. Mirrors
// pkg/evidence/containsAll's semantics — a token is "present" iff it
// occurs as a case-insensitive substring of the concatenated haystack.
// Substring (not whole-token) so dotted identifiers and snake_case
// fragments survive the FTS5 tokeniser splits without false negatives.
func filterHitsByAllTokens(hits []SearchHit, tokens []string) []SearchHit {
	if len(tokens) == 0 {
		return hits
	}
	out := hits[:0]
	for _, h := range hits {
		hay := strings.ToLower(h.Node.Name + " " + h.Node.QualifiedName + " " + h.Node.Signature + " " + h.Node.DocComment)
		ok := true
		for _, t := range tokens {
			if !strings.Contains(hay, t) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, h)
		}
	}
	return out
}

// SearchSubstr is a non-FTS fallback for queries the FTS5 unicode61
// tokeniser can't tokenise — primarily CJK input where text contains no
// whitespace separators. It runs `LIKE '%q%'` against name + qualified_name
// and is intentionally O(n) on the nodes table; expect 50–100ms on 200K
// rows. Use only when FTS isn't viable; see docs/VIEWER-ROADMAP.md L1.
func (s *sqliteStore) SearchSubstr(q string, limit int) ([]types.Node, error) {
	pat := "%" + q + "%"
	rows, err := s.db.Query(`SELECT n.id, n.type, n.name, n.qualified_name, n.file_path,
		n.start_line, n.end_line, n.start_byte, n.end_byte, n.language,
		COALESCE(n.visibility,''), COALESCE(n.signature,''), COALESCE(n.doc_comment,''),
		COALESCE(n.complexity,0), n.in_degree, n.out_degree, n.pagerank, n.usage_score,
		n.confidence, COALESCE(n.sub_kind,''), COALESCE(n.attrs,'')
		FROM nodes n
		WHERE n.name LIKE ? OR n.qualified_name LIKE ? LIMIT ?`, pat, pat, limit)
	if err != nil {
		return nil, fmt.Errorf("substring search %q: %w", q, err)
	}
	defer func() { _ = rows.Close() }()
	return scanNodes(rows)
}
