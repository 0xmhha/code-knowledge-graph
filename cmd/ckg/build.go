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
	var src, out, outTag, atCommit, dbDsn, filesFrom, policyFile, securityPatternFile string
	var langs []string
	var noCache, force, rebuildMetrics, strictValidate, lockPropagation, failOnParseErrors bool
	var temporalDepth int
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Parse a source tree and produce graph.db",
		RunE: func(cmd *cobra.Command, args []string) error {
			log, cleanup, err := newLogger(rootVerbose, rootLogFile)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer cleanup()

			effectiveSrc := src
			var worktreeCleanup func()
			if atCommit != "" {
				wt, clean, err := checkoutWorktree(src, atCommit)
				if err != nil {
					return err
				}
				effectiveSrc = wt
				worktreeCleanup = clean
				defer worktreeCleanup()
			}

			effectiveOut, err := resolveOutDir(out, outTag, effectiveSrc)
			if err != nil {
				return err
			}

			if !force && !noCache && graphExists(effectiveOut) {
				_, _ = fmt.Fprintf(os.Stderr, "ckg: graph already exists at %s (use --force to rebuild)\n", effectiveOut)
				return nil
			}

			m, err := buildpipe.Run(buildpipe.Options{
				SrcRoot:             effectiveSrc,
				OutDir:              effectiveOut,
				Languages:           langs,
				Logger:              log,
				CKGVersion:          ckgVersion,
				NoCache:             noCache,
				RebuildMetrics:      rebuildMetrics,
				DBDSN:               dbDsn,
				StrictValidate:      strictValidate,
				FilesFromPath:       filesFrom,
				LockPropagation:     lockPropagation,
				PolicyFile:          policyFile,
				SecurityPatternFile: securityPatternFile,
				TemporalDepth:       temporalDepth,
			})
			if err != nil {
				return err
			}
			// Surface dropped files: parse failures are skipped during the
			// build, so a plain success line would misreport a partial graph
			// as complete. --fail-on-parse-errors turns that into a hard
			// failure for CI / reproducible builds.
			if m.ParseErrorsCount > 0 {
				_, _ = fmt.Fprintf(os.Stderr, "ckg: built %d nodes / %d edges into %s (%d files failed to parse and were skipped)\n",
					m.Stats["nodes"], m.Stats["edges"], effectiveOut, m.ParseErrorsCount)
				if failOnParseErrors {
					return fmt.Errorf("ckg: %d files failed to parse (--fail-on-parse-errors)", m.ParseErrorsCount)
				}
				return nil
			}
			_, _ = fmt.Fprintf(os.Stderr, "ckg: built %d nodes / %d edges into %s\n",
				m.Stats["nodes"], m.Stats["edges"], effectiveOut)
			return nil
		},
	}
	cmd.Flags().StringVar(&src, "src", "", "source root (required)")
	cmd.Flags().StringVar(&out, "out", "", "output directory (required)")
	cmd.Flags().StringVar(&outTag, "out-tag", "",
		`suffix appended to --out directory; "auto-commit-hash" appends the source tree's path-aware HEAD commit (short SHA)`)
	cmd.Flags().StringVar(&atCommit, "at-commit", "",
		"build from a specific git commit using git worktree (leaves --src working tree untouched)")
	cmd.Flags().BoolVar(&force, "force", false,
		"rebuild even if graph.db already exists at the output directory")
	cmd.Flags().StringSliceVar(&langs, "lang", []string{"auto"}, "languages: auto|go,ts,sol")
	cmd.Flags().BoolVar(&noCache, "no-cache", false,
		"bypass A3 incremental cache; full rebuild from scratch")
	cmd.Flags().BoolVar(&rebuildMetrics, "rebuild-metrics", false,
		"force PageRank/Leiden recompute even when cache would otherwise reuse them")
	cmd.Flags().StringVar(&dbDsn, "db", "",
		"PostgreSQL DSN (e.g. postgres://user:pass@host/dbname); if set, store graph in PG instead of local SQLite. DEPRECATED (ADR-0003): SQLite is the sole maintained backend")
	cmd.Flags().BoolVar(&strictValidate, "strict-validate", false,
		"abort on first dangling edge (legacy v0.x behaviour); default lenient drops them with a warning")
	cmd.Flags().StringVar(&filesFrom, "files-from", "",
		"path to JSON file with {include, exclude} glob patterns; restricts which files reach the parsers")
	cmd.Flags().BoolVar(&lockPropagation, "lock-propagation", false,
		"enable Go cross-function lock propagation (W-A, D1 Stage B DFS depth=5); requires --no-cache to take full effect")
	cmd.Flags().StringVar(&policyFile, "policy-file", "",
		"path to governance/protocol policy YAML (pkg/policy); enriches the graph with NodePolicy + EdgeGovernedBy rows")
	cmd.Flags().IntVar(&temporalDepth, "temporal-depth", 0,
		"max commits per file the temporal pass walks for changed_in/Hunk/blame edges (0 = default 10; higher deepens commit history at ~linear graph-size cost; does not affect node_prs symbol history)")
	cmd.Flags().StringVar(&securityPatternFile, "security-pattern-file", "",
		"path to security risk pattern YAML (pkg/security); enriches the graph with NodeSecurityPattern + EdgeHasSecurityPattern rows")
	cmd.Flags().BoolVar(&failOnParseErrors, "fail-on-parse-errors", false,
		"exit non-zero if any source file fails to parse (default: skip the file and report the count); use in CI to reject partial graphs")
	_ = cmd.MarkFlagRequired("src")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

// graphExists returns true if the output directory already has a graph.db.
func graphExists(outDir string) bool {
	info, err := os.Stat(filepath.Join(outDir, "graph.db"))
	return err == nil && !info.IsDir()
}

// checkoutWorktree creates a temporary git worktree at the given commit
// and returns the worktree path + cleanup function.
func checkoutWorktree(repoSrc, commit string) (string, func(), error) {
	absRepo, err := filepath.Abs(repoSrc)
	if err != nil {
		return "", nil, err
	}
	toplevel, err := exec.Command("git", "-C", absRepo, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", nil, fmt.Errorf("--at-commit: %s is not a git repository", repoSrc)
	}
	repoRoot := strings.TrimSpace(string(toplevel))

	fullSHA, err := exec.Command("git", "-C", repoRoot, "rev-parse", commit).Output()
	if err != nil {
		return "", nil, fmt.Errorf("--at-commit: cannot resolve %q: %w", commit, err)
	}
	sha := strings.TrimSpace(string(fullSHA))
	short := sha
	if len(short) > 12 {
		short = short[:12]
	}

	wtDir, err := os.MkdirTemp("", "ckg-worktree-"+short+"-*")
	if err != nil {
		return "", nil, err
	}
	if out, err := exec.Command("git", "-C", repoRoot, "worktree", "add", "--detach", wtDir, sha).CombinedOutput(); err != nil {
		_ = os.RemoveAll(wtDir)
		return "", nil, fmt.Errorf("git worktree add: %s: %w", strings.TrimSpace(string(out)), err)
	}

	cleanup := func() {
		_ = exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", wtDir).Run()
		_ = os.RemoveAll(wtDir)
	}
	return wtDir, cleanup, nil
}

// resolveOutDir applies --out-tag to --out.
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
