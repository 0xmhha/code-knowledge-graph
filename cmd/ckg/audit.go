package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/audit"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// auditExitCode is returned by the cobra RunE so main() can translate it
// to an os.Exit. Cobra's default error → exit-1 path doesn't distinguish
// "diff" (1) from "internal error" (2); this small wrapper keeps the
// distinction explicit for CI / scripted callers.
type auditExitCode int

func (e auditExitCode) Error() string { return fmt.Sprintf("audit exit %d", int(e)) }

func newAuditCmd() *cobra.Command {
	var src, graph, format, language string
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Compare go/packages file set against the DB's recorded files",
		// Suppress cobra's auto-error printing — main() handles exit codes
		// via the auditExitCode sentinel and prints a tailored message.
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cleanup, err := newLogger(rootVerbose, rootLogFile)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "ckg: audit: init logger: %v\n", err)
				return auditExitCode(2)
			}
			defer cleanup()

			if language != "go" {
				_, _ = fmt.Fprintf(os.Stderr, "ckg: audit: only --language=go is supported in V0\n")
				return auditExitCode(2)
			}
			db := filepath.Join(graph, "graph.db")
			store, err := persist.OpenReadOnly(db)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "ckg: audit: open graph: %v\n", err)
				return auditExitCode(2)
			}
			defer func() { _ = store.Close() }()

			report, err := audit.RunGo(src, store)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "ckg: audit: %v\n", err)
				return auditExitCode(2)
			}

			out := cmd.OutOrStdout()
			switch format {
			case "json":
				if err := report.WriteJSON(out); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "ckg: audit: write json: %v\n", err)
					return auditExitCode(2)
				}
			case "text":
				if err := report.WriteText(out); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "ckg: audit: write text: %v\n", err)
					return auditExitCode(2)
				}
			default:
				_, _ = fmt.Fprintf(os.Stderr, "ckg: audit: unknown format %q (want text|json)\n", format)
				return auditExitCode(2)
			}

			if !report.IsParity() {
				return auditExitCode(1)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&src, "src", "", "source root (required)")
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory containing graph.db (required)")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text|json")
	cmd.Flags().StringVar(&language, "language", "go", "language to audit (only 'go' in V0)")
	_ = cmd.MarkFlagRequired("src")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}
