package main

import "github.com/spf13/cobra"

const ckgVersion = "0.1.0"

// rootVerbose and rootLogFile are bound to the persistent flags on the root
// command. All subcommands read these via the package-level vars so that
// newLogger can be called uniformly without flag-lookup boilerplate.
var (
	rootVerbose bool
	rootLogFile string
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ckg",
		Short:         "Code Knowledge Graph",
		Version:       ckgVersion,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Persistent flags are inherited by every subcommand.
	root.PersistentFlags().BoolVar(&rootVerbose, "verbose", false,
		"enable debug-level logging (also: CKG_LOG_LEVEL=debug)")
	root.PersistentFlags().StringVar(&rootLogFile, "log-file", "",
		"write structured JSON log to <path> in addition to stderr text")

	root.AddCommand(newBuildCmd(), newServeCmd(), newMCPCmd(),
		newWatchCmd(),
		newExportStaticCmd(), newExportPostgresCmd(), newExportJSONCmd(),
		newReportCmd(), newQuickstartCmd(), newPathCmd(), newBenchmarkCmd(),
		newQueryCmd(), newEvalRetrievalCmd(), newAuditCmd(),
		newValidateCmd(), newEvidenceCmd(), newBenchServerCmd(),
		newBenchMCPCmd(), newBenchMCPStdioCmd(), newBenchIndexCmd())
	return root
}
