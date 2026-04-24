package main

import (
	"errors"

	"github.com/spf13/cobra"
)

func newExportStaticCmd() *cobra.Command {
	var graph, out string
	cmd := &cobra.Command{
		Use:   "export-static",
		Short: "Export graph as chunked JSON for static hosting",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("export-static not yet implemented (Task 31)")
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().StringVar(&out, "out", "", "output directory (required)")
	_ = cmd.MarkFlagRequired("graph")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}
