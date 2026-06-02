package server

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/evidence"
	"github.com/0xmhha/code-knowledge-graph/pkg/impact"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// writeJSON sets Content-Type and emits v as a single JSON document. Errors
// from the encoder are intentionally ignored — once headers are written we
// cannot meaningfully recover, and the caller already validated v.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// handleManifest returns the persisted manifest annotated with a live
// staleness check. Staleness is recomputed at request time so the viewer
// can show "graph stale" without rebuilding.
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.GetManifest()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type Out struct {
		persist.Manifest
		GraphStale    bool   `json:"graph_stale"`
		CurrentCommit string `json:"current_commit,omitempty"`
	}
	cur, stale := s.stalenessCache.get(m)
	writeJSON(w, Out{Manifest: m, GraphStale: stale, CurrentCommit: cur})
}

// handleHierarchy returns either the package tree (kind=pkg, default) or the
// topic tree (kind=topic) as a flat list of HierarchyRow.
func (s *Server) handleHierarchy(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "pkg"
	}
	rows, err := s.store.LoadHierarchy(kind)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rows)
}

// handleNodes returns nodes either at the top level (parent="" → packages)
// or scoped under a parent via pkg_tree. Limit is bounded to 50k to keep
// JSON payload bounded.
//
// Responses are decorated with community_id + topic_label from the
// topic_tree projection (see community.go). The viewer reads these for
// COMMUNITY colour mode; if the topic_tree is missing or sparse the fields
// are omitted and the viewer falls back to LANG colouring.
func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get("parent")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 50000 {
		limit = 5000
	}
	nodes, err := s.store.QueryNodes(parent, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.decorateNodes(nodes))
}

// handleTopNodes returns the top-N nodes by a ranking metric (pagerank /
// usage), descending. Used by the viewer boot path so the initial canvas
// shows hub functions/types rather than disconnected Package nodes.
//
// metric defaults to "pagerank" when missing — the viewer's primary boot
// metric. limit is bounded to 100000 (raised from 1000 in 2026-05-22) so
// the viewer can pre-load its entire production node set in a single
// request and avoid the "every selection re-fetches" UX. JSON payload
// size stays manageable at the ceiling — ~40MB worst case for an all-
// types pull on a 100K-node repo, scales linearly down for the typical
// excludeTypes-filtered boot.
func (s *Server) handleTopNodes(w http.ResponseWriter, r *http.Request) {
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		metric = "pagerank"
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 100000 {
		limit = 200
	}
	// excludeTypes is a comma-separated list ("Commit,Hunk"). Empty entries
	// (e.g. trailing comma) are skipped so the SQL builder never sees an
	// empty type string. Cap at 16 entries to keep an unbounded query
	// string from blowing up the SQL placeholder list.
	var excludeTypes []string
	if raw := r.URL.Query().Get("excludeTypes"); raw != "" {
		for _, t := range strings.Split(raw, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				excludeTypes = append(excludeTypes, t)
			}
			if len(excludeTypes) >= 16 {
				break
			}
		}
	}
	nodes, err := s.store.TopNodes(metric, limit, excludeTypes...)
	if err != nil {
		if errors.Is(err, persist.ErrInvalidMetric) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.decorateNodes(nodes))
}

