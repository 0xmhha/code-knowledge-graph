package server

import (
	"encoding/json"
	"net/http"
	"strconv"

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

// handleSearch runs an FTS5 query against nodes_fts. Empty q returns an
// empty array (not 400) so the viewer can wire a debounced input without
// special-casing the empty state.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, []types.Node{})
		return
	}
	hits, err := s.store.SearchFTS(q, 20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, hits)
}
