package main

import (
	"context"
	"fmt"
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

			var store persist.StoreReader
			var sourceLabel string
			if dbDsn != "" {
				store, err = persist.OpenPostgresReadOnly(dbDsn)
				if err != nil {
					return fmt.Errorf("open postgres: %w", err)
				}
				sourceLabel = "postgres"
			} else {
				db := filepath.Join(graph, "graph.db")
				store, err = persist.OpenReadOnly(db)
				if err != nil {
					return fmt.Errorf("open graph: %w", err)
				}
				sourceLabel = db
			}
			defer store.Close()

			// CKG_DEV_VIEWER_DIR points to a `make viewer` output dir
			// (typically `internal/server/web_assets/`) so viewer changes are
			// picked up by browser reload without rebuilding ckg. Useful when
			// iterating on the viewer with the binary running against a real
			// graph.
			opts := server.Options{
				DevViewerDir: os.Getenv("CKG_DEV_VIEWER_DIR"),
				NoViewer:     noViewer,
			}
			srv := server.NewWithOptions(store, log, opts)

			// signal.NotifyContext gives us graceful Ctrl-C / SIGTERM handling;
			// the server's ListenAndServe path uses ctx.Done to trigger Shutdown.
			ctx, cancel := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer cancel()

			// Bind to loopback only — viewer is local-dev surface, not a public
			// service. Operators who need remote access should front it with a
			// reverse proxy.
			addr := fmt.Sprintf("127.0.0.1:%d", port)
			fmt.Fprintf(os.Stderr, "ckg: serving %s on http://%s\n", sourceLabel, addr)
			if noViewer {
				fmt.Fprintln(os.Stderr, "ckg: viewer disabled (--no-viewer); only /api/* is reachable")
			} else if opts.DevViewerDir != "" {
				fmt.Fprintf(os.Stderr, "ckg: viewer served from %s (CKG_DEV_VIEWER_DIR)\n", opts.DevViewerDir)
			}

			if open && !noViewer {
				go openBrowser("http://" + addr)
			}
			return srv.ListenAndServe(ctx, addr)
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory containing graph.db")
	cmd.Flags().StringVar(&dbDsn, "db", "",
		"PostgreSQL DSN (e.g. postgres://user:pass@host/dbname); if set, read graph from PG (--graph not required)")
	cmd.Flags().IntVar(&port, "port", 8787, "HTTP port")
	cmd.Flags().BoolVar(&open, "open", false, "open browser on start")
	cmd.Flags().BoolVar(&noViewer, "no-viewer", false,
		"disable embedded viewer; serve /api/* only (for reverse-proxy setups)")
	// --graph is no longer required when --db is provided; enforce manually in RunE.
	return cmd
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
