# CKG Viewer 리뉴얼 작업 핸드오프 — 2026-05-23

> 이 문서를 읽는 다음 세션의 Claude/사용자에게:
> 이번 세션에서 viewer의 큰 폭 리팩토링이 진행되었고 **검증과 commit은 미완료**입니다.
> 코드를 만지기 전 이 문서 전체를 읽고, 특히 "데이터 흐름 모델 변경"과 "남은 작업" 섹션을 숙지하세요.

---

## 한 줄 요약

`/tmp/ckg-stablenet` (go-stablenet 코드베이스, 노드 210K) 그래프 viewer의 UX/성능 개선. 캔버스 반응형·layout grid 복귀·hydration mismatch·OrbitControls 안정화·데이터 로딩 전략 전면 개편(매 클릭마다 refetch → boot 시 한 번 로드 후 focus halo 만 갱신)·Test 코드 토글·노드 수 사용자 제어까지 큰 변경 다수. 다음 세션은 **브라우저 검증 + commit 분할**부터.

---

## 1. 작업 컨텍스트

### 1.1 프로젝트
- **저장소 루트**: `/Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph`
- **Branch**: `main` (직접 작업, PR 없음)
- **viewer 위치**: `web/viewer-next/` (Next.js 15 · React 18.3 · TypeScript)
- **백엔드 위치**: `internal/server/` (Go, `cmd/ckg`에서 `ckg serve`로 기동)
- **그래프 라이브러리**: `react-force-graph-2d` + `react-force-graph-3d` (vasturiano). 내부적으로 `force-graph` (vanilla, Canvas2D) + `3d-force-graph` (vanilla, Three.js WebGL) + d3-force-3d layout

### 1.2 데이터 환경
- **DB 경로**: `/tmp/ckg-stablenet/graph.db` (sqlite)
- **소스 루트**: `/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest`
- **노드/엣지**: 210,799 / 708,046
- **Test 비율**: 83,680 / 210,799 = **39.7%**
- **노드 타입 분포** (상위): CallSite 112K (53%), IfStmt 22K, ReturnStmt 19K, Variable 8.8K, Method 8.4K, Field 7.9K, Function 6.5K, Import 6.4K, Hunk 5.4K, LoopStmt 5.2K, Struct 1.8K
- **엣지 타입 분포** (상위): changed_in 339K, contains 163K, calls 83K, modifies 38K, defines 36K

### 1.3 사용자 환경 / 제약
- 노트북 사양 한정적: **27K 노드는 못 버틴다**고 사용자가 명시 (2026-05-22 오후 확인). 현 디폴트는 5K, 사용자가 [500, 1K, 2K, 5K, 10K]에서 선택 가능.
- Playwright MCP + dev server + 백엔드 + 사용자 브라우저 동시 띄우면 리소스 압박 발생. 검증은 **사용자 브라우저로만** 진행 권장.
- 응답 언어: 한글 (사용자 선호). Commit 메시지: English, no co-author, no dev-stage jargon.

### 1.4 검증된 도구 버전 (이번 세션 작업 시점 기준)
| 도구 | 버전 | 비고 |
|---|---|---|
| Node.js | v24.4.0 | Next.js 15 호환 — v20+ 권장 |
| Go | 1.25.9 darwin/arm64 | `go.mod`은 1.25.5 minimum |
| sqlite3 (CLI) | 3.43.2 | DB inspect용. backend는 modernc.org/sqlite (CGO 없음) 사용 |
| npm | (Node v24 동봉) | `package-lock.json` 존재 → `npm ci` 권장 |

OS는 macOS (darwin/arm64)에서 검증. Linux x86_64에서도 동작 예상이나 미검증.

### 1.5 다른 머신 setup 절차 (clean clone부터)
```bash
# 1. clone + branch
git clone https://github.com/0xmhha/code-knowledge-graph.git
cd code-knowledge-graph
git checkout main

# 2. Go deps + backend 빌드
go mod download
go build -o bin/ckg ./cmd/ckg/

# 3. Viewer deps install (반드시 web/viewer-next/ 안에서)
cd web/viewer-next
npm ci
cd ../..

# 4. (필요 시) 대상 source repo 준비 — 예: go-stablenet
#    이번 세션은 사용자가 미리 받아둔 로컬 경로를 사용했음:
#    /Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest
#    다른 머신이라면 본인 환경에 맞게 clone 후 절대경로를 기록해 둘 것.
#    git clone <stable-net repo URL> /path/to/stable-net

# 5. graph.db 생성 (10~20분 소요 — 1259 Go 파일 + Solidity + TS 파싱 + PageRank/Leiden)
./bin/ckg build --src /path/to/stable-net --out /tmp/ckg-stablenet
#    --src: 분석 대상 source root (디렉토리)
#    --out: graph.db가 저장될 디렉토리 (sqlite + manifest.json)
#    완료 후 `/tmp/ckg-stablenet/graph.db` + `manifest.json` 생성됨

# 6. 백엔드 기동 (별도 터미널)
./bin/ckg serve --graph /tmp/ckg-stablenet --port 8080
#    healthcheck: curl http://localhost:8080/api/manifest | head

# 7. Viewer dev server (별도 터미널)
cd web/viewer-next && npm run dev
#    Next.js dev, port 3001. Fast Refresh 사용. 출력에서 "Ready in <ms>" 확인.

# 8. 브라우저 직접 접속
open "http://localhost:3001/?fresh=$(date +%s)"
#    ?fresh 쿼리는 dev mode의 308 redirect 캐시 회피
```

