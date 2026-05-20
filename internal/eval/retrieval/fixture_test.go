package retrieval

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadFixtures_Valid checks the happy-path: a well-formed fixture
// round-trips through YAML decode + validateFixture without error, and
// IDs come back sorted (the runner depends on deterministic order for
// reproducible JSON output).
func TestLoadFixtures_Valid(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "b.yaml", `
id: B
description: second fixture
probe:
  tool: find_symbol
  args: { name: "Foo" }
expected:
  symbols: ["pkg.Foo"]
scoring:
  recall_min: 1.0
`)
	writeFixture(t, dir, "a.yaml", `
id: A
description: first fixture
probe:
  tool: find_callers
  args: { qname: "pkg.Foo", depth: 2 }
expected:
  symbols: ["pkg.Bar"]
scoring:
  recall_min: 1.0
`)

	fs, err := LoadFixtures(dir)
	if err != nil {
		t.Fatalf("LoadFixtures: %v", err)
	}
	if len(fs) != 2 {
		t.Fatalf("got %d fixtures, want 2", len(fs))
	}
	if fs[0].ID != "A" || fs[1].ID != "B" {
		t.Errorf("not sorted by ID: %v, %v", fs[0].ID, fs[1].ID)
	}
}

// TestLoadFixtures_MissingID — a fixture without `id` must fail fast
// at load time, not silently produce an unnamed Result that's hard
// to identify in a diff.
func TestLoadFixtures_MissingID(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "bad.yaml", `
probe: { tool: find_symbol, args: { name: x } }
expected: { symbols: [a] }
`)
	_, err := LoadFixtures(dir)
	if err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

// TestLoadFixtures_EmptyExpected — fixtures with no expected symbols
// are unscoreable (recall denominator = 0); reject at load.
func TestLoadFixtures_EmptyExpected(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "bad.yaml", `
id: X
probe: { tool: find_symbol, args: { name: x } }
expected: { symbols: [] }
`)
	_, err := LoadFixtures(dir)
	if err == nil {
		t.Fatal("expected error for empty expected.symbols, got nil")
	}
}

// TestLoadFixtures_NonYamlIgnored — README.md or other non-YAML files
// next to fixtures must not be parsed as YAML and crash the load.
func TestLoadFixtures_NonYamlIgnored(t *testing.T) {
	dir := t.TempDir()
	writeFixture(t, dir, "a.yaml", `
id: A
probe: { tool: find_symbol, args: { name: x } }
expected: { symbols: [pkg.Y] }
scoring: { recall_min: 1.0 }
`)
	if err := os.WriteFile(filepath.Join(dir, "README.md"),
		[]byte("# Fixtures docs\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	fs, err := LoadFixtures(dir)
	if err != nil {
		t.Fatalf("LoadFixtures: %v", err)
	}
	if len(fs) != 1 {
		t.Errorf("expected 1 fixture, got %d (README.md should be ignored)", len(fs))
	}
}

func writeFixture(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", name, err)
	}
}
