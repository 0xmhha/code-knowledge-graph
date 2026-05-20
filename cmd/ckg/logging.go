package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// multiHandler fans out log records to two slog.Handler instances.
// Used to write JSON to a log file while also writing text to stderr.
type multiHandler struct {
	a, b slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return m.a.Enabled(ctx, level) || m.b.Enabled(ctx, level)
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	// Both handlers must be attempted even if the first returns an error
	// (e.g. file handler errors on disk-full should not suppress stderr output).
	var firstErr error
	if m.a.Enabled(ctx, r.Level) {
		firstErr = m.a.Handle(ctx, r.Clone())
	}
	if m.b.Enabled(ctx, r.Level) {
		if err := m.b.Handle(ctx, r.Clone()); firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &multiHandler{a: m.a.WithAttrs(attrs), b: m.b.WithAttrs(attrs)}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	return &multiHandler{a: m.a.WithGroup(name), b: m.b.WithGroup(name)}
}

// newLogger builds an slog.Logger from CLI flags and the CKG_LOG_LEVEL env var.
//
// Level resolution (first match wins):
//  1. --verbose flag → slog.LevelDebug
//  2. CKG_LOG_LEVEL=debug env var → slog.LevelDebug
//  3. default → slog.LevelInfo
//
// When logFile is non-empty the logger tees output:
//   - log file: JSON (machine-readable, one record per line)
//   - stderr:   text (human-readable)
//
// When logFile is empty only the text handler on stderr is used.
//
// The returned cleanup func closes the log file if one was opened.
// Callers must defer the cleanup call.
func newLogger(verbose bool, logFile string) (*slog.Logger, func(), error) {
	level := slog.LevelInfo
	if verbose || strings.EqualFold(os.Getenv("CKG_LOG_LEVEL"), "debug") {
		level = slog.LevelDebug
	}

	stderrOpts := &slog.HandlerOptions{Level: level}
	stderrHandler := slog.NewTextHandler(os.Stderr, stderrOpts)

	if logFile == "" {
		return slog.New(stderrHandler), func() {}, nil
	}

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, func() {}, err
	}

	fileOpts := &slog.HandlerOptions{Level: level}
	fileHandler := slog.NewJSONHandler(f, fileOpts)

	h := &multiHandler{a: fileHandler, b: stderrHandler}
	cleanup := func() { _ = f.Close() }
	return slog.New(h), cleanup, nil
}
