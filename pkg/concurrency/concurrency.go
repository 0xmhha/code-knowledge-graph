package concurrency

import (
	"fmt"
	"sort"

	"github.com/0xmhha/code-knowledge-graph/pkg/store"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// ConcurrencyEdgeTypes is the contract's five concurrency edge types
// (R1' 00 C1, S1). releases_lock is intentionally excluded — see doc.go.
var ConcurrencyEdgeTypes = []string{
	string(types.EdgeSpawns),
	string(types.EdgeSendsTo),
	string(types.EdgeRecvsFrom),
	string(types.EdgeAcquiresLock),
	string(types.EdgeAccessedUnderLock),
}

// DepthCap bounds BFS depth, mirroring pkg/impact.
const DepthCap = 5

// defaultDepth is used when Options.Depth is unset (zero).
const defaultDepth = 2

// Options bundles tunable knobs. The zero value resolves to documented
// defaults (Depth=2, MaxTotal=0=unbounded).
type Options struct {
	Depth    int // clamped to [1, DepthCap]; 0 -> defaultDepth
	MaxTotal int // 0 = unbounded; caps the returned Modules slice
}

// Module is one symbol reachable from the seed via a concurrency edge.
type Module struct {
	ID        string         `json:"id"`
	Type      types.NodeType `json:"type"`
	Name      string         `json:"name"`
	Qname     string         `json:"qname"`
	FilePath  string         `json:"file_path"`
	StartLine int            `json:"start_line"`
	Citation  string         `json:"citation,omitempty"` // "file:line" when both present
	Direction string         `json:"direction"`          // affects | affected_by | both
}

// Result is the payload cks's concurrency_impact tool consumes.
type Result struct {
	Seed     string         `json:"seed"`
	NotFound bool           `json:"not_found"`
	Depth    int            `json:"depth"`
	Modules  []Module       `json:"modules"`
	Edges    [][]any        `json:"edges"` // [src, dst, type, line] tuples, sorted
	Totals   map[string]any `json:"totals"`
}

// Analyze returns the concurrency blast radius of symbol: every module that
// affects or is affected by it via the five ConcurrencyEdgeTypes, BFS to
// depth in both directions, backed by store.Reader.NeighborhoodByQname.
//
// Deterministic: modules are sorted by qname (tiebreak id), edges by
// (type, src, dst, line), so cks's prompt cache stays stable.
func Analyze(r store.Reader, symbol string, opt Options) (Result, error) {
	d := opt.Depth
	if d == 0 {
		d = defaultDepth
	}
	if d < 1 {
		d = 1
	}
	if d > DepthCap {
		d = DepthCap
	}

	res := Result{Seed: symbol, Depth: d, Modules: []Module{}, Edges: [][]any{}}

	roots, err := r.FindSymbol(symbol, true, store.FindSymbolOptions{})
	if err != nil {
		return Result{}, err
	}
	if len(roots) == 0 {
		res.NotFound = true
		res.Totals = map[string]any{"modules": 0, "edges": 0}
		return res, nil
	}
	rootIDs := make(map[string]bool, len(roots))
	for _, n := range roots {
		rootIDs[n.ID] = true
	}

	fwdN, fwdE, err := r.NeighborhoodByQname(symbol, d, false, ConcurrencyEdgeTypes...)
	if err != nil {
		return Result{}, err
	}
	revN, revE, err := r.NeighborhoodByQname(symbol, d, true, ConcurrencyEdgeTypes...)
	if err != nil {
		return Result{}, err
	}

	dir := map[string]string{}
	nodeByID := map[string]types.Node{}
	for _, n := range fwdN {
		if rootIDs[n.ID] {
			continue
		}
		nodeByID[n.ID] = n
		dir[n.ID] = "affected_by"
	}
	for _, n := range revN {
		if rootIDs[n.ID] {
			continue
		}
		nodeByID[n.ID] = n
		if dir[n.ID] == "affected_by" {
			dir[n.ID] = "both"
		} else {
			dir[n.ID] = "affects"
		}
	}

	modules := make([]Module, 0, len(nodeByID))
	for id, n := range nodeByID {
		m := Module{
			ID:        id,
			Type:      n.Type,
			Name:      n.Name,
			Qname:     n.QualifiedName,
			FilePath:  n.FilePath,
			StartLine: n.StartLine,
			Direction: dir[id],
		}
		if n.FilePath != "" && n.StartLine > 0 {
			m.Citation = fmt.Sprintf("%s:%d", n.FilePath, n.StartLine)
		}
		modules = append(modules, m)
	}
	sort.Slice(modules, func(i, j int) bool {
		if modules[i].Qname != modules[j].Qname {
			return modules[i].Qname < modules[j].Qname
		}
		return modules[i].ID < modules[j].ID
	})
	if opt.MaxTotal > 0 && len(modules) > opt.MaxTotal {
		modules = modules[:opt.MaxTotal]
	}

	type ekey struct {
		t, s, dst string
		line      int
	}
	eseen := map[ekey]bool{}
	edges := make([][]any, 0, len(fwdE)+len(revE))
	for _, es := range [][]types.Edge{fwdE, revE} {
		for _, e := range es {
			k := ekey{string(e.Type), e.Src, e.Dst, e.Line}
			if eseen[k] {
				continue
			}
			eseen[k] = true
			edges = append(edges, []any{e.Src, e.Dst, string(e.Type), e.Line})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		ti, tj := edges[i][2].(string), edges[j][2].(string)
		if ti != tj {
			return ti < tj
		}
		si, sj := edges[i][0].(string), edges[j][0].(string)
		if si != sj {
			return si < sj
		}
		di, dj := edges[i][1].(string), edges[j][1].(string)
		if di != dj {
			return di < dj
		}
		return edges[i][3].(int) < edges[j][3].(int)
	})

	res.Modules = modules
	res.Edges = edges
	res.Totals = map[string]any{"modules": len(modules), "edges": len(edges)}
	return res, nil
}
