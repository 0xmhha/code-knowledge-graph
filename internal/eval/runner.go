package eval

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math/rand"
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
	TaskID   string
	Baseline Baseline
	// RunIdx identifies which of the N repeats of a (task, baseline)
	// pair this row represents (Axis 1, 2026-05-22). Single-shot eval
	// runs leave it at 0; multi-shot runs (--n-runs > 1) fill it with
	// 0..N-1. The report aggregator groups rows by (TaskID, Baseline)
	// and computes mean ± std across RunIdx, surfacing the
	// non-determinism the third smoke run made unmistakable (3 runs
	// of the same fixture produced 0, 0, and 4 hallucinated symbols).
	RunIdx int
	// UserPromptBytes is the application-level size of the
	// per-invocation user prompt the runner built for this row
	// (post-baseline-specific append: raw files for α,
	// get_subgraph result for β, smartContext for δ). It is the
	// only "prompt size" measurement that is independent of
	// claude CLI's internal prompt cache state, which carries
	// Claude Code's workspace context across invocations and
	// inflates cached_tokens to hundreds of thousands. H1's
	// question — "does δ supply less context than α?" — answers
	// cleanly against this field; cached_tokens reads the
	// CLI-side cache pattern instead and is the wrong proxy
	// (audit 2026-05-22).
	UserPromptBytes int
	InputTokens     int
	OutputTokens    int
	CachedTokens    int
	Score           float64
	LatencyMS       int64
	NumToolCalls    int
	Stale           bool
	RawOutput       string

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

	// Citation is the per-response file:line accuracy check (T-03).
	// Populated by runOne after scoreTask; measures whether the LLM's
	// source citations point to real files and valid line ranges.
	Citation CitationResult
}

