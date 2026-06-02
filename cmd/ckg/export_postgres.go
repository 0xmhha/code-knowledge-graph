package main

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

func newExportPostgresCmd() *cobra.Command {
	var dsn, source string

	cmd := &cobra.Command{
		Use:   "export-postgres",
		Short: "Export graph to PostgreSQL (one-shot push from SQLite)",
		Long: `Reads all nodes, edges and blobs from a CKG graph directory (graph.db)
and pushes them to a PostgreSQL database using the COPY protocol for
high-throughput bulk insertion.

The target schema is created automatically (IF NOT EXISTS). Re-running
against an already-populated database will fail with PK conflicts; truncate
the target tables first if you need a clean re-export.

pg_trgm trigram indexes are applied best-effort — a warning is emitted and
the export continues when the extension is unavailable.`,
		Example: `  ckg export-postgres --dsn "postgres://user:pass@localhost/mydb" --source ./graph-dir`,
		RunE: func(cmd *cobra.Command, args []string) error {
			log, cleanup, err := newLogger(rootVerbose, rootLogFile)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer cleanup()

			dbPath := filepath.Join(source, "graph.db")
			store, err := persist.OpenReadOnly(dbPath)
			if err != nil {
				return fmt.Errorf("open graph at %s: %w", dbPath, err)
			}
			defer func() { _ = store.Close() }()

			log.Info("starting postgres export",
				"source", dbPath,
				"dsn_host", persist.DSNHost(dsn),
			)

			exp := &persist.PostgresExporter{}
			if err := exp.Export(cmd.Context(), dsn, store, log); err != nil {
				return fmt.Errorf("export-postgres: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dsn, "dsn", "",
		`PostgreSQL connection string (e.g. postgres://user:pass@host/db)`)
	cmd.Flags().StringVar(&source, "source", "",
		`path to graph directory containing graph.db`)
	_ = cmd.MarkFlagRequired("dsn")
	_ = cmd.MarkFlagRequired("source")
	return cmd
}
