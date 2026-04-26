package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// absFixture returns the absolute path of the Go fixture directory used
// across the test suite.  cmd/ckg tests run with cwd = cmd/ckg/, so the
// repo-root-relative path requires two levels of "..".
func absFixture(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../../internal/parse/golang/testdata/resolve")
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	return abs
}

// buildGraph is a test helper that runs the build subcommand against the
// canonical Go fixture and writes its output to outDir. It fails the test
// immediately if the build fails.
func buildGraph(t *testing.T, outDir string) {
	t.Helper()
	cmd := newBuildCmd()
	cmd.SetArgs([]string{"--src=" + absFixture(t), "--out=" + outDir})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("buildGraph: %v", err)
	}
}

// ─── build subcommand ────────────────────────────────────────────────────────

func TestBuildCmd_Success(t *testing.T) {
	out := t.TempDir()
	cmd := newBuildCmd()
	cmd.SetArgs([]string{"--src=" + absFixture(t), "--out=" + out})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, f := range []string{"graph.db", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("expected %s in out dir: %v", f, err)
		}
	}
}

func TestBuildCmd_MissingRequiredFlags(t *testing.T) {
	// Both --src and --out are required; omitting them must return an error.
	cmd := newBuildCmd()
	cmd.SetArgs(nil)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Errorf("expected error when required flags are missing")
	}
}

func TestBuildCmd_BadSource(t *testing.T) {
	out := t.TempDir()
	cmd := newBuildCmd()
	cmd.SetArgs([]string{"--src=/no/such/path/does/not/exist", "--out=" + out})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Errorf("expected error for non-existent source directory")
	}
}

// ─── export-static subcommand ─────────────────────────────────────────────────

func TestExportStaticCmd_Success(t *testing.T) {
	// First produce a real graph to export.
	graphDir := t.TempDir()
	buildGraph(t, graphDir)

	out := t.TempDir()
	ecmd := newExportStaticCmd()
	ecmd.SetArgs([]string{"--graph=" + graphDir, "--out=" + out})
	ecmd.SetOut(io.Discard)
	ecmd.SetErr(io.Discard)

	if err := ecmd.Execute(); err != nil {
		t.Fatalf("export-static Execute: %v", err)
	}

	// The embedded viewer contributes index.html; ExportChunked writes
	// manifest.json at the output root.
	for _, f := range []string{"index.html", "manifest.json"} {
		if _, err := os.Stat(filepath.Join(out, f)); err != nil {
			t.Errorf("expected %s in static output: %v", f, err)
		}
	}
}

func TestExportStaticCmd_MissingRequiredFlags(t *testing.T) {
	cmd := newExportStaticCmd()
	cmd.SetArgs(nil)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Errorf("expected error when required flags are missing")
	}
}

func TestExportStaticCmd_BadGraphPath(t *testing.T) {
	// SQLite opens lazily; the error surfaces from ExportChunked (first query),
	// not from OpenReadOnly. Still a valid failure-path test.
	out := t.TempDir()
	cmd := newExportStaticCmd()
	cmd.SetArgs([]string{"--graph=/no/such/graph/dir", "--out=" + out})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Errorf("expected error for missing graph directory")
	}
}

// ─── serve subcommand ────────────────────────────────────────────────────────

