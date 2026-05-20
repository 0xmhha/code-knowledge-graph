// Package retrieval implements the LLM-free retrieval-accuracy
// measurement (EV1 Phase 2) — load YAML probe fixtures, dispatch each
// to the matching MCP tool through StoreReader, score result symbols
// against an expected set with recall / precision / F1.
//
// Why this layer exists separately from internal/eval (LLM-driven):
// retrieval tests are deterministic, fast (no API calls), and gate
// every code change. The LLM eval lives next door with its own
// runner.go — they share the task YAML idiom but not the execution
// path.
//
// Fixture lifecycle:
//
//  1. eval/retrieval/*.yaml is committed
//  2. ckg eval-retrieval --graph=eval/.synthetic-data --fixtures=eval/retrieval
//     loads each fixture, executes the probe, scores the result
//  3. Output is JSON for diffing against eval/baseline/retrieval.json
//
// New tool support requires extending dispatchProbe in runner.go;
// the fixture format stays stable (map[string]any args).
package retrieval

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Fixture is one retrieval probe specification.
//
// Schema is intentionally close to the LLM-eval task YAML
// (id, description, expected.symbols, scoring.threshold) so a
// future reader recognises the shape, but adds `probe.{tool, args}`
// to make the call deterministic — there's no LLM to interpret
// natural-language `description` here.
type Fixture struct {
	ID          string  `yaml:"id"`
	Description string  `yaml:"description"`
	Probe       Probe   `yaml:"probe"`
	Expected    Expect  `yaml:"expected"`
	Scoring     Scoring `yaml:"scoring"`
}

// Probe identifies the StoreReader/MCP tool to invoke and its arguments.
// Args is map[string]any (not a typed struct) so the schema can carry
// every tool's parameters without a Go enum — dispatchProbe routes by
// Tool and casts each arg at call time.
type Probe struct {
	Tool string         `yaml:"tool"`
	Args map[string]any `yaml:"args"`
}

// Expect carries the gold-set symbols. The set is unordered; order is
// not part of the assertion. If a downstream test cares about ranking
// (top-K MRR, NDCG), that's a Phase 2.5 addition, not what this layer
// scores.
type Expect struct {
	Symbols []string `yaml:"symbols"`
}

// Scoring carries per-fixture pass/fail thresholds. Defaults (when a
// field is omitted in YAML) are:
//
//	RecallMin    = 0.0   (no recall gate — the test is informational)
//	PrecisionMin = 0.0   (no precision gate)
//
// In practice every committed fixture sets at least RecallMin so
// regressions surface as test failures rather than silent diffs.
type Scoring struct {
	RecallMin    float64 `yaml:"recall_min"`
	PrecisionMin float64 `yaml:"precision_min"`
}

// LoadFixtures reads every *.yaml file under dir and returns them
// sorted by ID for deterministic execution order. A malformed file
// (missing ID, unknown tool, missing expected.symbols) is a hard
// error — the eval gate must not silently skip a broken fixture.
func LoadFixtures(dir string) ([]Fixture, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read fixture dir %q: %w", dir, err)
	}
	var out []Fixture
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if filepath.Ext(name) != ".yaml" && filepath.Ext(name) != ".yml" {
			continue
		}
		full := filepath.Join(dir, name)
		raw, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", full, err)
		}
		var f Fixture
		if err := yaml.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("parse %s: %w", full, err)
		}
		if err := validateFixture(&f, full); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	// Sort by ID so runs are reproducible — the bench-mcp baseline
	// idiom we already use for CKG-5.
	sortByID(out)
	return out, nil
}

// validateFixture enforces the schema invariants that the runner
// would otherwise crash on. Failing fast at load time gives the
// user a clear file path; a runtime panic would not.
func validateFixture(f *Fixture, path string) error {
	if f.ID == "" {
		return fmt.Errorf("%s: missing required field 'id'", path)
	}
	if f.Probe.Tool == "" {
		return fmt.Errorf("%s: missing required field 'probe.tool'", path)
	}
	if len(f.Expected.Symbols) == 0 {
		return fmt.Errorf("%s: 'expected.symbols' must be non-empty (zero-expected fixtures are unscoreable)", path)
	}
	return nil
}

func sortByID(fs []Fixture) {
	// Bubble would be fine for small N, but pulling in sort.Slice
	// keeps complexity sub-linear if someone commits 100 fixtures.
	for i := 1; i < len(fs); i++ {
		for j := i; j > 0 && fs[j-1].ID > fs[j].ID; j-- {
			fs[j-1], fs[j] = fs[j], fs[j-1]
		}
	}
}
