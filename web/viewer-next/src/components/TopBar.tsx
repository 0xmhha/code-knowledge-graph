'use client';

import { useStore } from '@/store/store';
import SearchBox from './SearchBox';
import type { IAPI } from '@/lib/api';

interface Props {
  api: IAPI;
  srcInfo: string;
  onTogglePanel: () => void;
  onHome: () => void;
  onHelpClick: () => void;
  panelOpen: boolean;
}

export default function TopBar({ api, srcInfo, onTogglePanel, onHome, onHelpClick, panelOpen }: Props) {
  const viewMode = useStore(s => s.viewMode);
  const colorMode = useStore(s => s.colorMode);
  const setViewMode = useStore(s => s.setViewMode);
  const setColorMode = useStore(s => s.setColorMode);

  return (
    <div className="topbar">
      <strong>ckg</strong>
      <SearchBox api={api} />
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
        title="Color by language vs community"
        onClick={() => {
          const next = colorMode === 'lang' ? 'community' : 'lang';
          setColorMode(next);
          try { localStorage.setItem('ckg.colorMode', next); } catch { /* ignore */ }
        }}
      >
        {colorMode === 'lang' ? 'LANG' : 'COMMUNITY'}
      </button>
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
