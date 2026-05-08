# CKG — 코드 설계 구조 종합 정리

> **목적**: `docs/` 16개 문서의 위계와 소스코드 패키지 구조를 한 문서에서 빠르게 조망.
> ARCHITECTURE.md(1-page) ↔ ARCHITECTURE-DETAILED.md(994 lines) 사이의 시각적·구조적 인덱스 역할.
>
> **대상 독자**: cold-read 신규 합류자 / 다음 세션 시작 시 / external reviewer
> **마지막 갱신**: 2026-05-05 (refresh 7 — G6 v4 + C1 + C2 + real-corpus parity ✅)

---

## 목차

1. [프로젝트 개요](#1-프로젝트-개요)
2. [`docs/` 문서 맵](#2-docs-문서-맵)
3. [시스템 개요 (High-Level Architecture)](#3-시스템-개요-high-level-architecture)
4. [패키지 구조 (Source Layout)](#4-패키지-구조-source-layout)
5. [7-Pass Build Pipeline](#5-7-pass-build-pipeline-cold-path)
6. [6-Graph Axis (CKS Deep-Dive Mapping)](#6-6-graph-axis-cks-deep-dive-mapping)
7. [Storage Schema](#7-storage-schema-sqlite-schema-15)
8. [MCP Tool Surface](#8-mcp-tool-surface-6-tools)
9. [HTTP Server API + Viewer](#9-http-server-api--viewer)
10. [Cache Routing (A3 Phase 1 + G6 v4)](#10-cache-routing-a3-phase-1--g6-v4)
11. [Cache Key & 무효화](#11-cache-key--무효화)
12. [의존 그래프 (Dependency Flow)](#12-의존-그래프-dependency-flow)
13. [Subcommand 요약](#13-7개-subcommand-요약)
14. [검증된 동작 (Capability)](#14-검증된-동작-capability)
15. [다음 작업 (Wave 9)](#15-다음-작업-wave-9-진입-가능)
16. [운영 함정](#16-운영-함정-handoffmd--5에서-누적)
17. [핵심 설계 원칙](#17-핵심-설계-원칙)

---

## 1. 프로젝트 개요

| Field | Value |
|---|---|
| **이름** | CKG (Code Knowledge Graph) |
| **버전** | v0.2.x (schema 1.5) |
| **언어** | Go 1.25.5 (single binary, CGO-free default) |
| **목적** | Go/TypeScript/Solidity 소스 → 코드 지식 그래프 (33 NodeType × 30 EdgeType) |
| **활용** | 3D viewer + MCP server + Eval framework + audit |
| **검증 corpus** | go-stablenet-latest 2,142 files → 217K nodes / 669K edges, audit PARITY ✅ |
| **저장소** | SQLite (default, modernc) / PostgreSQL (`--db postgres://...`, pgxpool) |

---

## 2. `docs/` 문서 맵

| 문서 | 역할 | 핵심 내용 | 갱신 빈도 |
|---|---|---|---|
| **HANDOFF.md** | 세션 경계 snapshot | 현재 상태, 남은 작업, 함정 (cold-read 5분) | wave마다 |
| **WORK-PLAN.md** | 작업 tracker | Group A~G, Wave 1~9 진행상황 | wave마다 |
| **ARCHITECTURE.md** | 1-page overview | 7-pass 파이프라인 한 줄 요약 | 안정 |
| **ARCHITECTURE-DETAILED.md** | 전체 설계서 (994줄) | 17 sections — 패키지/스키마/MCP/CKS gap 분석 | 분기마다 |
| **CODE-STRUCTURE.md** (본 문서) | 시각적·구조적 인덱스 | doc map + 패키지 layout + 다이어그램 모음 | 분기마다 |
| **SCHEMA.md** | 노드/엣지 카탈로그 | 33 NodeTypes × 30 EdgeTypes, schema 버전 history | schema bump마다 |
| **INCREMENTAL.md** | A3 캐시 가이드 | cache_key 공식, manifest v2, 무효화 규칙, partial-cache D4 fallback | A3/G6 변경시 |
| **EVAL.md** | 평가 사용법 | 4 baseline (α/β/γ/δ), 가설 H1/H2 | 안정 |
| **STUDY-GUIDE.md** | 외부 개념 학습 | Leiden, MCP, tree-sitter, 3D layout 학습 경로 | 안정 |
| **spec-ckg-v0.2.md** | foundation spec (497줄) | parser 마이그레이션·동시성·PG·incremental 설계 원전 | 안정 |
| **G6-INCREMENTAL-REDESIGN.md** | partial-cache 재설계 | G6 v1~v4 시도 history, root cause H3 분석 | G6 진척시 |
| **G6-V3-VALIDATION-FINDINGS.md** | v3 실패 분석 | +2675 phantom edges 원인 추적 | history |
| **VIEWER-PERF-CLUSTERING.md** | viewer 성능 노트 | Next.js viewer 마이그레이션 background | history |
| **STATUS-REPORT-2026-05-04.md** | 시점 status | refresh 7 시점 측정 metric | snapshot |

**문서 위계**:

```
spec-ckg-v0.2.md (설계 원전)
        │
        ▼
ARCHITECTURE.md ─────► ARCHITECTURE-DETAILED.md ─────► CODE-STRUCTURE.md (본 문서)
   (1-page)               (deep dive 994줄)               (visual + index)
        │
        ├──► SCHEMA.md         (data model)
        ├──► INCREMENTAL.md    (cache 운영)
        ├──► EVAL.md           (평가 사용)
        └──► STUDY-GUIDE.md    (외부 개념)

운영 tracker:
HANDOFF.md (세션 경계) ↔ WORK-PLAN.md (작업 진척)
        │
        ├──► G6-INCREMENTAL-REDESIGN.md (partial-cache 깊은 분석)
        └──► STATUS-REPORT-*.md (시점 metric snapshot)
```

---

## 3. 시스템 개요 (High-Level Architecture)

```
                     ┌───────────────────────────────────────────┐
                     │           ckg (Single Go Binary)          │
                     │   build / serve / mcp / export-* / eval / │
                     │   audit  ─  cobra rootCmd                 │
                     └──────────────┬──────────────┬─────────────┘
                                    │              │
                                    ▼              ▼
        ┌─────────────────────────────────┐   ┌──────────────────────┐
        │   buildpipe.Run() (orchestrator) │   │   Query surfaces     │
        └─────────────────────────────────┘   │  - HTTP API + viewer │
                                    │          │  - MCP (6 tools)    │
        ┌─── 7-Pass build pipeline ──┴────┐    │  - audit (parity)   │
        │                                 │    │  - export-static    │
        │  P1 detect  → P2 parse (3 lang) │    │  - export-postgres  │
        │  → P3 resolve → P4 graph build  │    └──────────────────────┘
        │  → P5 xlang link (G5)            │              │
        │  → P6 temporal (G6, git log)     │              │
        │  → P7 cluster + score            │              │
        │                                  │              │
        └────────────────┬─────────────────┘              │
                         ▼                                ▼
               ┌──────────────────┐         ┌─────────────────────────┐
               │  persist.Store   │         │  StoreReader interface  │
               │  (ISP split)     │◀────────│  (read-only consumers)  │
               └────┬─────────┬───┘         └─────────────────────────┘
                    │         │
              SQLite│         │PostgreSQL (--db)
              (default)       │ pgxpool  (C2)
                    │         │
                    ▼         ▼
                  graph.db  ckg schema
                  + manifest.json
```

---

## 4. 패키지 구조 (Source Layout)

```
code-knowledge-graph/
├── cmd/ckg/                ← CLI entry (cobra)
│   ├── main.go             root → Execute()
│   ├── root.go             persistent flags (--verbose, --log-file)
│   ├── build.go            buildpipe.Run()
│   ├── serve.go            server.NewWithOptions()
│   ├── mcp.go              mcp.Run() — stdio
│   ├── export_static.go    StoreReader.ExportChunked()
│   ├── export_postgres.go  pgx COPY (B2)
│   ├── eval.go             eval framework (4 baselines)
│   ├── audit.go            go/packages.Load vs DB diff
│   └── logging.go          slog multiHandler (text+JSON)
│
├── pkg/types/              ← public type system
│   ├── enums.go            33 NodeTypes + 30 EdgeTypes + Confidence
│   ├── node.go             Node struct
│   └── edge.go             Edge struct
│
├── internal/
│   ├── buildpipe/          ← 7-pass orchestrator
│   │   ├── pipeline.go     Run / runCold / runShortCircuit
│   │   ├── language_runners.go  per-lang dispatcher
│   │   ├── cache.go        SchemaVersion="1.5", DiffManifest, cache_key
│   │   ├── incremental.go  D4 escape hatch (dead code preserved)
│   │   ├── temporal.go     P6 git log emit
│   │   └── staleness.go    DB timestamp vs source mtime
│   │
│   ├── detect/             ← P1 file discovery
│   │   ├── walk.go         extension-based (TS/Sol)
│   │   └── go.go           go/packages.Load (Go, build constraints)
│   │
│   ├── parse/              ← P2/P3 parsing + resolve
│   │   ├── parser.go       Parser interface, ResolvedGraph
│   │   ├── dispatch.go     per-lang pipeline runner
│   │   ├── idgen.go        deterministic node ID
│   │   ├── golang/         go/packages + types.Info
│   │   ├── typescript/     tree-sitter v0.25
│   │   └── solidity/       vendored grammar v1.2.11 (ABI 14)
│   │
│   ├── graph/              ← P4 graph build
│   │   └── builder.go      Build (dedup nodes by ID, edges by key)
│   │
│   ├── link/               ← P5 cross-language
│   │   └── sol_to_ts.go    Sol ABI → TS binds_to
│   │
│   ├── temporal/           ← P6 (git → G6 edges)
│   │
│   ├── cluster/            ← P7a Leiden + pkg tree
│   ├── score/              ← P7b PageRank + usage
│   │
│   ├── persist/            ← Storage (ISP)
│   │   ├── store_interface.go  StoreReader / StoreWriter / Store
│   │   ├── sqlite.go       sqliteStore (modernc/sqlite)
│   │   ├── postgres_store.go   pgStore (pgxpool, C2)
│   │   ├── postgres_exporter.go  COPY-based bulk push (B2)
│   │   ├── chunked_export.go   static JSON
│   │   ├── manifest.go     FileEntry, Manifest
│   │   └── schema.sql      8 tables + FTS5
│   │
│   ├── server/             ← HTTP API + embedded viewer
│   │   ├── server.go       Server, NewWithOptions
│   │   ├── api.go          7 handlers
│   │   ├── viewer.go       go:embed web_assets
│   │   ├── community.go    cluster query helpers
│   │   ├── staleness.go    freshness banner
│   │   └── web_assets/     ← Next.js out/ mirror
│   │
│   ├── mcp/                ← MCP stdio server
│   │   ├── server.go       6 tool registration
│   │   ├── tools.go        find_symbol/callers/callees/get_subgraph/search_text
│   │   └── get_context.go  smart 1-shot tool 6
│   │
│   ├── eval/               ← α/β/γ/δ runner
│   ├── audit/              ← parity check
│   └── e2e/                ← end-to-end tests
│
├── web/
│   ├── viewer-next/        ← Next.js 14 + react-force-graph-3d + zustand
│   └── viewer/             ← legacy esbuild (slated for removal)
│
├── eval/tasks/             ← YAML eval scenarios
├── testdata/synthetic/     ← Go 3 + TS 3 + Sol 2 fixtures
└── docs/                   ← 14+ markdown docs
```

---

## 5. 7-Pass Build Pipeline (Cold Path)

```
┌────────────┐
│ ckg build  │  --src=… --out=…
└─────┬──────┘
      │  buildpipe.Run(Options)
      ▼
┌──────────────────────────┐
│ [Cache routing]          │
│  ├─ --no-cache?          │  YES → runCold
│  ├─ Manifest usable?     │  NO  → runCold
│  ├─ All cached?          │  YES → runShortCircuit (1s, manifest refresh)
│  └─ Mixed dirty/cached?  │      → runCold (D4 fallback for correctness)
└─────┬────────────────────┘
      ▼ runCold
┌─────────────────────────────────────────────────────────────────────────┐
│ P1 Detect           detect.Walk + detect.GoFiles (go/packages.Load)     │
│   ↓ DiscoveredFile[]                                                    │
│ P2 Parse (per-lang) Go (types.Info) │ TS (tree-sitter) │ Sol (vendored) │
│   ↓ ParseResult{Nodes, Edges, Pending}                                  │
│ P3 Resolve          Pass 2 — qname → node ID (suffix match)             │
│   ↓ ResolvedGraph per language                                          │
│ P4 Graph Build      graph.Build → dedup nodes by ID, edges by 4-tuple   │
│   ↓ unified Graph{Nodes, Edges}                                         │
│ P5 G5 Distributed   link.SolToTS(ABI) → binds_to edges                  │
│ P6 G6 Temporal      git log --raw → Commit nodes + changed_in/blame     │
│ P7a Cluster         cluster.BuildPkgTree + BuildTopicTree (Leiden γ∈{0.5,1,2}) │
│ P7b Score           score.Compute → PageRank, usage_score               │
│   ↓                                                                      │
│ Persist             openColdStore → wipe → InsertNodes/Edges/Blobs/      │
│                     Trees/PendingRefs → SetManifest → writeManifestJSON  │
└─────────────────────────────────────────────────────────────────────────┘
      │
      ▼
   graph.db + manifest.json + manifest blob
```

**라우팅 결과 (실측 go-stablenet 2142 files)**: cold ~115s · short-circuit <1s · partial=cold-fallback

---

## 6. 6-Graph Axis (CKS Deep-Dive Mapping)

```
┌─────────────────────────┬────────────────────────────────────────────────┐
│ G1 Structural   (50%)   │ contains, defines, imports, exports            │
│                         │ Node: Package, File, Struct, Class, Interface… │
├─────────────────────────┼────────────────────────────────────────────────┤
│ G2 Semantic     (70%)   │ references, implements, extends, uses_type,    │
│                         │ instantiates, reads/writes_field, reads/writes │
│                         │ _mapping, emits_event, has_modifier/decorator  │
├─────────────────────────┼────────────────────────────────────────────────┤
│ G3 Execution    (60%)   │ calls, invokes                                 │
│                         │ Node: IfStmt, LoopStmt, CallSite, ReturnStmt,  │
│                         │       SwitchStmt                               │
├─────────────────────────┼────────────────────────────────────────────────┤
│ G4 Concurrency  (80%)   │ spawns, sends_to, recvs_from,                  │
│                         │ acquires_lock, releases_lock, accessed_under_lock│
│                         │ Node: Goroutine, Channel, Mutex                │
│                         │ B1+G8+G9: Mutex 8→170, accessed_under_lock 0→2916│
├─────────────────────────┼────────────────────────────────────────────────┤
│ G5 Distributed  (70%)   │ listens_on, handles_message, rpc_calls, binds_to│
│                         │ Node: Endpoint, MessageType                    │
│                         │ Sol↔TS xlang via ABI heuristic (INFERRED)      │
├─────────────────────────┼────────────────────────────────────────────────┤
│ G6 Temporal     (90%)   │ changed_in, blame                              │
│                         │ Node: Commit (git log --raw, depth 10)         │
└─────────────────────────┴────────────────────────────────────────────────┘
       Overall CKS coverage: 71%   (cf. ARCHITECTURE-DETAILED.md §7.7)
```

---

## 7. Storage Schema (SQLite, schema 1.5)

```
┌────────────────────────────┐         ┌──────────────────────────┐
│ nodes                      │ 1     N │ edges                    │
│ ───────────                │◀────────│ ───────                  │
│ id (TEXT, PK)              │         │ id (AUTOINC, PK)         │
│ type, name, qualified_name │         │ src ──FK CASCADE         │
│ file_path, start/end_line  │         │ dst ──FK CASCADE         │
│ start/end_byte             │         │ type, file_path, line    │
│ language, visibility       │         │ count, confidence        │
│ signature, doc_comment     │         └──────────────────────────┘
│ complexity, in_/out_degree │
│ pagerank, usage_score      │         ┌──────────────────────────┐
│ confidence, sub_kind       │ 1     1 │ blobs                    │
└──┬─────────────────────────┘────────▶│ node_id ─FK CASCADE      │
   │                                   │ source (BLOB)            │
   │                                   └──────────────────────────┘
   │                                   ┌──────────────────────────┐
   │  1                                │ pkg_tree / topic_tree    │
   ├────────────────────────▶│ parent_id, child_id (FK CASCADE)   │
   │                                   │ resolution, topic_label  │
   │                                   └──────────────────────────┘
   │                                   ┌──────────────────────────┐
   │  1                                │ pending_refs (schema 1.5)│
   ├────────────────────────▶│ src_id (FK CASCADE), target_qname  │
   │                                   │ edge_type, line, hint    │
   │                                   └──────────────────────────┘
   │
   │  FTS5 virtual table: nodes_fts(name, qualified_name, signature, doc_comment)
   ▼
manifest table { schemaVersion, ckgVersion, buildTime, statistics, Files[] }
```

**Schema bump 이력**: 1.0 → 1.1 (lock slots) → 1.2 (ON DELETE CASCADE) → 1.3 (Endpoint/MessageType) → 1.4 (Commit) → **1.5 (pending_refs persistence, partial-cache infra)**

---

## 8. MCP Tool Surface (6 tools)

```
┌──────────────────────────────────────────────────────────────────────┐
│ Granular tools (single-axis)                          Smart tool (★) │
│                                                                       │
│ 1. find_symbol     name → nodes[]                  6. get_context_   │
│ 2. find_callers    qname,depth → reverse BFS          for_task       │
│ 3. find_callees    qname,depth → forward BFS                          │
│ 4. get_subgraph    seed,depth → bidir BFS         (a) FTS5 retrieve  │
│ 5. search_text     q,topK → BM25 + LIKE-CJK       (b) 1-hop expand  │
│                                                    (c) score-fuse:    │
│                                                        0.5·BM25 +     │
│                                                        0.3·PageRank + │
│                                                        0.2·usage      │
│                                                    (d) diversify     │
│                                                    (e) pack ≤budget  │
└──────────────────────────────────────────────────────────────────────┘
   transport: stdio JSON-RPC (mark3labs/mcp-go v0.49)
   eval baselines: α(none) β(get_subgraph) γ(granular) δ(smart 1-shot ★)
```

---

## 9. HTTP Server API + Viewer

```
┌─────────────────── server.NewWithOptions(store, log, opts) ────────────────┐
│                                                                              │
│  Options{ DevViewerDir, NoViewer }                                          │
│   ├─ CKG_DEV_VIEWER_DIR env  → disk-backed (dev hot reload)                │
│   └─ --no-viewer flag         → API-only (production-split)                │
│                                                                              │
│  Routes:                                                                     │
│   GET  /api/manifest        → graph stats + freshness banner                │
│   GET  /api/hierarchy       → pkg_tree / topic_tree                         │
│   GET  /api/nodes           → paginated nodes                               │
│   POST /api/nodes-by-ids    → bulk select                                   │
│   POST /api/edges           → subgraph edges                                │
│   GET  /api/blob/{id}       → source slice                                  │
│   GET  /api/search          → FTS5 query                                    │
│   GET  /                    → embedded Next.js viewer  (or 404 if NoViewer) │
└──────────────────────────────────────────────────────────────────────────────┘
                                       │
                                       ▼
        ┌──────────── Next.js viewer (web/viewer-next) ─────────────┐
        │ react-force-graph-3d + zustand                            │
        │ 6-axis filter UI (G1~G6 group toggle, localStorage 영속)  │
        │ EdgeTypeFilters (collapsible, 3-state)                    │
        │ NodeTypeFilters / SearchPanel / DetailPanel               │
        └────────────────────────────────────────────────────────────┘
```

---

## 10. Cache Routing (A3 Phase 1 + G6 v4)

```
                      ┌─────────────────────────────────┐
                      │   buildpipe.Run(Options)        │
                      └──────────────┬──────────────────┘
                                     │
            ┌────────────────────────┼────────────────────────────┐
            ▼                        ▼                            ▼
      --no-cache=true       no manifest                  manifest exists
            │              schema mismatch                       │
            │                    │                                │
            └──────────┬─────────┘                                │
                       │                                          │
                       │                 ┌────────────────────────┴─────────┐
                       │                 │   DiffManifest classifies files: │
                       │                 │     dirty / cached / removed     │
                       │                 └──────────────┬───────────────────┘
                       │                                │
                       │              ┌─────────────────┼──────────────────┐
                       ▼              ▼                 ▼                  ▼
                  ┌─────────┐  ┌──────────────┐  ┌──────────────┐  ┌────────────┐
                  │ runCold │  │ all cached   │  │ partial hit  │  │ all dirty  │
                  │  (full) │  │ + no removal │  │ (D4 fallback)│  │ (full)     │
                  └─────────┘  │              │  │              │  │            │
                               ▼              │  ▼              │  ▼
                          runShortCircuit     │ runCold (safe)  │ runCold
                          1s manifest refresh │ phantom-edge    │
                          ─ load-bearing CI   │ correctness     │
                                              │                 │
                                              └─ runIncremental │
                                                  (DEAD CODE,   │
                                                   schema 1.5   │
                                                   preserved    │
                                                   for v4 reuse)│
```

**측정 (go-stablenet, 2142 files)**: cold 40s → short-circuit 0.99s | partial=cold-fallback (correctness ≫ speed)

---

## 11. Cache Key & 무효화

```
cache_key = sha256(
    file_content
    + "|ckg:"     + ckg_version       // cmd/ckg/root.go 0.1.0
    + "|parser:"  + parser_version    // Go: runtime.Version() / TS,Sol: TS module
    + "|schema:"  + schema_version    // internal/buildpipe/cache.go "1.5"
)
```

**무효화 트리거**:

| 변경 | 영향 |
|---|---|
| 파일 내용 수정 | 그 파일만 dirty |
| ckgVersion bump | 전체 dirty (= 전체 cold) |
| schema_version bump | 전체 dirty |
| Go toolchain 변경 | 전체 Go file dirty |
| tree-sitter 모듈 bump | 전체 TS/Sol dirty |
| 파일 추가/삭제 | 해당 파일 dirty/removed |

---

## 12. 의존 그래프 (Dependency Flow)

```
cmd/ckg/{build,serve,mcp,eval,audit,export-*}.go
   ├──► internal/buildpipe (build)
   │     ├──► internal/detect            (P1)
   │     ├──► internal/parse/{golang,ts,sol}  (P2/P3)
   │     ├──► internal/graph             (P4)
   │     ├──► internal/link              (P5)
   │     ├──► internal/temporal          (P6)
   │     ├──► internal/cluster           (P7a)
   │     ├──► internal/score             (P7b)
   │     └──► internal/persist           (write)
   │
   ├──► internal/server  (serve)         ──► persist.StoreReader
   ├──► internal/mcp     (mcp)           ──► persist.StoreReader
   ├──► internal/eval    (eval)          ──► persist.StoreReader + Anthropic SDK
   ├──► internal/audit   (audit)         ──► persist.StoreReader + go/packages
   └──► internal/persist (export-static / export-postgres)

pkg/types  ←  공통 enum/struct (NodeType, EdgeType, Confidence, Node, Edge)
```

**ISP 분리**: `StoreReader` (read consumers) ⊂ `Store` ⊃ `StoreWriter` (buildpipe only)
**Backend 교체점**: `--db postgres://...` → `pgStore` (pgxpool ~1160 LOC) vs `sqliteStore` (default)

---

## 13. 7개 subcommand 요약

| Subcommand | 용도 | 입력 | 출력 |
|---|---|---|---|
| `build` | 그래프 생성 | `--src` | `graph.db` + `manifest.json` |
| `serve` | HTTP API + viewer | `--graph` (or `--db`) | `:8080` |
| `mcp` | stdio MCP server | `--graph` | 6 tools |
| `export-static` | 정적 호스팅용 chunked JSON | `--graph` | `out/*.json` + viewer |
| `export-postgres` | SQLite → PG one-shot | `--dsn`, `--source` | PG schema |
| `eval` | 4-baseline 비교 | `--tasks`, `--graph` | CSV + report.md |
| `audit` | 파일 누락 검증 | `--src`, `--graph` | exit 0/1/2 |

**Persistent flags (모든 subcommand)**: `--verbose`, `--log-file <path>`, `CKG_LOG_LEVEL=debug`

---

## 14. 검증된 동작 (Capability)

```
┌──────────── 사용자 4 완성도 조건 ─────────────────────────────┐
│ #1 모든 파일 누락없이 DB화         ✅ E2 (go/packages.Load)   │
│ #2 audit으로 검증 가능             ✅ E1 (ckg audit)          │
│ #3 CKS 6 graph (G1~G6) 지원        ✅ B1+E3+E4+G8+G9          │
│ #4 viewer + CLI eval               ✅ E5 + α/β/γ/δ            │
└────────────────────────────────────────────────────────────────┘

go-stablenet 실측 (2142 files):
  - audit: PARITY (1259/1259, exit 0)
  - cold:    214,343 nodes / 652,892 edges
  - partial: 214,343 nodes / 652,892 edges  ← G6 v4 후 diff = 0 ✅
```

**측정 가능한 개선 (Wave 5 + Group G)**:

| 메트릭 | Pre | Post | Δ |
|---|---|---|---|
| Mutex nodes | 0 (B1 전) | 170 (G9 후) | — |
| acquires_lock edges | 0 | 781 | — |
| accessed_under_lock edges | 0 | 2916 | — |
| Field-misclassified acquires_lock | 157 | 1 | -99.4% |
| changed_in edges (E4) | 0 | 344,946 | — |
| audit drift (E2) | 41 over-include | 0 | PARITY |
| pipeline.go LOC (G4) | 596 | 359 | -40% |

---

## 15. 다음 작업 (Wave 9 진입 가능)

| 우선 | 작업 | 추정 |
|---|---|---|
| 1 | **B3** Tree.Edit() incremental parsing | M |
| 2 | E2-FU `go.work` 회귀 테스트 | S |
| 2 | Wave1 DoD viewer dead-key 정리 (`reads/writes/…`) | S |
| 3 | E3-FU httprouter / Ethereum RPC client.Call | S |
| 3 | E4-FU line-level blame | M |
| 4 | **D1** SSA 정밀 동시성 (`--deep` opt-in) | XL |
| 4 | **D2** pgvector + Apache AGE | XL |

**의존성 그래프 (현재)**:
```
A1 ──► A2 (병렬 가능)                               ✅
A1 ──► B3 (incremental parsing)                    ← next
A3 ──► C1 (Pass 2 invalidation)                    ✅
A4 ──► B2 ──► C2 ──► D2                            ✅ (C2까지)
A5 ──► B1 ──► D1                                   ✅ (B1까지)
E1 ──► E2                                          ✅
E3, E4 ──► E5                                      ✅
F1, F2, F3                                         ✅
G6 v4 ──► C1 ──► B3                                ✅ (B3 진입 가능)
```

---

## 16. 운영 함정 (HANDOFF.md § 5에서 누적)

1. **subagent stall**: 큰 task는 token budget 명시(150-200K), real-corpus parity check 강제, 측정 결과 받기 전 commit 금지
2. **gopls 캐시 지연**: `BrokenImport`/`UndeclaredName` IDE 경고는 false positive, `go test ./...` 그린이면 무시
3. **commit 컨벤션**: NO Co-Authored-By 헤더, NO emoji, Conventional Commits English subject ≤70 chars, *why* 중심
4. **Viewer build coupling**: `make build` 시 stub-restore 메커니즘으로 `git status` clean 유지
5. **partial-cache D4**: mixed dirty/cached → cold fallback (correctness > speed). G6 v4(`ORDER BY start_line`) + C1(reverse-ref) 후 cold vs partial diff = 0 ✅
6. **Heredoc commit message**: perl regex 같은 escape-prone tooling 금지 (이전에 WORK-PLAN.md 망친 사고 있음)

---

## 17. 핵심 설계 원칙

| 원칙 | 구현 |
|---|---|
| **Single binary** | go:embed로 viewer까지 단일 실행파일 |
| **CGO-free default** | `modernc.org/sqlite` (cross-platform CI matrix) |
| **ISP** | Store interface 3분할 — read consumers는 writer 의존 X |
| **Pluggable backend** | `--db postgres://...` (B2/C2 완성) |
| **Cache correctness > speed** | partial-hit는 cold fallback (D4) |
| **Append-only enums** | NodeType/EdgeType 위치 변경 금지 (hash ID stability) |
| **Schema bump = global cache invalidation** | silent corruption 방어 |
| **Confidence triple** | EXTRACTED / INFERRED / AMBIGUOUS — 휴리스틱 정직성 |
| **CKS 6-graph 분리** | G1~G6 viewer toggle, MCP tool과 1:1 매핑 가능 |
| **Subagent-driven dev** | impl → review → fix loop, real-corpus parity check 강제 |

---

## Appendix A: 환경 의존성

### Go module (`go.mod`)

```
module github.com/0xmhha/code-knowledge-graph
go 1.25.5

require (
    github.com/0xmhha/cli-wrapper v0.2.1
    github.com/anthropics/anthropic-sdk-go v1.38.0
    github.com/jackc/pgx/v5 v5.9.2                          // B2 + C2 (PG)
    github.com/mark3labs/mcp-go v0.49.0                     // MCP stdio
    github.com/spf13/cobra v1.10.2                          // CLI
    github.com/tree-sitter/go-tree-sitter v0.25.0           // A1+A2 (smacker 대체)
    github.com/tree-sitter/tree-sitter-javascript v0.25.0
    github.com/tree-sitter/tree-sitter-typescript v0.23.2
    golang.org/x/tools v0.44.0                              // go/packages
    gopkg.in/yaml.v3 v3.0.1
    modernc.org/sqlite v1.49.1                              // CGO-free
)
```

### Vendored

- `internal/parse/solidity/binding/` — JoranHonig/tree-sitter-solidity v1.2.11 (LANGUAGE_VERSION=14, ABI 14는 upstream go-tree-sitter v0.25 ABI window 13..15 안에 들어가 regenerate 불요)

### Build artifacts (gitignored)

- `bin/ckg` — `make build`
- `web/viewer-next/{out,.next,node_modules}/`
- `internal/server/web_assets/_next/`, `404/`, `404.html`, `index.txt` (stub `index.html`만 commit)

### 검증 corpus

- `testdata/synthetic/` — Go 3 + TS 3 + Sol 2 = 8 files (소규모, 빠름)
- `go-stablenet-latest` — Go 1259 + TS 320 + Sol 563 = 2142 files (Ethereum-derived, 실 corpus)

---

## Appendix B: Quick Start (5분)

```bash
cd <repo root>
git log --oneline -10
go test ./...                               # 18 packages PASS
make build                                  # Next.js viewer + ckg binary
./bin/ckg build --src=testdata/synthetic --out=/tmp/ckg-synth
./bin/ckg serve --graph=/tmp/ckg-synth --port=8080 --open
./bin/ckg audit --src=testdata/synthetic --graph=/tmp/ckg-synth   # exit 0 = parity

# Wave 7 (Group F) 검증
./bin/ckg serve --graph=/tmp/ckg-synth --no-viewer --port=8788    # API only
make viewer && CKG_DEV_VIEWER_DIR=$(pwd)/internal/server/web_assets \
  ./bin/ckg serve --graph=/tmp/ckg-synth --port=8789              # disk viewer

# PostgreSQL backend (선택)
./bin/ckg build --src=testdata/synthetic --db=postgres://user:pass@localhost/ckg
./bin/ckg serve --db=postgres://user:pass@localhost/ckg --port=8080
```

---

**End of code structure overview.** 본 문서는 visual + structural index 역할로, 깊은 구현 디테일은 `ARCHITECTURE-DETAILED.md` 17 sections 참조.