**다른 source repo로도 가능**: `/tmp/ckg-stablenet` 경로는 단순 약속이라 `ckg build --out <어디든>` 후 `ckg serve --graph <같은 경로>`로 맞추기만 하면 됨. node/edge 통계는 source repo 크기에 비례하므로 nodeLimit default 5K가 작은 repo엔 과할 수 있음 (그땐 500 등으로 dial down).

---

## 2. 이번 세션의 큰 변경 (4 라운드)

### Round 1: 캔버스 반응형 + Layout
**문제**: 캔버스 크기가 viewport와 무관하게 고정. 패널이 absolute overlay라 canvas-host가 viewport 전체였고, 패널 뒤로 canvas 중심이 밀려남.

**해결**:
1. `react-force-graph`는 부모를 ResizeObserver로 추적하지 않음 — `force-graph.mjs:1021`에서 width/height 기본값을 `window.innerWidth/Height`로 한 번만 잡고 그대로 둠. `GraphCanvas` Props에 `width: number; height: number` 복원, App.tsx의 ResizeObserver-tracked `canvasSize`를 전달.
2. SideNav / Panel / CallFlow를 `position: absolute` overlay → CSS Grid 컬럼 (1·2·3·4)으로 복귀. canvas-host(col 3)가 자동으로 unoccluded band를 차지 → 기하학적 중심 = 시각적 중심.
3. `#app grid-template-columns: minmax(0, 1fr)` (bare `1fr`은 min-content가 viewport 초과 시 컬럼이 부풀어 H scrollbar 유발).

### Round 2: Hydration / OrbitControls / SideNav 단순화
**Hydration mismatch** (`CanvasLegend.tsx`): hex/star SVG polygon `points`에서 `Math.cos/sin`의 마지막 비트가 Node SSR과 브라우저에서 미세하게 달라 React가 attribute 단위 비교 시 mismatch로 잡음.
→ `fmt2(n) = parseFloat(n.toFixed(2))` 헬퍼로 정밀도 절단.

**OrbitControls 에러** (`GraphCanvas.tsx`): 노드 클릭 시 3d-force-graph가 dragend 시 dispatch하는 synthetic `pointerup` (pointerId=0)이 OrbitControls의 case 1 분기에서 `_pointerPositions[id].x` 읽기 실패.
→ rAF 루프 안에서 instance의 `_onPointerUp`을 한 번 try/catch로 wrap. **주의**: Three r182+ `connect()`는 element 인자 필수 — disconnect 전에 `ctrls.domElement` 저장하여 `connect(el)` 호출. 빠뜨리면 listener 전체가 detach되어 OrbitControls 마비.

**SideNav 단순화**: 항상 접힌 52px 아이콘 전용. 토글 버튼 제거. `SideNav.tsx`는 신규 untracked 파일.

### Round 3: 데이터 흐름 모델 + Test/Node Type 필터
**핵심 패러다임 변화**: 매 anchor 클릭마다 `traceFromNode`로 새 visibleIds 계산 → commit 하던 구조 폐기. boot 시 production non-statement 노드 한 번에 로드. 클릭 시 anchorId + focusDistance만 갱신, **visibleIds는 boot 이후 변하지 않음**. 사용자의 "튀는 현상" 호소 해결.

- `lib/depth.ts` `recomputeVisible`: anchor 없을 때 `topNodes('pagerank', s.nodeLimit, BOOT_EXCLUDED_TYPES)` + `isTestPath` 필터 + edges BATCH POST(5000개씩).
- `lib/testFilter.ts` (신규): `_test.go` / `/test(s)/` / `.test.ts` / `.spec.ts` / `/__tests__/` / `.t.sol` / `/mock(s)/` 패턴.
- store: `excludeTests: boolean` (default true) + `setExcludeTests` + localStorage.
- TopBar: `🧪 Test ON/OFF` 버튼 (default OFF).
- App.tsx `traceAndCommit`: visibleIds 유지, focusDistance만 새로 계산, edges는 필요 시 lazy fetch.
- App.tsx trace direction/depth 변경 effect: 마찬가지로 visibleIds 유지.
- 백엔드 `internal/server/api.go`: `handleTopNodes` limit cap 1000 → **100000** 상향.

