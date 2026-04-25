package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/eval"
)

func newEvalCmd() *cobra.Command {
	var tasksGlob, graph, outDir, model string
	var baselines []string
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run four-baseline evaluation against a graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			tasks, err := eval.LoadTasks(tasksGlob)
			if err != nil {
				return err
			}
			llm, err := eval.NewLLMClient(model)
			if err != nil {
				return err
			}
			bs := make([]eval.Baseline, 0, len(baselines))
			for _, b := range baselines {
				bs = append(bs, eval.Baseline(b))
			}
			results, err := eval.Run(context.Background(), tasks, bs, graph, llm, outDir)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "ckg: ran %d tasks × %d baselines into %s\n",
				len(tasks), len(bs), outDir)
			_ = results
			return nil
		},
	}
	cmd.Flags().StringVar(&tasksGlob, "tasks", "", "task YAML glob (required)")
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().StringVar(&outDir, "out", "eval/results", "output directory")
	cmd.Flags().StringVar(&model, "llm", "claude-sonnet-4-6", "LLM model id")
	cmd.Flags().StringSliceVar(&baselines, "baselines",
		[]string{"alpha", "beta", "gamma", "delta"}, "baselines to run")
	_ = cmd.MarkFlagRequired("tasks")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}
