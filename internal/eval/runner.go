package eval

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// Result is one row in the CSV.
type Result struct {
	TaskID       string
	Baseline     Baseline
	InputTokens  int
	OutputTokens int
	CachedTokens int
	Score        float64
	LatencyMS    int64
	NumToolCalls int
	Stale        bool
	RawOutput    string
}

// Run loops tasks × baselines and writes results.csv plus report.md.
func Run(ctx context.Context, tasks []Task, baselines []Baseline,
	graphDir string, llm *LLMClient, outDir string) ([]Result, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	store, err := persist.OpenReadOnly(filepath.Join(graphDir, "graph.db"))
	if err != nil {
		return nil, err
	}
	defer store.Close()
	stale := isStale(store, graphDir)

	var results []Result
	for _, t := range tasks {
		for _, b := range baselines {
			res, err := runOne(ctx, llm, store, t, b, stale)
			if err != nil {
				fmt.Fprintf(os.Stderr, "task %s/%s: %v\n", t.ID, b, err)
				continue
			}
			results = append(results, res)
		}
	}

	expected := len(tasks) * len(baselines)
	if dropped := expected - len(results); dropped > 0 {
		fmt.Fprintf(os.Stderr, "ckg eval: %d/%d (task,baseline) pairs failed; report H1/H2 may be biased\n", dropped, expected)
	}

	if err := writeCSV(filepath.Join(outDir, "results.csv"), results); err != nil {
		return results, err
	}
	if err := WriteReport(filepath.Join(outDir, "report.md"), results); err != nil {
		return results, err
	}
	return results, nil
}

// runOne executes a single (task, baseline) pair. V0 implementation:
//   - α: append raw files to user prompt, no tools
//   - β/γ/δ: register MCP tool names; tool execution is in-process here
//     (we call Store directly instead of spawning ckg mcp), keeping eval
//     hermetic and reproducible.
func runOne(ctx context.Context, llm *LLMClient, store *persist.Store,
	t Task, b Baseline, stale bool) (Result, error) {
	start := time.Now()
	system := SystemPrompt(b)
	user := t.Description

	if b == BaselineAlpha {
		// Append raw context: dump 5 random files from the corpus root.
		user += "\n\n--- raw files ---\n" + dumpFiles(t.CorpusPath, 5, 4000)
	}

	// V0 simplification: we don't actually loop tool_use round-trips here.
	// For β/γ/δ we *pre-call* the chosen tool against Store and append the
	// JSON result to the user prompt as if the LLM had received it. This
	// preserves the token-savings hypothesis test even without a tool loop.
	if b == BaselineBeta {
		if sub, _, err := store.SubgraphByQname("", 99); err == nil {
			user += "\n\n--- get_subgraph result ---\n" + jsonString(sub)
		}
	}
	if b == BaselineDelta {
		if ctxJSON, err := smartContext(store, t.Description); err == nil {
			user += "\n\n--- get_context_for_task result ---\n" + ctxJSON
		}
	}
	// γ is intentionally NOT pre-called — emulating the multi-turn cost,
	// we let the LLM ask in plain text. (Real tool-loop emulation arrives V1+.)

	out, err := llm.Complete(ctx, system, user, nil)
	if err != nil {
		return Result{}, err
	}

	score, calls := scoreTask(t, out.OutputText)
	return Result{
		TaskID: t.ID, Baseline: b,
		InputTokens: out.InputTokens, OutputTokens: out.OutputTokens,
		CachedTokens: out.CacheReadTokens + out.CacheCreateTokens,
		Score: score, LatencyMS: time.Since(start).Milliseconds(),
		NumToolCalls: calls, Stale: stale, RawOutput: out.OutputText,
	}, nil
}

// scoreTask dispatches by Task.Scoring.Type.
func scoreTask(t Task, output string) (float64, int) {
	switch t.Scoring.Type {
	case "precision_recall":
		got := extractSymbols(output)
		p, r := PrecisionRecall(got, t.Expected.Symbols)
		return (p + r) / 2, 0
	case "rubric":
		hits, total := RubricCheck(output, t.Expected.Rubric)
		if total == 0 {
			return 0, 0
		}
		return float64(hits) / float64(total), 0
	}
	return 0, 0
}

// extractSymbols pulls "pkg.Func" or backtick-quoted identifiers out of
// free text. Crude but adequate for V0 symbol_set tasks.
func extractSymbols(s string) []string {
	out := []string{}
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\n' || r == '`' || r == '"'
	}) {
		if strings.Contains(tok, ".") && !strings.HasPrefix(tok, ".") && !strings.HasSuffix(tok, ".") {
			out = append(out, strings.Trim(tok, ".:;()"))
		}
	}
	return out
}

func dumpFiles(root string, count, perFileLimit int) string {
	var b strings.Builder
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if count <= 0 {
			return filepath.SkipAll
		}
		ext := filepath.Ext(p)
		if ext != ".go" && ext != ".ts" && ext != ".sol" {
			return nil
		}
		buf, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		if len(buf) > perFileLimit {
			buf = buf[:perFileLimit]
		}
		fmt.Fprintf(&b, "\n=== %s ===\n%s\n", p, buf)
		count--
		return nil
	})
	return b.String()
}

func isStale(store *persist.Store, graphDir string) bool {
	m, err := store.GetManifest()
	if err != nil || m.StalenessMethod != "git" {
		return false
	}
	out, err := exec.Command("git", "-C", m.SrcRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != m.SrcCommit
}

// smartContext duplicates the get_context_for_task logic (in-process).
// In production it would call the MCP tool; for V0 hermetic eval we share
// the implementation. Should be moved into a shared package in V1.
func smartContext(store *persist.Store, query string) (string, error) {
	// Reuse internal/mcp.buildContext via an exported symbol — for V0 we
	// call SearchFTS + a brief packing here to avoid a circular import.
	hits, err := store.SearchFTS(query, 10)
	if err != nil {
		return "", err
	}
	return jsonString(hits), nil
}

func jsonString(v any) string {
	buf, _ := json.Marshal(v)
	return string(buf)
}

func writeCSV(path string, rows []Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write([]string{"task_id", "baseline", "input_tokens", "output_tokens",
		"cached_tokens", "score", "latency_ms", "num_tool_calls", "stale"})
	for _, r := range rows {
		_ = w.Write([]string{r.TaskID, string(r.Baseline),
			strconv.Itoa(r.InputTokens), strconv.Itoa(r.OutputTokens),
			strconv.Itoa(r.CachedTokens), fmt.Sprintf("%.4f", r.Score),
			strconv.FormatInt(r.LatencyMS, 10), strconv.Itoa(r.NumToolCalls),
			strconv.FormatBool(r.Stale)})
	}
	return nil
}
