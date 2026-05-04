// Package buildpipe — language_runners.go contains the per-language Pass 1 +
// Pass 2 driver functions (runGoPipeline, runTSPipeline, runSolPipeline) and
// their immediate helpers (stampFilePath, convertABI). Extracted from
// pipeline.go in G4 to keep the orchestrator file under the soft 400-line cap.
// Pure file move — no behavior change.
package buildpipe

import (
	"log/slog"
	"os"
	"path/filepath"

	"github.com/0xmhha/code-knowledge-graph/internal/detect"
	"github.com/0xmhha/code-knowledge-graph/internal/link"
	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	solp "github.com/0xmhha/code-knowledge-graph/internal/parse/solidity"
	tsp "github.com/0xmhha/code-knowledge-graph/internal/parse/typescript"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

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
				FilePath:    rel,
				SrcID:       pr.SrcID,
				TargetQName: pr.TargetQName,
				EdgeType:    string(pr.EdgeType),
				Line:        pr.Line,
				HintFile:    pr.HintFile,
			})
		}
	}
	return out
}

// runGoPipeline drives Pass 1 (per-file ParseFile) + Pass 2 (Resolve) for Go.
// Returns the resolved graph, count of files that failed to read or parse,
// and any fatal Resolve error.
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
func runGoPipeline(srcRoot string, files []string, log *slog.Logger) (*parse.ResolvedGraph, []persist.PendingRefRow, int, error) {
	p := gop.New(srcRoot)
	if pkgs, err := detect.GoPackages(srcRoot); err != nil {
		log.Warn("Go packages typed-load failed; concurrency pass falls back to AST-only", "err", err)
	} else {
		p.SetPackages(pkgs)
	}
	results := []*parse.ParseResult{}
	errs := 0
	for _, rel := range files {
		full := filepath.Join(srcRoot, rel)
		src, err := os.ReadFile(full)
		if err != nil {
			log.Warn("read file", "path", full, "err", err)
			errs++
			continue
		}
		r, err := p.ParseFile(full, src)
		if err != nil {
			log.Warn("parse file", "path", full, "err", err)
			errs++
			continue
		}
		stampFilePath(r)
		results = append(results, r)
	}
	pending := collectPendingRefs(results)
	rg, err := p.Resolve(results)
	return rg, pending, errs, err
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

// runTSPipeline drives Pass 1 + Pass 2 for TypeScript / JavaScript.
// Returns the resolved graph, count of files that failed to read or parse,
// and any fatal Resolve error. Mirrors runGoPipeline.
func runTSPipeline(srcRoot string, files []string, log *slog.Logger) (*parse.ResolvedGraph, []persist.PendingRefRow, int, error) {
	p := tsp.New(srcRoot)
	results := []*parse.ParseResult{}
	errs := 0
	for _, rel := range files {
		full := filepath.Join(srcRoot, rel)
		src, err := os.ReadFile(full)
		if err != nil {
			log.Warn("ts read", "path", full, "err", err)
			errs++
			continue
		}
		r, err := p.ParseFile(full, src)
		if err != nil {
			log.Warn("ts parse", "path", full, "err", err)
			errs++
			continue
		}
		stampFilePath(r)
		results = append(results, r)
	}
	pending := collectPendingRefs(results)
	rg, err := p.Resolve(results)
	return rg, pending, errs, err
}

// runSolPipeline drives Pass 1 + Pass 2 for Solidity. Returns the parser
// instance so callers can read the accumulated ABI for cross-language linking.
func runSolPipeline(srcRoot string, files []string, log *slog.Logger) (*parse.ResolvedGraph, []persist.PendingRefRow, int, *solp.Parser, error) {
	p := solp.New(srcRoot)
	results := []*parse.ParseResult{}
	errs := 0
	for _, rel := range files {
		full := filepath.Join(srcRoot, rel)
		src, err := os.ReadFile(full)
		if err != nil {
			log.Warn("sol read", "path", full, "err", err)
			errs++
			continue
		}
		r, err := p.ParseFile(full, src)
		if err != nil {
			log.Warn("sol parse", "path", full, "err", err)
			errs++
			continue
		}
		stampFilePath(r)
		results = append(results, r)
	}
	pending := collectPendingRefs(results)
	rg, err := p.Resolve(results)
	return rg, pending, errs, p, err
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

