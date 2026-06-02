// Package evidence implements the H3 EvidencePack assembler — given a
// free-form intent string and an optional seed qname, ranks the
// schema-1.8 Hunk corpus by BM25 over a (commit subject || patch text
// || modifies qnames) virtual document, groups the top-K hunks by
// their parent commit, decorates each with its `modifies` neighbours,
// and returns the Pack JSON the Coding Agent can fold into a few-shot
// prompt frame.
//
// Algorithm (mirrors docs/design/hunk-graph.md §5.2):
//
//  1. Build per-hunk virtual document = subject + decompressed patch
//     + modifies-qnames.
//  2. BM25-score against intent → top 50 hunks.
//  3. If seed_qname set: filter to hunks reaching seed via the
//     modifies edge directly OR via one hop on the G3 call graph
//     (calls/invokes). Take top-K survivors.
//  4. Group by parent commit; attach all hunks the commit contains
//     (the adjacent edge means the Agent reads the full change).
//  5. Decorate each hunk with its `modifies` neighbours' metadata
//     (qname, type, file_path, start/end lines — no body bytes).
//  6. Order commits by author timestamp DESC; stop emitting once
//     cumulative patch text exceeds budget_tokens.
//
// §11.3 retrieval boundary: only EXTRACTED-confidence Hunks and
// Commits enter the corpus. AMBIGUOUS rows (the unreachable-history
// recovery track) are filtered out at scan time so the LLM never sees
// code paths that were rolled back.
package evidence

