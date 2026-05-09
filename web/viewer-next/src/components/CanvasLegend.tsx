'use client';

import { useCallback, useRef, useState } from 'react';
import { GRAPH_GROUPS } from '@/lib/edges';

interface ShapeEntry {
  shape: 'circle' | 'hex' | 'square' | 'triangle' | 'diamond' | 'star' | 'micro' | 'tri-up' | 'chevron' | 'lock' | 'asterisk';
  label: string;
}

const SHAPES: ReadonlyArray<ShapeEntry> = [
  { shape: 'circle',   label: 'Function · Method' },
  { shape: 'hex',      label: 'Type · Struct · Interface · TypeAlias' },
  { shape: 'square',   label: 'Package' },
  { shape: 'triangle', label: 'Field · Variable · Constant' },
  { shape: 'diamond',  label: 'File' },
  { shape: 'star',     label: 'Commit' },
  { shape: 'micro',    label: 'CallSite · IfStmt · LoopStmt · Hunk · …' },
  { shape: 'tri-up',   label: 'Goroutine' },
  { shape: 'chevron',  label: 'Channel' },
  { shape: 'lock',     label: 'Mutex' },
  { shape: 'asterisk', label: 'Endpoint' },
];

interface EdgeStyleEntry {
  groupId: 'G1' | 'G2' | 'G3' | 'G4' | 'G5' | 'G6';
  label: string;
  dash: number[] | null;
  width: number;
}

const EDGE_STYLES: ReadonlyArray<EdgeStyleEntry> = [
  { groupId: 'G1', label: 'Structural',  dash: null,            width: 1.5 },
  { groupId: 'G2', label: 'Semantic',    dash: [6, 3],          width: 1.5 },
  { groupId: 'G3', label: 'Execution',   dash: null,            width: 1.5 },
  { groupId: 'G4', label: 'Concurrency', dash: [2, 2],          width: 1.5 },
  { groupId: 'G5', label: 'Distributed', dash: [6, 2, 2, 2],    width: 1.5 },
  { groupId: 'G6', label: 'Temporal',    dash: null,            width: 0.6 },
];

function ShapeIcon({ shape }: { shape: ShapeEntry['shape'] }) {
  const cx = 9, cy = 7;
  switch (shape) {
    case 'circle':
      return <svg width={18} height={14}><circle cx={cx} cy={cy} r={4} fill="#7ab8ff" /></svg>;
    case 'hex': {
      const pts: string[] = [];
      for (let i = 0; i < 6; i++) {
        const ang = (Math.PI / 3) * i;
        pts.push(`${cx + 4.5 * Math.cos(ang)},${cy + 4.5 * Math.sin(ang)}`);
      }
      return <svg width={18} height={14}><polygon points={pts.join(' ')} fill="#9aa" /></svg>;
    }
    case 'square':
      return <svg width={18} height={14}><rect x={cx - 4} y={cy - 4} width={8} height={8} rx={1.5} fill="#ffb060" /></svg>;
    case 'triangle': {
      const pts = `${cx},${cy - 4} ${cx - 3.5},${cy + 3} ${cx + 3.5},${cy + 3}`;
      return <svg width={18} height={14}><polygon points={pts} fill="#cfd0d3" /></svg>;
    }
    case 'diamond': {
      const pts = `${cx},${cy - 4.5} ${cx + 4.5},${cy} ${cx},${cy + 4.5} ${cx - 4.5},${cy}`;
      return <svg width={18} height={14}><polygon points={pts} fill="#66ccff" /></svg>;
    }
    case 'star': {
      const pts: string[] = [];
      for (let i = 0; i < 10; i++) {
        const r = i % 2 === 0 ? 5 : 2;
        const ang = (Math.PI / 5) * i - Math.PI / 2;
        pts.push(`${cx + r * Math.cos(ang)},${cy + r * Math.sin(ang)}`);
      }
      return <svg width={18} height={14}><polygon points={pts.join(' ')} fill="#ffd700" /></svg>;
    }
    case 'micro':
      return <svg width={18} height={14}><circle cx={cx} cy={cy} r={1.6} fill="#888" /></svg>;
    case 'tri-up': {
      const pts = `${cx},${cy - 5} ${cx - 4.5},${cy + 4} ${cx + 4.5},${cy + 4}`;
      return <svg width={18} height={14}><polygon points={pts} fill="#ff66cc" /></svg>;
    }
    case 'chevron':
      return (
        <svg width={18} height={14}>
          <polyline
            points={`${cx - 4},${cy - 3} ${cx + 2},${cy} ${cx - 4},${cy + 3}`}
            fill="none" stroke="#cc66cc" strokeWidth={1.6} strokeLinecap="round" strokeLinejoin="round"
          />
        </svg>
      );
    case 'lock':
      return (
        <svg width={18} height={14}>
          <rect x={cx - 4} y={cy - 4} width={8} height={8} rx={1} fill="#ff5577" />
          <rect x={cx - 1.5} y={cy - 1.5} width={3} height={3} fill="#1f2329" />
        </svg>
      );
    case 'asterisk':
      return (
        <svg width={18} height={14}>
          {[0, 60, 120].map(deg => (
            <line
              key={deg}
              x1={cx - 4 * Math.cos((deg * Math.PI) / 180)}
              y1={cy - 4 * Math.sin((deg * Math.PI) / 180)}
              x2={cx + 4 * Math.cos((deg * Math.PI) / 180)}
              y2={cy + 4 * Math.sin((deg * Math.PI) / 180)}
              stroke="#44aaff" strokeWidth={1.4} strokeLinecap="round"
            />
          ))}
        </svg>
      );
  }
}

