# Viewer hydration 패턴

**목적**: Next.js `output: 'export'` static 빌드와 client hydrate 사이의 React #418 (text/attribute mismatch) 재발 방지. `fa59535` 에서 모든 알려진 surface를 fix했으나 새 컴포넌트가 같은 함정에 빠지지 않도록 패턴 문서화.

**적용 범위**: viewer-next의 모든 컴포넌트 + zustand store. localStorage / cookies / window-only API를 읽는 모든 코드.

---

## 1. 문제

Next.js static export는 build time에 HTML을 한 번 생성한다. build 시점의 `useState` initializer는 `localStorage === undefined` 분기로 fallback을 반환한다.

```ts
// ❌ ANTI-PATTERN
const [collapsed, setCollapsed] = useState<boolean>(() => {
  if (typeof localStorage === 'undefined') return true;        // SSR build → true
  return localStorage.getItem(KEY) !== '0';                    // client first render → 다른 값 가능
});
```

Client 가 hydrate할 때:
1. SSR HTML 은 `collapsed=true` 기준으로 렌더링됨 (예: `<span>▶</span>`)
2. Client first render 가 `useState` initializer 다시 실행 — 이제는 localStorage 가 정의됨
3. 사용자가 이전에 expanded로 저장(`'0'`) → `collapsed=false` → `<span>▼</span>` 렌더 시도
4. **React #418**: text content "▶" vs "▼" mismatch

---

## 2. 패턴

### 2.1 컴포넌트 — `usePersistedState` 훅

```ts
// ✅ PATTERN
import { usePersistedBool } from '@/lib/usePersistedState';

const [collapsed, setCollapsed] = usePersistedBool(STORAGE_KEY, /* default */ true);
```

훅이 보장:
- 첫 render 는 `default` 값 (SSR HTML과 동일)
- `useEffect` mount 후 localStorage 읽어 stored value 적용 (한 frame 차이)
- setter 호출 시 즉시 localStorage 동기 저장

가용 변형:
- `usePersistedBool(key, default: boolean)`
- `usePersistedNumber(key, default: number)`
- `usePersistedJSON<T>(key, default: T, validate?)`

위치: `web/viewer-next/src/lib/usePersistedState.ts`

### 2.2 store — `hydrateFromStorage` action

zustand store의 module-level state는 `useState` 와 같은 함정. SSR-safe default 로 초기화하고 App mount 시 `hydrateFromStorage()` 호출.

```ts
// store.ts
const useStore = create<State>((set) => ({
  graphModeIsolation: false,    // SSR-safe default
  firstTimeSeen: true,          // SSR-safe default
  nodeTypeWhitelist: new Set(DEFAULT_NODE_TYPES_ON),  // SSR-safe default
  hydrateFromStorage: () => {
    const patch: Partial<State> = {};
    patch.graphModeIsolation = readGraphMode();       // localStorage read
    // ... apply stored values
    set(patch);
  },
}));

// App.tsx
useEffect(() => {
  hydrateFromStorage();
}, [hydrateFromStorage]);
```

---

## 3. 트레이드오프

**1-frame default flash**: returning user는 첫 paint에서 default 값 (예: collapsed) 을 잠깐 본 후, 다음 frame에서 stored value (예: expanded) 로 flip. 16-30ms 수준.

이 flash는 의도적 — 대안인 `useState(localStorage initializer)` 는 React #418 console error + degraded React commit phase 를 유발하므로 더 나쁨.

flash 가 시각적으로 거슬리는 surface (예: 전체 화면 modal) 는 별도 처리:
- App-level hydration gate (`if (!hydrated) return null`) — 첫 paint 자체를 비움
- 또는 `dynamic(() => import(...), { ssr: false })`

---

## 4. 새 컴포넌트 작성 시 점검

```
[ ] localStorage / sessionStorage / window.matchMedia 등 client-only API 읽는가?
    └─ 예 → 다음 점검
[ ] useState initializer 안에서 직접 호출하는가?
    └─ 예 → ANTI-PATTERN. usePersistedState 훅 또는 hydrateFromStorage 사용
[ ] 쿠키 / 외부 server state 읽는가?
    └─ 예 → fetch in useEffect 패턴 + loading state
[ ] Set/Object 등 collection state 인가?
    └─ 예 → usePersistedJSON<T> + validate
```

---

## 5. 기존 적용 surface 목록 (참조)

`fa59535` 에서 마이그레이션 완료:
- `TicketIndex.tsx` — `usePersistedBool`
- `RecoveryPanel.tsx` — `usePersistedBool`
- `CanvasLegend.tsx` — `usePersistedBool` + manual hydrate (width/height)
- `NodeList.tsx` — `usePersistedBool` × 2 (pkg / nodes section)
- `App.tsx` — manual hydrate (panelHidden / panelWidth)
- `NodeTypeFilters.tsx` — Set<string> + mounted gate
- `EdgeTypeFilters.tsx` — Set<GraphID> + mounted gate
- `store.ts` — `hydrateFromStorage()` action (graphModeIsolation / firstTimeSeen / nodeTypeWhitelist)

새 컴포넌트는 이 중 가장 가까운 사례를 참고.

---

## 6. 참조

- React #418 메시지 의미: https://react.dev/errors/418
- 관련 commit: `fa59535` "fix(viewer): eliminate React #418 hydration mismatch"
- 훅 구현: `web/viewer-next/src/lib/usePersistedState.ts`
- store 패턴: `web/viewer-next/src/store/store.ts::hydrateFromStorage`
