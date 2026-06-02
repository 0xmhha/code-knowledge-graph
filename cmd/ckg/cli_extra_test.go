package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
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

// TestBuildCmd_LockPropagationFlagWired — W-A minor 5. End-to-end CLI
// invocation of `ckg build --lock-propagation`. Builds the W-A fixture
// twice (flag OFF + flag ON), opens the resulting graph.db with the same
// persist.OpenReadOnly path production code uses, and asserts that the
// ON build emits *strictly more* accessed_under_lock edges than OFF.
//
// Why this exists when integration-level tests in
// internal/buildpipe/lock_propagation_test.go already exercise the same
// option directly through buildpipe.Run(): those tests drive Options
// in-process. They don't catch a regression where the cobra flag wiring
// in cmd/ckg/build.go fails to plumb `lockPropagation` into Options —
// the wire-up is one line, but a typo there would silently downgrade
// every user invocation to OFF while every internal test stays green.
// This e2e fills that gap by exercising the full cobra → buildpipe.Run
// → persist write → persist read pipeline.
func TestBuildCmd_LockPropagationFlagWired(t *testing.T) {
	fixtureAbs, err := filepath.Abs("../../internal/buildpipe/testdata/lock_propagation")
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}

	// Flag presence — guards against an accidental removal of the
	// --lock-propagation flag definition (the e2e diff below would then
	// fail with "unknown flag" instead of the clearer "flag is gone"
	// message this assertion surfaces).
	probe := newBuildCmd()
	if probe.Flag("lock-propagation") == nil {
		t.Fatal("build subcommand missing --lock-propagation flag")
	}

	runBuild := func(t *testing.T, lockProp bool) string {
		t.Helper()
		out := t.TempDir()
		args := []string{
			"--src=" + fixtureAbs,
			"--out=" + out,
			"--lang=go",
			"--no-cache", // required for full effect per the flag's own help text
		}
		if lockProp {
			args = append(args, "--lock-propagation")
		}
		cmd := newBuildCmd()
		cmd.SetArgs(args)
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("Execute (lockProp=%v): %v", lockProp, err)
		}
		return out
	}

	countUnderLock := func(t *testing.T, outDir string) int {
		t.Helper()
		store, err := persist.OpenReadOnly(filepath.Join(outDir, "graph.db"))
		if err != nil {
			t.Fatalf("OpenReadOnly: %v", err)
		}
		defer func() { _ = store.Close() }()
		edges, err := store.QueryEdgesByType(string(types.EdgeAccessedUnderLock))
		if err != nil {
			t.Fatalf("QueryEdgesByType: %v", err)
		}
		return len(edges)
	}

	offDir := runBuild(t, false)
	onDir := runBuild(t, true)
	offCount := countUnderLock(t, offDir)
	onCount := countUnderLock(t, onDir)

	if onCount <= offCount {
		t.Fatalf("--lock-propagation flag not wired: ON=%d, OFF=%d (expected ON > OFF)",
			onCount, offCount)
	}
	// Bound the floor: OFF must emit at least one edge (intra-fn B1 pass —
	// the W-A fixture has direct lock-then-touch in lock_propagation/single_hop.go).
	// Catches the reverse failure where the build inadvertently disables both
	// passes (e.g. emit table dropped).
	if offCount == 0 {
		t.Errorf("OFF emitted 0 accessed_under_lock edges; the intra-fn B1 pass should still fire")
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
	defer func() { _ = ln.Close() }()
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
	// buildGraph runs first so go/packages can locate the `go` toolchain on
	// PATH; the openBrowser goroutine that needs PATH="" comes after.
	graphDir := t.TempDir()
	buildGraph(t, graphDir)

	t.Setenv("PATH", "")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pre-bind: %v", err)
	}
	defer func() { _ = ln.Close() }()
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
	_ = w.Close() // writer closed → reader immediately returns EOF
	os.Stdin = r
	defer func() {
		os.Stdin = origStdin
		_ = r.Close()
	}()

	cmd := newMCPCmd()
	cmd.SetArgs([]string{"--graph=" + graphDir})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	// Execute returns nil (clean EOF) or a non-nil error — either is fine;
	// we only care that the RunE body ran without panicking or hanging.
	cmd.Execute() //nolint:errcheck
}
