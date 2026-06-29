package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/internal/server"
)

func newServeCmd() *cobra.Command {
	var graph, dbDsn string
	var port int
	var open bool
	var noViewer bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the embedded 3D viewer over HTTP",
		RunE: func(cmd *cobra.Command, args []string) error {
			log, cleanup, err := newLogger(rootVerbose, rootLogFile)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer cleanup()

			// Require exactly one of --graph or --db.
			if graph == "" && dbDsn == "" {
				return fmt.Errorf("one of --graph or --db must be provided")
			}
			return runServe(serveOpts{
				GraphDir: graph, DBDsn: dbDsn, Port: port,
				OpenBrowser: open, NoViewer: noViewer, Log: log,
			})
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory containing graph.db")
	cmd.Flags().StringVar(&dbDsn, "db", "",
		"PostgreSQL DSN (e.g. postgres://user:pass@host/dbname); if set, read graph from PG (--graph not required). DEPRECATED (ADR-0003): SQLite is the sole maintained backend")
	// Default port 8080 matches the EXECUTION-GUIDE S0 spec
	// ("HTTP 127.0.0.1:8080 loopback bind"). Operators who run multiple
	// CKG instances or already use 8080 can override via --port.
	cmd.Flags().IntVar(&port, "port", 8080, "HTTP port")
	cmd.Flags().BoolVar(&open, "open", false, "open browser on start")
	cmd.Flags().BoolVar(&noViewer, "no-viewer", false,
		"disable embedded viewer; serve /api/* only (for reverse-proxy setups)")
	// --graph is no longer required when --db is provided; enforce manually in RunE.
	return cmd
}

// serveOpts groups the runServe parameters so newServeCmd and the
// quickstart command can share the same path without an unwieldy
// positional signature.
type serveOpts struct {
	GraphDir    string // path to the graph.db dir (mutually exclusive with DBDsn)
	DBDsn       string // PostgreSQL DSN (overrides GraphDir when set)
	Port        int    // bind port on 127.0.0.1
	OpenBrowser bool   // open the default browser on start
	NoViewer    bool   // serve /api/* only; skip the embedded viewer
	Log         *slog.Logger
}

// runServe opens the configured store, builds the HTTP server, installs
// SIGINT/SIGTERM handlers, and blocks until the server exits. Extracted
// from the serve cobra RunE so quickstart can compose it after build.
func runServe(o serveOpts) error {
	var store persist.StoreReader
	var sourceLabel string
	var err error
	if o.DBDsn != "" {
		store, err = persist.OpenPostgresReadOnly(o.DBDsn)
		if err != nil {
			return fmt.Errorf("open postgres: %w", err)
		}
		sourceLabel = "postgres"
	} else {
		db := filepath.Join(o.GraphDir, "graph.db")
		store, err = persist.OpenReadOnly(db)
		if err != nil {
			return fmt.Errorf("open graph: %w", err)
		}
		sourceLabel = db
	}
	defer func() { _ = store.Close() }()

	// CKG_DEV_VIEWER_DIR points to a `make viewer` output dir (typically
	// `internal/server/web_assets/`) so viewer changes are picked up by
	// browser reload without rebuilding ckg.
	opts := server.Options{
		DevViewerDir: os.Getenv("CKG_DEV_VIEWER_DIR"),
		NoViewer:     o.NoViewer,
	}
	srv := server.NewWithOptions(store, o.Log, opts)

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer cancel()

	addr := fmt.Sprintf("127.0.0.1:%d", o.Port)
	_, _ = fmt.Fprintf(os.Stderr, "ckg: serving %s on http://%s\n", sourceLabel, addr)
	if o.NoViewer {
		_, _ = fmt.Fprintln(os.Stderr, "ckg: viewer disabled (--no-viewer); only /api/* is reachable")
	} else if opts.DevViewerDir != "" {
		_, _ = fmt.Fprintf(os.Stderr, "ckg: viewer served from %s (CKG_DEV_VIEWER_DIR)\n", opts.DevViewerDir)
	}

	if o.OpenBrowser && !o.NoViewer {
		go openBrowser("http://" + addr)
	}
	return srv.ListenAndServe(ctx, addr)
}

// openBrowser launches the platform's default URL handler. The child process
// is intentionally detached (no Wait) — the browser may outlive `ckg serve`,
// and blocking on it would defeat the goroutine.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
