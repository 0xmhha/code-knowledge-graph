// Package evidence — cache.go amortises the per-call corpus indexing
// across BuildPack invocations. The H3 ranking algorithm scans every
// node + edge + hunk blob to build the BM25 corpus; on a 240K-node /
// 9K-hunk graph that's ~4s wall time per query, dominated by ~9K
// GetBlob calls (each gzip-decompressing the patch text on the way
// out).
//
// Cache holds:
//   - the indexed `hunkCorpus` (per-SHA / per-hunk maps),
//   - the bm25.Scorer with the corpus pre-indexed (the expensive step
//     that materialises term-frequency stats across all docs),
// keyed by (manifest.BuildTimestamp + manifest.SrcCommit). Any rebuild
// drifts the key and the next call rebuilds the index lazily.
//
// Concurrency model: sync.RWMutex. The hot path is read-only — every
// concurrent BuildPack takes the read lock for a key check and reuses
// the cached corpus + scorer. Cache miss promotes one goroutine to
// the write lock; the rest queue at the read-side and benefit from
// the rebuilt state. Double-check-locked: the writer re-validates
// the key after acquiring the write lock so two concurrent
// invalidations don't double-build.
package evidence

import (
	"fmt"
	"sync"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/bm25"
)

// Cache is the BuildPack accelerator. Construct one per persistent
// process (mcp server, ckg serve) and reuse across calls — the cache
// invalidates itself when the underlying graph.db rebuilds.
type Cache struct {
	mu     sync.RWMutex
	key    string         // BuildTimestamp + "|" + SrcCommit
	corpus *hunkCorpus    // post-indexCorpus
	scorer bm25.Scorer    // post-Index over the per-hunk virtual docs
	docs   []bm25.Document
}

// NewCache returns a fresh, empty Cache. Safe to share across goroutines
// — every method takes the lock internally.
func NewCache() *Cache { return &Cache{} }

// BuildPack runs the H3 assembly with the cached index when possible.
// Falls back to the uncached path on the first call (or any rebuild
// of the underlying graph.db).
//
// Identical contract to the package-level BuildPack — same Options,
// same Pack JSON, same §11.3 retrieval boundary semantics — only the
// performance changes.
func (c *Cache) BuildPack(store persist.StoreReader, opt Options) (*Pack, error) {
	if opt.K <= 0 {
		opt.K = defaultK
	}
	if opt.BudgetTokens <= 0 {
		opt.BudgetTokens = defaultBudgetTokens
	}
	pack := &Pack{Intent: opt.Intent, Hits: []Hit{}}

	if err := c.ensureIndex(store); err != nil {
		return nil, err
	}

	c.mu.RLock()
	corpus := c.corpus
	scorer := c.scorer
	c.mu.RUnlock()
	if corpus == nil || len(corpus.hunks) == 0 {
		return pack, nil
	}

	// IssueID filter (H4 follow-up): restrict the candidate set to
	// hunks whose parent commit cites the requested ticket. When
	// Intent is empty AND IssueID is set, skip BM25 entirely and
	// rank by commit recency — the user is asking "show me everything
	// for ticket X" rather than "find hunks matching some text".
	queryTokens := bm25.Tokenize(opt.Intent)
	if opt.Intent != "" && len(queryTokens) == 0 {
		// Intent given but tokeniser stripped it (whitespace / punct only).
		return pack, nil
	}

	var scored []bm25.ScoredDoc
	switch {
	case opt.IssueID != "" && len(queryTokens) == 0:
		// Intent-free ticket browse: take every hunk in the ticket's
		// commits as a unit-scored doc; groupByCommit's recency sort
		// orders the resulting Hits.
		scored = hunksForIssueID(corpus, opt.IssueID)
	case len(queryTokens) > 0:
		scored = scorer.TopK(queryTokens, bm25TopN)
		if opt.IssueID != "" {
			scored = filterByIssueID(scored, corpus, opt.IssueID)
		}
	default:
		// Neither intent nor issue_id — empty result is honest.
		return pack, nil
	}

	if opt.SeedQname != "" {
		allowed := buildSeedAllowList(corpus, opt.SeedQname)
		scored = filterByModifiesReach(scored, corpus, allowed)
	}

	pack.Hits = groupByCommit(scored, corpus, opt.K, opt.BudgetTokens, store)
	return pack, nil
}

