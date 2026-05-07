'use client';

import { useStore } from '@/store/store';
import SearchBox from './SearchBox';
import type { IAPI } from '@/lib/api';

interface Props {
  api: IAPI;
  srcInfo: string;
  onTogglePanel: () => void;
  onHome: () => void;
}

export default function TopBar({ api, srcInfo, onTogglePanel, onHome }: Props) {
  const viewMode = useStore(s => s.viewMode);
  const colorMode = useStore(s => s.colorMode);
  const setViewMode = useStore(s => s.setViewMode);
  const setColorMode = useStore(s => s.setColorMode);
  // Home is only meaningful when the user has navigated away from the
  // root view — either by trace-anchoring a node or by typing a search.
  // Hiding it on first paint avoids drawing attention to an action that
  // would be a no-op.
  const homeRelevant = useStore(s => s.anchorId !== null || s.searchQuery.length > 0);

  return (
    <div className="topbar">
      <strong>ckg</strong>
      <SearchBox api={api} />
      {homeRelevant && (
        <button
          className="topbar-home"
          title="Return to root view (Home)"
          onClick={onHome}
        >
          🏠 Home
        </button>
      )}
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
      <button title="Hide / show right panel" onClick={onTogglePanel}>⇆</button>
      <span className="src-info">{srcInfo}</span>
    </div>
  );
}
