package mcp

import (
	"testing"
)

// ---------------------------------------------------------------------------
// textResult
// ---------------------------------------------------------------------------

// TestTextResultNonNil verifies textResult returns a non-nil *mcp.CallToolResult
// and that the payload is preserved in StructuredContent.
func TestTextResultNonNil(t *testing.T) {
	payload := map[string]any{"hello": "world", "count": 42}
	result := textResult(payload)
	if result == nil {
		t.Fatal("textResult returned nil")
	}
	sc := result.StructuredContent
	if sc == nil {
		t.Fatal("StructuredContent is nil; expected payload to be preserved")
	}
	m, ok := sc.(map[string]any)
	if !ok {
		t.Fatalf("StructuredContent type mismatch: got %T, want map[string]any", sc)
	}
	if m["hello"] != "world" {
		t.Errorf("hello field: got %v, want \"world\"", m["hello"])
	}
}

// TestTextResultNilPayload verifies textResult does not panic when given nil.
func TestTextResultNilPayload(t *testing.T) {
	result := textResult(nil)
	if result == nil {
		t.Fatal("textResult returned nil for nil payload")
	}
}

// TestTextResultIsNotError confirms the returned result does not have IsError set.
func TestTextResultIsNotError(t *testing.T) {
	result := textResult(map[string]any{"ok": true})
	if result.IsError {
		t.Error("textResult should not set IsError=true")
	}
}
