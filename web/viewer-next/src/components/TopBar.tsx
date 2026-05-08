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
  const setViewMode = useStore(s => s.setViewMode);
  const setColorMode = useStore(s => s.setColorMode);

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
