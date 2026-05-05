// Package eval is the LLM-based benchmark harness for CKG.
//
// It compares graph-context (β/γ/δ baselines) against raw-file context (α
// baseline) to validate the project's value proposition. The rest of CKG
// (build/serve/mcp/export-static/export-postgres/audit) is LLM-free; eval
// is the only part that pulls in LLM SDKs.
//
// EXTRACTION NOTE: this package is slated to move to a sister repo (e.g.
// code-knowledge-graph-eval). When that happens:
//
//   - Move:    internal/eval/, cmd/ckg/eval.go, eval/tasks/*.yaml
//   - Imports: already use pkg/store (this repo's public read surface);
//     the new module imports it as a regular Go module dep.
//   - Drop from this repo's go.mod:
//     github.com/anthropics/anthropic-sdk-go
//     github.com/0xmhha/cli-wrapper
//   - CI:      remove the CI=true skip in
//     llm_cli_test.go::TestCLIClient_Complete_Smoke_ClaudeFallback
//     once the new repo's CI runs the test reliably (the skip
//     was added to dodge a cli-wrapper Manager-reuse race).
//   - Open TODO at extraction time: smartContext in runner.go duplicates
//     the get_context_for_task MCP tool; extract to a shared
//     package (e.g. pkg/contextcompose) so both eval and mcp
//     share one implementation.
package eval
