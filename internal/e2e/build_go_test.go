//go:build e2e

package e2e_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestE2EBuildGoFixture(t *testing.T) {
	bin, err := exec.LookPath("ckg")
	if err != nil {
		// fall back to local build artifact
		bin, _ = filepath.Abs("../../bin/ckg")
	}
	out := t.TempDir()
	cmd := exec.Command(bin, "build",
		"--src", "../parse/golang/testdata/resolve",
		"--out", out)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("ckg build: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "graph.db")); err != nil {
		t.Errorf("expected graph.db: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "manifest.json")); err != nil {
		t.Errorf("expected manifest.json: %v", err)
	}
}
