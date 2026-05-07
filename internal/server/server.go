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
//
// The store field is the read-only persist.StoreReader interface — server
// has no business writing to the graph. This narrowing also lets the
// future PostgreSQL backend (spec §3 / WORK-PLAN B2) plug in without
// rewiring server.
type Server struct {
	store     persist.StoreReader
	mux       *http.ServeMux
	log       *slog.Logger
	community communityCache // lazy-loaded topic_tree projection (see community.go)
}

// Options tunes how Server mounts the static viewer surface. The zero value
// preserves the original behavior (embedded viewer at `/`).
//
//   - DevViewerDir overrides the embedded FS with a disk path. Set by
//     `CKG_DEV_VIEWER_DIR` so a viewer dev loop (`make viewer` after each
//     edit) doesn't require rebuilding the ckg binary. Ignored when empty.
//   - NoViewer skips the static mount entirely, leaving only `/api/*`
//     reachable. Used by `ckg serve --no-viewer` for operators who front
//     the API with their own reverse proxy + separately hosted viewer
//     (the `ckg export-static` bundle).
type Options struct {
	DevViewerDir string
	NoViewer     bool
}

// New wires routes against store and returns a ready-to-serve Server with
// default options (embedded viewer mounted at `/`). A nil log is replaced
// with a stderr text logger so handlers can always log without a nil check.
func New(store persist.StoreReader, log *slog.Logger) *Server {
	return NewWithOptions(store, log, Options{})
}

// NewWithOptions is the configurable constructor. See Options.
func NewWithOptions(store persist.StoreReader, log *slog.Logger, opts Options) *Server {
	if log == nil {
		log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	s := &Server{store: store, mux: http.NewServeMux(), log: log}
	s.routes(opts)
	return s
}

// routes registers the API + static viewer surfaces. The Go 1.22+ ServeMux
// pattern syntax (`GET /api/...`, `{id}` path params) is used directly —
// no third-party router needed.
func (s *Server) routes(opts Options) {
	s.mux.HandleFunc("GET /api/manifest", s.handleManifest)
	s.mux.HandleFunc("GET /api/hierarchy", s.handleHierarchy)
	s.mux.HandleFunc("GET /api/nodes", s.handleNodes)
	s.mux.HandleFunc("GET /api/nodes/top", s.handleTopNodes)
	s.mux.HandleFunc("POST /api/nodes-by-ids", s.handleNodesByIDs)
	s.mux.HandleFunc("POST /api/edges", s.handleEdges)
	s.mux.HandleFunc("GET /api/blob/{id}", s.handleBlob)
	s.mux.HandleFunc("GET /api/search", s.handleSearch)
	s.mux.HandleFunc("GET /api/impact", s.handleImpact)

	if opts.NoViewer {
		// API-only surface; operators wire their own viewer (typically the
		// `ckg export-static` bundle behind a reverse proxy).
		return
	}

	if opts.DevViewerDir != "" {
		// Disk-backed viewer for dev iteration. We do NOT verify index.html
		// exists at construction time: the loop is "edit viewer source →
		// `make viewer` → reload browser", and the index can briefly be
		// absent mid-build. http.FileServer will simply 404 until it's back.
		s.log.Info("server: viewer served from disk (dev mode)", "dir", opts.DevViewerDir)
		s.mux.Handle("/", http.FileServerFS(os.DirFS(opts.DevViewerDir)))
		return
	}

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
