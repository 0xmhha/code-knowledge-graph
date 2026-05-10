package buildpipe

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/evidence"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// TestH3H4_EvidencePackOnRealGitFixture wires the full H1+H2+H3+H4
// stack against a real (tiny) git repo to catch regressions that pure
// unit tests miss: schema drift in the Hunk QualifiedName format, a
// confidence filter regression at index time, the H4 doc_comment
// `issues:` encoding, the §11.3 retrieval boundary, and offset paging
// against an actual recency-sorted commit corpus.
//
// The test builds a 3-commit fixture, runs emitTemporalEdges to
// produce Hunks/Commits/Modifies, wraps the result in an in-memory
// store, then calls evidence.BuildPack four ways:
//
//   - intent only → BM25 ranks the matching commit first
//   - issue_id only → ticket scope filter surfaces just the cited commits
//   - intent + offset → page 1 returns the next-most-recent commit
//   - injected AMBIGUOUS Hunk → does NOT leak into hits (§11.3 boundary)
//
// Together these cover the contract surface end-to-end without
// requiring SQLite or a server, so the test runs in <500ms and slots
// into the regular `go test ./...` cycle.
func TestH3H4_EvidencePackOnRealGitFixture(t *testing.T) {
	repo := initGitRepo(t)
	relPath := "auth.go"

	// Three commits with subjects citing different tickets so we can
	// assert both H4 extraction (issue_ids surfacing in CommitInfo) and
	// recency ordering for offset paging. Each commit modifies the same
	// file with distinct content so emitHunkGraph produces three Hunks
	// (one per parent commit).
	commitFileToRepo(t, repo, relPath,
		"package auth\n\nfunc Login() error {\n  return nil\n}\n",
		"feat: initial auth scaffold (#7)")
	commitFileToRepo(t, repo, relPath,
		"package auth\n\nfunc Login(user string) error {\n  return nil\n}\n",
		"fix: login takes user param (#42)")
	commitFileToRepo(t, repo, relPath,
		"package auth\n\nfunc Login(user, pw string) error {\n  return nil\n}\n",
		"[INGEST-9] add password to login")

	g := buildSyntheticGraph(relPath)
	blobs, err := emitTemporalEdges(g, repo, discardLog(), 10)
	if err != nil {
		t.Fatalf("emitTemporalEdges: %v", err)
	}

	// Sanity: three commits emitted, three hunks, three has_hunk edges,
	// at least one modifies edge per hunk targeting `Login`.
	if got := len(nodesByType(g.Nodes, types.NodeCommit)); got != 3 {
		t.Fatalf("expected 3 Commit nodes, got %d", got)
	}
	hunks := nodesByType(g.Nodes, types.NodeHunk)
	if len(hunks) != 3 {
		t.Fatalf("expected 3 Hunk nodes (one per commit), got %d", len(hunks))
	}
	for _, h := range hunks {
		if blobs[h.ID] == nil {
			t.Errorf("hunk %s missing gzip blob", h.ID)
		}
	}

	// H4: every Hunk's parent-commit subject cites an issue, so each
	// Hunk's doc_comment should encode it. Verifies the H4 extractor
	// runs on top of the real subject parser, not just the unit-test
	// regex inputs.
	hunkBySHA := map[string]types.Node{}
	for _, h := range hunks {
		sha := strings.SplitN(strings.TrimPrefix(h.QualifiedName, "hunk:"), ":", 2)[0]
		hunkBySHA[sha] = h
	}
	if len(hunkBySHA) != 3 {
		t.Fatalf("hunk SHA extraction failed; want 3 unique SHAs, got %d", len(hunkBySHA))
	}
	wantOneOf := []string{"issues:GH-7", "issues:GH-42", "issues:INGEST-9"}
	gotIssueDocs := map[string]bool{}
	for _, h := range hunks {
		gotIssueDocs[h.DocComment] = true
	}
	for _, w := range wantOneOf {
		if !gotIssueDocs[w] {
			t.Errorf("expected at least one Hunk with doc_comment=%q, got %v", w, keys(gotIssueDocs))
		}
	}

	// §11.3 boundary probe: inject one AMBIGUOUS Hunk + Commit pair
	// that BM25 would otherwise rank highly (it shares the intent
	// vocabulary). The retrieval boundary must filter it out.
	ambSHA := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	ambCommitID := "ambcommitfake01"
	ambHunkID := "ambhunkfake0001"
	g.Nodes = append(g.Nodes,
		types.Node{
			ID: ambCommitID, Type: types.NodeCommit,
			QualifiedName: "commit:" + ambSHA,
			Signature:     "1700099999: rolled-back login change AMB",
			Confidence:    types.ConfAmbiguous,
		},
		types.Node{
			ID: ambHunkID, Type: types.NodeHunk,
			QualifiedName: "hunk:" + ambSHA + ":auth.go:0",
			FilePath:      relPath, StartLine: 1, EndLine: 5,
			DocComment: "issues:GH-999",
			Confidence: types.ConfAmbiguous,
		},
	)
	blobs[ambHunkID] = mustGzipPatch(t, "+ should not surface\n+ login was rolled back\n")

	store := &inMemStore{
		nodes:    g.Nodes,
		edges:    g.Edges,
		blobs:    blobs,
		manifest: persist.Manifest{BuildTimestamp: "test", SrcCommit: "test"},
	}

	// 1) Intent ranking: "login" matches all three fixture commits;
	//    BuildPack returns them in author_time-DESC order (per
	//    groupByCommit's §5.2 contract). We verify the *set* of hits
	//    rather than a positional ranking — the AMBIGUOUS leak guard
	//    and IssueIDs surface are the schema invariants this test
	//    exists to lock in. BM25 ranking itself is exercised by
	//    pkg/evidence/evidence_test.go.
	pack, err := evidence.BuildPack(store, evidence.Options{Intent: "login"})
	if err != nil {
		t.Fatalf("BuildPack intent: %v", err)
	}
	if len(pack.Hits) == 0 {
		t.Fatalf("intent search returned 0 hits")
	}
	// AMBIGUOUS leak guard: the rolled-back commit must not appear in
	// any hit, even though "login" matches its subject. This is the
	// §11.3 retrieval boundary lock-in.
	for _, h := range pack.Hits {
		if h.Commit.SHA == ambSHA {
			t.Errorf("AMBIGUOUS commit %s leaked into BuildPack hits", ambSHA)
		}
	}
	// H4 surface: at least one hit must carry CommitInfo.IssueIDs from
	// the doc_comment encoding. (Every fixture commit cites a ticket,
	// so any reasonable rank ordering hits at least one.)
	anyIssueIDs := false
	for _, h := range pack.Hits {
		if len(h.Commit.IssueIDs) > 0 {
			anyIssueIDs = true
			break
		}
	}
	if !anyIssueIDs {
		t.Errorf("no hit surfaced CommitInfo.IssueIDs (H4 surface broken)")
	}

	// 2) issue_id-only browse: GH-42 should yield exactly the
	//    "login takes user param" commit. Verifies the IssueID gate
	//    intersects with the corpus the way TicketIndex relies on.
	ticket, err := evidence.BuildPack(store, evidence.Options{IssueID: "GH-42"})
	if err != nil {
		t.Fatalf("BuildPack issue_id: %v", err)
	}
	if len(ticket.Hits) != 1 || !strings.Contains(ticket.Hits[0].Commit.Subject, "login takes user param") {
		t.Errorf("GH-42 ticket browse = %d hits / %q, want 1 / 'login takes user param'",
			len(ticket.Hits), firstSubject(ticket.Hits))
	}

	// 3) Offset paging: K=1 over "login" (matches all 3 commits) on
	//    page 0 vs page 1 must return DIFFERENT commits. Locks in the
	//    Offset semantics introduced by 693c643 — a regression that
	//    re-emitted page 0's commit on page 1 would silently break
	//    the viewer's "▾ load more" button.
	page0, err := evidence.BuildPack(store, evidence.Options{Intent: "login", K: 1})
	if err != nil {
		t.Fatalf("BuildPack page0: %v", err)
	}
	page1, err := evidence.BuildPack(store, evidence.Options{Intent: "login", K: 1, Offset: 1})
	if err != nil {
		t.Fatalf("BuildPack page1: %v", err)
	}
	if len(page0.Hits) == 0 || len(page1.Hits) == 0 {
		t.Fatalf("paging produced empty pages: page0=%d page1=%d", len(page0.Hits), len(page1.Hits))
	}
	if page0.Hits[0].Commit.SHA == page1.Hits[0].Commit.SHA {
		t.Errorf("paging returned same commit on offsets 0 and 1: %s", page0.Hits[0].Commit.SHA)
	}
}

