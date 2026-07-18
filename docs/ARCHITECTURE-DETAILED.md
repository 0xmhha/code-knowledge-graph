# CKG Architecture (2026-07-18)

**Document Date**: 2026-07-18  
**Status**: Current (post-#55 binary-reachable rescoping; G6 v4 incremental path live)  
**Schema Version**: 1.23 — authoritative in `internal/buildpipe/cache.go` (`const SchemaVersion`, cache-key contributor) and mirrored in docs/SCHEMA.md  
**Scope**: Single-binary `ckg` CLI — ~20 cobra subcommands (`cmd/ckg/root.go:30`) of which **five are production surfaces** (build, serve, mcp, eval, audit); 37 node types + 43 edge types (authoritative: `pkg/types/enums.go` `AllNodeTypes()`/`AllEdgeTypes()`, counts in docs/SCHEMA.md); 6 graph axes

---

## 1. System Overview

CKG is a single-binary, self-contained code knowledge graph builder and server:

```
Input (source code) → CLI build pipeline → SQLite graph.db → Multiple query surfaces
                           ↓ (build)              ↓          ├─ HTTP API + viewer
                      4 language parsers      Persist layer  ├─ MCP server (10 tools)
                      (Go/TS/Sol/Proto)       (SQLite)       ├─ Audit (verify parity)
                                                              └─ Export (static JSON)
```

### 1.1 Entry Points

**CLI**: `cmd/ckg/main.go` → `newRootCmd()` registers ~20 cobra subcommands
(`cmd/ckg/root.go:30-36`). Most are utilities (query, path, report, benchmark,
watch, export-*, bench-*, quickstart, evidence, validate). **Five are the
production surfaces** — everything else is a helper or a benchmark harness:

| Surface | Command | Purpose | Output | Key Package |
|---|---|---|---|---|
| `build` | `newBuildCmd` | Parse all files → graph.db | SQLite database + manifest.json | `internal/buildpipe` |
| `serve` | `newServeCmd` | HTTP server + embedded viewer | localhost:8080 (default) | `internal/server` |
| `mcp` | `newMCPCmd` | Model Context Protocol stdio server | 10 MCP tools | `internal/mcp` + `pkg/mcphandlers` |
| `eval` | `newEvalRetrievalCmd` (`ckg eval-retrieval`, `cmd/ckg/eval_retrieval.go`) | Keyword-retrieval accuracy eval | CSV + report.md | `internal/eval` |
| `audit` | `newAuditCmd` | Verify graph completeness vs. source | exit 0/1/2 | `internal/audit` |

> There is no `ckg eval` command and no `cmd/ckg/eval.go` — the eval surface is
> exposed as `eval-retrieval`. Static export is a utility subcommand
> (`export-static`, `export-json`, `export-postgres`), not a production surface.

### 1.2 Core Execution Model

Each build follows the **7-Pass Architecture** (spec §4.7):

| Phase | Name | Input | Output | Implementation |
|---|---|---|---|---|
| P1 | Detect | srcRoot | file list (Go/TS/Sol/Proto) | `internal/detect.Walk()` + `detect.GoFiles()` |
| P2 | Parse (per-lang) | files | nodes, edges, pending refs | Language parsers (Go/TS/Solidity/Proto — `internal/parse/{golang,typescript,solidity,proto}`) |
| P3 | Resolve (per-lang) | pending refs + nodes | resolved edges | `internal/parse.Resolve()` per language |
| P4 | Graph Build | per-lang ResolvedGraphs | unified graph (dedup) | `internal/graph.Build()` |
| P5 | Cross-Lang Link (G5) | Sol ABI + TS nodes | binds_to edges | `internal/link.SolToTS()` |
| P6 | Temporal (G6) | git history + nodes | changed_in/blame edges | `internal/temporal` |
| P7 | Derived (Cluster/Score) | unified graph | pkg/topic trees, PageRank | `internal/cluster` + `internal/score` |

---

## 2. Build Pipeline & Cache Routing

### 2.1 Cache Routing Decision Tree

Routing lives in `Run()` → the tail of `pipeline.go` (`pipeline.go:250-265`):

```
Run(Options) called
    │
    ├─ Is --no-cache set OR manifest unusable? → YES → runCold (full rebuild)
    │
    └─ Manifest usable (ManifestUsable): DiffManifest() classifies each file as:
        ├─ classDirty (SHA256 mismatch) ──┐
        ├─ classCached (hit) ─────────────┼→ IsAllCached()?
        └─ classRemoved (gone) ───────────┤
                                     ├─ YES: runShortCircuit
                                     │ (manifest timestamp refresh only)
                                     │
                                     └─ NO (partial hit):
                                         runIncremental  (pipeline.go:260)
                                         G6 v4 + C1 reverse-reference invalidation
                                         — LIVE, not a cold fallback
```

The earlier D4 cold-fallback escape hatch is **retired**: `runIncremental` is now
the live partial-hit path (`incremental.go:154`, dispatched at `pipeline.go:260`).
The old phantom-edge root cause (NodesByFilePath order ≠ AST declaration order) is
fixed — `NodesByFilePath` now sorts `ORDER BY start_line`
(`internal/persist/sqlite_reader.go:592`). See §2.4.

**Key Constants** (`internal/buildpipe/cache.go`):
- `const SchemaVersion = "1.23"` (`cache.go:166`) — the cache-key contributor.
  This is authoritative for the extraction schema; docs/SCHEMA.md mirrors it.
- Cache key = SHA256(file_content + "|ckg:VERSION|parser:VERSION|schema:1.23") (`cache.go:205`, `cache.go:217`)
- Parser version: Go ties to `runtime.Version()`; TS/Sol/Proto tie to the parser binding version

### 2.2 runCold Path (Full Rebuild)

**Main steps** (pipeline.go:143-251):

1. **Detect** → file lists for Go (go/packages.Load), TS (walk .{ts,js,tsx,jsx}), Sol (walk .sol), Proto (walk .proto)
2. **Parse per-language** → Go / TS / Solidity / Proto pipelines (`pipeline.go:347-360` runs the proto pass for schema 1.9+)
   - Go: `golang.org/x/tools/go/packages` + types.Info (build constraints honored)
   - TS/JS: tree-sitter + grammars
   - Solidity: vendored tree-sitter grammar + ABI extraction
   - Proto: hand-rolled lexer + recursive-descent parser (`internal/parse/proto/`, schema 1.9)
3. **Resolve** (Pass 2) → cross-file qname resolution, pending_refs → edges
4. **Graph Build** → `graph.Build()` (node dedup by ID, edge dedup by (Type, Src, Dst, Line))
5. **Derived Passes** → `emitDerivedPasses()`:
   - Cross-language link (G5): `link.SolToTS()` (Sol ABI → TS binds_to edges)
   - Temporal (G6): `emitTemporalEdges()` (git history → changed_in/blame)
   - Cluster: `cluster.BuildPkgTree()` + `cluster.BuildTopicTree()`
   - Score: `score.Compute()` (PageRank)
6. **Persist** (cold wipes DB first):
   - `openColdStore()` → `os.Remove(graph.db)` + reopen + migrate
   - `persistColdArtifacts()` → InsertNodes/Edges/PkgTree/TopicTree/Blobs
   - `InsertPendingRefs()` (schema 1.23) → per-file unresolved refs consumed by the next incremental build
   - `SetManifest()` → compute FileEntry[] (SHA256 + cache key per file)

### 2.3 runShortCircuit Path (Cache Hit, No Changes)

**Main steps** (pipeline.go:122-125):

- No parse needed. Load DB from prior build.
- Refresh manifest timestamp only (no Files recomputation).
- Load-bearing for CI re-runs on unchanged source.

### 2.4 runIncremental Path (Live — G6 v4 + C1)

**Status**: LIVE. Dispatched at `pipeline.go:260` for every partial cache hit;
implemented in `internal/buildpipe/incremental.go:154`.

**What it does** (`incremental.go` header + `runIncremental` flow comment):
- Parse only dirty files; reload cached node sets from the DB for untouched files;
  rerun Pass 2 / cluster / score across the merged graph.
- **C1 reverse-reference invalidation (implemented):** `store.ReverseDepsForFiles`
  (`sqlite_reader.go`) re-resolves cached files whose `pending_refs` target a
  dirty/removed file, while unaffected files keep their DB edges.

**H3 phantom-edge bug — fixed.** The former root cause (`NodesByFilePath` returning
nodes in rowid order rather than declaration order, which made ambiguous simple-name
resolution pick a different winner than cold and leak duplicate edges past
`(Type,Src,Dst,Line)` dedup) is resolved: `NodesByFilePath` now sorts
`ORDER BY start_line` (`internal/persist/sqlite_reader.go:592`). This is the "G6 v4"
fix referenced throughout `incremental.go`.

**Determinism caveat (ADR-0002):** incremental aims for the same logical graph as a
cold rebuild, but the guaranteed-identical artifact is the cold build. Canonical
measurement graphs must be built cold (`--no-cache` or a fresh out dir); incremental
serves `serve`-freshness. Remaining simplifications: PageRank/Leiden recompute on any
dirt; Sol↔TS link rebuilt whenever any TS/Sol file is dirty.

---

## 3. Package Architecture

```
cmd/ckg/                 ← ~20 subcommands (root.go:30-36); production surfaces starred
  ├─ main.go, root.go    (entry point + CLI setup)
  ├─ build.go *          (ckg build)
  ├─ serve.go *          (ckg serve + options)
  ├─ mcp.go *            (ckg mcp — thin wrapper over internal/mcp)
  ├─ eval_retrieval.go * (ckg eval-retrieval — the eval surface; there is no eval.go)
  ├─ audit.go *          (ckg audit)
  ├─ export_static.go / export_json.go / export_postgres.go  (utility exports)
  └─ query.go, path.go, report.go, benchmark.go, watch.go, quickstart.go,
     evidence.go, validate.go, bench_*.go                    (utilities + harnesses)

internal/
  ├─ buildpipe/          ← Orchestration (P1-P7)
  │   ├─ pipeline.go     (Run, runCold, runShortCircuit, emitDerivedPasses)
  │   ├─ cache.go        (DiffManifest, ComputeCacheKey, const SchemaVersion="1.23")
  │   └─ incremental.go  (LIVE — runIncremental: G6 v4 + C1 reverse-ref invalidation)
  │
  ├─ detect/             ← File discovery (P1) — Go via go/packages, TS/Sol/Proto via walk
  │
  ├─ parse/              ← Parser interface + dispatch
  │   ├─ parser.go, dispatch.go, idgen.go
  │   ├─ golang/         (Go AST parser)
  │   ├─ typescript/     (TS/JS tree-sitter)
  │   ├─ solidity/       (Sol tree-sitter + ABI)
  │   └─ proto/          (.proto: hand-rolled lexer + recursive-descent parser)
  │
  ├─ graph/              ← Graph construction (P4) — Build: dedup nodes by ID, edges by key
  │
  ├─ link/               ← Cross-language linker (G5, P5) — sol_to_ts.go (ABI → binds_to)
  │
  ├─ temporal/           ← Git history (G6, P6) — changed_in/blame edges
  │
  ├─ cluster/            ← Package/topic tree (P7)
  │   └─ leiden.go, pkg_tree.go, topic_tree.go, naming.go, persist_adapter.go
  │
  ├─ score/              ← Metrics (P7)
  │   └─ score.go, centrality.go (PageRank / centrality + usage_score)
  │
  ├─ persist/            ← Storage layer (SQLite default; Postgres deprecated, see §9.3)
  │   ├─ store_interface.go (StoreReader / StoreWriter / Store ISP)
  │   ├─ sqlite.go + sqlite_{reader,writer,fts,helpers,migrate}.go (split sqliteStore)
  │   ├─ postgres_{store,exporter}.go (deprecated backend — ADR-0003)
  │   ├─ schema.sql      (tables + FTS5)
  │   └─ manifest.go     (FileEntry, Manifest, SchemaVersion back-compat policy)
  │
  ├─ server/             ← HTTP server + viewer (server.go, api.go, options.go, staleness.go, web_assets/)
  │
  ├─ mcp/                ← MCP stdio entry point only
  │   └─ server.go       (Run: builds MCPServer, calls mcphandlers.RegisterAll(s, store);
  │                       the tool bodies moved to pkg/mcphandlers — tools.go/get_context.go are gone)
  │
  ├─ eval/               ← Evaluation framework (keyword-retrieval accuracy)
  ├─ audit/              ← Graph verification (compare go/packages.Load vs DB)
  ├─ filterlist/         ← path include/exclude filtering (MAIN_PKG rescoping, #55)
  └─ validate/           ← graph validation helpers
  (also: detect/, e2e/)

pkg/                     ← public contract consumed by CKV/CKS (11 packages)
  ├─ types/              (enums.go: 37 NodeTypes / 43 EdgeTypes via AllNodeTypes()/AllEdgeTypes(); node.go, edge.go)
  ├─ mcphandlers/        (the 10 MCP tool handlers + RegisterAll — moved out of internal/mcp)
  ├─ store/              (public Reader/store surface)
  ├─ bm25/, smartctx/, evidence/, impact/, concurrency/, hunkmodifies/, policy/, security/
```

### 3.1 Dependency Flow

```
cmd/ckg/build.go
    │
    └─→ buildpipe.Run()
        │
        ├─→ detect.Walk() + detect.GoFiles()
        │
        ├─→ parse.golang.Parser.ParseFile() + Resolve()
        ├─→ parse.typescript.Parser.ParseFile() + Resolve()
        ├─→ parse.solidity.Parser.ParseFile() + Resolve()
        └─→ parse.proto.Parser.ParseFile() + Resolve()
            │
            └─→ graph.Build() (dedup)
                │
                ├─→ link.SolToTS() (G5)
                ├─→ temporal.emitTemporalEdges() (G6)
                ├─→ cluster.BuildPkgTree() + BuildTopicTree()
                └─→ score.Compute()
                    │
                    └─→ persist.Store.Insert*() (bulkload)
                        │
                        └─→ SQLite: graph.db
```

---

## 4. Storage Schema (SQLite)

### 4.1 Key Tables

```sql
nodes (id, type, name, qualified_name, file_path, start_line, end_line,
       start_byte, end_byte, language, visibility, signature, doc_comment,
       complexity, in_degree, out_degree, pagerank, usage_score, confidence, sub_kind)
  ├─ PK: id (TEXT)
  └─ Indices: qname, file_path, type

edges (id, src, dst, type, file_path, line, count, confidence)
  ├─ PK: id (AUTOINCREMENT)
  ├─ FK: src → nodes(id) ON DELETE CASCADE
  ├─ FK: dst → nodes(id) ON DELETE CASCADE
  └─ Indices: src, dst, type

pkg_tree (parent_id, child_id, level)
  ├─ FK: parent_id → nodes(id) ON DELETE CASCADE
  ├─ FK: child_id → nodes(id) ON DELETE CASCADE
  └─ PK: (parent_id, child_id)

topic_tree (parent_id, child_id, resolution, topic_label)
  ├─ FK: child_id → nodes(id) ON DELETE CASCADE
  └─ PK: (child_id, resolution, parent_id)

blobs (node_id, source)
  ├─ PK: node_id
  ├─ FK: node_id → nodes(id) ON DELETE CASCADE
  └─ Content: source code slice (StartByte..EndByte)

nodes_fts (FTS5 virtual table over nodes: name, qualified_name, signature, doc_comment)
  └─ Used by: store.Search() (BM25 + auto-prefix ASCII / LIKE CJK fallback)

manifest (key, value)
  └─ Stores: schemaVersion, ckgVersion, buildTime, statistics, Files[] (with SHA256/cacheKey)

pending_refs (file_path, src_id, target_qname, edge_type, line, hint_file)
  ├─ Per-file unresolved cross-file refs, consumed by the incremental build path
  ├─ FK: src_id → nodes(id) ON DELETE CASCADE
  └─ PK: (file_path, src_id, target_qname, edge_type, line)

node_prs (node_id, number, title, summary, base_sha, head_sha, merged_at, repo)
  ├─ schema.sql:152 — merged pull-request provenance per node (backs change_history MCP tool)
  ├─ merged_at kept so retrieval can answer "what did we know at base_sha?" (no hindsight leak)
  ├─ FK: node_id → nodes(id) ON DELETE CASCADE
  ├─ Index: idx_node_prs_merged on merged_at
  └─ PK: (node_id, number)
```

Full table/column list and exact counts live in docs/SCHEMA.md — treat it as the
single source; the tables above are illustrative, not exhaustive.

### 4.2 ON DELETE CASCADE (A3 Incremental Cache)

Schema 1.2+ enforces FK `ON DELETE CASCADE` on:
- edges.src/dst → nodes(id)
- blobs.node_id → nodes(id)
- pkg_tree.parent_id/child_id → nodes(id)
- topic_tree.child_id → nodes(id)

When `DeleteNodesByFilePath(path)` is called, all dependent rows cascade-delete automatically.

### 4.3 Interface Segregation (A4)

```go
type StoreReader interface {
  // Manifest, hierarchy, node/edge queries, traversal, search, blobs, per-file lookups
}

type StoreWriter interface {
  // Migrate, InsertNodes/Edges/Blobs/Trees, DeleteNodesByFilePath, RebuildFTS, SetManifest
}

type Store interface {
  StoreReader
  StoreWriter
}
```

Consumers depend on the interface (not the concrete `sqliteStore`). A PostgreSQL
implementation exists behind the same interface but is **deprecated** (ADR-0003) —
SQLite is the sole maintained backend. See §9.3.

---

## 5. MCP Server & Tool Surface

### 5.1 MCP Server Architecture

```
cmd/ckg/mcp.go → internal/mcp/server.go (Run)
  │
  └─→ mcphandlers.RegisterAll(s, store)   (pkg/mcphandlers/registerall.go)
      │  (wraps store in the §11.3 H3-safe reader inside each Register*)
      ├─→ RegisterFindSymbol            [Tool 1]
      ├─→ RegisterFindCallers           [Tool 2]
      ├─→ RegisterFindCallees           [Tool 3]
      ├─→ RegisterGetSubgraph           [Tool 4]
      ├─→ RegisterSearchText            [Tool 5]
      ├─→ RegisterGetContextForTask     [Tool 6 — smart]
      ├─→ RegisterImpactOfChange        [Tool 7 — smart]
      ├─→ RegisterConcurrencyImpact     [Tool 8 — smart]
      ├─→ RegisterChangeHistory         [Tool 9 — smart]
      └─→ RegisterEvidenceForIntent     [Tool 10 — smart, one evidence.Cache per Run]
          │
          └─→ server.ServeStdio() (stdio MCP protocol)
```

The tool bodies live in `pkg/mcphandlers/` (public, so cks/ckv can mount the same
set); `internal/mcp/server.go` is now a thin stdio entry point that delegates to
`RegisterAll`. **Authoritative tool list: `pkg/mcphandlers/registerall.go`.**

### 5.2 Ten MCP Tools

| # | Tool | Input | Output | Graph Axes Used | Algorithm |
|---|---|---|---|---|---|
| 1 | `find_symbol` | name (str), language (opt), exact (bool), include_blobs (bool) | nodes[] + blobs (opt) | G1 (struct) | Exact or suffix match on qname/name via index |
| 2 | `find_callers` | qname (str), depth (int=1), include_blobs (bool) | nodes[] + edges[] + blobs (opt) | G3 (calls/invokes) | Reverse BFS from qname, depth limit, filter on call edge types |
| 3 | `find_callees` | qname (str), depth (int=1), include_blobs (bool) | nodes[] + edges[] + blobs (opt) | G3 (execution) | Forward BFS from qname, depth limit, filter on call edge types |
| 4 | `get_subgraph` | seed_qname (str), depth (int=2), include_blobs (bool) | nodes[] + edges[] + blobs (opt) | All (bidirectional) | Bidirectional BFS from seed, both directions, 1-hop neighbors |
| 5 | `search_text` | query (str), top_k (int=10), language (opt), include_blobs (bool) | nodes[] + blobs (opt) | G1 (FTS) | FTS5 BM25 (auto-prefix ASCII / LIKE CJK) → top-K |
| 6 | `get_context_for_task` | task_description (str), budget_tokens (int=8000), language (opt), include_blobs (bool), max_bodies (int=5) | {subgraph, bodies[], summaries[], tokens_estimated, trimmed, not_found} | All (multi-strategy) | BM25 retrieve (30) → 1-hop expand → score-fuse (0.5 BM25 + 0.3 PR + 0.2 usage) → diversify → pack within token budget |
| 7 | `impact_of_change` | qname (str) + options | impacted nodes/edges + blast radius | G2/G3 (reverse deps) | Reverse-dependency closure (broader than callers) to estimate blast radius |
| 8 | `concurrency_impact` | qname (str) + options | concurrency-related nodes/edges | G4 (concurrency) | Concurrency-axis traversal (goroutine/channel/mutex relations) |
| 9 | `change_history` | qname/name (str) + options | merged PRs touching the node | G6 (temporal) | Resolve node → `node_prs` provenance, ordered by merged_at |
| 10 | `evidence_for_intent` | intent/task (str) + options | evidence-ranked nodes/snippets | All (evidence) | H3 EvidencePack assembler over the commit/hunk corpus (BM25-backed) |

**Note**: Tools 1-5 are "granular" (single-axis). Tools 6-10 are "smart" (multi-axis
retrieval/analysis orchestration). `change_history` (Tool 9) is easy to miss in older
docs — it is registered by `RegisterAll` and backed by the `node_prs` table.
Authoritative tool list: `pkg/mcphandlers/registerall.go`.

### 5.3 get_context_for_task Algorithm (Tool 6)

**Steps**:

```
(a) Retrieve: store.Search(task_description, top=30)
    → FTS5 with auto-prefix for ASCII, LIKE substring for CJK

(b) Expand: QueryEdgesForNodes(candidate_ids) → 1-hop neighbors

(c) Score: fuse three signals:
    score = 0.5 * norm(BM25_rank) + 0.3 * norm(PageRank) + 0.2 * norm(usage_score)

(d) Diversify: V0 = simple cap of top-30 (no per-cluster cap)

(e) Pack: top max_bodies get full source_blob; next ≤15 get signature+doc_comment summary
    → cumulative tokens ≤ budget_tokens
```

**Output envelope**:
```json
{
  "task_description": "...",
  "subgraph": { "nodes": [...], "edges": [...] },
  "bodies": [ { "id": "...", "source": "..." } ],
  "summaries": [ { "id": "...", "signature": "...", "doc": "..." } ],
  "tokens_estimated": 7234,
  "trimmed": false,
  "not_found": false
}
```

---

## 6. HTTP Server API

### 6.1 Routes & Handlers (internal/server/server.go)

| Endpoint | Method | Handler | Consumers |
|---|---|---|---|
| `/api/manifest` | GET | handleManifest | viewer (graph stats, freshness) |
| `/api/hierarchy` | GET | handleHierarchy | viewer (pkg/topic tree) |
| `/api/nodes` | GET | handleNodes | viewer (paginated node list) |
| `/api/nodes-by-ids` | POST | handleNodesByIDs | viewer (select multiple nodes) |
| `/api/edges` | POST | handleEdges | viewer (query edges for subgraph) |
| `/api/blob/{id}` | GET | handleBlob | viewer (source code slice) |
| `/api/search` | GET | handleSearch | viewer (FTS search) |
| `/` (or dev path) | (static) | FileServer | embedded Next.js viewer (or dev overlay) |

### 6.2 Server Options (Wave 7 — Group F)

```go
type Options struct {
  DevViewerDir string  // env: CKG_DEV_VIEWER_DIR (disk-backed viewer for dev loop)
  NoViewer     bool    // flag: --no-viewer (API-only, operator's reverse-proxy pattern)
}
```

**Use cases**:
- `New(store, log)` → zero-options default (embedded viewer)
- `--open` auto-suppress when `--no-viewer` set
- `CKG_DEV_VIEWER_DIR=$(pwd)/internal/server/web_assets` → reload browser without recompiling

---

## 7. CKS Deep-Dive Comparison (6 Graph Axes)

This section compares CKS spec (from `04-cks-deep-dive.md`) vs CKG current implementation.

### 7.1 G1: Structural Graph

| Aspect | CKS Spec | CKG Current | Gap | Status |
|---|---|---|---|---|
| **Edge types** | contains, defines, imports, exports, configures | contains, defines, imports, exports | configures (config key → function) | **PARTIAL** — no config tracking |
| **Node types** | Package, File, Symbol (struct/func/type), ConfigKey | Package, File, + the rest of the 37 node types (docs/SCHEMA.md) | ConfigKey node type | **MISSING** — ConfigKey not emitted |
| **Query capability** | repo→module→package→file→symbol hierarchy | pkg_tree edges (parent_id, child_id, level) | No explicit repo/module nodes | **PARTIAL** — file-to-symbol OK, repo-level limited |
| **Storage** | Graph DB | SQLite with nodes/edges/pkg_tree tables + indices | FTS5 for symbol lookup | **FULL** |

### 7.2 G2: Semantic Graph

| Aspect | CKS Spec | CKG Current | Gap | Status |
|---|---|---|---|---|
| **Edge types** | references, implements, extends, overrides, tests, state_mutation, reads, writes, emits, consumes, handles | references (✓), implements (✓), extends (✓), uses_type (≈ references), instantiates, reads_field, writes_field, reads_mapping, writes_mapping, emits_event, has_modifier, has_decorator | overrides, tests, state_mutation, consumes, handles | **PARTIAL** — core edges present, some missing |
| **Node types** | Symbol, Test, Event, ConfigKey | Function, Method, Class, Interface, Struct, Enum, Field, Variable, Constant, Parameter, LocalVariable, Decorator, +more | Event as node type (not present) | **PARTIAL** — Event edges (emits_event) but no Event node type |
| **Implementation approach** | Type inference + AST walk | AST walk (Go types.Info, tree-sitter) + cross-file resolve | No semantic inference phase | **PARTIAL** — AST-based, no ML type inference |
| **Line-level precision** | Yes (file:line) | Yes (StartLine/EndLine/StartByte/EndByte) | — | **FULL** |

### 7.3 G3: Execution Graph

| Aspect | CKS Spec | CKG Current | Gap | Status |
|---|---|---|---|---|
| **Edge types** | calls, returns, branches, modifies, timeout_path, retry_path, cancellation_path | calls (✓), invokes (RPC variant) | returns (edge type missing), branches, modifies, timeout/retry/cancellation paths | **PARTIAL** — calls/invokes present, flow paths missing |
| **Node types** | Symbol, IfStmt, LoopStmt, CallSite, ReturnStmt, SwitchStmt | All above types present (NodeIfStmt, NodeLoopStmt, NodeCallSite, NodeReturnStmt, NodeSwitchStmt) | — | **FULL** |
| **Control flow** | If/loop/switch as nodes with edges to entry/exit | Nodes emitted per conditional, edges on branch condition | Granularity: statement-level nodes | **PARTIAL** — coarse granularity (not data-flow) |
| **Timeout/cancellation tracking** | Explicit context.WithTimeout → handler | Not explicitly modeled | Relies on code pattern (would require SSA) | **MISSING** — deferred to D1 (SSA phase) |

### 7.4 G4: Concurrency Graph

| Aspect | CKS Spec | CKG Current | Gap | Status |
|---|---|---|---|---|
| **Edge types** | spawns, sends_to, receives_from, locks, unlocks, waits_for, shares_state_with | spawns (✓), sends_to (✓), recvs_from (✓), acquires_lock (✓), releases_lock (✓), accessed_under_lock (✓) | waits_for, shares_state_with (data-flow analysis) | **PARTIAL** — synchronization OK, data-flow deferred |
| **Node types** | Goroutine, Channel, Mutex | Goroutine, Channel, Mutex (schema 1.1+) | — | **FULL** |
| **Implementation** | Flow-sensitive analysis | AST pattern matching (Go only) + underlock pass (G8 + G9) | No happens-before inference | **PARTIAL** — pattern-based, not SSA |
| **Accuracy** | Ideally 100% (SSA) | Pattern-based mutex/acquires_lock/accessed_under_lock detection on go-stablenet (runtime counts — re-measure; predate the #55 binary-reachable rescoping) | False negatives on local mutexes (1 known edge case) | **PARTIAL** — pattern-based, not SSA |

### 7.5 G5: Distributed Interaction Graph

| Aspect | CKS Spec | CKG Current | Gap | Status |
|---|---|---|---|---|
| **Edge types** | listens_on, handles_message, xlang_calls, rpc_calls, p2p_broadcasts, consensus_flow | listens_on (✓), handles_message (✓), rpc_calls (✓), binds_to (Sol→TS, xlang), + Endpoint/MessageType nodes | p2p_broadcasts, consensus_flow | **PARTIAL** — HTTP/RPC routing emitted, P2P consensus deferred |
| **Node types** | Endpoint, MessageType, Contract (Solidity) | Endpoint (schema 1.3), MessageType (schema 1.3), Contract (Solidity) | — | **FULL** |
| **Handler detection** | HTTP routes + RPC signatures | stdlib HandleFunc pattern + net/rpc + httprouter (limited) | Custom router DSLs (e.g. gin, Echo) not detected | **PARTIAL** — stdlib + rpc only, extensible |
| **Protocol specificity** | HTTP/gRPC/P2P distinguished | HTTP routes + net/rpc | No gRPC/P2P detection | **PARTIAL** — HTTP + RPC present, P2P missing |

### 7.6 G6: Temporal Graph

| Aspect | CKS Spec | CKG Current | Gap | Status |
|---|---|---|---|---|
| **Edge types** | changed_in (symbol → commit), blame (file:line → commit) | changed_in (✓), blame (✓, file-level only) | Line-level blame (Phase 2 deferred) | **PARTIAL** — file-level blame present, line-level deferred |
| **Node types** | Commit (git nodes) | Commit (schema 1.4, E4) | — | **FULL** |
| **Temporal depth** | Configurable (heuristic) | Bounded by Options.TemporalDepth (default 10, most-recent commits) | No history windowing control | **PARTIAL** — hard-coded depth, configurable in future |
| **Blame granularity** | Line-level (`file:line → commit`) | File-level (`File → Commit`) | Would require `git blame --line-porcelain` | **PARTIAL** — file scope only |
| **Staleness detection** | Implicit (freshness query) | server.Staleness: DB timestamp vs source mtime comparison | — | **FULL** |

### 7.7 Summary: CKS Coverage Matrix

Per-axis gaps (structural, stable):

```
G1 Structural   missing ConfigKey node/edge
G2 Semantic     missing overrides, tests, consumes, handles
G3 Execution    missing returns, branches, modifies, flow paths
G4 Concurrency  missing waits_for, shares_state_with
G5 Distributed  missing P2P/consensus; custom routers
G6 Temporal     missing line-level blame
```

> The former percentage bars (e.g. "Overall 71%") were runtime coverage figures —
> re-measure; they predate the #55 binary-reachable rescoping (commit `bf59fdb`),
> which changes the node/edge population the percentages are computed against. Keep
> the per-axis gap list above (which is structural) as the durable signal.

---

## 8. Success Condition Gap Analysis

User-defined success conditions (from session context):

### Condition 1: Input code → Output graph ✅ IMPLEMENTED

**Status**: **FULL**

- **Input**: `ckg build --src=<path>` scans all source files
- **Output**: SQLite graph.db with 37 node types, 43 edge types (authoritative: docs/SCHEMA.md)
- **Verification**: `ckg audit --src=<path> --graph=<path>` compares go/packages.Load set vs DB (exit 0 = parity)

**Metric** (go-stablenet): node/edge counts and file totals here are runtime figures —
re-measure; they predate the #55 binary-reachable rescoping (commit `bf59fdb`), which
scopes the graph to binary-reachable code via `MAIN_PKG` and therefore shrinks the
populated node/edge set versus the older whole-tree numbers. The structural claim
holds: audit reports **exit 0 (PARITY)** on a clean build.

### Condition 2: 6 Graph Structures (G1-G6) ✅ IMPLEMENTED (PARTIAL)

**Status**: **FULL (edges) / PARTIAL (semantics)**

| Graph | Edge Emission | Node Types | Freshness |
|---|---|---|---|
| G1 Structural | contains, defines, imports, exports ✅ | 37/37 ✅ (authoritative: docs/SCHEMA.md) | Cold + partial-cache |
| G2 Semantic | references, uses_type, instantiates, reads_field, writes_field ✅ | 37/37 ✅ (authoritative: docs/SCHEMA.md) | Cold + partial-cache |
| G3 Execution | calls, invokes ✅ | NodeIfStmt, LoopStmt, CallSite, ReturnStmt, SwitchStmt ✅ | Cold + partial-cache |
| G4 Concurrency | spawns, sends_to, recvs_from, acquires_lock, releases_lock, accessed_under_lock ✅ | Goroutine, Channel, Mutex ✅ | Cold + partial-cache |
| G5 Distributed | listens_on, handles_message, rpc_calls, binds_to ✅ | Endpoint, MessageType ✅ | Cold only (Sol parser retained) |
| G6 Temporal | changed_in, blame ✅ | Commit ✅ | Cold only (git history) |

### Condition 3: Better Understanding + Speed than tree-sitter Alone ✅ PARTIAL

**Status**: **PARTIAL**

- **Tree-sitter AST output**: syntax trees, no cross-file relations, no semantic resolution
- **CKG graph output**: 
  - Structural graph (package hierarchy, imports) → **better for module understanding**
  - Semantic graph (type resolution, inheritance) → **better for impact analysis**
  - Execution graph (call chains) → **better for dataflow understanding**
  - Concurrency graph (mutex/channel) → **better for race condition detection**
  - Distributed graph (RPC/HTTP) → **better for service topology**
  - Temporal graph (git history) → **tree-sitter doesn't provide**

**Speed**:
- Cold build (go-stablenet): parsing + graph + temporal + clustering (wall-clock is a
  runtime figure — re-measure; predates the #55 binary-reachable rescoping)
- Short-circuit (no changes): <1s (manifest refresh only)
- MCP tool execution: <100ms per query (index-backed)

**Missing**: Benchmarked comparison vs raw tree-sitter on identical task.

### Condition 4: MCP Query Capabilities ✅ FULL

**Status**: **FULL**

| Capability | CKS Requirement | CKG Tool | Status |
|---|---|---|---|
| 4a. Code structure + call flow | Find symbols, traverse calls | find_symbol, find_callers, find_callees, get_subgraph | ✅ FULL |
| 4b. Data processing flow | Track field reads/writes, type flow | search_text (on signature) + edges query | ⚠️ PARTIAL — edges present, no auto-stitching |
| 4c. Modification history | Who changed what, when | get_context_for_task (includes blame edges), temporal graph | ✅ FULL |
| 4d. Concurrency flow (runtime effects) | Goroutine/channel/mutex relations | get_subgraph (bidirectional BFS on concurrency edges) | ✅ FULL |
| 4e. Staleness (graph vs real code version gap) | Freshness query | server.Staleness check (DB timestamp vs source mtime) | ✅ FULL |

### Condition 5: Logging + Debug Mode Support ✅ FULL

**Status**: **FULL**

- **Log sink**: `slog` (stdlib log/slog)
- **Verbosity control**: `--verbose` / `--debug` flags (via cobra)
- **Output levels**: ERROR, WARN, INFO, DEBUG
- **Per-package logs**: buildpipe (discover, cache, build), parse (errors, pending refs), graph (validate), persist (bulk-insert)

**Build pipeline logs** (pipeline.go):
```go
log.Info("detected files", "go", goCount, "ts", tsCount, "sol", solCount, "proto", protoCount) // pipeline.go:239
log.Info("Cache: ...", ...) // decision rationale
log.Info("xlang linked", "binds_to", len(xlEdges))
log.Info("build complete", "nodes", len(g.Nodes), "edges", len(g.Edges), ...)
```

---

## 9. Known Limitations & Future Work

### 9.1 Incremental Build (resolved — was the critical path)

**G6 v4 partial-cache is LIVE** (`incremental.go:154`, dispatched `pipeline.go:260`),
so this is no longer a limitation:
- The old H3 phantom-edge root cause (NodesByFilePath order ≠ AST declaration order)
  is fixed by sorting `NodesByFilePath` `ORDER BY start_line`
  (`internal/persist/sqlite_reader.go:592`).
- C1 reverse-reference invalidation is implemented (`store.ReverseDepsForFiles`).

**Remaining caveat** (not a bug): the canonical, bit-identical artifact is the cold
build (ADR-0002); incremental targets `serve` freshness. Build canonical measurement
graphs cold (`--no-cache`). PageRank/Leiden still recompute on any dirt.

### 9.2 Minor Issues (Handoff § 4.6)

| Issue | Component | Workaround | Priority |
|---|---|---|---|
| G3-1: Custom router patterns (gin, Echo) | E3 (handler detection) | Manual endpoint specification | Low |
| G4-1: Ethereum RPC signature (client.Call) | E3 (RPC detection) | Add pattern matcher | Low |
| G6-temporal: Line-level blame | E4 (temporal phase) | Implemented file-level; line-level deferred (Phase 2) | Low |
| B1-1: Local mutex literal (`var mu sync.Mutex{}`) | G9 (underlock) | 1 false negative known | Very Low |

### 9.3 PostgreSQL Backend — Implemented but Deprecated

The PostgreSQL backend and `ckg export-postgres` are **implemented**
(`cmd/ckg/export_postgres.go`, `internal/persist/postgres_{store,exporter}.go`) —
this is no longer future work. However, per **ADR-0003 (Accepted, 2026-06-29)** the
Postgres backend is **deprecated**: it is unused, untested in CI, and already lags the
SQLite schema (no `canonical_id`/`simple_name`; `FindByCanonicalID` is a not-found
stub). **SQLite is the sole supported and maintained storage backend.** New schema
columns are NOT required to mirror to `pgStore` — those gaps are accepted deprecation
gaps, not bugs.

### 9.4 Other Future Work

| Group | Task | Depends On | Status |
|---|---|---|---|
| C1 | Reverse-reference invalidation | G6 redesign | DONE — live in `runIncremental` (§9.1) |
| D1 | SSA-based concurrency (`--deep` opt-in) | B1 | Not started (would replace pattern matching) |
| D2 | pgvector + Apache AGE integration | — | Not started; note Postgres backend is deprecated (see §9.3) |

---

## 10. Execution Model: Cold Build Walk-Through

**Example**: `ckg build --src=testdata/synthetic --out=/tmp/ckg-demo`

### Step-by-step execution:

1. **Enter buildpipe.Run(Options)**
   - Logger = default stderr
   - OutDir = /tmp/ckg-demo

2. **discoveryAll()** (detect.Walk + detect.GoFiles)
   - Find all .go files in testdata/synthetic → pass to go/packages.Load
   - Honors build constraints (// +build tags)
   - Find all .ts, .js, .tsx, .jsx files → extension-only discovery
   - Find all .sol files → extension-only discovery
   - Find all .proto files → extension-only discovery
   - Output: DiscoveredFile[] with Path (srcRoot-relative) and Language

3. **Cache routing** (ManifestUsable check)
   - Read /tmp/ckg-demo/graph.db manifest (if exists)
   - Check: SchemaVersion match? CKGVersion match? Files present?
   - Result: No prior build or mismatch → proceed to runCold

4. **runCold: Detect phase**
   - detect.Walk(srcRoot) → files {Go, TS, Sol} lists
   - detect.GoFiles(srcRoot) → go/packages.Load result (respects build constraints)

5. **runCold: Parse phase**
   - For each language in {go, ts, sol}:
     - Dispatch to language-specific parser
     - Call ParseFile() per file (parallelized via worker pool)
     - Each returns ParseResult {Path, Nodes[], Edges[], Pending[]}
     - Collect all into ResolvedGraph per language

   **Go parsing** (parse.golang.Parser):
   - packages.Load (respects build tags)
   - types.Info traversal → emit nodes for package, file, func, type, variable, etc.
   - Cross-file calls resolved via types.Info.Selections
   - Concurrency pass (B1) → Goroutine, spawns edges, Channel, send_to/recv_from
   - Underlock pass (G8/G9) → Mutex, acquires_lock, releases_lock, accessed_under_lock
   - Distributed pass (E3) → Endpoint (ListenAddr), MessageType, listens_on, handles_message

   **TS/JS parsing** (parse.typescript.Parser):
   - tree-sitter parse per file
   - Walk tree → emit nodes for class, function, variable, import, export, etc.
   - Cross-file resolution via import statements
   - Decorator pass → has_decorator edges

   **Solidity parsing** (parse.solidity.Parser):
   - tree-sitter parse per file
   - Walk tree → emit Contract, Function, Event, Mapping, etc.
   - ABI extraction → readABIFunction for later Sol→TS linking
   - Distributed pass (E3) → Event/message type detection

6. **runCold: Resolve phase** (Pass 2, per-language)
   - For each language: Resolve(per_file_ParseResults[])
   - Go: types.Info already cross-file-resolved; just emit ResolvedGraph
   - TS: tree-sitter doesn't provide types; heuristic symbol matching across imports
   - Solidity: contract-local resolution + ABI linking
   - Output: ResolvedGraph {Nodes[], Edges[]}

7. **runCold: Graph build** (Pass 4)
   - graph.Build(per_language_ResolvedGraphs[])
   - Merge: nodes deduplicated by ID (last-writer-wins)
   - Edges deduplicated by (Type, Src, Dst, Line) keep-first
   - Output: Graph {Nodes[], Edges[]}

8. **runCold: Derived passes** (emitDerivedPasses)

   a. **Cross-lang link** (G5):
   - link.SolToTS(graph.Nodes, solParser.ABI())
   - For each Solidity contract ABI function → find matching TS function by signature pattern
   - Emit binds_to edges (Sol function → TS function)
   - Append to graph.Edges

   b. **Temporal** (G6):
   - emitTemporalEdges(graph, srcRoot, log, depth=0)
   - git log --follow --max-count=depth per file
   - For each commit: emit Commit node, changed_in edges (symbol → commit), blame (file → commit)
   - Append to graph.Edges

   c. **Cluster**:
   - cluster.BuildPkgTree(graph) → package hierarchy (traversal order)
   - cluster.BuildTopicTree(graph, thresholds, seed) → Leiden clustering (topic discovery)
   - Output: PkgTree, TopicTree

   d. **Score**:
   - score.Compute(graph) → PageRank (per-node), usage_score (in_degree heuristic)

9. **runCold: Persist**
   - openColdStore(outDir) → os.Remove(graph.db) + reopen + migrate
   - persistColdArtifacts(store, srcRoot, graph, pkgTree, topicTree):
     - InsertNodes(graph.Nodes) → bulk insert (AUTOINCREMENT rowids)
     - InsertEdges(graph.Edges) → with FK references
     - InsertBlobs(extractBlobs(srcRoot, graph.Nodes)) → source code slices
     - InsertPkgTreeFromCluster(pkgTree.edges)
     - InsertTopicTree(topicTree)
     - RebuildFTS() → index nodes_fts for search
   - InsertPendingRefs(allPending) (schema 1.23 — consumed by the incremental path)
   - Manifest:
     - computeColdFileEntries(srcRoot, discovery, nodes, edges) → FileEntry[] (SHA256, CacheKey per file)
     - setStaleness(&manifest, log) → compute DB timestamp, source mtime max
     - SetManifest(manifest) → serialize to DB
     - writeManifestJSON(manifest.json) → human-readable copy

10. **Log output** (shape only; the specific counts are runtime figures that predate
    the #55 binary-reachable rescoping — re-measure rather than trusting these):
    ```
    detected files go=… ts=… sol=… proto=…
    Cache: bypassed (--no-cache); full rebuild
    xlang linked binds_to=…
    Cache: temporal edge emission complete changed_in=…
    build complete nodes=… edges=… pkg_tree_edges=… topic_resolutions=…
    ```

11. **Return**: persist.Manifest with statistics

12. **cmd/ckg/build.go** prints result to stdout/stderr, exits 0

---

## 11. API Layer & Viewer Integration

### 11.1 HTTP Server Initialization

```go
// cmd/ckg/serve.go
func runServe(cmd *cobra.Command, args []string) error {
  store := persist.OpenReadOnly(graphPath)
  opts := server.Options{
    DevViewerDir: os.Getenv("CKG_DEV_VIEWER_DIR"),
    NoViewer:     noViewerFlag,
  }
  srv := server.NewWithOptions(store, logger, opts)
  return srv.ListenAndServe(ctx, addr)
}
```

### 11.2 Viewer (Next.js + react-force-graph-3d)

- **Location**: `internal/server/web_assets/` (embedded via `go:embed`)
- **Framework**: React, zustand (state), d3-force-3d
- **Features**:
  - 3D force-directed graph visualization
  - 6-axis filter UI (G1-G6 graph toggles)
  - Node search, hover tooltip (signature + doc)
  - Source blob inline viewer (double-click node)
  - localStorage persistence (preferences, view state)

### 11.3 Static Export (ckg export-static)

```bash
./bin/ckg export-static --graph=/tmp/ckg-demo --out=/srv/ckg/static
```

**Output structure**:
```
/srv/ckg/static/
  ├─ index.html (viewer entry)
  ├─ nodes_0.json (chunked node export)
  ├─ nodes_1.json
  ├─ edges_0.json (chunked edge export)
  ├─ edges_1.json
  ├─ manifest.json
  └─ (other viewer assets)
```

**Use case**: `export-static` + static hosting (S3, Cloudflare Pages) + separate `ckg serve` API behind reverse proxy.

---

## 12. Implementation Highlights

### 12.1 Go Parser: Why go/packages.Load?

```go
// internal/detect/go.go
packages.Load(cfg, srcRoot)
  ├─ Respects // +build tags
  ├─ Loads go.mod/go.sum
  └─ Provides types.Info (cross-file resolution)
```

**Alternative**: tree-sitter (language-agnostic)
- Pros: Single grammar for all languages
- Cons: No type resolution, ignores build constraints, requires manual cross-file linking
- **Choice**: go/packages.Load for Go (better precision), tree-sitter for TS/Sol (no native AST)

### 12.2 Node ID Generation (pkg/types)

```go
// internal/parse/idgen.go
ID = hash(filePath + ":" + nodeName + ":" + startLine + ":" + startByte)
```

**Properties**:
- Deterministic (same source → same ID)
- Collision-resistant (different nodes → different IDs)
- Stable across builds (cache-friendly)

### 12.3 Edge Deduplication Key

```go
type edgeKey struct {
  Type     types.EdgeType
  Src, Dst string
  Line     int
}
```

**Semantics**: Two edges with identical (Type, Src, Dst, Line) describe the same relation at the same call site.
**Dedup strategy**: keep-first (not count summation, to avoid inflation under partial-cache rebuilds).

### 12.4 FTS5 Search Strategy

```go
store.Search(query, limit=10)
  ├─ ASCII query → FTS5 auto-prefix (fast, high recall)
  └─ CJK query → LIKE substring fallback (handles logographic scripts)
```

**Why FTS5?** SQLite native, no external dependency, reasonable performance for <1M nodes.

---

## 13. Error Handling & Graceful Degradation

### 13.1 Build Errors

| Error | Handling | Impact |
|---|---|---|
| Parse error in one file | Log warning, increment `parseErrs` counter, skip node emission | Graph contains N-1 files, marked in manifest |
| Cache key mismatch | Reparse file, emit fresh edges | No corruption, slowdown only |
| Missing go.mod in Go project | os.DirFS → partial parse, rely on fallback | Go files without mod parsed as standalone packages |
| Cross-file ref unresolvable | Emit PendingRef as AMBIGUOUS, keep edge | Edge remains in graph with Confidence=AMBIGUOUS |

### 13.2 Query Errors

| Error | Handling | Impact |
|---|---|---|
| Qname not found | Return empty result, not 404 | Client sees `nodes: []` |
| Invalid graph.db | Fail fast with descriptive message | User instructed to `--no-cache` and rebuild |
| Blob not found (node has no source) | Silent skip (Package nodes, for example) | Graceful missing field in response |

---

## 14. Testing & Validation

### 14.1 Test Coverage

- **Unit tests**: `*_test.go` in each package (17 test packages, `go test ./...` = PASS)
- **Integration tests**: e2e tests (full build → graph validation)
- **Eval framework**: `internal/eval` + 4 baselines (α/β/γ/δ) + YAML task suite

### 14.2 Audit Command

```bash
./bin/ckg audit --src=<path> --graph=<path>
```

**Comparison**:
- Set A: go/packages.Load files in srcRoot
- Set B: nodes in graph.db (by file_path)
- Output: A ⊆ B (every source file represented in graph)
- Exit codes: 0 (parity), 1 (drift detected), 2 (error)

**Result on go-stablenet** (file counts are runtime figures — re-measure; predate the
#55 binary-reachable rescoping):
- Historically E2 removed a set of file over-includes (smacker false positives)
- Current clean build: **exit 0 (PARITY)**

---

## 15. Deployment Topologies

### 15.1 Development Loop

```bash
make build-full
ckg build --src=<repo> --out=/tmp/ckg
ckg serve --graph=/tmp/ckg --port=8080 --open
```

**Typical cycle**: edit code → `make build-full` → `ckg build` (1-2 min) → reload browser.

### 15.2 Local Single-User

```bash
ckg serve --graph=/path/to/graph.db --port=8080 --open
```

**Embedded viewer** at localhost:8080, API at localhost:8080/api/*.

### 15.3 Production (Recommended: Production Split)

```bash
# 1. Once: build static bundle
ckg export-static --graph=/path/to/graph.db --out=/srv/ckg/static

# 2. Deploy viewer (CDN, S3, Cloudflare Pages)
# serve /srv/ckg/static/* as static files

# 3. Deploy API separately (autoscale, cache layer)
ckg serve --graph=/path/to/graph.db --port=8080 --no-viewer
# front with reverse proxy:
#   /api/* → localhost:8080
#   /* → https://cdn.example.com/ckg/static/
```

**Advantages**: Independent scaling, viewer cached globally, API behind auth/rate-limit.

### 15.4 MCP Integration (Claude Code)

```bash
claude mcp add ckg --command ./bin/ckg --args "mcp,--graph=/path/to/graph.db"
```

**Result**: 10 MCP tools available in Claude Code for code-aware assistance (see §5.2).

---

## 16. Configuration & Environment

### 16.1 Build-Time Configuration

| Variable | Default | Meaning |
|---|---|---|
| `CKG_VERSION` | (from git) | Manifest.CKGVersion (cache key contributor) |
| `GOOS`, `GOARCH` | (native) | Cross-compile target |

### 16.2 Runtime Configuration

| Flag | Env | Default | Purpose |
|---|---|---|---|
| `--src` | — | (required) | Source root path |
| `--out` | — | (required for build) | Output directory |
| `--graph` | — | (required for serve/mcp) | Graph directory (contains graph.db) |
| `--port` | — | `8080` | HTTP server port |
| `--open` | — | `false` | Auto-open browser on serve |
| `--no-cache` | — | `false` | Force full rebuild (ignore manifest) |
| `--no-viewer` | — | `false` | Serve API only (no static mount) |
| (dev) | `CKG_DEV_VIEWER_DIR` | (empty) | Disk path to viewer assets (dev overlay) |

---

## 17. Performance Characteristics

### 17.1 Build Times (go-stablenet, M2 Mac — illustrative)

> Per-file counts and wall-clock times below are runtime figures — re-measure; they
> predate the #55 binary-reachable rescoping (commit `bf59fdb`). Keep the phase
> **ordering and relative cost** (Go parse dominates) as the durable takeaway.

| Phase | Metric | Relative cost |
|---|---|---|
| Detect | go/packages.Load + walk | small |
| Parse (Go) | types.Info traversal | dominant |
| Parse (TS) | tree-sitter | moderate |
| Parse (Sol) | tree-sitter + ABI | small |
| Parse (Proto) | lexer + recursive descent | small |
| Resolve (per-lang) | Cross-file linking | small |
| Graph Build | Merge + dedup | <1s |
| Temporal | git log + edge emit | small |
| Cluster/Score | PageRank + Leiden | small |
| Persist | Bulk insert + FTS index | ~1s |
| **Total Short-Circuit** | (manifest refresh) | <1s |

### 17.2 Query Performance (MCP tools, go-stablenet graph)

| Tool | Query | Time | Notes |
|---|---|---|---|
| find_symbol | "main" | <10ms | Index + hash lookup |
| find_callers | qname, depth=1 | <50ms | BFS from 10K edges |
| find_callees | qname, depth=1 | <50ms | Forward BFS |
| get_subgraph | depth=2 | <100ms | Bidirectional BFS |
| search_text | "handler" | <100ms | FTS5 BM25 |
| get_context_for_task | task desc (100 words) | <300ms | Retrieve + expand + score-fuse |

### 17.3 Storage Size (go-stablenet — illustrative, re-measure post-#55)

| Artifact | Size | Notes |
|---|---|---|
| graph.db | ~200MB | Nodes + edges + blobs + indices |
| manifest.json | ~50MB | Files[] entries (SHA256 per file) |
| export-static bundle | ~300MB | Chunked JSON + viewer |

---

## Appendix: Glossary

| Term | Definition |
|---|---|
| **Cold rebuild** | Full parse, resolve, build, persist (all files processed from scratch) |
| **Short-circuit** | Manifest hit + no dirty files (manifest timestamp refresh only) |
| **Partial hit** | Mixed dirty/cached files → `runIncremental` (G6 v4 + C1); reparses dirty files, reloads cached node sets, reruns Pass 2/cluster/score |
| **Node ID** | Deterministic hash(file:name:startLine:startByte) |
| **Edge key** | (Type, Src, Dst, Line) — semantic identity for dedup |
| **Pending ref** | Unresolved cross-file reference from Pass 1, resolved/marked AMBIGUOUS in Pass 2 |
| **PendingRefRow** | `pending_refs` table (schema 1.23): per-file cross-file refs persisted for the incremental rebuild path |
| **G1–G6** | CKS 6-graph axes: Structural, Semantic, Execution, Concurrency, Distributed, Temporal |
| **FTS5** | SQLite full-text search (Okapi BM25 + auto-prefix) |
| **PageRank** | Iterative scoring: importance proxy (used in tool 6) |
| **Usage score** | in_degree heuristic: how many edges point to this node |
| **Confidence** | EXTRACTED (high precision, AST-based) vs INFERRED (lower precision, heuristic) vs AMBIGUOUS |
| **Staleness** | Freshness check: DB timestamp vs source mtime max (detects stale cache) |

---

**Document Generated**: 2026-07-18 (rewrite-in-place against current tree)  
**Maintainer**: CKG Team  
**Next Review**: after the next runtime re-measurement of node/edge/coverage figures (post-#55)
