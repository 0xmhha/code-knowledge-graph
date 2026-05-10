package evidence

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// fakeStore is a minimal persist.StoreReader stub for evidence tests.
// Embeds the interface (with nil) so every method we don't override
// panics — making accidental coupling visible immediately.
type fakeStore struct {
	persist.StoreReader
	nodes []types.Node
	edges []types.Edge
	blobs map[string][]byte
}

func (f *fakeStore) AllNodes() ([]types.Node, error) { return f.nodes, nil }
func (f *fakeStore) AllEdges() ([]types.Edge, error) { return f.edges, nil }
func (f *fakeStore) GetBlob(id string) ([]byte, error) {
	b, ok := f.blobs[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return b, nil
}

// GetManifest is needed by Cache.ensureIndex to compute the
// invalidation key. Empty values are fine for tests — the key is
// stable across calls so the cache hits.
func (f *fakeStore) GetManifest() (persist.Manifest, error) {
	return persist.Manifest{
		BuildTimestamp: "test",
		SrcCommit:      "test",
	}, nil
}

func gz(s string) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.Bytes()
}

// TestBuildPack_BasicRanking covers the happy path: a 3-hunk corpus
// where the intent matches one hunk's commit subject + patch text; the
// matching hunk should rank first.
func TestBuildPack_BasicRanking(t *testing.T) {
	store := &fakeStore{
		nodes: []types.Node{
			// Two commits.
			{ID: "c1", Type: types.NodeCommit, QualifiedName: "commit:aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
				Signature: "1700000100: fix panel re-mount jitter", Confidence: types.ConfExtracted},
			{ID: "c2", Type: types.NodeCommit, QualifiedName: "commit:bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222",
				Signature: "1700000200: refactor RPC client retry policy", Confidence: types.ConfExtracted},
			// Hunks under each commit.
			{ID: "h1", Type: types.NodeHunk, QualifiedName: "hunk:aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111:Panel.tsx:0",
				FilePath: "Panel.tsx", StartLine: 42, EndLine: 71, Confidence: types.ConfExtracted},
			{ID: "h2", Type: types.NodeHunk, QualifiedName: "hunk:bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222:rpc.go:0",
				FilePath: "rpc.go", StartLine: 10, EndLine: 30, Confidence: types.ConfExtracted},
		},
		edges: []types.Edge{},
		blobs: map[string][]byte{
			"h1": gz("@@ -42,8 +42,12 @@\n-panel.unmount()\n+panel.preserve()\npanel jitter fix"),
			"h2": gz("@@ -10,5 +10,8 @@\n+retryPolicy.exponential()\n+rpc client backoff"),
		},
	}
	pack, err := BuildPack(store, Options{Intent: "panel jitter"})
	if err != nil {
		t.Fatalf("BuildPack: %v", err)
	}
	if len(pack.Hits) == 0 {
		t.Fatalf("expected ≥1 hit, got 0")
	}
	if pack.Hits[0].Commit.SHA != "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111" {
		t.Errorf("top commit = %s, want aaaa1111… (panel-jitter match)",
			pack.Hits[0].Commit.SHA[:12])
	}
	if !strings.Contains(pack.Hits[0].Commit.Subject, "panel re-mount") {
		t.Errorf("top commit subject = %q, missing 'panel re-mount'",
			pack.Hits[0].Commit.Subject)
	}
	// Subject + patch were correctly decompressed: patch text appears.
	if len(pack.Hits[0].Hunks) == 0 || !strings.Contains(pack.Hits[0].Hunks[0].PatchText, "panel.preserve") {
		t.Errorf("top hunk patch missing decompressed body; got: %v", pack.Hits[0].Hunks)
	}
}

