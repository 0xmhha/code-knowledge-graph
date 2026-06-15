# Self-Graph Comparison (Baseline → Post-Refactor → Post-listens-on-Fix)

> **목적**: P0/P1/T1-A/T1-B/T1-C + listens_on root-cause fix 적용 전후의 자기 분석 결과 비교.
> **기준 명령**: `./bin/ckg build --src=. --out=<dir> --no-cache --lang=go [--strict-validate]`
> **마지막 갱신**: 2026-05-06 (commit 5eb4062 직후)

## 헤드라인 변화

| 측정값 | Baseline | Post-Refactor | Post-Fix |
|---|---|---|---|
| Go 파일 수 | 135 | 147 | 147 |
| Total nodes | 11,415 | 12,185 | 12,219 |
| Total edges | 52,527 | 53,856 | **57,116** |
| **`listens_on` edges** | 0 (drop됨) | 0 (drop됨) | **7** ✅ |
| Dangling drops (lenient) | 7 listens_on | 7 listens_on | **0** ✅ |
| `--strict-validate` self-build | 첫 dangling에서 abort | 첫 dangling에서 abort | **통과** ✅ |
| Empty value violations | 0 | 0 | 0 (유지) |
| Build time (cold) | sequential | parallel ~4.3s | parallel ~4.9s |

> Post-Refactor → Post-Fix의 edge 수 +3,260은 (a) 새 commit의 `changed_in` (+413) + (b) listens_on 7건 정상 emit + (c) 추가 회귀 테스트의 신규 nodes/edges 합산 결과. listens_on 7건이 lenient drop에서 빠져나와 실제 graph에 들어온 것이 가장 큰 정성적 변화.

## listens_on dangling fix (Task #10)

| 측정값 | Pre-Fix | Post-Fix |
|---|---|---|
| dangling drops (lenient build) | 7 listens_on | 0 |
| `listens_on` edges in graph | 0 | 7 |
| 모든 src가 유효한 Method 노드 | N/A (drop됨) | 7/7 ✅ |
| `--strict-validate` 자기 분석 | abort | 통과 |

**Root cause**: `idForFunc`가 `*types.Func.Pos()`(메서드 *이름* 위치)로 ID를 재계산했으나, `visitFuncDecl`은 `*ast.FuncDecl.Pos()`(`func` 키워드 위치)를 사용. 메서드의 receiver 절 폭(~17 byte) 때문에 두 ID가 일치하지 않아 같은 파일 메서드 핸들러 등록 시 dangling.

**Fix**: `MakeID` 재계산 대신 `v.nodes`에 qname 일치 lookup. ast.Walk이 emitDistributedDecls 전에 실행되므로 노드는 이미 emit되어 있음. Cross-file 핸들러는 기존 PendingRef → Pass 2 qIndex 경로 유지.

**자기 분석 결과 (실제 emit)**:

```
server.Server.handleManifest    →  http:GET /api/manifest
server.Server.handleHierarchy   →  http:GET /api/hierarchy
server.Server.handleNodes       →  http:GET /api/nodes
server.Server.handleNodesByIDs  →  http:POST /api/nodes-by-ids
server.Server.handleEdges       →  http:POST /api/edges
server.Server.handleBlob        →  http:GET /api/blob/{id}
server.Server.handleSearch      →  http:GET /api/search
```

→ `find_callers`/`impact_of_change` 같은 MCP 도구가 이제 이 정보를 활용 가능.

## 신규 패키지 emit 검증 (Post-Refactor에서 추가, 그대로 유지)

| 경로 | 노드 emit | 비고 |
|---|---|---|
| `pkg/bm25/okapi.go` | ✅ | Okapi BM25 구현 |
| `pkg/bm25/scorer.go` | ✅ | Scorer interface |
| `pkg/bm25/tokenize.go` | ✅ | 코드 식별자 토크나이저 |
| `pkg/bm25/scorer_test.go` | ✅ | |
| `pkg/smartctx/smartctx.go` | ✅ | eval ↔ MCP 통일 진입점 |
| `internal/validate/validator.go` | ✅ | Validator interface |
| `internal/validate/schema.go` | ✅ | SchemaValidator |
| `internal/validate/llm.go` | ✅ | LLMValidator skeleton |
| `internal/validate/schema_test.go` | ✅ | |
| `internal/filterlist/filterlist.go` | ✅ | --files-from JSON 파서 + glob |
| `internal/filterlist/filterlist_test.go` | ✅ | |

## G4 (Concurrency) — production code emit

