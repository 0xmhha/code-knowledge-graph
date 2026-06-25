// Package buildpipe — language_runners.go contains the per-language Pass 1 +
// Pass 2 driver functions (runGoPipeline, runTSPipeline, runSolPipeline) and
// their immediate helpers (stampFilePath, convertABI). Extracted from
// pipeline.go in G4 to keep the orchestrator file under the soft 400-line cap.
// Pure file move — no behavior change.
package buildpipe

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	"github.com/0xmhha/code-knowledge-graph/internal/detect"
	"github.com/0xmhha/code-knowledge-graph/internal/link"
	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	protop "github.com/0xmhha/code-knowledge-graph/internal/parse/proto"
	solp "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// parseWorkers caps the parallel parser goroutine count. Capped at 8 to keep
// disk IO predictable on laptops with NVMe + many cores; larger values gain
// little because parse work is mostly CPU-bound after read.
func parseWorkers() int {
	n := runtime.GOMAXPROCS(0)
	if n > 8 {
		return 8
	}
	if n < 1 {
		return 1
	}
	return n
}

// parseConcurrent runs parseOne across files in parallel and streams the
// per-file ParseResults through a channel to a single collector goroutine.
// This pattern is intentional dogfood material for ckg's own concurrency
// detector: parseConcurrent emits real `go` statements, sync.WaitGroup
// usage, channel sends/recvs, and a sync.Mutex guarding the error counter.
// When ckg analyses its own source, the resulting graph contains non-zero
// G4 (concurrency) edges that exercise spawns / sends_to / recvs_from /
// acquires_lock paths end-to-end.
//
// "Many parsers, single writer" is preserved — the parser side fans out
// freely, but the consumer side is one goroutine that drains the result
// channel before passing the slice to Pass 2 Resolve. This mirrors the
// SQLite single-writer constraint downstream and keeps Pass 2 deterministic.
//
// Determinism: results are sorted by file path before return, so Pass 2
// Resolve sees the same iteration order as the previous sequential code.
func parseConcurrent(
	srcRoot string,
	files []string,
	log *slog.Logger,
	parseOne func(full string, src []byte) (*parse.ParseResult, error),
	logTag string,
) ([]*parse.ParseResult, int) {
	workers := parseWorkers()
	resultCh := make(chan *parse.ParseResult, workers)
	sem := make(chan struct{}, workers)

	var wg sync.WaitGroup
	var errMu sync.Mutex
	errs := 0

	// Single collector goroutine — owns the result slice and is the only
	// writer to it (mirrors the persist single-writer contract). It runs
	// concurrently with the spawn loop so resultCh is drained while parsing:
	// required because the loop now blocks acquiring `sem`, and a loop that
	// filled resultCh with no live drainer would deadlock.
	collected := make(chan []*parse.ParseResult, 1)
	go func() {
		var out []*parse.ParseResult
		for r := range resultCh {
			out = append(out, r)
		}
		collected <- out
	}()

	for _, rel := range files {
		// Acquire the worker slot in the parent loop BEFORE spawning, so the
		// number of live goroutines is bounded by `workers`. The old form
		// (acquire inside the goroutine) spawned one goroutine per file up
		// front — hundreds of thousands on a large repo — each parked on the
		// semaphore, wasting scheduler memory.
		sem <- struct{}{}
		wg.Add(1)
		go func(rel string) {
			defer wg.Done()
			defer func() { <-sem }()

			full := filepath.Join(srcRoot, rel)
			src, err := os.ReadFile(full)
			if err != nil {
				log.Warn(logTag+" read", "path", full, "err", err)
				errMu.Lock()
				errs++
				errMu.Unlock()
				return
			}
			r, err := parseOne(full, src)
			if err != nil {
				log.Warn(logTag+" parse", "path", full, "err", err)
				errMu.Lock()
				errs++
				errMu.Unlock()
				return
			}
			stampFilePath(r)
			resultCh <- r
		}(rel)
	}

	// All slots spawned; wait for parsers, close resultCh so the collector
	// terminates, then take the collected slice.
	wg.Wait()
	close(resultCh)
	results := <-collected

	sort.Slice(results, func(i, j int) bool { return results[i].Path < results[j].Path })
	return results, errs
}

// collectPendingRefs flattens per-file ParseResults into PendingRefRow records
// (G6 v3, schema 1.5) with file_path stamped from each ParseResult.Path.
// Called by every language pipeline AFTER stampFilePath but BEFORE Resolve so
// the row data carries the rel-path Resolve will consume into edges.
func collectPendingRefs(results []*parse.ParseResult) []persist.PendingRefRow {
	var out []persist.PendingRefRow
	for _, r := range results {
		rel := filepath.ToSlash(r.Path)
		if rel == "" {
			continue
		}
		for _, pr := range r.Pending {
			out = append(out, persist.PendingRefRow{
				FilePath:     rel,
				SrcID:        pr.SrcID,
				TargetQName:  pr.TargetQName,
				EdgeType:     string(pr.EdgeType),
				Line:         pr.Line,
				HintFile:     pr.HintFile,
				DispatchKind: pr.DispatchKind,
			})
		}
	}
	return out
}

