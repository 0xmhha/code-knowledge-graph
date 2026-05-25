package retrieval

import (
	"fmt"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Result is the outcome of running one Fixture against a Reader.
type Result struct {
	Fixture Fixture
	Got     []string // unique qualified_names returned by the probe
	Score   Score
	// PassRecall / PassPrecision report whether the per-fixture
	// thresholds were met. Aggregator combines them into the overall
	// gate result.
	PassRecall    bool
	PassPrecision bool
}

// Pass returns true when both threshold gates passed (a missing gate
// passes by default because the zero value is 0.0).
func (r Result) Pass() bool {
	return r.PassRecall && r.PassPrecision
}

// Run executes a single fixture against the given Reader and produces
// a scored Result. A dispatch error (unknown tool, missing arg) is
// returned separately from a low-score Result — the caller distinguishes
// "ran and scored badly" from "could not run at all".
func Run(reader persist.StoreReader, f Fixture) (Result, error) {
	got, err := dispatchProbe(reader, f.Probe)
	if err != nil {
		return Result{}, fmt.Errorf("fixture %s: %w", f.ID, err)
	}
	score := ComputeScore(f.Expected.Symbols, got)
	return Result{
		Fixture:       f,
		Got:           got,
		Score:         score,
		PassRecall:    score.Recall >= f.Scoring.RecallMin,
		PassPrecision: score.Precision >= f.Scoring.PrecisionMin,
	}, nil
}

// RunAll runs every fixture sequentially and returns one Result per
// fixture in input order. Sequential rather than parallel because the
// per-probe cost is microseconds on the synthetic fixture — parallelism
// would add ordering noise to the JSON output for no real speedup.
func RunAll(reader persist.StoreReader, fixtures []Fixture) ([]Result, error) {
	out := make([]Result, 0, len(fixtures))
	for _, f := range fixtures {
		r, err := Run(reader, f)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

// dispatchProbe routes a Probe to the right Reader method and returns
// the unique qualified_names of the result set.
//
// Adding a new tool: add a case here, document the args it consumes,
// and write a fixture or two under eval/retrieval/. The Fixture schema
// itself does not need to change — args stays map[string]any.
func dispatchProbe(reader persist.StoreReader, p Probe) ([]string, error) {
	switch p.Tool {
	case "find_callers":
		return runFindCallers(reader, p.Args)
	case "find_callees":
		return runFindCallees(reader, p.Args)
	case "find_symbol":
		return runFindSymbol(reader, p.Args)
	case "search_text":
		return runSearchText(reader, p.Args)
	default:
		return nil, fmt.Errorf("unknown probe tool %q (supported: find_callers, find_callees, find_symbol, search_text)", p.Tool)
	}
}

// callEdgeTypes mirrors internal/mcp/tools.go — find_callers /
// find_callees filter to actual call edges, not containment/definition.
// Duplicated here rather than imported to avoid a cyclic dependency
// (internal/mcp depends on internal/persist; the retrieval layer also
// depends on internal/persist, but pulling in mcp would create a
// retrieval ← mcp ← persist + retrieval ← persist diamond).
var callEdgeTypes = []string{"calls", "invokes"}

func runFindCallers(r persist.StoreReader, args map[string]any) ([]string, error) {
	qname, err := requireString(args, "qname")
	if err != nil {
		return nil, err
	}
	depth, err := requireInt(args, "depth", 2)
	if err != nil {
		return nil, err
	}
	nodes, _, err := r.NeighborhoodByQname(qname, depth, true /*reverse*/, callEdgeTypes...)
	if err != nil {
		return nil, err
	}
	return uniqueQnames(nodes, qname), nil
}

func runFindCallees(r persist.StoreReader, args map[string]any) ([]string, error) {
	qname, err := requireString(args, "qname")
	if err != nil {
		return nil, err
	}
	depth, err := requireInt(args, "depth", 2)
	if err != nil {
		return nil, err
	}
	nodes, _, err := r.NeighborhoodByQname(qname, depth, false, callEdgeTypes...)
	if err != nil {
		return nil, err
	}
	return uniqueQnames(nodes, qname), nil
}

func runFindSymbol(r persist.StoreReader, args map[string]any) ([]string, error) {
	name, err := requireString(args, "name")
	if err != nil {
		return nil, err
	}
	exact, _ := args["exact"].(bool) // default false → suffix match allowed
	opts := persist.FindSymbolOptions{}
	if lang, ok := args["language"].(string); ok {
		opts.Language = lang
	}
	if kindsRaw, ok := args["kinds"].([]any); ok {
		for _, k := range kindsRaw {
			if s, ok := k.(string); ok {
				opts.Kinds = append(opts.Kinds, types.NodeType(s))
			}
		}
	}
	nodes, err := r.FindSymbol(name, exact, opts)
	if err != nil {
		return nil, err
	}
	return uniqueQnames(nodes, ""), nil
}

func runSearchText(r persist.StoreReader, args map[string]any) ([]string, error) {
	q, err := requireString(args, "query")
	if err != nil {
		return nil, err
	}
	limit, err := requireInt(args, "top_k", 20)
	if err != nil {
		return nil, err
	}
	opts := persist.SearchFTSOptions{}
	if lang, ok := args["language"].(string); ok {
		opts.Language = lang
	}
	if mode, ok := args["mode"].(string); ok {
		opts.Mode = mode
	}
	if kindsRaw, ok := args["node_kinds"].([]any); ok {
		for _, k := range kindsRaw {
			if s, ok := k.(string); ok {
				opts.NodeKinds = append(opts.NodeKinds, types.NodeType(s))
			}
		}
	}
	nodes, err := r.SearchWithOpts(q, limit, opts)
	if err != nil {
		return nil, err
	}
	return uniqueQnames(nodes, ""), nil
}

// uniqueQnames extracts qualified_name from nodes, dropping duplicates
// and (optionally) the seed itself. The seed exclusion matters for
// find_callers / find_callees — the seed is always in the BFS result
// but is not a "caller of itself" or "callee of itself".
func uniqueQnames(nodes []types.Node, excludeQname string) []string {
	seen := make(map[string]bool, len(nodes))
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.QualifiedName == "" {
			continue
		}
		if n.QualifiedName == excludeQname {
			continue
		}
		if seen[n.QualifiedName] {
			continue
		}
		seen[n.QualifiedName] = true
		out = append(out, n.QualifiedName)
	}
	return out
}

func requireString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required arg %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("arg %q must be a string, got %T", key, v)
	}
	return s, nil
}

// requireInt accepts either YAML int (decoded as int) or float64
// (when YAML carried a number expression). default_ is returned only
// when the key is absent — explicit zero is honoured.
func requireInt(args map[string]any, key string, defaultValue int) (int, error) {
	v, ok := args[key]
	if !ok {
		return defaultValue, nil
	}
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		return int(n), nil
	default:
		return 0, fmt.Errorf("arg %q must be a number, got %T", key, v)
	}
}