// handleEvidence returns the H3 EvidencePack JSON for an intent string
// + optional seed_qname. Mirrors the MCP evidence_for_intent tool —
// shares the same pkg/evidence.BuildPack assembler, so HTTP and stdio
// consumers observe the same ranking + budget behaviour.
//
// §11.3 retrieval boundary: BuildPack itself filters confidence=
// 'EXTRACTED' on Hunk/Commit rows at indexCorpus time. The HTTP layer
// doesn't get the MCP wrapper, so this in-package filter is the only
// guard — but it's airtight: the BM25 corpus is constructed from
// EXTRACTED hunks alone, and the gunzip-on-read path doesn't widen
// the source set.
func (s *Server) handleEvidence(w http.ResponseWriter, r *http.Request) {
	intent := r.URL.Query().Get("intent")
	issueID := r.URL.Query().Get("issue_id")
	// At least one of intent or issue_id must be set — both empty
	// is a misconfigured caller, not a "show everything" request.
	if intent == "" && issueID == "" {
		http.Error(w, "at least one of intent or issue_id is required", http.StatusBadRequest)
		return
	}
	k, _ := strconv.Atoi(r.URL.Query().Get("k"))
	budget, _ := strconv.Atoi(r.URL.Query().Get("budget_tokens"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	mode := r.URL.Query().Get("mode")
	if mode != "" && mode != "or" && mode != "and" {
		http.Error(w, "mode must be 'or' or 'and'", http.StatusBadRequest)
		return
	}
	pack, err := s.evidenceCache.BuildPack(s.store, evidence.Options{
		Intent:       intent,
		IssueID:      issueID,
		SeedQname:    r.URL.Query().Get("seed_qname"),
		K:            k,
		BudgetTokens: budget,
		Offset:       offset,
		Mode:         mode,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, pack)
}

// handleTickets returns the H4 issue-id index aggregated from the
// cached BM25 corpus: ticket IDs sorted by hunk count, with a few
// most-recent commit subjects per ticket. Powers the viewer's
// TicketIndex panel.
//
// Response shape: `[{issue_id, hunk_count, commit_count,
// sample_commits: [{sha, subject, author_time}]}]`. Empty array
// when the graph has no Hunks with `issues:…` doc_comment (a fresh
// repo or a build with H4 disabled).
//
// limit query param caps the result; default 100 strikes a balance
// between "browse the project's whole ticket footprint" and
// "shouldn't return 5K rows on huge codebases".
func (s *Server) handleTickets(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v, _ := strconv.Atoi(r.URL.Query().Get("limit")); v > 0 && v <= 5000 {
		limit = v
	}
	rows, err := s.evidenceCache.TicketIndex(s.store, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rows == nil {
		rows = []evidence.TicketRow{}
	}
	writeJSON(w, rows)
}

// handleAmbiguousNodes returns Hunk + Commit rows with confidence='AMBIGUOUS'
// — the §11.3 unreachable-history track populated by reflog/fsck. Used
// by the viewer's Recovery panel; deliberately stays unfiltered at the
// HTTP layer (the §11.3 retrieval boundary lives at the MCP layer for
// LLM consumers — humans browsing the viewer are the intended audience).
func (s *Server) handleAmbiguousNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.AmbiguousMetaNodes()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.decorateNodes(nodes))
}

// handleEdgeCounts returns total edge count per type across the whole
// graph. The viewer's EdgeFilters renders one count badge per CKS group
// pill (G1..G6) so users see axis weight without manually toggling and
// counting. Cheap single GROUP BY at the SQL layer; cached client-side.
func (s *Server) handleEdgeCounts(w http.ResponseWriter, r *http.Request) {
	counts, err := s.store.EdgeCountsByType()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if counts == nil {
		counts = map[string]int{}
	}
	writeJSON(w, counts)
}

// handleEdges accepts a JSON body {"ids":[...]} and returns every edge
// touching any of those IDs as src or dst. Used by the viewer to expand a
// neighbourhood without preloading the full edge table.
func (s *Server) handleEdges(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	edges, err := s.store.QueryEdgesForNodes(body.IDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if edges == nil {
		edges = []types.Edge{}
	}
	writeJSON(w, edges)
}

// handleBlob streams the raw source slice persisted for a node. The blob is
// served as text/plain so curl / browser preview just works.
//
// Hunk nodes (schema 1.8) get a gzip-decompression pass on the way out
// because the H1 design stores their unified-diff text gzipped to keep
// the blob table compact (~70% reduction). The compressed bytes are
// useless to a viewer / agent / curl session — decompress before send.
// Other node types (Function / Method / Struct / etc.) have raw source
// in the table, so they pass through untouched.
func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	src, err := s.store.GetBlob(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if isHunkBlob(src) {
		if decompressed, dErr := gunzipBytes(src); dErr == nil {
			src = decompressed
		}
		// On gzip-decompress error fall through with the raw bytes — the
		// caller still sees something rather than a 500, and the only way
		// to land here in practice is a legacy Hunk row written by a
		// pre-H1 build.
	}
	w.Header().Set("content-type", "text/plain; charset=utf-8")
	_, _ = w.Write(src)
}

// isHunkBlob detects gzip-compressed payloads via the standard 1f 8b 08
// magic. Used by handleBlob to decide whether to invoke the gunzip pass.
// We don't peek at the persisted node row to check Type=Hunk because the
// magic-byte check is both cheaper and forward-compatible: any future
// node kind that stores a gzipped blob gets the same treatment for free.
func isHunkBlob(b []byte) bool {
	return len(b) >= 3 && b[0] == 0x1f && b[1] == 0x8b && b[2] == 0x08
}

// gunzipBytes is the inverse of internal/buildpipe/temporal_hunks.go's
// gzipPatch — wraps a bytes.Reader in a gzip.Reader and reads to EOF.
// Errors propagate unchanged so handleBlob can fall back to the raw
// payload on malformed gzip headers.
func gunzipBytes(b []byte) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gr.Close() }()
	return io.ReadAll(gr)
}

// handleNodesByIDs returns full node records for a caller-supplied id list.
// The viewer's depth-driven navigation needs this: BFS-walking the edge
// index produces a set of neighbour ids, and each neighbour's metadata
// (qname, file_path, language, …) must come back in a single round-trip
// so depth-in doesn't fan out into 100 small fetches.
func (s *Server) handleNodesByIDs(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(body.IDs) == 0 {
		writeJSON(w, []apiNode{})
		return
	}
	nodes, err := s.store.NodesByIDs(body.IDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.decorateNodes(nodes))
}

// handleImpact delegates to pkg/impact.Compute so the viewer's "🔍 Impact"
// affordance returns the same shape the MCP impact_of_change tool returns.
// Inputs are GET query params (seed_qname / seed_file / depth / include_blobs)
// — small enough that POST + JSON body would be ceremonial. include_blobs
// defaults to false because the viewer surfaces source via /api/blob/{id} on
// demand; an LLM-targeted client can opt into inline blobs by passing 1.
func (s *Server) handleImpact(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	seedQ := q.Get("seed_qname")
	seedF := q.Get("seed_file")
	if seedQ == "" && seedF == "" {
		http.Error(w, "seed_qname or seed_file required", http.StatusBadRequest)
		return
	}
	// Defensive cap — these come from URL query and stream into DB
	// queries / impact computation; 4096 is comfortably above any real
	// qname or file path.
	const maxSeedLen = 4096
	if len(seedQ) > maxSeedLen || len(seedF) > maxSeedLen {
		http.Error(w, "seed_qname/seed_file exceeds 4096 bytes", http.StatusBadRequest)
		return
	}
	depth, _ := strconv.Atoi(q.Get("depth"))
	if depth <= 0 {
		depth = 2
	}
	includeBlobs := q.Get("include_blobs") == "1" || q.Get("include_blobs") == "true"
	out, err := impact.Compute(s.store, seedQ, seedF, impact.Options{
		Depth:        depth,
		IncludeBlobs: includeBlobs,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, out)
}

// handleSearch delegates the smart routing (FTS / CJK substring,
// auto-prefix) to persist.Store.Search so the HTTP API and the MCP
// tools share one implementation. See docs/VIEWER-ROADMAP.md L1/L2.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, []apiNode{})
		return
	}
	hits, err := s.store.Search(q, 20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.decorateNodes(hits))
}
