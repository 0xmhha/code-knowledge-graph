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

	"github.com/0xmhha/code-knowledge-graph/pkg/smartctx"
	pkgstore "github.com/0xmhha/code-knowledge-graph/pkg/store"
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

	// Hallucination is the per-response classification of every symbol
	// mention the LLM emitted, looked up against the same store the
	// runner used to answer. T-04 V1 (HANDOFF.md 2026-05-11, wired
	// 2026-05-21). Populated by runOne after scoreTask; nil store on
	// the runOne path is the rubric-only short-circuit and produces
	// Total=0 / Rate=0. The detailed Found/QnameDiverged/Hallucinated
	// lists are surfaced via the report.md path, not CSV, because
	// the lists are variable-length and would balloon CSV column
	// count beyond what spreadsheet readers handle cleanly.
	Hallucination HallucinationResult
}

// Run loops tasks × baselines and writes results.csv plus report.md.
// Run takes ownership of llm: it is Closed when Run returns, regardless
// of error path. Callers must NOT Close llm themselves.
func Run(ctx context.Context, tasks []Task, baselines []Baseline,
	graphDir string, llm LLMClient, outDir string) ([]Result, error) {
	defer func() { _ = llm.Close() }()
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	store, err := pkgstore.OpenReadOnly(filepath.Join(graphDir, "graph.db"))
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
func runOne(ctx context.Context, llm LLMClient, store pkgstore.Reader,
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

	out, err := llm.Complete(ctx, system, user)
	if err != nil {
		return Result{}, err
	}

	score, calls := scoreTask(t, out.OutputText)
	// T-04 V1: classify every symbol the LLM named against the same
	// store the runner used. Failures here would mask hallucinations
	// rather than report them, so a store error degrades to a
	// best-effort empty result (Total=0) instead of failing the
	// whole task — the eval continues, just without the hallucination
	// signal for this row. The error is surfaced in the report only
	// when every row in a baseline has Total=0 (see WriteReport).
	hallu, hErr := ValidateMentions(out.OutputText, store)
	if hErr != nil {
		fmt.Fprintf(os.Stderr, "task %s/%s: hallucination check: %v (continuing)\n", t.ID, b, hErr)
	}
	return Result{
		TaskID: t.ID, Baseline: b,
		InputTokens: out.InputTokens, OutputTokens: out.OutputTokens,
		CachedTokens: out.CacheReadTokens + out.CacheCreateTokens,
		Score:        score, LatencyMS: time.Since(start).Milliseconds(),
		NumToolCalls: calls, Stale: stale, RawOutput: out.OutputText,
		Hallucination: hallu,
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
//
// VERIFICATION_REPORT 2026-05-11 §5 L2: the previous Trim set kept
// pointer-receiver sigils (`*`, `&`) attached, so LLM responses written
// in idiomatic Go syntax (`*eth.Ethereum.New`) failed to match expected
// values written in spec notation (`eth.Ethereum.New`). `*` and `&` are
// added to the Trim set so receiver-prefixed identifiers normalise.
// Trailing markdown punctuation (`,`, `;`, `)`, `]`) was already
// covered; we also add `]` `[` for inline code spans like `[pkg.Func]`.
//
// VERIFICATION_REPORT 2026-05-11 §8.3 (L2-2-1): the simulated re-scoring
// of T01 showed that LLMs writing verbose answers ("see core.NewBlockChain
// in core/blockchain.go") dragged the file path into the dot-tokenised
// symbol set, dropping precision below the 0.7 threshold even when the
// real qualified-name answers were correct. We now drop two extra
// classes of dot-bearing tokens after the Trim:
//
//   - path-like tokens (`pkg/foo.go`, `path/to/file.ts`) — anything with
//     a `/` separator is treated as a file path rather than a symbol.
//   - tokens whose trailing dot-segment matches a source file extension
//     (`.go`, `.ts`, `.tsx`, `.js`, `.jsx`, `.sol`, `.proto`, `.py`,
//     `.rs`, `.java`, `.md`, `.yaml`, `.yml`, `.toml`, `.json`). Covers
//     file citations without a directory prefix (`blockchain.go`).
//
// The blacklist is conservative — it intentionally does not strip every
// dotted token, only those that look like file paths or stand-alone file
// names. Genuine `pkg.Func` identifiers stay in the output.
//
// 2026-05-21 (T-04 V1 first smoke-run, T-02 P0): the splitter previously
// only treated parens as a *trim* set, which Trim() only applies to
// prefix/suffix. A real LLM response of "Call h.vault.Deposit(req)
// directly" produced the token `h.vault.Deposit(req` — paren *inside*
// the token never split. extractSymbols then classified it as a
// hallucination (no node named `Deposit(req`). Promoting `(` and `)`
// to FieldsFunc separators splits the call site from the symbol so
// "h.vault.Deposit" stays in the output cleanly and "(req" / "req"
// gets discarded by the "must contain a dot" filter.
//
// Prose-abbreviation blacklist (e.g., i.e., et.al, ...) is applied
// next. LLMs use these in explanatory text and they all match the
// dot-bearing shape extractSymbols looks for. isProseAbbreviation
// drops them by case-folded lookup.
func extractSymbols(s string) []string {
	out := []string{}
	for _, tok := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\n' || r == '`' || r == '"' ||
			r == '(' || r == ')'
	}) {
		// Normalise pointer-receiver sigils + bracket/punct wrappers before
		// the dot-position check, so `*pkg.Func.` or `[pkg.Func]` both
		// reduce to `pkg.Func`. Parens stay in the Trim set as a defensive
		// belt-and-suspenders — if a future splitter change removes paren
		// separation, Trim still cleans simple wrappers.
		tok = strings.Trim(tok, ".:;()[]*&")
		if !strings.Contains(tok, ".") || strings.HasPrefix(tok, ".") || strings.HasSuffix(tok, ".") {
			continue
		}
		// L2-2-1 file path / extension blacklist (§8.3).
		if strings.Contains(tok, "/") {
			continue
		}
		if dot := strings.LastIndex(tok, "."); dot >= 0 && isFileExtension(tok[dot:]) {
			continue
		}
		// Prose abbreviation blacklist (T-04 V1 finding 2026-05-21).
		// `e.g`, `i.e`, `et.al`, `vs.something` shapes that LLMs use in
		// explanatory text but that have no chance of resolving in the
		// graph. Case-folded lookup so `E.g` is filtered the same as
		// `e.g`.
		if isProseAbbreviation(tok) {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// isProseAbbreviation matches common explanatory-prose tokens that
// extractSymbols' "dot-bearing identifier" filter would otherwise
// catch. The set is intentionally small and case-folded — adding
// rarer abbreviations is cheap. Keys are stored already case-folded.
func isProseAbbreviation(tok string) bool {
	switch strings.ToLower(tok) {
	case "e.g", "i.e", "et.al", "etc.", "vs.":
		return true
	}
	return false
}

// isFileExtension reports whether ext (including the leading dot) is a
// recognised source/markup file extension that should be excluded from
// extractSymbols output. The set is the dominant set in CKG-targeted
// repos; new languages or markup formats can be added incrementally.
func isFileExtension(ext string) bool {
	switch ext {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
		".sol", ".proto", ".py", ".rs", ".java", ".kt", ".swift",
		".cpp", ".cc", ".cxx", ".c", ".h", ".hpp",
		".md", ".markdown",
		".yaml", ".yml", ".toml", ".json", ".xml", ".html", ".css",
		".sh", ".bash", ".zsh":
		return true
	}
	return false
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

func isStale(store pkgstore.Reader, graphDir string) bool {
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

// smartContext now delegates to pkg/smartctx — the same shared algorithm
// MCP's get_context_for_task tool runs in production. Earlier eval used
// `SearchFTS top-10 dump` which made H1/H2 hypotheses non-comparable
// against real MCP behaviour (real BM25, 1-hop expand, score fusion).
// Citation Enforcement metadata.warnings flows through too, so eval can
// surface citation coverage as part of the δ baseline report.
func smartContext(store pkgstore.Reader, query string) (string, error) {
	pack, err := smartctx.BuildContext(store, query, smartctx.Options{
		BudgetTokens: 8000,
		IncludeBlobs: true,
		MaxBodies:    5,
	})
	if err != nil {
		return "", err
	}
	return jsonString(pack), nil
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
	// raw_output column added 2026-05-11 (VERIFICATION_REPORT §5 L2). The
	// Result struct already captured RawOutput from the LLM call, but the
	// CSV writer dropped it — post-hoc debugging of low scores (e.g. T01
	// all-zero) was impossible without re-running the eval. Trailing
	// column position is chosen so existing CSV readers that index by
	// field position keep working; new readers can opt in via header.
	// hallucination_* columns added 2026-05-21 (T-04 V1). Inserted
	// *before* raw_output so raw_output stays last — TestWriteCSV_RawOutputColumn
	// locks that invariant because spreadsheet readers tolerate a
	// trailing free-text column far better than a trailing numeric
	// one. Variable-length hallucinated-symbol *list* stays out of
	// CSV entirely; report.md carries the literal list so a single
	// mis-spelled symbol doesn't balloon a single CSV row to many KB.
	_ = w.Write([]string{"task_id", "baseline", "input_tokens", "output_tokens",
		"cached_tokens", "score", "latency_ms", "num_tool_calls", "stale",
		"hallucination_total", "hallucination_count", "hallucination_rate",
		"raw_output"})
	for _, r := range rows {
		_ = w.Write([]string{r.TaskID, string(r.Baseline),
			strconv.Itoa(r.InputTokens), strconv.Itoa(r.OutputTokens),
			strconv.Itoa(r.CachedTokens), fmt.Sprintf("%.4f", r.Score),
			strconv.FormatInt(r.LatencyMS, 10), strconv.Itoa(r.NumToolCalls),
			strconv.FormatBool(r.Stale),
			strconv.Itoa(r.Hallucination.Total),
			strconv.Itoa(len(r.Hallucination.Hallucinated)),
			fmt.Sprintf("%.4f", r.Hallucination.Rate),
			r.RawOutput})
	}
	return nil
}