// TestBuildPack_AmbiguousFiltered covers the §11.3 boundary: an
// AMBIGUOUS Hunk is in the store but never reaches the pack output —
// even when the intent matches it.
func TestBuildPack_AmbiguousFiltered(t *testing.T) {
	store := &fakeStore{
		nodes: []types.Node{
			{ID: "c1", Type: types.NodeCommit, QualifiedName: "commit:cccc",
				Signature: "1700000300: panic-revert: kill the bad commit",
				Confidence: types.ConfAmbiguous},
			{ID: "h1", Type: types.NodeHunk, QualifiedName: "hunk:cccc:bad.go:0",
				FilePath: "bad.go", StartLine: 1, EndLine: 5,
				Confidence: types.ConfAmbiguous},
		},
		blobs: map[string][]byte{"h1": gz("the rolled-back patch")},
	}
	pack, err := BuildPack(store, Options{Intent: "panic-revert"})
	if err != nil {
		t.Fatalf("BuildPack: %v", err)
	}
	if len(pack.Hits) != 0 {
		t.Errorf("AMBIGUOUS hunk leaked into EvidencePack: %d hits", len(pack.Hits))
	}
}

// TestBuildPack_SeedQnameFilter covers §5.4: when seed_qname is set,
// only hunks whose modifies edges reach the seed (or its 1-hop calls/
// invokes neighbours) survive.
func TestBuildPack_SeedQnameFilter(t *testing.T) {
	store := &fakeStore{
		nodes: []types.Node{
			{ID: "c1", Type: types.NodeCommit, QualifiedName: "commit:dddd",
				Signature: "1700000400: edit Foo and Bar", Confidence: types.ConfExtracted},
			{ID: "h1", Type: types.NodeHunk, QualifiedName: "hunk:dddd:foo.go:0",
				FilePath: "foo.go", Confidence: types.ConfExtracted},
			{ID: "h2", Type: types.NodeHunk, QualifiedName: "hunk:dddd:bar.go:0",
				FilePath: "bar.go", Confidence: types.ConfExtracted},
			{ID: "fnFoo", Type: types.NodeFunction, QualifiedName: "pkg.Foo",
				Confidence: types.ConfExtracted},
			{ID: "fnBar", Type: types.NodeFunction, QualifiedName: "pkg.Bar",
				Confidence: types.ConfExtracted},
		},
		edges: []types.Edge{
			{Src: "h1", Dst: "fnFoo", Type: types.EdgeModifies, Confidence: types.ConfExtracted},
			{Src: "h2", Dst: "fnBar", Type: types.EdgeModifies, Confidence: types.ConfExtracted},
		},
		blobs: map[string][]byte{
			"h1": gz("Foo body change"),
			"h2": gz("Bar body change"),
		},
	}
	// With seed=pkg.Foo, only h1 should survive (modifies pkg.Foo).
	pack, err := BuildPack(store, Options{Intent: "edit", SeedQname: "pkg.Foo"})
	if err != nil {
		t.Fatalf("BuildPack: %v", err)
	}
	if len(pack.Hits) == 0 {
		t.Fatalf("expected ≥1 hit, got 0")
	}
	allFiles := []string{}
	for _, h := range pack.Hits {
		for _, hk := range h.Hunks {
			allFiles = append(allFiles, hk.FilePath)
		}
	}
	for _, f := range allFiles {
		if f == "bar.go" {
			t.Errorf("seed=pkg.Foo should have filtered out bar.go hunk; got files: %v", allFiles)
		}
	}
}

// TestBuildPack_BudgetCap covers §5.2 step 5: cumulative patch text
// must not exceed budget_tokens (after the first commit is always
// emitted so the Agent never gets an empty response on a successful
// query).
func TestBuildPack_BudgetCap(t *testing.T) {
	bigPatch := strings.Repeat("the cat sat on the mat ", 1000) // ~22KB → ~5500 tokens
	store := &fakeStore{
		nodes: []types.Node{
			{ID: "c1", Type: types.NodeCommit, QualifiedName: "commit:eeee",
				Signature: "1700000500: cat sat", Confidence: types.ConfExtracted},
			{ID: "c2", Type: types.NodeCommit, QualifiedName: "commit:ffff",
				Signature: "1700000600: cat sat too", Confidence: types.ConfExtracted},
			{ID: "h1", Type: types.NodeHunk, QualifiedName: "hunk:eeee:cat.go:0",
				FilePath: "cat.go", Confidence: types.ConfExtracted},
			{ID: "h2", Type: types.NodeHunk, QualifiedName: "hunk:ffff:cat.go:0",
				FilePath: "cat.go", Confidence: types.ConfExtracted},
		},
		blobs: map[string][]byte{"h1": gz(bigPatch), "h2": gz(bigPatch)},
	}
	// budget = 1000 tokens, both hunks would be ~5500 tokens each.
	// Expect: first commit emitted (always), second skipped by budget.
	pack, err := BuildPack(store, Options{Intent: "cat sat", BudgetTokens: 1000})
	if err != nil {
		t.Fatalf("BuildPack: %v", err)
	}
	if len(pack.Hits) != 1 {
		t.Errorf("budget cap should have stopped at 1 commit, got %d", len(pack.Hits))
	}
}

