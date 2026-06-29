package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/internal/validate"
)

// validateExitCode mirrors auditExitCode — distinguishes "issues found"
// (1) from "internal error" (2) so CI can fail noisily on the right
// signal without false positives from infrastructure errors.
type validateExitCode int

func (e validateExitCode) Error() string { return fmt.Sprintf("validate exit %d", int(e)) }

func newValidateCmd() *cobra.Command {
	var graphDir, dbDsn, format string
	var useLLM, llmDryRun bool
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a built graph for integrity (empty values, dangling edges, schema invariants)",
		Long: `Loads graph.db and runs every registered validator against it. The
deterministic SchemaValidator always runs (empty fields, dangling edges,
edge-type semantic invariants). --llm additionally runs the LLM-based
validator skeleton (V0 no-op, V1+ wires real LLM judgments).

Exit codes:
  0  every validator passed (no error severity issues).
  1  at least one validator reported severity=error.
  2  internal failure (could not open graph, etc.).`,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, cleanup, err := newLogger(rootVerbose, rootLogFile)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "ckg: validate: init logger: %v\n", err)
				return validateExitCode(2)
			}
			defer cleanup()

			store, err := openValidateStore(graphDir, dbDsn)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "ckg: validate: open graph: %v\n", err)
				return validateExitCode(2)
			}
			defer func() { _ = store.Close() }()

			g, err := loadGraphFromStore(store)
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "ckg: validate: load graph: %v\n", err)
				return validateExitCode(2)
			}

			ctx := cmd.Context()
			validators := []validate.Validator{validate.NewSchemaValidator()}
			if useLLM {
				lv := validate.NewLLMValidator()
				// --llm-dry-run defaults to true; operators flip it to
				// false explicitly to opt into the (V1+) wired path.
				lv.DryRun = llmDryRun
				validators = append(validators, lv)
			}

			reports := make([]*validate.Report, 0, len(validators))
			anyErr := false
			for _, v := range validators {
				r, err := v.Validate(ctx, g, store)
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "ckg: validate: %s: %v\n", v.Name(), err)
					return validateExitCode(2)
				}
				if r.HasErrors() {
					anyErr = true
				}
				reports = append(reports, r)
			}

			out := cmd.OutOrStdout()
			switch format {
			case "json":
				if err := writeReportsJSON(out, reports); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "ckg: validate: write json: %v\n", err)
					return validateExitCode(2)
				}
			case "text":
				writeReportsText(out, reports)
			default:
				_, _ = fmt.Fprintf(os.Stderr, "ckg: validate: unknown format %q (want text|json)\n", format)
				return validateExitCode(2)
			}

			if anyErr {
				return validateExitCode(1)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&graphDir, "graph", "", "graph directory containing graph.db (required when --db is empty)")
	cmd.Flags().StringVar(&dbDsn, "db", "", "PostgreSQL DSN; takes precedence over --graph when set. DEPRECATED (ADR-0003): SQLite is the sole maintained backend")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text|json")
	cmd.Flags().BoolVar(&useLLM, "llm", false, "run the LLMValidator (V0 dry-run: emits LLM-ready prompts; V1+ wires real LLM calls)")
	cmd.Flags().BoolVar(&llmDryRun, "llm-dry-run", true, "in --llm mode, emit prompts instead of calling an LLM (default true; --llm-dry-run=false errors until V1 wiring lands)")
	return cmd
}

// openValidateStore returns a read-only store from either the local
// graph.db (default) or a Postgres DSN. Mirrors the build.go selection
// logic so operators can validate either backend.
func openValidateStore(graphDir, dbDsn string) (persist.StoreReader, error) {
	if dbDsn != "" {
		return persist.OpenPostgresReadOnly(dbDsn)
	}
	if graphDir == "" {
		return nil, fmt.Errorf("either --graph or --db must be set")
	}
	return persist.OpenReadOnly(filepath.Join(graphDir, "graph.db"))
}

// loadGraphFromStore reconstructs an in-memory graph by streaming every
// node and edge out of the store. Used only by `ckg validate` — other
// callers query selectively.
func loadGraphFromStore(store persist.StoreReader) (*graph.Graph, error) {
	nodes, err := store.AllNodes()
	if err != nil {
		return nil, fmt.Errorf("AllNodes: %w", err)
	}
	edges, err := store.AllEdges()
	if err != nil {
		return nil, fmt.Errorf("AllEdges: %w", err)
	}
	return &graph.Graph{Nodes: nodes, Edges: edges}, nil
}

// writeReportsJSON dumps every report as a JSON array. Stable shape so
// downstream tooling can consume it without unmarshalling helpers.
func writeReportsJSON(w writer, reports []*validate.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(reports)
}

// writeReportsText prints a human-readable summary: per-validator counts
// followed by every issue grouped by severity.
func writeReportsText(w writer, reports []*validate.Report) {
	for _, r := range reports {
		counts := r.CountBySeverity()
		_, _ = fmt.Fprintf(w, "── %s ──  errors=%d  warnings=%d  info=%d\n",
			r.Validator,
			counts[validate.SeverityError],
			counts[validate.SeverityWarning],
			counts[validate.SeverityInfo])
		for _, iss := range r.Issues {
			tail := ""
			switch {
			case iss.NodeID != "":
				tail = "  [" + iss.NodeID + "]"
			case iss.EdgeKey != "":
				tail = "  [" + iss.EdgeKey + "]"
			}
			if iss.FilePath != "" {
				tail += "  " + iss.FilePath
			}
			_, _ = fmt.Fprintf(w, "  %-7s  %-22s  %s%s\n",
				iss.Severity, iss.Code, iss.Message, tail)
		}
	}
}

// writer is the minimal interface used by writeReports* — keeps the
// dependency on io.Writer implicit and avoids importing io for one type.
type writer interface{ Write(p []byte) (int, error) }
