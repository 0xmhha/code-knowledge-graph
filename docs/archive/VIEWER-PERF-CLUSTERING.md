# Viewer 성능·클러스터링·코드 트레이스 개선

작성일: 2026-04-27
대상: `web/viewer-next/` (마이그레이션 완료) — 이전 `web/viewer/` 는 `make viewer-old` 로 폴백 가능, 검증 후 제거 예정
상태: **Phase 0~3 shipped + Next.js migration shipped (2026-04-27)** — 아래 Status 섹션 참조
참조: `docs/ARCHITECTURE.md`,
`internal/cluster/leiden.go`, `internal/persist/sqlite.go`(topic_tree)

---

## Status (2026-04-27)

| Phase | 상태 | 시점 | 주요 커밋 |
|-------|------|------|----------|
| **Migration**: vanilla esbuild → Next.js 14 (App Router, TS) | ✓ shipped | 2026-04-27 | `46bab08` |
| **Phase 0**: store.commit() + RAF coalescer + sync 인덱스화 | ✓ shipped | 2026-04-27 | `46bab08` |
| **Phase 1**: communityColor() hash→HSL + LANG/COMMUNITY 토글 + Legend (frontend) | ✓ shipped | 2026-04-27 | `46bab08` |
| **Phase 1 (backend)**: `/api/nodes` 에 `community_id` + `topic_label` 합류 | ✓ shipped | 2026-04-27 | `74f4bcf` |
| **Phase 2**: 자동 anchor 제거 + traceFromNode (callers/callees/both, asymmetric depth, edge-type 화이트리스트) | ✓ shipped | 2026-04-27 | `46bab08` |
| **Phase 3**: Legend dim/isolate (click/shift-click) + EdgeTypeFilters | ✓ shipped | 2026-04-27 | `46bab08` |
| **M4 integration**: Makefile swap, web_assets embed, viewer-old fallback | ✓ shipped | 2026-04-27 | `dcae9b4` |
| **Polish 1**: web_assets gitignore (build 산출물 churn 제거, stub fallback) | ✓ shipped | 2026-04-27 | `cd4f8d7`, `86c197e` |
| **Polish 2**: npm audit moderate (postcss override) | ✓ shipped | 2026-04-27 | `36dbdb4` |
| **Polish 3**: 단축키 (`m v t 1-4 ? +/-/0`) + on-canvas ControlLayer + HelpOverlay | ✓ shipped | 2026-04-27 | `54108cb`, `0095a62` |

**경로 변경**: 본 문서의 4·9 절은 변경 전 `web/viewer/src/*.js` 기준으로 작성됨. 실제 구현은 `web/viewer-next/src/{components,lib,store}/*.{ts,tsx}` 에 안착. 본문은 *원본 계획·근거 문서* 로 보존하고, **현재 작업 위치는 11절(Next Directions) 참고**.

---

## 1. 문제 정의

`web/viewer` 가 다음 세 가지 증상을 동시에 보인다.

1. **2D 모드에서도 느린 인터랙션** — 클릭/검색/뎁스 변경마다 force
   simulation 이 길게 도는 느낌. visible 캡(`MAX_VISIBLE=500`) 이내인데도
   부드럽지 않음.
2. **모든 노드가 같은 색** — 언어가 같으면 색이 같음 (`go=#00add8`,
   `ts=#3178c6`, `sol=#3c3c3d`). 코드 군집(community)을 시각적으로
   구분할 단서가 없음.
3. **클러스터 단위 필터/제거 UI 없음** — 어떤 모듈을 화면에서 빼고 다른
   모듈만 보고 싶을 때 방법이 없음. depth 조절은 있지만 "이 클러스터만
   고립" / "이 클러스터를 dim" 이 불가능.

추가로, 코드 추적(code tracing) UX 가 약하다.

4. **검색 → 자동 anchor → 즉시 펼침** 흐름이라 *후보를 고르기 전에*
   페치/렌더가 시작됨. 사용자가 "결과 후보 보기 → 그중 하나 선택 →
   서브그래프 확인" 패턴을 못 함.
5. **Caller(역방향) 추적이 안 보임** — `computeFocusDistance` 가 in/out
   양방향 BFS 를 하지만 그건 halo 투명도용. "이 함수를 부르는 모든
   경로를 한 화면에" 라는 명시 모드가 없음.
