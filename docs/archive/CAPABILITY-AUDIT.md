# CKG Capability Audit — 2026-05-25

> **ARCHIVED 2026-07-18.** Every capability gap and open decision this audit
> tracked is now closed in code (W-B `awaits` + W-C `overrides` emission,
> Korean/CJK `search_text` test, `ckg-NEW-9` bm25 external-import test, Stage-B
> ckv-mirror 12/12). Retained as the historical north-star → gap mapping.
> Live status → `docs/CONTINUITY.md`.

> **Goal of this document**: map the user-articulated north star
> (*"build a 6-axis graph DB + answer keyword queries at 100% accuracy"*)
> against what the code does today, and enumerate the concrete work that
> closes the gap.
>
> **Scope**: ckg only. cks-side orchestration, ckv-side vocab bridge, and
> coding-agent loop are out of scope here. They are addressed in
> `eval/stablenet/CKS-INTEGRATION-2026-05-23.md`.
>
> **Method**: each user requirement → current capability evidence (with
> file paths) → gap → concrete work item with size + priority.

## 1. North-star recap

The user articulated three requirements (2026-05-25):

1. **R-Build**: given a project path + an optional list of source files
   that are *actually used* in the build binary, write a graph DB
   covering the six axes (structural / semantic / execution / concurrency /
   distributed / temporal).
2. **R-Query**: query the graph DB with a single keyword OR multiple
   keywords combined with AND/OR.
3. **R-Accuracy**: keyword queries return *only the related code* —
   target precision and recall both at 100% on the chosen benchmark.

The first step ("ckg functionally complete" before cks integration)
collapses to: **(R-Build) + (R-Query) + (R-Accuracy at 100%)**.

## 2. R-Build: source → 6-axis graph DB

### 2.1 Current capability

| Capability | Evidence | Status |
|---|---|---|
| `--src <path>` source root | `cmd/ckg/build.go:47` | ✅ |
| `--out <dir>` output directory | `cmd/ckg/build.go:48` | ✅ |
| `--lang auto\|go,ts,sol` language selection | `cmd/ckg/build.go:49` | ✅ |
| **`--files-from <path>` JSON `{include, exclude}` glob patterns** | `cmd/ckg/build.go:58-59`; pattern format: `internal/filterlist/` package | ✅ |
| 7-pass pipeline | `internal/buildpipe/pipeline.go::Run` | ✅ |
| Incremental cache | `internal/buildpipe/cache.go`; `--no-cache` bypass | ✅ |
| PostgreSQL backend opt-in | `cmd/ckg/build.go:54-55` `--db postgres://...` | ✅ |
| Strict validation gate | `cmd/ckg/build.go:56` `--strict-validate` | ✅ |
| 6-axis edge enums | `pkg/types/enums.go:148-339` (40 EdgeTypes incl. slot-reserved) | ✅ |

### 2.2 6-axis emission matrix

