// cmd/ckg/quickstart.go — single-command path from "I have a repo" to
// "viewer is open in my browser". Inspired by graphify's `/graphify .`
// ergonomic; collapses the `ckg build` + `ckg serve` pair (plus an
// optional `ckg report`) into one entry point so first-time users
// don't have to learn the multi-step workflow before seeing results.
//
// Usage:
//
//	ckg quickstart                # build ./, output to ./ckg-out, serve on 8080
//	ckg quickstart --src ./apps   # build a subtree
//	ckg quickstart --no-serve     # build + report only, skip the HTTP server
//	ckg quickstart --no-report    # skip GRAPH_REPORT.md generation
//
// The quickstart command is intentionally a thin orchestrator over the
// existing build/serve/report subcommands — every option those expose
// is reachable directly when the user needs finer control.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

func newQuickstartCmd() *cobra.Command {
	var src, out string
	var port int
	var noServe, noReport bool

	cmd := &cobra.Command{
		Use:   "quickstart",
		Short: "One-command build + serve + report (the ergonomic entrypoint)",
		Long: `Build a graph from --src into --out, generate a GRAPH_REPORT.md
summary, and start the HTTP viewer on --port. The full first-time
flow in one command.

Equivalent to running:

  ckg build  --src <SRC> --out <OUT>
  ckg report --graph <OUT> --out <OUT>/GRAPH_REPORT.md
  ckg serve  --graph <OUT> --port <PORT>

Pass --no-serve to skip the HTTP server (useful for CI/scripts that
just want graph.db + GRAPH_REPORT.md). Pass --no-report to skip the
markdown summary when you only need the SQLite output.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			log, cleanup, err := newLogger(rootVerbose, rootLogFile)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer cleanup()

			absSrc, err := filepath.Abs(src)
			if err != nil {
				return fmt.Errorf("resolve --src: %w", err)
			}
			absOut, err := filepath.Abs(out)
			if err != nil {
				return fmt.Errorf("resolve --out: %w", err)
			}
			if err := os.MkdirAll(absOut, 0o755); err != nil {
				return fmt.Errorf("mkdir out: %w", err)
			}

			_, _ = fmt.Fprintf(os.Stderr, "ckg quickstart: build %s → %s\n", absSrc, absOut)
			if _, err := buildpipe.Run(buildpipe.Options{
				SrcRoot:    absSrc,
				OutDir:     absOut,
				Languages:  []string{"auto"},
				Logger:     log,
				CKGVersion: ckgVersion,
			}); err != nil {
				return fmt.Errorf("build: %w", err)
			}

			if !noReport {
				reportPath := filepath.Join(absOut, "GRAPH_REPORT.md")
				if err := generateReport(absOut, reportPath); err != nil {
					return fmt.Errorf("report: %w", err)
				}
				_, _ = fmt.Fprintf(os.Stderr, "ckg quickstart: wrote %s\n", reportPath)
			}

			if noServe {
				_, _ = fmt.Fprintf(os.Stderr, "ckg quickstart: build complete (skip serve per --no-serve)\n")
				return nil
			}

			_, _ = fmt.Fprintf(os.Stderr, "ckg quickstart: starting viewer at http://localhost:%d\n", port)
			return runServe(serveOpts{
				GraphDir: absOut, Port: port, Log: log,
			})
		},
	}
	cmd.Flags().StringVar(&src, "src", ".", "source repository to graph")
	cmd.Flags().StringVar(&out, "out", "./ckg-out", "output directory (graph.db + GRAPH_REPORT.md + manifest.json)")
	cmd.Flags().IntVar(&port, "port", 8080, "viewer HTTP port")
	cmd.Flags().BoolVar(&noServe, "no-serve", false, "skip the HTTP viewer (build + report only)")
	cmd.Flags().BoolVar(&noReport, "no-report", false, "skip GRAPH_REPORT.md generation")
	return cmd
}

// generateReport mirrors `ckg report` inline. We don't shell out to a
// separate ckg invocation because that would double the binary's
// startup cost and clutter the user's terminal with two banners.
func generateReport(graphDir, outPath string) error {
	store, err := persist.OpenReadOnly(filepath.Join(graphDir, "graph.db"))
	if err != nil {
		return fmt.Errorf("open graph: %w", err)
	}
	defer func() { _ = store.Close() }()
	manifest, err := store.GetManifest()
	if err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	nodes, err := store.AllNodes()
	if err != nil {
		return fmt.Errorf("nodes: %w", err)
	}
	edges, err := store.AllEdges()
	if err != nil {
		return fmt.Errorf("edges: %w", err)
	}
	topics, err := store.LoadHierarchy("topic")
	if err != nil {
		topics = nil // graphs without a Leiden run still get a useful report
	}
	report := buildReport(manifest, nodes, edges, topics, 25)
	return os.WriteFile(outPath, []byte(report), 0o644)
}
