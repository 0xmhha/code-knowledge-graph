//go:build e2e

package mcp_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/buildpipe"
)

func TestMCPListsAllTools(t *testing.T) {
	out := t.TempDir()
	if _, err := buildpipe.Run(buildpipe.Options{
		SrcRoot: "../parse/golang/testdata/resolve", OutDir: out,
		Languages: []string{"auto"}, CKGVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}
	bin, _ := filepath.Abs("../../bin/ckg")
	cmd := exec.Command(bin, "mcp", "--graph", out)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()

	br := bufio.NewReader(stdout)

	// initialize
	send(stdin, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"protocolVersion": "2024-11-05",
			"capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "0"}},
	})
	read(br)
	send(stdin, map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})

	// tools/list
	send(stdin, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	resp := read(br)
	// Signal EOF so the server loop terminates cleanly on defer.Kill.
	_ = stdin.Close()

	if resp == nil {
		t.Fatalf("tools/list returned no response")
	}
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatalf("tools/list missing result (resp=%v)", resp)
	}
	tools, _ := result["tools"].([]any)
	want := map[string]bool{
		"find_symbol": true, "find_callers": true, "find_callees": true,
		"get_subgraph": true, "search_text": true, "get_context_for_task": true,
	}
	got := map[string]bool{}
	for _, tool := range tools {
		got[tool.(map[string]any)["name"].(string)] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("missing tool %q (got %v)", name, got)
		}
	}
}

// mcp-go stdio transport uses newline-delimited JSON (NDJSON) framing, not
// LSP-style Content-Length headers. See github.com/mark3labs/mcp-go/server/stdio.go
// (readNextLine uses bufio.ReadString('\n'); writer emits "%s\n").

func send(w io.Writer, m map[string]any) {
	buf, _ := json.Marshal(m)
	fmt.Fprintf(w, "%s\n", buf)
}

func read(br *bufio.Reader) map[string]any {
	line, err := br.ReadString('\n')
	if err != nil {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(line), &m)
	return m
}
