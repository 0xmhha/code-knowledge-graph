package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
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
	cur, stale := computeStaleness(m)
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
	writeJSON(w, nodes)
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
func (s *Server) handleBlob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	src, err := s.store.GetBlob(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("content-type", "text/plain; charset=utf-8")
	_, _ = w.Write(src)
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
		writeJSON(w, []types.Node{})
		return
	}
	nodes, err := s.store.NodesByIDs(body.IDs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, nodes)
}

// handleSearch routes between FTS5 (English / token-aligned) and a
// substring fallback (CJK / non-tokenisable). Trade-offs:
//   - non-ASCII bytes anywhere in q → SearchSubstr (LIKE, O(n))
//   - benign ASCII (no FTS sigils, len ≥ 2) → append `*` so `gene` matches
//     `generator` (autocomplete-style prefix)
//   - explicit FTS syntax (quoted phrase, `*`, `(`, `)`, `:`) is passed
//     through unchanged so power users can still use boolean operators
// See docs/VIEWER-ROADMAP.md L1/L2 for option matrix.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, []types.Node{})
		return
	}
	const limit = 20
	var hits []types.Node
	var err error
	if hasNonASCII(q) {
		hits, err = s.store.SearchSubstr(q, limit)
	} else {
		hits, err = s.store.SearchFTS(rewriteFTSQuery(q), limit)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, hits)
}

// hasNonASCII reports whether q contains any byte ≥ 0x80. Used to route
// CJK-bearing queries away from the FTS5 unicode61 tokeniser, which
// segments Korean text into single un-matchable tokens.
func hasNonASCII(q string) bool {
	for i := 0; i < len(q); i++ {
		if q[i] >= 0x80 {
			return true
		}
	}
	return false
}

// rewriteFTSQuery promotes a benign single-word query to a prefix match
// so users typing `gene` get hits on `generator`/`Genesis`/etc. without
// having to know FTS5 syntax. Anything containing FTS sigils is passed
// through verbatim so power users keep their authored queries.
func rewriteFTSQuery(q string) string {
	if len(q) < 2 || strings.ContainsAny(q, `*"():`) {
		return q
	}
	return q + "*"
}
