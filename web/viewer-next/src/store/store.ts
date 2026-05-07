import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type {
  CommitGraph, GraphEdge, GraphNode, NodeId, ColorMode, ViewMode, TraceDirection,
} from '@/types';
import { DEFAULT_EDGE_TYPES, type GraphGroupSpec } from '@/lib/edges';

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
  anchorId: NodeId | null;
  depth: number;

  // Visual prefs (persisted in localStorage by the components that own them).
  viewMode: ViewMode;
  colorMode: ColorMode;
  fontSize: number;
  edgeTypeWhitelist: Set<string>;
  // graphModeIsolation: when true, GraphPillStrip pill clicks REPLACE the
  // whitelist with just that group's edges (single-graph view) instead of
  // bulk-toggling the group on/off. Used to study one CKS axis (e.g. G4
  // concurrency) in isolation when the dense G1/G3 layers would otherwise
  // dominate the canvas. Persisted via localStorage in EdgeTypeFilters.
  graphModeIsolation: boolean;
  dimmedCommunities: Set<number>;
  isolatedCommunity: number | null;

  // First-time UX overlay. firstTimeSeen flips to true once the user
  // dismisses the overlay; persisted via localStorage so subsequent
  // visits don't show it again. App.tsx renders FirstTimeOverlay only
  // when api is ready AND firstTimeSeen is false.
  firstTimeSeen: boolean;

  // Trace controls.
  traceDirection: TraceDirection;
  traceDepth: number;

  // Perf meter.
  lastRenderMs: number;

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
  toggleDimCommunity: (c: number) => void;
  setIsolatedCommunity: (c: number | null) => void;
  setTraceDirection: (d: TraceDirection) => void;
  setTraceDepth: (n: number) => void;
  setLastRenderMs: (n: number) => void;
}

let pending: CommitGraph | null = null;
let raf: number | null = null;

// Initialise the two persisted boolean flags synchronously at store
// creation so the first paint matches the user's last session. Reading
// in a useEffect (the previous approach) caused a one-frame flash where
// e.g. solo mode rendered OFF, then flipped ON. SSR safety: the
// `typeof localStorage` guard means static export (Next `output: 'export'`)
// still builds — the HTML is generated with localStorage undefined and
// the client hydrates from storage on first import.
const initGraphMode = (): boolean => {
  if (typeof localStorage === 'undefined') return false;
  try { return localStorage.getItem('ckg.graphMode') === '1'; }
  catch { return false; }
};
const initFirstTimeSeen = (): boolean => {
  if (typeof localStorage === 'undefined') return false;
  try { return localStorage.getItem('ckg.firstTimeSeen') === '1'; }
  catch { return false; }
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
  anchorId: null,
  depth: 0,
  viewMode: '3d',
  colorMode: 'lang',
  fontSize: 1.0,
  edgeTypeWhitelist: new Set(DEFAULT_EDGE_TYPES),
  graphModeIsolation: initGraphMode(),
  firstTimeSeen: initFirstTimeSeen(),
  dimmedCommunities: new Set(),
  isolatedCommunity: null,
  traceDirection: 'both',
  traceDepth: 2,
  lastRenderMs: 0,

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
      // Trigger re-derivation by replacing the top-level refs.
      set({
        edges: nextEdges.slice(),
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
  toggleDimCommunity: (c) => {
    const next = new Set(get().dimmedCommunities);
    if (next.has(c)) next.delete(c); else next.add(c);
    set({ dimmedCommunities: next });
  },
  setIsolatedCommunity: (c) => set({ isolatedCommunity: c }),
  setTraceDirection: (d) => set({ traceDirection: d }),
  setTraceDepth: (n) => set({ traceDepth: n }),
  setLastRenderMs: (n) => set({ lastRenderMs: n }),
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
