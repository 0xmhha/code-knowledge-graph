// Package buildpipe orchestrates the full Pass 1..4 build (spec §4.7):
// detect → parse → resolve → graph build/validate → cluster → score → persist.
// V0 supports a full rebuild only — incremental updates are not wired here.
package buildpipe

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/0xmhha/code-knowledge-graph/internal/cluster"
	"github.com/0xmhha/code-knowledge-graph/internal/detect"
	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/parse"
	gop "github.com/0xmhha/code-knowledge-graph/internal/parse/golang"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/internal/score"
	"github.com/0xmhha/code-knowledge-graph/pkg/types"
)

// Options controls one ckg build invocation.
type Options struct {
	SrcRoot    string
	OutDir     string
	Languages  []string // {"auto"} | subset of {"go","ts","sol"}
	Logger     *slog.Logger
	CKGVersion string
}

// Run executes the full pipeline. Side effects: writes OutDir/graph.db
// and OutDir/manifest.json. Returns the persisted Manifest summary so the
// caller can print stats without re-reading SQLite.
func Run(opt Options) (persist.Manifest, error) {
	log := opt.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	if err := os.MkdirAll(opt.OutDir, 0o755); err != nil {
		return persist.Manifest{}, fmt.Errorf("mkdir out: %w", err)
	}

	// (1) detect
	files, err := detect.Walk(opt.SrcRoot)
	if err != nil {
		return persist.Manifest{}, fmt.Errorf("detect: %w", err)
	}
	log.Info("detected files", "go", len(files.Go), "ts", len(files.TS), "sol", len(files.Sol))

	// (2)+(3) parse + link, per language
	resolved := []*parse.ResolvedGraph{}
	parseErrs := 0
	if shouldRun("go", opt.Languages) && len(files.Go) > 0 {
		rg, n, err := runGoPipeline(opt.SrcRoot, files.Go, log)
		if err != nil {
			return persist.Manifest{}, fmt.Errorf("go pipeline: %w", err)
		}
		parseErrs += n
		resolved = append(resolved, rg)
	}
	// TS / Sol pipelines wired in Phase 5.

	// (4) graph build + validate
	g, err := graph.Build(resolved)
	if err != nil {
		return persist.Manifest{}, fmt.Errorf("graph.Build: %w", err)
	}
	if err := graph.Validate(g); err != nil {
		return persist.Manifest{}, fmt.Errorf("graph.Validate: %w", err)
	}

	// (5) cluster
	pkgTree := cluster.BuildPkgTree(g)
	topicTree := cluster.BuildTopicTree(g, []float64{0.5, 1.0, 2.0}, 42)

	// (6) score
	score.Compute(g)

	// (7) persist — V0 = full rebuild only. Wipe any existing graph.db so we
	// don't accumulate stale rows between builds.
	dbPath := filepath.Join(opt.OutDir, "graph.db")
	_ = os.Remove(dbPath)
	store, err := persist.Open(dbPath)
	if err != nil {
		return persist.Manifest{}, err
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		return persist.Manifest{}, err
	}
	if err := store.InsertNodes(g.Nodes); err != nil {
		return persist.Manifest{}, err
	}
	if err := store.InsertEdges(g.Edges); err != nil {
		return persist.Manifest{}, err
	}
	if err := store.InsertPkgTreeFromCluster(pkgTree.PersistEdges()); err != nil {
		return persist.Manifest{}, err
	}
	if err := store.InsertTopicTree(topicTree); err != nil {
		return persist.Manifest{}, err
	}
	if err := store.InsertBlobs(extractBlobs(opt.SrcRoot, g.Nodes)); err != nil {
		return persist.Manifest{}, err
	}
	if err := store.RebuildFTS(); err != nil {
		return persist.Manifest{}, err
	}

	// Manifest with staleness fingerprint
	m := persist.Manifest{
		SchemaVersion:  "1.0",
		CKGVersion:     opt.CKGVersion,
		BuildTimestamp: time.Now().UTC().Format(time.RFC3339),
		SrcRoot:        opt.SrcRoot,
		Languages:      map[string]int{"go": len(files.Go), "ts": len(files.TS), "sol": len(files.Sol)},
		Stats: map[string]int{
			"nodes":          len(g.Nodes),
			"edges":          len(g.Edges),
			"pkg_tree_edges": len(pkgTree.Edges),
		},
		ParseErrorsCount: parseErrs,
		ClusteringStatus: "ok",
	}
	setStaleness(&m)
	if err := store.SetManifest(m); err != nil {
		return persist.Manifest{}, err
	}
	if err := writeManifestJSON(filepath.Join(opt.OutDir, "manifest.json"), m); err != nil {
		return persist.Manifest{}, err
	}
	log.Info("build complete",
		"nodes", len(g.Nodes), "edges", len(g.Edges),
		"pkg_tree_edges", len(pkgTree.Edges),
		"topic_resolutions", len(topicTree.Resolutions))
	return m, nil
}

