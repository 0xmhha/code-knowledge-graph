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
	var llmBackend, claudeBinary string
	var nRuns int
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run four-baseline evaluation against a graph",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cleanup, err := newLogger(rootVerbose, rootLogFile)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer cleanup()

			tasks, err := eval.LoadTasks(tasksGlob)
			if err != nil {
				return err
			}
			llm, err := selectLLMBackend(llmBackend, model, claudeBinary)
			if err != nil {
				return err
			}
			bs := make([]eval.Baseline, 0, len(baselines))
			for _, b := range baselines {
				bs = append(bs, eval.Baseline(b))
			}
			results, err := eval.Run(context.Background(), tasks, bs, graph, llm, outDir, nRuns)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "ckg: ran %d tasks × %d baselines × %d runs into %s\n",
				len(tasks), len(bs), maxInt(nRuns, 1), outDir)
			_ = results
			return nil
		},
	}
	cmd.Flags().StringVar(&tasksGlob, "tasks", "", "task YAML glob (required)")
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().StringVar(&outDir, "out", "eval/results", "output directory")
	cmd.Flags().StringVar(&model, "llm", "claude-sonnet-4-6", "LLM model id (api backend)")
	cmd.Flags().StringSliceVar(&baselines, "baselines",
		[]string{"alpha", "beta", "gamma", "delta"}, "baselines to run")
	cmd.Flags().StringVar(&llmBackend, "llm-backend", "cli", "LLM backend: api|cli (default cli)")
	cmd.Flags().StringVar(&claudeBinary, "llm-claude-binary", "",
		"path to claude binary (cli backend; empty = PATH lookup)")
	cmd.Flags().IntVar(&nRuns, "n-runs", 1,
		"number of repeats per (task, baseline) pair for mean ± std averaging (default 1)")
	_ = cmd.MarkFlagRequired("tasks")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// selectLLMBackend wires --llm-backend to the corresponding LLMClient
// constructor. The default ("cli" or empty) routes to CLIClient; "api"
// selects APIClient for use with ANTHROPIC_API_KEY.
func selectLLMBackend(backend, model, claudeBinary string) (eval.LLMClient, error) {
	switch backend {
	case "", "cli":
		return eval.NewCLIClient(eval.CLIClientOptions{Binary: claudeBinary})
	case "api":
		return eval.NewAPIClient(model)
	default:
		return nil, fmt.Errorf("unknown --llm-backend=%q (want api|cli)", backend)
	}
}
