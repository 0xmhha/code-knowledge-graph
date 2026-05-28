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

	pkgstore "github.com/0xmhha/code-knowledge-graph/pkg/store"
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
	// empty, the CLIWRAP_AGENT environment variable is consulted. CKG
	// does NOT install cliwrap-agent; set CLIWRAP_AGENT or pass this
	// field explicitly. See https://github.com/0xmhha/cli-wrapper.
	AgentPath string

	// RuntimeDir is where cli-wrapper stores per-process WAL/state. If
	// empty, a directory under os.TempDir() is created.
	RuntimeDir string
}

// ErrClaudeNotFound is returned when --llm-backend=cli is requested but the
// claude binary cannot be located (PATH lookup failed and no override was
// provided).
var ErrClaudeNotFound = errors.New("claude CLI binary not found in PATH; provide --llm-claude-binary")

// ErrCliwrapAgentNotFound is returned when cliwrap-agent path cannot be
// resolved. Set the CLIWRAP_AGENT environment variable or provide
// CLIClientOptions.AgentPath. See https://github.com/0xmhha/cli-wrapper.
var ErrCliwrapAgentNotFound = errors.New(
	"cliwrap-agent path not provided; set CLIWRAP_AGENT env var or " +
		"pass CLIClientOptions.AgentPath. See https://github.com/0xmhha/cli-wrapper for installation.",
)

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
		agent = os.Getenv("CLIWRAP_AGENT")
	}
	if agent == "" {
		return nil, ErrCliwrapAgentNotFound
	}
	// Validate the agent path resolves before handing it to cli-wrapper.
	// Reason: smoke-run 2026-05-21 produced `exec: "ccliwrap-agent not
	// found": executable file not found in $PATH` followed by a
	// nil-pointer panic during Close (cli-wrapper internal lifecycle bug
	// when Start fails on a registered handle). The exec error came from
	// cli-wrapper resolving CLIWRAP_AGENT and trying to launch it, with
	// no early validation on our side. A pre-check here catches the
	// common cases (CLIWRAP_AGENT pointing at a non-existent binary,
	// PATH lookup of a misspelled name, the binary not having executable
	// permission) and produces an actionable error before cli-wrapper
	// gets the chance to misfire in a hard-to-recover state.
	if _, err := os.Stat(agent); err != nil {
		return nil, fmt.Errorf("ckg eval: CLIWRAP_AGENT=%q is not accessible: %w "+
			"(check spelling and path; install with "+
			"`go install github.com/0xmhha/cli-wrapper/cmd/cliwrap-agent@latest`)",
			agent, err)
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
//
// The deferred recover guards against an upstream lifecycle bug
// surfaced by smoke-run 2026-05-21: when h.Start() fails on a
// processHandle that's already registered with the Manager, the
// subsequent Shutdown walks the handle list, calls
// processHandle.Stop, and reaches into a nil ipc.Conn (Seqs nil
// dereference). Without the recover, the runner's `defer
// llm.Close()` panics and overwrites the real error
// ("start claude: ...exec...") that the user actually needs to
// see. With the recover, Close converts the panic into a normal
// error return so the runner surfaces both: the original spawn
// failure on stderr from runOne, and this Close error from the
// final Run return value.
// CompleteWithTools runs the γ multi-turn tool-use loop via the
// prompt-based protocol. See gamma_loop.go.
func (c *CLIClient) CompleteWithTools(ctx context.Context, system, user string,
	store pkgstore.Reader) (LLMResult, error) {
	return runGammaPromptLoop(ctx, c, system, user, store)
}

func (c *CLIClient) Close() (err error) {
	if c.mgr == nil {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("ckg eval: cli-wrapper Shutdown panicked: %v "+
				"(upstream bug — likely a registered handle whose Start failed)", r)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.mgr.Shutdown(ctx)
}

// Complete spawns one `claude -p` invocation, waits for exit, snapshots
// stdout, and parses the result.
//
// Flags used:
//   - -p / --print: non-interactive mode required for piping/JSON output
//   - --no-session-persistence: do not write sessions to disk (prevents
//     user's session DB from being polluted by eval runs)
//   - --output-format json: single JSON document on stdout (schema below)
//
// Note: --bare is intentionally NOT used. Without it, claude uses normal
// auth (OAuth, keychain, ANTHROPIC_API_KEY — whatever the user configured),
// which is required for Pro/Max users who rely on OAuth/keychain auth.
//
// The `system` argument, if non-empty, is forwarded as
// --append-system-prompt. The `user` argument is the final positional
// prompt.
func (c *CLIClient) Complete(ctx context.Context, system, user string) (LLMResult, error) {
	args := []string{
		"-p",
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

	text, usage, err := extractClaudeText(raw)
	if err != nil {
		return LLMResult{}, err
	}

	// Token attribution: Claude's --output-format json `usage` block is
	// per-invocation accurate (input + output + cache_read +
	// cache_creation). token-monitor was previously the primary source
	// but it surfaces SESSION-CUMULATIVE counts from the local JSONL
	// (e.g. 138M cached_tokens after a long Claude Code session) which
	// is meaningless as a per-invocation metric. Stick with claude's
	// own numbers; queryTokenMonitor remains in the file as a hook for
	// future "session burn rate" or "billing block remaining" reporting,
	// but is no longer wired into LLMResult. See docs/EVAL.md.
	return LLMResult{
		OutputText:        text,
		InputTokens:       usage.InputTokens,
		OutputTokens:      usage.OutputTokens,
		CacheReadTokens:   usage.CacheRead,
		CacheCreateTokens: usage.CacheCreate,
	}, nil
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

// claudeUsage mirrors the token counts from claude's --output-format json
// `usage` block and is also used to carry token-monitor results.
type claudeUsage struct {
	InputTokens  int
	OutputTokens int
	CacheRead    int
	CacheCreate  int
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
		InputTokens         int `json:"input_tokens"`
		OutputTokens        int `json:"output_tokens"`
		CacheReadTokens     int `json:"cache_read_input_tokens"`
		CacheCreationTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// extractClaudeText pulls the assistant's response text from claude's
// --output-format json stdout and returns it alongside the parsed usage
// block (used as fallback only when token-monitor is unavailable).
// If is_error is true the result text is surfaced as a Go error.
func extractClaudeText(raw []byte) (text string, usage claudeUsage, err error) {
	if len(raw) == 0 {
		return "", claudeUsage{}, errors.New("ckg eval: claude produced empty output")
	}
	var resp claudeJSONOutput
	if jsonErr := json.Unmarshal(raw, &resp); jsonErr != nil {
		return "", claudeUsage{}, fmt.Errorf("ckg eval: parse claude json: %w", jsonErr)
	}
	if resp.IsError {
		// claude signalled an error in-band; surface it. The result
		// field typically contains a human-readable message in that
		// case (e.g., "Not logged in · Please run /login").
		return "", claudeUsage{}, fmt.Errorf("ckg eval: claude reported error: %s", resp.Result)
	}
	u := claudeUsage{
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
		CacheRead:    resp.Usage.CacheReadTokens,
		CacheCreate:  resp.Usage.CacheCreationTokens,
	}
	return resp.Result, u, nil
}

// queryTokenMonitor calls `token-monitor query --current --json` and returns
// the full token counts. This is the PRIMARY token source; claude's own usage
// block is only used when token-monitor is unavailable or returns no data.
// Returns (zero, false) on any failure — callers must treat that as a signal
// to use the claude fallback.
func queryTokenMonitor(ctx context.Context) (claudeUsage, bool) {
	bin, err := exec.LookPath("token-monitor")
	if err != nil {
		return claudeUsage{}, false
	}
	// 5s is generous for a local query; on a busy CI box a tighter budget
	// could expire before /bin/sh finishes interpreter startup.
	qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(qctx, bin, "query", "--current", "--json")
	raw, err := cmd.Output()
	if err != nil {
		return claudeUsage{}, false
	}
	var tm struct {
		InputTokens         int `json:"input_tokens"`
		OutputTokens        int `json:"output_tokens"`
		CacheReadTokens     int `json:"cache_read_tokens"`
		CacheCreationTokens int `json:"cache_creation_tokens"`
	}
	if err := json.Unmarshal(raw, &tm); err != nil {
		return claudeUsage{}, false
	}
	return claudeUsage{
		InputTokens:  tm.InputTokens,
		OutputTokens: tm.OutputTokens,
		CacheRead:    tm.CacheReadTokens,
		CacheCreate:  tm.CacheCreationTokens,
	}, true
}
