'use client';

import { useStore } from '@/store/store';
import { useShallow } from 'zustand/react/shallow';
import type { TraceDirection } from '@/types';

interface Props {
  onHelpClick: () => void;
}

const DIRS: Array<{ id: TraceDirection; label: string; title: string }> = [
  { id: 'callers', label: '◀', title: 'Trace callers (t)' },
  { id: 'both',    label: '◆', title: 'Trace both directions (t)' },
  { id: 'callees', label: '▶', title: 'Trace callees (t)' },
];

export default function ControlLayer({ onHelpClick }: Props) {
  const { viewMode, colorMode, traceDirection, traceDepth } = useStore(
    useShallow(s => ({
      viewMode: s.viewMode,
      colorMode: s.colorMode,
      traceDirection: s.traceDirection,
      traceDepth: s.traceDepth,
    })),
  );
  const setViewMode = useStore(s => s.setViewMode);
  const setColorMode = useStore(s => s.setColorMode);
  const setTraceDirection = useStore(s => s.setTraceDirection);
  const setTraceDepth = useStore(s => s.setTraceDepth);

  const handleViewToggle = () => {
    const next = viewMode === '2d' ? '3d' : '2d';
    setViewMode(next);
    try { localStorage.setItem('ckg.viewMode', next); } catch { /* ignore */ }
  };

  const handleColorToggle = () => {
    const next = colorMode === 'lang' ? 'community' : 'lang';
    setColorMode(next);
    try { localStorage.setItem('ckg.colorMode', next); } catch { /* ignore */ }
  };

  return (
    <div className="control-layer">
      <div className="cl-section">
        <div className="cl-label">Trace</div>
        <div className="cl-row">
          {DIRS.map(d => (
            <button
              key={d.id}
              className={`cl-btn${traceDirection === d.id ? ' active' : ''}`}
              onClick={() => setTraceDirection(d.id)}
              title={d.title}
            >
              {d.label}
            </button>
          ))}
        </div>
        <div className="cl-row">
          <span className="cl-muted">depth</span>
          {[1, 2, 3, 4].map(n => (
            <button
              key={n}
              className={`cl-btn${traceDepth === n ? ' active' : ''}`}
              onClick={() => setTraceDepth(n)}
              title={`Trace depth ${n} (key: ${n})`}
            >
              {n}
            </button>
          ))}
        </div>
      </div>
      <div className="cl-divider" />
      <div className="cl-section">
        <div className="cl-row">
          <span className="cl-muted">Colour</span>
          <button
            className="cl-btn cl-toggle"
            onClick={handleColorToggle}
            title="Toggle colour mode (m)"
          >
            {colorMode === 'lang' ? 'LANG' : 'COMM'}
          </button>
        </div>
        <div className="cl-row">
          <span className="cl-muted">View</span>
          <button
            className="cl-btn cl-toggle"
            onClick={handleViewToggle}
            title="Toggle 2D / 3D (v)"
          >
            {viewMode === '2d' ? '2D' : '3D'}
          </button>
        </div>
      </div>
      <div className="cl-divider" />
      <div className="cl-section">
        <div className="cl-row">
          <button
            className="cl-btn cl-help-btn"
            onClick={onHelpClick}
            title="Keyboard shortcuts (?)"
          >
            ? shortcuts
          </button>
        </div>
      </div>
    </div>
  );
}
