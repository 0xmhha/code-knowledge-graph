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
	var graph string
	var port int
	var open bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the embedded 3D viewer over HTTP",
		RunE: func(cmd *cobra.Command, args []string) error {
			db := filepath.Join(graph, "graph.db")
			store, err := persist.OpenReadOnly(db)
			if err != nil {
				return fmt.Errorf("open graph: %w", err)
			}
			defer store.Close()

			log := slog.New(slog.NewTextHandler(os.Stderr, nil))
			srv := server.New(store, log)

			// signal.NotifyContext gives us graceful Ctrl-C / SIGTERM handling;
			// the server's ListenAndServe path uses ctx.Done to trigger Shutdown.
			ctx, cancel := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer cancel()

			// Bind to loopback only — viewer is local-dev surface, not a public
			// service. Operators who need remote access should front it with a
			// reverse proxy.
			addr := fmt.Sprintf("127.0.0.1:%d", port)
			fmt.Fprintf(os.Stderr, "ckg: serving %s on http://%s\n", db, addr)

			if open {
				go openBrowser("http://" + addr)
			}
			return srv.ListenAndServe(ctx, addr)
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory (required)")
	cmd.Flags().IntVar(&port, "port", 8787, "HTTP port")
	cmd.Flags().BoolVar(&open, "open", false, "open browser on start")
	_ = cmd.MarkFlagRequired("graph")
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