// runGoPipeline drives Pass 1 (per-file ParseFile) + Pass 2 (Resolve) for Go.
// Returns the resolved graph, the per-Function/Method body field-touch map
// used by W-A cross-function lock propagation, count of files that failed
// to read or parse, and any fatal Resolve error.
//
// B1 (Wave 5): loads each module with full go/types info via detect.GoPackages
// and registers the result on the parser via SetPackages. This enables the
// concurrency pass to resolve sync.Mutex receivers via *types.Object identity
// (false-positive guard, spec §2 R2.1). The packages.Load is ~10x slower than
// the file-list-only mode used by detect.GoFiles, but is amortised against
// the per-file parse pass below.
//
// Failure of the typed load is a soft fallback — the parser will still work
// in AST-only mode (concurrency edges become INFERRED). Logs the warning so
// operators can investigate without breaking the build.
//
// funcFieldTouches (W-A return value) is nil when the typed-load fallback
// kicks in — AST-only mode has no reliable way to map field references to
// Field node IDs (parser.go comment). The propagator then no-ops, which
// matches the existing INFERRED-confidence semantics of AST-only builds.
func runGoPipeline(srcRoot string, files []string, log *slog.Logger) (*parse.ResolvedGraph, []persist.PendingRefRow, map[string]map[string]struct{}, int, error) {
	p := gop.New(srcRoot)
	if pkgs, err := detect.GoPackages(srcRoot); err != nil {
		log.Warn("Go packages typed-load failed; concurrency pass falls back to AST-only", "err", err)
	} else {
		p.SetPackages(pkgs)
	}
	results, errs := parseConcurrent(srcRoot, files, log, p.ParseFile, "go")
	pending := collectPendingRefs(results)
	rg, err := p.Resolve(results)
	// P0: emit implements / extends edges from go/types satisfaction. Runs
	// post-Resolve because (a) it needs the union of Struct/Interface IDs
	// across all files and (b) only the typed-load path has the package
	// scopes to query. Soft no-op when Resolve failed or pkgs are unavailable
	// (AST-only fallback) — preserves existing buildpipe error semantics.
	if err == nil && rg != nil {
		implEdges := gop.EmitImplementsEdges(p.Pkgs(), rg.Nodes)
		rg.Edges = append(rg.Edges, implEdges...)
		log.Debug("implements emitted", "count", len(implEdges))
		// Track C P0: uses_type post-pass. Same wiring rationale as
		// implements above. Cross-package types without a node (stdlib,
		// vendored deps) become PendingRefs — appended to the pending
		// slice so the cold path persists them via InsertPendingRefs (q4=A).
		usesEdges, usesPending := gop.EmitUsesTypeEdges(p.Pkgs(), rg.Nodes)
		rg.Edges = append(rg.Edges, usesEdges...)
		log.Debug("uses_type emitted", "edges", len(usesEdges), "pending", len(usesPending))
		// Anchor the pending refs to the file the SRC node was defined in,
		// so the partial-rebuild path's PendingRefsByFilePath query reaches
		// them. Build the SRC-ID → file_path lookup once.
		if len(usesPending) > 0 {
			srcFile := make(map[string]string, len(rg.Nodes))
			for _, n := range rg.Nodes {
				switch n.Type {
				case types.NodeFunction, types.NodeMethod, types.NodeStruct:
					if _, exists := srcFile[n.ID]; !exists {
						srcFile[n.ID] = n.FilePath
					}
				}
			}
			for _, pr := range usesPending {
				rel := srcFile[pr.SrcID]
				if rel == "" {
					continue // SRC outside this build — skip
				}
				pending = append(pending, persist.PendingRefRow{
					FilePath:    rel,
					SrcID:       pr.SrcID,
					TargetQName: pr.TargetQName,
					EdgeType:    string(pr.EdgeType),
					Line:        pr.Line,
					HintFile:    pr.HintFile,
				})
			}
		}
		// Track C P1c: instantiates post-pass. No pending_refs path —
		// composite-literal targets without a node in the graph are silently
		// dropped (the noise floor would be higher than the value).
		instEdges := gop.EmitInstantiatesEdges(p.Pkgs(), rg.Nodes)
		rg.Edges = append(rg.Edges, instEdges...)
		log.Debug("instantiates emitted", "count", len(instEdges))

		// Defect C: promoted-method nodes for embedded in-module types.
		promNodes, promEdges := gop.EmitPromotedMethods(p.Pkgs(), rg.Nodes)
		rg.Nodes = append(rg.Nodes, promNodes...)
		rg.Edges = append(rg.Edges, promEdges...)
		log.Debug("promoted methods emitted", "nodes", len(promNodes))

		// Defect E: writes_field edges (function -> struct field it assigns).
		wfEdges := gop.EmitFieldWriteEdges(p.Pkgs(), rg.Nodes)
		rg.Edges = append(rg.Edges, wfEdges...)
		log.Debug("writes_field emitted", "count", len(wfEdges))
	}
	return rg, pending, p.FuncFieldTouches(), errs, err
}

