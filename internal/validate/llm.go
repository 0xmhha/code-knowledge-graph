package validate

import (
	"context"
	"sort"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// LLMValidator is the LLM-as-judge stage. It surfaces findings that the
// deterministic SchemaValidator cannot express — e.g. "this calls edge
// looks like it should be a defer/recover relationship given the source
// snippet" or "interface X is heavily imported but has zero implements
// edges, did the analyzer miss something?".
//
// V0 (this cycle) is dry-run only: it samples suspicious edges/nodes
// from the graph and emits each candidate as a Prompt encoded into an
// Info issue. The operator can copy the Question + Citations into any
// LLM chat and apply the answer manually. Real API wiring is V1+ — the
// Endpoint/Model fields are reserved for that change so this struct
// does not have to be re-shaped later.
//
// The decision to default to DryRun=true (per dogfood plan instruction
// ④) means `--llm` is immediately useful: an operator running ckg
// validate gets actionable prompts without any external dependency or
// network access.
type LLMValidator struct {
	// DryRun, when true (the default), causes Validate to emit prompts
	// as Info issues and never make a network call. When false, it
	// returns a single Error issue documenting that real wiring is
	// pending — Validate still does NOT touch the network in V0.
	DryRun bool
	// MaxPrompts caps the number of prompts emitted per Validate call.
	// Default 10 (set by NewLLMValidator). Higher values multiply
	// future LLM cost linearly.
	MaxPrompts int
	// Endpoint is the LLM API URL — reserved for V1 wiring; unused in V0.
	Endpoint string
	// Model is the LLM model identifier — reserved for V1 wiring;
	// unused in V0.
	Model string
}

// NewLLMValidator returns a dry-run LLMValidator with MaxPrompts=10.
// The defaults are chosen so `--llm` produces a useful, bounded prompt
// list out of the box without any operator configuration.
func NewLLMValidator() *LLMValidator {
	return &LLMValidator{DryRun: true, MaxPrompts: 10}
}

// Name returns the validator identifier.
func (v *LLMValidator) Name() string { return "llm" }

// Validate samples suspicious edges/nodes and produces prompts. In
// DryRun mode (default) every prompt becomes an Info issue with code
// "llm-prompt-dry-run". In non-DryRun mode a single Error issue is
// returned because real LLM wiring is not in this build — Validate
// never opens a network connection regardless of mode.
func (v *LLMValidator) Validate(ctx context.Context, g *graph.Graph, store persist.StoreReader) (*Report, error) {
	report := &Report{Validator: v.Name()}

	if !v.DryRun {
		// Explicit Error so CI scripts that flip --no-llm-dry-run by
		// mistake see a loud signal rather than a silent no-op. We do
		// NOT fall through to dry-run: the operator asked for "real",
		// the only honest answer is "not yet".
		report.Issues = append(report.Issues, Issue{
			Severity: SeverityError,
			Code:     "llm-not-yet-wired",
			Message:  "LLMValidator non-dry-run mode is not implemented; real LLM wiring is V1+",
		})
		return report, nil
	}

	max := v.MaxPrompts
	if max <= 0 {
		max = 10
	}
	prompts := SampleSuspiciousFromGraph(g, store, max)
	for _, p := range prompts {
		report.Issues = append(report.Issues, promptToIssue(p))
	}

	_ = ctx
	return report, nil
}

// promptToIssue encodes a Prompt as an Info issue. The Question is the
// human-facing message; Subject doubles as NodeID/EdgeKey so existing
// CLI formatters (which print the trailing bracket) surface it without
// changes. We pick NodeID vs EdgeKey based on Task — sparse-subgraph is
// node-scoped, edge-plausibility is edge-scoped.
func promptToIssue(p Prompt) Issue {
	iss := Issue{
		Severity: SeverityInfo,
		Code:     "llm-prompt-dry-run",
		Message:  p.Question,
	}
	if len(p.Citations) > 0 {
		iss.FilePath = p.Citations[0].File
	}
	switch p.Task {
	case "edge-plausibility":
		iss.EdgeKey = p.Subject
	default:
		iss.NodeID = p.Subject
	}
	return iss
}

// SampleSuspiciousFromGraph runs every V0 sampler and concatenates
// their outputs, capped at maxPrompts globally. Sampler order is fixed
// (edge-plausibility first, then sparse-subgraph) so prompt lists are
// reproducible across runs given the same graph.
//
// V1+ TODO: add sampleCitationFreshness — needs store.GetBlob to fetch
// the actual source text and ask the LLM whether the file:line really
// declares the qname claimed by the node. Skipped here because the V0
// dry-run path has no way to surface the snippet to the operator
// without dumping arbitrarily large blobs into the issue list; we
// would rather defer than ship a half-useful sampler.
func SampleSuspiciousFromGraph(g *graph.Graph, store persist.StoreReader, maxPrompts int) []Prompt {
	if g == nil || maxPrompts <= 0 {
		return nil
	}
	out := make([]Prompt, 0, maxPrompts)
	out = append(out, sampleEdgePlausibility(g, maxPrompts)...)
	if len(out) >= maxPrompts {
		return out[:maxPrompts]
	}
	out = append(out, sampleSparseImplements(g, maxPrompts-len(out))...)
	if len(out) > maxPrompts {
		return out[:maxPrompts]
	}
	_ = store // V1+: citation-freshness sampler will use store.GetBlob.
	return out
}

// sampleEdgePlausibility picks edges with confidence=INFERRED — the
// signal the deterministic analyzer admits "I'm not sure". These are
// exactly the cases where a human or LLM judgment adds value over
// schema rules. We sort by EdgeKey before truncating so the output is
// deterministic across runs (no random sampling in V0; if the user
// wants more variety they raise MaxPrompts).
func sampleEdgePlausibility(g *graph.Graph, n int) []Prompt {
	if n <= 0 {
		return nil
	}
	byID := make(map[string]*types.Node, len(g.Nodes))
	for i := range g.Nodes {
		byID[g.Nodes[i].ID] = &g.Nodes[i]
	}
	type cand struct {
		key string
		p   Prompt
	}
	cands := make([]cand, 0, 64)
	for _, e := range g.Edges {
		if e.Confidence != types.ConfInferred {
			continue
		}
		src, srcOK := byID[e.Src]
		dst, dstOK := byID[e.Dst]
		if !srcOK || !dstOK {
			// Dangling — already covered by SchemaValidator; skip to
			// avoid duplicate findings.
			continue
		}
		// Need a citation for the prompt to be useful. Line==0 is
		// allowed (some analyzers know the file but not the line);
		// the operator can still open the file and search.
		if e.FilePath == "" {
			continue
		}
		key := edgeKey(string(e.Type), e.Src, e.Dst, e.Line)
		subject := string(e.Type) + ":src=" + src.QualifiedName + ":dst=" + dst.QualifiedName
		question := "Does the source at this location actually represent a " +
			string(e.Type) + " from " + src.Name + " to " + dst.Name + "?"
		cands = append(cands, cand{
			key: key,
			p: Prompt{
				Task:           "edge-plausibility",
				Subject:        subject,
				Citations:      []Citation{{File: e.FilePath, Line: e.Line}},
				Question:       question,
				ResponseSchema: `{"verdict":"valid|misclassified|unsure","explanation":"<=200 chars"}`,
			},
		})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].key < cands[j].key })
	if len(cands) > n {
		cands = cands[:n]
	}
	out := make([]Prompt, len(cands))
	for i, c := range cands {
		out[i] = c.p
	}
	return out
}

