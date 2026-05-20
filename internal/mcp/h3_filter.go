// Package mcp — h3_filter.go enforces the schema-1.8 §11.3 retrieval
// boundary: no AMBIGUOUS-confidence Hunk or Commit nodes (the
// reflog/fsck-collected unreachable-history track) are ever surfaced
// to LLM consumers. Every MCP tool runs its result through
// filterLLMSafe before returning, so the agent never reasons about
// code paths that were rolled back by force-push.
//
// The HTTP /api/* surface (server/api.go) is intentionally NOT
// filtered — those endpoints power the human-driven viewer, where a
// future "Recovery" panel will surface AMBIGUOUS hunks deliberately
// for an operator who needs to undo an agent's overwrite. The boundary
// lives at the MCP layer because that's the LLM-attached door.
package mcp

import (
	"database/sql"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// llmSafeStoreReader wraps a persist.StoreReader and filters AMBIGUOUS
// Hunk/Commit nodes (and edges that touch them) out of every read
// surface — single point of enforcement for the §11.3 H3 retrieval
// boundary. Used by mcp.Run so all 7 tools (including the smart
// get_context_for_task path that delegates to pkg/smartctx) get the
// boundary applied without each tool having to remember.
//
// Methods that StoreReader exposes but that don't surface Hunk/Commit
// data (LoadHierarchy, manifest helpers, EdgeCountsByType, etc.)
// pass through unmodified — embedded interface composition gives us
// those for free, and we only override the methods that need filtering.
type llmSafeStoreReader struct {
	persist.StoreReader
}

// newLLMSafeStoreReader wraps store. Returns the wrapped reader directly
// (not as an interface) so the caller can pass it where a concrete
// persist.StoreReader is expected.
func newLLMSafeStoreReader(store persist.StoreReader) persist.StoreReader {
	return &llmSafeStoreReader{StoreReader: store}
}

func (s *llmSafeStoreReader) FindSymbol(name, lang string, exact bool) ([]types.Node, error) {
	out, err := s.StoreReader.FindSymbol(name, lang, exact)
	return filterLLMSafe(out), err
}

func (s *llmSafeStoreReader) NodesByIDs(ids []string) ([]types.Node, error) {
	out, err := s.StoreReader.NodesByIDs(ids)
	return filterLLMSafe(out), err
}

func (s *llmSafeStoreReader) QueryNodes(parent string, limit int) ([]types.Node, error) {
	out, err := s.StoreReader.QueryNodes(parent, limit)
	return filterLLMSafe(out), err
}

func (s *llmSafeStoreReader) TopNodes(metric string, limit int, excludeTypes ...string) ([]types.Node, error) {
	out, err := s.StoreReader.TopNodes(metric, limit, excludeTypes...)
	return filterLLMSafe(out), err
}

func (s *llmSafeStoreReader) NeighborhoodByQname(qname string, depth int, reverse bool, edgeTypes ...string) ([]types.Node, []types.Edge, error) {
	nodes, edges, err := s.StoreReader.NeighborhoodByQname(qname, depth, reverse, edgeTypes...)
	if err != nil {
		return nil, nil, err
	}
	nodes = filterLLMSafe(nodes)
	edges = filterLLMSafeEdges(edges, nodeIDSet(nodes))
	return nodes, edges, nil
}

func (s *llmSafeStoreReader) SubgraphByQname(qname string, depth int) ([]types.Node, []types.Edge, error) {
	nodes, edges, err := s.StoreReader.SubgraphByQname(qname, depth)
	if err != nil {
		return nil, nil, err
	}
	nodes = filterLLMSafe(nodes)
	edges = filterLLMSafeEdges(edges, nodeIDSet(nodes))
	return nodes, edges, nil
}

func (s *llmSafeStoreReader) Search(q string, limit int) ([]types.Node, error) {
	out, err := s.StoreReader.Search(q, limit)
	return filterLLMSafe(out), err
}

func (s *llmSafeStoreReader) SearchFTS(q string, limit int) ([]persist.SearchHit, error) {
	hits, err := s.StoreReader.SearchFTS(q, limit)
	if err != nil {
		return nil, err
	}
	// filterLLMSafe drops AMBIGUOUS meta-nodes; we re-pair the survivors
	// with their original scores so downstream rerankers still see a
	// usable ranking after the safety filter.
	kept := make([]persist.SearchHit, 0, len(hits))
	for _, h := range hits {
		safe := filterLLMSafe([]types.Node{h.Node})
		if len(safe) == 1 {
			kept = append(kept, h)
		}
	}
	return kept, nil
}

// GetBlob is the defensive backstop: even if a stale ID for an
// AMBIGUOUS Hunk reaches the LLM somehow (via cache, prior session,
// or out-of-band), we refuse to return its patch text. The cost of
// the per-call NodesByIDs lookup is one indexed query — negligible
// next to the LLM round-trip the result feeds.
func (s *llmSafeStoreReader) GetBlob(id string) ([]byte, error) {
	nodes, lookupErr := s.StoreReader.NodesByIDs([]string{id})
	if lookupErr == nil && len(nodes) == 1 && isAmbiguousMeta(nodes[0]) {
		return nil, sql.ErrNoRows
	}
	return s.StoreReader.GetBlob(id)
}

func (s *llmSafeStoreReader) NodesByFilePath(path string) ([]types.Node, error) {
	out, err := s.StoreReader.NodesByFilePath(path)
	return filterLLMSafe(out), err
}

func (s *llmSafeStoreReader) AllNodes() ([]types.Node, error) {
	out, err := s.StoreReader.AllNodes()
	return filterLLMSafe(out), err
}

func (s *llmSafeStoreReader) AllEdges() ([]types.Edge, error) {
	// Without the corresponding node set there's no clean way to filter
	// edges in isolation here. Pass through; consumers that touch
	// AllEdges directly are eval/audit code paths, not LLM surfaces.
	// (mcp tools never call AllEdges; this stub exists only because
	// embedded interface satisfaction would otherwise re-export the
	// raw method via the wrapper — explicit pass-through is honest
	// about the trade-off.)
	return s.StoreReader.AllEdges()
}

// filterLLMSafe drops AMBIGUOUS Hunk + Commit nodes (the unreachable
// history track per §11.3) from a node slice. Other AMBIGUOUS rows
// (e.g. cross-file calls Resolve couldn't disambiguate) pass through:
// the AMBIGUOUS confidence on those is a precision signal the LLM
// should still see, not a recovery-only data class.
//
// The filter mutates a copy — callers may safely retain the original
// slice for downstream non-LLM use.
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

// filterLLMSafeEdges complements filterLLMSafe: drops edges whose src
// or dst was filtered out of the node set. Callers pass the
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

// nodeIDSet builds the membership map filterLLMSafeEdges expects. The
// helper exists so each tool can collapse the (filter nodes → derive
// id set → filter edges) idiom into a 2-line call sequence.
func nodeIDSet(nodes []types.Node) map[string]bool {
	set := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		set[n.ID] = true
	}
	return set
}

// isAmbiguousMeta reports whether the node is part of the §11.3
// unreachable-history track (and therefore must not leak to LLM
// consumers). Two conditions both hold:
//
//   - Type is Hunk or Commit (the §11.3 candidate kinds — other
//     node types may carry AMBIGUOUS for different reasons that don't
//     warrant LLM-hiding).
//   - Confidence is AMBIGUOUS (HEAD-reachable history stays EXTRACTED
//     — see §11.3 hybrid).
func isAmbiguousMeta(n types.Node) bool {
	if n.Confidence != types.ConfAmbiguous {
		return false
	}
	return n.Type == types.NodeHunk || n.Type == types.NodeCommit
}
