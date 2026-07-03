import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type {
  CommitGraph, GraphEdge, GraphNode, NodeId, ColorMode, ViewMode, TraceDirection,
} from '@/types';
import { DEFAULT_EDGE_TYPES, type GraphGroupSpec } from '@/lib/edges';

// Default node-type whitelist for the boot canvas. Two tiers stay OFF:
//   - Statement-level kinds (IfStmt / LoopStmt / ReturnStmt / SwitchStmt /
//     CallSite) — verbose control-flow detail; turn on for "show me the
//     control structure" views.
//   - Fine-grained-detail kinds (Parameter / LocalVariable / Import /
//     Export / Decorator / Modifier / Constructor) — usually only useful
//     when drilling into a single function.
//
// Everything else is ON by default. The 2026-05-09 graphify-comparison
// audit showed users were reading "data is missing" when the boot canvas
// hid Field / Variable / Constant / Goroutine / Channel / Mutex even
// though the graph had thousands of each. Default-on follows the
// principle: surface every node the parsers extract for the major
// languages so the data the user paid build-time for is visible without
// rummaging through filter UI. Turn off in NodeTypeFilters if a specific
// graph has too many of one kind to render.
const DEFAULT_NODE_TYPES_ON: ReadonlyArray<string> = [
  // Symbols: top-level declarations
  'Function', 'Method', 'Type', 'Struct', 'Interface', 'TypeAlias',
  'Class', 'Enum', 'Contract',
  // Members: Field/Variable/Constant intentionally excluded from the
  // default whitelist. They balloon node count ~18K on the target
  // repo without contributing to the architectural picture the boot
  // view tries to convey. Users opt them in via NodeTypeFilters;
  // backend boot seed (BOOT_EXCLUDED_TYPES in lib/depth.ts) keeps
  // them out of the initial fetch too so opt-in needs a re-load.
  // Containers: scopes and route entrypoints
  'Package', 'File', 'Endpoint', 'MessageType',
  // Concurrency primitives (Go) / VCS meta nodes
  'Goroutine', 'Channel', 'Mutex', 'Commit',
];

// HistorySnapshot captures everything a "go back" navigation needs to
// restore visually identical state. Keep this minimal — anything not
// captured here (edgeTypeWhitelist, nodeTypeWhitelist, traceDirection,
// dimmedCommunities, view/color mode) is treated as a *preference* that
// survives navigation rather than as part of the location stack. This
// matches browser back-button semantics: scroll position and selection
// come back, but global preferences don't.
export interface HistorySnapshot {
  anchorId: NodeId | null;
  depth: number;
  selectedId: NodeId | null;
  visibleRootIds: Set<NodeId>;
  dimmedNodes: Set<NodeId>;
  searchQuery: string;
  focusDistance: Map<NodeId, number>;
}

const HISTORY_MAX = 20;

interface State {
  // Read-only data caches.
  nodes: Map<NodeId, GraphNode>;
  edges: GraphEdge[];
  edgesBySrc: Map<NodeId, GraphEdge[]>;
  edgesByDst: Map<NodeId, GraphEdge[]>;

  // Render-driving state. These three are the *commit* surface — they
  // change together via store.commit() and the canvas listens to all three.
  visibleIds: Set<NodeId>;
  focusDistance: Map<NodeId, number>;
  lastCommitReason: CommitGraph['reason'];

  // visibleRootIds is the "non-search" base — i.e. what visibleIds was the
  // last time something other than a search committed (boot / navigate /
  // trace / filter). Search results are unioned ON TOP of this set, so
  // each new search REPLACES the previous search additions instead of
  // accumulating them, and clearing search reverts to this snapshot.
  // Without it, successive searches grew visibleIds past MAX_VISIBLE.
  visibleRootIds: Set<NodeId>;

  // UX state.
  selectedId: NodeId | null;
  searchResults: GraphNode[];
  searchQuery: string;
  // searchHighlightIds: live as-you-type match set (projector-style).
  // Every keystroke updates it from a client-side substring match over
  // the loaded nodes; the canvas tints these reddish WITHOUT changing
  // the view (no visibleIds commit). Cleared when the query empties.
  searchHighlightIds: Set<NodeId>;
  // layoutMode: "레이아웃 = 데이터에 던지는 질문" 전환.
  //   force — 군집 구조 (무엇끼리 뭉치나; 기본)
  //   dag   — 호출·의존 흐름 (무엇이 무엇을 부르나; 좌→우 계층)
  // anchorId 기반 dagMode(내비게이션 시 국소 DAG)와 별개의 전역 모드.
  layoutMode: 'force' | 'dag';
  anchorId: NodeId | null;
  depth: number;