// Run loops tasks × baselines × nRuns and writes results.csv plus
// report.md. Each (task, baseline) pair runs nRuns times; per-run
// rows carry RunIdx 0..nRuns-1 so the report aggregator can compute
// mean ± std across repeats (Axis 1, 2026-05-22). nRuns ≤ 0 is
// treated as 1 for backwards compatibility with single-shot callers.
//
// Run takes ownership of llm: it is Closed when Run returns, regardless
// of error path. Callers must NOT Close llm themselves.
func Run(ctx context.Context, tasks []Task, baselines []Baseline,
	graphDir string, llm LLMClient, outDir string, nRuns int) ([]Result, error) {
	defer func() { _ = llm.Close() }()
	if nRuns < 1 {
		nRuns = 1
	}
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
			for runIdx := 0; runIdx < nRuns; runIdx++ {
				res, err := runOne(ctx, llm, store, t, b, stale)
				if err != nil {
					fmt.Fprintf(os.Stderr, "task %s/%s run %d: %v\n", t.ID, b, runIdx, err)
					continue
				}
				res.RunIdx = runIdx
				results = append(results, res)
			}
		}
	}

	expected := len(tasks) * len(baselines) * nRuns
	if dropped := expected - len(results); dropped > 0 {
		fmt.Fprintf(os.Stderr, "ckg eval: %d/%d (task,baseline,run) triples failed; report H1/H2 may be biased\n", dropped, expected)
	}

	csvPath := filepath.Join(outDir, "results.csv")
	existing := readCSV(csvPath)
	merged := mergeResults(existing, results)
	if err := writeCSV(csvPath, merged); err != nil {
		return merged, err
	}
	if err := WriteReport(filepath.Join(outDir, "report.md"), merged); err != nil {
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
		// α: "정답 코드가 포함된 파일의 전체 내용" — task-aware file dump.
		// Resolves task.Expected.Symbols (or Description keywords as
		// fallback) to file paths, then emits each file's full source.
		user += "\n\n--- raw files ---\n" + alphaFileDump(store, t)
	}

	if b == BaselineBeta {
		// β: "관련된 그래프 전체" — task-aware subgraph dump.
		// Search task description → top candidates → 1-hop neighborhood
		// → emit all nodes + edges in the relevant region. Distinct from
		// δ in that β supplies the raw subgraph without summarization or
		// token-budget packing.
		user += "\n\n--- get_subgraph result ---\n" + betaSubgraphDump(store, t)
	}
	if b == BaselineDelta {
		// 2026-05-22 (post-V3 smoke run): smartContext was failing
		// silently and δ ran with only the task description as
		// context — UserPromptBytes was 157 (γ-equivalent) instead
		// of the expected ~32KB. The early `if err == nil` masked the
		// real failure; we now log the error so the next smoke run
		// surfaces what's breaking.
		ctxJSON, err := smartContext(store, t.Description)
		if err != nil {
			fmt.Fprintf(os.Stderr, "task %s/delta: smartContext failed: %v (continuing with bare task description)\n", t.ID, err)
		} else {
			user += "\n\n--- get_context_for_task result ---\n" + ctxJSON
		}
	}
	userPromptBytes := len(user)
	var out LLMResult
	var err error
	if b == BaselineGamma {
		// γ V1 (2026-05-28): LLM runs a real multi-turn tool-use loop.
		// The 5 retrieval tools dispatch in-process against `store`.
		// Requires the API backend (Anthropic tool_use protocol);
		// CLI backend returns ErrToolsUnsupported.
		out, err = llm.CompleteWithTools(ctx, system, user, store)
	} else {
		out, err = llm.Complete(ctx, system, user)
	}
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
	cite, cErr := ValidateCitations(out.OutputText, store)
	if cErr != nil {
		fmt.Fprintf(os.Stderr, "task %s/%s: citation check: %v (continuing)\n", t.ID, b, cErr)
	}
	// γ multi-turn accumulates a larger user message than runner's first
	// turn — prefer the cumulative value when CompleteWithTools reported it.
	effectiveUserBytes := userPromptBytes
	if out.UserPromptBytes > 0 {
		effectiveUserBytes = out.UserPromptBytes
	}
	return Result{
		TaskID: t.ID, Baseline: b,
		UserPromptBytes: effectiveUserBytes,
		InputTokens:     out.InputTokens, OutputTokens: out.OutputTokens,
		CachedTokens: out.CacheReadTokens + out.CacheCreateTokens,
		Score:        score, LatencyMS: time.Since(start).Milliseconds(),
		NumToolCalls: calls, Stale: stale, RawOutput: out.OutputText,
		Hallucination: hallu,
		Citation:      cite,
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

// extractSymbols pulls dotted identifier tokens (`pkg.Func`,
// `Vault.deposit`, `eth.Ethereum.New`) out of free LLM response text.
// The output feeds scoreTask (symbol_set scoring) and the hallucination
// + citation validators.
//
// Pipeline (each step's history is in docs/eval-trajectory.md):
//
//  1. Split on separators (isSymbolSeparator) — whitespace, prose
//     punctuation, brackets/braces, Hangul syllables, ckg node-ID
//     sigils (# @), Unicode arrows
//  2. Trim wrapping sigils — pointer (* &), brackets, dot/colon/semi
//  3. Require an interior dot (`x.y`, never `.x` or `x.`)
//  4. Drop file paths (any '/')
//  5. Drop bare file names (`.go`, `.ts`, `.sol`, …)
//  6. Drop file:line citations (`handler.go:23`)
//  7. Drop prose abbreviations (`e.g`, `i.e`, …)
//  8. Drop pure numeric literals (`0.7`, `1.0`)
func extractSymbols(s string) []string {
	var out []string
	for _, raw := range strings.FieldsFunc(s, isSymbolSeparator) {
		tok := strings.Trim(raw, symbolTrimChars)
		if !hasInteriorDot(tok) {
			continue
		}
		if isFilePathLike(tok) ||
			isBareFileName(tok) ||
			isFileLineCitation(tok) ||
			isProseAbbreviation(tok) ||
			isAllNumeric(tok) {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// symbolTrimChars are stripped from each token's edges after splitting.
// Parens stay here as a belt-and-suspenders — they are also separators,
// but Trim catches anything a future splitter change might miss.
const symbolTrimChars = ".:;()[]*&"

// isSymbolSeparator reports whether a rune terminates a token. The set
// has grown organically: each entry was added to fix a real LLM-output
// false positive (see eval-trajectory cycles).
func isSymbolSeparator(r rune) bool {
	switch r {
	case ' ', ',', '\n', '`', '"', '(', ')', '{', '}':
		return true
	case '#', '@', 0x2192: // ckg node-ID sigils + Unicode arrow (→)
		return true
	}
	// Hangul syllables (U+AC00..U+D7A3) — Korean particles attach to
	// symbols ("Vault.deposit을") and must break the token.
	return r >= 0xAC00 && r <= 0xD7A3
}

// hasInteriorDot returns true when tok contains a '.' that is neither
// at the start nor at the end. Empty / dot-prefixed / dot-suffixed
// tokens cannot be valid `pkg.Func` identifiers.
func hasInteriorDot(tok string) bool {
	if !strings.Contains(tok, ".") {
		return false
	}
	return !strings.HasPrefix(tok, ".") && !strings.HasSuffix(tok, ".")
}

// isFilePathLike treats any token containing '/' as a path citation,
// not a symbol. LLMs write "see core/blockchain.go" — the slash is the
// signal.
func isFilePathLike(tok string) bool {
	return strings.Contains(tok, "/")
}

// isBareFileName matches tokens whose trailing dot-segment is a
// recognised source/markup extension ("blockchain.go", "schema.yaml").
func isBareFileName(tok string) bool {
	dot := strings.LastIndex(tok, ".")
	return dot >= 0 && isFileExtension(tok[dot:])
}

// isFileLineCitation matches "file.ext:N" patterns where the suffix
// after the last colon is purely numeric and the prefix is a bare file
// name. Drops "handler.go:23", "Vault.sol:7" etc.
func isFileLineCitation(tok string) bool {
	colon := strings.LastIndex(tok, ":")
	if colon <= 0 || colon >= len(tok)-1 {
		return false
	}
	suffix := tok[colon+1:]
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return false
		}
	}
	prefix := tok[:colon]
	dot := strings.LastIndex(prefix, ".")
	return dot >= 0 && isFileExtension(prefix[dot:])
}

// isProseAbbreviation matches common explanatory-prose tokens that the
// "dot-bearing identifier" filter would otherwise catch. Case-folded.
func isProseAbbreviation(tok string) bool {
	switch strings.ToLower(tok) {
	case "e.g", "i.e", "et.al", "etc.", "vs.":
		return true
	}
	return false
}

// isAllNumeric reports whether tok consists only of digits and dots
// (numeric literal like "0.7" or "1.0"). Identifiers with letters
// such as "v1.Func" return false.
func isAllNumeric(tok string) bool {
	if tok == "" {
		return false
	}
	for _, r := range tok {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
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

func dumpFiles(root string, count, perFileLimit int, seed string) string {
	var candidates []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(p)
		if ext != ".go" && ext != ".ts" && ext != ".sol" {
			return nil
		}
		if strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}
		candidates = append(candidates, p)
		return nil
	})
	if len(candidates) == 0 {
		return ""
	}
	h := fnv.New64a()
	h.Write([]byte(seed))
	rng := rand.New(rand.NewSource(int64(h.Sum64())))
	rng.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})
	if count > 0 && len(candidates) > count {
		candidates = candidates[:count]
	}
	var b strings.Builder
	for _, p := range candidates {
		buf, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if len(buf) > perFileLimit {
			buf = buf[:perFileLimit]
		}
		fmt.Fprintf(&b, "\n=== %s ===\n%s\n", p, buf)
	}
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