**Statement-level 노드 default off**: `DEFAULT_NODE_TYPES_ON`에서 이미 제외됨. 추가로 `BOOT_EXCLUDED_TYPES`로 fetch 자체도 제외 (CallSite/IfStmt/ReturnStmt/LoopStmt/SwitchStmt/Hunk/Commit/Import/Field/Variable/Constant).

**dagMode**: anchor 활성 시 `dagMode='lr'`, `onDagError='remove'` (call graph cycle 안전), `dagLevelDistance=120`. 화살표 길이 3→6, RelPos 0.95→0.92.

### Round 4: nodeLimit 사용자 제어
**문제**: 32K 노드가 사용자 노트북에서 안 돌아감. 디폴트만 줄여도 다른 사용자/머신엔 부적합.

**해결**:
- store에 `nodeLimit: number` (default 5000) + `setNodeLimit(n)` (clamp `[100, 100000]`) + localStorage.
- `lib/depth.ts`의 하드코딩 `BOOT_VISIBLE_LIMIT` 제거 → `s.nodeLimit` live read.
- App.tsx subscribe effect를 확장: `excludeTests`와 `nodeLimit` 두 subscribe가 같은 `requestId` 가드를 공유 → 빠른 동시 변경에서도 latest fetch 하나만 commit.
- TopBar에 `📊 [500 / 1K / 2K / 5K / 10K ▼]` select dropdown 추가.

**Test 토글 OFF→ON은 되는데 ON→OFF가 안 되던 버그**: 동일 라운드에서 발견. 원인은 ref-기반 equality 패턴이 React 18 자동 배칭에서 transition을 누락. zustand `subscribeWithSelector` middleware의 `useStore.subscribe(selector, listener)`로 교체 (이미 store에 middleware 설정되어 있음). 이게 핵심 fix.

---

## 3. 변경 파일 인벤토리

```
$ git status -s
 M internal/server/api.go                              ← Round 3 (backend limit cap)
 M internal/server/web_assets/index.html               ← 이전 빌드 산출물 (이번 세션 직접 변경 아님; 재빌드 시 갱신됨)
 M web/viewer-next/app/globals.css                     ← Round 1·2·3·4 모두
 M web/viewer-next/next.config.mjs                     ← Round 1 (skipTrailingSlashRedirect)
 M web/viewer-next/src/components/App.tsx              ← Round 1·3·4
 M web/viewer-next/src/components/CanvasLegend.tsx     ← Round 2 (fmt2)
 M web/viewer-next/src/components/GraphCanvas.tsx      ← Round 1·2·3
 M web/viewer-next/src/components/TopBar.tsx           ← Round 3·4
 M web/viewer-next/src/lib/depth.ts                    ← Round 3·4
 M web/viewer-next/src/store/store.ts                  ← Round 3·4
?? web/viewer-next/README.md                          ← 이번 세션 외 작업
?? web/viewer-next/src/components/SideNav.tsx         ← Round 2 (신규)
?? web/viewer-next/src/lib/testFilter.ts              ← Round 3 (신규)
?? eval/stablenet/CKS-INTEGRATION-2026-05-23.md       ← 별도 작업 (viewer와 무관)

총 변경: +1222 / -165 (10 files modified, 3 untracked)
```

### 파일별 핵심 변경

