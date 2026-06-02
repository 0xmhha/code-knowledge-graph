// cmd/ckg/evidence.go — `ckg evidence` runs the H3 EvidencePack
// assembler from a one-shot CLI invocation, no `ckg serve` required.
// Targets shell scripts, CI pipelines, and ad-hoc inspection where
// the long-running server would be overkill.
//
// The CLI mirrors the /api/evidence query shape:
//
//	ckg evidence --graph DIR [--intent TEXT] [--issue ID]
//	             [--seed-qname QNAME] [-k N] [--budget N] [--offset N]
//	             [--format json|text]
//
// At least one of --intent or --issue is required (matches the server-
// side check). text format emits a compact human-readable summary
// (commit subject + first few patch lines + issue badges); json emits
// the full pkg/evidence.Pack so downstream tooling can pipe through
// jq / Go templates / etc.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/0xmhha/code-knowledge-graph/internal/persist"
	"github.com/0xmhha/code-knowledge-graph/pkg/evidence"
)

func newEvidenceCmd() *cobra.Command {
	var (
		graph     string
		intent    string
		issueID   string
		seedQname string
		k         int
		budget    int
		offset    int
		format    string
		mode      string
	)
	cmd := &cobra.Command{
		Use:   "evidence",
		Short: "Run the H3 EvidencePack assembler from the CLI (no `ckg serve` needed)",
		Long: `Build an EvidencePack for a free-form intent and/or an issue ID,
without spinning up the embedded server. Mirrors the /api/evidence
query surface — intent for BM25 ranking, issue_id for ticket-scope
filtering, seed_qname for modifies-reach restriction.

At least one of --intent or --issue is required (the server-side
contract: a request with neither is a misconfigured caller, not a
"show everything" request).

Output formats:
  text — commit subject + first ~5 patch lines per hunk, issue badges,
         compact and shell-friendly.
  json — the full pkg/evidence.Pack JSON; pipe through jq for
         downstream tooling.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if intent == "" && issueID == "" {
				return fmt.Errorf("at least one of --intent or --issue is required")
			}
			if format != "json" && format != "text" {
				return fmt.Errorf("--format must be json or text, got %q", format)
			}
			if mode != "" && mode != "or" && mode != "and" {
				return fmt.Errorf("--mode must be or or and, got %q", mode)
			}
			db := filepath.Join(graph, "graph.db")
			store, err := persist.OpenReadOnly(db)
			if err != nil {
				return fmt.Errorf("open graph: %w", err)
			}
			defer func() { _ = store.Close() }()

			pack, err := evidence.NewCache().BuildPack(store, evidence.Options{
				Intent:       intent,
				IssueID:      issueID,
				SeedQname:    seedQname,
				K:            k,
				BudgetTokens: budget,
				Offset:       offset,
				Mode:         mode,
			})
			if err != nil {
				return fmt.Errorf("BuildPack: %w", err)
			}
			if format == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(pack)
			}
			renderEvidenceText(os.Stdout, pack)
			return nil
		},
	}
	cmd.Flags().StringVar(&graph, "graph", "", "graph directory containing graph.db (required)")
	cmd.Flags().StringVar(&intent, "intent", "",
		"free-text intent — BM25 ranks against (commit subject || patch || modifies-qnames)")
	cmd.Flags().StringVar(&issueID, "issue", "",
		"ticket id (e.g. GH-42, INGEST-789) — restricts hits to commits citing this ticket")
	cmd.Flags().StringVar(&seedQname, "seed-qname", "",
		"qualified name — restricts to hunks reaching this CodeNode via 1-hop calls/invokes")
	cmd.Flags().IntVarP(&k, "k", "k", 5, "top-K commits to return")
	cmd.Flags().IntVar(&budget, "budget", 6000,
		"stop emitting commits past this many tokens of cumulative patch text (4 chars/token)")
	cmd.Flags().IntVar(&offset, "offset", 0,
		"skip the first N commits in the recency-sorted result (paging)")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text | json")
	cmd.Flags().StringVar(&mode, "mode", "",
		"term-match strategy: or (default) — BM25 any-term-match; and — require every query token in the doc")
	_ = cmd.MarkFlagRequired("graph")
	return cmd
}

// renderEvidenceText emits a compact human-readable summary of the
// pack: per-commit header (SHA + subject + issue badges) followed by
// each hunk's file path and the first ~5 lines of its patch. Designed
// to be greppable and pipe-friendly while staying scannable in a
// 100-column terminal.
func renderEvidenceText(w io.Writer, pack *evidence.Pack) {
	if pack == nil || len(pack.Hits) == 0 {
		_, _ = fmt.Fprintln(w, "(no hits)")
		return
	}
	const previewLines = 5
	_, _ = fmt.Fprintf(w, "%d commit(s):\n\n", len(pack.Hits))
	for i, hit := range pack.Hits {
		issues := ""
		if len(hit.Commit.IssueIDs) > 0 {
			issues = " [" + strings.Join(hit.Commit.IssueIDs, ",") + "]"
		}
		_, _ = fmt.Fprintf(w, "%d. %s%s — %s\n",
			i+1, hit.Commit.SHA[:12], issues, hit.Commit.Subject)
		for _, h := range hit.Hunks {
			_, _ = fmt.Fprintf(w, "   %s L%d-%d:\n", h.FilePath, h.StartLine, h.EndLine)
			lines := strings.Split(h.PatchText, "\n")
			n := len(lines)
			if n > previewLines {
				n = previewLines
			}
			for _, line := range lines[:n] {
				_, _ = fmt.Fprintf(w, "     %s\n", line)
			}
			if len(lines) > previewLines {
				_, _ = fmt.Fprintf(w, "     … (%d more lines)\n", len(lines)-previewLines)
			}
		}
		_, _ = fmt.Fprintln(w)
	}
}
