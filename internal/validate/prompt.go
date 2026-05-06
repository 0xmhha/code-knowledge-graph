package validate

// Prompt is the typed unit the LLMValidator emits (in dry-run mode) or
// would send to a real LLM (in V1+ wired mode). The shape mirrors the
// citation-first pattern used by pkg/smartctx: every Prompt MUST cite at
// least one source location so the operator (or eventual LLM) can verify
// the claim independently. Fields are deliberately simple strings/slices
// to keep JSON-encoding for future API calls trivial.
//
// Why this exists separately from Issue: an Issue is a finding (already
// judged), a Prompt is a request for judgment. Keeping them distinct
// lets dry-run mode emit prompts as Info issues today and lets a future
// wiring layer post the same struct to an LLM endpoint without rewriting
// the sampler. Both layers share the Subject string so a real LLM
// response can be rejoined with the prompt that produced it.
type Prompt struct {
	// Task names the check kind. Stable identifiers a future router
	// can switch on: "edge-plausibility", "sparse-subgraph",
	// "citation-freshness".
	Task string
	// Subject is a short ID describing what is being judged. For edge
	// checks it is "calls:src=<qname>:dst=<qname>"; for node checks it
	// is the qualified name of the node.
	Subject string
	// Citations are the file:line references the LLM should consult.
	// At least one entry is required; samplers that cannot produce a
	// citation MUST skip the candidate rather than emit a citationless
	// prompt.
	Citations []Citation
	// Question is the actual prompt text. Kept concise (50-150 chars)
	// because the model still needs context budget for the citations.
	Question string
	// ResponseSchema is a description (or JSON-schema fragment) of the
	// expected response shape. The wiring layer passes this as the
	// system message so the LLM returns parseable JSON instead of prose.
	ResponseSchema string
}

// Citation is a single file:line reference. Snippet is optional — the
// dry-run path leaves it empty (the operator looks up the file
// themselves). The future wired path will populate Snippet via
// store.GetBlob so the LLM sees the actual source text.
type Citation struct {
	File    string
	Line    int
	Snippet string
}