| 파일 | 핵심 변경 요약 |
|---|---|
| `web/viewer-next/app/globals.css` | grid 4컬럼 (`52px CALLFLOW_W 1fr PANEL_W`), `.side-nav grid-column:1`, `.call-flow grid-column:2`, `.canvas-host grid-column:3`, `.panel grid-column:4`. topbar/bottombar/banner는 `grid-column: 1 / -1`. `.topbar-test-toggle` + `.topbar-node-limit` 스타일 추가. SideNav `.collapsed` variant 삭제(항상 52px). |
| `web/viewer-next/next.config.mjs` | `skipTrailingSlashRedirect: true` (dev mode `/api` 308 redirect 루프 차단) |
| `web/viewer-next/src/components/App.tsx` | `excludeTests` + `nodeLimit` zustand subscribe effect (공유 `requestId` 가드). `traceAndCommit`이 visibleIds 유지하고 focusDistance만 갱신. 패널 toggle 시 inline `gridTemplateColumns` 적용. `canvasHostRef` ResizeObserver로 `canvasSize` 추적 → GraphCanvas `width`/`height` props로 전달. |
| `web/viewer-next/src/components/CanvasLegend.tsx` | `fmt2 = parseFloat(n.toFixed(2))` 헬퍼로 hex/star/asterisk SVG 좌표 정밀도 절단. |
| `web/viewer-next/src/components/GraphCanvas.tsx` | Props `width`/`height` 복원. rAF tick에서 OrbitControls `_onPointerUp` 한 번 patch (disconnect → wrap with try/catch → `connect(domElement)`). `d3Force('charge').strength(-120)` + `d3Force('link').distance(80)` 튜닝. `dagMode={anchorId ? 'lr' : undefined}` + `onDagError=()=>'remove'` + `dagLevelDistance=120`. `linkDirectionalArrowLength=6`, `linkDirectionalArrowRelPos=0.92`. `cooldownTicks` 노드 수에 따라 80↔250 분기. |
| `web/viewer-next/src/components/TopBar.tsx` | `🧪 Test ON/OFF` 버튼 + `📊 nodeLimit select`. excludeTests/nodeLimit는 zustand selector로 read, setter로 write. |
| `web/viewer-next/src/lib/depth.ts` | `recomputeVisible`이 anchor 없을 때 `s.nodeLimit` 만큼 top-by-pagerank fetch + `BOOT_EXCLUDED_TYPES` 제외 + `s.excludeTests`면 `isTestPath`로 filter + edges BATCH(5000) POST. |
| `web/viewer-next/src/store/store.ts` | `excludeTests: boolean`, `nodeLimit: number` state + setter + localStorage (`ckg.excludeTests`, `ckg.nodeLimit`). `hydrateFromStorage`에서 둘 다 복원. `DEFAULT_NODE_TYPES_ON`에서 Field/Variable/Constant 제거. |
| `web/viewer-next/src/components/SideNav.tsx` *(신규)* | 항상 접힌 52px 아이콘 전용 nav. toggle 없음. |
| `web/viewer-next/src/lib/testFilter.ts` *(신규)* | `isTestPath(filePath)` — test 경로 식별 헬퍼. Go/TS/Solidity/Jest 컨벤션 모두 커버. |
| `internal/server/api.go` | `handleTopNodes`의 `if limit > 1000 { limit = 200 }` → `if limit > 100000 { limit = 200 }`. **이미 `bin/ckg`로 재빌드 완료** (2026-05-22 18:01). |
| `internal/server/web_assets/index.html` | 이전 viewer build 산출물. **이번 세션에서 직접 편집 아님**. 다음에 `npm run build` 후 embed 빌드 step 실행 시 자동 갱신. |

---

## 4. 핵심 아키텍처 결정사항 (Why)

### 4.1 "boot에서 한 번 로드 + 클릭은 focus만 갱신"
- **Before**: 클릭마다 `traceFromNode` → BFS로 새 visibleIds 계산 → commit. 시각적으로 그래프가 "확 줄었다 늘었다" 튀고, Back으로 되돌리기 어색.
- **After**: boot에서 N개 노드 한 번에 로드. 클릭은 `anchorId` + `focusDistance` (BFS depth 0/1/2)만 갱신. visibleIds 안 변함. Back은 anchor/focus 복원으로 즉시 동작.
- **트레이드오프**: 한 번에 N개 force simulation 돌려야 함 → N이 너무 크면 cooldown 시간 길어짐. 그래서 nodeLimit 사용자 제어 도입.

### 4.2 zustand subscribe (not React effect + ref)
- React 18 자동 배칭에서 같은 tick의 hydration write + user click이 하나의 render로 합쳐지면, effect는 한 번만 fire하고 ref가 final value로 점프 → transition 누락.
- `useStore.subscribe(selector, listener)`는 store 단의 immediate notification이라 모든 transition 빠짐없이 잡힘. `subscribeWithSelector` middleware가 store에 이미 적용되어 있음 (이미 다른 곳에서 쓰는 패턴).
- `requestId` 가드로 빠른 연속 변경 시에도 race condition 자연스럽게 해결.

### 4.3 Statement/Field/Variable/Constant default 제외
- CallSite 112K (53%), IfStmt/ReturnStmt/LoopStmt 등 합 23%, Variable/Field/Constant 9% — 합치면 85%가 "architectural overview에 안 필요한 노이즈".
- `DEFAULT_NODE_TYPES_ON` (render gate)에서도 제외 + `BOOT_EXCLUDED_TYPES` (fetch gate)에서도 제외. 두 layer 모두 차단해야 데이터/렌더 비용 둘 다 절약.
- 사용자가 NodeTypeFilters panel에서 opt-in 가능 (다만 한 번 opt-in해도 데이터가 cache에 없으면 빈 화면). 추후 NodeTypeFilters opt-in시 lazy-load 트리거 추가하면 좋을 듯 (남은 작업 candidate).

### 4.4 nodeLimit 사용자 제어
- 사용자 노트북에서 32K 안 돎. 디폴트만 줄이면 workstation에선 underused.
- store-driven config: 같은 패턴(state + setter + localStorage + subscribe effect)으로 다른 설정도 쉽게 추가 가능. 예: 미래에 "엣지 limit", "초기 zoom level", "force tuning strength".

