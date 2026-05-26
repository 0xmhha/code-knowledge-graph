package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
)

func newBuildCmd() *cobra.Command {
	var src, out, outTag, dbDsn, filesFrom string
	var langs []string
	var noCache, rebuildMetrics, strictValidate, lockPropagation bool
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Parse a source tree and produce graph.db",
		RunE: func(cmd *cobra.Command, args []string) error {
			log, cleanup, err := newLogger(rootVerbose, rootLogFile)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer cleanup()

			effectiveOut, err := resolveOutDir(out, outTag, src)
			if err != nil {
				return err
			}

			m, err := buildpipe.Run(buildpipe.Options{
				SrcRoot:         src,
				OutDir:          effectiveOut,
				Languages:       langs,
				Logger:          log,
				CKGVersion:      ckgVersion,
				NoCache:         noCache,
				RebuildMetrics:  rebuildMetrics,
				DBDSN:           dbDsn,
				StrictValidate:  strictValidate,
				FilesFromPath:   filesFrom,
				LockPropagation: lockPropagation,
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "ckg: built %d nodes / %d edges into %s\n",
				m.Stats["nodes"], m.Stats["edges"], effectiveOut)
			return nil
		},
	}
	cmd.Flags().StringVar(&src, "src", "", "source root (required)")
	cmd.Flags().StringVar(&out, "out", "", "output directory (required)")
	cmd.Flags().StringVar(&outTag, "out-tag", "",
		`suffix appended to --out directory; "auto-commit-hash" appends the source tree's path-aware HEAD commit (short SHA)`)
	cmd.Flags().StringSliceVar(&langs, "lang", []string{"auto"}, "languages: auto|go,ts,sol")
	cmd.Flags().BoolVar(&noCache, "no-cache", false,
		"bypass A3 incremental cache; full rebuild from scratch")
	cmd.Flags().BoolVar(&rebuildMetrics, "rebuild-metrics", false,
		"force PageRank/Leiden recompute even when cache would otherwise reuse them")
	cmd.Flags().StringVar(&dbDsn, "db", "",
		"PostgreSQL DSN (e.g. postgres://user:pass@host/dbname); if set, store graph in PG instead of local SQLite")
	cmd.Flags().BoolVar(&strictValidate, "strict-validate", false,
		"abort on first dangling edge (legacy v0.x behaviour); default lenient drops them with a warning")
	cmd.Flags().StringVar(&filesFrom, "files-from", "",
		"path to JSON file with {include, exclude} glob patterns; restricts which files reach the parsers")
	cmd.Flags().BoolVar(&lockPropagation, "lock-propagation", false,
		"enable Go cross-function lock propagation (W-A, D1 Stage B DFS depth=5); requires --no-cache to take full effect")
	_ = cmd.MarkFlagRequired("src")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

// resolveOutDir applies --out-tag to --out. When tag is empty, out is
// returned unchanged. "auto-commit-hash" resolves the path-aware HEAD
// of srcRoot and appends "-<short-sha>" to out. Any other value is
// appended verbatim as "-<tag>".
func resolveOutDir(out, tag, srcRoot string) (string, error) {
	if tag == "" {
		return out, nil
	}
	if tag == "auto-commit-hash" {
		sha, err := srcCommitHash(srcRoot)
		if err != nil {
			return "", fmt.Errorf("--out-tag=auto-commit-hash: %w", err)
		}
		if sha == "" {
			return "", fmt.Errorf("--out-tag=auto-commit-hash: no git history for %s", srcRoot)
		}
		short := sha
		if len(short) > 12 {
			short = short[:12]
		}
		return out + "-" + short, nil
	}
	return out + "-" + tag, nil
}

// srcCommitHash returns the path-aware HEAD commit SHA for srcRoot.
func srcCommitHash(srcRoot string) (string, error) {
	absRoot, err := filepath.Abs(srcRoot)
	if err != nil {
		return "", err
	}
	out, err := exec.Command("git", "-C", absRoot, "log", "-1", "--format=%H").Output()
	if err != nil {
		return "", fmt.Errorf("git log in %s: %w", absRoot, err)
	}
	return strings.TrimSpace(string(out)), nil
}