// stampFilePath populates Edge.FilePath for every per-file edge that lacks
// one, drawing from the ParseResult.Path the parser already recorded.
// Required by the A3 incremental cache: EdgesByFilePath reloads cached
// edges by file_path, and the parsers historically left it blank because
// the V0 schema didn't surface the field. Stamping is idempotent — pre-set
// FilePaths (e.g. on edges with line numbers) are preserved.
//
// Stamping per-file edges is safe: an edge emitted while parsing file X
// belongs to X by construction. Cross-file edges come from Pass 2 (Resolve),
// not per-file ParseFile, so this stamping doesn't touch them.
func stampFilePath(r *parse.ParseResult) {
	lineQualifyDuplicateCanonicalIDs(r.Nodes)
	rel := filepath.ToSlash(r.Path)
	if rel == "" {
		return
	}
	for i := range r.Edges {
		if r.Edges[i].FilePath == "" {
			r.Edges[i].FilePath = rel
		}
	}
}

// lineQualifyDuplicateCanonicalIDs appends "@<startLine>" to any canonical_id
// shared by more than one node in the same file (B3), so same-file same-name
// symbols — a minified-JS `function t(){}` repeated dozens of times, or several
// Go `init` functions in one file — get distinct ids. Single-occurrence ids are
// left untouched, so normal code keeps a stable, line-independent canonical id;
// only genuine intra-file collisions pay the line-dependence cost. Runs per
// ParseResult (one file) at the single post-ParseFile chokepoint.
func lineQualifyDuplicateCanonicalIDs(nodes []types.Node) {
	if len(nodes) < 2 {
		return
	}
	counts := make(map[string]int, len(nodes))
	for _, n := range nodes {
		if n.CanonicalID != "" {
			counts[n.CanonicalID]++
		}
	}
	for i := range nodes {
		if cid := nodes[i].CanonicalID; cid != "" && counts[cid] > 1 {
			nodes[i].CanonicalID = fmt.Sprintf("%s@%d", cid, nodes[i].StartLine)
		}
	}
}

// runTSPipeline drives Pass 1 + Pass 2 for TypeScript / JavaScript.
// Returns the resolved graph, count of files that failed to read or parse,
// and any fatal Resolve error. Mirrors runGoPipeline.
func runTSPipeline(srcRoot string, files []string, log *slog.Logger) (*parse.ResolvedGraph, []persist.PendingRefRow, int, error) {
	p := tsp.New(srcRoot)
	results, errs := parseConcurrent(srcRoot, files, log, p.ParseFile, "ts")
	pending := collectPendingRefs(results)
	rg, err := p.Resolve(results)
	return rg, pending, errs, err
}

// runSolPipeline drives Pass 1 + Pass 2 for Solidity. Returns the parser
// instance so callers can read the accumulated ABI for cross-language linking.
func runSolPipeline(srcRoot string, files []string, log *slog.Logger) (*parse.ResolvedGraph, []persist.PendingRefRow, int, *solp.Parser, error) {
	p := solp.New(srcRoot)
	results, errs := parseConcurrent(srcRoot, files, log, p.ParseFile, "sol")
	pending := collectPendingRefs(results)
	rg, err := p.Resolve(results)
	return rg, pending, errs, p, err
}

// runProtoPipeline drives Pass 1 + Pass 2 for `.proto` files (schema 1.9 W3a).
// Mirrors runTSPipeline — proto has no cross-language ABI to surface back to
// the caller, so the signature stays minimal.
func runProtoPipeline(srcRoot string, files []string, log *slog.Logger) (*parse.ResolvedGraph, []persist.PendingRefRow, int, error) {
	p := protop.New(srcRoot)
	results, errs := parseConcurrent(srcRoot, files, log, p.ParseFile, "proto")
	pending := collectPendingRefs(results)
	rg, err := p.Resolve(results)
	return rg, pending, errs, err
}

// convertABI bridges solidity.ABISig (parser output) and link.ABISig (linker
// input) to keep the link package free of any per-language parser imports.
func convertABI(in map[string][]solp.ABISig) map[string][]link.ABISig {
	out := make(map[string][]link.ABISig, len(in))
	for k, v := range in {
		converted := make([]link.ABISig, len(v))
		for i, s := range v {
			converted[i] = link.ABISig{
				ContractName: s.ContractName,
				FunctionName: s.FunctionName,
				ParamTypes:   s.ParamTypes,
			}
		}
		out[k] = converted
	}
	return out
}
