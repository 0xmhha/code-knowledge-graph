package eval

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestExtractClaudeText_HappyPath confirms extractClaudeText pulls the result
// text and basic input/output token counts out of a minimal JSON document.
func TestExtractClaudeText_HappyPath(t *testing.T) {
	raw := []byte(`{"result":"hi","usage":{"input_tokens":10,"output_tokens":3}}`)
	text, usage, err := extractClaudeText(raw)
	if err != nil {
		t.Fatalf("extractClaudeText: %v", err)
	}
	if text != "hi" {
		t.Errorf("text: want %q got %q", "hi", text)
	}
	if usage.InputTokens != 10 || usage.OutputTokens != 3 {
		t.Errorf("tokens: want (10,3) got (%d,%d)", usage.InputTokens, usage.OutputTokens)
	}
}

// TestExtractClaudeText_WithCache confirms cache fields are parsed into the
// claudeUsage fallback struct and returned correctly.
func TestExtractClaudeText_WithCache(t *testing.T) {
	raw := []byte(`{"result":"ok","usage":{"input_tokens":1,"output_tokens":2,` +
		`"cache_read_input_tokens":7,"cache_creation_input_tokens":11}}`)
	_, usage, err := extractClaudeText(raw)
	if err != nil {
		t.Fatalf("extractClaudeText: %v", err)
	}
	if usage.CacheRead != 7 {
		t.Errorf("CacheRead: want 7 got %d", usage.CacheRead)
	}
	if usage.CacheCreate != 11 {
		t.Errorf("CacheCreate: want 11 got %d", usage.CacheCreate)
	}
}

// TestExtractClaudeText_Malformed: bad JSON returns a wrapped parse error.
func TestExtractClaudeText_Malformed(t *testing.T) {
	_, _, err := extractClaudeText([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for malformed json")
	}
	if !strings.Contains(err.Error(), "parse claude json") {
		t.Errorf("error should be wrapped: %v", err)
	}
}

// TestExtractClaudeText_Empty: empty bytes is a hard error (claude wrote
// nothing to stdout, which usually means it crashed before producing output).
func TestExtractClaudeText_Empty(t *testing.T) {
	_, _, err := extractClaudeText(nil)
	if err == nil {
		t.Fatal("expected error for empty output")
	}
}

// TestExtractClaudeText_EmptyResult: claude can legitimately return an
// empty result string with valid usage. That should NOT be an error.
func TestExtractClaudeText_EmptyResult(t *testing.T) {
	raw := []byte(`{"result":"","usage":{"input_tokens":5,"output_tokens":0}}`)
	text, usage, err := extractClaudeText(raw)
	if err != nil {
		t.Fatalf("extractClaudeText: %v", err)
	}
	if text != "" {
		t.Errorf("text should be empty: %q", text)
	}
	if usage.InputTokens != 5 {
		t.Errorf("InputTokens: want 5 got %d", usage.InputTokens)
	}
}

// TestExtractClaudeText_IsErrorTrue: when claude reports is_error=true the
// result text becomes the error message and extractClaudeText returns an
// error so the caller does not silently accept a failed completion.
func TestExtractClaudeText_IsErrorTrue(t *testing.T) {
	raw := []byte(`{"result":"Not logged in","is_error":true,"usage":{}}`)
	_, _, err := extractClaudeText(raw)
	if err == nil {
		t.Fatal("expected error when is_error=true")
	}
	if !strings.Contains(err.Error(), "Not logged in") {
		t.Errorf("error should include claude message: %v", err)
	}
}

// TestQueryTokenMonitor_NotFound silently returns (zero, false) when
// token-monitor is unavailable on PATH.
func TestQueryTokenMonitor_NotFound(t *testing.T) {
	t.Setenv("PATH", "")
	usage, ok := queryTokenMonitor(context.Background())
	if ok {
		t.Error("expected ok=false when binary missing")
	}
	if usage != (claudeUsage{}) {
		t.Errorf("expected zero usage when binary missing: %+v", usage)
	}
}

// TestQueryTokenMonitor_FromFakeBinary writes a tiny shell stub that emits
// canned JSON, points PATH at it, and verifies ALL token fields are parsed.
// This proves the primary token-source path works end-to-end without
// depending on a real token-monitor install.
func TestQueryTokenMonitor_FromFakeBinary(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "token-monitor")
	body := "#!/bin/sh\n" +
		`echo '{"input_tokens":99,"output_tokens":50,"cache_read_tokens":7,"cache_creation_tokens":3}'` +
		"\n"
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+oldPath)

	usage, ok := queryTokenMonitor(context.Background())
	if !ok {
		t.Fatal("expected ok=true with fake token-monitor")
	}
	if usage.InputTokens != 99 {
		t.Errorf("InputTokens want 99 got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("OutputTokens want 50 got %d", usage.OutputTokens)
	}
	if usage.CacheRead != 7 {
		t.Errorf("CacheRead want 7 got %d", usage.CacheRead)
	}
	if usage.CacheCreate != 3 {
		t.Errorf("CacheCreate want 3 got %d", usage.CacheCreate)
	}
}

