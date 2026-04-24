package main

import (
	"errors"

	"github.com/spf13/cobra"
)

func newEvalCmd() *cobra.Command {
	var tasks, graph, out, llm string
	var baselines []string
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run four-baseline evaluation against a graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("eval not yet implemented (Task 35)")
		},
	}
	cmd.Flags().StringVar(&tasks, "tasks", "", "glob of task YAMLs (required)")
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().StringVar(&out, "out", "eval/results", "output directory")
	cmd.Flags().StringVar(&llm, "llm", "claude-sonnet-4-6", "LLM model id")
	cmd.Flags().StringSliceVar(&baselines, "baselines",
		[]string{"alpha", "beta", "gamma", "delta"}, "baselines to run")
	_ = cmd.MarkFlagRequired("tasks")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}
