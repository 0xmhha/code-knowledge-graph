package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/0xmhha/cli-wrapper/pkg/cliwrap"
	"github.com/0xmhha/cli-wrapper/pkg/event"
)

// CLIClient runs `claude -p` via cli-wrapper as the LLM backend. It is a
// drop-in alternative to APIClient: each call to Complete spawns one
// subprocess, waits for exit, parses the JSON the binary writes to stdout,
// and returns an LLMResult. The cli-wrapper Manager is reused across
// invocations and torn down by Close.
type CLIClient struct {
	binary    string
	mgr       *cliwrap.Manager
	invokeIdx int
}

// CLIClientOptions configures CLIClient construction.
type CLIClientOptions struct {
	// Binary is the path to the `claude` executable. If empty,
	// exec.LookPath("claude") is used; if that fails, NewCLIClient
	// returns ErrClaudeNotFound.
	Binary string

	// AgentPath is the absolute path to the cliwrap-agent binary. If
	// empty, exec.LookPath("cliwrap-agent") is used; if that fails an
	// informative error is returned. CKG does NOT install cliwrap-agent;
	// users must install it from github.com/0xmhha/cli-wrapper.
	AgentPath string

	// RuntimeDir is where cli-wrapper stores per-process WAL/state. If
	// empty, a directory under os.TempDir() is created.
	RuntimeDir string
}

// ErrClaudeNotFound is returned when --llm-backend=cli is requested but the
// claude binary cannot be located (PATH lookup failed and no override was
// provided).
var ErrClaudeNotFound = errors.New("claude CLI binary not found in PATH; provide --llm-claude-binary")

// ErrCliwrapAgentNotFound is returned when cliwrap-agent cannot be located.
// Users must install it separately from github.com/0xmhha/cli-wrapper.
var ErrCliwrapAgentNotFound = errors.New("cliwrap-agent not found in PATH; install from github.com/0xmhha/cli-wrapper")

// NewCLIClient constructs a CLIClient. It resolves the claude binary path
// (override or PATH lookup), locates cliwrap-agent, and constructs a
// cli-wrapper Manager that will be reused across Complete calls.
func NewCLIClient(opts CLIClientOptions) (*CLIClient, error) {
	bin := opts.Binary
	if bin == "" {
		var err error
		bin, err = exec.LookPath("claude")
		if err != nil {
			return nil, ErrClaudeNotFound
		}
	} else if abs, err := filepath.Abs(bin); err == nil {
		// cli-wrapper requires an absolute, runnable Command path.
		bin = abs
	}

	agent := opts.AgentPath
	if agent == "" {
		p, err := exec.LookPath("cliwrap-agent")
		if err != nil {
			return nil, ErrCliwrapAgentNotFound
		}
		agent = p
	}

	runtimeDir := opts.RuntimeDir
	if runtimeDir == "" {
		runtimeDir = filepath.Join(os.TempDir(), "ckg-cliwrap")
	}
	if err := os.MkdirAll(runtimeDir, 0o700); err != nil {
		return nil, fmt.Errorf("ckg eval: mkdir runtime dir: %w", err)
	}

	mgr, err := cliwrap.NewManager(
		cliwrap.WithAgentPath(agent),
		cliwrap.WithRuntimeDir(runtimeDir),
	)
	if err != nil {
		return nil, fmt.Errorf("ckg eval: cliwrap.NewManager: %w", err)
	}
	return &CLIClient{binary: bin, mgr: mgr}, nil
}

// Close shuts down the underlying cli-wrapper Manager, draining its WAL
// outbox. A 5s timeout is used so a wedged shutdown does not hang the
// eval run; failures are returned to the caller.
func (c *CLIClient) Close() error {
	if c.mgr == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.mgr.Shutdown(ctx)
}

// Complete spawns one `claude -p` invocation, waits for exit, snapshots
// stdout, and parses the result.
//
// Flags used:
//   - -p / --print: non-interactive mode required for piping/JSON output
//   - --bare: skip hooks, plugins, keychain, auto-memory; auth strictly via
//     ANTHROPIC_API_KEY (avoids polluting the eval with the user's session)
//   - --no-session-persistence: do not write sessions to disk
//   - --output-format json: single JSON document on stdout (schema below)
//
// The `system` argument, if non-empty, is forwarded as
// --append-system-prompt. The `user` argument is the final positional
// prompt.
func (c *CLIClient) Complete(ctx context.Context, system, user string) (LLMResult, error) {
	args := []string{
		"-p",
		"--bare",
		"--no-session-persistence",
		"--output-format", "json",
	}
	if system != "" {
		args = append(args, "--append-system-prompt", system)
	}
	args = append(args, user)

	c.invokeIdx++
	procID := fmt.Sprintf("ckg-claude-%d", c.invokeIdx)

	spec, err := cliwrap.NewSpec(procID, c.binary, args...).
		WithRestart(cliwrap.RestartNever).
		Build()
	if err != nil {
		return LLMResult{}, fmt.Errorf("ckg eval: build spec: %w", err)
	}

	// Subscribe BEFORE Start so we never miss the lifecycle events.
	sub := c.mgr.Events().Subscribe(event.Filter{
		Types:      []event.Type{event.TypeProcessStopped, event.TypeProcessCrashed},
		ProcessIDs: []string{procID},
	})
	defer sub.Close()

	h, err := c.mgr.Register(spec)
	if err != nil {
		return LLMResult{}, fmt.Errorf("ckg eval: register: %w", err)
	}
	if err := h.Start(ctx); err != nil {
		return LLMResult{}, fmt.Errorf("ckg eval: start claude: %w", err)
	}

	if err := waitForExit(ctx, sub); err != nil {
		return LLMResult{}, err
	}

	raw := c.mgr.LogsSnapshot(procID, 0) // 0 = stdout
	return parseClaudeJSON(raw)
}

