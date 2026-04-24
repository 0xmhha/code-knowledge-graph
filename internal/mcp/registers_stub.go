// Package mcp — placeholder tool registration helpers.
//
// These no-op stubs exist solely so that server.go compiles in T27.
// Task 28 replaces this file with real tool implementations
// (`tools.go` + `get_context.go`); delete this file then.
package mcp

import (
	server "github.com/mark3labs/mcp-go/server"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

func registerFindSymbol(s *server.MCPServer, store *persist.Store)        {}
func registerFindCallers(s *server.MCPServer, store *persist.Store)       {}
func registerFindCallees(s *server.MCPServer, store *persist.Store)       {}
func registerGetSubgraph(s *server.MCPServer, store *persist.Store)       {}
func registerSearchText(s *server.MCPServer, store *persist.Store)        {}
func registerGetContextForTask(s *server.MCPServer, store *persist.Store) {}