### 4.5 grid layout 복귀 (overlay → grid)
- react-force-graph는 부모의 geometric center 기준으로 centerForce/zoomToFit/OrbitControls.target 계산. canvas가 viewport 전체일 때 패널 뒤로 center가 밀려가는 게 사용자가 본 정확한 증상.
- grid 컬럼으로 패널이 실제 space를 차지하면 canvas-host가 unoccluded band를 자동으로 차지 → centering 자연스럽게 맞음. trade-off: 패널 toggle 시 force simulation이 새 width로 재안정화하는 시간이 필요함 (`cooldownTicks` 일부 소모).

### 4.6 OrbitControls patch 방식
- 라이브러리 자체 fix를 기다리지 않고 instance 수준에서 `_onPointerUp` wrap.
- `connect()` 인자 변경 (Three r182+)은 이번 세션에서 발견 — disconnect 전에 `ctrls.domElement` 저장해서 `connect(el)`로 재호출. 빠뜨리면 listener 전체가 detach되어 OrbitControls 무력화. **다음 세션에서 OrbitControls patch 관련 코드 수정 시 반드시 이 점 기억**.

---

## 5. 남은 작업

### 5.1 브라우저 검증 (필수) — Task #16 + #18 마무리
다음 세션에서 dev server + 백엔드 재기동 후 **반드시 사용자 브라우저로 직접** 확인.

**Expected values는 stable-net DB (210K 노드) 기준**. 다른 source repo면 절대값은 다르나 상대 동작은 동일해야 함.

| # | 항목 | 기대 결과 | 확인 방법 |
|---|---|---|---|
| 1 | 초기 boot 노드 수 (default 5K, Test OFF) | bottombar `~3,000–5,000 nodes` (top-N pagerank · 매크로 only · test 제외). stable-net은 약 4.8K 예상 | `.bottombar`의 "N nodes / M edges" 표시 |
| 2 | Test 토글 OFF → ON | 노드 수 증가 (~+1K). bottombar 카운트 갱신 + force layout cooldown 재시작 | bottombar count 변화 |
| 3 | **Test 토글 ON → OFF** ★ | 노드 수 원래대로 감소. 이게 이전 핵심 버그 | bottombar count 감소 확인 |
| 4 | nodeLimit 500 | bottombar `≤500 nodes`. cooldown 매우 빠름 (~1s) | select + bottombar |
| 5 | nodeLimit 1K | bottombar `~1000 nodes` | 동상 |
| 6 | nodeLimit 10K | bottombar `~10,000 nodes`. cooldown 길어짐 (~6s, isLargeGraph 분기) | 동상 |
| 7 | nodeLimit 변경 시 자동 refetch | select onChange → 즉시 새 boot fetch (별도 버튼 클릭 불필요) | DevTools Network 탭에서 `/api/nodes/top` 호출 확인 |
| 8 | **노드 클릭 시 visibleIds 유지** ★ | 클릭 전후 bottombar count **불변**. focus halo만 anchor 주변에 적용 | bottombar count 비교 |
| 9 | anchor 활성 시 dagMode='lr' | caller가 anchor의 왼쪽 / callee가 오른쪽으로 자동 재배치 | 시각 확인 |
| 10 | anchor 해제 (Home 또는 다른 노드 클릭) 시 layout | dagMode 해제 → 자유 force 모드로 복귀 | 시각 확인 |
| 11 | Back 버튼 | 이전 anchor + focusDistance 복원 (visibleIds는 변하지 않으므로 매끄러움) | TopBar `← Back` 클릭 후 anchor 표시 확인 |
| 12 | 패널 close/reopen | TopBar `📋 Detail ▸/◂` 토글 시 canvas-host width 즉시 조정, force-graph 캔버스가 새 width로 resize | viewport 우측 영역 시각 확인 |
| 13 | viewport resize | 브라우저 창 크기 변경 시 grid가 따라 변함. 1400×900 / 1024×768 / 800×600 모두 정상 | 창 리사이즈 |
| 14 | Hydration error 없음 | DevTools Console에 "hydrated but some attributes ... didn't match" **없음** | DevTools Console |
| 15 | OrbitControls 에러 없음 | 노드 클릭 시 `OrbitControls.onPointerUp` TypeError **없음** (이전 라운드 패치 검증) | DevTools Console |

★ 표시는 이전 세션에서 발견된 핵심 버그 — 반드시 통과해야 함.

