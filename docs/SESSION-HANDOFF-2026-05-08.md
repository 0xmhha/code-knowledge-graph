# Session Hand-off — 2026-05-08

> 다음 세션에서 본 작업을 이어가기 위한 단일 진입점 문서.
> 이 문서를 먼저 읽고 → 필요한 세부 plan/design 문서로 이동.

---

## 1. 한 줄 요약

CKG (Code Knowledge Graph)의 **viewer-next 대대적 UX 개편 + Track C(detector 강화) + Track D(viewer 가시화) + S0 spec 정합**이 본 세션의 주요 산출물. 다음 세션 후보는 **Hunk H1 구현** (Track F의 첫 단계) 또는 **TS body walk (P3)**.

---

## 2. Repo 상태 (2026-05-08 기준)

- **Path**: `/Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph`
- **Branch**: `main`
- **HEAD**: `5f2b16b` `fix(viewer): legend → bottom-right tip box with drag resize`
- **Untracked**: 없음 (clean)
- **Schema 버전**: **1.7** (dispatch_kind 컬럼; Track C에서 bump)
- **Schema 1.8 예약**: Track F (Hunk graph) 구현 시점에 사용 (`docs/design/hunk-graph.md` §2.6)

### 사이드 repo
- `/Users/wm-it-22-00661/Work/github/tools/code-knowledge-vector` — CKV repo (별도 git, S1 plan만 작성됨)

### 외부 docs (read-only)
- `/Users/wm-it-22-00661/Work/github/study/ai/01.study/projects/stablenet-ai-agent/claudedocs/EXECUTION-GUIDE.md` — vertical-slice S0~S6 spec

---

## 3. 환경

| 항목 | 값 |
|---|---|
| HTTP port (default) | **8080** (S0 spec 정합, commit `c3849aa` 변경) |
| Self-graph dir | `/tmp/ckg-self` |
| Self-graph counts | nodes 21,231 / edges 81,531 (Track C 적용 후) |
| Self-graph languages | go 13,691 / git 197 / ts 6,846 / sol 16 |
| Build command | `make build` (Next.js export → embed → Go binary) |
| Restart serve | `lsof -nP -iTCP:8080 -sTCP:LISTEN \| tail -1 \| awk '{print $2}' \| xargs kill && nohup ./bin/ckg serve --graph /tmp/ckg-self > /tmp/ckg-serve.log 2>&1 &` |
| Rebuild self-graph | `./bin/ckg build --src . --out /tmp/ckg-self --no-cache` |

---

## 4. 본 세션 commit 누적 (시간 역순)

