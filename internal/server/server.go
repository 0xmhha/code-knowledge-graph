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
	"github.com/0xmhha/code-knowledge-graph/pkg/evidence"
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
	// evidenceCache amortises the H3 BuildPack BM25 corpus across
	// /api/evidence calls. Manifest-keyed invalidation handles
	// concurrent `ckg build` rebuilds — see pkg/evidence/cache.go.
	evidenceCache *evidence.Cache
	// stalenessCache debounces computeStaleness's git spawn (the
	// residual cost on /api/manifest after the manifest read itself
	// got cached). 5-second TTL — short enough no human notices a
	// stale indicator linger, long enough to absorb viewer poll
	// bursts without re-spawning git.
	stalenessCache *stalenessCache
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
//
// Manifest caching: the underlying SQLite kv-read for /api/manifest
// measured at p50=235ms on the go-stablenet baseline. Because the
// manifest only changes on a fresh `ckg build`, we read it once at
// construction time and serve every subsequent caller from memory via
// the cachedManifestStore wrapper. Trade-off: external graph rebuild
// while serve is up will produce stale manifest reads (and stale
// evidence-cache invalidation) — `ckg serve` has always been "stop
// and restart on rebuild" in practice, so this matches the existing
// operational contract.
//
// TicketIndex pre-warm: the evidence cache's first BuildPack /
// TicketIndex call materialises the BM25 corpus + per-hunk virtual
// docs (~5s on the same graph). Kicking it off in a background
// goroutine at boot pushes that cost off the user's first request.
// Subsequent calls hit the warm cache (sync.RWMutex double-check
// locked) and land at ~190ms p50.
func NewWithOptions(store persist.StoreReader, log *slog.Logger, opts Options) *Server {
	if log == nil {
		log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	cached := newCachedManifestStore(store, log)
	s := &Server{
		store:          cached,
		mux:            http.NewServeMux(),
		log:            log,
		evidenceCache:  evidence.NewCache(),
		stalenessCache: newStalenessCache(stalenessCacheTTL),
	}
	s.routes(opts)
	go s.prewarmTicketIndex()
	go s.prewarmEdgeCounts()
	return s
}

// prewarmTicketIndex kicks the BM25 corpus build off the user's first
// /api/evidence or /api/tickets call. The result is discarded — the
// cache populates as a side effect of TicketIndex's ensureIndex.
// Errors are logged at debug level only; a graph without H4 issues
// returns no rows but populates the corpus all the same, which is
// the actual goal.
func (s *Server) prewarmTicketIndex() {
	if _, err := s.evidenceCache.TicketIndex(s.store, 0); err != nil {
		s.log.Debug("server: ticket index pre-warm failed (non-fatal)", "err", err)
	}
}

// prewarmEdgeCounts kicks the SELECT-COUNT-GROUP-BY scan off the
// viewer's first /api/edges/counts call. The viewer fetches this on
// every boot to populate per-pill axis-weight badges, so the lazy
// cache miss would always land on the user. Sequencing it here
// pushes the ~290ms scan into the boot window. Result is discarded;
// the side-effect is populating cachedManifestStore.edgeCounts.
func (s *Server) prewarmEdgeCounts() {
	if _, err := s.store.EdgeCountsByType(); err != nil {
		s.log.Debug("server: edge counts pre-warm failed (non-fatal)", "err", err)
	}
}

// routes registers the API + static viewer surfaces. The Go 1.22+ ServeMux
// pattern syntax (`GET /api/...`, `{id}` path params) is used directly —
// no third-party router needed.
func (s *Server) routes(opts Options) {
	s.mux.HandleFunc("GET /api/manifest", s.handleManifest)
	s.mux.HandleFunc("GET /api/hierarchy", s.handleHierarchy)
	s.mux.HandleFunc("GET /api/nodes", s.handleNodes)
	s.mux.HandleFunc("GET /api/nodes/top", s.handleTopNodes)
	s.mux.HandleFunc("GET /api/nodes/ambiguous", s.handleAmbiguousNodes)
	s.mux.HandleFunc("POST /api/nodes-by-ids", s.handleNodesByIDs)
	s.mux.HandleFunc("POST /api/edges", s.handleEdges)
	s.mux.HandleFunc("GET /api/edges/counts", s.handleEdgeCounts)
	s.mux.HandleFunc("GET /api/blob/{id}", s.handleBlob)
	s.mux.HandleFunc("GET /api/search", s.handleSearch)
	s.mux.HandleFunc("GET /api/impact", s.handleImpact)
	s.mux.HandleFunc("GET /api/evidence", s.handleEvidence)
	s.mux.HandleFunc("GET /api/tickets", s.handleTickets)

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