6. **연속 입력 중 재렌더** — 줌/스크롤/연속 키 입력에 force-graph 가
   매번 graphData() push 를 받으면 cooldown 시뮬을 다시 돌림. "키 입력이
   끝난 시점에 한 번만 렌더" 패턴이 없음.

---

## 2. 사실 확인 (코드 레벨)

### 2.1 클러스터 데이터는 이미 백엔드에 있다

| 위치 | 내용 |
|------|------|
| `internal/cluster/leiden.go` | Leiden community detection 구현(modularity 기반) |
| `internal/persist/sqlite.go:117` | `TopicTreeInput` — 해상도별 community 트리 |
| `internal/persist/sqlite.go:255` `LoadHierarchy("topic")` | `topic_tree` 를 (parent_id, child_id, resolution, topic_label) 평면 슬라이스로 반환 |
| `internal/server/api.go` `/api/hierarchy?kind=topic` | HTTP 노출 |
| `internal/persist/chunked_export.go:49` | static export 시 `hierarchy/topic_tree.json` 작성 |

**그러나 뷰어는 이걸 한 번도 부르지 않는다.** 뷰어가 부르는
`api.hierarchy(kind='pkg')` 도 사실 사용 흔적이 거의 없음
(`store.hierarchyKind='pkg'` 만 갖고 있고 실제 필터링에는 미사용).

### 2.2 색상 결정 로직

```javascript
// web/viewer/src/encoding.js:6
const LANG_COLOR = { go: 0x00add8, ts: 0x3178c6, sol: 0x3c3c3d };
// web/viewer/src/layout.js:20
const LANG_COLOR_2D = { go: '#00add8', ts: '#3178c6', sol: '#3c3c3d' };
```

color 입력이 `n.language` 한 가지뿐. Leiden community 가 노드에 붙어
있어도 무시될 수밖에 없는 구조.

### 2.3 렌더 핫스팟 3곳

#### (a) `sync()` 의 O(|E|) edge 필터 — `layout.js:185`

```javascript
const links = store.edges.filter(
  e => visible.has(e.src) && visible.has(e.dst)
);
```

`store.edges` 는 append-only 누적 배열. visible 이 500이어도
누적된 edges 가 10K 이면 매 emit 마다 10K 회 스캔.
`edgesBySrc/edgesByDst` 인덱스가 이미 있는데 여기서 안 씀.

#### (b) emit fan-out 과잉 — `store.js:43-46`

`emit()` 한 번에 두 listener 가 동시에 깨어남:
- `layout.sync()` → `fg.graphData(...)` → force simulation 재시작 (cooldownTicks=80)
- `main.refreshList()` → 사이드바 200행 DOM 비교/재구성

`navigate()` 1회에 `recomputeVisible` 내부의 `loadNodes`/`addEdges`/
`setVisible`/`computeFocusDistance` 가 각각 emit 을 트리거할 수 있어
(batch 가 일부 흡수하지만), graphData push 가 1회로 보장되지 않음.

#### (c) force simulation 재시작 — `layout.js:158-159`

```javascript
.cooldownTicks(80)
.cooldownTime(2500);
```

graphData push 마다 80 tick 시뮬. 노드 수가 작아도 매 tick D3 force
연산이 돌고 canvas 가 80번 페인트. "scroll/zoom 도중 재렌더 금지"
요구는 force-graph 모델과 정면 충돌 — push 빈도를 *외부에서 1회로
강제* 하는 수밖에 없음.

### 2.4 검색 흐름

```javascript
// web/viewer/src/search.js:32
if (Array.isArray(results) && results.length) {
  store.batch(() => { /* 결과를 visible 에 합류 */ });
  onPick(results[0].id);   // ← 자동으로 첫 결과를 anchor 로 설정
}
```

후보 5~10개를 보여주고 *고르기 전에* 첫 결과의 depth=1 페치/렌더가
이미 진행됨. 사용자가 "둘러보고 고르고 싶다" 가 안 됨.

### 2.5 코드 추적 모드 부재

- `setAnchor(id)` 는 in/out 무관 BFS 로 depth 만큼 이웃을 모음.
- `computeFocusDistance(id, 2)` 도 in/out 같이 본다.
- "이 함수를 부르는 caller 만, 끝까지" / "이 함수가 부르는 callee
  만 depth=2 로" 같은 *방향성 트레이스* 가 없음.