T1-A의 parallel parser + single-writer channel writer 도입으로 production 코드에 *진짜 동시성 primitive*가 들어가면서 자기 분석 시 G4 edges가 emit됨. listens_on fix는 G4에 영향 없음 (그대로 유지):

| 항목 | Baseline (V0) | Post-Refactor / Post-Fix |
|---|---|---|
| `Mutex` 노드 | 0 | **2** (`parseConcurrent.errMu`, `Parser.abiMu`) |
| `Channel` 노드 | 0 | **3** (resultCh, sem, collected) |
| `Goroutine` 노드 | 0 | **5** (worker, closer, collector, ListenAndServe 외) |
| `spawns` edge | 0 | **5** |
| `sends_to` | 0 | **3** |
| `recvs_from` | 0 | **4** |
| `acquires_lock` | 0 | **2** |
| `releases_lock` | 0 | **2** |
| `accessed_under_lock` | 0 | **3** |

→ **CKG가 자기 코드의 동시성 패턴을 정확히 검출**. detector 자체의 정확도를 dogfood로 검증한 결과.

## 변경 사항 매핑 (commit별)

| Commit | 작업 | 결과물 | 검증 |
|---|---|---|---|
| `b4d76b8` | graph: Inspect/Sanitize lenient API | `internal/graph/validate.go` | ValidationReport, dangling 수집 |
| `0f1d258` | persist: AllNodes/AllEdges | `store_interface.go`, `sqlite.go`, `postgres_store.go` | 두 backend + mock |
| `acffad2` | T1-A parallel parser + channel writer | `language_runners.go:parseConcurrent`, Sol abiMu | -race 통과, Mutex/Channel/Goroutine emit |
| `6487e10` | T1-C: pkg/bm25 Okapi BM25 | `pkg/bm25/` 4 files | 8 unit tests, bleve+rank_bm25 cross-check |
| `46216e8` | P0-3+P0-4+P0-2: smartctx 통일 + real BM25 + Citation | `pkg/smartctx/`, mcp/eval rewrite | metadata.warnings, file:line on every body/summary |
| `169301f` | T1-B: Validator interface + Schema/LLM | `internal/validate/` 4 files | 4 unit tests |
| `431ff20` | P0-5 part 1: filterlist package | `internal/filterlist/` | 6 unit tests, doublestar matcher |
| `1a4f9db` | buildpipe 통합: lenient + --files-from + --strict-validate | `pipeline.go`, `incremental.go`, `build.go` | 두 모드 검증, 147→4 filter 검증 |
| `0a7859f` | P1-1: ckg validate subcommand | `cmd/ckg/validate.go` + root/main 통합 | exit 0/1/2, --llm 스켈레톤 |
| `17a9d76` | docs: baseline + comparison | `docs/analysis/SELF-GRAPH-*.md` | |
| `5eb4062` | listens_on dangling fix | `parse/golang/distributed.go` qname lookup, fixture, 회귀 테스트 | 7→0 drops, strict-validate 통과 |

## Critical gap (Post-Fix 시점)

| 항목 | 상태 | 후속 |
|---|---|---|
| `implements` edges | 여전히 **0** (Interface 노드 10개 검출되었으나 미연결) | Go의 implicit interface satisfaction 분석 추가 (P3) |
| TS/Sol body walk | V0 simplification 그대로 | 사용자 명시적 후순위 (A안) |
| ~~listens_on dangling 7건~~ | ✅ **해결** (commit 5eb4062) | — |

## 추가 dogfood 결과

- **`./bin/ckg validate --graph=/tmp/ckg-self-final`** → exit 0, schema validator 0 errors / 0 warnings / 0 info
- **`./bin/ckg validate --llm`** → LLMValidator skeleton에서 단일 Info issue ("llm-not-yet-wired")
- **`./bin/ckg build --files-from=...`** → go=147→4 / ts=14049→0 / sol=5→0 정확 적용
- **`./bin/ckg build --strict-validate`** → ✅ 통과 (이전엔 첫 dangling에서 abort)

## 회귀 테스트

```
go test -race ./...   →   22 packages PASS (cmd/ckg + 19 internal/* + 3 pkg/*)
```

새 회귀 테스트:
- `TestDistributed_HTTP_MethodHandler_NoDangling` — listens_on dangling 재발 방지 (testdata/distributed/method_handlers.go fixture)

**End of comparison.** Source of truth: `/tmp/ckg-self-final/graph.db` 재생성 가능 — `./bin/ckg build --src=. --out=/tmp/ckg-self-final --no-cache --lang=go --strict-validate`.