| Axis | Edge type | Emitter | Status |
|---|---|---|---|
| **Structural** | contains | all parsers | ✅ |
|  | defines | all parsers | ✅ |
|  | imports | all parsers | ✅ |
|  | exports | TS parser, Go pkg exports | ✅ |
| **Semantic** | references | all parsers | ✅ |
|  | implements | TS heritage, Sol implements | ✅ |
|  | extends | TS heritage, Sol inheritance | ✅ |
|  | uses_type | Go uses_type.go, TS, Sol declarations | ✅ |
|  | reads_field / writes_field | Go statements.go, TS, Sol | ✅ |
|  | has_modifier | Go, TS (decorators), Sol (function modifiers) | ✅ |
|  | has_decorator | TS decorators | ✅ |
|  | emits_event | Sol emit statements | ✅ |
|  | reads_mapping / writes_mapping | Sol mapping ops | ✅ |
| **Execution** | calls | all parsers | ✅ |
|  | invokes | all parsers, with `dispatch_kind` metadata (schema 1.7) | ✅ |
| **Concurrency** | spawns | Go GoStmt | ✅ |
|  | sends_to / recvs_from | Go SendStmt / UnaryExpr ARROW | ✅ |
|  | timeout_path / cancellation_path | Go P2 (context.WithTimeout/WithCancel) | ✅ |
|  | **acquires_lock / releases_lock / accessed_under_lock** | B1 Stage 1 live: `internal/parse/golang/concurrency.go` (Mutex nodes + lock edges) + `concurrency_underlock.go` (field × mutex pairs). Self-index: 19 acquires_lock / 19 releases_lock / 40 accessed_under_lock / 5 Mutex nodes (2026-05-26 verification) | ✅ |
| **Distributed** | listens_on | Go net/http HandleFunc/Handle, mux variants | ✅ |
|  | handles_message | Go net/rpc handler signature match | ✅ |
|  | rpc_calls | Go client.Call(...) | ✅ |
|  | binds_to | `internal/link/sol_to_ts.go` cross-language Sol ABI → TS | ✅ |
|  | http_calls (W2) | Go http client + TS fetch/axios/useSWR/useQuery (schema 1.9) | ✅ |
|  | grpc_listens_on / grpc_calls (W3b) | Go gRPC server impl + client stubs (schema 1.9) | ✅ |
| **Temporal** | changed_in | post-Build temporal pass (git log --raw) | ✅ |
|  | blame | File node → most recent commit | ✅ |
|  | has_hunk / adjacent (H1, schema 1.8) | unified-diff @@ headers per commit | ✅ |
|  | modifies (H2, schema 1.8) | hunk interval overlap with 13 whitelisted CodeNode kinds | ✅ |
| **Emitted (was 1.10 slot)** | awaits (W-B) | ✅ emitted since `0866ef0` (2026-05-11): `internal/parse/typescript/async.go:120` (`emitAwaitPoint`), wired at `declarations.go:92`. Verified: a build of async TS yields `awaits` edges. | ✅ |
|  | overrides (W-C) | ✅ emitted since `be80e3d` (2026-05-18): `internal/parse/solidity/overrides.go:200` + `resolve.go:1173`. Verified: a build of a Sol `override` yields `overrides` edges. | ✅ |

### 2.3 R-Build gaps

| Gap | Impact on north star | Recommended work | Priority |
|---|---|---|---|
| Concurrency `acquires_lock` family — **CLOSED** (docs were drift) | The Stage 1 implementation has been in tree for some time (`internal/parse/golang/concurrency.go` + `concurrency_underlock.go`); SCHEMA.md / PROJECT-OVERVIEW were stale. Synthetic corpus had no Mutex fixture so retrieval regression cover was absent. | **R11/R12 retrieval fixtures + synthetic concurrent.go** lock the emission against future drift. SSA cross-function chain (`--lock-propagation`, W-A Stage B) remains a separate opt-in surface. | n/a (already done) |
| W-B `awaits` (TS async/await suspension) unimplemented | TS programs without async-flow detection have broken concurrency story for the JS side of any node/express service. | W-B W2 detector — `~700 LOC` per design (`docs/design/ts-async-await-and-interface.md`) | P1 (cks Stage B may surface need) |
| W-C `overrides` (Sol virtual/override) unimplemented | Sol contracts with inheritance show wrong call dispatch — false negatives for any override chain. | W-C W2 detector — `~1100-1200 LOC` per design | P1 |
| `--files-from` schema documentation | Today the format is "JSON `{include, exclude}` glob patterns" but the doc lives only in `cmd/ckg/build.go:58-59` flag string. Users compose lists wrong (e.g. expecting newline-separated paths). | Single docs entry under `docs/BUILD.md` (new) or `INCREMENTAL.md` (existing). Add a `--files-from-example` flag dumping the schema. | P2 |
| audit subcommand parity test for `--files-from` paths | `ckg audit` currently parities `go/packages.Load` vs DB but does not validate that `--files-from` restricted builds didn't drop edges incorrectly. | Extend `internal/audit/` with a `--files-from`-aware mode. | P1 |

## 3. R-Query: single + multiple keyword (AND/OR)

### 3.1 Current capability