**stale-net 외 다른 source repo**라면 노드 수 절대값은 달라지나 상대 동작 (#2/#3/#7/#8/#9/#10/#11/#12) 은 동일해야 함.

### 5.2 commit 분할 (필수)
검증 통과 시 user의 commit 스타일 (English, no co-author)로 분할 commit 권장:

```bash
# Round 1: layout
git add web/viewer-next/next.config.mjs web/viewer-next/app/globals.css \
         web/viewer-next/src/components/GraphCanvas.tsx \
         web/viewer-next/src/components/App.tsx
# 단, App.tsx와 GraphCanvas.tsx는 여러 라운드 변경이 섞여있어 한 번에 가는 게 깔끔할 수도

# 권장: 한 commit으로 묶고 풍부한 body로
git commit -m "feat(viewer): grid layout + boot-once data model + test/node limit controls

- restore grid 4-column layout (sidenav | callflow | canvas | panel)
- forward width/height props so force-graph reacts to ResizeObserver
- new data-loading model: boot loads N production non-statement nodes;
  node clicks only update anchor/focusDistance (no visibleIds replace)
- TopBar Test ON/OFF toggle (default OFF) + Node-count select
- dagMode='lr' on anchor activation; arrow length 3→6
- force tuning (charge -120, link distance 80) + cooldown scale w/ count
- skipTrailingSlashRedirect to fix dev /api proxy 308 loop
- raise backend /api/nodes/top cap 1000 → 100000
- fix CanvasLegend SSR hydration mismatch (toFixed(2) on SVG points)
- wrap OrbitControls _onPointerUp to swallow library race
- SideNav: collapsed-only (52px icon strip)"
```

또는 raw round 단위로 3-4개 commit. 사용자 취향 확인 후 진행.

### 5.3 보조 작업 (선택)

- [ ] **embedded viewer 갱신**: `web/viewer-next/out/index.html`을 `internal/server/web_assets/`로 복사하는 빌드 step이 어딘가 있을 것 (Makefile 확인 필요). production 모드 사용자에게 새 변경 반영하려면 필요.
- [ ] **lint hint 정리**: `internal/server/api.go:108`의 `Ranging over SplitSeq is more efficient` (staticcheck 권장).
- [ ] **NodeTypeFilters opt-in 시 lazy load**: 사용자가 Field/Variable/CallSite 등을 NodeTypeFilters에서 켜도 데이터가 없어 빈 화면. opt-in 시 자동 fetch 트리거 추가하면 UX 좋아짐.
- [ ] **directed flow 시각화 강화**: 현재 dagMode + arrow boost로 충분히 알아볼 수 있지만, 사용자가 더 명확한 caller/callee 색 구분 원하면 `linkColor`를 source-relative로 (incoming=blue, outgoing=orange) 추가 가능.

---

## 6. 다음 세션 시작 방법

```bash
# 0. uncommitted 변경 확인 (있어야 정상)
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-graph
git status

# 1. 백엔드 (별도 터미널 #1)
./bin/ckg serve --graph /tmp/ckg-stablenet --port 8080
# bin/ckg는 2026-05-22 18:01에 limit cap 100000으로 재빌드된 상태. 그대로 사용 가능.
# 만약 internal/server/api.go 추가 수정 시: go build -o bin/ckg ./cmd/ckg/

# 2. dev server (별도 터미널 #2)
cd web/viewer-next && npm run dev
# Next.js 15, port 3001. SSR 사용. Fast Refresh 동작.

# 3. 사용자 브라우저 직접 접속 (Playwright MCP 동시 사용 자제)
open "http://localhost:3001/?fresh=$(date +%s)"
# ?fresh 쿼리는 브라우저의 308 redirect 캐시를 회피하기 위함.

# 4. 검증 후 정리
# 작업 끝나면 둘 다 종료
lsof -ti :3001 :8080 2>/dev/null | xargs kill
```

---

## 7. 알려진 제약 / 주의 사항

| 항목 | 설명 |
|---|---|
| **브라우저 308 캐시** | `skipTrailingSlashRedirect`로 해결했으나 이전에 캐시된 308이 있으면 `ERR_TOO_MANY_REDIRECTS` 잔존. URL에 `?fresh=N` 쿼리로 회피. |
| **THREE.WARNING: Multiple instances of Three.js** | react-force-graph-3d가 자체 Three.js 번들, 우리도 `three` 직접 import. 콘솔 경고만, 동작 무영향. 정리하려면 react-force-graph-3d가 peer dep으로 받도록 변경 필요 (라이브러리 수정 필요). |
| **OrbitControls patch는 instance level** | rAF tick에서 한 번 patch. 만약 ForceGraph3D 컴포넌트가 unmount/remount되면 새 instance에 다시 patch 필요 — 현재 코드는 `viewMode`/`colorMode` 변경 시 `key` 바뀌어 remount되고 rAF effect cleanup → 재시작 → 새 instance patch. 정상. |
| **dagMode='lr' + cycle** | call graph에는 mutual recursion이 흔함. `onDagError='remove'`로 cycle 형성 edge를 level 계산에서 제외. edge 자체는 여전히 렌더링됨. |
| **nodeLimit과 cooldownTicks 임계** | `GraphCanvas.tsx`에서 `graphData.nodes.length > 8000 ? 250 : 80`. 5K 디폴트는 80 ticks (2.5s). 10K 선택 시 250 ticks (6s) 적용. |
| **edge fetch BATCH 크기** | `lib/depth.ts`에서 `BATCH = 5000`. 백엔드 `/api/edges` POST는 5000 ID 정도 안정적. 더 크면 body size 한계 우려. |
| **`internal/server/web_assets/index.html` modified 상태** | 이전 viewer 빌드 산출물. 이번 세션에서 직접 편집 아님. 만약 frontend 변경을 production viewer (embedded)에 반영하려면 `npm run build` 후 web_assets로 복사 필요 — 빌드 step 위치 미확인. |

---

## 8. Task 진행 상태

```
#10 [completed] isTestPath helper + excludeTests store state
#11 [completed] Expand initial load to 32K → (later reduced to 5K default in #17)
#12 [completed] NodeTypeFilters: statement-level default OFF
#13 [completed] TopBar: 🧪 Test ON/OFF toggle
#14 [completed] Force simulation tuning (charge -120, link 80)
#15 [completed] dagMode hierarchical + arrow boost
#16 [pending]   Verify all changes in browser ← 다음 세션 최우선
#17 [completed] Bug fix: Test toggle ON→OFF no-op + scale down to 5K
#18 [in_progress] Add user-controllable node limit ← 코드는 완료, 검증만 남음
```

---

## 9. Commit 안 됨 — 사용자 확인 필요

이번 세션 모든 변경은 uncommitted (`git status` 위 5장 참조).
검증 통과 후 5.2 항목 참조하여 commit 진행 권장.

사용자 commit 스타일 (memory `feedback_commit_message_style.md` 기준):
- English summary
- No `Co-Authored-By`
- No "Generated with [Claude Code]"
- No dev-stage jargon ("WIP", "draft" 등 회피)

---

---

## 10. 이번 세션의 사용자 의사결정 history

다음 세션 Claude가 "왜 이렇게 결정됐는지" 추론해야 할 때를 위한 의도 보존.

| # | 사용자 발화 (원문 발췌) | Claude의 응답 (요약) | 결과 |
|---|---|---|---|
| 1 | "왼쪽과 오른쪽 패널이 canvas에 오버레이로 동작함으로써, canvas의 중앙지점이 ... 중앙이 아니게되는 문제" | grid layout 복귀 제안 | Round 1 |
| 2 | "테스트 코드의 표현은 default 로 off 하고 ... 테스트 코드를 제외한 실제 동작 코드 관련한 그래프는 처음에 모두 로드해서 보여주는 방식이 한눈에 ... 복잡하게 얽혀있고 사용되고 있는 중요한 프로젝트인지 이해하기 쉬울것" | "처음에 모두 로드"의 범위 묻기 | "Field/Variable/Constant까지 포함 32K" 선택 |
| 3 | 옵션 4지선다 답변 | Spacing: A+B 둘 다 / Flow: dagMode + arrow / Test: TopBar + default 제외 / Scope: 한 번에 | Round 3 base |
| 4 | "지금 정도의 수치는 현재 노트북에서는 렌더링을 감당하지 못하네" | 32K → 5K로 다운, 더 보수적인 BOOT_EXCLUDED_TYPES (+ Field/Variable/Constant) | Round 3 끝 |
| 5 | "TopBar의 Test OFF 버튼에서 ON으로 변경하면 그래프가 다시 그려지는데, 다시 OFF 할때는 아무 변화가 없어. 코드를 검토하고 버그를 수정해야해" | ref-equality pattern bug 진단, zustand subscribe로 refactor | Round 4 (앞부분) |
| 6 | "버튼으로, 몇개의 노드를 보여줄지 선택할수 있게 해줄수 있어?? pc 의 사양마다 표현할수 있는것이 다르기때문에" | TopBar select dropdown + store nodeLimit | Round 4 (뒷부분) |
| 7 | "다른 머신, 다른 세션에서 작업을 진행하려고 하면, 완벽하게 이어서 진행할수 있도록 문서가 정리되었어??" | doc에 환경 setup + 사용자 의사결정 history + Claude 지침 보강 | 이 commit |

**다음 세션이 새 결정을 내려야 할 때 참고할 패턴**:
- "복잡한 그래프" 인상이 중요하다고 사용자가 강조 (#2) → 무조건 작은 N으로 가지 말 것
- 노트북 사양 제약 명확 (#4) → 무거운 default 피할 것
- 버그 발견 시 빠른 진단 + 근본 원인 fix 선호 (#5)
- "버튼으로" 같은 UI 패러다임 선호도 직접 표현 (#6)

---

## 11. 다음 세션 Claude를 위한 작업 지침

다른 머신의 Claude에는 이 사용자의 글로벌 룰 (`~/.claude/CLAUDE.md`)이 없을 가능성이 큼. 이 doc에 핵심을 박아둠.

### 11.1 응답 / 협업 스타일
- **응답 언어**: 사용자가 한국어로 메시지하면 한국어로 응답. 코드 식별자·경로·commit 메시지는 영어 원어.
- **Fact 분리**: 추측과 사실을 분리. 확신 라벨 (High / Mid / Low / None) 사용 권장.
- **간결성**: 헤더·표로 정리. 한국어/영어 혼용 자연스럽게.

### 11.2 코드 변경 원칙
- **Read before Edit**: 절대 경로로 Read → Edit. 추측 금지.
- **검증 분리 step**: 변경 직후 `npm run typecheck` + `npm run lint`로 검증. UI 변경이면 브라우저로 확인. 통과 못 하면 사용자에게 보고.
- **실패 상한**: 동일 오류 3회 / 수정 시도 2회 실패 / 가설이 3개 이상으로 분기 → 사용자에게 보고 후 지시 대기.
- **에러 silent swallow 금지**: `catch {}` 절대 금지. 단, 라이브러리 버그를 instance-level patch로 막는 경우 (예: 이번 세션 OrbitControls)는 의도 주석 필수.
- **불변성**: 가변 상태 변경 대신 새 객체 반환.
- **파일/함수 크기 권장**: 파일 200~400 줄 (상한 800), 함수 < 50 줄, 중첩 ≤ 3.

### 11.3 Git / Commit / Push 규칙
- **Commit 메시지**: English summary. `Co-Authored-By` 또는 "Generated with [Claude Code]" 류 attribution **포함 금지**.
- **분할 commit**: 논리적 단위로 묶되 너무 잘게 쪼개지 말 것 (이번 세션은 4 라운드를 한 commit으로 묶었음 — 향후엔 라운드별 분할도 가능).
- **HEREDOC으로 메시지 전달**: 줄바꿈 보존 위해 `git commit -m "$(cat <<'EOF' ... EOF)"`.
- **Destructive git 명령 금지**: `push --force`, `reset --hard`, `branch -D` 등은 사용자가 명시적으로 요청한 경우에만.
- **uncommitted 변경 처리**: 작업 종료 시 uncommitted가 있으면 사용자에게 commit 여부 먼저 확인. 자율 commit/폐기 금지.
- **lockfile 잔여물**: `.git/index.lock` 발견 시 active git 프로세스 확인 (`ps`) 후 안전히 제거.

### 11.4 작업 흐름 권장
- 큰 변경 전: TaskCreate로 작업 추적. in_progress → completed 정직히 갱신.
- 검증 안 끝났는데 commit 진행하지 말 것. 사용자 명시 요청 시는 OK (이번 세션처럼 doc + commit + push로 다른 세션에 넘기는 경우).
- 새 결정이 필요할 때 (옵션 여러 개) AskUserQuestion 활용. 단 4개 이하.
- 사용자가 결정한 사항은 추측으로 뒤집지 말 것. 단, 정보가 새로 들어와 분명히 잘못된 게 드러나면 사용자에게 보고 후 결정.

### 11.5 도구 사용 시 리소스 절제
- **Playwright MCP + dev server + backend 동시 띄우기 자제**. 노트북 부담 큼. 검증은 사용자 브라우저로 직접.
- Background process는 작업 끝나면 명시적으로 종료 (`lsof -ti :3001 :8080 | xargs kill`).
- 무한 sleep / 폴링 금지. `Monitor` 또는 `run_in_background` 활용.

### 11.6 이 프로젝트만의 특수 사항
- viewer는 dev mode (port 3001)와 embedded mode (port 8080) 두 경로로 동작. dev는 next.js이고 embedded는 Go binary가 `internal/server/web_assets/`의 static export를 serve. 둘이 desync될 수 있음.
- `/tmp/ckg-stablenet`는 단순 경로 약속. 사용자가 다른 source repo로 시험하려면 `--src/--out`만 바꾸면 됨.
- `react-force-graph`는 props (특히 `width`/`height`)를 mount 시점에만 잡으니 변경 추적 반드시 필요.
- `subscribeWithSelector` middleware가 store에 이미 적용되어 있음. 새 설정 추가 시 같은 패턴으로 (`state + setter with localStorage + App.tsx subscribe effect with requestId`).

---

**Last updated**: 2026-05-23 (revision 2 — environment/setup/handoff guide added per user request)
**Session model**: Claude Opus 4.7 (1M context)
**Initial commit**: `efe9db7 feat(viewer): grid layout + boot-once data model + Test/Node controls`
**This revision commit**: (will appear after next commit)
