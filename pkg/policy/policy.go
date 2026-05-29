// Package policy loads governance/protocol policy metadata from a YAML
// file and converts it into NodePolicy + EdgeGovernedBy rows that fold
// into the main CKG graph.
//
// Why a separate package (and an external YAML rather than parsed code):
// the policies CKG cares about — fork block activations, gas schedules,
// consensus parameters, security pattern annotations — are typically
// described in documentation and config files that don't have a single
// canonical place in the source tree. Parsing them out of source would
// either miss most of the signal (the rationale lives in comments) or
// over-fit each codebase's idiosyncratic file layout. A separate YAML
// per project keeps the policy surface explicit and editable without
// forcing CKG to know each project's conventions.
//
// See docs/PROJECT-BLUEPRINT-ALIGNMENT.md §4.2 P1 #4 for the design
// intent and the go-stablenet-specific use cases (params/config.go
// fork blocks, consensus/wbft/* policies, systemcontracts/*).
//
// # Schema
//
// The YAML envelope:
//
//	policies:
//	  - id: "fork.berlin"
//	    name: "Berlin Hard Fork"
//	    category: "consensus"
//	    description: "Increases gas cost for SLOAD/SSTORE..."
//	    activated_at: 12244000        # optional, fork-style policies
//	    governs:                      # qnames of code symbols this policy constrains
//	      - "params.MainnetChainConfig.BerlinBlock"
//	      - "core/vm.gasSLoadEIP2929"
//
// Every Policy node carries id, name, category, description as
// node fields (id → QualifiedName; name → Name; category → SubKind).
// activated_at and any other typed metadata fall into the attrs JSON
// blob via marshalNodeAttrs at persist time — this keeps the schema
// extensible without per-field column churn.
package policy

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Entry is one policy row from the YAML file. Field tags use snake_case
// to match the canonical YAML form; the lowerCamelCase JSON tags exist
// so the same struct can round-trip through internal/manifest if a
// caller wants to embed loaded policies in a build report.
type Entry struct {
	ID          string   `yaml:"id" json:"id"`
	Name        string   `yaml:"name" json:"name"`
	Category    string   `yaml:"category,omitempty" json:"category,omitempty"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	ActivatedAt int64    `yaml:"activated_at,omitempty" json:"activated_at,omitempty"`
	Governs     []string `yaml:"governs,omitempty" json:"governs,omitempty"`
}

// File is the top-level YAML envelope. Single key "policies" keeps the
// document explicit (a bare list at the root is valid YAML but harder
// to read in editors and harder to extend with envelope-level metadata
// — version, source-of-truth pointer, etc.).
type File struct {
	Policies []Entry `yaml:"policies"`
}

// LoadFromFile reads, parses, and validates a policy YAML file. Returns
// an error wrapping the underlying yaml.Unmarshal / os.ReadFile error
// when the file is missing or malformed — callers should treat policy
// loading as best-effort enrichment and fall back to building without
// it when this errors. An empty / absent `policies:` key is NOT an
// error; the result has zero entries.
//
// Validation: every entry must have a non-empty ID. Duplicate IDs are
// flagged because Resolve treats ID as the node's primary key (it
// becomes QualifiedName); two entries sharing an ID would collide on
// INSERT OR REPLACE and silently drop one. Empty governs lists are
// allowed — a policy entry that documents context without a direct
// code anchor still adds searchable rationale to the graph.
func LoadFromFile(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy file %s: %w", path, err)
	}
	var f File
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse policy yaml %s: %w", path, err)
	}
	seen := map[string]bool{}
	for i, e := range f.Policies {
		if strings.TrimSpace(e.ID) == "" {
			return nil, fmt.Errorf("policy[%d] has empty id (name=%q)", i, e.Name)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("policy id %q is declared more than once", e.ID)
		}
		seen[e.ID] = true
	}
	return &f, nil
}

// ResolveResult bundles the outcome of Resolve so the buildpipe caller
// can persist the rows and surface the warnings as build metadata.
type ResolveResult struct {
	Nodes    []types.Node
	Edges    []types.Edge
	Warnings []ResolveWarning
}

// ResolveWarning records a governs[] entry that didn't find a matching
// code node in the parsed graph. Surfaced verbatim in the build log
// so editors of the policy YAML can spot stale references (a function
// got renamed, a config field got moved) without having to compare
// the YAML against the source tree by hand.
type ResolveWarning struct {
	PolicyID  string
	TargetRef string
	Reason    string
}

// Resolve builds the Policy nodes + governed_by edges to fold into the
// main graph.
//
//   - One NodePolicy per entry, with QualifiedName=ID, Name=Name,
//     SubKind=Category, DocComment=Description. FilePath cites the
//     loading file (set by the caller before persist if a citation is
//     wanted; Resolve doesn't see the path).
//   - One EdgeGovernedBy per (matched governs[i], policy) pair. Direction
//     = governed code symbol → policy.
//
// The matching loop walks an index built once from byQname so a 50-entry
// policy file × 200k code nodes stays at the O(P+N) order rather than
// O(P·N). Missing references emit a ResolveWarning instead of failing
// the build — policy metadata is additive; an outage in the YAML must
// not block the parsing-derived graph from landing.
func Resolve(f *File, codeNodes []types.Node, policyFilePath string) ResolveResult {
	out := ResolveResult{}
	if f == nil || len(f.Policies) == 0 {
		return out
	}
	byQname := make(map[string]string, len(codeNodes))
	for _, n := range codeNodes {
		if n.QualifiedName == "" {
			continue
		}
		byQname[n.QualifiedName] = n.ID
	}
	for _, e := range f.Policies {
		policyNode := types.Node{
			ID:            policyNodeID(e.ID),
			Type:          types.NodePolicy,
			Name:          e.Name,
			QualifiedName: e.ID,
			FilePath:      policyFilePath,
			StartLine:     1, // YAML position tracking lands in a follow-up
			Language:      "policy",
			Visibility:    "public",
			Signature:     buildSignature(e),
			DocComment:    e.Description,
			SubKind:       e.Category,
			Confidence:    types.ConfExtracted,
		}
		out.Nodes = append(out.Nodes, policyNode)

		for _, target := range e.Governs {
			target = strings.TrimSpace(target)
			if target == "" {
				continue
			}
			srcID, ok := byQname[target]
			if !ok {
				out.Warnings = append(out.Warnings, ResolveWarning{
					PolicyID:  e.ID,
					TargetRef: target,
					Reason:    "no code node found with this qualified_name",
				})
				continue
			}
			out.Edges = append(out.Edges, types.Edge{
				Src:        srcID,
				Dst:        policyNode.ID,
				Type:       types.EdgeGovernedBy,
				FilePath:   policyFilePath,
				Confidence: types.ConfExtracted,
			})
		}
	}
	// Stable ordering so persist-time INSERT order is deterministic
	// across runs — keeps the DB byte-stable when nothing changed.
	sort.Slice(out.Nodes, func(i, j int) bool {
		return out.Nodes[i].QualifiedName < out.Nodes[j].QualifiedName
	})
	sort.Slice(out.Edges, func(i, j int) bool {
		if out.Edges[i].Src != out.Edges[j].Src {
			return out.Edges[i].Src < out.Edges[j].Src
		}
		return out.Edges[i].Dst < out.Edges[j].Dst
	})
	sort.Slice(out.Warnings, func(i, j int) bool {
		if out.Warnings[i].PolicyID != out.Warnings[j].PolicyID {
			return out.Warnings[i].PolicyID < out.Warnings[j].PolicyID
		}
		return out.Warnings[i].TargetRef < out.Warnings[j].TargetRef
	})
	return out
}

// policyNodeID is the deterministic node-id derivation for policy
// rows. The "policy:" prefix keeps the namespace separate from
// parser-derived node IDs (which are content-hash + qname-derived)
// so a code symbol and a policy with the same qname could never
// collide on the nodes PK.
func policyNodeID(id string) string {
	return "policy:" + id
}

// buildSignature renders a one-line summary for the Policy node's
// Signature field so search_text / smartContext have something to
// match on beyond name + description. Order is fixed (category →
// activated_at → governs count) so search snippets stay readable.
func buildSignature(e Entry) string {
	parts := make([]string, 0, 3)
	if e.Category != "" {
		parts = append(parts, "category="+e.Category)
	}
	if e.ActivatedAt > 0 {
		parts = append(parts, fmt.Sprintf("activated_at=%d", e.ActivatedAt))
	}
	if n := len(e.Governs); n > 0 {
		parts = append(parts, fmt.Sprintf("governs=%d", n))
	}
	return strings.Join(parts, " ")
}
