// Package mcphandlers exposes ckg's MCP tool handlers as a public,
// reusable surface so sister repos (code-knowledge-system / cks,
// code-knowledge-vector / ckv) can wire their own mcp-go servers to
// the same eight-tool set without re-implementing the bodies.
//
// The handlers live here rather than under internal/mcp/ because the
// Go internal/ rule blocks external import of anything inside it.
// Moving them to pkg/ is the T-14 public-surface promotion described
// in eval/stablenet/HANDOFF.md.
//
// # Tool catalogue
//
//	find_symbol          resolve a qname / name to nodes
//	find_callers         reverse call graph over calls + invokes edges
//	find_callees         forward call graph over calls + invokes edges
//	get_subgraph         bidirectional BFS rooted at qname
//	search_text          FTS5 search with AND/OR mode + language pushdown
//	impact_of_change     reverse-dependency closure (broader than callers)
//	get_context_for_task smart 1-shot: BM25 -> 1-hop -> score-fuse -> pack
//	evidence_for_intent  H3 EvidencePack assembler over commit/hunk corpus
//
// # H3 retrieval boundary
//
// Every handler operates on a wrapped Reader (see [NewLLMSafeReader])
// that filters AMBIGUOUS Hunk + Commit nodes — the §11.3
// unreachable-history track — out of every result set before it
// reaches an LLM. [RegisterAll] applies the wrapper for the entire
// tool set; individual Register* callers must wrap their reader
// themselves if they want the same guarantee.
//
// # Minimal cks-side wiring
//
//	import (
//	    server "github.com/mark3labs/mcp-go/server"
//	    "github.com/0xmhha/code-knowledge-graph/pkg/mcphandlers"
//	    "github.com/0xmhha/code-knowledge-graph/pkg/store"
//	)
//
//	r, err := store.OpenReadOnly("/tmp/ckg-graph")
//	if err != nil { /* ... */ }
//	defer r.Close()
//
//	s := server.NewMCPServer("cks-mcp", "0.1.0")
//	mcphandlers.RegisterAll(s, r)
//	_ = server.ServeStdio(s)
//
// # Stability
//
// The Register* function signatures and the tool schema definitions
// follow semantic versioning once the cks/ckv extractions land. Adding
// a new optional argument to a tool is a non-breaking change; removing
// or renaming an existing argument is breaking.
package mcphandlers