// TestQueryTokenMonitor_BadJSON: returns (zero, false) when token-monitor
// produces malformed JSON — no panic.
func TestQueryTokenMonitor_BadJSON(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "token-monitor")
	body := "#!/bin/sh\necho 'not json'\n"
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+oldPath)

	usage, ok := queryTokenMonitor(context.Background())
	if ok {
		t.Error("expected ok=false for malformed JSON")
	}
	if usage != (claudeUsage{}) {
		t.Errorf("malformed token-monitor output should yield zero usage: %+v", usage)
	}
}

// TestQueryTokenMonitor_NonZeroExit: returns (zero, false) when token-monitor
// exits non-zero — no panic.
func TestQueryTokenMonitor_NonZeroExit(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "token-monitor")
	body := "#!/bin/sh\nexit 1\n"
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+":"+oldPath)

	usage, ok := queryTokenMonitor(context.Background())
	if ok {
		t.Error("expected ok=false for non-zero exit")
	}
	if usage != (claudeUsage{}) {
		t.Errorf("non-zero exit should yield zero usage: %+v", usage)
	}
}

// TestNewCLIClient_NoBinary: with PATH cleared, NewCLIClient returns
// ErrClaudeNotFound.
func TestNewCLIClient_NoBinary(t *testing.T) {
	t.Setenv("PATH", "")
	_, err := NewCLIClient(CLIClientOptions{})
	if !errors.Is(err, ErrClaudeNotFound) {
		t.Errorf("want ErrClaudeNotFound, got: %v", err)
	}
}

