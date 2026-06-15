# Go 프로젝트 → 그래프 DB 변환 Flow 검토

> **대상**: `ckg build --src=<go-project> --out=<dir>`
> **흐름**: CLI 진입 → Detection → Pass 1 (Parse) → Pass 2 (Resolve) → Pass 4 (Graph.Build) → Pass 5/6 (Derived) → Pass 7 (Cluster/Score) → Persist
> **참조 파일**: `cmd/ckg/build.go`, `internal/buildpipe/pipeline.go`, `internal/buildpipe/language_runners.go`, `internal/detect/golang.go`, `internal/parse/golang/{parser,resolve,declarations,concurrency,distributed,statements}.go`, `internal/graph/builder.go`, `internal/persist/sqlite.go`
> **선행 문서**: `docs/CODE-STRUCTURE.md` §3~5 (전체 아키텍처) / `docs/ARCHITECTURE-DETAILED.md` §10 (cold-build walk-through)
> **마지막 갱신**: 2026-05-05

---

## 목차

0. [진입 (CLI Entry)](#0-진입-cli-entry)
1. [Detection (P1) — Go-specific 처리](#1-detection-p1--go-specific-처리)
2. [Cache Routing](#2-cache-routing-buildpiperun)
3. [Pass 1 + Pass 2 — runGoPipeline](#3-pass-1--pass-2--rungopipeline)
4. [Pass 4 — Graph Build](#4-pass-4--graph-build)
5. [Derived Passes (P5/P6/P7)](#5-derived-passes-emitderivedpasses)
6. [Persist (Cold Path)](#6-persist-cold-path)
7. [전체 Sequence Diagram](#7-전체-sequence-diagram)
8. [Go 프로젝트 특화 고려사항](#8-go-프로젝트-특화-고려사항)
9. [Go 프로젝트에서 emit되는 Node/Edge 매핑](#9-go-프로젝트에서-emit되는-nodeedge-매핑)
10. [Flow의 핵심 안전장치](#10-flow의-핵심-안전장치)
11. [결과 검증](#11-결과-검증)
12. [핵심 한 줄 요약](#12-핵심-한-줄-요약)

---

## 0. 진입 (CLI Entry)

**`cmd/ckg/build.go`**:

```go
buildpipe.Run(buildpipe.Options{
    SrcRoot, OutDir, Languages, Logger, CKGVersion,
    NoCache, RebuildMetrics, DBDSN,
})
```

`Languages: ["auto"]` (default) → Go/TS/Sol 모두 자동 감지. `--lang=go`로 한정 가능.

---

## 1. Detection (P1) — Go-specific 처리

### 1.1 `detect.GoFiles(srcRoot)` (file 목록만)

```
detect.GoFiles(srcRoot)
   └─► detect.GoPackagesMode(srcRoot, ModeFiles)
         ├─ filepath.Abs(srcRoot)
         ├─ findModuleDirs(absRoot)        // 재귀 walk → go.mod 위치 모두 수집
         │     └─ skip: vendor/, node_modules/, .git/, testdata/
         │
         └─ for each modDir:
              loadModule(modDir, ModeFiles)
                └─ packages.Load(cfg, "./...")
                     ├─ cfg.Mode = NeedName | NeedFiles | NeedModule
                     ├─ cfg.Dir  = modDir
                     └─ cfg.Tests = true     // _test.go 포함 (audit drift 방지)
   ↓
   Project-relative slash-path으로 변환 + dedup + sort
   ↓
   ["cmd/main.go", "internal/foo/bar.go", ...]
```

**핵심 의도**:

- `go list ./...`와 동일 의미론으로 build constraints (`//go:build`) 자동 적용
- `// +build ignore` 파일 자동 제외
- CGO 조건부 alternates 자동 필터링
- 멀티-모듈 프로젝트 시 모든 go.mod 산하 packages.Load 합집합

### 1.2 `detect.GoPackages(srcRoot)` (typed mode, B1+)

`runGoPipeline` 내부에서 한 번 더 호출:

```go
detect.GoPackages(srcRoot)
   └─► GoPackagesMode(srcRoot, ModeTypes)
         └─ Mode = NeedSyntax | NeedTypes | NeedTypesInfo
                 | NeedImports | NeedDeps | NeedCompiledGoFiles
                 (~10× 더 느림)
```

이걸로 `*types.Info` 확보 → 동시성 패스에서 `sync.Mutex` 리시버를 `*types.Object` identity로 정확히 식별. AST-only fallback은 INFERRED confidence로 떨어짐.

---

## 2. Cache Routing (`buildpipe.Run`)

```
discoveryAll() → DiscoveredFile[] 수집 후

old := readOldManifestFromDB(dbPath, dbDSN)

if --no-cache               → runCold
if !ManifestUsable(old)     → runCold     (no manifest, schema/version mismatch)
DiffManifest() classifies:
  IsAllCached() == true     → runShortCircuit (manifest timestamp만 갱신, ~1s)
  mixed dirty/cached        → runIncremental (D4 fallback → 실질 cold)
```

**Go 프로젝트 첫 빌드**: manifest 없음 → **runCold** path

---

## 3. Pass 1 + Pass 2 — `runGoPipeline`

```
runGoPipeline(srcRoot, files, log)
│
├─ p := gop.New(srcRoot)                  // Parser 인스턴스
├─ pkgs, err := detect.GoPackages(srcRoot)
│  ├─ err 발생 시 → log.Warn → AST-only 모드로 fallback
│  └─ 성공: p.SetPackages(pkgs)
│           └─ buildFileIndex(pkgs)
│                └─ map[absFilePath]typedFile{info, file, fset}
│
├─ for each rel in files:                 // Pass 1
│    full := filepath.Join(srcRoot, rel)
│    src  := os.ReadFile(full)
│    r    := p.ParseFile(full, src)       ← per-file 추출 (아래 §3.1)
│    stampFilePath(r)                     // edge.FilePath 채움
│    results = append(results, r)
│
├─ pending := collectPendingRefs(results) // PendingRefRow[] 평탄화 (schema 1.5용)
│
└─ rg, err := p.Resolve(results)          ← Pass 2 cross-file 해석 (아래 §3.2)
   return rg, pending, parseErrCount, err
```

### 3.1 ParseFile (per-file Pass 1)

```
parser.ParseFile(path, src)
│
├─ rel := filepath.Rel(srcRoot, path)
│
├─ tf, ok := p.lookupTyped(path)          // fileIndex 조회
│  │  (path → /private/tmp 같은 macOS 심볼릭 fallback 포함)
│  │
│  ├─ ok == true (typed mode):
│  │    v := newDeclVisitor(tf.fset, rel, tf.file.Name.Name)
│  │    v.typesInfo = tf.info
│  │    v.emitConcurrencyDecls(tf.file)   // (a) Mutex 노드 pre-emit
│  │    ast.Walk(v, tf.file)              // (b) 메인 walk
│  │    v.emitDistributedDecls(tf.file)   // (c) E3 G5 post-emit
│  │
│  └─ ok == false (AST-only fallback):
│       f := parser.ParseFile(p.fset, path, src, parser.ParseComments)
│       v := newDeclVisitor(p.fset, rel, f.Name.Name)
│       v.emitConcurrencyDecls(f); ast.Walk(v, f); v.emitDistributedDecls(f)
│
└─ return ParseResult{Path: rel, Nodes, Edges, Pending}
```

**newDeclVisitor 부트스트랩** (per file):

- Package 노드 1개 (`MakeID(pkgName, "go", 0)`)
- File 노드 1개 (`pkgName + "/" + relPath` qname)
- `Package --contains--> File` 엣지 1개

**(a) `emitConcurrencyDecls` (pre-walk)** — `concurrency.go`:

- `scanStructForMutex` → 구조체 필드의 `sync.Mutex` 발견 → Mutex 노드 emit
- `scanValueSpecForMutex` → 패키지/파일 레벨 var/const의 `sync.Mutex`
- `scanFuncBodyForMutexLocals` → 함수 local mutex literal
- 각 Mutex 노드는 `qname#mutex` suffix로 disambiguate (G9 fix — Field와 ID 충돌 방지)
- 결과: `mutexNodeIDs map[*types.Object]string` 채움

**(b) `ast.Walk` (메인 visit)** — `declarations.go`:

| AST Node | 처리 | 추가 emit |
|---|---|---|
| `*ast.GenDecl` | `visitGenDecl` |  |
| ↳ `IMPORT` | `emitImportSpec` | Import 노드 + `imports` 엣지 |
| ↳ `TYPE` | `emitTypeSpec` | Struct/Interface/TypeAlias/Enum 노드 |
| ↳ `VAR/CONST` | `emitValueSpec` | Variable/Constant 노드 |
| Struct fields | `emitFields` | Field 노드 + `defines` 엣지 |
| Interface methods | `emitInterfaceMethod` | Method 노드 (signature only) |
| `*ast.FuncDecl` | `visitFuncDecl` | Function/Method + Parameter + body walk |
| Function body | `emitFunctionBodyPos` | IfStmt/LoopStmt/SwitchStmt/CallSite/ReturnStmt + pending refs |
| `Lock/Unlock/RLock/RUnlock` | `maybeEmitLockEdge` | acquires_lock / releases_lock 엣지 |
| `make(chan T)` | `emitChannelFromMake` | Channel 노드 + chanVarIDs 매핑 |
| `go func() { … }()` | (concurrency) | Goroutine 노드 + spawns 엣지 + body 별도 walk |
| `ch <- x`, `<-ch` | (concurrency) | sends_to / recvs_from → Channel 노드 직접 |

**Pending refs**: cross-file 호출(예: 다른 패키지의 함수)은 ParseFile 시점엔 해석 불가 → `parse.PendingRef{SrcID, TargetQName, EdgeType, Line, HintFile}` 큐잉. Pass 2에서 일괄 해소.

**(c) `emitDistributedDecls` (post-walk)** — `distributed.go`:

- `http.HandleFunc("/foo", handler)` → Endpoint 노드 + `listens_on` pending
- `(*ServeMux).Handle` 동등 처리
- net/rpc handler 시그니처 매칭 `func (T) M(args A, reply *R) error` → MessageType + `handles_message`
- `client.Call("Service.Method", args, &reply)` → MessageType + `rpc_calls`

### 3.2 Resolve (per-language Pass 2)

```go
func (p *Parser) Resolve(results []*ParseResult) (*ResolvedGraph, error)
```

```
1. qIndex: map[qname]nodeID 구축 (Function/Method만)
   - 정확 qname (예: "mypkg.Helper")
   - simpleName(suffix) (예: "Helper")
2. callSiteParent: CallSite ID → 둘러싼 Function ID
   - CallSite qname 형식: "<parentQname>#<Kind>@<offset>"
3. for each ParseResult.Pending:
     id := qIndex[pr.TargetQName]
     if !ok:
        suffix match: q where strings.HasSuffix(q, "."+target) || q == target
     if !ok:
        DROP (V0 simplification — FK 위반 회피)
     src := callSiteParent[pr.SrcID] || pr.SrcID
     emit Edge{Src, Dst, Type, Line, Confidence: EXTRACTED}
     // 단 EdgeListensOn은 src↔dst swap (handler→endpoint 방향)
```

결과: `ResolvedGraph{Nodes, Edges}` (Go-only).

---

## 4. Pass 4 — Graph Build

```go
g, err := graph.Build(resolved)  // resolved = []*ResolvedGraph{Go} (TS/Sol 비활성)
```

```
graph.Build([Go ResolvedGraph]):
  byID := map[string]Node              // last-writer-wins (정상시 동일 ID는 동일 attr)
  seenEdge := map[edgeKey]bool         // (Type, Src, Dst, Line) keep-first
  for each part.Nodes: byID[n.ID] = n
  for each part.Edges:
     k := edgeKey{e.Type, e.Src, e.Dst, e.Line}
     if !seenEdge[k]: append + seenEdge[k] = true
  sort nodes by ID (deterministic)
  return Graph{Nodes, Edges}
```

`graph.Validate(g)` — orphan dst, cyclic contains 등 검증.

---

## 5. Derived Passes (`emitDerivedPasses`)

```
emitDerivedPasses(g, srcRoot, solParser=nil, log):
   ├─ P5 G5 xlang link:
   │    if solParser != nil: link.SolToTS(g.Nodes, abi) → binds_to edges
   │    └─ Go-only project → SKIP (solParser is nil)
   │
   ├─ P6 G6 Temporal:
   │    emitTemporalEdges(g, srcRoot, log, depth=0)
   │    └─ git log --raw --no-renames (한번 호출, 파일별 cap=10 commits)
   │       → Commit 노드 + changed_in (file 닿은 모든 symbol → commit) + blame (File → 최신 commit)
   │       └─ git checkout 아니면 graceful skip
   │
   ├─ P7a Cluster:
   │    pkgTree   := cluster.BuildPkgTree(g)         (deterministic, dir hierarchy)
   │    topicTree := cluster.BuildTopicTree(g, [0.5, 1.0, 2.0], seed=42)
   │
   └─ P7b Score:
        score.Compute(g)   // PageRank + usage_score (in_degree)
```

Go 프로젝트에선 P5(xlang)는 항상 skip, P6는 git checkout일 때만 실행.

---

## 6. Persist (Cold Path)

```
openColdStore(outDir, dbDSN)
   ├─ DSN 있으면: persist.OpenPostgresCold(dsn) — TRUNCATE
   └─ 아니면:
        os.Remove(outDir/graph.db)        // 기존 DB 완전 wipe
        store := persist.Open(dbPath)
        store.Migrate()                    // schema 1.5 DDL 적용

persistColdArtifacts(store, srcRoot, g, pkgTree, topicTree):
   1. store.InsertNodes(g.Nodes)          // PK conflict는 nodes에 없음 (Build dedup)
   2. store.InsertEdges(g.Edges)          // AUTOINCREMENT id, FK CASCADE
   3. store.InsertPkgTreeFromCluster(pkgTree.PersistEdges())
   4. store.InsertTopicTree(topicTree)
   5. store.InsertBlobs(extractBlobs(srcRoot, g.Nodes))
        └─ 노드별 source slice [StartByte..EndByte] 추출 (Package 노드 제외)
        └─ 파일별 cache로 IO 최소화
   6. store.RebuildFTS()                  // nodes_fts 인덱스 재구축

store.InsertPendingRefs(allPending)       // schema 1.5 — 다음 partial 빌드용

manifest skeleton 생성:
   m.Files = computeColdFileEntries(srcRoot, ckgVersion, discovery, nodes, edges)
        ├─ 각 파일 SHA256 계산
        ├─ cache_key = sha256(content + "|ckg:" + V + "|parser:" + V + "|schema:1.5")
        ├─ mtime 기록
        └─ NodeIDs[] / EdgeIDs[] 매핑

setStaleness(&m, log)                      // DB timestamp vs source mtime
store.SetManifest(m)                       // manifest 테이블에 직렬화
writeManifestJSON(outDir/manifest.json, m) // 사람이 읽을 사본

log.Info("build complete", nodes, edges, pkg_tree_edges, topic_resolutions)
return m
```

---

## 7. 전체 Sequence Diagram

```
User                cmd/ckg/build.go     buildpipe         detect           parse/golang        graph         persist
  │                     │                    │                │                  │                 │              │
  │ ckg build --src=X   │                    │                │                  │                 │              │
  │────────────────────►│                    │                │                  │                 │              │
  │                     │ buildpipe.Run(opts)│                │                  │                 │              │
  │                     │───────────────────►│                │                  │                 │              │
  │                     │                    │ discoveryAll() │                  │                 │              │
  │                     │                    │───────────────►│ Walk + GoFiles   │                 │              │
  │                     │                    │                │ packages.Load(./...) ModeFiles      │              │
  │                     │                    │◄───────────────│ DiscoveredFile[] │                 │              │
  │                     │                    │                                                                     │
  │                     │                    │ ManifestUsable + DiffManifest                                       │
  │                     │                    │ ─ no manifest → runCold                                              │
  │                     │                    │                                                                     │
  │                     │                    │ runCold:                                                            │
  │                     │                    │ ├ detect.Walk + detect.GoFiles  (TS/Sol/Go file lists)              │
  │                     │                    │ │                                                                   │
  │                     │                    │ ├ runGoPipeline(srcRoot, goFiles, log):                             │
  │                     │                    │ │   detect.GoPackages (ModeTypes — 10x slower, NeedTypes/Syntax)    │
  │                     │                    │ │     parser.SetPackages(pkgs)  // typed file index                 │
  │                     │                    │ │   for each file:                                                  │
  │                     │                    │ │     os.ReadFile + parser.ParseFile                                │
  │                     │                    │ │       ├ lookupTyped → typed mode (EXTRACTED concurrency)          │
  │                     │                    │ │       │   newDeclVisitor + emitConcurrencyDecls (pre)             │
  │                     │                    │ │       │   ast.Walk → declarations + statements + lockEdges        │
  │                     │                    │ │       │   emitDistributedDecls (post — HTTP/RPC)                  │
  │                     │                    │ │       └ AST-only fallback (INFERRED concurrency)                  │
  │                     │                    │ │     stampFilePath(r) — edge.FilePath 채움                          │
  │                     │                    │ │   collectPendingRefs(results)                                      │
  │                     │                    │ │   parser.Resolve(results)                                          │
  │                     │                    │ │     qIndex(qname → nodeID) + callSiteParent                        │
  │                     │                    │ │     for each pending: 정확/suffix 매치 → edge emit (or drop)      │
  │                     │                    │ │   → ResolvedGraph(Go)                                              │
  │                     │                    │ │                                                                    │
  │                     │                    │ ├ (TS/Sol — Go-only이면 skip)                                        │
  │                     │                    │ │                                                                    │
  │                     │                    │ ├ graph.Build([rg_go])                                              │
  │                     │                    │ │   dedup nodes by ID, edges by 4-tuple                             │
  │                     │                    │ │ → Graph{Nodes, Edges}                                             │
  │                     │                    │ │ graph.Validate                                                     │
  │                     │                    │ │                                                                    │
  │                     │                    │ ├ emitDerivedPasses(g, srcRoot, nil, log):                          │
  │                     │                    │ │   xlang skip (no solParser)                                       │
  │                     │                    │ │   emitTemporalEdges (git log → Commit + changed_in/blame)         │
  │                     │                    │ │   cluster.BuildPkgTree + BuildTopicTree (Leiden γ∈{0.5,1,2})      │
  │                     │                    │ │   score.Compute (PageRank + usage)                                │
  │                     │                    │ │                                                                    │
  │                     │                    │ ├ openColdStore: os.Remove(graph.db) + Open + Migrate               │
  │                     │                    │ ├ persistColdArtifacts:                                             │
  │                     │                    │ │   InsertNodes/Edges/PkgTree/TopicTree/Blobs + RebuildFTS          │
  │                     │                    │ ├ InsertPendingRefs (schema 1.5)                                    │
  │                     │                    │ ├ computeColdFileEntries (SHA256 + cache_key per file)              │
  │                     │                    │ ├ setStaleness + SetManifest + writeManifestJSON                    │
  │                     │                    │ │                                                                    │
  │                     │                    │ │ log.Info("build complete", nodes, edges, …)                       │
  │                     │                    │◄┘                                                                    │
  │                     │◄───────────────────│ Manifest                                                             │
  │ "ckg: built N nodes /│                                                                                          │
  │  M edges into X"     │                                                                                          │
  │◄────────────────────│                                                                                           │
```

---

## 8. Go 프로젝트 특화 고려사항

| 항목 | 처리 | 영향 |
|---|---|---|
| **Build constraints** (`//go:build linux`) | `go/packages`가 자동 적용 | 호스트 OS/Arch와 무관한 파일은 자연스럽게 제외 |
| **Build ignore** (`//go:build ignore`) | 자동 제외 | 도구 스크립트 등 |
| **`_test.go`** | `Tests:true` | 인덱싱됨 → audit drift 방지 |
| **Multi-module** (여러 go.mod) | 모듈마다 packages.Load 후 union | 멀티-모듈 monorepo 지원 |
| **`go.work`** (workspace) | 미지원 (E2-FU) | workspace 멤버가 srcRoot 외부일 시 누락 |
| **`vendor/`** | findModuleDirs 단계에서 skip | go.mod 이중 파싱 방지 |
| **`testdata/`** | findModuleDirs 단계에서 skip | go list ./... 의미론과 일치 |
| **CGO** | `CompiledGoFiles` 우선 | preprocessing 후 파일 사용 |
| **macOS symlinks** | `/private/tmp` ↔ `/tmp` lookup fallback | `lookupTyped`에서 EvalSymlinks |
| **타입 로드 실패** | log.Warn → AST-only mode | 동시성 EXTRACTED → INFERRED로 degrade |
| **Cross-file calls** | Pass 2 qIndex (정확 + suffix match) | 미해소시 V0는 drop (FK 위반 방지) |

---

## 9. Go 프로젝트에서 emit되는 Node/Edge 매핑

| AST 요소 | Node 출력 | Edge 출력 |
|---|---|---|
| package 선언 | Package | — |
| 파일 자체 | File | Package -contains-> File |
| `import "x"` | Import | File -imports-> Import (source) / -references-> 대상 |
| `type T struct{}` | Struct + Field × N | File -defines-> Struct, Struct -contains-> Field |
| `type T interface{}` | Interface + Method × N | File -defines-> Interface |
| `type T = U` | TypeAlias | -uses_type-> U |
| `var/const x` | Variable/Constant | File -defines-> |
| `func F()` | Function + Parameter × N | File -defines-> Function |
| `func (r T) M()` | Method | T -contains-> Method |
| if/for/switch | IfStmt / LoopStmt / SwitchStmt | enclosing Function -contains-> |
| 함수 호출 | CallSite | enclosing -calls-> target (Pass 2 resolve) |
| `return x` | ReturnStmt | enclosing -contains-> |
| `sync.Mutex` 필드/var | Mutex (qname#mutex suffix) | owner -contains-> |
| `mu.Lock()` | (no node) | enclosing Func -acquires_lock-> Mutex |
| `mu.Unlock()` | (no node) | enclosing Func -releases_lock-> Mutex |
| Lock~Unlock 사이 field 접근 | (no node) | enclosing Func -accessed_under_lock-> Field |
| `make(chan T)` | Channel | (chanVarIDs 매핑) |
| `ch <- x` / `<-ch` | (no node) | enclosing -sends_to/recvs_from-> Channel |
| `go func(){...}()` | Goroutine | enclosing -spawns-> Goroutine |
| `http.HandleFunc(...)` | Endpoint | handler -listens_on-> Endpoint (Pass 2 swap) |
| net/rpc handler 시그니처 | MessageType | handler -handles_message-> MessageType |
| `client.Call("S.M", ...)` | MessageType (placeholder) | enclosing -rpc_calls-> MessageType |
| (P6 git history) | Commit | symbol -changed_in-> Commit, File -blame-> Commit |

---

## 10. Flow의 핵심 안전장치

1. **Detection oracle 통일**: `detect.GoFiles`(file 목록) ↔ `detect.GoPackages`(typed) 모두 `GoPackagesMode`로 단일 oracle. audit 및 production이 동일 set 보장.
2. **AST-only fallback**: 타입 로드 실패 시 graceful degrade — 빌드 진행, 동시성 confidence만 낮아짐.
3. **Pending refs drop policy**: V0는 미해소시 silent drop (FK 위반 회피). 향후 nullable dst로 AMBIGUOUS 보존 예정.
4. **Edge dedup**: `(Type, Src, Dst, Line)` 4-tuple keep-first → partial-cache 시 cold/partial 동등 보장.
5. **Cold = wipe & rewrite**: stale row 누적 차단. CASCADE FK로 노드 삭제 시 edge/blob/tree 동시 정리.
6. **Schema bump = global cache invalidation**: silent corruption 방어.
7. **Audit으로 closing the loop**: `ckg audit --src --graph` → `go/packages.Load` set vs DB set 비교 (exit 0 = parity).

---

## 11. 결과 검증

```bash
$ ckg build --src=/path/to/go-project --out=/tmp/ckg
ckg: built 217513 nodes / 669421 edges into /tmp/ckg

$ ls /tmp/ckg
  graph.db        ← SQLite DB (schema 1.5)
  manifest.json   ← cache fingerprint + Files[] + statistics

$ ckg audit --src=/path/to/go-project --graph=/tmp/ckg
  exit 0  (PARITY — go/packages.Load 와 DB 파일 set 일치)

$ ckg serve --graph=/tmp/ckg --port=8080 --open
  → 브라우저에서 3D viewer 확인

$ ckg mcp --graph=/tmp/ckg
  → Claude Code 에서 6 tools 사용 가능
```

---

## 12. 핵심 한 줄 요약

> **`ckg build` → `go/packages.Load`로 build oracle 확립 → 타입 정보 포함 per-file Pass 1 (declarations + concurrency + distributed) → cross-file Pass 2 qname resolve → `graph.Build` dedup → temporal/cluster/score derived passes → SQLite wipe + bulk insert + manifest commit**

---

## Appendix: 측정 (실측)

go-stablenet-latest (Go 1259 + TS 320 + Sol 563 = 2142 files) 기준:

| Phase | Metric | Time |
|---|---|---|
| Detect (Go) | go/packages.Load + walk | ~5s |
| Parse (Go) | 1259 files | ~80s (types.Info traversal) |
| Resolve (Go) | Cross-file linking | ~1s |
| Graph Build | Merge + dedup | <1s |
| Temporal | git log + edge emit | ~5s |
| Cluster/Score | PageRank + Leiden | ~2s |
| Persist | Bulk insert + FTS index | ~1s |
| **Total Cold** | | ~115s |
| **Short-Circuit** | (manifest refresh only) | <1s |

audit 결과: 1259 build set vs 1259 DB set → **PARITY (exit 0)**

---

**End of Go-project build flow analysis.** 단계별 deep-dive는 본 문서, 전체 아키텍처는 `../CODE-STRUCTURE.md` / `../ARCHITECTURE-DETAILED.md` 참조.