// sampleSparseImplements picks Interface nodes that have zero implements
// edges arriving at them yet appear as the dst of edges from ≥2 distinct
// files (any edge type). The signal: a heavily-referenced interface
// with no known implementor smells like the analyzer missed an
// implements rule. Sorted by qname for determinism.
//
// We use "any incoming edge from a distinct file" as the popularity
// signal rather than the narrower `imports` edges because the imports
// table in the current schema targets Import nodes (synthesised
// per-file), not the Interface itself — so an imports-only count
// would be near-zero for every interface even when the type is heavily
// used. Threshold=2 is intentionally loose: V0 wants a recall-oriented
// hint, not a precise accusation; the LLM judges precision.
func sampleSparseImplements(g *graph.Graph, n int) []Prompt {
	if n <= 0 {
		return nil
	}
	implementsByDst := make(map[string]int, 64)
	popularityByDst := make(map[string]map[string]struct{}, 64)
	for _, e := range g.Edges {
		if e.Type == types.EdgeImplements {
			implementsByDst[e.Dst]++
		}
		if e.FilePath == "" {
			continue
		}
		set, ok := popularityByDst[e.Dst]
		if !ok {
			set = make(map[string]struct{}, 4)
			popularityByDst[e.Dst] = set
		}
		set[e.FilePath] = struct{}{}
	}
	type cand struct {
		qname string
		p     Prompt
	}
	cands := make([]cand, 0, 32)
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.Type != types.NodeInterface {
			continue
		}
		if implementsByDst[n.ID] > 0 {
			continue
		}
		popularity := len(popularityByDst[n.ID])
		if popularity < 2 {
			continue
		}
		// Citation: the interface declaration itself.
		if n.FilePath == "" || n.StartLine <= 0 {
			continue
		}
		question := "Interface " + n.Name + " has 0 implements edges but is referenced by " +
			itoa(popularity) + " files. Did the analyzer miss an implementor?"
		cands = append(cands, cand{
			qname: n.QualifiedName,
			p: Prompt{
				Task:           "sparse-subgraph",
				Subject:        n.QualifiedName,
				Citations:      []Citation{{File: n.FilePath, Line: n.StartLine}},
				Question:       question,
				ResponseSchema: `{"verdict":"missed|truly-unimplemented|unsure","candidate_implementors":["..."]}`,
			},
		})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].qname < cands[j].qname })
	if len(cands) > n {
		cands = cands[:n]
	}
	out := make([]Prompt, len(cands))
	for i, c := range cands {
		out[i] = c.p
	}
	return out
}

// Compile-time check that *LLMValidator satisfies Validator.
var _ Validator = (*LLMValidator)(nil)
