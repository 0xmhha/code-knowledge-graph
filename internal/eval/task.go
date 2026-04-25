// Package eval runs the four-baseline measurement (spec §9).
// Each task is a YAML file; baselines differ only in the MCP tool
// allowlist (and α uses no tools at all).
package eval

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Task mirrors the eval/tasks/*.yaml schema (spec §9.3).
type Task struct {
	ID           string   `yaml:"id"`
	Corpus       string   `yaml:"corpus"`        // "synthetic" | "real" | absolute path
	CorpusPath   string   `yaml:"corpus_path"`   // optional override
	Description  string   `yaml:"description"`
	ExpectedKind string   `yaml:"expected_kind"` // "symbol_set" | "code_patch" | "rubric"
	Expected     Expected `yaml:"expected"`
	Scoring      Scoring  `yaml:"scoring"`
}

type Expected struct {
	// symbol_set kind
	Symbols []string `yaml:"symbols,omitempty"`
	// code_patch kind
	MustUseSymbols      []string `yaml:"must_use_symbols,omitempty"`
	MustCall            []string `yaml:"must_call,omitempty"`
	MustNotBreakSig     bool     `yaml:"must_not_break_signature,omitempty"`
	// rubric kind
	Rubric []string `yaml:"rubric,omitempty"`
}

type Scoring struct {
	Type      string             `yaml:"type"` // "precision_recall" | "rubric"
	Threshold map[string]float64 `yaml:"threshold,omitempty"`
}

// LoadTasks reads any *.yaml under glob (e.g. "eval/tasks/synthetic-*.yaml").
func LoadTasks(glob string) ([]Task, error) {
	paths, err := filepath.Glob(glob)
	if err != nil {
		return nil, err
	}
	var tasks []Task
	for _, p := range paths {
		buf, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var t Task
		if err := yaml.Unmarshal(buf, &t); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}