// shouldRun returns true when lang is requested explicitly or via the "auto"
// catch-all in opts.
func shouldRun(lang string, opts []string) bool {
	for _, l := range opts {
		if l == "auto" || l == lang {
			return true
		}
	}
	return false
}

// runGoPipeline drives Pass 1 (per-file ParseFile) + Pass 2 (Resolve) for Go.
// Returns the resolved graph, count of files that failed to read or parse,
// and any fatal Resolve error.
func runGoPipeline(srcRoot string, files []string, log *slog.Logger) (*parse.ResolvedGraph, int, error) {
	p := gop.New(srcRoot)
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
		results = append(results, r)
	}
	rg, err := p.Resolve(results)
	return rg, errs, err
}

// extractBlobs reads every node's source slice (StartByte..EndByte) into a
// per-node blob, caching file contents to amortize IO. Package nodes are
// skipped (they have no syntactic body) and offsets are bounds-checked
// defensively to avoid panics on malformed nodes.
func extractBlobs(root string, nodes []types.Node) map[string][]byte {
	blobs := map[string][]byte{}
	cache := map[string][]byte{}
	for _, n := range nodes {
		if n.Type == types.NodePackage {
			continue
		}
		full := filepath.Join(root, n.FilePath)
		src, ok := cache[full]
		if !ok {
			b, err := os.ReadFile(full)
			if err != nil {
				continue
			}
			cache[full] = b
			src = b
		}
		if n.StartByte < 0 || n.EndByte > len(src) || n.StartByte >= n.EndByte {
			continue
		}
		blobs[n.ID] = append([]byte(nil), src[n.StartByte:n.EndByte]...)
	}
	return blobs
}

// setStaleness records the staleness fingerprint on the manifest. Prefers a
// git commit SHA; falls back to summing mtimes of up to 5 detected files when
// the source root is not a git checkout.
func setStaleness(m *persist.Manifest) {
	out, err := exec.Command("git", "-C", m.SrcRoot, "rev-parse", "HEAD").Output()
	if err == nil {
		m.SrcCommit = strings.TrimSpace(string(out))
		m.StalenessMethod = "git"
		return
	}
	m.StalenessMethod = "mtime"
	files, _ := detect.Walk(m.SrcRoot)
	all := append(append([]string{}, files.Go...), files.TS...)
	all = append(all, files.Sol...)
	if len(all) > 5 {
		all = all[:5]
	}
	var sum int64
	for _, rel := range all {
		st, err := os.Stat(filepath.Join(m.SrcRoot, rel))
		if err == nil {
			sum += st.ModTime().UnixNano()
		}
	}
	m.StalenessFiles = all
	m.StalenessMTimeSum = sum
}

// writeManifestJSON pretty-prints the manifest to path for human inspection.
func writeManifestJSON(path string, m persist.Manifest) error {
	buf, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	return os.WriteFile(path, buf, 0o644)
}
