package mcphandlers

import (
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/pkg/evidence"
	"github.com/0xmhha/code-knowledge-graph/pkg/store"
)

// RegisterAll wires every ckg tool to s in one call. Equivalent to
// invoking each Register* function individually plus
// [RegisterEvidenceForIntent] with a fresh [evidence.NewCache].
//
// reader is wrapped by [NewLLMSafeReader] before any handler sees it,
// so the §11.3 H3 retrieval boundary is enforced for the entire tool
// set. Callers that need a different lifecycle (one cache across
// multiple servers, partial tool subset, raw reader without the
// safety wrap) should compose the individual Register* calls
// instead.
//
// Existing callers within this module use this to keep server.go
// down to ~10 lines; sister-repo wiring (cks / ckv) is recommended
// to use this too unless there's a specific reason not to.
func RegisterAll(s *server.MCPServer, reader store.Reader) {
	safe := NewLLMSafeReader(reader)
	RegisterFindSymbol(s, safe)
	RegisterFindCallers(s, safe)
	RegisterFindCallees(s, safe)
	RegisterGetSubgraph(s, safe)
	RegisterSearchText(s, safe)
	RegisterGetContextForTask(s, safe)
	RegisterImpactOfChange(s, safe)
	// One Cache per Run amortises BM25 corpus indexing across every
	// evidence_for_intent call. Manifest-keyed invalidation handles
	// the case where a concurrent `ckg build` rebuilds the graph.
	RegisterEvidenceForIntent(s, safe, evidence.NewCache())
}
