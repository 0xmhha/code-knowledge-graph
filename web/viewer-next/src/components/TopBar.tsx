'use client';

import { useStore } from '@/store/store';
import SearchBox from './SearchBox';
import type { IAPI } from '@/lib/api';

interface Props {
  api: IAPI;
  srcInfo: string;
  onTogglePanel: () => void;
  onHome: () => void;
  onBack: () => void;
  onHelpClick: () => void;
  panelOpen: boolean;
  // canGoBack mirrors store.historyStack.length > 0 — passed in by the
  // parent so the disabled state is reactive without TopBar needing its
  // own selector subscription. Same pattern as panelOpen.
  canGoBack: boolean;
}

export default function TopBar({
  api, srcInfo, onTogglePanel, onHome, onBack, onHelpClick,
  panelOpen, canGoBack,
}: Props) {
  const viewMode = useStore(s => s.viewMode);
  const colorMode = useStore(s => s.colorMode);
  const layoutMode = useStore(s => s.layoutMode);
  const setViewMode = useStore(s => s.setViewMode);
  const setColorMode = useStore(s => s.setColorMode);
  const setLayoutMode = useStore(s => s.setLayoutMode);
  const excludeTests = useStore(s => s.excludeTests);
  const setExcludeTests = useStore(s => s.setExcludeTests);
  const nodeLimit = useStore(s => s.nodeLimit);
  const setNodeLimit = useStore(s => s.setNodeLimit);

  return (
    <div className="topbar">
      <strong>ckg</strong>
      <SearchBox api={api} />
      {/* ← Back is between SearchBox and Home. Disabled when no history
          is captured. Amber accent distinguishes it from Home (blue):
          Home resets EVERYTHING while Back unwinds the most recent
          navigation only. Keyboard: Backspace (when no input focused)
          fires the same handler. */}
      <button
        type="button"
        className="topbar-back"
        title={canGoBack
          ? 'Go back to the previous navigation (Backspace)'
          : 'No previous state — navigate the graph to populate history'}
        onClick={onBack}
        disabled={!canGoBack}
      >
        ← Back
      </button>
      {/* Home is always visible. Click resets exploration/filter state
          to its initial form (anchor/selection/search/trace/whitelist
          /isolation/community-dim) while preserving display preferences.
          Idempotent on the root view, so showing it unconditionally
          gives a stable affordance and avoids the previous
          "click → button vanishes" UX glitch. */}
      <button
        className="topbar-home"
        title="Reset to initial state (Home)"
        onClick={onHome}
      >
        🏠 Home
      </button>
      <button
        title="Toggle 2D / 3D rendering"
        onClick={() => {
          const next = viewMode === '2d' ? '3d' : '2d';
          setViewMode(next);
          try { localStorage.setItem('ckg.viewMode', next); } catch { /* ignore */ }
        }}
      >
        {viewMode === '2d' ? '2D' : '3D'}
      </button>
      <button
        title="Color by node type / language / community (key: m)"
        onClick={() => {
          const next = colorMode === 'type' ? 'lang'
            : colorMode === 'lang' ? 'community' : 'type';
          setColorMode(next);
          try { localStorage.setItem('ckg.colorMode', next); } catch { /* ignore */ }
        }}
      >
        {colorMode === 'type' ? 'TYPE' : colorMode === 'lang' ? 'LANG' : 'COMMUNITY'}
      </button>
      <button
        title="레이아웃 = 던지는 질문 — 군집(force): 무엇끼리 뭉치나 / 흐름(계층): 무엇이 무엇을 부르나 (좌→우)"
        onClick={() => {
          const next = layoutMode === 'force' ? 'dag' : 'force';
          setLayoutMode(next);
          try { localStorage.setItem('ckg.layoutMode', next); } catch { /* ignore */ }
        }}
      >
        {layoutMode === 'force' ? '군집' : '흐름→'}
      </button>
      <button
        type="button"
        className={`topbar-test-toggle${excludeTests ? '' : ' is-on'}`}
        title={excludeTests
          ? 'Test code is hidden. Click to load + show test files (_test.go / .test.ts / .spec.ts / tests/).'
          : 'Test code is shown. Click to hide and re-load production-only.'}
        onClick={() => setExcludeTests(!excludeTests)}
      >
        🧪 Test {excludeTests ? 'OFF' : 'ON'}
      </button>
      {/* Node-limit selector — lets users dial the boot seed up or
          down based on their hardware. Lower = faster cooldown +
          smoother interaction; higher = more architecture visible at
          once. Changing the value triggers a refetch via the same
          subscribe handler that watches excludeTests. */}
      <label className="topbar-node-limit" title="Max nodes to load (top by PageRank). Lower for weaker machines.">
        <span aria-hidden="true">📊</span>
        <select
          value={nodeLimit}
          onChange={(e) => setNodeLimit(parseInt(e.target.value, 10))}
          aria-label="Node count limit"
        >
          <option value={500}>500</option>
          <option value={1000}>1K</option>
          <option value={2000}>2K</option>
          <option value={5000}>5K</option>
          <option value={10000}>10K</option>
        </select>
      </label>
      <button
        type="button"
        className={`topbar-detail-toggle${panelOpen ? ' is-open' : ''}`}
        title={panelOpen ? 'Hide the right detail panel' : 'Show the right detail panel'}
        onClick={onTogglePanel}
      >
        📋 Detail {panelOpen ? '▸' : '◂'}
      </button>
      <button
        type="button"
        title="Keyboard shortcuts (?)"
        onClick={onHelpClick}
      >
        ?
      </button>
      <span className="src-info">{srcInfo}</span>
    </div>
  );
}
