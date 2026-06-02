// Package concurrency computes the concurrency blast radius of a symbol:
// every module that affects or is affected by it via the contract's five
// concurrency edge types (spawns, sends_to, recvs_from, acquires_lock,
// accessed_under_lock), BFS in both directions over a store.Reader.
//
// It backs the cks.context.concurrency_impact tool (R1' 00 C1, S1). cks
// calls Analyze in-process; the dev-only ckg MCP server may also expose it
// via pkg/mcphandlers.RegisterConcurrencyImpact.
//
// Direction semantics on each returned Module:
//
//	"affected_by" — forward edge: seed -> ... -> module
//	"affects"     — reverse edge: module -> ... -> seed
//	"both"        — reached in both traversals
//
// Design notes:
//
//   - releases_lock is intentionally excluded. The unlock half of a lock
//     pair adds no "what is affected" signal over acquires_lock.
//
//   - store.Reader.NeighborhoodByQname is single-direction per call, so a
//     FUNCTION seed reaches its own goroutine and the channel it writes,
//     but NOT the peer goroutine across the channel (crossing a channel
//     requires switching edge direction at the Channel node). To recover
//     producer<->consumer or lock-sharing peers, seed a Channel / Mutex /
//     Field node instead. This makes the byzantine-fairness query "what
//     else touches this field/mutex under the same lock" (R1' 00 sec 4.3
//     L2) work by seeding the Field/Mutex node and reading "affects".
//
//   - Channel and lock edges are usually INFERRED confidence; they are
//     surfaced (not filtered to EXTRACTED), or concurrency impact would be
//     empty on real Go code.
package concurrency
