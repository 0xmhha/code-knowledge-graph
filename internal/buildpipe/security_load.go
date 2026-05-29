package buildpipe

import (
	"log/slog"

	"github.com/0xmhha/code-knowledge-graph/pkg/security"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// loadSecurityPatterns is the cold-path glue between pkg/security and
// the build pipeline. Mirrors loadPolicy: ([], [], nil) on empty path
// for a trivial no-op branch in the caller, propagate hard errors
// (missing file, malformed YAML, duplicate ids, invalid severity)
// up, log soft warnings (unmatched matches[] qnames) at warn level.
func loadSecurityPatterns(securityFile string, codeNodes []types.Node, log *slog.Logger) ([]types.Node, []types.Edge, error) {
	if securityFile == "" {
		return nil, nil, nil
	}
	f, err := security.LoadFromFile(securityFile)
	if err != nil {
		return nil, nil, err
	}
	res := security.Resolve(f, codeNodes, securityFile)
	for _, w := range res.Warnings {
		log.Warn("security pattern matches[] target not found",
			"pattern_id", w.PatternID,
			"target", w.TargetRef,
			"reason", w.Reason)
	}
	return res.Nodes, res.Edges, nil
}