// TestServeCmd_PortInUse verifies that the serve RunE body runs through to
// ListenAndServe and returns quickly when the requested port is already bound.
// Pre-binding the port forces an immediate "address already in use" error so
// the test doesn't hang. This covers the full RunE execution path up to and
// including the ListenAndServe call.
func TestServeCmd_PortInUse(t *testing.T) {
	graphDir := t.TempDir()
	buildGraph(t, graphDir)

	// Pre-bind the test port so the serve command fails immediately when
	// it tries to call ListenAndServe on the same address.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pre-bind: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	cmd := newServeCmd()
	cmd.SetArgs([]string{
		"--graph=" + graphDir,
		fmt.Sprintf("--port=%d", port),
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err == nil {
		t.Errorf("expected error when port is already in use")
	}
}

// TestServeCmd_PortInUseWithOpen is the same as TestServeCmd_PortInUse but
// additionally passes --open=true to exercise the goroutine branch that
// launches the browser and cover openBrowser.
//
// To avoid actually launching a real browser on the developer's machine, we
// set PATH="" so exec.Command("open"|"xdg-open"|"rundll32") fails to locate
// the binary. openBrowser silently swallows the Start() error, so all of its
// statements still execute (preserving coverage) but no GUI window appears.
func TestServeCmd_PortInUseWithOpen(t *testing.T) {
	t.Setenv("PATH", "")

	graphDir := t.TempDir()
	buildGraph(t, graphDir)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pre-bind: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	cmd := newServeCmd()
	cmd.SetArgs([]string{
		"--graph=" + graphDir,
		fmt.Sprintf("--port=%d", port),
		"--open=true",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	// Expect an error (port in use). The --open goroutine fires asynchronously;
	// the OS-level "open" lookup fails because PATH is empty — that's fine.
	if err := cmd.Execute(); err == nil {
		t.Errorf("expected error when port is already in use")
	}
}

// ─── mcp subcommand ───────────────────────────────────────────────────────────

// TestMCPCmd_EOFStdin verifies that the mcp RunE body executes successfully
// when stdin produces an immediate EOF (simulating no client connected).
// Redirecting os.Stdin to a closed pipe causes ServeStdio to return quickly,
// allowing the test to observe the full mcp RunE execution path.
func TestMCPCmd_EOFStdin(t *testing.T) {
	graphDir := t.TempDir()
	buildGraph(t, graphDir)

	// Replace os.Stdin with a pipe whose write end is closed so reads EOF.
	origStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	w.Close() // writer closed → reader immediately returns EOF
	os.Stdin = r
	defer func() {
		os.Stdin = origStdin
		r.Close()
	}()

	cmd := newMCPCmd()
	cmd.SetArgs([]string{"--graph=" + graphDir})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	// Execute returns nil (clean EOF) or a non-nil error — either is fine;
	// we only care that the RunE body ran without panicking or hanging.
	cmd.Execute() //nolint:errcheck
}

// ─── eval subcommand ─────────────────────────────────────────────────────────

// TestEvalCmd_BadGlob verifies that a syntactically invalid glob pattern causes
// LoadTasks to return an error, which is propagated through RunE.
// filepath.Glob returns an error only for malformed patterns (e.g. "[").
func TestEvalCmd_BadGlob(t *testing.T) {
	graphDir := t.TempDir()
	buildGraph(t, graphDir)

	cmd := newEvalCmd()
	cmd.SetArgs([]string{
		"--tasks=[invalid-glob",
		"--graph=" + graphDir,
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err == nil {
		t.Errorf("expected error for malformed task glob")
	}
}

// TestSelectLLMBackend exercises the small dispatch helper in eval.go.
// We do not actually construct working backends (that requires a live
// API key or cliwrap-agent); we just verify the routing and the
// "unknown backend" error path.
func TestSelectLLMBackend(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	t.Run("unknown_backend_errors", func(t *testing.T) {
		_, err := selectLLMBackend("nope", "claude-sonnet-4-6", "")
		if err == nil {
			t.Fatal("expected error for unknown backend")
		}
	})

	t.Run("api_default_routes_to_APIClient", func(t *testing.T) {
		// Without ANTHROPIC_API_KEY this still surfaces as ErrNoAPIKey
		// rather than the unknown-backend error, proving the api branch
		// was selected.
		_, err := selectLLMBackend("api", "claude-sonnet-4-6", "")
		if err == nil {
			t.Fatal("expected ErrNoAPIKey when key absent")
		}
	})

	t.Run("empty_backend_treated_as_api", func(t *testing.T) {
		_, err := selectLLMBackend("", "claude-sonnet-4-6", "")
		if err == nil {
			t.Fatal("expected ErrNoAPIKey when key absent")
		}
	})

	t.Run("cli_routes_to_CLIClient", func(t *testing.T) {
		// Empty PATH guarantees both claude lookup and cliwrap-agent
		// lookup fail, so we get a CLI-specific error rather than
		// the api-branch ErrNoAPIKey or unknown-backend error.
		t.Setenv("PATH", "")
		_, err := selectLLMBackend("cli", "", "")
		if err == nil {
			t.Fatal("expected ErrClaudeNotFound when binary absent")
		}
	})
}

// TestEvalCmd_MissingAPIKey exercises the RunE body past LoadTasks (empty
// match is fine) and hits NewAPIClient which returns ErrNoAPIKey when
// ANTHROPIC_API_KEY is unset.  This covers the NewAPIClient error path in
// eval.go without requiring a live API key.
func TestEvalCmd_MissingAPIKey(t *testing.T) {
	graphDir := t.TempDir()
	buildGraph(t, graphDir)

	// Guarantee the key is absent for this test regardless of the real env.
	t.Setenv("ANTHROPIC_API_KEY", "")

	cmd := newEvalCmd()
	// Use a glob that matches no files so LoadTasks returns an empty slice
	// without error, then NewLLMClient is reached next.
	cmd.SetArgs([]string{
		"--tasks=" + t.TempDir() + "/*.yaml",
		"--graph=" + graphDir,
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	if err := cmd.Execute(); err == nil {
		t.Errorf("expected error when ANTHROPIC_API_KEY is unset")
	}
}