// hunksForIssueID returns every hunk whose parent commit cites the
// requested ticket, scored 1.0 each. Used when the caller wants the
// full ticket footprint without a text query — groupByCommit's
// recency sort then surfaces the most recent commits first.
func hunksForIssueID(corpus *hunkCorpus, issueID string) []bm25.ScoredDoc {
	out := make([]bm25.ScoredDoc, 0, 64)
	for hunkID, sha := range corpus.hunkSHA {
		if corpus.issuesBySHA[sha][issueID] {
			out = append(out, bm25.ScoredDoc{ID: hunkID, Score: 1.0})
		}
	}
	return out
}

// filterByIssueID drops hunks whose parent commit doesn't cite the
// requested ticket. Used to intersect the BM25 ranking with the
// IssueID gate when both Intent and IssueID are set.
func filterByIssueID(scored []bm25.ScoredDoc, corpus *hunkCorpus, issueID string) []bm25.ScoredDoc {
	out := make([]bm25.ScoredDoc, 0, len(scored))
	for _, s := range scored {
		sha := corpus.hunkSHA[s.ID]
		if corpus.issuesBySHA[sha][issueID] {
			out = append(out, s)
		}
	}
	return out
}

// ensureIndex populates / rebuilds the cached corpus + scorer when
// the manifest key drifts. Fast path takes the read lock for the
// key check; slow path takes the write lock and re-validates.
func (c *Cache) ensureIndex(store persist.StoreReader) error {
	man, err := store.GetManifest()
	if err != nil {
		return fmt.Errorf("evidence cache: manifest: %w", err)
	}
	wantKey := man.BuildTimestamp + "|" + man.SrcCommit

	c.mu.RLock()
	cached := c.key == wantKey && c.corpus != nil
	c.mu.RUnlock()
	if cached {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// Re-check after lock — another goroutine may have rebuilt.
	if c.key == wantKey && c.corpus != nil {
		return nil
	}

	nodes, err := store.AllNodes()
	if err != nil {
		return fmt.Errorf("evidence cache: load nodes: %w", err)
	}
	edges, err := store.AllEdges()
	if err != nil {
		return fmt.Errorf("evidence cache: load edges: %w", err)
	}
	corpus := indexCorpus(nodes, edges)

	docs := make([]bm25.Document, 0, len(corpus.hunks))
	for _, h := range corpus.hunks {
		docs = append(docs, bm25.Document{
			ID:     h.ID,
			Tokens: hunkDocTokens(store, h, corpus),
		})
	}
	scorer := bm25.NewOkapi()
	scorer.Index(docs)

	c.corpus = corpus
	c.docs = docs
	c.scorer = scorer
	c.key = wantKey
	return nil
}

// TicketRow is one entry in the TicketIndex output: an issue/PR ID
// the H4 extractor recognised + how many hunks / commits cite it +
// up to 3 most-recent commit subjects for context. The Coding Agent
// or a human reviewer uses this to navigate "what tickets does this
// codebase track most heavily" without round-tripping to GitHub.
type TicketRow struct {
	IssueID       string       `json:"issue_id"`
	HunkCount     int          `json:"hunk_count"`
	CommitCount   int          `json:"commit_count"`
	SampleCommits []CommitInfo `json:"sample_commits,omitempty"`
}

// TicketIndex returns ticket statistics aggregated from the cached
// hunk corpus. Rebuilds the corpus on the first call (or after a
// graph.db rebuild); subsequent calls are pure in-memory walks over
// already-indexed data.
//
// limit ≤ 0 returns the full sorted list; otherwise the top N rows
// by HunkCount descending. SampleCommits is capped at 3 per ticket
// (most-recent first by author timestamp).
//
// §11.3 boundary: only EXTRACTED Hunks/Commits feed indexCorpus, so
// AMBIGUOUS unreachable-history tickets — even if a force-pushed
// commit's subject mentioned a ticket — never surface here. The
// Recovery panel is the dedicated surface for that data.
func (c *Cache) TicketIndex(store persist.StoreReader, limit int) ([]TicketRow, error) {
	if err := c.ensureIndex(store); err != nil {
		return nil, err
	}
	c.mu.RLock()
	corpus := c.corpus
	c.mu.RUnlock()
	if corpus == nil || len(corpus.issuesBySHA) == 0 {
		return nil, nil
	}

	// Per-ticket stats: hunk count, commit set, and sample commits.
	hunksByID := make(map[string]int, 64)
	commitsByID := make(map[string]map[string]bool, 64)
	for hunkID, sha := range corpus.hunkSHA {
		_ = hunkID
		for id := range corpus.issuesBySHA[sha] {
			hunksByID[id]++
			set, ok := commitsByID[id]
			if !ok {
				set = make(map[string]bool, 4)
				commitsByID[id] = set
			}
			set[sha] = true
		}
	}

	rows := make([]TicketRow, 0, len(hunksByID))
	for id, hunkCount := range hunksByID {
		shas := commitsByID[id]
		row := TicketRow{
			IssueID:       id,
			HunkCount:     hunkCount,
			CommitCount:   len(shas),
			SampleCommits: pickSampleCommits(shas, corpus, 3),
		}
		rows = append(rows, row)
	}
	sortTicketRows(rows)
	if limit > 0 && limit < len(rows) {
		rows = rows[:limit]
	}
	return rows, nil
}

// pickSampleCommits returns up to n most-recent commits from the SHA
// set, decorated with their CommitInfo. Used by TicketIndex to attach
// a few context-rich examples to each ticket without forcing the
// caller to round-trip /api/evidence.
func pickSampleCommits(shas map[string]bool, corpus *hunkCorpus, n int) []CommitInfo {
	if len(shas) == 0 {
		return nil
	}
	infos := make([]CommitInfo, 0, len(shas))
	for sha := range shas {
		commitNode, ok := corpus.commitBySHA[sha]
		if !ok {
			continue
		}
		infos = append(infos, CommitInfo{
			SHA:        sha,
			Subject:    stripCommitTimestamp(commitNode.Signature),
			AuthorTime: commitTimestamp(commitNode.Signature),
		})
	}
	sortCommitsByRecency(infos)
	if n > 0 && len(infos) > n {
		infos = infos[:n]
	}
	return infos
}

func sortTicketRows(rows []TicketRow) {
	sortInPlace(rows, func(i, j int) bool {
		if rows[i].HunkCount != rows[j].HunkCount {
			return rows[i].HunkCount > rows[j].HunkCount
		}
		// Tie-break by commit count, then by ticket id (lexical) for
		// determinism.
		if rows[i].CommitCount != rows[j].CommitCount {
			return rows[i].CommitCount > rows[j].CommitCount
		}
		return rows[i].IssueID < rows[j].IssueID
	})
}

func sortCommitsByRecency(infos []CommitInfo) {
	sortInPlaceCommits(infos, func(i, j int) bool {
		return infos[i].AuthorTime > infos[j].AuthorTime
	})
}

// Tiny generic-free sort wrappers to avoid pulling in "sort" at
// package scope (the file already does in evidence.go but in a
// different translation unit; we duplicate the minimal helper here
// rather than refactor the import surface). Both inputs are small
// (per-ticket SampleCommits is ≤ N, the rows slice is one row per
// observed ticket — typically < 200).
func sortInPlace(rows []TicketRow, less func(i, j int) bool) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && less(j, j-1); j-- {
			rows[j-1], rows[j] = rows[j], rows[j-1]
		}
	}
}

func sortInPlaceCommits(infos []CommitInfo, less func(i, j int) bool) {
	for i := 1; i < len(infos); i++ {
		for j := i; j > 0 && less(j, j-1); j-- {
			infos[j-1], infos[j] = infos[j], infos[j-1]
		}
	}
}

// Invalidate clears the cached state, forcing the next BuildPack to
// rebuild from scratch. Used by tests; production code shouldn't need
// this — manifest-based invalidation handles the common rebuild case.
func (c *Cache) Invalidate() {
	c.mu.Lock()
	c.corpus = nil
	c.scorer = nil
	c.docs = nil
	c.key = ""
	c.mu.Unlock()
}

// CachedKey returns the manifest signature the cache currently holds.
// Empty string means the cache is uninitialised. Useful for testing
// and for telemetry (logging "cache hit, key=…").
func (c *Cache) CachedKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.key
}