| Surface | Single keyword | Multi-keyword OR | Multi-keyword AND |
|---|---|---|---|
| MCP `find_symbol` | ✅ (qname exact or suffix match; `language` filter pushdown) | n/a — name-based, not keyword | n/a |
| MCP `search_text` | ✅ (FTS5 + auto-prefix; CJK substring fallback) | ✅ default (multi-token OR-joined prefix tags) | ✅ **EXPOSED** — `mode="and"` param (`pkg/mcphandlers/handlers.go:176,185`) → `filterHitsByAllTokens` (`internal/persist/sqlite_fts.go:79-82`) |
| MCP `evidence_for_intent` | ✅ (BM25 over subject/patch/modifies-qnames virtual doc) | ✅ (BM25 ranks intersection-of-meaning candidates) | ✅ via `Mode: "and"` (`pkg/evidence/cache.go:119`, `filterByAllTokensPresent`) — but **this is for evidence/PR retrieval**, not generic code search |
| HTTP `/api/search` | ✅ | ✅ default OR | ✅ via `SearchFTSOptions{Mode:"and"}` (same store path as the MCP tool) |
| HTTP `/api/evidence?mode=and\|or` | ✅ | ✅ | ✅ (BM25 → AND post-filter); `mode=or` is the default |
| `pkg/store.Reader.SearchFTS` | ✅ + `SearchFTSOptions{Language}` | ✅ via power-user mode (raw FTS5 query containing `AND`/`OR`/`*`/`"`) | ✅ via power-user mode (caller writes `foo AND bar`) |
| `pkg/store.Reader.FindSymbol` | ✅ + `FindSymbolOptions{Language, Kinds[]}` | n/a | n/a |

### 3.2 R-Query gaps

| Gap | Impact | Recommended work | Priority |
|---|---|---|---|
| ~~MCP `search_text` does not expose AND/OR mode~~ | **✅ DONE (verified 2026-07-10).** `search_text` takes `mode="or"` (default) / `mode="and"` — `pkg/mcphandlers/handlers.go:176,185`; AND post-filter is `filterHitsByAllTokens` (`internal/persist/sqlite_fts.go:79-82`, mirrored in `postgres_store.go:1131`). | — | ~~P0~~ → done |
| ~~`pkg/store` lacks an idiomatic AND/OR query API~~ | **✅ DONE (verified 2026-07-10).** `pkg/store` exposes `SearchFTSOptions{Mode, Language, NodeKinds}` (`pkg/store/store.go:53-55` → `persist.SearchFTSOptions`) and `Reader.SearchWithOpts(q, limit, opts)`; external consumers pass `Mode:"and"` instead of hand-building FTS5 syntax. | — | ~~P0~~ → done |
| ~~Multi-keyword retrieval fixtures absent~~ | **✅ DONE (verified 2026-07-10).** `eval/retrieval/` carries `R06`(OR), `R07`(three-token AND), `R08`/`R09`(language filter), `R10`(strict-go AND), `R14`(statement opt-in). | — | ~~P0~~ → done |
| Korean / CJK query graceful degradation untested | `Search()` routes non-ASCII through `SearchSubstr` LIKE-based path. Behaviour on mixed Korean + English (the real cks-via-ckv input shape) untested. ckg-NEW-1 lives here. | Add `TestFTS5Query_KoreanInput_Graceful` covering the 3 mix patterns from `CKS-INTEGRATION-2026-05-23 §3.1`. Estimate: ~80 LOC test only (no behavioural change unless a panic is found). | P1 |

## 4. R-Accuracy: keyword query at 100%

### 4.1 Current accuracy baseline

Source: `eval/baseline/retrieval.json` (current; the 2026-05-20 EV1 Phase 2
snapshot below is superseded by the AND-mode fixtures).

| Fixture | Tool | Recall | Precision | Pass |
|---|---|---|---|---|
| R01-find-callers-vault-deposit | find_callers | 1.00 | 1.00 | ✅ |
| R04-find-symbol-vault | find_symbol | 1.00 | 1.00 | ✅ |
| R05-search-text-vault-deposit-and-go | search_text (AND) | **1.00** | **1.00** | ✅ |
| R07-search-text-three-token-and | search_text (AND) | 1.00 | 1.00 | ✅ |
| R10-search-text-deposit-strict-go | search_text (AND) | 1.00 | 1.00 | ✅ |
| R06-search-text-vault-or-deposit-go | search_text (OR) | 1.00 | 0.94 | ✅ |

