'use client';

import { useState } from 'react';
import { GRAPH_GROUPS } from '@/lib/edges';

// Per-shape entries for the Node Shapes section. The kind label maps
// 1:1 with the switch in GraphCanvas.drawNode2D so users can read off
// what each polygon represents without consulting the source.
//
// 3D parity: deferred. The 3D path uses lib/encoding.nodeMesh whose
// PRIMITIVE table is richer (per-type Three.js geometry). This legend
// is a reading aid for the 2D mode; in 3D the existing geometry table
// already encodes types via mesh kind. The legend stays mounted in
// either mode because the symbols (circle, square, triangle) are
// intuitive enough to remain useful as a colour-shape cross-reference.
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
  { shape: 'micro',    label: 'CallSite · IfStmt · LoopStmt · …' },
  { shape: 'tri-up',   label: 'Goroutine' },
  { shape: 'chevron',  label: 'Channel' },
  { shape: 'lock',     label: 'Mutex' },
  { shape: 'asterisk', label: 'Endpoint' },
];

// Edge-style entries. The dash array MUST match GraphCanvas.linkLineDash
// so the swatch is a literal preview of what the canvas draws. null
// dash → solid line.
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
  // 18×14 viewBox; geometry centred so shapes line up vertically.
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
      // Slightly fatter / taller upward triangle to read distinct from
      // generic 'triangle'.
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
      // Outer filled square with hollow centre — proxies a mutex glyph
      // without an external icon font.
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

// CanvasLegend persists open/closed across reloads under
// `ckg.canvasLegend.open`. Default OPEN on first paint so the symbols
// show up without the user having to hunt for them.
export default function CanvasLegend() {
  const [open, setOpen] = useState<boolean>(() => {
    if (typeof localStorage === 'undefined') return true;
    return localStorage.getItem('ckg.canvasLegend.open') !== '0';
  });
  const toggle = () => setOpen(v => {
    const next = !v;
    try { localStorage.setItem('ckg.canvasLegend.open', next ? '1' : '0'); } catch { /* ignore */ }
    return next;
  });

  if (!open) {
    return (
      <div className="canvas-legend collapsed">
        <div className="canvas-legend-header">
          <span
            className="canvas-legend-title"
            onClick={toggle}
            role="button"
            tabIndex={0}
            onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') toggle(); }}
            title="Show legend"
          >
            ▶ Legend
          </span>
        </div>
      </div>
    );
  }

  return (
    <div className="canvas-legend">
      <div className="canvas-legend-header">
        <span
          className="canvas-legend-title"
          onClick={toggle}
          role="button"
          tabIndex={0}
          onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') toggle(); }}
          title="Collapse legend"
        >
          ▼ Legend
        </span>
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
  );
}
