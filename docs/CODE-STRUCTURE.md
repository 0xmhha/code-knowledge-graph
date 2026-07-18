# CKG — 코드 설계 구조 종합 정리

> **목적**: 소스코드 패키지 구조 · 빌드 파이프라인 · 6-graph axis · 캐시 라우팅을
> 한 문서에서 다이어그램으로 조망하는 **visual + structural index**.
> ARCHITECTURE.md(1-page) ↔ ARCHITECTURE-DETAILED.md 사이의 시각적 인덱스 역할.
> **문서 목록/위계는 이 문서가 아니라 [docs/DOC-MAP.md](DOC-MAP.md)가 authoritative.**
>
> **대상 독자**: cold-read 신규 합류자 / 다음 세션 시작 시 / external reviewer
> **마지막 갱신**: 2026-07-18 (full refresh — schema 1.23 / eval-retrieval surface /
> pkg/mcphandlers 10 tools / binary-reachable rescoping PR #55 반영)

---

## 목차

1. [프로젝트 개요](#1-프로젝트-개요)
2. [문서 맵 (→ DOC-MAP.md)](#2-문서-맵--doc-mapmd)
3. [시스템 개요 (High-Level Architecture)](#3-시스템-개요-high-level-architecture)
4. [패키지 구조 (Source Layout)](#4-패키지-구조-source-layout)
5. [7-Pass Build Pipeline](#5-7-pass-build-pipeline-cold-path)
6. [6-Graph Axis (CKS Deep-Dive Mapping)](#6-6-graph-axis-cks-deep-dive-mapping)
7. [Storage Schema](#7-storage-schema-sqlite-schema-123)
8. [MCP Tool Surface](#8-mcp-tool-surface-10-tools)
9. [HTTP Server API + Viewer](#9-http-server-api--viewer)
10. [Cache Routing (A3 Phase 1 + G6 v4)](#10-cache-routing-a3-phase-1--g6-v4)
11. [Cache Key & 무효화](#11-cache-key--무효화)
12. [의존 그래프 (Dependency Flow)](#12-의존-그래프-dependency-flow)
13. [Subcommand 요약](#13-subcommand-요약-5-production-surfaces--cobra-root-약-20개-등록)
14. [검증된 동작 (Capability)](#14-검증된-동작-capability)
15. [핵심 설계 원칙](#15-핵심-설계-원칙)

---

## 1. 프로젝트 개요

| Field | Value |
|---|---|
| **이름** | CKG (Code Knowledge Graph) |
| **버전** | `ckgVersion = "0.1.0"` (`cmd/ckg/root.go:5`), extraction schema **1.23** (`internal/buildpipe/cache.go:166`) |
| **언어** | Go 1.25.x (single binary, CGO-free default) |
| **목적** | Go / TypeScript / Solidity / Protobuf 소스 → 코드 지식 그래프. NodeType/EdgeType 카탈로그와 schema 이력은 **docs/SCHEMA.md**가 authoritative (→ `pkg/types/enums.go` `AllNodeTypes()` / `AllEdgeTypes()`) |
| **활용** | 3D viewer + MCP server + keyword-retrieval eval + audit(parity) |
| **검증 corpus** | go-stablenet + synthetic fixtures. 노드/엣지 절대 수치는 PR #55(binary-reachable 재범위화, commit bf59fdb) 이후 변동 — 아래 수치는 **재측정 필요** |
| **저장소** | SQLite (default, modernc) / PostgreSQL (`--db postgres://…`, pgxpool; ADR-0003에서 deprecate 결정) |

---

## 2. 문서 맵 (→ DOC-MAP.md)

문서 목록·위계·authoritative 판정은 **[docs/DOC-MAP.md](DOC-MAP.md)** 하나가 소유한다.
여기서 병렬 인덱스를 유지하지 않는다(중복 = 두 곳이 서로 stale 해지는 원인).

빠른 방향만:

| 질문 | 정답 문서 |
|---|---|
| "지금 무엇이 참인가" (current state) | **코드 + git** (ground truth). 상태 스냅샷은 **[CONTINUITY.md](CONTINUITY.md)** — 유일한 live Tier 3 status |
| "왜 그렇게 결정했나" (why decided) | `docs/adr/` ADR + Tier 2 design 문서 |
| "무엇을 지향하나" (vision) | **[VISION.md](VISION.md)** |
| 노드/엣지 카탈로그 · schema 이력 | **[SCHEMA.md](SCHEMA.md)** (authoritative) |
| 캐시 동작 | **[INCREMENTAL.md](INCREMENTAL.md)** |
| 전체 설계 deep-dive | **[ARCHITECTURE-DETAILED.md](ARCHITECTURE-DETAILED.md)** |

> **주의**: 과거 이 문서가 나열하던 SESSION-HANDOFF / NEXT-CANDIDATES /
> PROJECT-OVERVIEW / SELF-VERIFICATION / spec-ckg-v0.2 / eval-trajectory /
> CAPABILITY-AUDIT / PROJECT-BLUEPRINT-ALIGNMENT 등은 `docs/archive/`로 이동됨.
> live 문서로 취급하지 말 것. 최신 목록은 항상 DOC-MAP.md를 볼 것.

---

## 3. 시스템 개요 (High-Level Architecture)

```
                     ┌───────────────────────────────────────────┐
                     │           ckg (Single Go Binary)          │
                     │   build / serve / mcp / eval-retrieval /  │
                     │   audit  ─  cobra rootCmd (~20 subcmds)   │
                     └──────────────┬──────────────┬─────────────┘
                                    │              │
                                    ▼              ▼
        ┌─────────────────────────────────┐   ┌──────────────────────┐
        │   buildpipe.Run() (orchestrator) │   │   Query surfaces     │
        └─────────────────────────────────┘   │  - HTTP API + viewer │
                                    │          │  - MCP (10 tools)   │
        ┌─── 7-Pass build pipeline ──┴────┐    │  - audit (parity)   │
        │                                 │    │  - eval-retrieval   │
        │  P1 detect  → P2 parse (4 lang) │    │  - export-*         │
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
              (default)       │ pgxpool  (ADR-0003 deprecated)
                    │         │
                    ▼         ▼
                  graph.db  ckg schema
                  + manifest.json
```

파서 4종: Go (`internal/parse/golang`), TypeScript (`typescript`), Solidity
(`solidity`), Protobuf (`proto`).

---

## 4. 패키지 구조 (Source Layout)

```
code-knowledge-graph/
├── cmd/ckg/                ← CLI entry (cobra); ~20 subcommand 등록 (root.go:30-36)
│   ├── main.go             root → Execute()
│   ├── root.go             ckgVersion="0.1.0", persistent flags (--verbose, --log-file)
│   ├── build.go            buildpipe.Run()
│   ├── serve.go            server.NewWithOptions()
│   ├── mcp.go              mcphandlers.RegisterAll — stdio
│   ├── watch.go            fsnotify 기반 재빌드 watcher
│   ├── export_static.go    StoreReader.ExportChunked()
│   ├── export_postgres.go  pgx COPY
│   ├── export_json.go      JSON dump
│   ├── eval_retrieval.go   keyword-retrieval eval (구 "eval" surface; eval.go는 없음)
│   ├── audit.go            go/packages.Load vs DB diff
│   ├── validate.go         graph 무결성 검증
│   ├── evidence.go         evidence-for-intent 조회
│   ├── query.go / path.go / report.go / quickstart.go   유틸
│   ├── benchmark.go / bench_server.go / bench_mcp.go /
│   │   bench_mcp_stdio.go / bench_index.go               벤치 표면
│   └── logging.go          slog multiHandler (text+JSON)
│
├── pkg/                    ← public API / stable contract (CKV·CKS 소비; 11 packages)
│   ├── types/              enums.go(NodeType/EdgeType — authoritative: docs/SCHEMA.md),
│   │                       node.go, edge.go
│   ├── bm25/               BM25 랭킹
│   ├── concurrency/        동시성 영향 분석 (concurrency_impact tool)
│   ├── evidence/           evidence-for-intent + cache
│   ├── hunkmodifies/       Hunk-graph 파생
│   ├── impact/             impact-of-change 분석
│   ├── mcphandlers/        ★ MCP tool 구현 (registerall.go → RegisterAll)
│   ├── policy/             정책/필터
│   ├── security/           보안 규칙
│   ├── smartctx/           get_context_for_task 스마트 패킹
│   └── store/              store.Reader 등 public store 계약
│
├── internal/
│   ├── buildpipe/          ← 7-pass orchestrator
│   │   ├── pipeline.go     Run / runCold / runShortCircuit / runIncremental (dispatch)
│   │   ├── cache.go        const SchemaVersion="1.23", DiffManifest, cache_key
│   │   ├── incremental.go  runIncremental — LIVE (pipeline.go:260에서 호출)
│   │   ├── temporal.go     P6 git log emit (temporalDepthDefault=10)
│   │   └── staleness.go    DB timestamp vs source mtime
│   │
│   ├── detect/             ← P1 file discovery (walk + go/packages.Load)
│   ├── parse/              ← P2/P3 parsing + resolve
│   │   ├── parser.go       Parser interface, ResolvedGraph
│   │   ├── dispatch.go     per-lang pipeline runner
│   │   ├── idgen.go        deterministic node ID
│   │   ├── golang/         go/packages + types.Info
│   │   ├── typescript/     tree-sitter
│   │   ├── solidity/       vendored grammar
│   │   └── proto/          protobuf (.proto) 파서   ← 4번째 언어
│   │
│   ├── graph/              ← P4 graph build (dedup nodes by ID, edges by key)
│   ├── link/               ← P5 cross-language (Sol ABI → TS binds_to)
│   ├── temporal/           ← P6 git → G6 edges
│   ├── cluster/            ← P7a Leiden + pkg tree
│   ├── score/              ← P7b PageRank + usage
│   ├── filterlist/         ← include/exclude 필터 목록 처리
│   ├── validate/           ← 그래프 무결성 검증 (validate 서브커맨드)
│   │
│   ├── persist/            ← Storage (ISP)
│   │   ├── store_interface.go  StoreReader / StoreWriter / Store
│   │   ├── sqlite*.go       sqliteStore (modernc/sqlite) + reader/writer/migrate
│   │   ├── postgres_*.go    pgStore (pgxpool) + COPY exporter (ADR-0003 deprecated)
│   │   ├── chunked_export.go   static JSON
│   │   ├── manifest.go     Manifest / SchemaVersion(back-compat policy — cache.go와 별개)
│   │   └── schema.sql      tables + FTS5
│   │
│   ├── server/             ← HTTP API + embedded viewer (go:embed web_assets)
│   ├── mcp/                ← MCP stdio 진입점 (server.go → mcphandlers.RegisterAll)
│   ├── eval/retrieval/     ← keyword-retrieval eval + fixtures
│   ├── audit/              ← parity check
│   └── e2e/                ← end-to-end tests
│
├── web/viewer-next/        ← Next.js 3D force-graph viewer (embedded)
├── eval/                   ← YAML eval scenarios / corpora
├── testdata/               ← synthetic fixtures
└── docs/                   ← DOC-MAP.md가 index (본 문서 포함)
```

> **두 개의 SchemaVersion 구분** (CLAUDE.md 규약): `internal/persist/manifest.go`의
> `SchemaVersion`(manifest 후방호환 정책, BREAKING 시에만 bump) ≠
> `internal/buildpipe/cache.go`의 `SchemaVersion="1.23"`(cache-key 기여자, bump 시
> 캐시 무효화 → 전체 reindex). 둘을 혼동하지 말 것.

---

## 5. 7-Pass Build Pipeline (Cold Path)

```
┌────────────┐
│ ckg build  │  --src=… --out=…
└─────┬──────┘
      │  buildpipe.Run(Options)
      ▼
┌──────────────────────────┐
│ [Cache routing]          │  (pipeline.go dispatch)
│  ├─ --no-cache / no manifest / schema mismatch  → runCold
│  ├─ 100% cached, no removals                    → runShortCircuit (~1s)
│  └─ partial hit (dirty+cached 혼재)             → runIncremental (LIVE)
└─────┬────────────────────┘
      ▼ runCold
┌─────────────────────────────────────────────────────────────────────────┐
│ P1 Detect           detect.Walk + go/packages.Load                       │
│   ↓ DiscoveredFile[]                                                    │
│ P2 Parse (per-lang) Go │ TS │ Sol │ Proto  (4 languages)                │
│   ↓ ParseResult{Nodes, Edges, Pending}                                  │
│ P3 Resolve          Pass 2 — qname → node ID (suffix match)             │
│   ↓ ResolvedGraph per language                                          │
│ P4 Graph Build      graph.Build → dedup nodes by ID, edges by key       │
│   ↓ unified Graph{Nodes, Edges}                                         │
│ P5 G5 Distributed   link.SolToTS(ABI) → binds_to edges                  │
│ P6 G6 Temporal      git log --raw → Commit nodes + changed_in/blame     │
│ P7a Cluster         cluster.BuildPkgTree + BuildTopicTree (Leiden)      │
│ P7b Score           score.Compute → PageRank, usage_score               │
│   ↓                                                                      │
│ Persist             openColdStore → wipe → InsertNodes/Edges/Blobs/      │
│                     Trees/PendingRefs → SetManifest → writeManifestJSON  │
└─────────────────────────────────────────────────────────────────────────┘
      │
      ▼
   graph.db + manifest.json + manifest blob
```

**라우팅 3-way**: cold(전체) · short-circuit(100% hit, manifest refresh) ·
incremental(부분 hit — dirty만 파싱 + cached reload). 세 경로 모두 live
(`internal/buildpipe/pipeline.go:258-265`). 런타임 소요 수치는 corpus·머신 의존이며
PR #55 이후 재측정 필요.

---

## 6. 6-Graph Axis (CKS Deep-Dive Mapping)

```
┌─────────────────────────┬────────────────────────────────────────────────┐
│ G1 Structural           │ contains, defines, imports, exports            │
│                         │ Node: Package, File, Struct, Class, Interface… │
├─────────────────────────┼────────────────────────────────────────────────┤
│ G2 Semantic             │ references, implements, extends, uses_type,    │
│                         │ instantiates, reads/writes_field, reads/writes │
│                         │ _mapping, emits_event, has_modifier/decorator  │
├─────────────────────────┼────────────────────────────────────────────────┤
│ G3 Execution            │ calls, invokes                                 │
│                         │ Node: IfStmt, LoopStmt, CallSite, ReturnStmt,  │
│                         │       SwitchStmt                               │
├─────────────────────────┼────────────────────────────────────────────────┤
│ G4 Concurrency          │ spawns, sends_to, recvs_from,                  │
│                         │ acquires_lock, releases_lock, accessed_under_lock│
│                         │ Node: Goroutine, Channel, Mutex                │
├─────────────────────────┼────────────────────────────────────────────────┤
│ G5 Distributed          │ listens_on, handles_message, rpc_calls, binds_to│
│                         │ Node: Endpoint, MessageType                    │
│                         │ Sol↔TS xlang via ABI heuristic (INFERRED)      │
├─────────────────────────┼────────────────────────────────────────────────┤
│ G6 Temporal             │ changed_in, blame                              │
│                         │ Node: Commit (git log --raw, depth 10)         │
└─────────────────────────┴────────────────────────────────────────────────┘
```

축 정의(edge/node type 매핑)는 `pkg/types/enums.go` + docs/SCHEMA.md가 authoritative.
CKS coverage 비율·축별 edge/node **절대 수치**는 런타임 측정치이며 PR #55(binary-reachable
재범위화) 이후 stale — 정확한 값은 `ckg audit` / 재빌드로 재측정할 것.

---

## 7. Storage Schema (SQLite, schema 1.23, authoritative: docs/SCHEMA.md)

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
│ confidence, sub_kind …     │ 1     1 │ blobs                    │
└──┬─────────────────────────┘────────▶│ node_id ─FK CASCADE      │
   │                                   │ source (BLOB)            │
   │                                   └──────────────────────────┘
   │                                   ┌──────────────────────────┐
   │  1                                │ pkg_tree / topic_tree    │
   ├────────────────────────▶│ parent_id, child_id (FK CASCADE)   │
   │                                   │ resolution, topic_label  │
   │                                   └──────────────────────────┘
   │                                   ┌──────────────────────────┐
   │  1                                │ pending_refs             │
   ├────────────────────────▶│ src_id (FK CASCADE), target_qname  │
   │                                   │ edge_type, line, hint    │
   │                                   └──────────────────────────┘
   │
   │  FTS5 virtual table: nodes_fts(name, qualified_name, signature, doc_comment)
   ▼
manifest table { schema_version, ckg_version, buildTime, statistics, Files[] }
```

**Schema 버전**: 현재 **1.23** (`internal/buildpipe/cache.go:166`). 전체 bump 이력 +
NodeType/EdgeType 카탈로그는 **docs/SCHEMA.md**가 단일 authoritative 소스
(→ `pkg/types/enums.go`). 이 문서는 수치를 복제하지 않는다.

---

## 8. MCP Tool Surface (10 tools)

authoritative 등록 목록: `pkg/mcphandlers/registerall.go` `RegisterAll` (내부적으로
`internal/mcp/server.go`가 호출). 과거의 `internal/mcp/{tools.go,get_context.go}`는
제거됨 — 모든 tool 구현은 `pkg/mcphandlers/`로 이동.

```
┌──────────────────────────────────────────────────────────────────────┐
│ 1. find_symbol            name → nodes[]                              │
│ 2. find_callers           qname,depth → reverse BFS                   │
│ 3. find_callees           qname,depth → forward BFS                   │
│ 4. get_subgraph           seed,depth → bidir BFS                      │
│ 5. search_text            q,topK → BM25 + FTS5                        │
│ 6. get_context_for_task   ★ smart 1-shot (retrieve→expand→fuse→pack) │
│ 7. impact_of_change       변경 파급 분석 (pkg/impact)                 │
│ 8. concurrency_impact     동시성 영향 (pkg/concurrency)               │
│ 9. change_history         G6 temporal 이력                            │
│10. evidence_for_intent    의도별 근거 (pkg/evidence, NewCache)        │
└──────────────────────────────────────────────────────────────────────┘
   transport: stdio JSON-RPC (mark3labs/mcp-go)
   §11.3 H3 retrieval boundary는 각 Register* 내부에서 강제됨
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

> viewer는 `make build-full` 시에만 실제로 빌드됨. `make build`는 Go 바이너리만
> 만들고 embedded viewer는 tracked stub으로 남는다.

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
                  │  (full) │  │ + no removal │  │              │  │ (full)     │
                  └─────────┘  └──────┬───────┘  └──────┬───────┘  └─────┬──────┘
                                      ▼                 ▼                ▼
                              runShortCircuit    runIncremental       runCold
                              (~1s manifest      (LIVE — dirty만       (full)
                               refresh;           파싱 + cached
                               load-bearing CI)   reload; pipeline.go:260)
```

partial hit는 이제 **runIncremental**로 처리된다 (LIVE, `internal/buildpipe/pipeline.go:260`).
과거 문서의 "partial=cold-fallback / runIncremental=DEAD CODE" 서술은 폐기됨.
cache-key contributor는 `internal/buildpipe/cache.go` `SchemaVersion="1.23"`.

---

## 11. Cache Key & 무효화

```
cache_key = sha256(
    file_content
    + "|ckg:"     + ckg_version       // cmd/ckg/root.go:5  "0.1.0"
    + "|parser:"  + parser_version    // Go: runtime.Version() / TS,Sol,Proto: module ver
    + "|schema:"  + schema_version    // internal/buildpipe/cache.go:166  "1.23"
)
```

**무효화 트리거**:

| 변경 | 영향 |
|---|---|
| 파일 내용 수정 | 그 파일만 dirty |
| ckgVersion bump | 전체 dirty (= 전체 cold) |
| schema_version bump (cache.go) | 전체 dirty |
| Go toolchain 변경 | 전체 Go file dirty |
| tree-sitter / grammar 모듈 bump | 해당 언어 파일 dirty |
| 파일 추가/삭제 | 해당 파일 dirty/removed |

---

## 12. 의존 그래프 (Dependency Flow)

```
cmd/ckg/{build,serve,mcp,eval-retrieval,audit,validate,export-*}.go
   ├──► internal/buildpipe (build)
   │     ├──► internal/detect            (P1)
   │     ├──► internal/parse/{golang,typescript,solidity,proto}  (P2/P3)
   │     ├──► internal/graph             (P4)
   │     ├──► internal/link              (P5)
   │     ├──► internal/temporal          (P6)
   │     ├──► internal/cluster           (P7a)
   │     ├──► internal/score             (P7b)
   │     └──► internal/persist           (write)
   │
   ├──► internal/mcp     (mcp)           ──► pkg/mcphandlers ──► pkg/store.Reader
   ├──► internal/server  (serve)         ──► persist.StoreReader
   ├──► internal/eval/retrieval (eval-retrieval) ──► StoreReader (keyword-retrieval)
   ├──► internal/audit   (audit)         ──► persist.StoreReader + go/packages
   ├──► internal/validate (validate)     ──► persist.StoreReader
   └──► internal/persist (export-static / export-postgres / export-json)

pkg/  ←  public contract (types, store, bm25, smartctx, impact, concurrency,
         evidence, hunkmodifies, policy, security, mcphandlers) — CKV/CKS 소비
```

**ISP 분리**: `StoreReader` (read consumers) ⊂ `Store` ⊃ `StoreWriter` (buildpipe only).
**Backend 교체점**: `--db postgres://…` → `pgStore` (pgxpool) vs `sqliteStore` (default).
PostgreSQL 백엔드는 ADR-0003에서 deprecate 결정 — SQLite가 유일 유지 대상.

---

## 13. Subcommand 요약 (5 production surfaces · cobra root 약 20개 등록)

cobra root는 약 20개 subcommand를 등록한다 (`cmd/ckg/root.go:30-36`의 `AddCommand`).
그중 **5 production surfaces** (build / serve / mcp / eval-retrieval / audit)가 1차 표면이고
나머지는 export / bench / report / query / validate / watch 등 유틸리티. 대표 발췌:

| Subcommand | 용도 | 입력 | 출력 |
|---|---|---|---|
| `build` | 그래프 생성 | `--src` | `graph.db` + `manifest.json` |
| `serve` | HTTP API + viewer | `--graph` (or `--db`) | `:8080` |
| `mcp` | stdio MCP server | `--graph` | 10 tools |
| `eval-retrieval` | keyword-retrieval eval | `--graph`, tasks | 측정 리포트 |
| `audit` | 파일 누락 검증 (parity) | `--src`, `--graph` | exit 0/1/2 |
| `export-static` | 정적 호스팅용 chunked JSON | `--graph` | `out/*.json` + viewer |
| `export-postgres` | SQLite → PG one-shot | `--dsn`, `--source` | PG schema |
| `validate` | 그래프 무결성 검증 | `--graph` | pass/fail |
| `watch` | 소스 변경 감시 재빌드 | `--src` | fsnotify loop |

> 구 `eval` 서브커맨드/`cmd/ckg/eval.go`는 존재하지 않는다 — 실제 커맨드는
> `eval-retrieval` (`cmd/ckg/eval_retrieval.go`).

**Persistent flags (모든 subcommand)**: `--verbose`, `--log-file <path>`, `CKG_LOG_LEVEL=debug`

---

## 14. 검증된 동작 (Capability)

```
┌──────────── 사용자 완성도 조건 ───────────────────────────────┐
│ #1 모든 파일 누락없이 DB화         ✅ go/packages.Load        │
│ #2 audit으로 검증 가능             ✅ ckg audit               │
│ #3 CKS 6 graph (G1~G6) 지원        ✅ enums.go 축 매핑        │
│ #4 viewer + CLI eval               ✅ serve + eval-retrieval  │
└────────────────────────────────────────────────────────────────┘
```

> **수치 주의**: 과거 스냅샷의 노드/엣지/edge-type 절대 수치(예: Mutex nodes,
> acquires_lock / accessed_under_lock / changed_in edge 수, 파일별 parity 카운트)는
> PR #55(binary-reachable 재범위화, commit bf59fdb)로 그래프가 MAIN_PKG-도달 코드로
> 좁혀지면서 모두 변동됨. 이 문서는 임의 수치를 재기재하지 않는다 — 최신 값은
> `ckg build` 후 `ckg audit` / `/api/manifest`로 재측정할 것.

---

## 15. 핵심 설계 원칙

| 원칙 | 구현 |
|---|---|
| **Single binary** | go:embed로 viewer까지 단일 실행파일 (`make build-full`) |
| **LLM-free deterministic build** | 그래프 빌드는 LLM 미사용·결정적. LLM은 eval 표면에만 |
| **CGO-free default** | `modernc.org/sqlite` (cross-platform CI matrix) |
| **ISP** | Store interface 3분할 — read consumers는 writer 의존 X |
| **Public boundary** | 교차-repo API는 `pkg/`에만. CKV/CKS는 `internal/` 접근 금지 |
| **Cache correctness > speed** | partial-hit도 정확성 우선 (runIncremental가 phantom-edge 방지) |
| **Append-only enums** | NodeType/EdgeType 위치 변경 금지 (hash ID stability) |
| **Two SchemaVersions** | manifest(back-compat) ≠ cache(cache-key) — 혼동 금지 |
| **Confidence triple** | EXTRACTED / INFERRED / AMBIGUOUS — 휴리스틱 정직성 |
| **Supersede, don't delete (docs)** | 결정=ADR 1개; 변경 시 새 ADR + "Superseded by" |

---

## Appendix A: 환경 의존성

### Go module (`go.mod`)

```
module github.com/0xmhha/code-knowledge-graph
go 1.25.5
toolchain go1.25.12

require (
    github.com/fsnotify/fsnotify v1.10.1                    // watch 서브커맨드
    github.com/jackc/pgx/v5 v5.10.0                         // PG (ADR-0003 deprecated)
    github.com/mark3labs/mcp-go v0.56.0                     // MCP stdio
    github.com/spf13/cobra v1.10.2                          // CLI
    github.com/tree-sitter/go-tree-sitter v0.25.0
    github.com/tree-sitter/tree-sitter-javascript v0.25.0
    github.com/tree-sitter/tree-sitter-typescript v0.23.2
    golang.org/x/tools v0.48.0                              // go/packages
    gopkg.in/yaml.v3 v3.0.1
    modernc.org/sqlite v1.53.0                              // CGO-free
)
```

> extraction schema = **1.23** (`internal/buildpipe/cache.go:166`), ckgVersion =
> **0.1.0** (`cmd/ckg/root.go:5`). 과거 부록의 `anthropic-sdk-go` /
> `cli-wrapper` 의존은 현재 `go.mod`에 없음. 위 버전은 현 `go.mod` 기준이며 이후
> 갱신될 수 있다 — 정확한 값은 `go.mod`가 ground truth.

### Vendored

- `internal/parse/solidity/binding/` — vendored tree-sitter-solidity grammar
  (구체 버전/ABI window는 코드가 ground truth; **버전 문자열 미검증**).

### Build artifacts (gitignored)

- `bin/ckg` — `make build`
- `web/viewer-next/{out,.next,node_modules}/`
- `internal/server/web_assets/_next/`, `404/`, `404.html`, `index.txt`
  (tracked stub `index.html`만 commit)

---

## Appendix B: Quick Start

```bash
cd <repo root>
git log --oneline -10
make test                                   # = go test ./...
make build-full                             # Next.js viewer + ckg binary
./bin/ckg build --src=testdata --out=/tmp/ckg-synth
./bin/ckg serve --graph=/tmp/ckg-synth --port=8080 --open
./bin/ckg audit --src=testdata --graph=/tmp/ckg-synth   # exit 0 = parity

# API-only / disk viewer
./bin/ckg serve --graph=/tmp/ckg-synth --no-viewer --port=8788
make viewer && CKG_DEV_VIEWER_DIR=$(pwd)/internal/server/web_assets \
  ./bin/ckg serve --graph=/tmp/ckg-synth --port=8789
```

---

**End of code structure overview.** 본 문서는 visual + structural index이며, 문서
목록은 `docs/DOC-MAP.md`, 노드/엣지·schema는 `docs/SCHEMA.md`, 깊은 설계는
`docs/ARCHITECTURE-DETAILED.md`가 authoritative.
</content>
</invoke>
