// Package security loads security risk pattern annotations from a YAML
// file and converts them into NodeSecurityPattern + EdgeHasSecurityPattern
// rows that fold into the main CKG graph.
//
// Where pkg/policy answers "why does this code exist?" (governance,
// fork blocks, gas schedules), pkg/security answers "what could go
// wrong with this code?" — the curated list of reentrancy candidates,
// access-control gaps, Byzantine attack surfaces, integer overflow
// hotspots that domain experts have flagged. An LLM modifying a
// listed symbol sees the risk surface as part of the retrieval
// envelope instead of having to run a separate static analyser
// (slither / mythril / semgrep) and reconcile its findings.
//
// See docs/PROJECT-BLUEPRINT-ALIGNMENT.md §4.2 P1 #5 for the design
// intent. The MVP is purely YAML-driven (operators curate the
// matches[] list); automated pattern detection (slither-style flow
// rules) is deferred to a follow-up — the data model here is the
// same either way.
//
// # Schema
//
// The YAML envelope:
//
//	security_patterns:
//	  - id: "reentrancy.external_call_after_state_change"
//	    name: "Reentrancy: external call after state change"
//	    category: "reentrancy"        # reentrancy | access-control | byzantine | overflow | …
//	    severity: "high"              # info | low | medium | high | critical
//	    description: "..."
//	    remediation: "..."            # optional fix guidance
//	    matches:                      # qnames of code symbols this pattern applies to
//	      - "Vault.withdraw"
//	      - "Token.transfer"
//
// Every SecurityPattern node carries id → QualifiedName, name → Name,
// category → SubKind, description + remediation → DocComment.
// severity rides in the Signature field as "severity=<level>" so
// search snippets and post-filter ordering can use it without
// touching the attrs JSON blob.
package security

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Severity levels. Mirror the conventional ranking used by Slither,
// Semgrep, CodeQL et al. so consumers familiar with those tools can
// map findings directly without an enum translation layer.
const (
	SeverityInfo     = "info"
	SeverityLow      = "low"
	SeverityMedium   = "medium"
	SeverityHigh     = "high"
	SeverityCritical = "critical"
)

// validSeverities is the closed enum the loader validates against.
// Sorted ascending so a future "severity ≥ medium" filter can use
// binary search; today's check is a small switch in isValidSeverity.
var validSeverities = []string{
	SeverityInfo, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical,
}

func isValidSeverity(s string) bool {
	return slices.Contains(validSeverities, s)
}

