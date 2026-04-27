'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { API, StaticAPI, detectMode } from '@/lib/api';
import type { IAPI } from '@/lib/api';
import { useStore } from '@/store/store';
import { recomputeVisible } from '@/lib/depth';
import { traceFromNode } from '@/lib/trace';
import GraphCanvas from './GraphCanvas';
import type { GraphCanvasHandle } from './GraphCanvas';
import ControlLayer from './ControlLayer';
import HelpOverlay from './HelpOverlay';
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
  const [panelHidden, setPanelHidden] = useState(false);
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
      const g = await traceFromNode(api, id, {
        direction: s.traceDirection,
        depth: s.traceDepth,
        edgeTypes: s.edgeTypeWhitelist,
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
    await navigate(async () => {
      const g = await recomputeVisible(api);
      commit(g);
    });
  }, [api, navigate, commit, setAnchor, setSelected]);

  // Sidebar list pick — inspect without navigating (preserves anchor).
  const onListPick = useCallback((id: NodeId) => {
    setSelected(id);
  }, [setSelected]);

  // Keyboard shortcuts.
  useEffect(() => {
    const handler = (ev: KeyboardEvent) => {
      if (document.activeElement?.tagName === 'INPUT') return;

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
      {stale && (
        <div className="stale-banner" style={{ gridColumn: '1 / span 2' }}>
          ⚠️ Graph built from {stale.src} but src is now at {stale.cur}. Run `ckg build` to refresh.
        </div>
      )}
      {apiBox && (
        <TopBar
          api={apiBox}
          srcInfo={srcInfo}
          onTogglePanel={() => setPanelHidden(p => !p)}
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
        <NodeList onPick={onListPick} />
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