// waitForExit blocks until a process.stopped or process.crashed event is
// received on sub, or ctx is cancelled.
func waitForExit(ctx context.Context, sub event.Subscription) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-sub.Events():
			if !ok {
				return errors.New("ckg eval: cli-wrapper event bus closed before exit")
			}
			switch ev.EventType() {
			case event.TypeProcessStopped, event.TypeProcessCrashed:
				return nil
			}
		}
	}
}

// claudeJSONOutput is the schema emitted by `claude -p --output-format json`.
// Only the fields we care about are decoded; the rest is discarded. Schema
// confirmed empirically against claude v2.x:
//
//	{
//	  "type": "result",
//	  "subtype": "success",
//	  "is_error": false,
//	  "result": "...",
//	  "usage": {
//	    "input_tokens": int,
//	    "output_tokens": int,
//	    "cache_read_input_tokens": int,
//	    "cache_creation_input_tokens": int
//	  }
//	}
type claudeJSONOutput struct {
	Result  string `json:"result"`
	IsError bool   `json:"is_error"`
	Usage   struct {
		InputTokens          int `json:"input_tokens"`
		OutputTokens         int `json:"output_tokens"`
		CacheReadInputTokens int `json:"cache_read_input_tokens"`
		CacheCreationTokens  int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// parseClaudeJSON extracts the assistant's response text and token usage
// from claude's --output-format json stdout. If usage cache fields are
// zero, attempts a best-effort enrichment via `token-monitor query
// --current --json` (silently skipped if the binary is unavailable).
func parseClaudeJSON(raw []byte) (LLMResult, error) {
	if len(raw) == 0 {
		return LLMResult{}, errors.New("ckg eval: claude produced empty output")
	}
	var resp claudeJSONOutput
	if err := json.Unmarshal(raw, &resp); err != nil {
		return LLMResult{}, fmt.Errorf("ckg eval: parse claude json: %w", err)
	}
	if resp.IsError {
		// claude signalled an error in-band; surface it. The result
		// field typically contains a human-readable message in that
		// case (e.g., "Not logged in · Please run /login").
		return LLMResult{}, fmt.Errorf("ckg eval: claude reported error: %s", resp.Result)
	}
	out := LLMResult{
		OutputText:        resp.Result,
		InputTokens:       resp.Usage.InputTokens,
		OutputTokens:      resp.Usage.OutputTokens,
		CacheReadTokens:   resp.Usage.CacheReadInputTokens,
		CacheCreateTokens: resp.Usage.CacheCreationTokens,
	}
	if out.CacheReadTokens == 0 && out.CacheCreateTokens == 0 {
		enrichFromTokenMonitor(&out)
	}
	return out, nil
}

// enrichFromTokenMonitor calls `token-monitor query --current --json` and
// merges any available cache_read/cache_creation token counts into out.
// This is a best-effort enrichment: a missing or failing token-monitor is
// silently ignored — token-monitor is OPTIONAL.
func enrichFromTokenMonitor(out *LLMResult) {
	bin, err := exec.LookPath("token-monitor")
	if err != nil {
		return
	}
	// 5s is generous for a local query; on a busy CI box the previous 2s
	// budget could expire before /bin/sh finished interpreter startup.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "query", "--current", "--json")
	raw, err := cmd.Output()
	if err != nil {
		return
	}
	var tm struct {
		CacheRead     int `json:"cache_read_input_tokens"`
		CacheCreation int `json:"cache_creation_input_tokens"`
	}
	if err := json.Unmarshal(raw, &tm); err != nil {
		return
	}
	if out.CacheReadTokens == 0 {
		out.CacheReadTokens = tm.CacheRead
	}
	if out.CacheCreateTokens == 0 {
		out.CacheCreateTokens = tm.CacheCreation
	}
}
