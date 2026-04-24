package main

import (
	"errors"

	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	var graph string
	var port int
	var open bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the embedded 3D viewer over HTTP",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("serve not yet implemented (Task 26)")
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().IntVar(&port, "port", 8787, "HTTP port")
	cmd.Flags().BoolVar(&open, "open", false, "open browser on start")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}
