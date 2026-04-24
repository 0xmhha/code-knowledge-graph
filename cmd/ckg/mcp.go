package main

import (
	"errors"

	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	var graph string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the MCP stdio server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("mcp not yet implemented (Task 29)")
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}
