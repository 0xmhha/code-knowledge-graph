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

// TestParseClaudeJSON_HappyPath confirms parseClaudeJSON pulls the result
// text and basic input/output token counts out of a minimal JSON document.
func TestParseClaudeJSON_HappyPath(t *testing.T) {
	raw := []byte(`{"result":"hi","usage":{"input_tokens":10,"output_tokens":3}}`)
	got, err := parseClaudeJSON(raw)
	if err != nil {
		t.Fatalf("parseClaudeJSON: %v", err)
	}
	if got.OutputText != "hi" {
		t.Errorf("OutputText: want %q got %q", "hi", got.OutputText)
	}
	if got.InputTokens != 10 || got.OutputTokens != 3 {
		t.Errorf("tokens: want (10,3) got (%d,%d)", got.InputTokens, got.OutputTokens)
	}
}

// TestParseClaudeJSON_WithCache confirms cache_read_input_tokens and
// cache_creation_input_tokens populate when present.
func TestParseClaudeJSON_WithCache(t *testing.T) {
	// Disable token-monitor enrichment for this test in case it is on PATH.
	t.Setenv("PATH", "")
	raw := []byte(`{"result":"ok","usage":{"input_tokens":1,"output_tokens":2,` +
		`"cache_read_input_tokens":7,"cache_creation_input_tokens":11}}`)
	got, err := parseClaudeJSON(raw)
	if err != nil {
		t.Fatalf("parseClaudeJSON: %v", err)
	}
	if got.CacheReadTokens != 7 {
		t.Errorf("CacheReadTokens: want 7 got %d", got.CacheReadTokens)
	}
	if got.CacheCreateTokens != 11 {
		t.Errorf("CacheCreateTokens: want 11 got %d", got.CacheCreateTokens)
	}
}

// TestParseClaudeJSON_Malformed: bad JSON returns a wrapped parse error.
func TestParseClaudeJSON_Malformed(t *testing.T) {
	_, err := parseClaudeJSON([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for malformed json")
	}
	if !strings.Contains(err.Error(), "parse claude json") {
		t.Errorf("error should be wrapped: %v", err)
	}
}

// TestParseClaudeJSON_Empty: empty bytes is a hard error (claude wrote
// nothing to stdout, which usually means it crashed before producing
// output).
func TestParseClaudeJSON_Empty(t *testing.T) {
	_, err := parseClaudeJSON(nil)
	if err == nil {
		t.Fatal("expected error for empty output")
	}
}

// TestParseClaudeJSON_EmptyResult: claude can legitimately return an
// empty result string with valid usage. That should NOT be an error.
func TestParseClaudeJSON_EmptyResult(t *testing.T) {
	t.Setenv("PATH", "")
	raw := []byte(`{"result":"","usage":{"input_tokens":5,"output_tokens":0}}`)
	got, err := parseClaudeJSON(raw)
	if err != nil {
		t.Fatalf("parseClaudeJSON: %v", err)
	}
	if got.OutputText != "" {
		t.Errorf("OutputText should be empty: %q", got.OutputText)
	}
	if got.InputTokens != 5 {
		t.Errorf("InputTokens: want 5 got %d", got.InputTokens)
	}
}

// TestParseClaudeJSON_IsErrorTrue: when claude reports is_error=true the
// result text becomes the error message and parseClaudeJSON returns an
// error so the caller does not silently accept a failed completion.
func TestParseClaudeJSON_IsErrorTrue(t *testing.T) {
	raw := []byte(`{"result":"Not logged in","is_error":true,"usage":{}}`)
	_, err := parseClaudeJSON(raw)
	if err == nil {
		t.Fatal("expected error when is_error=true")
	}
	if !strings.Contains(err.Error(), "Not logged in") {
		t.Errorf("error should include claude message: %v", err)
	}
}

// TestEnrichFromTokenMonitor_NotFound silently does nothing when
// token-monitor is unavailable on PATH.
func TestEnrichFromTokenMonitor_NotFound(t *testing.T) {
	t.Setenv("PATH", "")
	out := LLMResult{InputTokens: 1, OutputTokens: 2}
	enrichFromTokenMonitor(&out)
	if out.CacheReadTokens != 0 || out.CacheCreateTokens != 0 {
		t.Errorf("enrichment should be a no-op when binary missing: %+v", out)
	}
}

// TestEnrichFromTokenMonitor_FromFakeBinary writes a tiny shell stub that
// emits canned JSON, points PATH at it, and verifies the cache fields are
// populated. This proves the enrichment path works end-to-end without
// depending on a real token-monitor install.
func TestEnrichFromTokenMonitor_FromFakeBinary(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "token-monitor")
	body := "#!/bin/sh\n" +
		"echo '{\"cache_read_input_tokens\":13,\"cache_creation_input_tokens\":17}'\n"
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir)

	out := LLMResult{InputTokens: 1, OutputTokens: 2}
	enrichFromTokenMonitor(&out)
	if out.CacheReadTokens != 13 {
		t.Errorf("CacheReadTokens want 13 got %d", out.CacheReadTokens)
	}
	if out.CacheCreateTokens != 17 {
		t.Errorf("CacheCreateTokens want 17 got %d", out.CacheCreateTokens)
	}
}

// TestEnrichFromTokenMonitor_BadJSON: enrichment silently skips when
// token-monitor produces malformed JSON.
func TestEnrichFromTokenMonitor_BadJSON(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "token-monitor")
	body := "#!/bin/sh\necho 'not json'\n"
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir)

	out := LLMResult{}
	enrichFromTokenMonitor(&out)
	if out.CacheReadTokens != 0 || out.CacheCreateTokens != 0 {
		t.Errorf("malformed token-monitor output should be ignored: %+v", out)
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

// TestNewCLIClient_NoAgent: with a valid claude path but no cliwrap-agent
// on PATH, NewCLIClient returns ErrCliwrapAgentNotFound.
func TestNewCLIClient_NoAgent(t *testing.T) {
	dir := t.TempDir()
	fakeClaude := filepath.Join(dir, "claude")
	if err := os.WriteFile(fakeClaude, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	// Empty PATH so cliwrap-agent lookup definitely fails. The Binary
	// override means claude lookup is bypassed.
	t.Setenv("PATH", "")

	_, err := NewCLIClient(CLIClientOptions{Binary: fakeClaude})
	if !errors.Is(err, ErrCliwrapAgentNotFound) {
		t.Errorf("want ErrCliwrapAgentNotFound, got: %v", err)
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

// TestCLIClient_Complete_Smoke spawns a fake `claude` script under cli-wrapper
// supervision, runs CLIClient.Complete, and asserts the parsed LLMResult.
// This exercises the full happy path: NewSpec → Register → Start →
// waitForExit → LogsSnapshot → parseClaudeJSON.
func TestCLIClient_Complete_Smoke(t *testing.T) {
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
	if got.InputTokens != 42 || got.OutputTokens != 7 {
		t.Errorf("tokens: want (42,7) got (%d,%d)", got.InputTokens, got.OutputTokens)
	}

	// Second invocation must reuse the same Manager and increment the
	// procID counter without colliding. Empty system prompt exercises
	// the branch that omits --append-system-prompt.
	got2, err := client.Complete(ctx, "", "second call")
	if err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if got2.OutputText != "fake response" {
		t.Errorf("second call OutputText: %q", got2.OutputText)
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
// either an error from parseClaudeJSON or a successful parse depending
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
