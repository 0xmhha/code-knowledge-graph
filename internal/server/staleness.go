package server

import (
	"os/exec"
	"strings"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// computeStaleness compares the manifest's recorded SrcCommit against the
// live `git rev-parse HEAD` of m.SrcRoot. Returns ("", false) when the
// manifest was not git-fingerprinted, or when git lookup fails — in that
// case the viewer simply won't show a stale banner.
func computeStaleness(m persist.Manifest) (current string, stale bool) {
	if m.StalenessMethod != "git" || m.SrcRoot == "" {
		return "", false
	}
	out, err := exec.Command("git", "-C", m.SrcRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", false
	}
	current = strings.TrimSpace(string(out))
	return current, current != m.SrcCommit
}