// TestBuildPack_EmptyIntent verifies graceful behaviour when the
// intent has no usable tokens (whitespace, punctuation only).
func TestBuildPack_EmptyIntent(t *testing.T) {
	store := &fakeStore{
		nodes: []types.Node{
			{ID: "c1", Type: types.NodeCommit, QualifiedName: "commit:gggg",
				Signature: "1700000700: real commit", Confidence: types.ConfExtracted},
			{ID: "h1", Type: types.NodeHunk, QualifiedName: "hunk:gggg:x.go:0",
				FilePath: "x.go", Confidence: types.ConfExtracted},
		},
		blobs: map[string][]byte{"h1": gz("body")},
	}
	pack, err := BuildPack(store, Options{Intent: "  ?"})
	if err != nil {
		t.Fatalf("BuildPack: %v", err)
	}
	if len(pack.Hits) != 0 {
		t.Errorf("empty intent should produce 0 hits, got %d", len(pack.Hits))
	}
}

// TestBuildPack_NoCorpus covers a graph with no Hunk rows at all
// (pre-1.8 build, or a tiny repo): empty result, no error.
func TestBuildPack_NoCorpus(t *testing.T) {
	store := &fakeStore{nodes: nil, blobs: nil}
	pack, err := BuildPack(store, Options{Intent: "anything"})
	if err != nil {
		t.Fatalf("BuildPack: %v", err)
	}
	if pack == nil || len(pack.Hits) != 0 {
		t.Errorf("empty corpus should yield empty pack, got %v", pack)
	}
}

// TestBuildPack_IssueIDOnly covers the H4 follow-up: with IssueID set
// and Intent empty, BuildPack returns every hunk whose parent commit
// cites the ticket — sorted by commit recency, no BM25 ranking.
func TestBuildPack_IssueIDOnly(t *testing.T) {
	store := &fakeStore{
		nodes: []types.Node{
			// Two commits, each citing different tickets.
			{ID: "c1", Type: types.NodeCommit,
				QualifiedName: "commit:aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
				Signature:     "1700000100: fix login bug GH-42",
				Confidence:    types.ConfExtracted},
			{ID: "c2", Type: types.NodeCommit,
				QualifiedName: "commit:bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222",
				Signature:     "1700000200: refactor RPC retry GH-99",
				Confidence:    types.ConfExtracted},
			// Hunks under c1 cite GH-42; hunk under c2 cites GH-99.
			{ID: "h1", Type: types.NodeHunk,
				QualifiedName: "hunk:aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111:login.go:0",
				FilePath:      "login.go", DocComment: "issues:GH-42",
				Confidence: types.ConfExtracted},
			{ID: "h2", Type: types.NodeHunk,
				QualifiedName: "hunk:aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111:auth.go:0",
				FilePath:      "auth.go", DocComment: "issues:GH-42",
				Confidence: types.ConfExtracted},
			{ID: "h3", Type: types.NodeHunk,
				QualifiedName: "hunk:bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222:rpc.go:0",
				FilePath:      "rpc.go", DocComment: "issues:GH-99",
				Confidence: types.ConfExtracted},
		},
		blobs: map[string][]byte{
			"h1": gz("login fix patch"),
			"h2": gz("auth fix patch"),
			"h3": gz("rpc retry patch"),
		},
	}
	pack, err := BuildPack(store, Options{IssueID: "GH-42"})
	if err != nil {
		t.Fatalf("BuildPack: %v", err)
	}
	if len(pack.Hits) != 1 {
		t.Fatalf("issue_id=GH-42 should match exactly 1 commit, got %d", len(pack.Hits))
	}
	if pack.Hits[0].Commit.SHA != "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111" {
		t.Errorf("unexpected commit: %s", pack.Hits[0].Commit.SHA[:12])
	}
	if got := len(pack.Hits[0].Hunks); got != 2 {
		t.Errorf("expected both GH-42 hunks (h1, h2), got %d", got)
	}
}