`edgesBySrc`(출구) / `edgesByDst`(입구) 가 이미 분리되어 있어
방향성 BFS 자체는 5줄로 가능. UX 만 없는 상태.

---

## 3. 진단 요약 (인지 vs. 성능 분리)

| 증상 | 본질 | 우선 |
|------|------|------|
| 모두 같은 색 | **인지(perception)** — 가진 데이터를 시각화에 못 꽂음 | 중 |
| 클러스터 필터 없음 | **인지·UX** | 중 |
| 2D가 느림 | **성능** — sync O(|E|), emit fan-out, sim 재시작 | 상 |
| 검색이 후보 고르기 전에 펼쳐짐 | **UX 흐름** | 상 |
| 역방향 추적 없음 | **UX 누락** — 데이터는 있음 | 중 |
| 입력 중 재렌더 | **렌더 게이팅 미설계** | 상 |

성능과 인지를 섞어서 보면 "클러스터링 추가 ≈ 더 무거워짐" 으로 오해
하기 쉽지만, 클러스터 색은 *노드 객체에 필드 하나 더하는 것* 이라
렌더 비용은 그대로다. 두 축은 독립적으로 처리 가능.

---

## 4. 개선 설계

### Phase 0 — 렌더 commit 패턴 + sync 인덱스화 (perf)

#### 0.1 store 에 `commit(graph)` 도입

현재: 변경마다 emit → listener 가 graphData 재푸시.
개선: store 외부에서 *후보 graph* 를 다 만든 뒤 `commit` 1회 호출 →
graphData push 1회.

**시그니처 안**:
```javascript
store.commit({
  visibleIds: Set,
  focusDistance: Map<id, number>,
  reason: 'navigate'|'trace'|'search-pick'|'filter',
});
// 내부에서 해당 필드만 갈아끼우고 emit 1회
```

기존 `setVisible / computeFocusDistance` 는 내부에서 `commit` 으로
수렴시키되, 호환을 위해 thin wrapper 로 남겨도 됨.

#### 0.2 `sync()` 가 인덱스를 쓰도록 변경

`web/viewer/src/layout.js:178-188` 의 `sync()` 본문 교체:

- 현재: `store.edges.filter(...)` (O(|E_total|))
- 개선: `for id in visible → for e in edgesBySrc.get(id) → if visible.has(e.dst) push`
  (O(|V_visible| · avg_deg))

10K edges 누적, visible 500 기준 — 실측 전이라 단정은 못 하지만
**최소 한 자릿수 배 빨라질 것으로 예상** (확신도: Mid).

#### 0.3 입력 중 렌더 게이팅

force-graph 의 `graphData` push 자체를 게이트:

- 사용자가 "트레이스/검색/depth 변경" 같은 *명시적 navigation* 을 하면
  `commit` 1회 → push 1회.
- 줌/스크롤/팬 등 카메라 이벤트는 store 를 건드리지 않음 (이미 그런데
  `recomputeVisible` 호출 경로가 카메라와 분리돼 있는지 한 번 더 검증).
- 연속 navigation (예: depth-in 을 빠르게 두 번) 에는 RAF 기반 coalescer
  하나로 충분: 마지막 commit 만 다음 frame 에 push.

```javascript
// 의사코드
let pendingCommit = null;
store.commit = (graph) => {
  pendingCommit = graph;
  if (pendingCommit._scheduled) return;
  pendingCommit._scheduled = true;
  requestAnimationFrame(() => {
    apply(pendingCommit);   // visibleIds, focusDistance 갈아끼움 + emit
    pendingCommit = null;
  });
};
```

#### Phase 0 산출물

- `store.commit` 단일 진입
- `sync()` 인덱스 기반
- emit fan-out 1회 보장
- 측정: visible 500 / edges 10K 시 navigate 1회 wall-clock 기록 (현재 vs. 개선)

---

### Phase 1 — 커뮤니티 색 + legend (인지)

#### 1.1 노드에 community_id 합류

옵션:

| 안 | 위치 | 비용 | 비고 |
|----|------|------|------|
| **A. 백엔드 응답에 합류** | `internal/server/api.go` `handleNodes` 가 `topic_tree` 에서 노드 id → community_id 조회 후 응답에 추가 | 서버 메모리 캐시 1번이면 O(1) 룩업 | 권장 |
| **B. 클라이언트 합류** | 부트 시 `/api/hierarchy?kind=topic` 받아 클라에서 join | 서버 변경 0 | 중복 메모리, 처음 fetch 비용 |
| **C. 노드 테이블에 컬럼 추가** | persist 레이어 변경 | 가장 깨끗 | 마이그레이션 필요 |

