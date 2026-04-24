package main

import (
	"errors"

	"github.com/spf13/cobra"
)

func newBuildCmd() *cobra.Command {
	var src, out string
	var langs []string
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Parse a source tree and produce graph.db",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("build not yet implemented (Task 16)")
		},
	}
	cmd.Flags().StringVar(&src, "src", "", "source root (required)")
	cmd.Flags().StringVar(&out, "out", "", "output directory (required)")
	cmd.Flags().StringSliceVar(&langs, "lang", []string{"auto"}, "languages: auto|go,ts,sol")
	_ = cmd.MarkFlagRequired("src")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}
