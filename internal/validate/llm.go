package validate

import (
	"context"

	"github.com/0xmhha/code-knowledge-graph/internal/graph"
	"github.com/0xmhha/code-knowledge-graph/internal/persist"
)

// LLMValidator is the skeleton for the LLM-as-judge validator. It will
// surface findings that deterministic rules (SchemaValidator) cannot
// express — e.g. "this calls edge looks like it should be invokes given
// the surrounding context" or "the implements edges to interface X look
// suspiciously sparse compared to its known implementors".
//
// V0 is a no-op that returns a single Info issue documenting the planned
// behaviour. The decision (per user instruction ④ in the dogfood plan)
// is to start with `ckg validate` as a manual subcommand and integrate
// LLM checks once SchemaValidator findings prove stable. Until then the
// skeleton ensures the orchestrator surface (Validator interface, ckg
// validate flag, JSON report shape) is exercised end-to-end so adding
// real LLM calls later is a single-file change.
type LLMValidator struct {
	// Endpoint is the LLM API endpoint URL. Empty until the V1 wiring lands.
	Endpoint string
	// Model is the model identifier to call. Empty until V1.
	Model string
}

// NewLLMValidator returns a stateless skeleton instance.
func NewLLMValidator() *LLMValidator { return &LLMValidator{} }

// Name returns the validator identifier.
func (v *LLMValidator) Name() string { return "llm" }

// Validate currently no-ops with a single Info issue describing the V1
// scope. Real implementation will sample suspicious edges and ask an
// LLM to confirm/deny, recording each judgment as a Warning issue.
func (v *LLMValidator) Validate(ctx context.Context, g *graph.Graph, store persist.StoreReader) (*Report, error) {
	_ = ctx
	_ = g
	_ = store
	return &Report{
		Validator: v.Name(),
		Issues: []Issue{{
			Severity: SeverityInfo,
			Code:     "llm-not-yet-wired",
			Message:  "LLMValidator skeleton: real LLM checks are V1+; --llm flag accepted but produces no findings",
		}},
	}, nil
}

// Compile-time check that *LLMValidator satisfies Validator.
var _ Validator = (*LLMValidator)(nil)
