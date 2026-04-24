package main

import "github.com/spf13/cobra"

const ckgVersion = "0.1.0"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ckg",
		Short:         "Code Knowledge Graph",
		Version:       ckgVersion,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newBuildCmd(), newServeCmd(), newMCPCmd(),
		newExportStaticCmd(), newEvalCmd())
	return root
}
