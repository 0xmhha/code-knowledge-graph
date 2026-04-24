package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/mcp"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

func newMCPCmd() *cobra.Command {
	var graph string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP stdio server",
		RunE: func(cmd *cobra.Command, args []string) error {
			db := filepath.Join(graph, "graph.db")
			store, err := persist.OpenReadOnly(db)
			if err != nil {
				return fmt.Errorf("open graph: %w", err)
			}
			defer store.Close()
			fmt.Fprintf(os.Stderr, "ckg mcp: stdio server bound to %s\n", db)
			return mcp.Run(context.Background(), store)
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}