// TestNewCLIClient_NoAgent: with a valid claude path but no CLIWRAP_AGENT env
// and no opts.AgentPath, NewCLIClient returns ErrCliwrapAgentNotFound whose
// message mentions CLIWRAP_AGENT.
func TestNewCLIClient_NoAgent(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	// Ensure CLIWRAP_AGENT is unset and PATH doesn't accidentally find cliwrap-agent.
	t.Setenv("CLIWRAP_AGENT", "")
	t.Setenv("PATH", "")

	_, err := NewCLIClient(CLIClientOptions{Binary: fakeClaude})
	if !errors.Is(err, ErrCliwrapAgentNotFound) {
		t.Errorf("want ErrCliwrapAgentNotFound, got: %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "CLIWRAP_AGENT") {
		t.Errorf("error should mention CLIWRAP_AGENT: %v", err)
	}
}

// TestNewCLIClient_AgentFromEnv: setting CLIWRAP_AGENT env var resolves the
// agent path and allows the constructor to proceed past the agent check.
func TestNewCLIClient_AgentFromEnv(t *testing.T) {
	agentPath := buildCliwrapAgentForTest(t)

	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	// Set CLIWRAP_AGENT so no opts.AgentPath is needed.
	t.Setenv("CLIWRAP_AGENT", agentPath)

	c, err := NewCLIClient(CLIClientOptions{
		Binary:     fakeClaude,
		RuntimeDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewCLIClient with CLIWRAP_AGENT env: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestCLIClient_Close_NilManager: a CLIClient whose mgr is nil (e.g.,
// because construction failed partway through) must Close cleanly.
func TestCLIClient_Close_NilManager(t *testing.T) {
	c := &CLIClient{}
	if err := c.Close(); err != nil {
		t.Errorf("Close on nil-manager CLIClient: %v", err)
	}
}

// buildCliwrapAgentForTest builds the cliwrap-agent binary into t.TempDir
// using `go build` against the current module's go.mod. It is the test
// equivalent of having `cliwrap-agent` installed on PATH and lets the
// CLIClient smoke tests run in CI without a system-wide install.
//
// Skips the test (rather than failing) if the build cannot complete —
// e.g., the module cache is unavailable or `go` is missing from PATH.
func buildCliwrapAgentForTest(t *testing.T) string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go binary not on PATH; skipping CLIClient smoke test")
	}
	out := filepath.Join(t.TempDir(), "cliwrap-agent")
	cmd := exec.Command(goBin, "build", "-o", out,
		"github.com/0xmhha/cli-wrapper/cmd/cliwrap-agent")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Skipf("could not build cliwrap-agent: %v", err)
	}
	return out
}

// TestNewCLIClient_BinaryAbsResolution: a relative Binary override is
// resolved to an absolute path before being handed to cli-wrapper.
func TestNewCLIClient_BinaryAbsResolution(t *testing.T) {
	agentPath := buildCliwrapAgentForTest(t)

	dir := t.TempDir()
	rel := filepath.Join(dir, "claude")
	if err := os.WriteFile(rel, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	c, err := NewCLIClient(CLIClientOptions{
		Binary:     rel,
		AgentPath:  agentPath,
		RuntimeDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewCLIClient: %v", err)
	}
	if !filepath.IsAbs(c.binary) {
		t.Errorf("binary should be absolute: %s", c.binary)
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestNewCLIClient_DefaultRuntimeDir: with empty RuntimeDir the client
// builds a default under os.TempDir without erroring.
func TestNewCLIClient_DefaultRuntimeDir(t *testing.T) {
	agentPath := buildCliwrapAgentForTest(t)

	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	c, err := NewCLIClient(CLIClientOptions{
		Binary:    fakeClaude,
		AgentPath: agentPath,
	})
	if err != nil {
		t.Fatalf("NewCLIClient: %v", err)
	}
	defer func() { _ = c.Close() }()
}

// TestCLIClient_Complete_Smoke_ClaudeFallback spawns a fake `claude` script
// under cli-wrapper supervision with no token-monitor on PATH, and asserts
// that the result uses claude's own usage block as the fallback token source.
func TestCLIClient_Complete_Smoke_ClaudeFallback(t *testing.T) {
	agentPath := buildCliwrapAgentForTest(t)

	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	body := "#!/bin/sh\n" +
		"cat <<'EOF'\n" +
		"{\"result\":\"fake response\",\"usage\":{\"input_tokens\":42,\"output_tokens\":7}}\n" +
		"EOF\n"
	if err := os.WriteFile(fakeClaude, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	// Ensure token-monitor is NOT on PATH by using a clean temp dir.
	// We keep standard PATH entries (e.g. /bin, /usr/bin) so the shell
	// scripts can still execute — only token-monitor must be absent.
	noTMDir := t.TempDir() // contains no token-monitor binary
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", noTMDir+":"+oldPath)

	client, err := NewCLIClient(CLIClientOptions{
		Binary:     fakeClaude,
		AgentPath:  agentPath,
		RuntimeDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewCLIClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	got, err := client.Complete(ctx, "system prompt", "hello")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.OutputText != "fake response" {
		t.Errorf("OutputText: want %q got %q", "fake response", got.OutputText)
	}
	// With no token-monitor, must fall back to claude's own numbers.
	if got.InputTokens != 42 || got.OutputTokens != 7 {
		t.Errorf("tokens (claude fallback): want (42,7) got (%d,%d)", got.InputTokens, got.OutputTokens)
	}
	if got.CacheReadTokens != 0 || got.CacheCreateTokens != 0 {
		t.Errorf("cache tokens should be 0 with no token-monitor: cache_read=%d cache_create=%d",
			got.CacheReadTokens, got.CacheCreateTokens)
	}

	// Second invocation must reuse the same Manager and increment the
	// procID counter without colliding.
	got2, err := client.Complete(ctx, "", "second call")
	if err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if got2.OutputText != "fake response" {
		t.Errorf("second call OutputText: %q", got2.OutputText)
	}
}

// TestCLIClient_Complete_ClaudeUsagePrimary asserts the per-invocation
// token attribution comes from claude's --output-format json `usage` block,
// not from token-monitor. token-monitor surfaces SESSION-CUMULATIVE counts
// (e.g. 138M cached tokens for a long-running Claude Code session) which
// is meaningless as a per-invocation metric — see docs/EVAL.md and
// commit history for the policy reversal.
func TestCLIClient_Complete_ClaudeUsagePrimary(t *testing.T) {
	agentPath := buildCliwrapAgentForTest(t)

	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	claudeBody := "#!/bin/sh\n" +
		"cat <<'EOF'\n" +
		`{"result":"hi","usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":2,"cache_creation_input_tokens":1}}` + "\n" +
		"EOF\n"
	if err := os.WriteFile(fakeClaude, []byte(claudeBody), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	// Fake token-monitor returns wildly higher numbers — these must NOT
	// override claude's per-invocation values.
	tmDir := t.TempDir()
	tmStub := filepath.Join(tmDir, "token-monitor")
	tmBody := "#!/bin/sh\n" +
		`echo '{"input_tokens":99999,"output_tokens":50000,"cache_read_tokens":777,"cache_creation_tokens":333}'` +
		"\n"
	if err := os.WriteFile(tmStub, []byte(tmBody), 0o755); err != nil {
		t.Fatalf("write fake token-monitor: %v", err)
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", tmDir+":"+oldPath)

	client, err := NewCLIClient(CLIClientOptions{
		Binary:     fakeClaude,
		AgentPath:  agentPath,
		RuntimeDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewCLIClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	got, err := client.Complete(ctx, "", "hi")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.OutputText != "hi" {
		t.Errorf("OutputText: want %q got %q", "hi", got.OutputText)
	}
	// claude's usage block must win — token-monitor's huge values must be ignored.
	if got.InputTokens != 10 {
		t.Errorf("InputTokens: want 10 (from claude), got %d", got.InputTokens)
	}
	if got.OutputTokens != 3 {
		t.Errorf("OutputTokens: want 3 (from claude), got %d", got.OutputTokens)
	}
	if got.CacheReadTokens != 2 {
		t.Errorf("CacheReadTokens: want 2 (from claude), got %d", got.CacheReadTokens)
	}
	if got.CacheCreateTokens != 1 {
		t.Errorf("CacheCreateTokens: want 1 (from claude), got %d", got.CacheCreateTokens)
	}
}

// TestCLIClient_Complete_ContextCancelled verifies waitForExit honors
// context cancellation and returns the context error.
func TestCLIClient_Complete_ContextCancelled(t *testing.T) {
	agentPath := buildCliwrapAgentForTest(t)

	dir := t.TempDir()
	// A claude script that sleeps long enough that ctx.Done fires first.
	fakeClaude := filepath.Join(dir, "claude")
	body := "#!/bin/sh\nsleep 10\n"
	if err := os.WriteFile(fakeClaude, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	client, err := NewCLIClient(CLIClientOptions{
		Binary:     fakeClaude,
		AgentPath:  agentPath,
		RuntimeDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewCLIClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err = client.Complete(ctx, "", "hi")
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("want DeadlineExceeded/Canceled, got: %v", err)
	}
}

// TestCLIClient_Complete_NonZeroExit verifies that when claude crashes
// or is killed (the controller transitions to crashed/failed) we still
// return — the test reads whatever was written to stdout. Here the
// script writes a partial JSON document then exits non-zero; we expect
// either an error from extractClaudeText or a successful parse depending
// on what bytes made it to stdout. Either way the test must not hang.
func TestCLIClient_Complete_NonZeroExit(t *testing.T) {
	agentPath := buildCliwrapAgentForTest(t)

	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	// Write valid JSON, then exit non-zero. Cli-wrapper treats this as
	// a crash; waitForExit unblocks on either Stopped or Crashed.
	body := "#!/bin/sh\n" +
		"echo '{\"result\":\"crashy\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n" +
		"exit 7\n"
	if err := os.WriteFile(fakeClaude, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}

	client, err := NewCLIClient(CLIClientOptions{
		Binary:     fakeClaude,
		AgentPath:  agentPath,
		RuntimeDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewCLIClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// We don't assert on the exact return value; the contract is "does
	// not hang and returns a definite outcome". A successful parse is
	// fine because cli-wrapper captured stdout before the non-zero exit.
	_, err = client.Complete(ctx, "", "hi")
	_ = err
}
