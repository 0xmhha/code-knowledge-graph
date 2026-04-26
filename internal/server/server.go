package server

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// Server bundles a read-only Store, a routed mux, and a logger. Construct
// one per `ckg serve` invocation. Server implements http.Handler so callers
// (and tests via httptest) can drive it directly.
type Server struct {
	store *persist.Store
	mux   *http.ServeMux
	log   *slog.Logger
}

// New wires routes against store and returns a ready-to-serve Server.
// A nil log is replaced with a stderr text logger so handlers can always
// log without a nil check.
func New(store *persist.Store, log *slog.Logger) *Server {
	if log == nil {
		log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	s := &Server{store: store, mux: http.NewServeMux(), log: log}
	s.routes()
	return s
}

// routes registers the API + static viewer surfaces. The Go 1.22+ ServeMux
// pattern syntax (`GET /api/...`, `{id}` path params) is used directly —
// no third-party router needed.
func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/manifest", s.handleManifest)
	s.mux.HandleFunc("GET /api/hierarchy", s.handleHierarchy)
	s.mux.HandleFunc("GET /api/nodes", s.handleNodes)
	s.mux.HandleFunc("POST /api/nodes-by-ids", s.handleNodesByIDs)
	s.mux.HandleFunc("POST /api/edges", s.handleEdges)
	s.mux.HandleFunc("GET /api/blob/{id}", s.handleBlob)
	s.mux.HandleFunc("GET /api/search", s.handleSearch)

	// Static viewer — fs.Sub strips the `web_assets/` prefix so the embedded
	// `index.html` is served at `/`.
	sub, err := fs.Sub(viewerFS, "web_assets")
	if err != nil {
		// Compile-time `go:embed all:web_assets` guarantees the directory
		// exists; an error here is unrecoverable startup state.
		panic("server: viewer FS missing web_assets/: " + err.Error())
	}
	s.mux.Handle("/", http.FileServerFS(sub))
}

// ServeHTTP makes Server satisfy http.Handler, primarily so tests can drive
// it via httptest.NewServer.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// ListenAndServe runs the HTTP server until ctx is cancelled. On cancel,
// http.Server.Shutdown is invoked with a fresh background context so the
// graceful path runs even after the parent ctx is already done.
//
// http.ErrServerClosed is suppressed because that is the expected outcome
// of a clean Shutdown — surfacing it would force every caller to special-case it.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		// Use a detached context with a small deadline so in-flight requests
		// get a chance to finish but a stuck handler can't pin the server.
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
