package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveOutDir_Empty(t *testing.T) {
	got, err := resolveOutDir("/tmp/out", "", ".")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/out" {
		t.Errorf("empty tag: got %q want /tmp/out", got)
	}
}

func TestResolveOutDir_LiteralTag(t *testing.T) {
	got, err := resolveOutDir("/tmp/out", "v2-snapshot", ".")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/out-v2-snapshot" {
		t.Errorf("literal tag: got %q want /tmp/out-v2-snapshot", got)
	}
}

func TestResolveOutDir_AutoCommitHash(t *testing.T) {
	got, err := resolveOutDir("/tmp/out", "auto-commit-hash", ".")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "/tmp/out-") {
		t.Fatalf("auto tag: got %q, want prefix /tmp/out-", got)
	}
	suffix := strings.TrimPrefix(got, "/tmp/out-")
	if len(suffix) != 12 {
		t.Errorf("auto tag suffix should be 12-char short SHA, got %q (len=%d)", suffix, len(suffix))
	}
}

func TestResolveOutDir_AutoCommitHash_NonGitDir(t *testing.T) {
	_, err := resolveOutDir("/tmp/out", "auto-commit-hash", t.TempDir())
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}

func TestGraphExists_True(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "graph.db"), []byte("x"), 0o644)
	if !graphExists(dir) {
		t.Error("should detect existing graph.db")
	}
}

func TestGraphExists_False(t *testing.T) {
	if graphExists(t.TempDir()) {
		t.Error("should return false for empty dir")
	}
}

func TestCheckoutWorktree_NonGitDir(t *testing.T) {
	_, _, err := checkoutWorktree(t.TempDir(), "HEAD")
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}

func TestCheckoutWorktree_Success(t *testing.T) {
	wt, cleanup, err := checkoutWorktree(".", "HEAD")
	if err != nil {
		t.Fatalf("checkoutWorktree: %v", err)
	}
	defer cleanup()

	if _, err := os.Stat(filepath.Join(wt, "go.mod")); err != nil {
		t.Errorf("worktree should contain go.mod: %v", err)
	}
}