**P0 안: A.** 응답 페이로드에 `community_id`(int) 와 `topic_label`(string)
2필드 추가. resolution 은 1개(예: r=1.0) 고정.

#### 1.2 색 매핑 함수

`web/viewer/src/encoding.js` 에 추가:

```javascript
// 결정 사항: 아래 3안 중 택1 (고민 포인트 #1)
function communityColor(id) {
  // (a) hash → HSL : 무한 커뮤니티, 결정적
  // (b) ColorBrewer / Tableau-20 팔레트 + freq 정렬
  // (c) topic_label 키워드 기반 의미 매핑
}
```

- 모드 토글 버튼 (top bar 또는 bottom bar): `LANG / COMMUNITY` 2단.
  localStorage 에 저장.

#### 1.3 Legend (사이드바 또는 bottom drawer)

reference `graph.html` 의 패턴 차용:
- 커뮤니티 도트 + label + 노드 수
- 클릭 → "isolate / dim / clear" 토글 (Phase 3에서 행동 연결)

---

### Phase 2 — 검색→트레이스 UX (코드 추적)

#### 2.1 검색 결과 자동 anchor 제거

`web/viewer/src/search.js:32` 의 `onPick(results[0].id)` 한 줄 제거.
검색 입력 후 사이드바에 후보가 뜨고, *클릭해야* 페치/렌더가 시작.

#### 2.2 트레이스 함수 추가

`web/viewer/src/depth.js` 에 신규:

```javascript
// 결정 사항: direction/edgeTypes 기본값 (고민 포인트 #2, #4)
async function traceFromNode(api, store, id, opts) {
  // opts: { direction: 'callers'|'callees'|'both',
  //         depth: number,
  //         edgeTypes: Set<string> }
  // 1) 시작 노드부터 방향성 BFS
  //    - 'callers' → edgesByDst 만 따라감 (역방향)
  //    - 'callees' → edgesBySrc 만 따라감 (순방향)
  //    - 'both'    → 양쪽
  // 2) 미로딩 노드들을 한 번에 prefetch
  // 3) edgeTypes 화이트리스트로 엣지 필터
  // 4) 결과 객체 {nodes, edges, focusDist} 반환 — 커밋 안 함
}
```

핵심: **렌더 commit 은 호출자(main.js)가 1회만 한다.** 트레이스 도중
중간 결과를 store 에 쓰지 않는다.

#### 2.3 트레이스 컨트롤 UI

bottom bar 또는 사이드바 상단에 3-state 토글:
```
◀ callers   ◆ both   ▶ callees     depth: [1] [2] [3]
```

기본값은 결정 포인트 #2 #3 에 따라.

#### 2.4 후보 → 트레이스 → 추가 노드 클릭

후보 클릭 = 트레이스 시작점 설정. 이후 트레이스 결과 안의 노드를
클릭하면? 두 가지 모델:

- **모델 X (replace)**: 클릭한 노드를 새 시작점으로 트레이스 재실행
- **모델 Y (drill)**: 클릭한 노드 detail 만 띄우고 그래프는 유지

reference graph.html 은 X 패턴, 현재 viewer 의 `setAnchor` 도 X 패턴
이라 일관성 있음. 유지 권장 (확신도: High).

---

### Phase 3 — 클러스터 isolate/dim 필터

#### 3.1 legend 클릭 동작

```
single click  → toggle dim (해당 community 알파 0.18)
shift-click   → isolate (그 community 만 visible)
double click  → solo + zoom-to-fit
```

#### 3.2 edge-type 필터

`EDGE_STYLE` 옆에 `edgeVisible: Set<string>` 사용자 설정.
체크박스 그룹 (사이드바 하단):
```
[x] calls       [x] invokes      [ ] uses_type
[ ] references  [x] binds_to     [x] extends/implements
```

`linkVisibility` 콜백을 이 Set 으로 분기.

---

## 5. 결정 포인트 (취향·도메인 의존)

