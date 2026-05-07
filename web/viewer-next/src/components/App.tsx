'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { API, StaticAPI, detectMode } from '@/lib/api';
import type { IAPI } from '@/lib/api';
import { useStore, computeFocusDistance } from '@/store/store';
import { recomputeVisible } from '@/lib/depth';
import { traceFromNode } from '@/lib/trace';
import GraphCanvas from './GraphCanvas';
import type { GraphCanvasHandle } from './GraphCanvas';
import ControlLayer from './ControlLayer';
import HelpOverlay from './HelpOverlay';
import FirstTimeOverlay from './FirstTimeOverlay';
import TopBar from './TopBar';
import BottomBar from './BottomBar';
import NodeList from './NodeList';
import NodeDetail from './NodeDetail';
import Legend from './Legend';
import EdgeTypeFilters from './EdgeTypeFilters';
import TraceControls from './TraceControls';
import type { NodeId, ViewMode, ColorMode, TraceDirection } from '@/types';

const DEPTH_MAX = 6;
const FONT_SIZES: Record<string, number> = { S: 0.85, M: 1.0, L: 1.2 };
const TRACE_DIR_CYCLE: TraceDirection[] = ['callers', 'both', 'callees'];

export default function App() {
  const [api, setApi] = useState<IAPI | null>(null);
  const [srcInfo, setSrcInfo] = useState('');
  const [stale, setStale] = useState<{ src: string; cur: string } | null>(null);
  // Persist the right-panel visibility across reloads. Defaulting to
  // closed sidesteps the brief flash users reported when the panel
  // mounted, ran its first NodeDetail render, and then immediately
  // re-rendered as the boot commit landed. With the toggle now a
  // prominent labelled button (📋 Detail) operators open it on demand
  // instead of having it always pop in. Read synchronously at first
  // render to avoid a hydration flash.
  const [panelHidden, setPanelHidden] = useState<boolean>(() => {
    if (typeof localStorage === 'undefined') return true;
    return localStorage.getItem('ckg.panelOpen') !== '1';
  });
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
    });
  }, [api, navigate, commit, setAnchor, setSelected]);

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
    setAnchor(null, 0);
    setSelected(null);
    // Clear the user's search state too — the spec for the prominent
    // Home button is "return to root view", which means dropping any
    // active query/results so the canvas isn't still tinted by hits
    // from the previous search.
    useStore.getState().setSearchQuery('');
    useStore.getState().setSearchResults([]);
    await navigate(async () => {
      const g = await recomputeVisible(api);
      commit(g);
    });
  }, [api, navigate, commit, setAnchor, setSelected]);

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
  }, [api, setSelected, commit]);

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
    onDepthIn, onDepthOut, onHome,
    setColorMode, setViewMode, setTraceDirection, setTraceDepth,
  ]);

  const apiBox = useMemo(() => api, [api]);

  return (
    <div id="app" className={panelHidden ? 'no-panel' : ''}>
      <HelpOverlay open={helpOpen} onClose={() => setHelpOpen(false)} />
      {/* FirstTimeOverlay self-gates: renders nothing once dismissed.
          Mount only after api is ready so the overlay doesn't appear
          briefly on top of an empty canvas during boot. */}
      {apiBox && <FirstTimeOverlay />}
      {stale && (
        <div className="stale-banner" style={{ gridColumn: '1 / span 2' }}>
          ⚠️ Graph built from {stale.src} but src is now at {stale.cur}. Run `ckg build` to refresh.
        </div>
      )}
      {apiBox && (
        <TopBar
          api={apiBox}
          srcInfo={srcInfo}
          panelOpen={!panelHidden}
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
        />
      )}
      <div className="canvas-host">
        {apiBox && (
          <GraphCanvas
            ref={forceGraphRef}
            onNodeClick={traceAndCommit}
          />
        )}
        <ControlLayer onHelpClick={() => setHelpOpen(true)} />
      </div>
      <div className="panel">
        <NodeList onPick={onListPick} apiReady={apiBox !== null} />
        {apiBox && <NodeDetail api={apiBox} />}
        <TraceControls />
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