**Update (2026-07-10):** the "precision shortfall" that once blocked the
*user-listed 100% precision goal* is closed for keyword search. With `mode="and"`
the AND fixtures (R05/R07/R10) hit **R=P=1.00** — the AND post-filter
(`filterHitsByAllTokens`) drops the over-recall that the OR default produces. OR
mode intentionally trades precision for recall (R06 P≈0.94), which is the correct
default for exploratory queries. The keyword-search accuracy gap is resolved.

### 4.2 What "100% accuracy on the user benchmark" requires

The synthetic corpus has 8 files. Real measurement must run on
go-stablenet (2,142 files) with ckv-mirror fixtures (12 PRs from
`CKS-INTEGRATION-2026-05-23 §6`).

| Capability | Current | Target |
|---|---|---|
| Fixture count | 5 (synthetic) | 5 + 12 (ckv mirror) + 4-6 (multi-keyword) = **21-23** |
| Corpus | `eval/.synthetic-data` | + `go-stablenet-latest` (existing graph at `/tmp/ckg-stablenet`) |
| Tool coverage | find_callers / find_callees / find_symbol / search_text | + evidence_for_intent / impact_of_change |
| Aggregate F1 target | 0.75 | **1.00** with sensible precision/recall floors per fixture |
| Multi-keyword AND/OR coverage | 0 fixtures | At least 4 mode=and + 2 mode=or fixtures |

### 4.3 R-Accuracy gaps

| Gap | Recommended work | Priority |
|---|---|---|
| `search_text` precision shortfall (R05) | **✅ done (2026-05-26)** via the X-NodeKinds work — `SearchFTSOptions.NodeKinds` whitelist with a default symbol-only filter strips statement / Commit / Hunk / Import / Export rows at the SQL layer. R05/R06/R07/R08/R10 expecteds narrowed accordingly, aggregate R=P=F1=1.00 across 12 fixtures. | done |
| Stage B harness over 12 ckv-fixture-mirror tasks | **ckg-NEW-5 + ckg-NEW-8** from CKS-INTEGRATION. YAML authoring + harness wiring. Stage B then measures the 14-task × 4-baseline matrix on the real corpus. | P0 |
| Retrieval scorer cannot distinguish "off-target" candidates from "ranked-lower-but-relevant" | Current scorer is symbol-set match. Multi-keyword AND/OR queries return ranked lists where MRR matters. Add an optional `top_k` field on `Scoring` and an MRR scoring path. | P1 |
| No regression alert when precision drops | `make eval` exits 0 even when precision dips. The CI gate (EV1 Phase 3, deferred for manual user application) reads recall_min/precision_min thresholds but defaults to 0 for `search_text`. Need a recommended-threshold scan that fails when committed thresholds tighten and break. | P1 (rolls in with EV1 Phase 3) |

## 5. Cross-cutting blockers (cks integration)

These do not affect ckg's standalone capability but block the *step after*
("ckg functionally complete → integrate with cks"):

| ID | Work | Source doc | Status |
|---|---|---|---|
| T-14 | `pkg/mcphandlers/` public registration surface — currently cks cannot import the 8-tool registrations because they live in `internal/mcp/`. cks S1 entry blocker. | `eval/stablenet/HANDOFF.md` | **✅ done (2026-05-26)** — pkg/mcphandlers with 8 Register*, RegisterAll, NewLLMSafeReader, and cks-style smoke tests. internal/mcp kept as the production path; cleanup PR (T-14b) tracks the eventual shim migration. |
| ckg-NEW-2/3/4 | PR breadcrumb metadata on Node + temporal slicing + accessor (R12) | `eval/stablenet/CKS-INTEGRATION-2026-05-23.md §3.2` | **✅ done (2026-05-26)** — `pkg/types.PRRef`, `Node.RecentPRs`, `node_prs` table (schema 1.12), `Reader.GetNodePRs(nodeID, cutoff)`, `internal/buildpipe.ScanPRHistory` (git log scan with `(#NNN)` regex + patch line-range overlap + remote.origin.url → owner/repo derivation). |
| ckg-NEW-9 | `pkg/bm25` external-import stability (R13 3-leg) | CKS-INTEGRATION §3.5 | P1, open |

