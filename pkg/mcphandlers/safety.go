package mcphandlers

import (
	"database/sql"

	"github.com/0xmhha/code-knowledge-graph/pkg/store"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// llmSafeReader wraps a [store.Reader] and filters AMBIGUOUS Hunk +
// Commit nodes (the schema-1.8 §11.3 unreachable-history track) out
// of every read surface — single enforcement point for the H3
// retrieval boundary. Methods that don't surface Hunk/Commit data
// pass through unmodified via the embedded interface, so future
// Reader additions inherit the boundary automatically until they
// explicitly leak the wrong type.
type llmSafeReader struct {
	store.Reader
}

// NewLLMSafeReader wraps r so AMBIGUOUS Hunk/Commit nodes are
// stripped from every read result. Every Register* handler applies this
// boundary itself via [safeReader], so the §11.3 H3 boundary holds no
// matter how tools are mounted; this constructor stays exported for
// callers that want the wrapped reader for their own use.
func NewLLMSafeReader(r store.Reader) store.Reader {
	return &llmSafeReader{Reader: r}
}

// safeReader returns r wrapped in the §11.3 H3 boundary, idempotently: a
// reader that is already wrapped is returned unchanged, so handlers can wrap
// unconditionally without double-filtering when RegisterAll (or a caller)
// already wrapped. This makes the boundary enforced by construction rather
// than an opt-in each individual Register* caller must remember.
func safeReader(r store.Reader) store.Reader {
	if _, ok := r.(*llmSafeReader); ok {
		return r
	}
	return &llmSafeReader{Reader: r}
}

func (s *llmSafeReader) FindSymbol(name string, exact bool, opts store.FindSymbolOptions) ([]types.Node, error) {
	out, err := s.Reader.FindSymbol(name, exact, opts)
	return filterLLMSafe(out), err
}

func (s *llmSafeReader) NodesByIDs(ids []string) ([]types.Node, error) {
	out, err := s.Reader.NodesByIDs(ids)
	return filterLLMSafe(out), err
}

func (s *llmSafeReader) QueryNodes(parent string, limit int) ([]types.Node, error) {
	out, err := s.Reader.QueryNodes(parent, limit)
	return filterLLMSafe(out), err
}

func (s *llmSafeReader) TopNodes(metric string, limit int, excludeTypes ...string) ([]types.Node, error) {
	out, err := s.Reader.TopNodes(metric, limit, excludeTypes...)
	return filterLLMSafe(out), err
}

func (s *llmSafeReader) NeighborhoodByQname(qname string, depth int, reverse bool, edgeTypes ...string) ([]types.Node, []types.Edge, error) {
	nodes, edges, err := s.Reader.NeighborhoodByQname(qname, depth, reverse, edgeTypes...)
	if err != nil {
		return nil, nil, err
	}
	nodes = filterLLMSafe(nodes)
	edges = filterLLMSafeEdges(edges, nodeIDSet(nodes))
	return nodes, edges, nil
}

func (s *llmSafeReader) SubgraphByQname(qname string, depth int) ([]types.Node, []types.Edge, error) {
	nodes, edges, err := s.Reader.SubgraphByQname(qname, depth)
	if err != nil {
		return nil, nil, err
	}
	nodes = filterLLMSafe(nodes)
	edges = filterLLMSafeEdges(edges, nodeIDSet(nodes))
	return nodes, edges, nil
}

func (s *llmSafeReader) Search(q string, limit int) ([]types.Node, error) {
	out, err := s.Reader.Search(q, limit)
	return filterLLMSafe(out), err
}

func (s *llmSafeReader) SearchWithOpts(q string, limit int, opts store.SearchFTSOptions) ([]types.Node, error) {
	out, err := s.Reader.SearchWithOpts(q, limit, opts)
	return filterLLMSafe(out), err
}

func (s *llmSafeReader) SearchFTS(q string, limit int, opts store.SearchFTSOptions) ([]store.SearchHit, error) {
	hits, err := s.Reader.SearchFTS(q, limit, opts)
	if err != nil {
		return nil, err
	}
	// filterLLMSafe drops AMBIGUOUS meta-nodes; re-pair each survivor
	// with its original score so downstream rerankers still see a
	// useful ranking after the safety filter.
	kept := make([]store.SearchHit, 0, len(hits))
	for _, h := range hits {
		safe := filterLLMSafe([]types.Node{h.Node})
		if len(safe) == 1 {
			kept = append(kept, h)
		}
	}
	return kept, nil
}

// GetBlob is the defensive backstop: even if a stale ID for an
// AMBIGUOUS Hunk reaches the agent somehow (cache, prior session,
// out-of-band), refuse to return its patch text. One indexed
// NodesByIDs lookup is negligible next to the LLM round-trip the
// result feeds.
func (s *llmSafeReader) GetBlob(id string) ([]byte, error) {
	nodes, lookupErr := s.Reader.NodesByIDs([]string{id})
	if lookupErr == nil && len(nodes) == 1 && isAmbiguousMeta(nodes[0]) {
		return nil, sql.ErrNoRows
	}
	return s.Reader.GetBlob(id)
}

func (s *llmSafeReader) NodesByFilePath(path string) ([]types.Node, error) {
	out, err := s.Reader.NodesByFilePath(path)
	return filterLLMSafe(out), err
}

func (s *llmSafeReader) AllNodes() ([]types.Node, error) {
	out, err := s.Reader.AllNodes()
	return filterLLMSafe(out), err
}

func (s *llmSafeReader) AllEdges() ([]types.Edge, error) {
	// Without the corresponding node set there's no clean way to filter
	// edges in isolation here. Pass through; consumers that touch
	// AllEdges directly are eval/audit/evidence paths, not direct LLM
	// surfaces. Evidence still filters confidence='EXTRACTED' at the
	// indexer for defence in depth.
	return s.Reader.AllEdges()
}

// filterLLMSafe drops AMBIGUOUS Hunk + Commit nodes (the
// unreachable-history track per §11.3) from a node slice. Other
// AMBIGUOUS rows (e.g. cross-file calls Resolve couldn't disambiguate)
// pass through: that AMBIGUOUS is a precision signal the LLM should
// still see, not a recovery-only data class.
//
// Returns a fresh slice — callers may safely retain the original for
// downstream non-LLM use.
func filterLLMSafe(nodes []types.Node) []types.Node {
	out := make([]types.Node, 0, len(nodes))
	for _, n := range nodes {
		if isAmbiguousMeta(n) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// filterLLMSafeEdges complements filterLLMSafe: drops edges whose
// endpoints were filtered out of the node set. Callers pass the
// post-filter node set so the predicate is a simple membership test.
func filterLLMSafeEdges(edges []types.Edge, allowedNodeIDs map[string]bool) []types.Edge {
	out := make([]types.Edge, 0, len(edges))
	for _, e := range edges {
		if !allowedNodeIDs[e.Src] || !allowedNodeIDs[e.Dst] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// nodeIDSet builds the membership map filterLLMSafeEdges expects.
// Extracted so each handler collapses (filter nodes -> derive id set
// -> filter edges) into a two-line call sequence.
func nodeIDSet(nodes []types.Node) map[string]bool {
	set := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		set[n.ID] = true
	}
	return set
}

// isAmbiguousMeta reports whether the node is part of the §11.3
// unreachable-history track:
//
//   - Type is Hunk or Commit (the §11.3 candidate kinds — other node
//     types may carry AMBIGUOUS for unrelated reasons that don't
//     warrant LLM-hiding)
//   - Confidence is AMBIGUOUS (HEAD-reachable history stays
//     EXTRACTED — see hunk-graph.md §11.3)
func isAmbiguousMeta(n types.Node) bool {
	if n.Confidence != types.ConfAmbiguous {
		return false
	}
	return n.Type == types.NodeHunk || n.Type == types.NodeCommit
}
