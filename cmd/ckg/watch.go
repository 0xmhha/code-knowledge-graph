package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
)

// newWatchCmd wires `ckg watch` — P3 #11. Runs an initial build, then
// watches --src for filesystem events and triggers an incremental
// rebuild on relevant changes. Pairs with `ckg serve --graph=<out>` in
// a second terminal: SQLite WAL allows the reader to keep observing
// the same graph.db file while the writer (this command) lands
// incremental updates in place.
//
// Why not fold this into `serve --watch`: the running server holds an
// open StoreReader; closing-and-reopening it on every change would
// race with in-flight HTTP requests and complicate the lifecycle for
// negligible gain. A standalone watcher keeps the writer/reader
// concerns separate and lets operators script either side without
// regard for the other.
func newWatchCmd() *cobra.Command {
	var src, out, outTag string
	var langs []string
	var debounceMS int
	var policyFile, securityPatternFile, filesFrom string
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch a source tree and incrementally rebuild graph.db on change (P3 #11)",
		Long: `Runs an initial build, then watches the source tree for filesystem events
and triggers an incremental rebuild on Go / TypeScript / Solidity / Proto
file changes. Multiple events inside the debounce window collapse to a
single rebuild so editor-save bursts don't thrash the pipeline.

Pair with 'ckg serve --graph=<out>' in another terminal for a live
viewer: SQLite WAL mode lets the serve reader observe in-place
updates that this writer lands. Cold rebuilds (which truncate
graph.db) only happen on the initial run; from then on every event
takes the incremental cache path.`,
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
			runOpts := func() buildpipe.Options {
				return buildpipe.Options{
					SrcRoot:             src,
					OutDir:              effectiveOut,
					Languages:           langs,
					Logger:              log,
					CKGVersion:          ckgVersion,
					FilesFromPath:       filesFrom,
					PolicyFile:          policyFile,
					SecurityPatternFile: securityPatternFile,
				}
			}

			_, _ = fmt.Fprintf(os.Stderr, "ckg watch: initial build src=%s out=%s\n", src, effectiveOut)
			if _, err := buildpipe.Run(runOpts()); err != nil {
				return fmt.Errorf("initial build: %w", err)
			}

			ctx, cancel := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer cancel()

			return runWatchLoop(ctx, src, time.Duration(debounceMS)*time.Millisecond, log, runOpts)
		},
	}
	cmd.Flags().StringVar(&src, "src", "", "source root (required)")
	cmd.Flags().StringVar(&out, "out", "", "output directory (required)")
	cmd.Flags().StringVar(&outTag, "out-tag", "", "suffix appended to --out directory")
	cmd.Flags().StringSliceVar(&langs, "lang", []string{"auto"}, "languages: auto|go,ts,sol")
	cmd.Flags().IntVar(&debounceMS, "debounce", 250,
		"milliseconds to wait after the last event before rebuilding; editor-save bursts collapse to a single rebuild")
	cmd.Flags().StringVar(&filesFrom, "files-from", "",
		"path to JSON file with {include, exclude} glob patterns")
	cmd.Flags().StringVar(&policyFile, "policy-file", "", "path to policy YAML (pkg/policy)")
	cmd.Flags().StringVar(&securityPatternFile, "security-pattern-file", "",
		"path to security pattern YAML (pkg/security)")
	_ = cmd.MarkFlagRequired("src")
	_ = cmd.MarkFlagRequired("out")
	return cmd
}

// runWatchLoop is the file-watch event loop. Extracted so a future
// test can drive it with a synthetic event source. Currently uses
// fsnotify directly — abstracting that out is a follow-up.
func runWatchLoop(ctx context.Context, src string, debounce time.Duration,
	log *slog.Logger, opts func() buildpipe.Options) error {

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify.NewWatcher: %w", err)
	}
	defer func() { _ = w.Close() }()

	if err := addWatchedDirs(w, src); err != nil {
		return fmt.Errorf("add watches: %w", err)
	}

	// rebuildOnce serialises rebuilds so two events that fire inside
	// the debounce window never spawn overlapping buildpipe.Run calls.
	// A mutex is enough — concurrent rebuilds against the same out
	// dir would race on graph.db anyway.
	var mu sync.Mutex
	doRebuild := func() {
		mu.Lock()
		defer mu.Unlock()
		log.Info("watch: rebuild starting")
		started := time.Now()
		if _, err := buildpipe.Run(opts()); err != nil {
			log.Error("watch: rebuild failed", "err", err)
			return
		}
		log.Info("watch: rebuild complete", "elapsed", time.Since(started))
	}

	var timer *time.Timer
	resetTimer := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(debounce, doRebuild)
	}

	_, _ = fmt.Fprintf(os.Stderr, "ckg watch: watching %s (debounce=%v); Ctrl-C to stop\n",
		src, debounce)
	for {
		select {
		case <-ctx.Done():
			_, _ = fmt.Fprintln(os.Stderr, "ckg watch: stopping")
			if timer != nil {
				timer.Stop()
			}
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if !isRelevantEvent(ev) {
				continue
			}
			log.Debug("watch: event", "op", ev.Op.String(), "name", ev.Name)
			resetTimer()
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			log.Warn("watch: fsnotify error", "err", err)
		}
	}
}

// addWatchedDirs walks src recursively and registers every directory
// with fsnotify. fsnotify is non-recursive by design — we add each
// dir explicitly so new files in nested packages trigger events.
// Hidden dirs (.git, .ckg-data, node_modules, web/viewer-next/.next)
// are skipped to keep the watch set bounded.
func addWatchedDirs(w *fsnotify.Watcher, src string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if isSkippedDir(base) {
			return filepath.SkipDir
		}
		return w.Add(path)
	})
}

// isSkippedDir returns true for directories the watcher should not
// descend into. Curated allowlist of well-known noise sources rather
// than a blanket "skip dotfiles" rule — we DO want to watch e.g.
// `.github` if the user adds it to the source tree, but never the
// hot directories below.
func isSkippedDir(name string) bool {
	switch name {
	case ".git", ".ckg-data", "node_modules", ".next", "out",
		"playwright-report", "test-results":
		return true
	}
	return false
}

// isRelevantEvent filters fsnotify events down to changes that
// actually affect the graph. We watch for Create/Write/Remove/Rename
// on parser-supported extensions; Chmod events are noise (editor
// permission shuffling) and never reflect content changes.
func isRelevantEvent(ev fsnotify.Event) bool {
	if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
		return false
	}
	switch strings.ToLower(filepath.Ext(ev.Name)) {
	case ".go", ".ts", ".tsx", ".sol", ".proto":
		return true
	}
	return false
}