| Commit | 카테고리 | 내용 |
|---|---|---|
| `5f2b16b` | viewer fix | legend → bottom-right tip box with drag resize |
| `a9c8184` | viewer feat | call-flow side panel + 3-column grid layout (#4) |
| `67e3fe0` | viewer feat | canvas legend overlay + per-type node shapes + edge dash patterns (#3) |
| `a74e523` | viewer fix | search input race + collapsible NodeList sections (#1, #2) |
| `2392914` | viewer feat | node-type filter + package picker + impact dim + back nav (#6+#7+#8+#10) |
| `e50f6a8` | docs | hunk-graph schema bump 1.7 → 1.8 retarget |
| `8125f73` | viewer feat | Track D — 6-graph axis weight badges + empty-axis warning |
| `e24d4f3` | viewer feat | TraceControls anchor-aware disable + visible count |
| `7b32031` | parse feat | **Track C** — uses_type/instantiates/invokes detectors + lock fix + schema 1.7 |
| `6f129f6` | viewer feat | selection ring + community hide + retrace + resize handle (#1+#2+#4+#5a+#9) |
| `1eeb6ce` | docs | Track C detector-gap analysis |
| `c3849aa` | chore | **default port 8787 → 8080** (S0 spec 정합) |
| `4090fdd` | viewer fix | explicit grid-row to survive stale-banner absence |
| `79f646c` | test | regression spec for the 7 user-reported complaints |
| `5a52108` | docs | Hunk graph design plan (H1~H4) |
| `b60a50f` | viewer fix | exclude Commit from boot + auto-enable group on empty trace |
| `ed0359f` | detect fix | match nested .ckgignore dir patterns + ignore .next/out |
| `1543f74` | viewer feat | Home button = full initial-state reset, always visible |
| `7323e35` | viewer fix | force panel above canvas via explicit stacking context |
| `db83aef` | viewer refactor | panel visual contrast + drop redundant ControlLayer |
| `8323775` | viewer fix | responsive panel grid + safer migration write order |
| `719f3e8` | viewer fix | widen panel + migrate edge-filter default + visible empty state |
| `493ca63` | viewer fix | panel default-open + edge-filter sections default-collapsed |

전체 24 commits in this session. 이전 세션의 commit은 `2fb3d3a` 이전.

---

## 5. 완료된 트랙 / 작업 그룹

### A. 사용자 호소 진단 (Track A, commit base 1543f74)
- 사용자 7가지 호소 중 6개 = stale cache, 1개 (Commit-click empty edge) = 진짜 버그
- `b60a50f`로 fix (Plan A: boot에서 Commit 제외 + Plan B: 0-edge auto-enable group)
- regression spec: `web/viewer-next/tests/diag-7-complaints.spec.js`

### B. .ckgignore matcher 수정 (commit `ed0359f`)
- nested 디렉토리 (`web/viewer-next/node_modules`) 제외 안 되던 버그
- TS/Sol 파서가 갑자기 활성화된 부수 효과 (TS 6,846 / Sol 16 nodes 등장)
- P3 (TS body walk)와 무관 — discovery layer 버그

### C. 6-graph detector 강화 (commit `7b32031`, plan `1eeb6ce`)
- **uses_type**: 0 → 460 (Func args/return + struct field type)
- **instantiates**: 0 → 276 (composite literal + new())
- **invokes**: 0 → 334 (interface_method 228 / func_value 96 / closure 8 / method_value 2)
- **acquires_lock/releases_lock**: 2/2 → 4/4 (goroutine body lock CallExpr 누락 fix)
- **schema 1.7**: edges + pending_refs에 `dispatch_kind` 컬럼 추가
- **G2 Semantic 22 → 759** (3445% 증가, axis 부활)

### D. Viewer 6-graph 분포 가시화 (commit `8125f73`)
- `/api/edges/counts` 새 endpoint
- EdgeFilters의 G1~G6 pill 옆에 count badge ("21k", "759", "23")
- 0건/희소 group은 `.g-empty` (italic + amber ⚠) 표시
- 측정값: G1 21,092 / G2 759 / G3 2,251 / G4 23 ⚠ / G5 14 ⚠ / G6 57,391

### E. Panel 가시성 + UX 개편 시리즈 (다수 commit)
- 직전 세션 issue들 모두 해결
- panel grid auto-placement (stale-banner 부재 시 row shift) → explicit grid-row
- panel z-index + isolation:isolate (canvas overlay 차단)
- panel resize handle (240~800px drag)
- selection cyan ring + 1-hop ring + 2-hop dim
- community toggle = visibility off (이전: alpha 0.18 dim)
- Home button = full initial-state reset (anchor/search/whitelist/isolation/dim/trace 모두)
- TraceControls anchor-aware disable + visible count chip

### F. M batch (commit `2392914`)
- **#6 NODE TYPES filter**: NodeTypeFilters 컴포넌트 (5 collapsible groups: Symbols/Members/Containers/Statements/Concurrency-VCS), localStorage persist
- **#7 Package picker**: NodeList 위 📦 Packages 섹션 (`api.nodes('', 500)` 1회 fetch, 알파벳 정렬, 25개 preview + show all)
- **#8 Impact dim**: visibleIds 유지 + dimmedNodes로 0.2 alpha (이전: visibleIds 교체)
- **#10 Back history**: historyStack (cap 20) + ← Back 버튼 + Backspace 키보드

### G. Search + section toggle (commit `a74e523`)
- 검색창 글자 입력 race fix (useEffect deps에서 q를 ref로 분리)
- NodeList Packages + Visible Nodes 섹션 collapsible (▶/▼)

### H. Canvas legend + call flow (commits `67e3fe0` + `a9c8184` + `5f2b16b`)
- Node shapes 11종 (drawNode2D switch on type)
- Edge dash patterns (G1 solid / G2 dashed / G3 solid / G4 dotted / G5 dash-dot / G6 thin)
- CanvasLegend 컴포넌트: 우측 하단 220x220 tip box + 좌상단 corner drag resize + ✕/▶ trigger
- CallFlow 컴포넌트: 좌측 새 column (anchor 있을 때만), callees BFS depth 3, ASCII tree (├─/└─)
- 3-column grid 전환 (`#app` grid-template-columns)

---

## 6. Plan / design 문서 위치

| 파일 | 내용 | 상태 |
|---|---|---|
| `docs/design/track-c-detector-gap.md` (318 lines) | Track C 진단: 0건/희소 edge type 분석 + 우선순위 | ✅ 구현 완료 |
| `docs/design/hunk-graph.md` (1165 lines, 14 sections) | Hunk graph H1~H4 설계 + 7 open decisions | 🟡 plan only, 구현 대기 |
| `/Users/wm-it-22-00661/Work/github/tools/code-knowledge-vector/docs/plan-S1-ckv.md` (637 lines, 16 sections) | CKV S1 vertical-slice plan | 🟡 plan only, 외부 repo |
| `web/viewer-next/tests/diag-7-complaints.spec.js` | viewer 7 호소 regression spec | ✅ 사용 가능 |

---

## 7. 진행 중 / 대기 중 작업

### 🟡 #15 P3 — TS body walk (사용자 명시 후순위)
- TypeScript function body 안 statement-level parsing (calls, invokes, etc).
- 현재 TS 노드는 6,846개 있지만 함수 body 내부 분석은 없음.
- 사용자 명시: P3은 후순위, 진행 전 재확인 필요.

### 🟡 #35 Hunk H1 — Hunk graph 첫 단계 구현
- Plan: `docs/design/hunk-graph.md` (이미 작성됨, 1165 lines).
- 7 open decisions가 사용자 결정 대기 중:
  1. Patch encoding (gzip vs zstd vs raw) — 추천 gzip
  2. Hunk dedup 처리 — 추천 H1에서는 안 함
  3. Soft-delete (force-push로 unreachable commits) — 추천 H1는 그대로 둠
  4. Multi-language hunks — 추천 target 파일 language 따라
  5. Cross-commit hunk linking — 추천 out of scope (H1~H3)
  6. Blob retrieval cap — 추천 64KB
  7. Pagerank/Leiden treatment of Hunk + Commit — 추천 둘 다 exclude
- Schema 1.7 → **1.8** bump 예정 (Track C가 1.7 차지함; e50f6a8 retarget 적용)
- 구현 단위: H1 (git collector → Hunk row + has_hunk edge + patch blob), H2 (modifies edge), H3 (EvidencePack assembler), H4 (issue ID extraction)

---

## 8. 알려진 잔존 호소 / known issues

### Cosmetic noise (panel과 무관)
- React #418 hydration mismatch warning — `App.tsx`의 `panelHidden` useState initialiser가 SSR/CSR mismatch. 동작은 정상, console 경고만.
- `/manifest.json` 404 — Next.js PWA manifest, 미구현. 동작 무관.
- `/favicon.ico` 404 — 미구현. 동작 무관.
- Three.js multiple-instances console warning — react-force-graph-2d / 3d 둘 다 three를 import. webpack alias로 해결 가능하지만 미적용.
- Pre-existing ESLint warnings:
  - `App.tsx:203` unused `eslint-disable-next-line no-console`
  - `GraphCanvas.tsx:219` `useMemo` unnecessary `viewMode` dep

### Stale graph banner
- `src_commit` 변경 시 노란 banner 표시. `./bin/ckg build --src . --out /tmp/ckg-self --no-cache`로 갱신 가능.

### 사용자가 검증해야 할 것 (이번 세션 마지막 commit `5f2b16b` 이후)
- Cmd+Shift+R 후 8080에서 모든 viewer 기능 동작 확인
- 특히 마지막 fix: legend 우측 하단 tip box + drag resize

---

## 9. 우선순위 권고 (다음 세션)

| 우선 | 작업 | 사이즈 | 비고 |
|---|---|---|---|
| **1** | 사용자 잔존 호소 처리 (있을 경우) | 가변 | hard refresh로 검증 후 |
| **2** | Hunk H1 구현 (#35) | M (~4h) | plan 검토 + 7 open decisions 답변 후 |
| **3** | TS body walk (P3, #15) | M-L (~6-8h) | P3은 사용자 후순위 명시. 진행 전 재확인 |
| **4** | Hunk H2 (modifies edge) | M (~3h) | H1 land 후 |
| **5** | Hunk H3 (EvidencePack assembler) | L (~5h) | CKV S1과 같이 진행 권고 |
| **6** | CKV S1 구현 시작 | XL | code-knowledge-vector repo, plan-S1-ckv.md 참조 |
| **7** | Cosmetic cleanup (#418, manifest/favicon, three.js alias) | S | 우선순위 낮음 |

---

## 10. 글로벌 룰 / 컨벤션

- **commit message에 Co-Authored-By 또는 "Generated with Claude Code" 금지** (사용자 글로벌 룰)
- 한국어로 사용자 응답 (사용자 한국어 사용 시)
- 작업 종료 시 uncommitted 변경사항이 남아있으면 commit 여부 확인
- 모델 라우팅: 구현/리팩토링/디버깅 = Sonnet 4.6 / 아키텍처/크리티컬 리뷰 = Opus 4.7
- file size 권장 200~400 lines / 함수 < 50 lines / 중첩 ≤3 levels

---

## 11. Decisions log (이번 세션)

| 결정 | 옵션 | 선택 | 이유 |
|---|---|---|---|
| Track C q1 (uses_type granularity) | A: arg+return / B: A+var annotation / C: B+composite | **B** | 사용자 결정 |
| Track C q2 (function value 호출 분류) | A: calls / B: invokes / B+meta: invokes + dispatch_kind | **B+meta** | 사용자 결정. dispatch_kind 컬럼 추가 |
| Track C q3 (lock 버그 fix 위치) | A: GoStmt return false 수정 / B: emitGoroutineChannelEdges 확장 | **B** | 사용자 결정. 변경 영향 작음 |
| Track C q4 (uses_type 미해결 타입 처리) | A: pending_refs / B: drop+warning | **A** | 사용자 결정. incremental cache 일관성 |
| #5 Commit-click fix | A: boot ranking type-aware / B: 0-edge auto-enable / A+B | **A+B** | 사용자 결정 |
| Track F schema 충돌 | 1.7 (Track C와 충돌) / 1.8 retarget | **1.8** | 본 세션에서 retarget |
| Port spec | 8787 default / 8080 spec 따름 | **8080** | EXECUTION-GUIDE S0 명시 |
| Track B (.ckgignore matcher) commit | 적용 / revert | **유지** | TS/Sol fix 부수 효과는 P3와 무관 |

---

## 12. 다음 세션 cold-start 권장 흐름

1. **이 문서를 먼저 읽기** (`docs/SESSION-HANDOFF-2026-05-08.md`)
2. `git log --oneline | head -30`으로 본 세션 commit 확인
3. `lsof -nP -iTCP:8080 -sTCP:LISTEN`로 ckg serve 떠 있는지 확인 (없으면 §3의 명령으로 재기동)
4. 사용자에게 **검증 결과 + 다음 우선순위** 묻기
5. 진행 결정 후 §6의 plan 파일 또는 §9의 우선순위 표 따라 dispatch

---

## 13. 부록: 주요 파일 위치 cheat-sheet

### Backend (Go)
- `cmd/ckg/serve.go` — serve CLI (default port 8080)
- `cmd/ckg/build.go` — build CLI
- `internal/server/api.go` — HTTP handlers (`handleTopNodes`, `handleEdgeCounts`, `handleEdges`, `handleImpact`)
- `internal/server/server.go` — route registry
- `internal/persist/store_interface.go` — StoreReader/StoreWriter/Store ISP
- `internal/persist/sqlite.go` — SQLite impl + Migrate (1.6→1.7 dispatch_kind)
- `internal/persist/postgres_store.go` — Postgres impl
- `internal/persist/schema.sql` — schema 1.7
- `internal/buildpipe/cache.go` — SchemaVersion = "1.7"
- `internal/buildpipe/language_runners.go` — concurrent fan-out
- `internal/parse/golang/{uses_type,instantiates,implements,context_paths,concurrency,statements,resolve}.go` — Go detectors
- `internal/parse/typescript/` — TS parser
- `internal/parse/solidity/` — Solidity parser
- `internal/temporal/git.go` — Git history collector (Hunk H1 확장 예정)
- `pkg/types/{node,edge}.go` — public types (Edge.DispatchKind 추가)
- `pkg/store/store.go` — Reader 공개 API
- `pkg/bm25/scorer.go` — Okapi BM25
- `pkg/impact/impact.go` — impact analysis

### Frontend (TypeScript)
- `web/viewer-next/src/components/App.tsx` — root, history stack, panel resize
- `web/viewer-next/src/components/{TopBar,BottomBar,SearchBox,NodeList,NodeDetail,TraceControls,EdgeTypeFilters,NodeTypeFilters,Legend,GraphCanvas,CanvasLegend,CallFlow,HelpOverlay,FirstTimeOverlay}.tsx`
- `web/viewer-next/src/store/store.ts` — zustand state
- `web/viewer-next/src/lib/{api,depth,trace,edges,encoding}.ts`
- `web/viewer-next/src/types.ts` — GraphNode/GraphEdge interfaces
- `web/viewer-next/app/globals.css` — 모든 viewer 스타일

### Tests
- `internal/parse/golang/uses_type_test.go`
- `internal/parse/golang/concurrency_test.go`
- `internal/persist/sqlite_extra_test.go`
- `web/viewer-next/tests/diag-7-complaints.spec.js`

### Docs
- `docs/design/track-c-detector-gap.md`
- `docs/design/hunk-graph.md`
- `docs/SESSION-HANDOFF-2026-05-08.md` (이 문서)
- `docs/SCHEMA.md` (외부 — schema 1.7 반영 필요할 수 있음)
- `docs/ARCHITECTURE.md`

---

**End of hand-off.** 이 문서는 `git add docs/SESSION-HANDOFF-2026-05-08.md` 후 commit 권고.
