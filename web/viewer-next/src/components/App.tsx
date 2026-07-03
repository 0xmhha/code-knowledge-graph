'use client';

import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { API, StaticAPI, detectMode } from '@/lib/api';
import type { IAPI } from '@/lib/api';
import { useStore, computeFocusDistance } from '@/store/store';
import { recomputeVisible } from '@/lib/depth';
import GraphCanvas from './GraphCanvas';
import type { GraphCanvasHandle } from './GraphCanvas';
import SideNav from './SideNav';
import HelpOverlay from './HelpOverlay';
import FirstTimeOverlay from './FirstTimeOverlay';
import TopBar from './TopBar';
import BottomBar from './BottomBar';
import NodeList from './NodeList';
import NodeDetail from './NodeDetail';
import Legend from './Legend';
import EdgeTypeFilters from './EdgeTypeFilters';
import NodeTypeFilters from './NodeTypeFilters';
import TraceControls from './TraceControls';
import CanvasLegend from './CanvasLegend';
import CallFlow from './CallFlow';
import RecoveryPanel from './RecoveryPanel';
import TicketIndex from './TicketIndex';
import { DEFAULT_EDGE_TYPES, GRAPH_GROUPS, edgeToGroup } from '@/lib/edges';
import type { NodeId, ViewMode, ColorMode, TraceDirection } from '@/types';
import type { HistorySnapshot } from '@/store/store';

const DEPTH_MAX = 6;
const FONT_SIZES: Record<string, number> = { S: 0.85, M: 1.0, L: 1.2 };
const TRACE_DIR_CYCLE: TraceDirection[] = ['callers', 'both', 'callees'];