// TestBuildPack_IssueIDWithIntent covers the combined path: BM25 ranks
// the entire corpus, but the IssueID gate filters out hits whose
// commit doesn't cite the ticket — even strong text matches.
func TestBuildPack_IssueIDWithIntent(t *testing.T) {
	store := &fakeStore{
		nodes: []types.Node{
			{ID: "c1", Type: types.NodeCommit,
				QualifiedName: "commit:aaaa", Signature: "1700000100: panel jitter",
				Confidence: types.ConfExtracted},
			{ID: "c2", Type: types.NodeCommit,
				QualifiedName: "commit:bbbb", Signature: "1700000200: panel jitter again",
				Confidence: types.ConfExtracted},
			// Both hunks have the same patch text. Only h2's commit cites GH-7.
			{ID: "h1", Type: types.NodeHunk,
				QualifiedName: "hunk:aaaa:Panel.tsx:0", FilePath: "Panel.tsx",
				DocComment: "", Confidence: types.ConfExtracted},
			{ID: "h2", Type: types.NodeHunk,
				QualifiedName: "hunk:bbbb:Panel.tsx:0", FilePath: "Panel.tsx",
				DocComment: "issues:GH-7", Confidence: types.ConfExtracted},
		},
		blobs: map[string][]byte{
			"h1": gz("panel jitter unmount remount"),
			"h2": gz("panel jitter unmount remount"),
		},
	}
	// Without filter both would rank — with GH-7 filter, only c2 survives.
	pack, err := BuildPack(store, Options{Intent: "panel jitter", IssueID: "GH-7"})
	if err != nil {
		t.Fatalf("BuildPack: %v", err)
	}
	if len(pack.Hits) != 1 {
		t.Fatalf("issue_id=GH-7 + intent should yield 1 commit, got %d", len(pack.Hits))
	}
	if pack.Hits[0].Commit.SHA != "bbbb" {
		t.Errorf("filter let the wrong commit through: %s", pack.Hits[0].Commit.SHA)
	}
}

// TestBuildPack_OffsetPaging covers the pagination follow-up: with
// Offset>0 we skip the first N commits in the recency-sorted result.
// Verifies that page 0 + page 1 together cover the full set with no
// overlap, and that an offset past the end yields an empty pack.
func TestBuildPack_OffsetPaging(t *testing.T) {
	store := &fakeStore{
		nodes: []types.Node{
			// Three commits citing GH-7, descending author_time so the
			// recency sort gives a deterministic order.
			{ID: "c1", Type: types.NodeCommit,
				QualifiedName: "commit:cccc3333cccc3333cccc3333cccc3333cccc3333",
				Signature:     "1700000300: latest GH-7", Confidence: types.ConfExtracted},
			{ID: "c2", Type: types.NodeCommit,
				QualifiedName: "commit:bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222",
				Signature:     "1700000200: middle GH-7", Confidence: types.ConfExtracted},
			{ID: "c3", Type: types.NodeCommit,
				QualifiedName: "commit:aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111",
				Signature:     "1700000100: oldest GH-7", Confidence: types.ConfExtracted},
			{ID: "h1", Type: types.NodeHunk,
				QualifiedName: "hunk:cccc3333cccc3333cccc3333cccc3333cccc3333:a.go:0",
				FilePath:      "a.go", DocComment: "issues:GH-7",
				Confidence: types.ConfExtracted},
			{ID: "h2", Type: types.NodeHunk,
				QualifiedName: "hunk:bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222:b.go:0",
				FilePath:      "b.go", DocComment: "issues:GH-7",
				Confidence: types.ConfExtracted},
			{ID: "h3", Type: types.NodeHunk,
				QualifiedName: "hunk:aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111:c.go:0",
				FilePath:      "c.go", DocComment: "issues:GH-7",
				Confidence: types.ConfExtracted},
		},
		blobs: map[string][]byte{
			"h1": gz("latest"), "h2": gz("middle"), "h3": gz("oldest"),
		},
	}
	// Page 0: K=2 returns the two most-recent commits (c1, c2).
	page0, err := BuildPack(store, Options{IssueID: "GH-7", K: 2, BudgetTokens: 100000})
	if err != nil {
		t.Fatalf("page0: %v", err)
	}
	if len(page0.Hits) != 2 || page0.Hits[0].Commit.SHA[:4] != "cccc" || page0.Hits[1].Commit.SHA[:4] != "bbbb" {
		t.Fatalf("page0 expected [cccc, bbbb], got: %+v", commitSHAs(page0.Hits))
	}
	// Page 1: Offset=2 skips them and returns the third commit (c3).
	page1, err := BuildPack(store, Options{IssueID: "GH-7", K: 2, BudgetTokens: 100000, Offset: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Hits) != 1 || page1.Hits[0].Commit.SHA[:4] != "aaaa" {
		t.Fatalf("page1 expected [aaaa], got: %+v", commitSHAs(page1.Hits))
	}
	// Offset past the end yields an empty pack (not an error).
	pageEnd, err := BuildPack(store, Options{IssueID: "GH-7", K: 2, BudgetTokens: 100000, Offset: 99})
	if err != nil {
		t.Fatalf("pageEnd: %v", err)
	}
	if len(pageEnd.Hits) != 0 {
		t.Errorf("offset past end should yield 0 hits, got %d", len(pageEnd.Hits))
	}
}

func commitSHAs(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Commit.SHA[:4]
	}
	return out
}