  // Visual prefs (persisted in localStorage by the components that own them).
  viewMode: ViewMode;
  colorMode: ColorMode;
  fontSize: number;
  edgeTypeWhitelist: Set<string>;
  // Node-type whitelist mirrors edgeTypeWhitelist for parity with the
  // edge filter UX. Persisted via NodeTypeFilters under
  // `ckg.nodeTypeWhitelist`. GraphCanvas.nodeVisibility consults this
  // set so toggling 'Function' off hides every Function node without a
  // re-fetch (the node data stays cached, only render gates flip).
  nodeTypeWhitelist: Set<string>;
  // graphModeIsolation: when true, GraphPillStrip pill clicks REPLACE the
  // whitelist with just that group's edges (single-graph view) instead of
  // bulk-toggling the group on/off. Used to study one CKS axis (e.g. G4
  // concurrency) in isolation when the dense G1/G3 layers would otherwise
  // dominate the canvas. Persisted via localStorage in EdgeTypeFilters.
  graphModeIsolation: boolean;
  dimmedCommunities: Set<number>;
  isolatedCommunity: number | null;
  // dimmedNodes: render at low alpha but keep in the visible set. Used
  // by Impact-item clicks (NodeDetail.onImpactItemClick) to spotlight
  // the impact subgraph against the wider context instead of replacing
  // the visible set. Cleared by Home / search-clear / canvas-node click.
  dimmedNodes: Set<NodeId>;
  // historyStack: rolling LIFO of navigation snapshots. pushHistory
  // appends; popHistory removes + returns the top. Capped at HISTORY_MAX
  // so heavy explorers don't accumulate unbounded state. The TopBar
  // back button gates on `historyStack.length === 0`.
  historyStack: HistorySnapshot[];

  // First-time UX overlay. firstTimeSeen flips to true once the user
  // dismisses the overlay; persisted via localStorage so subsequent
  // visits don't show it again. App.tsx renders FirstTimeOverlay only
  // when api is ready AND firstTimeSeen is false.
  firstTimeSeen: boolean;

  // selectedIssueID: when non-null, TicketIndex auto-expands the
  // panel, opens the matching row, kicks off /api/evidence?issue_id=…,
  // and scrolls the row into view. Wired up by NodeDetail's H4 issue
  // pills so the operator can jump from "this function changed in a
  // hunk citing GH-66" → "show me GH-66's full footprint" without
  // hunting in the side panel. Cleared by TicketIndex once the
  // navigation completes (so a second click on the same pill re-fires).
  selectedIssueID: string | null;

  // Trace controls.
  traceDirection: TraceDirection;
  traceDepth: number;

  // excludeTests: when true (default), test-code nodes (file_path
  // matches isTestPath) are filtered out of the boot seed and any
  // visibility recomputation. TopBar exposes a toggle button so users
  // can flip it without re-opening the filter panel. Persisted via
  // localStorage ('ckg.excludeTests') so a user who turned tests ON
  // doesn't have to do it every session.
  //
  // Lives in store (not in App.tsx state) because both the data layer
  // (lib/depth.recomputeVisible reads it during boot) and the UI layer
  // (TopBar button reflects + writes it) need to stay in lockstep
  // without prop-drilling.
  excludeTests: boolean;

  // nodeLimit: maximum nodes the boot seed requests from the backend
  // (top-N by PageRank). Lower values run smoothly on constrained
  // hardware (laptops, low-power machines); higher values surface more
  // of the architecture at the cost of force-simulation cooldown time.
  // Range UI-bounded to [500, 10000]. Persisted via localStorage.
  nodeLimit: number;

  // Perf meter.
  lastRenderMs: number;

  // Total edge count per edge type across the WHOLE graph (not just
  // visibleIds). Boot fetches this once via /api/edges/counts. Powers
  // the EdgeFilters per-pill axis-weight badges so users can see
  // "G4 has 19 edges total" without manually toggling and counting.
  // Empty object until boot completes (renderers must tolerate that).
  edgeCountsByType: Record<string, number>;

  // ── actions ──────────────────────────────────────────────────────────
  loadNodes: (arr: GraphNode[]) => void;
  addEdges: (arr: GraphEdge[]) => number;
  edgesIncidentTo: (id: NodeId) => GraphEdge[];

