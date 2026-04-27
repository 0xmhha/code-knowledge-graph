'use client';

import { useStore } from '@/store/store';

interface Props {
  onDepthIn: () => void;
  onDepthOut: () => void;
  onHome: () => void;
}

const FONT_SIZES: Record<string, number> = { S: 0.85, M: 1.0, L: 1.2 };
const DEPTH_MAX = 6;

export default function BottomBar({ onDepthIn, onDepthOut, onHome }: Props) {
  const anchorId = useStore(s => s.anchorId);
  const depth = useStore(s => s.depth);
  const visibleCount = useStore(s => s.visibleIds.size);
  const lastRenderMs = useStore(s => s.lastRenderMs);
  const setFontSize = useStore(s => s.setFontSize);

  const edgeCount = useStore(s => {
    let n = 0;
    for (const id of s.visibleIds) {
      const outs = s.edgesBySrc.get(id);
      if (!outs) continue;
      for (const e of outs) if (s.visibleIds.has(e.dst)) n++;
    }
    return n;
  });

  const depthLabel = anchorId ? `depth ${depth}/${DEPTH_MAX}` : 'depth root';

  return (
    <div className="bottombar">
      <span style={{ display: 'inline-flex', gap: 4 }}>
        <button title="Depth out ([)" onClick={onDepthOut}>⇱</button>
        <button title="Depth in (])" onClick={onDepthIn}>⇲</button>
        <button title="Home" onClick={onHome}>🏠</button>
        <span>{depthLabel}</span>
      </span>
      <span style={{ color: '#7ab8ff' }}>
        {lastRenderMs.toFixed(0)} ms · {visibleCount} nodes / {edgeCount} edges
      </span>
      <span style={{ marginLeft: 'auto', display: 'inline-flex', gap: 4 }}>
        <span style={{ color: '#888', marginRight: 4 }}>font</span>
        {(['S', 'M', 'L'] as const).map(label => (
          <button
            key={label}
            onClick={() => {
              setFontSize(FONT_SIZES[label]);
              try { localStorage.setItem('ckg.fontSize', label); } catch { /* ignore */ }
            }}
          >
            {label}
          </button>
        ))}
      </span>
    </div>
  );
}