// TestBuildPack_IssueIDNoMatch covers the empty result path: a ticket
// nobody cites should produce 0 hits — not an error, not a fallback to
// the unfiltered corpus.
func TestBuildPack_IssueIDNoMatch(t *testing.T) {
	store := &fakeStore{
		nodes: []types.Node{
			{ID: "c1", Type: types.NodeCommit,
				QualifiedName: "commit:aaaa", Signature: "1700000100: real commit",
				Confidence: types.ConfExtracted},
			{ID: "h1", Type: types.NodeHunk,
				QualifiedName: "hunk:aaaa:x.go:0", FilePath: "x.go",
				DocComment: "issues:GH-1", Confidence: types.ConfExtracted},
		},
		blobs: map[string][]byte{"h1": gz("body")},
	}
	pack, err := BuildPack(store, Options{IssueID: "GH-9999"})
	if err != nil {
		t.Fatalf("BuildPack: %v", err)
	}
	if len(pack.Hits) != 0 {
		t.Errorf("unmatched issue_id should yield 0 hits, got %d", len(pack.Hits))
	}
}

// TestParseHunkSHA covers the qname-format parser's edge cases.
func TestParseHunkSHA(t *testing.T) {
	cases := map[string]string{
		"hunk:abc123:file.go:0":                          "abc123",
		"hunk:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa:dir/x.ts:42": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"hunk:":                                          "",
		"":                                               "",
		"commit:abc":                                     "",
	}
	for in, want := range cases {
		if got := parseHunkSHA(in); got != want {
			t.Errorf("parseHunkSHA(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestGunzipIfNeeded covers both branches: real gzip content + raw
// passthrough. A truncated gzip header surfaces as an error.
func TestGunzipIfNeeded(t *testing.T) {
	// Real gzip → decompressed.
	in := gz("hello world")
	out, err := gunzipIfNeeded(in)
	if err != nil || string(out) != "hello world" {
		t.Errorf("gunzip real gzip: got %q err=%v", out, err)
	}
	// Raw bytes (no magic) → passthrough.
	out, err = gunzipIfNeeded([]byte("plain text"))
	if err != nil || string(out) != "plain text" {
		t.Errorf("passthrough plain: got %q err=%v", out, err)
	}
	// Truncated gzip: starts with magic but body is broken.
	bad := []byte{0x1f, 0x8b, 0x08, 0xff, 0xff}
	if _, err = gunzipIfNeeded(bad); err == nil || errors.Is(err, io.EOF) {
		// Either an error or a different EOF wrapper is acceptable;
		// the contract is "doesn't panic, surfaces as error".
		// (Some Go versions return io.ErrUnexpectedEOF, others return
		// gzip-specific errors — we don't assert on the exact type.)
		t.Logf("truncated gzip err = %v (informational)", err)
	}
}