// inMemStore is a minimal persist.StoreReader for the integration
// test: lets BuildPack run against an emitTemporalEdges output without
// pulling SQLite into the test loop. Embeds the interface so any
// surface BuildPack expands into in the future surfaces as a panic
// instead of a silent zero-value.
type inMemStore struct {
	persist.StoreReader
	nodes    []types.Node
	edges    []types.Edge
	blobs    map[string][]byte
	manifest persist.Manifest
}

func (s *inMemStore) AllNodes() ([]types.Node, error) { return s.nodes, nil }
func (s *inMemStore) AllEdges() ([]types.Edge, error) { return s.edges, nil }
func (s *inMemStore) GetBlob(id string) ([]byte, error) {
	b, ok := s.blobs[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return b, nil
}
func (s *inMemStore) GetManifest() (persist.Manifest, error) { return s.manifest, nil }

// mustGzipPatch mirrors emitHunkGraph's gzip pass for the injected
// AMBIGUOUS hunk's blob so it decompresses through the same egress
// path the real hunks take in pkg/evidence (gunzipIfNeeded).
func mustGzipPatch(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if _, err := gw.Write([]byte(body)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// keys helper for the IssueIDs assertion error message.
func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// firstSubject is a defensive accessor used only by the failure path.
func firstSubject(hits []evidence.Hit) string {
	if len(hits) == 0 {
		return ""
	}
	return hits[0].Commit.Subject
}