import (
	"bytes"
	"compress/gzip"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/bm25"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Options controls one BuildPack invocation. The defaults below match
// the design §5.1 schema. Zero / negative values fall back to the
// defaults — callers can pass an empty struct for "default behaviour
// over the default intent" (rare but legal).
//
// IssueID, when set, restricts the candidate hunks to those whose
// parent commit's H4-extracted issue set contains the requested ID.
// Combines additively with Intent / SeedQname:
//   - IssueID alone: returns the ticket's hunks ordered by commit recency.
//   - IssueID + Intent: BM25-rank inside the ticket subset.
//   - IssueID + SeedQname: filter by ticket AND by the seed neighbourhood.
type Options struct {
	Intent       string
	SeedQname    string
	IssueID      string
	K            int
	BudgetTokens int
	// Offset skips the first N commits in the recency-sorted result —
	// the "Load more" page boundary. Used by viewer/agent flows that
	// already consumed page 0 and want to keep walking back through a
	// large ticket without raising BudgetTokens (which would also
	// inflate the per-call payload). Stable across calls because
	// commit recency tie-breaks on SHA in groupByCommit's sort.
	Offset int
	// Mode picks the term-match strategy applied on top of the BM25
	// ranking:
	//   - "" or "or" (default): keep BM25's any-term-match behaviour.
	//     A high-scoring hit only needs to share one query token with
	//     the candidate doc; useful for fuzzy semantic search.
	//   - "and": after BM25 ranking, drop hits whose virtual document
	//     doesn't contain *every* query token. Useful for precise
	//     agent queries like "all hunks mentioning RetryPolicy AND
	//     Backoff" where OR's looser fuzzy match would surface noise.
	//
	// AND is purely a post-filter: the BM25 ranking still computes
	// across the full corpus, so the relative scoring among AND-mode
	// survivors matches what they would have been in OR mode.
	Mode string
}

// Pack is the EvidencePack JSON shape (§1.5). Stable field names so the
// Agent prompt format is portable across CKG versions.
type Pack struct {
	Intent string `json:"intent"`
	Hits   []Hit  `json:"hits"`
}

type Hit struct {
	Commit CommitInfo `json:"commit"`
	Hunks  []HunkRow  `json:"hunks"`
}

type CommitInfo struct {
	SHA        string `json:"sha"`
	Subject    string `json:"subject"`
	AuthorTime int64  `json:"author_time"`
	// IssueIDs is reserved for the H4 issue-id extraction stage; left
	// empty by H3 so the schema is stable now and the Agent doesn't
	// crash on a missing field once H4 lands.
	IssueIDs []string `json:"issue_ids,omitempty"`
	// TopFiles is populated by TicketIndex's pickSampleCommits — the
	// top-3 most-frequently-touched directory paths across the
	// commit's hunks (e.g. ["crypto/secp256k1", "consensus", "core"]).
	// Lets the viewer's TicketIndex panel hint at a ticket's reach
	// before the user pays the cost of fetching the full EvidencePack.
	// Left empty (omitempty) by EvidencePack's BuildPack flow because
	// hunk file_path is already surfaced per-HunkRow there.
	TopFiles []string `json:"top_files,omitempty"`
}

type HunkRow struct {
	ID        string         `json:"id"`
	FilePath  string         `json:"file_path"`
	StartLine int            `json:"start_line"`
	EndLine   int            `json:"end_line"`
	PatchText string         `json:"patch_text"`
	Modifies  []ModifiesInfo `json:"modifies,omitempty"`
}

type ModifiesInfo struct {
	Qname     string `json:"qname"`
	Type      string `json:"type"`
	FilePath  string `json:"file_path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

const (
	defaultK            = 5
	defaultBudgetTokens = 6000
	bm25TopN            = 50 // first-stage shortlist size before seed/budget filtering
)

// BuildPack runs the full H3 assembly. Returns an empty Pack (with
// hits=[]) when no hunks match — never nil — so the Agent's JSON
// parsers don't have to guard against null. Errors propagate from the
// underlying store or from gzip decompression.
//
// This is the uncached entrypoint — every call rebuilds the BM25
// corpus from scratch. Long-lived processes (ckg serve, mcp Run)
// should hold a *Cache instance instead and call Cache.BuildPack so
// the indexing cost amortises across queries. See cache.go.
func BuildPack(store persist.StoreReader, opt Options) (*Pack, error) {
	// Single-shot path: a per-call Cache that gets discarded after
	// return. The Cache code path is the source of truth for the
	// algorithm; this wrapper exists for callers that don't want the
	// caching surface (one-off CLI tools, tests, eval harnesses).
	return NewCache().BuildPack(store, opt)
}

// hunkCorpus is the in-memory index BuildPack walks. Built once per
// invocation; for repeat queries the caller is responsible for caching.
type hunkCorpus struct {
	hunks         []types.Node          // EXTRACTED hunks only
	hunkByID      map[string]types.Node // for fast lookup
	commitBySHA   map[string]types.Node // EXTRACTED commits only
	modifiesOf    map[string][]types.Node
	calleesOf     map[string][]types.Node // qname → its calls/invokes targets (for seed expansion)
	callersOf     map[string][]types.Node // qname → things that call it
	nodeByID      map[string]types.Node
	hunkSHA       map[string]string // hunk ID → parent commit SHA
	hunkModifyIDs map[string]map[string]bool
	// issuesBySHA: H4 §10.4 aggregation. Hunks store their commit's
	// issue IDs in DocComment with the `issues:` prefix; we union
	// per-SHA so the EvidencePack's CommitInfo.IssueIDs reflects the
	// full set seen across that commit's hunks. (In practice all hunks
	// of one commit share the same set, but we union defensively in
	// case a future change varies it per-hunk.)
	issuesBySHA map[string]map[string]bool
}

// indexCorpus builds the hunkCorpus from the raw nodes + edges slices.
// Filters to confidence='EXTRACTED' on Hunk + Commit per §11.3 — other
// node types (Function/Method/etc) keep their own AMBIGUOUS rows so
// the modifies-decoration pass surfaces them as long as the source
// hunk is EXTRACTED.
func indexCorpus(nodes []types.Node, edges []types.Edge) *hunkCorpus {
	c := &hunkCorpus{
		hunkByID:      make(map[string]types.Node),
		commitBySHA:   make(map[string]types.Node),
		modifiesOf:    make(map[string][]types.Node),
		calleesOf:     make(map[string][]types.Node),
		callersOf:     make(map[string][]types.Node),
		nodeByID:      make(map[string]types.Node),
		hunkSHA:       make(map[string]string),
		hunkModifyIDs: make(map[string]map[string]bool),
		issuesBySHA:   make(map[string]map[string]bool),
	}
	for _, n := range nodes {
		c.nodeByID[n.ID] = n
		switch n.Type {
		case types.NodeHunk:
			if n.Confidence == types.ConfExtracted {
				c.hunks = append(c.hunks, n)
				c.hunkByID[n.ID] = n
				sha := parseHunkSHA(n.QualifiedName)
				c.hunkSHA[n.ID] = sha
				// H4: parse `issues:` prefix from doc_comment and
				// union into the per-SHA aggregate.
				if ids := decodeIssuesFromDocComment(n.DocComment); len(ids) > 0 {
					set, hit := c.issuesBySHA[sha]
					if !hit {
						set = make(map[string]bool, len(ids))
						c.issuesBySHA[sha] = set
					}
					for _, id := range ids {
						set[id] = true
					}
				}
			}
		case types.NodeCommit:
			if n.Confidence == types.ConfExtracted {
				sha := strings.TrimPrefix(n.QualifiedName, "commit:")
				c.commitBySHA[sha] = n
			}
		}
	}
	// Index edges. Skip AMBIGUOUS rows even on non-meta types — for
	// the call-graph expansion the LLM-bound retrieval pass should not
	// dispatch through edges Resolve was unsure about.
	for _, e := range edges {
		if e.Confidence == types.ConfAmbiguous {
			continue
		}
		switch e.Type {
		case types.EdgeModifies:
			dst, ok := c.nodeByID[e.Dst]
			if !ok {
				continue
			}
			c.modifiesOf[e.Src] = append(c.modifiesOf[e.Src], dst)
			set, hit := c.hunkModifyIDs[e.Src]
			if !hit {
				set = make(map[string]bool)
				c.hunkModifyIDs[e.Src] = set
			}
			set[dst.ID] = true
		case types.EdgeCalls, types.EdgeInvokes:
			srcN, ok1 := c.nodeByID[e.Src]
			dstN, ok2 := c.nodeByID[e.Dst]
			if !ok1 || !ok2 {
				continue
			}
			c.calleesOf[srcN.QualifiedName] = append(c.calleesOf[srcN.QualifiedName], dstN)
			c.callersOf[dstN.QualifiedName] = append(c.callersOf[dstN.QualifiedName], srcN)
		}
	}
	return c
}

// decodeIssuesFromDocComment extracts the H4 §10.4 issue list from a
// Hunk's doc_comment column (formatted `issues:ID1;ID2`). Inlined
// here rather than imported from internal/temporal because pkg/
// evidence is a public package and shouldn't depend on internal/.
// Returns nil for any input lacking the prefix.
func decodeIssuesFromDocComment(docComment string) []string {
	const prefix = "issues:"
	if !strings.HasPrefix(docComment, prefix) {
		return nil
	}
	body := docComment[len(prefix):]
	if body == "" {
		return nil
	}
	parts := strings.Split(body, ";")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseHunkSHA pulls the commit SHA out of a hunk's QualifiedName,
// formatted "hunk:<full-sha>:<file>:<idx>" by makeHunkNode. Returns
// "" on malformed names so the caller can skip the hunk gracefully.
func parseHunkSHA(qname string) string {
	if !strings.HasPrefix(qname, "hunk:") {
		return ""
	}
	rest := qname[len("hunk:"):]
	idx := strings.IndexByte(rest, ':')
	if idx <= 0 {
		return ""
	}
	return rest[:idx]
}

// hunkDocTokens builds the BM25 virtual document for one hunk:
// subject + (decompressed) patch text + modifies qnames. We tokenise
// each piece with the bm25.Tokenize splitter (camelCase + snake_case
// + qname-aware) so the resulting term frequency captures both
// natural-language tokens (commit subjects) and code identifiers
// (callee qnames / patch identifiers) in one pass.
//
// Errors fetching the blob (or decompressing) silently degrade to
// "no patch tokens" — better a less-rich document than none.
func hunkDocTokens(store persist.StoreReader, h types.Node, c *hunkCorpus) []string {
	var out []string
	if commit, ok := c.commitBySHA[c.hunkSHA[h.ID]]; ok {
		subject := stripCommitTimestamp(commit.Signature)
		out = append(out, bm25.Tokenize(subject)...)
	}
	if blob, err := store.GetBlob(h.ID); err == nil {
		if patch, gErr := gunzipIfNeeded(blob); gErr == nil {
			out = append(out, bm25.Tokenize(string(patch))...)
		}
	}
	for _, m := range c.modifiesOf[h.ID] {
		out = append(out, bm25.Tokenize(m.QualifiedName)...)
	}
	return out
}

// stripCommitTimestamp converts the persisted Signature shape
// "<unix>: <subject>" back to the bare subject text. Mirrors the
// makeCommitNode formatting in internal/buildpipe/temporal.go.
func stripCommitTimestamp(sig string) string {
	idx := strings.IndexByte(sig, ':')
	if idx < 0 {
		return sig
	}
	return strings.TrimSpace(sig[idx+1:])
}

// commitTimestamp parses the unix-seconds prefix of Signature so we
// can sort hits by recency without hitting the Timestamp column on
// the persisted CommitInfo (the persisted node row only carries the
// Signature concatenation).
func commitTimestamp(sig string) int64 {
	idx := strings.IndexByte(sig, ':')
	if idx < 0 {
		return 0
	}
	t, err := strconv.ParseInt(strings.TrimSpace(sig[:idx]), 10, 64)
	if err != nil {
		return 0
	}
	return t
}

// gunzipIfNeeded mirrors handleBlob's egress logic: hunk blobs come
// gzipped from the H1 storage layer; pre-1.8 data may be raw. The
// magic-byte check covers both eras.
func gunzipIfNeeded(b []byte) ([]byte, error) {
	if len(b) >= 3 && b[0] == 0x1f && b[1] == 0x8b && b[2] == 0x08 {
		gr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		defer func() { _ = gr.Close() }()
		return io.ReadAll(gr)
	}
	return b, nil
}

// buildSeedAllowList returns the set of node IDs the seed-qname
// expansion permits as `modifies` targets: the seed itself + its 1-hop
// neighbours on the G3 call graph (calls / invokes, both directions).
//
// Per design §5.4 the expansion is bounded — depth=1, max-degree-cap
// = 50 per node — so super-hub seeds (e.g. `Errorf`) don't pull every
// hunk in the corpus into the candidate set.
func buildSeedAllowList(c *hunkCorpus, seedQname string) map[string]bool {
	allowed := map[string]bool{}
	// Seed itself: every node whose qname matches.
	for _, n := range c.nodeByID {
		if n.QualifiedName == seedQname {
			allowed[n.ID] = true
		}
	}
	if len(allowed) == 0 {
		return allowed
	}
	const maxDegree = 50
	// One hop in G3 (callers + callees). Cap per-node to bound noise.
	addCapped := func(targets []types.Node) {
		for i, t := range targets {
			if i >= maxDegree {
				break
			}
			allowed[t.ID] = true
		}
	}
	addCapped(c.calleesOf[seedQname])
	addCapped(c.callersOf[seedQname])
	return allowed
}

// filterByModifiesReach drops hunks whose `modifies` edges don't reach
// any allowed node. Hunks with no modifies edges are also dropped —
// without seed_qname we'd keep them; with seed_qname the user is
// asking specifically for hunks that touch the seed neighbourhood.
func filterByModifiesReach(scored []bm25.ScoredDoc, c *hunkCorpus, allowed map[string]bool) []bm25.ScoredDoc {
	if len(allowed) == 0 {
		// Seed not found — return empty rather than an unfiltered
		// result. Matches the §5.4 contract: a misspelled seed is a
		// signal the Agent should fix the prompt, not silently fall
		// back to all hunks.
		return nil
	}
	out := make([]bm25.ScoredDoc, 0, len(scored))
	for _, s := range scored {
		mods := c.hunkModifyIDs[s.ID]
		hit := false
		for id := range mods {
			if allowed[id] {
				hit = true
				break
			}
		}
		if hit {
			out = append(out, s)
		}
	}
	return out
}

// groupByCommit takes the BM25-ranked hunks, groups them under their
// parent commit, decorates each hunk with its `modifies` metadata,
// orders commits by recency, and stops emitting once the cumulative
// patch_text size exceeds budget_tokens.
//
// Token estimation uses the standard ~4-chars-per-token heuristic
// matching cmd/ckg/benchmark.go's `charsPerToken` constant. Different
// from the Agent's actual tokeniser but fine for ratio-based budget
// enforcement at this scale.
func groupByCommit(scored []bm25.ScoredDoc, c *hunkCorpus, k, budgetTokens, offset int, store persist.StoreReader) []Hit {
	const charsPerToken = 4
	if len(scored) == 0 {
		return nil
	}
	if offset < 0 {
		offset = 0
	}

	// Group hunks by parent SHA. We materialise patch text now so the
	// budget cap can use real bytes. (Re-fetching the blob in the
	// final emit pass would either round-trip the store twice or
	// require a per-hunk cache of comparable size.)
	type hunkPayload struct {
		row   HunkRow
		score float64
		sha   string
	}
	payloads := make([]hunkPayload, 0, len(scored))
	for _, s := range scored {
		h, ok := c.hunkByID[s.ID]
		if !ok {
			continue
		}
		patch := ""
		if blob, err := store.GetBlob(h.ID); err == nil {
			if dec, gErr := gunzipIfNeeded(blob); gErr == nil {
				patch = string(dec)
			}
		}
		mods := c.modifiesOf[h.ID]
		row := HunkRow{
			ID:        h.ID,
			FilePath:  h.FilePath,
			StartLine: h.StartLine,
			EndLine:   h.EndLine,
			PatchText: patch,
			Modifies:  decorateModifies(mods),
		}
		payloads = append(payloads, hunkPayload{
			row:   row,
			score: s.Score,
			sha:   c.hunkSHA[h.ID],
		})
	}

	// Group by SHA + per-commit order by intra-hunk score desc.
	commits := map[string]*Hit{}
	commitOrder := []string{}
	for _, p := range payloads {
		if p.sha == "" {
			continue
		}
		hit, exists := commits[p.sha]
		if !exists {
			commitNode, ok := c.commitBySHA[p.sha]
			if !ok {
				continue
			}
			hit = &Hit{
				Commit: CommitInfo{
					SHA:        p.sha,
					Subject:    stripCommitTimestamp(commitNode.Signature),
					AuthorTime: commitTimestamp(commitNode.Signature),
					IssueIDs:   sortedIssueIDs(c.issuesBySHA[p.sha]),
				},
				Hunks: []HunkRow{},
			}
			commits[p.sha] = hit
			commitOrder = append(commitOrder, p.sha)
		}
		hit.Hunks = append(hit.Hunks, p.row)
	}

	// Sort commits by author timestamp DESC (recency tie-break per §5.2).
	// SHA tie-break inside the comparator keeps the order stable across
	// calls so Offset paging is deterministic — two commits with
	// identical author_time would otherwise pair-swap between sorts.
	sort.SliceStable(commitOrder, func(i, j int) bool {
		ti := commits[commitOrder[i]].Commit.AuthorTime
		tj := commits[commitOrder[j]].Commit.AuthorTime
		if ti != tj {
			return ti > tj
		}
		return commitOrder[i] < commitOrder[j]
	})

	if offset >= len(commitOrder) {
		return nil
	}
	commitOrder = commitOrder[offset:]

	out := make([]Hit, 0, k)
	usedTokens := 0
	for _, sha := range commitOrder {
		if len(out) >= k {
			break
		}
		hit := commits[sha]
		// Estimate this commit's contribution to the budget.
		commitTokens := 0
		for _, h := range hit.Hunks {
			commitTokens += len(h.PatchText) / charsPerToken
		}
		if usedTokens+commitTokens > budgetTokens && len(out) > 0 {
			// Stop — already have at least one hit, and adding this
			// commit would blow the budget. Keep the first commit
			// even if it alone exceeds the budget, so the Agent
			// always gets some response on a successful query.
			break
		}
		out = append(out, *hit)
		usedTokens += commitTokens
	}
	return out
}

// sortedIssueIDs flattens the per-SHA issue set into a deterministic
// slice. Returns nil for empty/missing sets so JSON serialisation
// elides the IssueIDs field via its `omitempty` tag.
func sortedIssueIDs(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// decorateModifies converts the modifies-target Node slice into the
// JSON-friendly ModifiesInfo shape. Returns nil rather than [] for
// hunks with no modifies edges so JSON serialization elides the field
// entirely (matches the `omitempty` tag on the struct).
func decorateModifies(targets []types.Node) []ModifiesInfo {
	if len(targets) == 0 {
		return nil
	}
	out := make([]ModifiesInfo, 0, len(targets))
	for _, t := range targets {
		out = append(out, ModifiesInfo{
			Qname:     t.QualifiedName,
			Type:      string(t.Type),
			FilePath:  t.FilePath,
			StartLine: t.StartLine,
			EndLine:   t.EndLine,
		})
	}
	return out
}