| # | 결정 | 옵션 | 영향 |
|---|------|------|------|
| 1 | 커뮤니티 색 매핑 | (a) hash→HSL · (b) 팔레트 · (c) 라벨 의미 | encoding.js 한 함수 |
| 2 | 트레이스 기본 방향 | callers / callees / both | search 후 첫 화면 인상 |
| 3 | 트레이스 깊이 정책 | 고정값 / 슬라이더 / 비대칭 (callers 무한, callees=2) | UX 복잡도 |
| 4 | 트레이스 edge-type 화이트리스트 | calls+invokes / +binds_to / +references/uses_type | 호출 그래프 vs. 영향 그래프 |
| 5 | 렌더 commit 트리거 | 200ms 디바운스 / keyup / 명시 Render 버튼 | 반응성 vs. 안정성 |

---

## 6. 단계별 진행안과 트레이드

| 안 | 범위 | 장점 | 단점 |
|----|------|------|------|
| **A. Phase 0 single-PR** | perf only | 측정 후 다음 단계 우선순위 재판단 가능, 블로커 없음 | 클러스터·트레이스는 다음 회차 |
| **B. Phase 0+1+2 big-PR** | 결정 5건 모두 받고 일괄 | 한 번에 사용 경험 개선 | PR 큼, 결정 선행 필요 |
| **C. Phase 0 → 측정 → 1·2 우선순위 재정렬** | 단계적 | 데이터 기반 우선순위 | 일정 길어짐 |

**권장: C.** Phase 0 은 결정 무관이라 즉시 착수 가능하고, 그 결과
(현재 sync 시간) 를 가지고 1·2 의 ROI 를 계산하는 게 가장 합리적.

---

## 7. 측정 계획 (Phase 0 검수용)

`#render-meter` 가 이미 ms·nodes·edges 를 표시하므로 그 값을 기록:

| 시나리오 | 현재 | Phase 0 후 | 목표 |
|----------|------|-----------|------|
| 부트 (root, 182 packages) | ? ms | | < 200 ms |
| `setAnchor` (depth=1) | ? ms | | < 150 ms |
| `depthIn` (depth=1→2) | ? ms | | < 250 ms |
| 검색 (term=`genesis`, 결과 합류) | ? ms | | < 300 ms |
| visible 500 / edges 10K sync | ? ms | | < 50 ms |

go-stablenet 그래프 (212K nodes / 314K edges) 에서 측정.

---

## 8. 비범위 (Out of scope, 명시)

- **force-graph 라이브러리 교체** (vis-network / pixi / regl). 측정 없는
  교체는 비용만 큼. Phase 0 + 1 + 2 후에도 부족할 때 재논의.
- **Static 모드 검색 활성화 (MiniSearch)**. 별도 작업 (분리).
- **노드 테이블 community_id 컬럼 추가 마이그레이션**. Phase 1 의 A 안
  (서버 응답 합류)이 충분하면 불필요.
- **3D 모드 mesh pool 재사용**. 현재 mesh 가 noeq pull 되는 문제는 별
  성격, 별도 추적.

---

## 9. 변경 파일 예측

```
web/viewer/src/store.js          : commit() 추가, emit fan-out 정리
web/viewer/src/layout.js         : sync() 인덱스화, communityColor 분기
web/viewer/src/depth.js          : recomputeVisible 시그니처 변경, traceFromNode 추가
web/viewer/src/search.js         : 자동 anchor 제거
web/viewer/src/encoding.js       : communityColor() 추가
web/viewer/src/panel.js          : renderLegend 추가
web/viewer/src/main.js           : commit 진입점 단일화, 트레이스 컨트롤 wiring
web/viewer/index.html            : legend, edge-type 체크박스, color-mode 토글
internal/server/api.go (또는 viewer.go) : nodes 응답에 community_id/topic_label 합류
```

대략 +400 / -150 LOC 예상 (Phase 0+1+2 합계).

---

## 10. 다음 액션 (원본 — Phase 0 착수 직전)

1. 위 결정 포인트 5건 중 #1·#2·#4 답변 → Phase 1·2 코드 모양 확정
2. Phase 0 PR 분리 착수 (결정 무관)
3. Phase 0 머지 후 측정 → ROI 비교 → Phase 1·2 우선순위 재확정

> 위 원본 계획은 4-7번 커밋에서 모두 이행됨. 결정 포인트 5건의 채택 결과는 11절 참고.

---

## 11. Next Directions (2026-04-27 기준, Phase 0~3 + Migration 완료 후)

Phase 0~3 와 Next.js 마이그레이션이 모두 머지된 시점에서 *그 다음* 무엇을 할지 정리. 우선순위는 **사용자 가치 / 코드 리뷰 효용 / 위험 노출** 순.

