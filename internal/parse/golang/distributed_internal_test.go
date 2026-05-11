package golang

import "testing"

// TestSplitGo122Pattern locks the Go 1.22+ HTTP pattern splitter's
// conservative-fallback semantics. The function backs schema 1.9 §6.2's
// `http:METHOD /route` qname format, so any silent regression here would
// mangle cross-language endpoint matching (W2). The bound is:
//   - Valid Go 1.22 method-prefix form: `"<UPPER>+ <slash-prefixed-path>"`
//     → split into (method, route).
//   - Anything else (no space, lowercase verb, no leading slash, multi-token
//     head, trailing-space-only): fall through to `("*", original-pattern)`.
//     net/http treats the original as a path-only pattern accepting all
//     verbs; we mirror that by keeping the original verbatim as route.
func TestSplitGo122Pattern(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantMethod string
		wantRoute  string
	}{
		// Valid Go 1.22 method-prefixed patterns.
		{"GET", "GET /users", "GET", "/users"},
		{"POST with param", "POST /api/users/:id", "POST", "/api/users/:id"},
		{"DELETE with brace", "DELETE /admin/{id}", "DELETE", "/admin/{id}"},
		{"PUT minimal", "PUT /x", "PUT", "/x"},

		// Fall-through (kept verbatim as wildcard route).
		{"plain path no method", "/users", "*", "/users"},
		{"empty", "", "*", ""},
		{"method-only no space", "GET", "*", "GET"},
		{"lowercase verb", "get /users", "*", "get /users"},
		{"double space", "GET  /users", "*", "GET  /users"},
		{"multi-token head", "GET POST /x", "*", "GET POST /x"},
		{"trailing space only", "GET ", "*", "GET "},
		{"path without slash", "GET users", "*", "GET users"},
		{"leading space", " GET /x", "*", " GET /x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, r := splitGo122Pattern(tc.in)
			if m != tc.wantMethod || r != tc.wantRoute {
				t.Errorf("splitGo122Pattern(%q) = (%q, %q), want (%q, %q)",
					tc.in, m, r, tc.wantMethod, tc.wantRoute)
			}
		})
	}
}