// Entry is one security pattern row from the YAML file.
type Entry struct {
	ID          string   `yaml:"id" json:"id"`
	Name        string   `yaml:"name" json:"name"`
	Category    string   `yaml:"category,omitempty" json:"category,omitempty"`
	Severity    string   `yaml:"severity,omitempty" json:"severity,omitempty"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Remediation string   `yaml:"remediation,omitempty" json:"remediation,omitempty"`
	Matches     []string `yaml:"matches,omitempty" json:"matches,omitempty"`
}

// File is the top-level YAML envelope. The single-key envelope mirrors
// pkg/policy.File — explicit, editor-friendly, room to grow envelope-
// level metadata (source-of-truth URL, last-reviewed date) without
// breaking existing consumers.
type File struct {
	SecurityPatterns []Entry `yaml:"security_patterns"`
}

// LoadFromFile reads, parses, and validates a security pattern YAML.
// Hard errors:
//   - I/O / parse failure (wrapped underlying error)
//   - Empty id (would collide on the SecurityPattern node PK)
//   - Duplicate id (same collision)
//   - Severity outside the closed enum (loose strings would let typos
//     past the boundary and surface as silent under/over-counts in
//     downstream "severity ≥ high" filters)
//
// Empty / absent security_patterns key is NOT an error; the result
// has zero entries — useful when an operator wants to land the file
// before populating it.
func LoadFromFile(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read security file %s: %w", path, err)
	}
	var f File
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse security yaml %s: %w", path, err)
	}
	seen := map[string]bool{}
	for i, e := range f.SecurityPatterns {
		if strings.TrimSpace(e.ID) == "" {
			return nil, fmt.Errorf("security_patterns[%d] has empty id (name=%q)", i, e.Name)
		}
		if seen[e.ID] {
			return nil, fmt.Errorf("security pattern id %q is declared more than once", e.ID)
		}
		seen[e.ID] = true
		if e.Severity != "" && !isValidSeverity(e.Severity) {
			return nil, fmt.Errorf("security_patterns[%d] (id=%q) has invalid severity %q (want one of %v)",
				i, e.ID, e.Severity, validSeverities)
		}
	}
	return &f, nil
}

// ResolveResult bundles the rows Resolve produced and any matching
// warnings (matches[] qnames that found no code node).
type ResolveResult struct {
	Nodes    []types.Node
	Edges    []types.Edge
	Warnings []ResolveWarning
}

// ResolveWarning records a matches[] entry that didn't find a target
// in the parsed graph. Surfaced verbatim so editors of the YAML can
// spot stale references (renamed functions, moved fields).
type ResolveWarning struct {
	PatternID string
	TargetRef string
	Reason    string
}

// Resolve builds NodeSecurityPattern nodes + EdgeHasSecurityPattern
// edges to fold into the main graph.
//
//   - One NodeSecurityPattern per entry. QualifiedName = ID, Name =
//     Name, SubKind = Category. DocComment merges Description and
//     Remediation (blank line between them when both are present).
//   - One EdgeHasSecurityPattern per (matched qname, pattern) pair.
//     Direction = at-risk code symbol → pattern node so the natural
//     query "what risks does X exhibit?" is a single FK lookup.
//
// O(P + N) overall: a single qname → id index built from codeNodes
// drives the matches[] hot loop. Missing references emit a
// ResolveWarning rather than failing — security annotation is
// strictly additive metadata.
func Resolve(f *File, codeNodes []types.Node, yamlPath string) ResolveResult {
	out := ResolveResult{}
	if f == nil || len(f.SecurityPatterns) == 0 {
		return out
	}
	byQname := make(map[string]string, len(codeNodes))
	for _, n := range codeNodes {
		if n.QualifiedName == "" {
			continue
		}
		byQname[n.QualifiedName] = n.ID
	}
	for _, e := range f.SecurityPatterns {
		patternNode := types.Node{
			ID:            patternNodeID(e.ID),
			Type:          types.NodeSecurityPattern,
			Name:          e.Name,
			QualifiedName: e.ID,
			FilePath:      yamlPath,
			StartLine:     1,
			Language:      "security",
			Visibility:    "public",
			Signature:     buildSignature(e),
			DocComment:    buildDocComment(e),
			SubKind:       e.Category,
			Confidence:    types.ConfExtracted,
		}
		out.Nodes = append(out.Nodes, patternNode)

		for _, target := range e.Matches {
			target = strings.TrimSpace(target)
			if target == "" {
				continue
			}
			srcID, ok := byQname[target]
			if !ok {
				out.Warnings = append(out.Warnings, ResolveWarning{
					PatternID: e.ID,
					TargetRef: target,
					Reason:    "no code node found with this qualified_name",
				})
				continue
			}
			out.Edges = append(out.Edges, types.Edge{
				Src:        srcID,
				Dst:        patternNode.ID,
				Type:       types.EdgeHasSecurityPattern,
				FilePath:   yamlPath,
				Confidence: types.ConfExtracted,
			})
		}
	}
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
		if out.Warnings[i].PatternID != out.Warnings[j].PatternID {
			return out.Warnings[i].PatternID < out.Warnings[j].PatternID
		}
		return out.Warnings[i].TargetRef < out.Warnings[j].TargetRef
	})
	return out
}

// patternNodeID namespaces the SecurityPattern node ID separately
// from parser-derived and policy-derived IDs so the three node-id
// families can coexist in nodes.id without collision risk.
func patternNodeID(id string) string {
	return "security:" + id
}

// buildSignature renders a one-line tag string for the SecurityPattern
// node's Signature field. Order is fixed (category → severity →
// matches count) so FTS snippets read consistently.
func buildSignature(e Entry) string {
	parts := make([]string, 0, 3)
	if e.Category != "" {
		parts = append(parts, "category="+e.Category)
	}
	if e.Severity != "" {
		parts = append(parts, "severity="+e.Severity)
	}
	if n := len(e.Matches); n > 0 {
		parts = append(parts, fmt.Sprintf("matches=%d", n))
	}
	return strings.Join(parts, " ")
}

// buildDocComment combines description + remediation into the single
// doc_comment field so MCP/eval consumers don't need a separate
// remediation accessor. A blank line separates the two when both
// are present — same convention git commit bodies use to delimit
// subject + body.
func buildDocComment(e Entry) string {
	switch {
	case e.Description == "" && e.Remediation == "":
		return ""
	case e.Remediation == "":
		return e.Description
	case e.Description == "":
		return "Remediation: " + e.Remediation
	default:
		return e.Description + "\n\nRemediation: " + e.Remediation
	}
}