### 11.1 채택된 결정 포인트 (5건의 결과)

| # | 결정 | 채택안 | 구현 위치 |
|---|------|--------|----------|
| 1 | 커뮤니티 색 매핑 | (a) hash→HSL (137.5° 골든앵글) | `web/viewer-next/src/lib/encoding.ts` `communityColorHex()` |
| 2 | 트레이스 기본 방향 | `both` | `web/viewer-next/src/store/store.ts` `traceDirection: 'both'` |
| 3 | 트레이스 깊이 정책 | 1~4 슬라이더 + asymmetric (callers = depth+2) | `web/viewer-next/src/lib/trace.ts` |
| 4 | edge-type 화이트리스트 기본값 | `calls / invokes / binds_to / extends / implements` | `web/viewer-next/src/lib/edges.ts` `DEFAULT_EDGE_TYPES` |
| 5 | 렌더 commit 트리거 | 200ms debounce(검색) + RAF coalescer(navigation/trace) | `web/viewer-next/src/store/store.ts` `commit()` |

추가 채택 (계획 외):
- **단축키**: `[`, `]`, `Home`, `/`, `Esc`, `m`, `v`, `t`, `1-4`, `?`, `+/=`, `-`, `0`
- **on-canvas ControlLayer** (top-right, glass) + **HelpOverlay modal** — 사이드바 패널 숨겨도 모든 컨트롤 접근 가능
- **stub `index.html`** — `make viewer` 안 돌린 상태에서도 `go build` 가 작동하는 binary 생성

### 11.2 권장 다음 작업 (우선순위순)

#### A. 실 그래프 검증 — **권장 P0 (즉시)**

**무엇을**: `make build && bin/ckg serve --db <real-graph.db>` 로 띄우고 실제 코드베이스(go-stablenet 같은 ~200K node 그래프)에서 새 viewer 의 perf·UX 를 사용자가 직접 검증.

**왜 가장 중요한가**: 모든 Phase 가 통과했지만, 검증된 건 합성 fixture(10 nodes / 9 edges) 와 `npm run build` 까지. 실 데이터에서:
- Phase 0 commit 패턴이 200K 노드 / 314K 엣지 환경에서 약속된 만큼 빠른가
- communityColor hash→HSL 이 188개 community(reference graph 기준)에서 실제로 시각적으로 구별 가능한가
- `t` 키 + 노드 클릭 = 트레이스 워크플로가 *코드 추적* 도구로서 실제로 쓸만한가

**산출물**: `docs/VIEWER-PERF-CLUSTERING.md` 의 §7 측정 표 채우기 (현재 ms 값 미입력). 미달 시 Phase 0+ 추가 최적화 결정.

**시간 예상**: 30분~2시간 (실 그래프 빌드는 이미 있음)

**위험**: 낮음. 발견된 perf 미달은 별도 PR.

#### B. 사이드바·ControlLayer DRY 정리 (Final review B1) — **권장 P1**

**무엇을**: `viewMode` / `colorMode` localStorage 쓰기가 3곳에 중복(`App.tsx:181`, `ControlLayer.tsx:40`, `TopBar.tsx:34`). Zustand 스토어의 `setViewMode` / `setColorMode` 액션이 직접 localStorage 까지 책임지도록 통합.

**왜**: 같은 키에 같은 값을 쓰는 코드가 3개 — 미래에 한 곳만 변경되면 silent drift. `coding-style.md` 의 DRY 위반.

**파일**: `web/viewer-next/src/store/store.ts` (`setViewMode`/`setColorMode` 안에 try/catch localStorage 흡수), 호출 3곳에서 localStorage 라인 제거.

**시간**: 15분. 위험 0.

#### C. `as never` / `Record<string, unknown>` 타입 캐스트 정리 — **권장 P2**

**무엇을**: `web/viewer-next/src/components/GraphCanvas.tsx` 에는 react-force-graph 타입 부재로 인한 캐스트가 다수. Polish 3 에서 일부 정리(typed `FGShim` 인터페이스 추출)했지만 `as never` 가 prop level 에 6군데 남아있음. 

옵션:
- (i) 별도 `.d.ts` 모듈을 작성해 `react-force-graph-2d`/`-3d` 의 NodeAccessor / Link 시그니처를 우리 도메인 타입에 맞게 재선언
- (ii) `react-force-graph` 의 GitHub 에 PR 보내기 (장기적으로 가장 깨끗)