  // commit() is the single render trigger. Callers build their entire
  // visible/focus state outside the store, then push it once. Multiple
  // commits inside one animation frame are coalesced into the last one
  // (RAF gating, see implementation).
  commit: (graph: CommitGraph) => void;

  setSelected: (id: NodeId | null) => void;
  setSearchResults: (rs: GraphNode[]) => void;
  setSearchQuery: (q: string) => void;
  setSearchHighlightIds: (ids: Set<NodeId>) => void;
  setLayoutMode: (m: 'force' | 'dag') => void;
  setAnchor: (id: NodeId | null, depth: number) => void;
  setViewMode: (m: ViewMode) => void;
  setColorMode: (m: ColorMode) => void;
  setFontSize: (n: number) => void;
  toggleEdgeType: (t: string) => void;
  setEdgeTypeWhitelistBulk: (edgeTypes: ReadonlyArray<string>, on: boolean) => void;
  // setEdgeTypeWhitelistOnlyGroup REPLACES the whitelist with the given
  // group's edges. Used by GraphPillStrip when graphModeIsolation is on
  // so the user can switch between G1..G6 axes without having to first
  // turn the previously-active group off.
  setEdgeTypeWhitelistOnlyGroup: (group: GraphGroupSpec) => void;
  setGraphModeIsolation: (on: boolean) => void;
  setFirstTimeSeen: (v: boolean) => void;
  setEdgeCountsByType: (m: Record<string, number>) => void;
  toggleDimCommunity: (c: number) => void;
  setIsolatedCommunity: (c: number | null) => void;
  // Node-type whitelist setters. toggleNodeType flips one type;
  // setNodeTypeWhitelistBulk turns N types on/off in a single commit
  // (used by group "all on / all off" controls).
  toggleNodeType: (t: string) => void;
  setNodeTypeWhitelistBulk: (nodeTypes: ReadonlyArray<string>, on: boolean) => void;
  // Dim-set actions. setDimmedNodes replaces the entire set in one
  // commit (callers compute the dim set themselves). clearDimmedNodes
  // is a convenience for the home / canvas-click reset paths.
  setDimmedNodes: (s: Set<NodeId>) => void;
  clearDimmedNodes: () => void;
  // History actions. pushHistory captures the current navigation slice
  // BEFORE a navigation runs (caller is responsible for ordering).
  // popHistory removes and returns the most recent snapshot, or null
  // when the stack is empty.
  pushHistory: (snap: HistorySnapshot) => void;
  popHistory: () => HistorySnapshot | null;
  setTraceDirection: (d: TraceDirection) => void;
  setTraceDepth: (n: number) => void;
  setExcludeTests: (v: boolean) => void;
  setNodeLimit: (n: number) => void;
  setLastRenderMs: (n: number) => void;
  setSelectedIssueID: (id: string | null) => void;
  // hydrateFromStorage applies persisted preferences (graphModeIsolation,
  // firstTimeSeen, nodeTypeWhitelist) onto the store. Called by App on
  // mount; the store's create() initialiser uses SSR-safe defaults so
  // build-time render and the client's first render agree on shape,
  // and only the post-hydrate values diverge — by then React has
  // committed the static markup and we can mutate state freely.
  hydrateFromStorage: () => void;
}

let pending: CommitGraph | null = null;
let raf: number | null = null;

// readGraphMode / readFirstTimeSeen / readNodeTypeWhitelist read the
// stored preference from localStorage. Used by hydrateFromStorage()
// AFTER the React tree commits — calling them inside the zustand
// create() initialiser would diverge the SSR snapshot from the
// client's first render and trigger React #418.
//
// All three default to the "neutral" branch on missing/corrupt storage:
//   - graphModeIsolation defaults OFF so first-time users see the
//     classic per-edge-type filter behaviour.
//   - firstTimeSeen defaults TRUE so returning users (the common case)
//     see no overlay flash; first-time users have no stored value, so
//     hydrateFromStorage's `=== '1'` check leaves it TRUE, then the
//     boot-time check in App.tsx flips it to FALSE if it was never set.
//     (See App's first-time gate for the full handshake.)
//   - nodeTypeWhitelist defaults to DEFAULT_NODE_TYPES_ON.
const readGraphMode = (): boolean => {
  if (typeof localStorage === 'undefined') return false;
  try { return localStorage.getItem('ckg.graphMode') === '1'; }
  catch { return false; }
};
const readFirstTimeSeen = (): boolean | null => {
  if (typeof localStorage === 'undefined') return null;
  try {
    const raw = localStorage.getItem('ckg.firstTimeSeen');
    if (raw == null) return null;
    return raw === '1';
  } catch { return null; }
};
const readNodeTypeWhitelist = (): Set<string> | null => {
  if (typeof localStorage === 'undefined') return null;
  try {
    const raw = localStorage.getItem('ckg.nodeTypeWhitelist');
    if (raw) {
      const arr = JSON.parse(raw);
      if (Array.isArray(arr)) {
        return new Set(arr.filter((x): x is string => typeof x === 'string'));
      }
    }
  } catch { /* localStorage may be blocked or stored payload corrupt */ }
  return null;
};

