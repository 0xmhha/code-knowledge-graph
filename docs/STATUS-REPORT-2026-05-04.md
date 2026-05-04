# CKG Status Report — 2026-05-04

> 사용자 검토용. 현재 상태 + 성공 조건 Gap 분석 + 우선순위 정리.

---

## 1. 현재 상태 요약

**V0 완료** (38/38 commits): 단일 Go 바이너리 `ckg` 5 subcommand (build/serve/mcp/export-static/eval), 33 node types × 30 edge types, 3 언어 파서 (Go/TS/Solidity), 6 graph axis (G1~G6), MCP stdio server 6 tools, Next.js 3D viewer with filter UI.

**Wave 7 (Group F) 완료** (2026-05-03): CKG_DEV_VIEWER_DIR env hot-reload + --no-viewer API-only mode + production-split 문서화.

**G6 v3 D4 탈출 실행 (2026-05-04)**: 3회 incremental-cache 시도 모두 실패 (v1: 92201 edges -100%, v2: 347986 edges -52%, v3: +2675 phantom edges). Root cause 확인: H3 (NodesByFilePath rowid-sorted ≠ AST declaration order). 현재 routing cold-fallback 복귀, schema 1.5 dead infrastructure 보존 (v4 대비).

**사용자 4 완성도 조건**: 모두 충족 (#1-#4 ✅). audit 검증 가능, 6 graph 지원, viewer + CLI eval 가능.

---

## 2. 성공 조건 Gap 분석

| # | 성공 조건 | 현재 상태 | Gap | 우선순위 |
|---|---|---|---|---|
| 1 | Code path → knowledge graph | ✅ IMPLEMENTED | — | — |
| 2 | 6 graph structures (G1~G6) | ✅ IMPLEMENTED | — | — |
| 3 | tree-sitter보다 향상된 이해도 + 속도 | 🟡 PARTIAL | **(1)** call flow: direct만 지원, interface dispatch / IPA 미지원. **(2)** data flow: field-level, whole-program 미지원. **(3)** channel async data flow: `sends_to`/`recvs_from`이 Channel 노드 직접 가리킴 (make(chan T) 변수 추적, 2026-05-04 ✅). channel 파라미터는 CallSite fallback (known limitation). | P1 |
| 4a | **Call flow query** (find_callers/callees/get_subgraph) | 🟡 PARTIAL | Direct calls only (calls/invokes edges). Indirect (interface dispatch/callback/reflection) 미지원. | P1 |
| 4b | **Data flow query** (reads_field/writes_field, reads_mapping/writes_mapping) | 🟡 PARTIAL | Per-function scope (cross-file, across module 경계 미지원). **Channel 경유 데이터 흐름 별도 gap** (4d 참조). | P2 |
| 4c | **History query** (changed_in/blame edges) | 🟡 PARTIAL | File-level 구현 (line-level, submodule 미지원). 344946 edges 검증 ✅. | P3 |
| 4d | **Concurrency flow query** (acquires_lock/releases_lock/accessed_under_lock + channel) | 🟡 PARTIAL | **(a)** Lock: intra-function heuristic only (cross-fn SSA/D1 deferred). go-stablenet: 781 acquires_lock, 834 releases_lock, 2916 accessed_under_lock. **(b)** Channel flow: `sends_to`/`recvs_from`가 Channel 노드 직접 가리킴 (2026-05-04 ✅). 채널 파라미터 변수는 CallSite fallback. producer→consumer 연결은 Channel 노드 경유로 그래프 traversal 가능. | P1 |
| 4e | **Version gap detection** (ckg audit) | 🟡 PARTIAL | File-level parity (semantic drift 미지원). go-stablenet 1259/1259 PARITY ✅. | P2 |
| 5 | **Logging + debug mode** | ✅ IMPLEMENTED | `--verbose` (slog.LevelDebug), `--log-file <path>` (JSON 파일 + text stderr tee), `CKG_LOG_LEVEL=debug` env. 6 subcommand 적용. buildpipe 스테이지 Debug 마커 추가. (2026-05-04, 4fc69ff) | — |

---

## 3. 현재 이슈 목록 (우선순위 순)

### P0 — Critical / 기능 완성도 직접 영향

| ID | 이슈 | 영향 | 관련 작업 | 액션 |
|---|---|---|---|---|
| — | 없음 | — | — | G6 v3 D4 close로 critical 제거됨 |

### P1 — Important / 운영성 + 쿼리 정확도

| ID | 이슈 | 영향 | 관련 작업 | 액션 |
|---|---|---|---|---|
| G6-partial-cache | Incremental cache 3회 시도 실패 (root cause: H3) | 현재 cold-fallback → 대규모 재빌드 성능 저하 | G6 v4 (spec-redesign 필요) | B3 or C1 prereq 후 v4 attempt |
| G4-concurrency-cross-func | acquires_lock/releases_lock intra-fn only | SSA 필요 | D1 (Stage 2: SSA) | B1 기초 위에 D1 spec 작성 후 dispatch |
| B1-1 (Field-targeting) | 1건 남은 acquires_lock → Field node (local mutex literal) | Minor but semantic | B1 follow-up | 향후 SSA 경로에서 자연스럽게 해결 |

### P2 — Medium / 기술 부채 + 완전성

| ID | 이슈 | 영향 | 관련 작업 | 액션 |
|---|---|---|---|---|
| E2-FU (go.work) | workspace 멤버 srcRoot 외부 case 미지원 | Edge case 누락 | E2 follow-up | TestGoFiles_GoWorkspace documented; future extension |
| G3-1 (E3 follow-up) | go-stablenet은 julienschmidt/httprouter 사용 → stdlib pattern detector 적게 fire | listens_on 검출 부족 | E3 follow-up (custom router 패턴) | Router abstraction 추가 디자인 후 확장 |
| G4-1 (E3 follow-up) | Ethereum RPC `client.Call(&result, "method", ...)` 미지원 | rpc_calls 미검출 | E3 follow-up (RPC 시그니처) | net/rpc + variant 패턴 mapper 추가 |
| Wave1-DoD (viewer dead-key) | edges.ts 의 reads/writes/modifies/decorates/emits 정의는 있으나 backend emit 없음 | 뷰어 edge group 표시 불확실 | E5 follow-up 또는 제거 | clarify: backend emit 시작 vs client 제거 결정 |

### P3 — Low / 장기 계획

| ID | 이슈 | 영향 | 관련 작업 | 액션 |
|---|---|---|---|---|
| G6-temporal (E4 follow-up) | Line-level blame (현재 file-level) | Deep history analysis 불가 | G6 Phase 2 (git blame --line-porcelain) | 별도 선택적 feature |
| G6-temporal-submodule | git submodule 경계 미지원 | History 불완전 | G6 Phase 2 follow-up | rare use case |
| D1/D2 | SSA + pgvector | v0.3.0 로드맵 | D1 (XL) + D2 (XL) | spec 작성 후 2026 후반 |

---

## 4. 작업 그룹 잔여 항목

| Group | ID | 작업 | 추정 | 의존 | 상태 | 다음 액션 |
|---|---|---|---|---|---|---|
| B | B2 | `ckg export-postgres --dsn ... --source ...` | M | A4 ✅ | ✅ **완료 13317f7** | — |
| B | Logging | `--verbose` / `--log-file` / `CKG_LOG_LEVEL` | S | — | ✅ **완료 4fc69ff** | — |
| B | Channel | `sends_to`/`recvs_from` → Channel 노드 직접 연결 | M | B1✅ | ✅ **완료 eb5e9bb** | — |
| B | B3 | Tree.Edit() incremental parsing 인프라 | M | A1✅+A3✅ | READY | G6 v4 후 또는 병렬 |
| G6 v4 | — | `NodesByFilePath ORDER BY start_line ASC` + routing 복귀 | S | — | **READY** | **우선순위 #1** — ORDER BY 한 줄, real-corpus 검증 |
| C | C1 | reverse-reference invalidation | L | A3✅ | READY | G6 v4 후, partial-cache 성능 prereq |
| C | C2 | direct PG build (ckg build --db postgres://) | L | B2✅ | READY | B2 ✅ 완료로 진입 가능 |
| D | D1 | SSA concurrency (--deep opt-in) | XL | B1✅ | SPEC NEEDED | D1 spec 작성 필요 |
| D | D2 | pgvector + Apache AGE | XL | C2 | SPEC NEEDED | D1 후 |
| minor | E2-FU | go.work workspace outside srcRoot | S | — | DOCUMENTED | 선택적 follow-up |
| minor | E3 follow-ups | httprouter + Ethereum RPC patterns | S+M | E3✅ | READY | Minor dispatch |
| minor | Wave1-DoD | viewer dead-key clarification | S | E5✅ | READY | 설계 결정 후 1줄 fix 또는 제거 |

---

## 5. 권장 다음 작업 순서 (의존도 + 우선순위 기반)

### Tier 1: 즉시 진입 가능

**G6 v4: `NodesByFilePath ORDER BY start_line ASC`** (S effort, ~30분)
- **이유**: Root cause H3 fix는 단 한 줄 SQL. B3/C1은 성능 prereq이지 정확도 prereq 아님.
- **실행**: `sqlite.go:796` ORDER BY 추가 → `runIncremental` 라우팅 복귀 → real-corpus diff 검증.
- **성공 기준**: cold vs partial edge count diff = 0 (또는 H4의 -5 허용)
- **실패 시**: D4 재실행 (routing cold-fallback 복귀) — 이미 검증된 롤백 경로

### Tier 2: G6 v4 이후 (GROUP C — v0.2.2)

**C1: reverse-reference invalidation** (L effort, 반나절+)
- **이유**: G6 v4 정확도 확인 후 partial-cache 성능 개선. pending_refs(schema 1.5) source 활용.
- **의존**: G6 v4 ✅ 먼저

**C2: direct PG build** (L, B2✅ 완료로 즉시 진입 가능)
- **이유**: PostgreSQL 네이티브 그래프 빌드. B2 경험 위에서.

### Tier 3: 선택적 Minor cleanups (병렬 가능)

**E3/E4 follow-ups** (S+M, router patterns + RPC variants)

**Wave1-DoD + E2-FU** (S+S, viewer dead-key + go.work edge case)

---

## 6. 성능 현황 (go-stablenet 2142 files / 217K nodes / 669K edges)

| 메트릭 | 현재 값 | 목표 | Gap | 상태 |
|---|---|---|---|---|
| Cold build (2142 files) | ~75s | — | — | ✅ Acceptable (A3 cache warmup 후 대부분 생략) |
| Warm rebuild (all cached, short-circuit) | ~1s | — | — | ✅ |
| Partial rebuild (1 file dirty) | ~75s (cold fallback) | <3s | Fallback due to G6 v3 fail | ⛔ Requires B3 or C1 + v4 redesign |
| File-level audit parity | 1259/1259 ✅ | PARITY | — | ✅ Complete |
| Nodes extracted | 217K | — | — | ✅ Inclusive (go/packages + TS all-files + Sol grammar) |
| Edge density | 669K edges | — | — | ✅ Comprehensive (6 graph axis, 30 types) |

**주요 메트릭 해석**:
- Cold ~75s는 Go 1259 + TS 320 + Sol 563 파일 parse + cluster + temporal (git log) 포함. Acceptable for daily use.
- Warm 1s는 manifest-only (content hash match → all cached).
- **Partial fallback는 architecture limitation** — Pass 2 Resolve 재설계 (B3 or C1) + v4 pending_refs 활용 후 해결.

---

## 7. 로깅 현황 + 권고

### 현재 상태

**Go 측**:
- `log/slog` 기본 라이브러리 사용 (pipeline.go L9, build.go L5)
- Handler: `slog.NewTextHandler(os.Stderr, nil)` — 고정 info level, 옵션 없음
- 로그 출력: emitDerivedPasses L48 `log.Info("xlang linked", "binds_to", len(xlEdges))` 같은 주요 지점만 명시

**CLI 측**:
- `--verbose` / `--debug` flag 미구현
- `--no-cache` / `--rebuild-metrics` flag만 존재 (build subcommand)
- `CKG_LOG_LEVEL` 같은 env var 미지원

**MCP 측**:
- server.go에서 logging 출력 없음 (stdio는 MCP protocol)

### 권고 (Priority P2)

**단기 (선택적)**:
1. **Log level control**: slog.Logger를 `slog.LevelDebug` 옵션 가능하도록 CLI flag 추가 (estimated S, ~20분)
   ```go
   // build.go 수정
   logLevel := slog.LevelInfo
   if verbose { logLevel = slog.LevelDebug }
   log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
   ```

2. **선택적 구조화 로그**: 기존 text handler 유지하되, eval.go처럼 JSON output option 추가 (현재 eval은 JSON report 생성)

**장기**:
- User-facing log output (info + error) vs debug output (trace-level) 분리
- Production split (serve --no-viewer 처럼 operator 대상) 시 structured JSON 로깅

### 현재 로깅은 충분한가?

**결론**: 사용자 4 완성도 조건 관점에서는 충분 (build stats, audit results를 stderr에 출력).
사용자가 deep-dive 또는 troubleshooting 필요 시 추후 로그 레벨 fine-tuning으로 해결 가능.

---

## 8. 다음 세션 액션 아이템 (Quick checklist)

- [ ] `docs/HANDOFF.md` § 4 우선순위 확인
- [ ] B2 spec detail + test plan 정리 (1-2h effort, A4 StoreWriter interface 활용)
- [ ] E2-FU + Wave1-DoD 병렬 처리 (main session, S effort)
- [ ] B2 subagent dispatch (large-task checklist: token budget ~150-200K, real-corpus parity check 명시)
- [ ] B2 review (code-reviewer subagent 또는 main session)
- [ ] C1/G6 v4 로드맵 재검토 (B3 vs C1 진입점 선택)

---

## 9. 참고 문서

- **HANDOFF.md** — 현재 snapshot + 다음 우선순위 (본 리포트와 동기화)
- **WORK-PLAN.md** — 작업 tracking (Group A-G, Wave 1-7)
- **spec-ckg-v0.2.md** — v0.2 foundation spec (B/C/D 상세)
- **G6-INCREMENTAL-REDESIGN.md** — 3회 실패 분석 + v4 설계 도움말 (§ 8 D4)
- **G6-V3-VALIDATION-FINDINGS.md** — root cause H3 + 가설 검증 가이드
- **INCREMENTAL.md** — A3 cache 운영 (operator-facing)
- **SCHEMA.md** — 33 nodes × 30 edges reference

---

**작성**: Claude Code (status agent)  
**검토 대상**: 사용자 (다음 작업 방향 결정)  
**Refresh date**: 2026-05-04 (refresh 2 — B2/Logging/Channel flow 완료)  
**상태**: B2 ✅ Logging ✅ Channel flow ✅. 다음 우선순위: G6 v4 (ORDER BY start_line ASC — S effort, 즉시 진입 가능)
