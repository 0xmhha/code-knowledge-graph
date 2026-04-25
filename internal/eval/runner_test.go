package eval_test

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-graph/internal/eval"
)

// ---------------------------------------------------------------------------
// LoadTasks error paths
// ---------------------------------------------------------------------------

func TestLoadTasksErrors(t *testing.T) {
	t.Run("invalid glob returns error", func(t *testing.T) {
		// "[invalid" is an invalid glob pattern that filepath.Glob rejects
		_, err := eval.LoadTasks("[invalid")
		if err == nil {
			t.Error("want error for invalid glob, got nil")
		}
	})

	t.Run("invalid yaml returns error", func(t *testing.T) {
		dir := t.TempDir()
		bad := filepath.Join(dir, "bad.yaml")
		// Write clearly invalid YAML to trigger yaml.Unmarshal error
		if err := os.WriteFile(bad, []byte(": : : invalid\t\x00"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, err := eval.LoadTasks(filepath.Join(dir, "*.yaml"))
		if err == nil {
			t.Error("want error for invalid yaml, got nil")
		}
	})

	t.Run("unreadable file returns error", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root can read any file; skip chmod test")
		}
		dir := t.TempDir()
		f := filepath.Join(dir, "secret.yaml")
		if err := os.WriteFile(f, []byte("id: T01\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := os.Chmod(f, 0o000); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(f, 0o644) })
		_, err := eval.LoadTasks(filepath.Join(dir, "*.yaml"))
		if err == nil {
			t.Error("want error for unreadable file, got nil")
		}
	})
}

// ---------------------------------------------------------------------------
// RubricCheck stop-word path
// ---------------------------------------------------------------------------

// TestRubricCheckStopWords exercises the len(w) < stopWordMinLen → continue path
// that is skipped by the existing TestRubricMatchesItems test.
func TestRubricCheckStopWords(t *testing.T) {
	// "a" and "is" are shorter than 4 chars → skipped as stop-words → eligible=0
	// → match/max(1,0) = 0/1 = 0 < 0.6 → no hit
	hits, total := eval.RubricCheck("a is here", []string{"a is"})
	if total != 1 {
		t.Errorf("want total=1, got %d", total)
	}
	if hits != 0 {
		t.Errorf("want hits=0 when all words are stop-words, got %d", hits)
	}
}

// ---------------------------------------------------------------------------
// scoreTask
// ---------------------------------------------------------------------------

func TestScoreTask(t *testing.T) {
	t.Run("precision_recall hit", func(t *testing.T) {
		task := eval.Task{
			Scoring:  eval.Scoring{Type: "precision_recall"},
			Expected: eval.Expected{Symbols: []string{"a.Foo", "b.Bar"}},
		}
		// output contains both symbols → p=1, r=1 → avg=1
		score, calls := eval.ScoreTask(task, "a.Foo b.Bar")
		if score < 0.99 || score > 1.01 {
			t.Errorf("want score≈1.0, got %.4f", score)
		}
		if calls != 0 {
			t.Errorf("want calls=0, got %d", calls)
		}
	})

	t.Run("precision_recall miss", func(t *testing.T) {
		task := eval.Task{
			Scoring:  eval.Scoring{Type: "precision_recall"},
			Expected: eval.Expected{Symbols: []string{"a.Foo", "b.Bar"}},
		}
		// output contains neither symbol → p=0, r=0 → avg=0
		score, calls := eval.ScoreTask(task, "nothing here")
		if score != 0 {
			t.Errorf("want score=0, got %.4f", score)
		}
		if calls != 0 {
			t.Errorf("want calls=0, got %d", calls)
		}
	})

	t.Run("rubric hit", func(t *testing.T) {
		task := eval.Task{
			Scoring:  eval.Scoring{Type: "rubric"},
			Expected: eval.Expected{Rubric: []string{"validates input addr"}},
		}
		// "validates input addr" — "validates" + "addr" are long enough; both in output
		score, calls := eval.ScoreTask(task, "we validate the input addr before proceeding")
		if score < 0.99 || score > 1.01 {
			t.Errorf("want score≈1.0, got %.4f", score)
		}
		if calls != 0 {
			t.Errorf("want calls=0, got %d", calls)
		}
	})

	t.Run("rubric zero total", func(t *testing.T) {
		task := eval.Task{
			Scoring:  eval.Scoring{Type: "rubric"},
			Expected: eval.Expected{Rubric: []string{}},
		}
		score, calls := eval.ScoreTask(task, "anything")
		if score != 0 {
			t.Errorf("want score=0 when rubric is empty, got %.4f", score)
		}
		if calls != 0 {
			t.Errorf("want calls=0, got %d", calls)
		}
	})

	t.Run("unknown scoring type returns zero", func(t *testing.T) {
		task := eval.Task{
			Scoring: eval.Scoring{Type: "unknown_type"},
		}
		score, calls := eval.ScoreTask(task, "a.Foo b.Bar")
		if score != 0 {
			t.Errorf("want score=0 for unknown type, got %.4f", score)
		}
		if calls != 0 {
			t.Errorf("want calls=0, got %d", calls)
		}
	})
}

// ---------------------------------------------------------------------------
// extractSymbols
// ---------------------------------------------------------------------------

func TestExtractSymbols(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "two qualified names space-separated",
			in:   "foo.Bar baz.Qux",
			want: []string{"foo.Bar", "baz.Qux"},
		},
		{
			name: "with surrounding parens and comma",
			in:   "(pkg.Func), other.Method",
			want: []string{"pkg.Func", "other.Method"},
		},
		{
			name: "backtick wrapped",
			in:   "`mod.Sym`",
			want: []string{"mod.Sym"},
		},
		{
			name: "leading dot excluded",
			in:   ".prefix",
			want: nil,
		},
		{
			name: "trailing dot excluded",
			in:   "suffix.",
			want: nil,
		},
		{
			name: "no dot",
			in:   "plain word here",
			want: nil,
		},
		{
			name: "empty input",
			in:   "",
			want: nil,
		},
		{
			name: "newline-separated",
			in:   "a.b\nc.d",
			want: []string{"a.b", "c.d"},
		},
		{
			name: "trailing punctuation trimmed",
			in:   "func.Call;",
			want: []string{"func.Call"},
		},
		{
			name: "double-quote wrapped",
			in:   `"mod.Symbol"`,
			want: []string{"mod.Symbol"},
		},
		{
			name: "comma-separated list",
			in:   "a.Alpha,b.Beta,c.Gamma",
			want: []string{"a.Alpha", "b.Beta", "c.Gamma"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := eval.ExtractSymbols(tc.in)
			if len(got) == 0 && (tc.want == nil || len(tc.want) == 0) {
				return // both empty / nil — pass
			}
			if len(got) != len(tc.want) {
				t.Errorf("len mismatch: want %v, got %v", tc.want, got)
				return
			}
			wantSet := map[string]struct{}{}
			for _, w := range tc.want {
				wantSet[w] = struct{}{}
			}
			for _, g := range got {
				if _, ok := wantSet[g]; !ok {
					t.Errorf("unexpected symbol %q in output %v", g, got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// jsonString
// ---------------------------------------------------------------------------

func TestJSONString(t *testing.T) {
	t.Run("struct marshals to JSON", func(t *testing.T) {
		type inner struct {
			Name string `json:"name"`
			Val  int    `json:"val"`
		}
		v := inner{Name: "test", Val: 42}
		got := eval.JSONString(v)
		var decoded inner
		if err := json.Unmarshal([]byte(got), &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if decoded.Name != "test" || decoded.Val != 42 {
			t.Errorf("roundtrip mismatch: %+v", decoded)
		}
	})

	t.Run("nil marshals to null", func(t *testing.T) {
		got := eval.JSONString(nil)
		if got != "null" {
			t.Errorf("want \"null\", got %q", got)
		}
	})

	t.Run("slice marshals correctly", func(t *testing.T) {
		got := eval.JSONString([]string{"a", "b"})
		if got != `["a","b"]` {
			t.Errorf("want [\"a\",\"b\"], got %q", got)
		}
	})
}

// ---------------------------------------------------------------------------
// writeCSV
// ---------------------------------------------------------------------------

func TestWriteCSV(t *testing.T) {
	t.Run("nonexistent dir returns error", func(t *testing.T) {
		path := filepath.Join("/nonexistent/dir/that/does/not/exist", "results.csv")
		err := eval.WriteCSV(path, nil)
		if err == nil {
			t.Error("want error when parent dir does not exist, got nil")
		}
	})

	t.Run("empty rows produces header only", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "results.csv")
		if err := eval.WriteCSV(path, nil); err != nil {
			t.Fatalf("writeCSV: %v", err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open csv: %v", err)
		}
		defer f.Close()
		records, err := csv.NewReader(f).ReadAll()
		if err != nil {
			t.Fatalf("parse csv: %v", err)
		}
		if len(records) != 1 {
			t.Errorf("want 1 row (header), got %d", len(records))
		}
		header := records[0]
		expectedCols := []string{"task_id", "baseline", "input_tokens", "output_tokens",
			"cached_tokens", "score", "latency_ms", "num_tool_calls", "stale"}
		if len(header) != len(expectedCols) {
			t.Errorf("header cols: want %v, got %v", expectedCols, header)
		}
		for i, c := range expectedCols {
			if i < len(header) && header[i] != c {
				t.Errorf("header[%d]: want %q, got %q", i, c, header[i])
			}
		}
	})

	t.Run("two rows written and readable", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "results.csv")
		rows := []eval.Result{
			{
				TaskID: "T01", Baseline: eval.BaselineAlpha,
				InputTokens: 100, OutputTokens: 50, CachedTokens: 10,
				Score: 0.75, LatencyMS: 1234, NumToolCalls: 0, Stale: false,
			},
			{
				TaskID: "T02", Baseline: eval.BaselineDelta,
				InputTokens: 200, OutputTokens: 80, CachedTokens: 20,
				Score: 0.5, LatencyMS: 5678, NumToolCalls: 3, Stale: true,
			},
		}
		if err := eval.WriteCSV(path, rows); err != nil {
			t.Fatalf("writeCSV: %v", err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open csv: %v", err)
		}
		defer f.Close()
		records, err := csv.NewReader(f).ReadAll()
		if err != nil {
			t.Fatalf("parse csv: %v", err)
		}
		if len(records) != 3 { // header + 2 rows
			t.Fatalf("want 3 records, got %d", len(records))
		}
		// Row 1 (index 1): T01
		r1 := records[1]
		if r1[0] != "T01" {
			t.Errorf("row1 task_id: want T01, got %q", r1[0])
		}
		if r1[1] != "alpha" {
			t.Errorf("row1 baseline: want alpha, got %q", r1[1])
		}
		if r1[5] != "0.7500" {
			t.Errorf("row1 score: want 0.7500, got %q", r1[5])
		}
		if r1[6] != "1234" {
			t.Errorf("row1 latency_ms: want 1234, got %q", r1[6])
		}
		// Row 2 (index 2): T02
		r2 := records[2]
		if r2[0] != "T02" {
			t.Errorf("row2 task_id: want T02, got %q", r2[0])
		}
		if r2[1] != "delta" {
			t.Errorf("row2 baseline: want delta, got %q", r2[1])
		}
		if r2[5] != "0.5000" {
			t.Errorf("row2 score: want 0.5000, got %q", r2[5])
		}
		if r2[7] != "3" {
			t.Errorf("row2 num_tool_calls: want 3, got %q", r2[7])
		}
		if r2[8] != "true" {
			t.Errorf("row2 stale: want true, got %q", r2[8])
		}
	})
}

// ---------------------------------------------------------------------------
// dumpFiles
// ---------------------------------------------------------------------------

func TestDumpFiles(t *testing.T) {
	// create a tmp directory with files of varying extensions
	tmp := t.TempDir()
	writeFile := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(tmp, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	body200 := strings.Repeat("x", 200)
	writeFile("a.go", body200)
	writeFile("b.ts", body200)
	writeFile("c.sol", body200)
	writeFile("d.txt", body200)
	writeFile("e.md", body200)

	// nested file
	nested := filepath.Join(tmp, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(nested, "f.go"), []byte(body200), 0o644); err != nil {
		t.Fatalf("write nested/f.go: %v", err)
	}

	t.Run("only go/ts/sol included txt/md excluded", func(t *testing.T) {
		out := eval.DumpFiles(tmp, 10, 4000)
		if strings.Contains(out, "d.txt") {
			t.Error("d.txt should be excluded")
		}
		if strings.Contains(out, "e.md") {
			t.Error("e.md should be excluded")
		}
		if !strings.Contains(out, "a.go") {
			t.Error("a.go should be included")
		}
		if !strings.Contains(out, "b.ts") {
			t.Error("b.ts should be included")
		}
		if !strings.Contains(out, "c.sol") {
			t.Error("c.sol should be included")
		}
	})

	t.Run("nested go file included via Walk", func(t *testing.T) {
		out := eval.DumpFiles(tmp, 10, 4000)
		if !strings.Contains(out, "f.go") {
			t.Error("nested/f.go should be included via Walk recursion")
		}
	})

	t.Run("count limits number of files", func(t *testing.T) {
		out := eval.DumpFiles(tmp, 2, 4000)
		// Count the number of "===" header occurrences
		count := strings.Count(out, "===")
		// Each file header has "=== name ===" → 2 occurrences per file
		// so total === count / 2 gives file count
		fileCount := count / 2
		if fileCount > 2 {
			t.Errorf("count=2 should limit to 2 files, got %d file headers", fileCount)
		}
	})

	t.Run("perFileLimit truncates content", func(t *testing.T) {
		out := eval.DumpFiles(tmp, 10, 50)
		// Split by file sections and check each body is at most 50 bytes
		parts := strings.Split(out, "\n=== ")
		for i, part := range parts {
			if i == 0 {
				continue // before first header
			}
			// part looks like: "path/a.go ===\n<body>\n"
			headerEnd := strings.Index(part, "===\n")
			if headerEnd < 0 {
				continue
			}
			body := part[headerEnd+4:]
			// trim trailing newline for measurement
			body = strings.TrimRight(body, "\n")
			if len(body) > 50 {
				t.Errorf("file body exceeds 50 bytes (got %d): %q", len(body), body)
			}
		}
	})

	t.Run("nonexistent root returns empty string", func(t *testing.T) {
		out := eval.DumpFiles("/path/that/does/not/exist/at/all", 5, 4000)
		if out != "" {
			t.Errorf("want empty string for nonexistent root, got %q", out)
		}
	})

	t.Run("unreadable go file is skipped", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root can read any file; skip chmod test")
		}
		dir := t.TempDir()
		f := filepath.Join(dir, "secret.go")
		if err := os.WriteFile(f, []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := os.Chmod(f, 0o000); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(f, 0o644) })
		// dumpFiles should silently skip the unreadable file (returns nil from Walk fn)
		out := eval.DumpFiles(dir, 5, 4000)
		if strings.Contains(out, "secret.go") {
			t.Errorf("unreadable file should be skipped, got: %q", out)
		}
	})
}
