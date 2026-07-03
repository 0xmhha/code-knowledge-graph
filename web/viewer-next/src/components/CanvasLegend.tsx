'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { GRAPH_GROUPS } from '@/lib/edges';
import { typeColorCss } from '@/lib/encoding';
import { usePersistedBool } from '@/lib/usePersistedState';

interface ShapeEntry {
  shape: 'circle' | 'hex' | 'square' | 'triangle' | 'diamond' | 'star' | 'micro' | 'tri-up' | 'chevron' | 'lock' | 'asterisk';
  label: string;
  // types: 이 행이 대표하는 node.type 들 — TYPE 색 모드(기본값)의 실제
  // 팔레트 점을 행 옆에 그려 "색 = 타입" 채널을 범례에 포함시킨다.
  // 렌더와 동일 소스(encoding.typeColorCss)라 드리프트 불가.
  types: string[];
}

// 감사(2026-07-03) 반영: 2D drawShape 의 폴백(원)으로 렌더되던
// Contract/Mapping/Event · Enum/Class/Modifier/Constructor ·
// Import/Export/Decorator 를 명시 행으로 추가하고, triangle 행에
// Parameter·LocalVariable 누락을 보충했다. 모양 아이콘은 2D 캔버스
// 기준이며 3D 는 타입별 대응 입체(콘/큐브/토러스/…)를 쓴다 — 본문
// 캡션에 명시. 색 점은 두 모드 공통.
const SHAPES: ReadonlyArray<ShapeEntry> = [
  { shape: 'circle',   label: 'Function · Method',
    types: ['Function', 'Method'] },
  { shape: 'hex',      label: 'Type · Struct · Interface · TypeAlias',
    types: ['Type', 'Struct', 'Interface', 'TypeAlias'] },
  { shape: 'square',   label: 'Package',
    types: ['Package'] },
  { shape: 'triangle', label: 'Field · Var · Const · Param · LocalVar',
    types: ['Field', 'Variable', 'Constant', 'Parameter', 'LocalVariable'] },
  { shape: 'diamond',  label: 'File',
    types: ['File'] },
  { shape: 'star',     label: 'Commit',
    types: ['Commit'] },
  { shape: 'circle',   label: 'Contract · Mapping · Event (sol)',
    types: ['Contract', 'Mapping', 'Event'] },
  { shape: 'circle',   label: 'Enum · Class · Modifier · Constructor',
    types: ['Enum', 'Class', 'Modifier', 'Constructor'] },
  { shape: 'circle',   label: 'Import · Export · Decorator',
    types: ['Import', 'Export', 'Decorator'] },
  { shape: 'micro',    label: 'CallSite · IfStmt · LoopStmt · Hunk · …',
    types: ['CallSite', 'Hunk'] },
  { shape: 'tri-up',   label: 'Goroutine',
    types: ['Goroutine'] },
  { shape: 'chevron',  label: 'Channel',
    types: ['Channel'] },
  { shape: 'lock',     label: 'Mutex',
    types: ['Mutex'] },
  { shape: 'asterisk', label: 'Endpoint',
    types: ['Endpoint'] },
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

// fmt2: bound floating-point precision so SVG `points`/`x1`/etc. attributes
// serialize identically on Node SSR and the browser. Math.cos/sin can return
// last-bit-different doubles on the two platforms (different libm builds),
// producing strings like "10.897114317029974" vs "10.897114317029976" — same
// number, different React hydration attribute, triggering hydration mismatch
// warnings on every reload. parseFloat strips trailing zeros (so "5.000" →
// "5") which keeps the served HTML compact.
const fmt2 = (n: number): number => parseFloat(n.toFixed(2));

function ShapeIcon({ shape }: { shape: ShapeEntry['shape'] }) {
  const cx = 9, cy = 7;
  switch (shape) {
    case 'circle':
      return <svg width={18} height={14}><circle cx={cx} cy={cy} r={4} fill="#7ab8ff" /></svg>;
    case 'hex': {
      const pts: string[] = [];
      for (let i = 0; i < 6; i++) {
        const ang = (Math.PI / 3) * i;
        pts.push(`${fmt2(cx + 4.5 * Math.cos(ang))},${fmt2(cy + 4.5 * Math.sin(ang))}`);
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
        pts.push(`${fmt2(cx + r * Math.cos(ang))},${fmt2(cy + r * Math.sin(ang))}`);
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
              x1={fmt2(cx - 4 * Math.cos((deg * Math.PI) / 180))}
              y1={fmt2(cy - 4 * Math.sin((deg * Math.PI) / 180))}
              x2={fmt2(cx + 4 * Math.cos((deg * Math.PI) / 180))}
              y2={fmt2(cy + 4 * Math.sin((deg * Math.PI) / 180))}
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
// h2: 범례 행이 11→14개로 늘며 기본 높이를 220→420 으로 올렸다. 구 키에
// 저장된 220 안팎 값이 새 기본값을 덮지 않도록 키를 버전 업(h → h2).
const LS_H = 'ckg.canvasLegend.h2';
const MIN_W = 160, MAX_W = 480;
const MIN_H = 120, MAX_H = 680;

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
  // SSR-safe defaults so the build-time HTML matches the client's
  // first render. The hooks / effects below pull saved values after
  // mount so a returning user gets the previously-resized box on
  // their second render — one frame after the open/220×220 default.
  const [open, setOpen] = usePersistedBool(LS_OPEN, true);
  const [width, setWidth] = useState<number>(240);
  const [height, setHeight] = useState<number>(420);
  useEffect(() => {
    if (typeof localStorage === 'undefined') return;
    try {
      const wv = parseInt(localStorage.getItem(LS_W) ?? '', 10);
      if (Number.isFinite(wv)) setWidth(Math.min(MAX_W, Math.max(MIN_W, wv)));
      const hv = parseInt(localStorage.getItem(LS_H) ?? '', 10);
      if (Number.isFinite(hv)) setHeight(Math.min(MAX_H, Math.max(MIN_H, hv)));
    } catch { /* ignore */ }
  }, []);
  const wRef = useRef(width); wRef.current = width;
  const hRef = useRef(height); hRef.current = height;

  const toggle = () => setOpen(!open);

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
        <div style={{ color: '#7d8188', fontSize: 10, margin: '0 0 4px 2px' }}>
          모양 = 2D 기준 (3D는 대응 입체) · 점 색 = TYPE 색 모드
        </div>
        {SHAPES.map(s => (
          // key 는 label — circle 모양이 여러 행에서 재사용되므로 shape 를
          // key 로 쓰면 React 가 행을 병합해 버린다.
          <div key={s.label} className="legend-row">
            <span className="legend-icon"><ShapeIcon shape={s.shape} /></span>
            <span className="legend-label">{s.label}</span>
            <span style={{ display: 'inline-flex', gap: 2, marginLeft: 'auto', flex: 'none' }}>
              {s.types.map(t => (
                <span
                  key={t}
                  title={t}
                  style={{
                    width: 7, height: 7, borderRadius: '50%',
                    background: typeColorCss(t), display: 'inline-block',
                  }}
                />
              ))}
            </span>
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
