package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

type benchIndexResult struct {
	SrcRoot      string  `json:"src_root"`
	FullBuildMS  int64   `json:"full_build_ms"`
	FullNodes    int     `json:"full_nodes"`
	FullEdges    int     `json:"full_edges"`
	IncrBuildMS  int64   `json:"incr_build_ms"`
	IncrNodes    int     `json:"incr_nodes"`
	IncrEdges    int     `json:"incr_edges"`
	SpeedupRatio float64 `json:"speedup_ratio"`
	ModifiedFile string  `json:"modified_file"`
	Iterations   int     `json:"iterations"`
	IncrP50MS    int64   `json:"incr_p50_ms,omitempty"`
	IncrP95MS    int64   `json:"incr_p95_ms,omitempty"`
}

func newBenchIndexCmd() *cobra.Command {
	var src, format string
	var langs []string
	var iterations int
	cmd := &cobra.Command{
		Use:   "bench-index",
		Short: "Measure full vs incremental build time",
		Long: `Runs a full build (--no-cache), then touches one source file and
re-builds with the incremental cache enabled. Reports the time
for both passes and the speedup ratio.

Use --iterations to repeat the incremental pass N times and get
p50/p95 latencies.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			log, cleanup, err := newLogger(rootVerbose, rootLogFile)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer cleanup()

			workDir, err := os.MkdirTemp("", "ckg-bench-index-*")
			if err != nil {
				return err
			}
			defer func() { _ = os.RemoveAll(workDir) }()

			srcCopy := filepath.Join(workDir, "src")
			if err := copyDir(src, srcCopy); err != nil {
				return fmt.Errorf("copy src: %w", err)
			}
			out := filepath.Join(workDir, "out")

			// Phase 1: full build (cold, no cache)
			t0 := time.Now()
			mFull, err := buildpipe.Run(buildpipe.Options{
				SrcRoot:    srcCopy,
				OutDir:     out,
				Languages:  langs,
				Logger:     log,
				CKGVersion: ckgVersion,
				NoCache:    true,
			})
			if err != nil {
				return fmt.Errorf("full build: %w", err)
			}
			fullMS := time.Since(t0).Milliseconds()

			// Find a .go file to touch for the incremental pass
			modFile, err := pickFileToModify(srcCopy)
			if err != nil {
				return fmt.Errorf("pick file: %w", err)
			}
			if err := touchFile(modFile); err != nil {
				return fmt.Errorf("touch %s: %w", modFile, err)
			}

			// Phase 2: incremental build(s)
			var incrTimes []int64
			var mIncr persist.Manifest
			for i := 0; i < max(iterations, 1); i++ {
				if i > 0 {
					if err := touchFile(modFile); err != nil {
						return err
					}
				}
				t1 := time.Now()
				m, err := buildpipe.Run(buildpipe.Options{
					SrcRoot:    srcCopy,
					OutDir:     out,
					Languages:  langs,
					Logger:     log,
					CKGVersion: ckgVersion,
				})
				if err != nil {
					return fmt.Errorf("incr build %d: %w", i, err)
				}
				incrTimes = append(incrTimes, time.Since(t1).Milliseconds())
				mIncr = m
			}

			relModFile, _ := filepath.Rel(srcCopy, modFile)
			result := benchIndexResult{
				SrcRoot:      src,
				FullBuildMS:  fullMS,
				FullNodes:    mFull.Stats["nodes"],
				FullEdges:    mFull.Stats["edges"],
				IncrBuildMS:  incrTimes[0],
				IncrNodes:    mIncr.Stats["nodes"],
				IncrEdges:    mIncr.Stats["edges"],
				ModifiedFile: relModFile,
				Iterations:   len(incrTimes),
			}
			if fullMS > 0 {
				result.SpeedupRatio = float64(fullMS) / float64(incrTimes[0])
			}
			if len(incrTimes) >= 3 {
				sorted := sortedCopyInt64(incrTimes)
				result.IncrP50MS = percentileInt64(sorted, 50)
				result.IncrP95MS = percentileInt64(sorted, 95)
			}

			switch format {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			default:
				_, _ = fmt.Fprintf(os.Stderr, "ckg bench-index: full=%dms incr=%dms speedup=%.1fx modified=%s\n",
					result.FullBuildMS, result.IncrBuildMS, result.SpeedupRatio, result.ModifiedFile)
				if result.IncrP50MS > 0 {
					_, _ = fmt.Fprintf(os.Stderr, "  p50=%dms p95=%dms (%d iterations)\n",
						result.IncrP50MS, result.IncrP95MS, result.Iterations)
				}
				return nil
			}
		},
	}
	cmd.Flags().StringVar(&src, "src", "", "source root (required)")
	cmd.Flags().StringSliceVar(&langs, "lang", []string{"auto"}, "languages")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text|json")
	cmd.Flags().IntVar(&iterations, "iterations", 1, "number of incremental passes for p50/p95")
	_ = cmd.MarkFlagRequired("src")
	return cmd
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

// pickFileToModify finds the first .go file under src to use as the
// incremental-cache invalidation target.
func pickFileToModify(src string) (string, error) {
	var found string
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if filepath.Ext(p) == ".go" && found == "" {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("no .go file found under %s", src)
	}
	return found, nil
}

// touchFile appends a blank line to force content-hash change for
// the incremental cache.
func touchFile(path string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString("\n")
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func sortedCopyInt64(xs []int64) []int64 {
	cp := make([]int64, len(xs))
	copy(cp, xs)
	for i := range cp {
		for j := i + 1; j < len(cp); j++ {
			if cp[j] < cp[i] {
				cp[i], cp[j] = cp[j], cp[i]
			}
		}
	}
	return cp
}

func percentileInt64(sorted []int64, p int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p * len(sorted)) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
