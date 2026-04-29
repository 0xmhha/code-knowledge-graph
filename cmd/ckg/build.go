package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
)

func newBuildCmd() *cobra.Command {
	var src, out string
	var langs []string
	var noCache, rebuildMetrics bool
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Parse a source tree and produce graph.db",
		RunE: func(cmd *cobra.Command, args []string) error {
			log := slog.New(slog.NewTextHandler(os.Stderr, nil))
			m, err := buildpipe.Run(buildpipe.Options{
				SrcRoot:        src,
				OutDir:         out,
				Languages:      langs,
				Logger:         log,
				CKGVersion:     ckgVersion,
				NoCache:        noCache,
				RebuildMetrics: rebuildMetrics,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "ckg: built %d nodes / %d edges into %s\n",
				m.Stats["nodes"], m.Stats["edges"], out)
			return nil
		},
	}
	cmd.Flags().StringVar(&src, "src", "", "source root (required)")
	cmd.Flags().StringVar(&out, "out", "", "output directory (required)")
	cmd.Flags().StringSliceVar(&langs, "lang", []string{"auto"}, "languages: auto|go,ts,sol")
	cmd.Flags().BoolVar(&noCache, "no-cache", false,
		"bypass A3 incremental cache; full rebuild from scratch")
	cmd.Flags().BoolVar(&rebuildMetrics, "rebuild-metrics", false,
		"force PageRank/Leiden recompute even when cache would otherwise reuse them")
	_ = cmd.MarkFlagRequired("src")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}
