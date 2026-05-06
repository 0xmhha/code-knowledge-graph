// Package validate provides post-build inspectors that audit a constructed
// graph for integrity issues. Two complementary stages exist: SchemaValidator
// (deterministic, fast — checks empty values, FK consistency, edge-type
// semantic invariants) and LLMValidator (LLM-as-judge, slower — for cases
// where deterministic rules can't capture intent, such as "this calls edge
// looks like it should be an invokes edge").
//
// The Validator interface is the shared contract so `ckg validate` can run
// any subset over the same graph without touching the orchestrator each
// time a new check is added.
package validate

import (
	"context"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// Severity levels for validation issues. Error and Warning are operator-
// actionable; Info is purely descriptive (e.g. "skipped 2 stdlib refs").
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

// Issue is one finding from a validator. Most fields are optional — a
// validator should fill what it can and leave the rest empty.
type Issue struct {
	// Severity classifies how the operator should react.
	Severity string
	// Code is a stable identifier for the check (e.g. "empty-qname",
	// "dangling-edge"). Used by CLI filters and dashboards.
	Code string
	// Message is a human-readable description of what went wrong.
	Message string
	// NodeID, when set, points to the offending node.
	NodeID string
	// EdgeKey, when set, identifies an edge as "type:src:dst:line".
	EdgeKey string
	// FilePath helps the operator locate the issue in source.
	FilePath string
}

// Report aggregates issues from a single validator pass.
type Report struct {
	Validator string
	Issues    []Issue
}

// CountBySeverity groups issues by their Severity. Cheap helper for CLI
// output ("N errors / M warnings").
func (r *Report) CountBySeverity() map[string]int {
	out := make(map[string]int, 3)
	for _, iss := range r.Issues {
		out[iss.Severity]++
	}
	return out
}

// HasErrors reports whether at least one Issue has SeverityError. Used by
// `ckg validate` to set the exit code.
func (r *Report) HasErrors() bool {
	for _, iss := range r.Issues {
		if iss.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Validator inspects a graph for integrity issues. Implementations MUST be
// read-only — never mutate g or store. Concurrent calls to a single
// Validator are not required to be safe; the orchestrator runs validators
// sequentially.
type Validator interface {
	// Name returns a short stable identifier (e.g. "schema", "llm").
	// Surfaced in CLI output and JSON reports.
	Name() string
	// Validate produces a Report. ctx is honoured for cancellation;
	// long-running validators (LLM) should poll ctx.Done(). store may be
	// nil when the caller has only an in-memory graph — validators that
	// need persisted data (manifest, blobs) should detect nil and return
	// an Info issue documenting the skipped check.
	Validate(ctx context.Context, g *graph.Graph, store persist.StoreReader) (*Report, error)
}

// edgeKey formats an edge identity in the same shape Issue.EdgeKey uses
// across validators: "type:src:dst:line". Helper to keep formatting
// consistent.
func edgeKey(et, src, dst string, line int) string {
	return et + ":" + src + ":" + dst + ":" + itoa(line)
}

// itoa is a tiny dependency-free int-to-string. fmt.Sprintf would pull
// runtime/internal locale handling for one digit conversion; this avoids it.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Compile-time assertion that the SchemaValidator (defined in schema.go)
// satisfies Validator. Mirrored for LLMValidator once that lands.
var _ Validator = (*SchemaValidator)(nil)
