// Package distributed_fixture exercises the E3 distributed pass: HTTP
// handler registration via net/http, ServeMux, JSON-RPC handler shape,
// and net/rpc client.Call form. See distributed_test.go for assertions.
package distributed_fixture

import (
	"net/http"
)

// usersHandler is registered via http.HandleFunc — expected:
//
//	NodeEndpoint qname="http:* /users"   (method=* because no Go 1.22 prefix)
//	listens_on(usersHandler -> http:* /users)
func usersHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("users"))
}

// adminHandler is registered via mux.HandleFunc — expected:
//
//	NodeEndpoint qname="http:* /admin"
//	listens_on(adminHandler -> http:* /admin)
func adminHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("admin"))
}

// healthHandler is registered via http.Handle wrapped in http.HandlerFunc
// — V0 detector matches Handle calls too. Expected:
//
//	NodeEndpoint qname="http:* /health"
//	listens_on(healthHandler -> http:* /health)
func healthHandler(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("ok"))
}

// SetupRoutes registers the three named handlers above plus an anonymous
// handler at /ping. The anonymous endpoint should still emit a NodeEndpoint
// (route is a string literal) but no listens_on edge (no named function
// node to anchor on — V0 simplification).
//
// Go 1.22+ method-prefixed pattern ("GET /scoped") is exercised below — the
// detector splits the pattern and emits qname "http:GET /scoped" rather
// than the wildcard form.
func SetupRoutes() {
	http.HandleFunc("/users", usersHandler)
	mux := http.NewServeMux()
	mux.HandleFunc("/admin", adminHandler)
	http.Handle("/health", http.HandlerFunc(healthHandler))
	// Go 1.22+ method-prefixed pattern — Endpoint qname is "http:GET /scoped".
	http.HandleFunc("GET /scoped", usersHandler)
	// Anonymous handler — endpoint emitted, listens_on edge skipped.
	http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pong"))
	})
}

// FakeRouter is a user-defined type with a HandleFunc method. The E3
// false-positive guard should NOT emit listens_on edges for fake.HandleFunc
// because typesInfo confirms the receiver isn't *http.ServeMux.
//
// V0 status: AST-only mode flags everything as INFERRED rather than
// dropping; with typesInfo we emit at INFERRED rather than EXTRACTED for
// non-net/http receivers. That's an intentional MVP trade-off — running
// the test with packages.Load (LoadAndResolve does this) yields INFERRED
// confidence here, distinguishable from EXTRACTED on real http.* calls.
type FakeRouter struct{}

func (f *FakeRouter) HandleFunc(path string, h func()) {}

// UseFake exercises the false-positive surface: a FakeRouter.HandleFunc
// call with a string-literal route. We accept that this currently emits
// an Endpoint with INFERRED confidence — operators can filter on confidence
// in the viewer. A stricter guard (require receiver to be net/http) would
// improve precision at the cost of missing common third-party routers
// (gorilla/mux, chi) — punted to follow-up.
func UseFake() {
	r := &FakeRouter{}
	r.HandleFunc("/fake", func() {})
}
