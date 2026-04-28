package main

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"testing"
)

// absSyntheticGoBackend returns the absolute path to the Go-only subset of
// the synthetic corpus. cmd/ckg tests run with cwd = cmd/ckg/, so the
// repo-root-relative path requires two levels of "..".
func absSyntheticGoBackend(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../../testdata/synthetic")
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	return abs
}

// TestAuditCmd_SyntheticParity is the integration assertion for user
// completeness condition #2: build the synthetic Go corpus, then audit the
// resulting graph and confirm the build set and DB set agree (exit 0,
// IsParity true). If this ever flips to drift, the production build path
// has regressed against the Go build oracle.
func TestAuditCmd_SyntheticParity(t *testing.T) {
	graphDir := t.TempDir()

	// Build with --lang=go to keep the audit comparison apples-to-apples.
	bcmd := newBuildCmd()
	bcmd.SetArgs([]string{
		"--src=" + absSyntheticGoBackend(t),
		"--out=" + graphDir,
		"--lang=go",
	})
	bcmd.SetOut(io.Discard)
	bcmd.SetErr(io.Discard)
	if err := bcmd.Execute(); err != nil {
		t.Fatalf("build: %v", err)
	}

	acmd := newAuditCmd()
	acmd.SetArgs([]string{
		"--src=" + absSyntheticGoBackend(t),
		"--graph=" + graphDir,
	})
	var stdout bytes.Buffer
	acmd.SetOut(&stdout)
	acmd.SetErr(io.Discard)

	if err := acmd.Execute(); err != nil {
		// Any error here is a genuine failure: parity should yield nil.
		t.Fatalf("audit: %v\noutput: %s", err, stdout.String())
	}
	out := stdout.String()
	if !contains(out, "verdict: PARITY") {
		t.Errorf("expected PARITY in audit output, got: %s", out)
	}
}

func TestAuditCmd_MissingFlags(t *testing.T) {
	cmd := newAuditCmd()
	cmd.SetArgs(nil)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	if err := cmd.Execute(); err == nil {
		t.Errorf("expected error when required flags are missing")
	}
}

func TestAuditCmd_UnknownLanguage(t *testing.T) {
	graphDir := t.TempDir()
	cmd := newAuditCmd()
	cmd.SetArgs([]string{
		"--src=" + absSyntheticGoBackend(t),
		"--graph=" + graphDir,
		"--language=ts",
	})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unsupported language")
	}
	var ae auditExitCode
	if !errors.As(err, &ae) || int(ae) != 2 {
		t.Errorf("expected auditExitCode(2), got %v", err)
	}
}

func contains(s, sub string) bool { return bytes.Contains([]byte(s), []byte(sub)) }
