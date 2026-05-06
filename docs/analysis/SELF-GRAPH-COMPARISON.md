# Self-Graph Comparison (Baseline → Post-Refactor)

> **목적**: P0/P1/T1-A/T1-B/T1-C 적용 전후 자기 분석 결과 비교.
> **기준 명령**: `./bin/ckg build --src=. --out=<dir> --no-cache --lang=go`
> **마지막 갱신**: 2026-05-06

## 헤드라인 변화

| 측정값 | Baseline | Post-Refactor | Δ |
|---|---|---|---|
| Go 파일 수 | 135 | 147 | +12 (신규 패키지) |
| Total nodes | 11,415 | 12,185 | +770 |
| Total edges | 52,527 | 53,856 | +1,329 |
| Empty value violations | 0 | 0 | 유지 ✅ |
| Dangling drops | 7 listens_on | 7 listens_on | 동일 (Task #10 별도 추적) |
| Build time (cold) | sequential | parallel ~4.3s | 177-193% CPU |

## 신규 패키지 emit 검증

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

T1-A의 parallel parser + single-writer channel writer 도입으로 production 코드에 *진짜 동시성 primitive*가 들어가면서 자기 분석 시 G4 edges가 emit됨:

| 항목 | Baseline (V0) | Post-Refactor |
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

## 변경 사항 매핑

| Task | 결과물 | 검증 |
|---|---|---|
| **graph.Validate lenient 모드** | `internal/graph/validate.go`의 `Inspect`/`Sanitize` 함수, `Options.StrictValidate` 플래그, `--strict-validate` CLI | listens_on dangling 7건 drop, 빌드 진행 |
| **T1-A** parallel parser + channel writer | `internal/buildpipe/language_runners.go:parseConcurrent`, `internal/parse/solidity/parser.go` Mutex 추가 | 4.3s 빌드, Mutex/Channel/Goroutine 노드 emit |
| **T1-B** Validator interface | `internal/validate/{validator,schema,llm}.go`, schema_test.go | 4 unit tests pass |
| **T1-C + P0-3** real BM25 | `pkg/bm25/` 4 files (okapi/scorer/tokenize/test) | 8 unit tests pass; 1/(rank+1) → real Okapi |
| **P0-2** Citation Enforcement (warn) | `pkg/smartctx/smartctx.go`의 `metadata.warnings`, citation_for | 응답에 file:line 필수, missing 시 warning record |
| **P0-4** eval ↔ MCP 통일 | `pkg/smartctx/` + `internal/eval/runner.go:smartContext` 위임 | 두 caller 동일 알고리즘 |
| **P0-5** `--files-from` | `internal/filterlist/`, `Options.FilesFromPath` | go=147→4 / ts=14049→0 / sol=5→0 검증 |
| **P1-1** ckg validate | `cmd/ckg/validate.go`, exit 0/1/2 | self-graph 0 errors / 0 warnings |

## Critical gap (변경 없음, 후속 작업)

| 항목 | 상태 | 후속 |
|---|---|---|
| `implements` edges | 여전히 **0** | Go의 implicit interface satisfaction 분석 추가 (P3) |
| listens_on dangling 7건 | lenient drop으로 우회 | Task #10에서 root cause fix 별도 |
| TS/Sol body walk | V0 simplification 그대로 | 사용자 명시적 후순위 (A안) |

## 추가 dogfood 결과

- **`./bin/ckg validate --graph=/tmp/ckg-self-final`** → exit 0, schema validator 0 errors
- **`./bin/ckg validate --llm`** → LLMValidator skeleton에서 단일 Info issue ("llm-not-yet-wired")
- **`./bin/ckg build --files-from=...`** → 정확한 include/exclude 적용

## 회귀 테스트

```
go test -race ./...   →   24 packages PASS (cmd/ckg + 19 internal/* + 4 pkg/*)
```

**End of comparison.** Source of truth: `/tmp/ckg-self-final/graph.db` (재생성 가능).
