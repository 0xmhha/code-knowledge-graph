'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { API, StaticAPI, detectMode } from '@/lib/api';
import type { IAPI } from '@/lib/api';
import { useStore, computeFocusDistance } from '@/store/store';
import { recomputeVisible } from '@/lib/depth';
import { traceFromNode } from '@/lib/trace';
import GraphCanvas from './GraphCanvas';
import type { GraphCanvasHandle } from './GraphCanvas';
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
  // Right-panel visibility, persisted across reloads via localStorage
  // ckg.panelOpen ∈ {'1','0'}. Default OPEN — the panel is the only way
  // to see the visible-nodes list and the per-node detail / impact
  // panel; defaulting closed left users with no visible affordance
  // beyond the bare canvas. Operators who don't want it explicitly
  // close via the 📋 Detail toggle and the choice persists.
  // Read synchronously at first render so the first paint matches the
  // user's stored preference (no hydration flash).
  const [panelHidden, setPanelHidden] = useState<boolean>(() => {
    if (typeof localStorage === 'undefined') return false;
    // Only treat the panel as hidden when the user has explicitly set
    // it to '0'. Any other value (null, '1', leftover) → open.
    return localStorage.getItem('ckg.panelOpen') === '0';
  });
  // Panel column width (px). Persisted across sessions. Bounds: 240
  // (legibility floor, matches CSS clamp min) and 800 (cap so the
  // canvas always retains a usable share). Default 360 mirrors the
  // CSS clamp max so first paint is identical to the pre-resize
  // experience.
  const [panelWidth, setPanelWidth] = useState<number>(() => {
    if (typeof localStorage === 'undefined') return 360;
    const raw = parseInt(localStorage.getItem('ckg.panelWidth') ?? '', 10);
    if (!Number.isFinite(raw)) return 360;
    return Math.min(800, Math.max(240, raw));
  });
  const panelWidthRef = useRef(panelWidth);
  panelWidthRef.current = panelWidth;
  const [helpOpen, setHelpOpen] = useState(false);

  const forceGraphRef = useRef<GraphCanvasHandle>(null);

  const setAnchor = useStore(s => s.setAnchor);
  const setSelected = useStore(s => s.setSelected);
  const commit = useStore(s => s.commit);
  const setLastRenderMs = useStore(s => s.setLastRenderMs);
  const setViewMode = useStore(s => s.setViewMode);
  const setColorMode = useStore(s => s.setColorMode);
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
        if (cm === 'lang' || cm === 'community') setColorMode(cm);
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
  }, [commit, setColorMode, setFontSize, setLastRenderMs, setViewMode]);

  const navigate = useCallback(async (mutator: () => Promise<void>) => {
    if (!api) return;
    const t0 = performance.now();
    await mutator();
    requestAnimationFrame(() => setLastRenderMs(performance.now() - t0));
  }, [api, setLastRenderMs]);

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
      // edgeTypes intentionally omitted: trace walks all incident edges
      // (V2-3). The renderer filters by edgeTypeWhitelist at draw time,
      // so a user can flip a graph pill on after the trace and instantly
      // see the relevant edges without re-fetching.
      const g = await traceFromNode(api, id, {
        direction: s.traceDirection,
        depth: s.traceDepth,
      });
      // Trace also sets the anchor so depth-in/out from here makes sense.
      setAnchor(id, s.traceDepth);
      commit(g);

      // Plan B: empty-edge auto-recover. After committing the trace, we
      // know the visible neighbours but the renderer may still hide every
      // connecting line because the dominant edge type for those nodes
      // is in a graph group the user hasn't enabled (e.g. clicking a
      // Commit node whose only edges are `changed_in` ∈ G6 Temporal,
      // off by default). Detect that case and silently turn on the
      // dominant group's edges so the trace becomes visible.
      const after = useStore.getState();
      let visibleAllowedEdges = 0;
      const typeFreq = new Map<string, number>();
      for (const src of after.visibleIds) {
        const outs = after.edgesBySrc.get(src);
        if (!outs) continue;
        for (const e of outs) {
          if (!after.visibleIds.has(e.dst)) continue;
          if (after.edgeTypeWhitelist.has(e.type)) visibleAllowedEdges++;
          typeFreq.set(e.type, (typeFreq.get(e.type) ?? 0) + 1);
        }
      }
      if (visibleAllowedEdges === 0 && typeFreq.size > 0) {
        // Pick the most frequent edge type that maps to a known group.
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
            // eslint-disable-next-line no-console
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
    const s = useStore.getState();
    if (!api || !s.anchorId) return;
    let cancelled = false;
    (async () => {
      const g = await traceFromNode(api, s.anchorId!, {
        direction: traceDirection,
        depth: traceDepth,
      });
      if (cancelled) return;
      setAnchor(s.anchorId!, traceDepth);
      commit(g);
    })();
    return () => { cancelled = true; };
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

      // Cycle colour mode.
      if (ev.key === 'm') {
        const cur = useStore.getState().colorMode;
        const next: ColorMode = cur === 'lang' ? 'community' : 'lang';
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
      const next = Math.min(800, Math.max(240, startWidth + dx));
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

  // Inline grid-template-columns is applied only when the panel is
  // open. With panelHidden, .no-panel CSS rule (1fr 0px) takes over —
  // overriding it with inline style would defeat the hide.
  const appStyle = panelHidden
    ? undefined
    : { gridTemplateColumns: `minmax(0, 1fr) ${panelWidth}px` };

  return (
    <div id="app"
         className={panelHidden ? 'no-panel' : ''}
         style={appStyle}>
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
      <div className="canvas-host">
        {apiBox && (
          <GraphCanvas
            ref={forceGraphRef}
            onNodeClick={traceAndCommit}
          />
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
      </div>
      <BottomBar
        onDepthIn={onDepthIn}
        onDepthOut={onDepthOut}
        onHome={onHome}
      />
    </div>
  );
}
