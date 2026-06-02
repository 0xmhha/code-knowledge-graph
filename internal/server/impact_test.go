package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/server"
)

// TestHandleImpact_FunctionCallers builds the resolve fixture, queries
// /api/impact?seed_qname=a.Greet&depth=2 and asserts that `Hello` appears
// in the `callers` bucket. Mirrors internal/mcp.TestImpact_FunctionCallers
// so a regression in either the HTTP handler or the shared pkg/impact
// algorithm shows up here too.
func TestHandleImpact_FunctionCallers(t *testing.T) {
	store := buildFixture(t)
	srv := server.New(store, nil)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	q := url.Values{}
	q.Set("seed_qname", "a.Greet")
	q.Set("depth", "2")

	resp, err := http.Get(ts.URL + "/api/impact?" + q.Encode())
	if err != nil {
		t.Fatalf("GET /api/impact: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("GET /api/impact = %d, body: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("content-type"); ct != "application/json" {
		t.Errorf("content-type = %q, want application/json", ct)
	}

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if nf, _ := out["not_found"].(bool); nf {
		t.Fatalf("expected not_found=false; got %+v", out)
	}
	impact, _ := out["impact"].(map[string]any)
	if impact == nil {
		t.Fatalf("impact missing: %+v", out)
	}
	callersRaw, _ := impact["callers"].([]any)
	if len(callersRaw) == 0 {
		t.Fatalf("expected callers non-empty; got %+v", impact)
	}
	foundHello := false
	for _, c := range callersRaw {
		m, _ := c.(map[string]any)
		if name, _ := m["name"].(string); name == "Hello" {
			foundHello = true
			break
		}
	}
	if !foundHello {
		t.Errorf("expected Hello in callers; got %+v", callersRaw)
	}
}

// TestHandleImpact_BadRequest verifies that omitting both seed_qname and
// seed_file returns 400 (not 500), since the algorithm itself accepts
// either-or-neither and returns not_found.
func TestHandleImpact_BadRequest(t *testing.T) {
	store := buildFixture(t)
	srv := server.New(store, nil)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/impact")
	if err != nil {
		t.Fatalf("GET /api/impact: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// TestHandleImpact_SeedTooLong verifies the 4096-byte defensive cap on
// seed_qname / seed_file so an unbounded URL query can't stream into DB
// queries / impact computation. Either field above the cap returns 400.
func TestHandleImpact_SeedTooLong(t *testing.T) {
	store := buildFixture(t)
	srv := server.New(store, nil)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	huge := strings.Repeat("a", 4097)

	// seed_qname over the cap.
	q := url.Values{}
	q.Set("seed_qname", huge)
	resp, err := http.Get(ts.URL + "/api/impact?" + q.Encode())
	if err != nil {
		t.Fatalf("GET /api/impact (qname): %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized seed_qname status = %d, want 400", resp.StatusCode)
	}

	// seed_file over the cap (independent path).
	q = url.Values{}
	q.Set("seed_file", huge)
	resp, err = http.Get(ts.URL + "/api/impact?" + q.Encode())
	if err != nil {
		t.Fatalf("GET /api/impact (file): %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized seed_file status = %d, want 400", resp.StatusCode)
	}
}

// TestHandleImpact_NotFound asserts an unresolved seed surfaces
// not_found=true (rather than 500) so the viewer can render an empty
// state without parsing error bodies.
func TestHandleImpact_NotFound(t *testing.T) {
	store := buildFixture(t)
	srv := server.New(store, nil)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	q := url.Values{}
	q.Set("seed_qname", "totally.bogus.qname.xyz")
	resp, err := http.Get(ts.URL + "/api/impact?" + q.Encode())
	if err != nil {
		t.Fatalf("GET /api/impact: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if nf, _ := out["not_found"].(bool); !nf {
		t.Errorf("expected not_found=true; got %+v", out)
	}
	if got, _ := out["seed_qname"].(string); !strings.Contains(got, "bogus") {
		t.Errorf("expected seed_qname echo; got %v", out["seed_qname"])
	}
}
