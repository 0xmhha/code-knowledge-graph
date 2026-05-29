package buildpipe

import (
	"log/slog"

	"github.com/0xmhha/code-knowledge-graph/pkg/policy"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// loadPolicy is the cold-path glue between pkg/policy and the build
// pipeline. Returns ([], [], nil) when policyFile is empty so the
// caller's "did anything change?" branch stays trivial.
//
// All non-empty inputs go through policy.LoadFromFile + policy.Resolve;
// resolve warnings are logged but never returned as errors — they
// represent stale governs[] references in the YAML and must not gate
// the build. Hard errors (missing file, malformed YAML, duplicate IDs)
// are returned so the caller can decide whether to fail or warn.
func loadPolicy(policyFile string, codeNodes []types.Node, log *slog.Logger) ([]types.Node, []types.Edge, error) {
	if policyFile == "" {
		return nil, nil, nil
	}
	f, err := policy.LoadFromFile(policyFile)
	if err != nil {
		return nil, nil, err
	}
	res := policy.Resolve(f, codeNodes, policyFile)
	for _, w := range res.Warnings {
		log.Warn("policy governs[] target not found",
			"policy_id", w.PolicyID,
			"target", w.TargetRef,
			"reason", w.Reason)
	}
	return res.Nodes, res.Edges, nil
}
