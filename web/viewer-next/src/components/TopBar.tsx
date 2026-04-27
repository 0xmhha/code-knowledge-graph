'use client';

import { useStore } from '@/store/store';
import SearchBox from './SearchBox';
import type { IAPI } from '@/lib/api';

interface Props { api: IAPI; srcInfo: string; onTogglePanel: () => void; }

export default function TopBar({ api, srcInfo, onTogglePanel }: Props) {
  const viewMode = useStore(s => s.viewMode);
  const colorMode = useStore(s => s.colorMode);
  const setViewMode = useStore(s => s.setViewMode);
  const setColorMode = useStore(s => s.setColorMode);

  return (
    <div className="topbar">
      <strong>ckg</strong>
      <SearchBox api={api} />
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