func readCSV(path string) []Result {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil || len(records) < 2 {
		return nil
	}
	var out []Result
	for _, rec := range records[1:] {
		if len(rec) < 15 {
			continue
		}
		score, _ := strconv.ParseFloat(rec[7], 64)
		latency, _ := strconv.ParseInt(rec[8], 10, 64)
		haluTotal, _ := strconv.Atoi(rec[11])
		haluCount, _ := strconv.Atoi(rec[12])
		haluRate, _ := strconv.ParseFloat(rec[13], 64)
		upb, _ := strconv.Atoi(rec[3])
		it, _ := strconv.Atoi(rec[4])
		ot, _ := strconv.Atoi(rec[5])
		ct, _ := strconv.Atoi(rec[6])
		tc, _ := strconv.Atoi(rec[9])
		ri, _ := strconv.Atoi(rec[2])
		out = append(out, Result{
			TaskID: rec[0], Baseline: Baseline(rec[1]), RunIdx: ri,
			UserPromptBytes: upb,
			InputTokens:     it, OutputTokens: ot, CachedTokens: ct,
			Score: score, LatencyMS: latency, NumToolCalls: tc,
			Stale: rec[10] == "true",
			Hallucination: HallucinationResult{
				Total:        haluTotal,
				Hallucinated: make([]string, haluCount),
				Rate:         haluRate,
			},
			RawOutput: rec[14],
		})
	}
	return out
}

func mergeResults(old, new_ []Result) []Result {
	type key struct {
		task     string
		baseline Baseline
		run      int
	}
	m := make(map[key]Result, len(old)+len(new_))
	var order []key
	for _, r := range old {
		k := key{r.TaskID, r.Baseline, r.RunIdx}
		if _, exists := m[k]; !exists {
			order = append(order, k)
		}
		m[k] = r
	}
	for _, r := range new_ {
		k := key{r.TaskID, r.Baseline, r.RunIdx}
		if _, exists := m[k]; !exists {
			order = append(order, k)
		}
		m[k] = r
	}
	out := make([]Result, 0, len(order))
	for _, k := range order {
		out = append(out, m[k])
	}
	return out
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
	//
	// run_idx column added 2026-05-22 (Axis 1, multi-shot averaging).
	// Positioned right after baseline so (task, baseline, run_idx) is
	// the natural group key for any external analysis. Single-shot
	// runs leave it at 0.
	// user_prompt_bytes added 2026-05-22 (smartContext audit). The
	// application-level prompt size H1 actually cares about — see
	// the Result struct doc for why cached_tokens is the wrong
	// proxy.
	_ = w.Write([]string{"task_id", "baseline", "run_idx",
		"user_prompt_bytes",
		"input_tokens", "output_tokens",
		"cached_tokens", "score", "latency_ms", "num_tool_calls", "stale",
		"hallucination_total", "hallucination_count", "hallucination_rate",
		"raw_output"})
	for _, r := range rows {
		_ = w.Write([]string{r.TaskID, string(r.Baseline),
			strconv.Itoa(r.RunIdx),
			strconv.Itoa(r.UserPromptBytes),
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