## 6. Recommended sequencing

Two valid lanes; both deliver R-Build + R-Query + R-Accuracy progress but
along different shape:

### Lane X — Capability-first (deliver "100% accuracy on keyword query")

1. **MCP `search_text` mode=and/or** (~80 LOC + 2 tests) — §3.2 row 1
2. **`pkg/store.SearchKeywords` API + tests** (~80 LOC) — §3.2 row 2
3. **Multi-keyword retrieval fixtures** (R06-R10, YAML only) — §3.2 row 3
4. **Tighten `rewriteFTSQuery` / repurpose R05 to AND mode** — §4.3 row 1
5. **Korean graceful test** (ckg-NEW-1, ~80 LOC) — §3.2 row 4
6. **Re-run `make eval` → confirm aggregate F1 = 1.00 over expanded fixture set**
7. **B1 Wave 5 — acquires_lock / accessed_under_lock parser emission** (~400 LOC) — §2.3 row 1

**Outcome**: ckg standalone achieves the user-listed first-step goal.
6 axes all emit, AND/OR query works, accuracy bar is hit on the expanded
benchmark.

### Lane Y — Integration-first (deliver "cks unblock")

1. **T-14** — `pkg/mcphandlers/` surface
2. **ckg-NEW-2/3/4** — PR breadcrumb (one PR with T-14)
3. **ckg-NEW-9** — bm25 external import stability
4. **ckg-NEW-5** — 12 ckv-fixture mirror YAMLs
5. **ckg-NEW-8** — Stage B harness
6. **T-04 V4** — 30-question hallucination measurement
7. **Stage C** — cks integration (out of ckg scope)

**Outcome**: cks can compose ckg without import friction; Stage B
measurement runs end-to-end; 30-question baseline lands.

### Recommended choice

**Lane X first**, then Lane Y. Reason: Lane X closes the user-articulated
*first-step success criterion* (6-axis + 100% accuracy) which is the
explicit precondition the user named. Lane X items 1-5 are 4-6 hours
total. Lane Y assumes capability completeness; running it before Lane X
risks cks integrating a ckg that still has the AND/OR + locks gap.

If a single PR sequence is preferred, items X-1 / X-2 / X-3 / X-4 share
the same files (`internal/mcp/tools.go`, `pkg/store/store.go`,
`eval/retrieval/*.yaml`, `internal/persist/sqlite.go::rewriteFTSQuery`)
and can ship as one ~200-300 LOC change with focused tests.

## 7. Open decisions for the user

1. **Lane X vs Lane Y first** (§6). Recommend Lane X. Tradeoff: Lane Y
   gives cks early visibility; Lane X gives ckg a tighter foundation.
2. **`search_text` AND mode behaviour**: post-filter (all tokens in
   matched node's name/qname/signature/doc — current `evidence` pattern)
   vs FTS5-native `AND` (`foo AND bar` syntax). Recommend post-filter
   for consistency with `evidence` Mode=and; cheaper to reason about.
3. **R05 disposition**: tighten `rewriteFTSQuery` (changes prose-query
   behaviour for everyone) vs convert R05 to AND mode (no behaviour
   change, fixture intent shifts). Recommend conversion.
4. **B1 timing**: include in Lane X (item 7) or defer to Lane Y prep?
   Locks are part of the user-listed 6-axis spec, so leaving them slot-
   reserved technically fails R-Build. Recommend keeping in Lane X.

Once decided, the working tracker is `docs/CONTINUITY.md §5` (next-action
queue) and the per-task spec lives in either CKS-INTEGRATION-2026-05-23
(Stream C) or todo-cks-dogfood-followups-2026-05-20 (Stream A historical).
