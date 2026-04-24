// Package link runs cross-language linking after per-language Pass 2 (spec §4.7).
// V0 implements only Sol -> TS bindings via name match. Cross-lang Go links are V1+.
package link

import (
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// ABISig mirrors solidity.ABISig to avoid coupling link → parse/solidity.
type ABISig struct {
	ContractName string
	FunctionName string
	ParamTypes   []string
}

// SolToTS emits binds_to edges from each Solidity Contract node to a
// matching TypeScript Class node sharing the same Name. The ABI map is
// retained to support future signature-aware matching.
func SolToTS(nodes []types.Node, abi map[string][]ABISig) []types.Edge {
	tsClassByName := map[string][]types.Node{}
	for _, n := range nodes {
		if n.Language == "ts" && n.Type == types.NodeClass {
			tsClassByName[n.Name] = append(tsClassByName[n.Name], n)
		}
	}
	var out []types.Edge
	for _, n := range nodes {
		if n.Language != "sol" || n.Type != types.NodeContract {
			continue
		}
		matches := tsClassByName[n.Name]
		if len(matches) == 0 {
			continue
		}
		_ = abi[n.Name] // V0: no signature filter; reserved for V1+
		// Pick the most-likely binding: shortest path containing "contracts" or "typechain".
		best := pickBest(matches)
		out = append(out, types.Edge{
			Src: n.ID, Dst: best.ID, Type: types.EdgeBindsTo,
			Count: 1, Confidence: types.ConfInferred,
		})
	}
	return out
}

func pickBest(cands []types.Node) types.Node {
	best := cands[0]
	for _, c := range cands[1:] {
		if score(c) > score(best) {
			best = c
		}
	}
	return best
}

func score(n types.Node) int {
	s := 0
	for _, hint := range []string{"typechain", "contracts", "abi"} {
		if containsFold(n.FilePath, hint) {
			s++
		}
	}
	return s
}

func containsFold(s, sub string) bool {
	// case-insensitive substring; avoids importing strings just to lowercase.
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