// Persist node-type whitelist on every change so the next session boots
// with the user's choices applied. Failures are silent — localStorage
// being blocked is a degraded but non-fatal mode.
const persistNodeTypeWhitelist = (s: Set<string>): void => {
  if (typeof localStorage === 'undefined') return;
  try { localStorage.setItem('ckg.nodeTypeWhitelist', JSON.stringify([...s])); }
  catch { /* ignore */ }
};

export const useStore = create<State>()(subscribeWithSelector((set, get) => ({
  nodes: new Map(),
  edges: [],
  edgesBySrc: new Map(),
  edgesByDst: new Map(),
  visibleIds: new Set(),
  focusDistance: new Map(),
  lastCommitReason: 'boot',
  visibleRootIds: new Set(),
  selectedId: null,
  searchResults: [],
  searchQuery: '',
  searchHighlightIds: new Set(),
  layoutMode: 'force',
  anchorId: null,
  depth: 0,
  viewMode: '3d',
  // 'type' default: real indexes are often single-language, which made
  // the old 'lang' default paint every node the same colour. Type-based
  // colour is the mode where the per-type shapes actually read.
  colorMode: 'type',
  fontSize: 1.0,
  edgeTypeWhitelist: new Set(DEFAULT_EDGE_TYPES),
  // The three slots below are SSR-safe defaults; the real values get
  // applied by hydrateFromStorage() after React commits the static
  // markup. Initialising them from localStorage at create()-time would
  // diverge build-time HTML from the client's first render and trigger
  // React #418 (text/attribute hydration mismatch).
  nodeTypeWhitelist: new Set(DEFAULT_NODE_TYPES_ON),
  graphModeIsolation: false,
  firstTimeSeen: true,
  dimmedCommunities: new Set(),
  isolatedCommunity: null,
  dimmedNodes: new Set(),
  historyStack: [],
  traceDirection: 'both',
  traceDepth: 2,
  // excludeTests default TRUE matches user intent ("test 코드를 제외하고
  // 보여지는 기능이 필요해"). Hydrated from localStorage post-mount via
  // hydrateFromStorage; SSR snapshot uses the default to keep React #418
  // safe.
  excludeTests: true,
  // nodeLimit default 5000 — notebook-friendly. Hydrated from
  // localStorage post-mount. The UI restricts user-selectable values
  // to {500, 1000, 2000, 5000, 10000}; the store accepts any positive
  // integer for forward compatibility (e.g. a future "custom" input).
  nodeLimit: 5000,
  lastRenderMs: 0,
  edgeCountsByType: {},
  selectedIssueID: null,

  loadNodes: (arr) => {
    if (!Array.isArray(arr) || arr.length === 0) return;
    const next = new Map(get().nodes);
    for (const n of arr) if (n && n.id) next.set(n.id, n);
    set({ nodes: next });
  },

  addEdges: (arr) => {
    if (!Array.isArray(arr) || arr.length === 0) return 0;
    const { edges, edgesBySrc, edgesByDst } = get();
    let added = 0;
    const nextEdges = edges;            // append in place; we replace refs at end
    const nextBySrc = edgesBySrc;
    const nextByDst = edgesByDst;
    for (const e of arr) {
      if (!e || !e.src || !e.dst) continue;
      const fromList = nextBySrc.get(e.src);
      if (fromList && fromList.some(x => x.dst === e.dst && x.type === e.type)) continue;
      nextEdges.push(e);
      added++;
      if (fromList) fromList.push(e);
      else nextBySrc.set(e.src, [e]);
      const toList = nextByDst.get(e.dst);
      if (toList) toList.push(e);
      else nextByDst.set(e.dst, [e]);
    }
    if (added) {
      // P5: trigger re-derivation by replacing the INDEX refs only.
      // `edges` is already appended in place above — the old
      // `edges.slice()` re-copied the whole array on every call, which
      // made multi-batch boots quadratic in edge count, and nothing
      // subscribes to `edges` identity (GraphCanvas derives links from
      // edgesBySrc; NodeDetail reads the maps). The shallow Map copies
      // stay: they are what notifies subscribers (NodeDetail,
      // GraphCanvas.fullData) that new edges landed.
      set({
        edges: nextEdges,
        edgesBySrc: new Map(nextBySrc),
        edgesByDst: new Map(nextByDst),
      });
    }
    return added;
  },

  edgesIncidentTo: (id) => {
    const { edgesBySrc, edgesByDst } = get();
    return (edgesBySrc.get(id) ?? []).concat(edgesByDst.get(id) ?? []);
  },

  commit: (graph) => {
    pending = graph;
    if (raf != null) return;
    raf = requestAnimationFrame(() => {
      raf = null;
      const g = pending;
      pending = null;
      if (!g) return;
      // Track the "root" view (everything except search additions) so
      // SearchBox can union onto a stable base instead of unioning onto
      // the previous union — that accumulation grew visibleIds past
      // MAX_VISIBLE on repeated queries. Filter commits also leave the
      // root snapshot alone: the user is just re-rendering.
      const patch: Partial<State> = {
        visibleIds: g.visibleIds,
        focusDistance: g.focusDistance,
        lastCommitReason: g.reason,
      };
      // 'list-pick' is also transient: clicking a Visible-Nodes list item
      // updates focusDistance (so the canvas highlights the picked node)
      // without changing the underlying view. Reverting search after a
      // list-pick should restore the trace/boot view, not the list-picked
      // single-node halo.
      if (g.reason !== 'search-pick' && g.reason !== 'filter' && g.reason !== 'list-pick') {
        patch.visibleRootIds = g.visibleIds;
      }
      set(patch);
    });
  },

  setSelected: (id) => set({ selectedId: id }),
  setSearchResults: (rs) => set({ searchResults: rs }),
  setSearchQuery: (q) => set({ searchQuery: q }),
  setSearchHighlightIds: (ids) => set({ searchHighlightIds: ids }),
  setLayoutMode: (m) => set({ layoutMode: m }),
  setAnchor: (id, depth) => set({ anchorId: id, depth }),
  setViewMode: (m) => set({ viewMode: m }),
  setColorMode: (m) => set({ colorMode: m }),
  setFontSize: (n) => set({ fontSize: n }),
  toggleEdgeType: (t) => {
    const next = new Set(get().edgeTypeWhitelist);
    if (next.has(t)) next.delete(t); else next.add(t);
    set({ edgeTypeWhitelist: next });
  },
  setEdgeTypeWhitelistBulk: (edgeTypes, on) => {
    const next = new Set(get().edgeTypeWhitelist);
    if (on) for (const t of edgeTypes) next.add(t);
    else    for (const t of edgeTypes) next.delete(t);
    set({ edgeTypeWhitelist: next });
  },
  setEdgeTypeWhitelistOnlyGroup: (group) => {
    set({ edgeTypeWhitelist: new Set(group.edges) });
  },
  setGraphModeIsolation: (on) => set({ graphModeIsolation: on }),
  setFirstTimeSeen: (v) => set({ firstTimeSeen: v }),
  setEdgeCountsByType: (m) => set({ edgeCountsByType: m }),
  toggleDimCommunity: (c) => {
    const next = new Set(get().dimmedCommunities);
    if (next.has(c)) next.delete(c); else next.add(c);
    set({ dimmedCommunities: next });
  },
  setIsolatedCommunity: (c) => set({ isolatedCommunity: c }),
  toggleNodeType: (t) => {
    const next = new Set(get().nodeTypeWhitelist);
    if (next.has(t)) next.delete(t); else next.add(t);
    persistNodeTypeWhitelist(next);
    set({ nodeTypeWhitelist: next });
  },
  setNodeTypeWhitelistBulk: (nodeTypes, on) => {
    const next = new Set(get().nodeTypeWhitelist);
    if (on) for (const t of nodeTypes) next.add(t);
    else    for (const t of nodeTypes) next.delete(t);
    persistNodeTypeWhitelist(next);
    set({ nodeTypeWhitelist: next });
  },
  setDimmedNodes: (s) => set({ dimmedNodes: s }),
  clearDimmedNodes: () => {
    // Only allocate a fresh empty Set when the current value is non-empty.
    // No-op skips the render trigger that an unconditional set() would
    // cause on every Home/clear path even when nothing is dimmed.
    if (get().dimmedNodes.size === 0) return;
    set({ dimmedNodes: new Set() });
  },
  pushHistory: (snap) => {
    const cur = get().historyStack;
    // LIFO append, drop oldest when over cap. slice() keeps the array
    // immutable from the consumer's perspective so subscribers see a new
    // reference and React reconciles correctly.
    const next = cur.length >= HISTORY_MAX
      ? cur.slice(1).concat(snap)
      : cur.concat(snap);
    set({ historyStack: next });
  },
  popHistory: () => {
    const cur = get().historyStack;
    if (cur.length === 0) return null;
    const top = cur[cur.length - 1];
    set({ historyStack: cur.slice(0, -1) });
    return top;
  },
  setTraceDirection: (d) => set({ traceDirection: d }),
  setTraceDepth: (n) => set({ traceDepth: n }),
  setExcludeTests: (v) => {
    set({ excludeTests: v });
    if (typeof localStorage !== 'undefined') {
      try { localStorage.setItem('ckg.excludeTests', v ? '1' : '0'); }
      catch { /* ignore */ }
    }
  },
  setNodeLimit: (n) => {
    // Clamp to a safe range so a stale localStorage value (or a future
    // UI bug) can't ask the backend for a million nodes. Lower bound
    // 100 keeps the graph non-trivial; upper bound 100000 matches the
    // backend's /api/nodes/top hard cap.
    const clamped = Math.max(100, Math.min(100000, Math.floor(n)));
    set({ nodeLimit: clamped });
    if (typeof localStorage !== 'undefined') {
      try { localStorage.setItem('ckg.nodeLimit', String(clamped)); }
      catch { /* ignore */ }
    }
  },
  setLastRenderMs: (n) => set({ lastRenderMs: n }),
  setSelectedIssueID: (id) => set({ selectedIssueID: id }),
  hydrateFromStorage: () => {
    const patch: Partial<State> = {};
    patch.graphModeIsolation = readGraphMode();
    const seen = readFirstTimeSeen();
    if (seen !== null) {
      // Returning user (or anyone who explicitly cleared the overlay).
      patch.firstTimeSeen = seen;
    } else {
      // First-time visitor: no stored value yet. Drop the overlay
      // flag so FirstTimeOverlay renders one frame after hydration.
      patch.firstTimeSeen = false;
    }
    const wl = readNodeTypeWhitelist();
    if (wl !== null) patch.nodeTypeWhitelist = wl;
    if (typeof localStorage !== 'undefined') {
      try {
        const raw = localStorage.getItem('ckg.excludeTests');
        // Only flip when explicitly stored. Missing key → keep default
        // (true) so first-time users get the test-free view.
        if (raw === '0') patch.excludeTests = false;
        else if (raw === '1') patch.excludeTests = true;
      } catch { /* ignore */ }
      try {
        const raw = localStorage.getItem('ckg.nodeLimit');
        if (raw) {
          const n = parseInt(raw, 10);
          if (Number.isFinite(n) && n > 0) {
            patch.nodeLimit = Math.max(100, Math.min(100000, n));
          }
        }
      } catch { /* ignore */ }
    }
    set(patch);
  },
})));

// computeFocusDistance: BFS undirected, capped. Pure function — callers
// pass it the indices they already have. Returns the map; commit() applies.
export function computeFocusDistance(
  focusId: NodeId | null,
  edgesBySrc: Map<NodeId, GraphEdge[]>,
  edgesByDst: Map<NodeId, GraphEdge[]>,
  maxDepth: number,
): Map<NodeId, number> {
  const dist = new Map<NodeId, number>();
  if (!focusId) return dist;
  dist.set(focusId, 0);
  let frontier: NodeId[] = [focusId];
  for (let d = 0; d < maxDepth; d++) {
    const next: NodeId[] = [];
    for (const id of frontier) {
      const outs = edgesBySrc.get(id) ?? [];
      const ins = edgesByDst.get(id) ?? [];
      for (const e of outs) if (!dist.has(e.dst)) { dist.set(e.dst, d + 1); next.push(e.dst); }
      for (const e of ins) if (!dist.has(e.src)) { dist.set(e.src, d + 1); next.push(e.src); }
    }
    frontier = next;
    if (!frontier.length) break;
  }
  return dist;
}
