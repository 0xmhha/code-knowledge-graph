// Package distributed_fixture (clients) — W2 of schema 1.9 (CKS G5 cross-
// language interop): Go HTTP client call sites. The same testdata directory
// hosts both server-side handlers (http_handlers.go, method_handlers.go)
// AND clients here, so the W2 cascade matcher has both endpoints of a
// hypothetical round-trip in scope.
//
// Expected emit (per call site below, all under the enclosing function):
//
//   - GetUsersFromAPI         → http_calls → http:GET /users      (cascades to http:* /users via W2 wildcard fallback)
//   - PostAdminCommand        → http_calls → http:POST /admin     (cascades to http:* /admin)
//   - HeadHealthcheck         → http_calls → http:HEAD /health    (cascades to http:* /health)
//   - PostFormSubmit          → http_calls → http:POST /admin     (PostForm, also cascades)
//   - NewRequestExplicitVerb  → http_calls → http:PUT /method-a   (NewRequest with literal verb)
//   - CallExternal            → http_calls → http:GET /external/endpoint (no server match → AMBIGUOUS placeholder retained)
//   - ClientGetUsers          → http_calls → http:GET /users      (client.Get receiver form, matches via wildcard)
//   - DynamicURLSkipped       → no edge (URL variable — V0 skips dynamic)
package distributed_fixture

import (
	"net/http"
	"net/url"
	"strings"
)

// GetUsersFromAPI exercises http.Get with a path that maps to the existing
// "/users" server handler — the W2 cascade should rewire this http_calls
// edge from the AMBIGUOUS placeholder to the real "http:* /users" Endpoint.
func GetUsersFromAPI() {
	_, _ = http.Get("/users")
}

// PostAdminCommand exercises http.Post (3-arg form) targeting "/admin".
// Wildcard cascade: client method=POST, server method=*  → match via stage 2.
func PostAdminCommand() {
	_, _ = http.Post("/admin", "application/json", strings.NewReader(`{}`))
}

// HeadHealthcheck exercises http.Head — minor variant just to exercise the
// dispatch table beyond Get/Post.
func HeadHealthcheck() {
	_, _ = http.Head("/health")
}

// PostFormSubmit exercises http.PostForm (2-arg, method=POST per stdlib).
func PostFormSubmit() {
	_, _ = http.PostForm("/admin", url.Values{"k": {"v"}})
}

// NewRequestExplicitVerb exercises http.NewRequest where the method is a
// string literal — W2 picks "PUT" from arg[0]. /method-a is one of the
// MethodServer routes; cascade should match via wildcard.
func NewRequestExplicitVerb() {
	req, _ := http.NewRequest("PUT", "/method-a", strings.NewReader(""))
	_ = req
}

// CallExternal targets a URL that no server handler in this fixture
// listens on, so the AMBIGUOUS placeholder Endpoint should be retained
// after MatchHTTPClients — surfacing the external-API call for audit.
// Both absolute and path-form URLs are exercised; absolute URLs strip
// scheme+host and use just the path "/endpoint".
func CallExternal() {
	_, _ = http.Get("https://api.external.example.com/external/endpoint")
}

// ClientGetUsers exercises receiver-based client.Get on a *http.Client.
// Matches the same /users path as GetUsersFromAPI via wildcard cascade.
func ClientGetUsers(client *http.Client) {
	_, _ = client.Get("/users")
}

// DynamicURLSkipped passes a variable as the URL — V0 detector skips
// (no http_calls edge emitted). Documented limitation: const-fold is
// out of scope for V0.
func DynamicURLSkipped() {
	target := "/admin"
	_, _ = http.Get(target)
}