export default function App() {
  const [api, setApi] = useState<IAPI | null>(null);
  const [srcInfo, setSrcInfo] = useState('');
  const [stale, setStale] = useState<{ src: string; cur: string } | null>(null);
  // Right-panel visibility (false = open, true = hidden). Default
  // OPEN on SSR + first client render so static export HTML matches
  // the hydrated DOM (avoids React #418); a useEffect below pulls the
  // user's stored preference one frame later.
  const [panelHidden, setPanelHidden] = useState<boolean>(false);
  // Panel column width (px). Bounds: 240 floor / 800 cap. SSR-safe
  // default 360 mirrors the CSS clamp max so first paint is consistent
  // before the stored value applies.
  const [panelWidth, setPanelWidth] = useState<number>(360);
  useEffect(() => {
    if (typeof localStorage === 'undefined') return;
    try {
      // Only treat the panel as hidden when the user has explicitly
      // set it to '0'. Any other value (null, '1', leftover) → open.
      if (localStorage.getItem('ckg.panelOpen') === '0') {
        setPanelHidden(true);
      }
      const raw = parseInt(localStorage.getItem('ckg.panelWidth') ?? '', 10);
      if (Number.isFinite(raw)) {
        setPanelWidth(Math.min(800, Math.max(240, raw)));
      }
    } catch { /* ignore */ }
  }, []);
  const panelWidthRef = useRef(panelWidth);
  panelWidthRef.current = panelWidth;
  const [helpOpen, setHelpOpen] = useState(false);

  const forceGraphRef = useRef<GraphCanvasHandle>(null);
  // canvasHostRef + canvasSize: ResizeObserver on .canvas-host feeds
  // explicit width/height props into GraphCanvas → ForceGraph. Without
  // this, the canvas measured its parent only on mount; subsequent
  // browser resizes or panel-toggle column changes left the WebGL/2D
  // canvas at its original dimensions, clipping nodes near the edges.
  const canvasHostRef = useRef<HTMLDivElement>(null);
  const [canvasSize, setCanvasSize] = useState<{ w: number; h: number }>({ w: 0, h: 0 });
  // H: viewerReady — hide the canvas until ForceGraph's simulation has
  // settled (onEngineStop). Without this the user sees the camera
  // animate from its default frustum to the zoom-to-fit framing, which
  // reads as a janky zoom-out the moment they load the page. Hidden
  // canvas + opacity fade in gives a clean "graph just appeared".
  const [viewerReady, setViewerReady] = useState(false);
  // mounted: false during SSR / first render. We hold off the inline
  // grid-template-columns until after mount so the SSR HTML defers to
  // the CSS fallback (which is vw / clamp-based and adapts to the
  // actual viewport). Without this, the static export bakes the
  // 1920-default windowSize into the markup and small monitors first
  // see a layout sized for 1920 — panel right-edge falls outside the
  // viewport and its scrollbar with it.
  const [mounted, setMounted] = useState(false);
  useEffect(() => { setMounted(true); }, []);
  // windowSize: track viewport so panelWidth + nav + callflow stay
  // inside the available width.
  //
  // Lazy initializer reads window.innerWidth on the first client render
  // so the inline grid template never starts from a stale 1920 default.
  // useLayoutEffect (not useEffect) keeps the resize handler attached
  // before the browser paints, so even an immediate resize between
  // hydration and first paint is captured. Hydration safety: the inline
  // grid template stays unset until `mounted` (one tick later), so SSR
  // and the very first client render still agree on the markup.
  const [windowSize, setWindowSize] = useState<{ w: number; h: number }>(() => ({
    w: typeof window !== 'undefined' ? window.innerWidth : 1920,
    h: typeof window !== 'undefined' ? window.innerHeight : 1080,
  }));
  useLayoutEffect(() => {
    if (typeof window === 'undefined') return;
    setWindowSize({ w: window.innerWidth, h: window.innerHeight });
    const onResize = () => setWindowSize({ w: window.innerWidth, h: window.innerHeight });
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);
  useEffect(() => {
    const el = canvasHostRef.current;
    if (!el) return;
    const ro = new ResizeObserver(entries => {
      for (const ent of entries) {
        const cr = ent.contentRect;
        setCanvasSize(prev => {
          // Skip identical updates so we don't churn ForceGraph on every
          // sub-pixel sub-RO firing (e.g. devicePixelRatio changes during
          // window drag between displays).
          if (prev.w === cr.width && prev.h === cr.height) return prev;
          return { w: cr.width, h: cr.height };
        });
      }
    });
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const setAnchor = useStore(s => s.setAnchor);
  const setSelected = useStore(s => s.setSelected);
  // selectedId drives the re-center effect: when the canvas dimensions
  // change (browser resize or panel toggle) we pull the currently selected
  // node back to centre so it isn't left clipped or stuck off-screen.
  const selectedId = useStore(s => s.selectedId);
  const commit = useStore(s => s.commit);
  const setLastRenderMs = useStore(s => s.setLastRenderMs);
  const setViewMode = useStore(s => s.setViewMode);
  const setColorMode = useStore(s => s.setColorMode);
  const setLayoutMode = useStore(s => s.setLayoutMode);
  const layoutMode = useStore(s => s.layoutMode);
  const anchorIdLive = useStore(s => s.anchorId);
  const hydrateFromStorage = useStore(s => s.hydrateFromStorage);

  // Apply persisted preferences (graphModeIsolation, firstTimeSeen,
  // nodeTypeWhitelist) AFTER React hydration commits the static
  // markup. The store's initial values are SSR-safe defaults, so the
  // build-time HTML matches the first client render — only the second
  // render flips these to the user's stored values, eliminating
  // React #418 (hydration mismatch) without sacrificing persistence.
  useEffect(() => {
    hydrateFromStorage();
  }, [hydrateFromStorage]);
  const setFontSize = useStore(s => s.setFontSize);
  const setTraceDirection = useStore(s => s.setTraceDirection);
  const setTraceDepth = useStore(s => s.setTraceDepth);
  const pushHistory = useStore(s => s.pushHistory);
  const popHistory = useStore(s => s.popHistory);
  const clearDimmedNodes = useStore(s => s.clearDimmedNodes);
  const historyDepth = useStore(s => s.historyStack.length);

  // snapshotCurrent: build a HistorySnapshot from the live store. Called
  // BEFORE each navigation that should be undoable. Captured by value
  // (Set/Map copies) so the popped snapshot can't be mutated by later
  // navigation that happens to share the same reference.
  const snapshotCurrent = useCallback((): HistorySnapshot => {
    const s = useStore.getState();
    return {
      anchorId: s.anchorId,
      depth: s.depth,
      selectedId: s.selectedId,
      visibleRootIds: new Set(s.visibleRootIds),
      dimmedNodes: new Set(s.dimmedNodes),
      searchQuery: s.searchQuery,
      focusDistance: new Map(s.focusDistance),
    };
  }, []);

  // Boot: detect mode, restore prefs, fetch manifest, push initial commit.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const mode = await detectMode();
      const a: IAPI = mode === 'static' ? new StaticAPI() : new API('');
      if (cancelled) return;
      setApi(a);

      try {
        const vm = (typeof localStorage !== 'undefined' && localStorage.getItem('ckg.viewMode')) as ViewMode | null;
        if (vm === '2d' || vm === '3d') setViewMode(vm);
        const cm = (typeof localStorage !== 'undefined' && localStorage.getItem('ckg.colorMode')) as ColorMode | null;
        if (cm === 'lang' || cm === 'community' || cm === 'type') setColorMode(cm);
        const lm = typeof localStorage !== 'undefined' ? localStorage.getItem('ckg.layoutMode') : null;
        if (lm === 'force' || lm === 'dag') setLayoutMode(lm);
        const fs = typeof localStorage !== 'undefined' ? localStorage.getItem('ckg.fontSize') : null;
        if (fs && FONT_SIZES[fs]) setFontSize(FONT_SIZES[fs]);
      } catch { /* localStorage may be blocked */ }

      try {
        const m = await a.manifest();
        if (cancelled) return;
        setSrcInfo(m.src_root ?? '');
        if (m.graph_stale && m.src_commit && m.current_commit) {
          setStale({ src: m.src_commit, cur: m.current_commit });
        }
      } catch (e) { console.warn('manifest fetch failed', e); }

      // Boot-time edge count fetch — total count per edge type across the
      // whole graph (NOT visibleIds-restricted). Powers EdgeFilters axis-
      // weight badges. Fail-soft: empty object on error so the UI degrades
      // to no badges rather than crashing on older backends.
      a.edgeCounts().then(counts => {
        if (!cancelled) useStore.getState().setEdgeCountsByType(counts);
      }).catch((e) => console.warn('edgeCounts fetch failed', e));

      const t0 = performance.now();
      const g = await recomputeVisible(a);
      if (cancelled) return;
      commit(g);
      requestAnimationFrame(() => setLastRenderMs(performance.now() - t0));
    })();
    return () => { cancelled = true; };
  }, [commit, setColorMode, setFontSize, setLastRenderMs, setViewMode, setLayoutMode]);

  const navigate = useCallback(async (mutator: () => Promise<void>) => {
    if (!api) return;
    const t0 = performance.now();
    await mutator();
    requestAnimationFrame(() => setLastRenderMs(performance.now() - t0));
  }, [api, setLastRenderMs]);

  // Re-run the boot fetch when excludeTests toggles AFTER initial boot.
  // Uses zustand's `subscribe` (enabled by the subscribeWithSelector
  // middleware on the store) rather than a React-effect-with-ref dance:
  // the previous ref-based equality check could lose a transition when
  // a hydration write + a user click landed in the same tick — both
  // triggered a single effect with the latest value, and the ref
  // moved to the latest value without firing the refetch. Direct store
  // subscription fires exactly on transition and ignores the initial
  // value, so the OFF→ON / ON→OFF round-trip is symmetric.
  //
  // requestId pattern handles fast successive toggles: each fetch tags
  // itself with an incrementing id, and only the latest id is allowed
  // to commit. An in-flight fetch from a stale toggle gets dropped.
  useEffect(() => {
    if (!api) return;
    let currentRequestId = 0;
    const refetch = async () => {
      const reqId = ++currentRequestId;
      const g = await recomputeVisible(api);
      if (reqId !== currentRequestId) return;  // superseded
      commit(g);
    };
    // Two store subscriptions share the same requestId guard so a
    // user who flips excludeTests AND nodeLimit in rapid succession
    // ends up with exactly the LATEST fetch committed. Older fetches
    // resolve quietly without overwriting the user's intent.
    const unsubA = useStore.subscribe((s) => s.excludeTests, refetch);
    const unsubB = useStore.subscribe((s) => s.nodeLimit, refetch);
    return () => {
      currentRequestId = Number.POSITIVE_INFINITY;  // invalidate any in-flight
      unsubA();
      unsubB();
    };
  }, [api, commit]);

  const traceAndCommit = useCallback(async (id: NodeId) => {
    if (!api) return;
    const s = useStore.getState();
    // History push BEFORE the navigation mutates state, so ← Back can
    // restore the pre-click view. Clicking a graph node also clears the
    // dim set — the user is starting a new exploration arc and the
    // previous Impact spotlight is no longer the relevant context.
    pushHistory(snapshotCurrent());
    clearDimmedNodes();
    setSelected(id);
    await navigate(async () => {
      // New post-2026-05-22 navigation model:
      // The boot seed pre-loads the entire production node set, so node
      // clicks NO LONGER replace visibleIds. Instead we lazy-fetch any
      // missing edges around `id`, then update focusDistance so the
      // halo highlights the trace neighbourhood without removing the
      // wider context. This eliminates the "튀는" jump and makes Back
      // navigation feel continuous.
      //
      // edgeTypes intentionally omitted on the edge fetch: we want all
      // incident edges in the cache so the renderer can apply the
      // user's current edgeTypeWhitelist at draw time without re-fetching.
      const cached = useStore.getState();
      const hasEdges =
        (cached.edgesBySrc.get(id)?.length ?? 0) > 0 ||
        (cached.edgesByDst.get(id)?.length ?? 0) > 0;
      if (!hasEdges) {
        const fresh = await api.edges([id]);
        if (fresh.length) useStore.getState().addEdges(fresh);
      }
      setAnchor(id, s.traceDepth);
      // Recompute focusDistance from the current edge cache. depth=2
      // matches the existing list-pick semantics — dist 0/1/2/∞ buckets
      // drive the FOCUS / direct / 2-hop / far rings in the renderer.
      const after = useStore.getState();
      const focus = computeFocusDistance(
        id, after.edgesBySrc, after.edgesByDst,
        Math.min(s.traceDepth, 2),
      );
      commit({
        visibleIds: after.visibleIds,  // unchanged → no jump
        focusDistance: focus,
        reason: 'navigate',
      });
      forceGraphRef.current?.centerOnNode(id);

      // Empty-edge auto-recover: same idea as before, but checks the
      // local neighbourhood (visible nodes whose edge endpoint is the
      // anchor or in the focusDistance ball) instead of the full visible
      // set. Keeps the auto-enable scoped to the trace context.
      let visibleAllowedEdges = 0;
      const typeFreq = new Map<string, number>();
      for (const src of focus.keys()) {
        const outs = after.edgesBySrc.get(src);
        if (!outs) continue;
        for (const e of outs) {
          if (!after.visibleIds.has(e.dst)) continue;
          if (!focus.has(e.dst)) continue;
          if (after.edgeTypeWhitelist.has(e.type)) visibleAllowedEdges++;
          typeFreq.set(e.type, (typeFreq.get(e.type) ?? 0) + 1);
        }
      }
      if (visibleAllowedEdges === 0 && typeFreq.size > 0) {
        let dominant: string | null = null;
        let dominantCount = 0;
        for (const [t, c] of typeFreq) {
          if (c > dominantCount && edgeToGroup(t) !== null) {
            dominant = t;
            dominantCount = c;
          }
        }
        if (dominant) {
          const groupId = edgeToGroup(dominant);
          const group = GRAPH_GROUPS.find(g => g.id === groupId);
          if (group) {
            useStore.getState().setEdgeTypeWhitelistBulk(group.edges, true);
            console.info(
              `[ckg] auto-enabled ${group.id} ${group.label} ` +
              `(${dominantCount} edges) — trace had hidden edges only.`,
            );
          }
        }
      }
    });
  }, [api, navigate, commit, setAnchor, setSelected, pushHistory, snapshotCurrent, clearDimmedNodes]);

  // Re-trace when traceDirection / traceDepth change while an anchor is
  // active. Without this effect, the TraceControls buttons updated the
  // store but the canvas only reflected the change at the next
  // node-click — users perceived the controls as inert. Now flipping
  // direction or depth on an anchored view immediately re-runs trace
  // BFS and commits the new visible set.
  const traceDirection = useStore(s => s.traceDirection);
  const traceDepth = useStore(s => s.traceDepth);
  useEffect(() => {
    if (!api) return;
    const s = useStore.getState();
    if (!s.anchorId) return;
    // Re-derive focusDistance only (visibleIds stays at the boot seed
    // so the wider context survives trace-control changes too). The
    // direction is currently advisory — focusDistance is undirected
    // BFS; turning it into a directed read here is future work.
    const focus = computeFocusDistance(
      s.anchorId, s.edgesBySrc, s.edgesByDst, Math.min(traceDepth, 2),
    );
    setAnchor(s.anchorId, traceDepth);
    commit({
      visibleIds: s.visibleIds,
      focusDistance: focus,
      reason: 'navigate',
    });
  }, [api, traceDirection, traceDepth, commit, setAnchor]);

  const onDepthIn = useCallback(async () => {
    if (!api) return;
    const s = useStore.getState();
    if (!s.anchorId || s.depth >= DEPTH_MAX) return;
    setAnchor(s.anchorId, s.depth + 1);
    await navigate(async () => {
      const g = await recomputeVisible(api);
      commit(g);
    });
  }, [api, navigate, commit, setAnchor]);

  const onDepthOut = useCallback(async () => {
    if (!api) return;
    const s = useStore.getState();
    if (!s.anchorId) return;
    if (s.depth <= 0) {
      setAnchor(null, 0);
      setSelected(null);
    } else {
      setAnchor(s.anchorId, s.depth - 1);
    }
    await navigate(async () => {
      const g = await recomputeVisible(api);
      commit(g);
    });
  }, [api, navigate, commit, setAnchor, setSelected]);

  const onHome = useCallback(async () => {
    if (!api) return;
    // Push the current navigation slice so ← Back can return to it.
    // Home is one of the most aggressive resets in the UI; without
    // history capture the user has no recourse if they hit it by
    // accident on a deep exploration.
    pushHistory(snapshotCurrent());
    // Home = "reset to initial state". Wipe exploration + filter state
    // (anchor, selection, search, trace settings, edge-type whitelist,
    // graph-isolation, community dim/isolate, dimmedNodes) but preserve
    // the user's display preferences (viewMode, colorMode, fontSize,
    // panel open state, edgeFiltersCollapsed) and one-shot flags
    // (firstTimeSeen). Zustand setState merges partials atomically so
    // subscribers re-render once instead of N times across individual
    // setters.
    useStore.setState({
      anchorId: null,
      depth: 0,
      selectedId: null,
      searchQuery: '',
      searchResults: [],
      edgeTypeWhitelist: new Set(DEFAULT_EDGE_TYPES),
      graphModeIsolation: false,
      dimmedCommunities: new Set<number>(),
      isolatedCommunity: null,
      dimmedNodes: new Set<NodeId>(),
      traceDirection: 'both',
      traceDepth: 2,
    });
    await navigate(async () => {
      const g = await recomputeVisible(api);
      commit(g);
    });
  }, [api, navigate, commit, pushHistory, snapshotCurrent]);

  // Sidebar list pick — keep the anchor + visible set, but make the
  // canvas highlight the picked node so the user can actually see what
  // they selected.
  //
  // Three steps in order:
  //   1. Update detail panel via setSelected (cheap, immediate).
  //   2. Lazy-fetch edges for the picked node so computeFocusDistance
  //      can BFS its 1-hop / 2-hop neighbours within the visible set.
  //      Skip the fetch when the edges are already cached.
  //   3. Commit a 'list-pick' graph with the SAME visibleIds and a
  //      fresh focusDistance map. The store excludes 'list-pick' from
  //      visibleRootIds updates, so a subsequent search-clear still
  //      reverts to the trace/boot view that was active before this
  //      pick — not to the single-node halo state.
  //   4. Tell GraphCanvas to centerOnNode so an off-screen pick is
  //      pulled into view; the focus halo alone wouldn't help if the
  //      picked dot is far outside the camera frustum.
  const onListPick = useCallback(async (id: NodeId) => {
    // List-pick is undoable too: capture the pre-click slice so ← Back
    // returns to whatever was selected (or root view) before this pick.
    pushHistory(snapshotCurrent());
    setSelected(id);
    if (!api) return;
    const s = useStore.getState();
    if (!s.edgesBySrc.has(id) && !s.edgesByDst.has(id)) {
      const fresh = await api.edges([id]);
      if (fresh.length) s.addEdges(fresh);
    }
    const after = useStore.getState();
    const focus = computeFocusDistance(id, after.edgesBySrc, after.edgesByDst, 2);
    commit({ visibleIds: after.visibleIds, focusDistance: focus, reason: 'list-pick' });
    forceGraphRef.current?.centerOnNode(id);
  }, [api, setSelected, commit, pushHistory, snapshotCurrent]);

  // onBack: pop the last history snapshot and apply it. Disabled when
  // the stack is empty (TopBar gates on historyStack.length). The
  // navigate() wrapper ensures the bottom-bar render-time meter
  // updates so users see latency feedback on Back too.
  const onBack = useCallback(async () => {
    const snap = popHistory();
    if (!snap) return;
    if (!api) return;
    await navigate(async () => {
      // Restore the captured slice atomically. We deliberately do NOT
      // restore searchResults here — search results were a transient
      // derived view; restoring them without re-querying would surface
      // stale GraphNode entries. The searchQuery comes back so users
      // can re-fire the search if they want.
      useStore.setState({
        anchorId: snap.anchorId,
        depth: snap.depth,
        selectedId: snap.selectedId,
        visibleIds: new Set(snap.visibleRootIds),
        visibleRootIds: new Set(snap.visibleRootIds),
        focusDistance: new Map(snap.focusDistance),
        dimmedNodes: new Set(snap.dimmedNodes),
        searchQuery: snap.searchQuery,
        searchResults: [],
        lastCommitReason: 'navigate',
      });
    });
  }, [api, navigate, popHistory]);

  // Keyboard shortcuts.
  useEffect(() => {
    const handler = (ev: KeyboardEvent) => {
      // Skip when typing into any text-input surface — INPUT today, but
      // also TEXTAREA and contenteditable so future UI (notes, comments)
      // can't get its keys hijacked.
      const ae = document.activeElement as HTMLElement | null;
      if (ae && (ae.tagName === 'INPUT' || ae.tagName === 'TEXTAREA' || ae.isContentEditable)) return;

      // Escape: close help overlay first; otherwise SearchBox's handler runs.
      if (ev.key === 'Escape') {
        if (helpOpen) {
          setHelpOpen(false);
          ev.preventDefault();
        }
        return;
      }

      // Help overlay toggle.
      if (ev.key === '?') {
        setHelpOpen(h => !h);
        ev.preventDefault();
        return;
      }

      // Existing depth / home shortcuts.
      if (ev.key === ']') { onDepthIn(); return; }
      if (ev.key === '[') { onDepthOut(); return; }
      if (ev.key === 'Home') { onHome(); return; }

      // Back navigation. Backspace is the natural fit (matches browser
      // behaviour) and we already early-returned on input focus above
      // so it can't intercept text editing. preventDefault() keeps
      // some browsers from also navigating the embedding window's
      // history stack on top of our own pop.
      if (ev.key === 'Backspace') {
        ev.preventDefault();
        onBack();
        return;
      }

      // Cycle colour mode: type → lang → community → type.
      if (ev.key === 'm') {
        const cur = useStore.getState().colorMode;
        const next: ColorMode =
          cur === 'type' ? 'lang' : cur === 'lang' ? 'community' : 'type';
        setColorMode(next);
        try { localStorage.setItem('ckg.colorMode', next); } catch { /* ignore */ }
        return;
      }

      // Cycle view mode.
      if (ev.key === 'v') {
        const cur = useStore.getState().viewMode;
        const next: ViewMode = cur === '2d' ? '3d' : '2d';
        setViewMode(next);
        try { localStorage.setItem('ckg.viewMode', next); } catch { /* ignore */ }
        return;
      }

      // Cycle trace direction: callers → both → callees → callers.
      if (ev.key === 't') {
        const cur = useStore.getState().traceDirection;
        const idx = TRACE_DIR_CYCLE.indexOf(cur);
        const next = TRACE_DIR_CYCLE[(idx + 1) % TRACE_DIR_CYCLE.length];
        setTraceDirection(next);
        return;
      }

      // Trace depth 1–4.
      if (ev.key === '1') { setTraceDepth(1); return; }
      if (ev.key === '2') { setTraceDepth(2); return; }
      if (ev.key === '3') { setTraceDepth(3); return; }
      if (ev.key === '4') { setTraceDepth(4); return; }

      // Zoom shortcuts.
      if (ev.key === '+' || ev.key === '=') {
        forceGraphRef.current?.zoomIn();
        return;
      }
      if (ev.key === '-') {
        forceGraphRef.current?.zoomOut();
        return;
      }
      if (ev.key === '0') {
        forceGraphRef.current?.zoomReset();
        return;
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [
    helpOpen,
    onDepthIn, onDepthOut, onHome, onBack,
    setColorMode, setViewMode, setTraceDirection, setTraceDepth,
  ]);

  const apiBox = useMemo(() => api, [api]);

  // Panel resize drag handler. Mouse-down on .panel-resizer captures the
  // pointer; mousemove updates panelWidth in [240, 800] (matching the
  // CSS clamp bounds); mouseup persists the final value to localStorage.
  // useRef keeps the closure reading the live width without re-binding
  // listeners on every state update.
  const onResizeStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    const startX = e.clientX;
    const startWidth = panelWidthRef.current;
    const onMove = (ev: MouseEvent) => {
      // Panel is on the right; dragging the handle leftward grows it.
      const dx = startX - ev.clientX;
      // Compute the viewport-aware cap on every move so window resize
      // mid-drag is respected. Reserve = nav(52, always collapsed) +
      // callflow(260 when anchor active) + canvas_min(500). Panel
      // cannot exceed viewport - reserve.
      const navW = 52;
      const callflowW = useStore.getState().anchorId ? 260 : 0;
      const liveMax = Math.max(240, window.innerWidth - navW - callflowW - 500);
      const next = Math.min(liveMax, Math.max(240, startWidth + dx));
      setPanelWidth(next);
    };
    const onUp = () => {
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
      try {
        localStorage.setItem('ckg.panelWidth', String(panelWidthRef.current));
      } catch { /* ignore */ }
    };
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
  }, []);

  // F: when there's no selection, frame the whole graph to the canvas
  // on every resize. Without this the camera keeps its old position
  // even after the canvas-host column has shrunk or grown — nodes drift
  // off-centre or vanish off the right edge. selectedId ≠ null skips
  // this effect because the centerOnNode path below already pulls the
  // camera to the chosen node, which we want to win over a global fit.
  useEffect(() => {
    if (canvasSize.w === 0 || canvasSize.h === 0) return;
    if (selectedId) return;
    const t = setTimeout(() => forceGraphRef.current?.zoomReset(), 120);
    return () => clearTimeout(t);
  }, [canvasSize.w, canvasSize.h, panelHidden, selectedId]);

  // R1-c + B1: re-center the camera on the selected node whenever the
  // canvas resizes (browser resize or panel toggle changes the column
  // width) OR the panel toggles. ForceGraph itself doesn't re-frame on
  // resize, so without this the selected node can drift off-centre or
  // fall outside the visible frustum after a layout shift.
  //
  // Two timers — B1 fix: on a fresh trace commit the force simulation
  // hasn't filled the new node's x/y yet, so centerOnNode hits its own
  // "no coords" guard and the camera stays put. Fire once early (80ms)
  // for resize/panel cases where positions are already settled, and
  // once after the simulation has had a chance to place the new neighbours
  // (700ms — cooldownTicks=80 at ~16ms per tick lands around there).
  useEffect(() => {
    if (!selectedId) return;
    const t1 = setTimeout(() => forceGraphRef.current?.centerOnNode(selectedId), 80);
    const t2 = setTimeout(() => forceGraphRef.current?.centerOnNode(selectedId), 700);
    return () => { clearTimeout(t1); clearTimeout(t2); };
  }, [selectedId, canvasSize.w, canvasSize.h, panelHidden]);

  // anchorId drives the .has-callflow class so the grid expands col 1
  // only when the CallFlow component has something to render. Reading
  // it via the store keeps the grid in lockstep with the trace state
  // without prop drilling.
  const anchorId = useStore(s => s.anchorId);
  const hasCallflow = anchorId !== null;

  // 4-column grid layout — each side surface (SideNav, CallFlow,
  // Panel) takes a real column so the canvas-host cell occupies
  // exactly the unoccluded middle band. That way the canvas's
  // geometric centre lines up with the visible centre and
  // force-graph's centering/zoomToFit framing don't bias into the
  // area the user can't see.
  //
  // effectivePanelWidth caps at half the viewport so an absurd
  // stored value (saved from a wide monitor) can't hog more than 50%
  // when the panel reopens on a small monitor.
  const SIDENAV_W = 52;
  const CALLFLOW_W = 'clamp(220px, 22vw, 300px)';
  const effectivePanelWidth = Math.min(panelWidth, Math.max(240, Math.floor(windowSize.w * 0.5)));
  // Each entry maps 1:1 to col 1/2/3/4 in globals.css. Setting a col
  // to 0px collapses it without unmounting its children — panel state
  // and editor focus survive a close/reopen toggle.
  const gridTemplateColumns = [
    `${SIDENAV_W}px`,
    hasCallflow ? CALLFLOW_W : '0px',
    'minmax(0, 1fr)',
    panelHidden ? '0px' : `${effectivePanelWidth}px`,
  ].join(' ');
  // Hold the inline template back until after hydration so SSR HTML
  // matches the first client render (CSS holds the same SSR-safe
  // default `52px 0 minmax(0, 1fr) 0` — see globals.css).
  const appStyle: React.CSSProperties | undefined = mounted
    ? { gridTemplateColumns }
    : undefined;

  return (
    <div id="app" style={appStyle}>
      <HelpOverlay open={helpOpen} onClose={() => setHelpOpen(false)} />
      {/* FirstTimeOverlay self-gates: renders nothing once dismissed.
          Mount only after api is ready so the overlay doesn't appear
          briefly on top of an empty canvas during boot. */}
      {apiBox && <FirstTimeOverlay />}
      {stale && (
        <div className="stale-banner">
          ⚠️ Graph built from {stale.src} but src is now at {stale.cur}. Run `ckg build` to refresh.
        </div>
      )}
      {apiBox && (
        <TopBar
          api={apiBox}
          srcInfo={srcInfo}
          panelOpen={!panelHidden}
          canGoBack={historyDepth > 0}
          onTogglePanel={() => {
            setPanelHidden(p => {
              const nextHidden = !p;
              try {
                localStorage.setItem('ckg.panelOpen', nextHidden ? '0' : '1');
              } catch { /* ignore */ }
              return nextHidden;
            });
          }}
          onHome={onHome}
          onBack={onBack}
          onHelpClick={() => setHelpOpen(true)}
        />
      )}
      <SideNav />
      {hasCallflow && <CallFlow onPick={onListPick} />}
      <div className={`canvas-host${viewerReady ? ' viewer-ready' : ''}`} ref={canvasHostRef}>
        {apiBox && (
          <GraphCanvas
            ref={forceGraphRef}
            // width/height come from the ResizeObserver on .canvas-host
            // (see canvasSize state above). force-graph defaults to
            // window.innerWidth/Height at construction and never reacts
            // to parent resize — so the canvas would freeze at the
            // viewport size of the very first frame and overflow its
            // grid cell. Forwarding the live container dimensions keeps
            // it in sync with browser resize, panel toggles, and any
            // future grid-template changes.
            width={canvasSize.w}
            height={canvasSize.h}
            onNodeClick={traceAndCommit}
            onEngineStop={() => {
              // Idempotent: only the first stop transitions the canvas
              // from invisible → fading-in. Subsequent simulation stops
              // (after user clicks adjust the visible set) leave it on.
              if (!viewerReady) setViewerReady(true);
            }}
          />
        )}
        {/* CanvasLegend mounts after the canvas so it overlays correctly
            in the natural DOM order. Self-gates open/closed via
            localStorage; renders nothing structural when api is still
            booting (cheap to mount unconditionally). */}
        <CanvasLegend />
        {/* 계층(흐름) 레이아웃 축 라벨 — "레이아웃 = 던지는 질문"을 화면에
            명시한다. 전역 흐름 모드 또는 앵커 내비게이션(국소 DAG) 시 표시. */}
        {(layoutMode === 'dag' || anchorIdLive != null) && (
          <div className="dag-axis-hint">
            → 호출·의존 방향 — 왼쪽: 진입점·상위 호출자 / 오른쪽: 말단
            <span className="dag-axis-sub"> (엣지 필터로 calls만 남기면 순수 호출 계층)</span>
          </div>
        )}
      </div>
      <div className="panel">
        {/* Resize handle on the panel's left edge. Hover paints a
            cyan strip; drag adjusts the column width. Hidden when the
            panel itself is hidden (parent display:none cascades). */}
        <div
          className="panel-resizer"
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize panel column"
          onMouseDown={onResizeStart}
          title="Drag to resize panel"
        />
        <NodeList onPick={onListPick} apiReady={apiBox !== null} />
        {apiBox && <NodeDetail api={apiBox} />}
        <TraceControls />
        <NodeTypeFilters />
        <EdgeTypeFilters />
        <Legend />
        <TicketIndex api={apiBox} />
        <RecoveryPanel api={apiBox} />
      </div>
      <BottomBar
        onDepthIn={onDepthIn}
        onDepthOut={onDepthOut}
        onHome={onHome}
      />
    </div>
  );
}