**왜**: 코드 리뷰 효용. 현재는 strict TS 의 안전망이 라이브러리 경계에서 끊김. 신규 contributor 가 prop callback 시그니처를 리팩토링할 때 컴파일러 보호 못 받음.

**시간**: (i) 1~2시간, (ii) 며칠 (커뮤니티 응답 대기). (i) 부터.

#### D. 기존 `web/viewer/` retire — **권장 P2**

**무엇을**: `web/viewer/` 디렉토리 + `make viewer-old` 타겟 제거. 한 단계: `git mv web/viewer web/viewer-legacy` 로 rename 후 deprecation 표시 → 다음 사이클에 삭제. 또는 한 번에 `git rm`.

**전제 조건**: A 완료 (실 검증) 후. 그 전엔 fallback 가치 있음.

**시간**: 30분 (Makefile 정리 + `.gitignore` `/web/viewer/` 라인 제거 + git rm 또는 mv).

#### E. App.tsx 키보드 핸들러 테스트 (Final review B2) — **권장 P2**

**무엇을**: `App.tsx:147-229` (~80 LOC) 가 user-facing 의 절반인데 테스트 0. Vitest + @testing-library/react 로 smoke 테스트:
- "press 'v' → store.viewMode flips"
- "press '?' → helpOpen=true → press Escape → helpOpen=false"
- "press 'm' while focus on input → store.colorMode unchanged" (input-focus guard)

**왜**: 단축키 추가는 앞으로 자연스럽게 늘어남. 한 번 테스트 인프라 구축해두면 이후 추가는 cheap.

**파일**: `web/viewer-next/src/components/__tests__/App.test.tsx` 신규. `package.json` 에 vitest scripts 추가.

**시간**: 처음 인프라 구축 1.5~2시간 + 테스트 작성 1시간.

#### F. e2e 테스트 포팅 — **권장 P3**

**무엇을**: 기존 `web/viewer/tests/` (Playwright) 가 있다면 `web/viewer-next/` 로 이전 + 새 단축키 / ControlLayer 시나리오 추가.

**왜**: D (web/viewer 제거) 의 전제 조건. 또는 D 후에 새로 작성.

**시간**: 4~6시간 (시나리오 4~6개 기준).

#### G. 부수 정리 (낮은 우선순위, batch 가능)

| 항목 | 출처 | 시간 |
|------|------|------|
| Community decoration 음성 케이스 테스트 (sparse topic_tree → 필드 부재 검증) | Final review B3 | 30분 |
| Help overlay Escape 핸들러 중복 (App + Component) — 한쪽으로 정리 | Task 3 review M5 | 10분 |
| `_RefHack` 같은 dead export 잔재 sweep | 일반 cleanup | 30분 |
| `web_assets/` allowlist 패턴 정착 (favicon 등 추가 파일 대비) | Final review B5 | 1시간 |
| ESLint 설정 (Next.js 권장) | M1 잔여 | 30분 |
| Static 모드 검색 (MiniSearch) | 별도 작업 | 4~8시간 |

#### H. 보안 후속 — `next` major bump (별도 작업)

`npm audit` 의 high (next 14.2.x 의 5건 server-runtime CVE) 는 우리 deploy 모델(`output: 'export'` 정적 export, Go binary embed)에선 unreachable 이라 비차단. 그러나 next 16+ 으로 가면 사라짐. 단 App Router behavior 변경 영향이 있으므로 **별도 마이그레이션 PR** 로 관리. 현재 없음 = 정상 결정.

### 11.3 권장 진행 순서

```
P0:    A (실 검증, 30분~2h)
       ↓ 결과에 따라 분기
       ├── perf 충분 → B → C → D → E → F → G
       └── perf 미달 → 추가 Phase 0+ 최적화 (별도 PR)

병렬 가능: B (DRY) 와 G(부수) 는 A·C·D 와 독립
```

### 11.4 한 문장 요약

**A 만 하고 나머지는 다음 사이클** 이 가장 비용 효율적. A 결과에 따라 우선순위가 *데이터 기반* 으로 재배치됨. B·E 는 합쳐서 30~120분이라 "겸사겸사 PR" 후보. C·D·F 는 코드 리뷰 가치 vs 비용 균형으로 다음 마일스톤 분리.
