package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	staticfs "github.com/0xmhha/code-knowledge-graph/internal/server"
)

func newExportStaticCmd() *cobra.Command {
	var graph, out string
	cmd := &cobra.Command{
		Use:   "export-static",
		Short: "Export graph as chunked JSON for static hosting",
		RunE: func(cmd *cobra.Command, args []string) error {
			db := filepath.Join(graph, "graph.db")
			store, err := persist.OpenReadOnly(db)
			if err != nil {
				return fmt.Errorf("open graph: %w", err)
			}
			defer store.Close()

			// Chunk sizes match spec §6.6: 5k nodes, 10k edges per chunk —
			// small enough to stream incrementally, big enough to keep
			// manifest/directory overhead bounded on repos with ~100k nodes.
			if err := store.ExportChunked(out, 5000, 10000); err != nil {
				return fmt.Errorf("export chunked: %w", err)
			}

			// Copy embedded viewer (index.html + assets/) alongside the JSON
			// bundle so `out/` is a single self-contained static site — any
			// HTTP file server rooted at `out` will serve the viewer.
			if err := staticfs.CopyViewerAssetsTo(out); err != nil {
				return fmt.Errorf("copy viewer assets: %w", err)
			}

			fmt.Fprintf(os.Stderr, "ckg: exported static graph to %s\n", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().StringVar(&out, "out", "", "output directory (required)")
	_ = cmd.MarkFlagRequired("graph")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}