function EdgeIcon({ entry }: { entry: EdgeStyleEntry }) {
  const group = GRAPH_GROUPS.find(g => g.id === entry.groupId);
  const colorHex = group?.color ?? 0x888888;
  const color = `#${colorHex.toString(16).padStart(6, '0')}`;
  return (
    <svg width={18} height={14}>
      <line
        x1={1} y1={7} x2={17} y2={7}
        stroke={color}
        strokeWidth={entry.width}
        strokeDasharray={entry.dash ? entry.dash.join(',') : undefined}
      />
    </svg>
  );
}

// Persistence keys for legend state. Three pieces survive reloads: open
// flag, panel width, panel height. Default size 220×220 picks a corner
// footprint smaller than NodeList's 240px clamp floor — the legend is a
// reading aid, not a primary surface.
const LS_OPEN = 'ckg.canvasLegend.open';
const LS_W = 'ckg.canvasLegend.w';
const LS_H = 'ckg.canvasLegend.h';
const MIN_W = 160, MAX_W = 480;
const MIN_H = 120, MAX_H = 520;

// CanvasLegend renders in the bottom-right corner of the canvas-host as
// a tip overlay — small enough to leave the graph visible, draggable
// from its top-left corner to expand. Closed state collapses to a tiny
// "ℹ Legend" affordance pinned in the same corner.
//
// Bottom-right placement is intentional: top-right would compete with
// the (now-removed) ControlLayer slot some users still expect; bottom-
// right is empty real estate on most graph canvases. The top-left
// corner of the box hosts the resize grip — diagonally opposite the
// box's anchor (bottom-right) so dragging it grows toward the canvas
// centre, the natural expansion direction.
export default function CanvasLegend() {
  const [open, setOpen] = useState<boolean>(() => {
    if (typeof localStorage === 'undefined') return true;
    return localStorage.getItem(LS_OPEN) !== '0';
  });
  const [width, setWidth] = useState<number>(() => {
    if (typeof localStorage === 'undefined') return 220;
    const v = parseInt(localStorage.getItem(LS_W) ?? '', 10);
    return Number.isFinite(v) ? Math.min(MAX_W, Math.max(MIN_W, v)) : 220;
  });
  const [height, setHeight] = useState<number>(() => {
    if (typeof localStorage === 'undefined') return 220;
    const v = parseInt(localStorage.getItem(LS_H) ?? '', 10);
    return Number.isFinite(v) ? Math.min(MAX_H, Math.max(MIN_H, v)) : 220;
  });
  const wRef = useRef(width); wRef.current = width;
  const hRef = useRef(height); hRef.current = height;

  const toggle = () => setOpen(v => {
    const next = !v;
    try { localStorage.setItem(LS_OPEN, next ? '1' : '0'); } catch { /* ignore */ }
    return next;
  });

  // Resize handle drag — anchored at the bottom-right, the grip is at
  // the top-left of the box. Dragging the grip up/left grows the box
  // toward the canvas centre. dx/dy are inverted because the box's
  // origin is bottom-right.
  const onResizeStart = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    const startX = e.clientX, startY = e.clientY;
    const startW = wRef.current, startH = hRef.current;
    const onMove = (ev: MouseEvent) => {
      const dx = startX - ev.clientX;
      const dy = startY - ev.clientY;
      const nextW = Math.min(MAX_W, Math.max(MIN_W, startW + dx));
      const nextH = Math.min(MAX_H, Math.max(MIN_H, startH + dy));
      setWidth(nextW);
      setHeight(nextH);
    };
    const onUp = () => {
      window.removeEventListener('mousemove', onMove);
      window.removeEventListener('mouseup', onUp);
      try {
        localStorage.setItem(LS_W, String(wRef.current));
        localStorage.setItem(LS_H, String(hRef.current));
      } catch { /* ignore */ }
    };
    window.addEventListener('mousemove', onMove);
    window.addEventListener('mouseup', onUp);
  }, []);

  if (!open) {
    return (
      <button
        type="button"
        className="canvas-legend-trigger"
        onClick={toggle}
        title="Show node/edge legend"
        aria-label="Show legend"
      >
        ℹ Legend
      </button>
    );
  }

  return (
    <div
      className="canvas-legend"
      style={{ width: `${width}px`, height: `${height}px` }}
    >
      <div
        className="canvas-legend-resizer"
        onMouseDown={onResizeStart}
        role="separator"
        aria-orientation="horizontal"
        aria-label="Resize legend"
        title="Drag to resize"
      />
      <div className="canvas-legend-header">
        <span className="canvas-legend-title">Legend</span>
        <button
          type="button"
          className="canvas-legend-close"
          onClick={toggle}
          title="Close legend"
          aria-label="Close legend"
        >
          ✕
        </button>
      </div>
      <div className="canvas-legend-body">
        <h5>Node Shapes</h5>
        {SHAPES.map(s => (
          <div key={s.shape} className="legend-row">
            <span className="legend-icon"><ShapeIcon shape={s.shape} /></span>
            <span className="legend-label">{s.label}</span>
          </div>
        ))}
        <h5>Edge Styles</h5>
        {EDGE_STYLES.map(e => (
          <div key={e.groupId} className="legend-row">
            <span className="legend-icon"><EdgeIcon entry={e} /></span>
            <span className="legend-label">{e.label}</span>
            <span className="legend-tag">{e.groupId}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
