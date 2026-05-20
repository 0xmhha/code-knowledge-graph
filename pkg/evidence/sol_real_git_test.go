package evidence_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	sol "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	"github.com/0xmhha/code-knowledge-graph/internal/temporal"
	"github.com/0xmhha/code-knowledge-graph/pkg/evidence"
	"github.com/0xmhha/code-knowledge-graph/pkg/hunkmodifies"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

const walletV0 = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Wallet {
    address public owner;
    uint256 public balance;

    constructor() { owner = msg.sender; }

    function deposit(uint256 amount) external {
        balance += amount;
    }

    function withdraw(uint256 amount) external {
        balance -= amount;
    }
}
`

const walletV1 = `// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

contract Wallet {
    address public owner;
    uint256 public balance;

    constructor() { owner = msg.sender; }

    function deposit(uint256 amount) external {
        balance += amount;
    }

    function withdraw(uint256 amount) external {
        require(msg.sender == owner, "not owner");
        balance -= amount;
    }
}
`

// TestBuildPack_RealGitFixture — W-C W11 V2 (2026-05-19). The W11 V1
// integration test wired a hand-assembled Commit + Hunk into the
// evidence layer to prove the parser→BuildPack wiring works after
// every W-C series addition (HasAssembly, IsFunctionTyped, SlotIndex
// with V2 packing, etc.). V2 replaces the synthetic temporal nodes
// with rows materialised from a real git checkout via the temporal
// package, exercising the LoadHistory + LoadHunks paths and the H2
// line-overlap modifies pass that buildpipe runs in production.
//
// The fixture stages a fresh git repo with two commits on Wallet.sol:
// an initial version without an owner check, and a second commit
// that hardens withdraw() with require(msg.sender == owner). The
// hunk extracted from commit 2 overlaps with Wallet.withdraw's
// line range, so the EdgeModifies pass links them. BuildPack then
// returns the harden-commit subject when the intent matches.
func TestBuildPack_RealGitFixture(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not available")
	}
	tmp := t.TempDir()
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", tmp}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit("init", "-q", "-b", "main")
	walletPath := filepath.Join(tmp, "Wallet.sol")
	if err := os.WriteFile(walletPath, []byte(walletV0), 0o644); err != nil {
		t.Fatalf("write v0: %v", err)
	}
	runGit("add", "Wallet.sol")
	runGit("commit", "-q", "-m", "Initial Wallet contract")
	if err := os.WriteFile(walletPath, []byte(walletV1), 0o644); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	runGit("commit", "-aqm", "Harden Wallet.withdraw with owner check")

	src, err := os.ReadFile(walletPath)
	if err != nil {
		t.Fatalf("read Wallet.sol: %v", err)
	}
	p := sol.New(tmp)
	pr, err := p.ParseFile(walletPath, src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	resolved, err := p.Resolve([]*parse.ParseResult{pr})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	hist, err := temporal.LoadHistory(tmp, 10)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if len(hist.Commits) < 2 {
		t.Fatalf("expected >=2 commits in real git fixture, got %d", len(hist.Commits))
	}
	hunks, err := temporal.LoadHunks(tmp, 200)
	if err != nil {
		t.Fatalf("LoadHunks: %v", err)
	}
	if len(hunks) == 0 {
		t.Fatalf("expected real hunks from git log -p, got 0")
	}

	nodes := append([]types.Node(nil), resolved.Nodes...)
	edges := append([]types.Edge(nil), resolved.Edges...)

	commitIDBySHA := make(map[string]string, len(hist.Commits))
	for sha, ci := range hist.Commits {
		id := parse.MakeID("commit:"+sha, "git", 0)
		commitIDBySHA[sha] = id
		nodes = append(nodes, types.Node{
			ID: id, Type: types.NodeCommit,
			QualifiedName: "commit:" + sha,
			Name:          sha[:8],
			Signature:     fmt.Sprintf("%d: %s", ci.Timestamp, ci.Subject),
			Confidence:    types.ConfExtracted,
		})
	}

	for i, h := range hunks {
		startLine := h.NewStart
		if startLine < 1 {
			startLine = 1
		}
		endLine := startLine
		if h.NewLines > 1 {
			endLine = h.NewStart + h.NewLines - 1
		}
		qname := fmt.Sprintf("hunk:%s:%s:%d", h.SHA, h.FilePath, h.Index)
		hid := parse.MakeID(qname, "git", i)
		nodes = append(nodes, types.Node{
			ID: hid, Type: types.NodeHunk,
			QualifiedName: qname,
			Name:          fmt.Sprintf("%s#%d", h.FilePath, h.Index),
			FilePath:      h.FilePath,
			StartLine:     startLine,
			EndLine:       endLine,
			Confidence:    types.ConfExtracted,
		})
		if cid, ok := commitIDBySHA[h.SHA]; ok {
			edges = append(edges, types.Edge{
				Src: cid, Dst: hid, Type: types.EdgeHasHunk,
				Count: 1, Confidence: types.ConfExtracted,
			})
		}
	}

	edges = append(edges, hunkmodifies.BuildEdges(nodes)...)

	store := &realParserFakeStore{nodes: nodes, edges: edges}
	pack, err := evidence.BuildPack(store, evidence.Options{
		Intent:       "wallet withdraw owner check",
		K:            5,
		BudgetTokens: 4000,
	})
	if err != nil {
		t.Fatalf("BuildPack: %v", err)
	}
	if pack == nil {
		t.Fatalf("BuildPack returned nil")
	}
	if len(pack.Hits) == 0 {
		t.Fatalf("expected >=1 hit for harden-withdraw intent")
	}
	for _, h := range pack.Hits {
		if h.Commit.SHA == "" {
			t.Errorf("hit with empty commit SHA: %+v", h)
		}
	}
}
