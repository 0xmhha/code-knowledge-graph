'use client';

import { useStore } from '@/store/store';
import type { TraceDirection } from '@/types';

const DIRS: Array<{ id: TraceDirection; label: string; title: string }> = [
  { id: 'callers', label: '◀ callers', title: 'Trace what calls the selected node' },
  { id: 'both',    label: '◆ both',    title: 'Trace both directions' },
  { id: 'callees', label: 'callees ▶', title: 'Trace what the selected node calls' },
];

// TraceControls is meaningful only when an anchor node is set — direction
// and depth feed traceFromNode, which BFS-walks neighbours of the anchor.
// Without an anchor the controls update store state but do nothing visible
// (App.tsx's re-trace useEffect early-returns on `!anchorId`). Earlier
// builds left the buttons enabled in that state which read as "broken
// UI" — clicking did nothing on a root view. Now the entire row is
// explicitly disabled, the heading reflects the anchor (or lack thereof),
// and a live visible-node count gives feedback when depth/direction
// changes do produce a different fanout (otherwise depth 1↔4 looks
// identical on hub anchors that already saturate at MAX_TRACE_VISIBLE).
export default function TraceControls() {
  const dir = useStore(s => s.traceDirection);
  const depth = useStore(s => s.traceDepth);
  const setDir = useStore(s => s.setTraceDirection);
  const setDepth = useStore(s => s.setTraceDepth);
  const anchorId = useStore(s => s.anchorId);
  const anchorName = useStore(s => {
    if (!s.anchorId) return null;
    return s.nodes.get(s.anchorId)?.name ?? null;
  });
  const visibleCount = useStore(s => s.visibleIds.size);
  const disabled = anchorId === null;

  const heading = anchorName
    ? `Trace from ${anchorName}`
    : 'Trace (click a node first)';
  const dirHint = disabled
    ? 'Click a graph node to anchor, then use Trace controls.'
    : '';

  return (
    <div className={`trace-controls${disabled ? ' is-disabled' : ''}`}>
      <h4 title={dirHint}>{heading}</h4>
      {DIRS.map(d => (
        <button
          key={d.id}
          className={dir === d.id ? 'active' : ''}
          onClick={() => setDir(d.id)}
          title={disabled ? dirHint : d.title}
          disabled={disabled}
        >
          {d.label}
        </button>
      ))}
      <span style={{ color: '#888', marginLeft: 8 }}>depth</span>
      {[1, 2, 3, 4].map(n => (
        <button
          key={n}
          className={depth === n ? 'active' : ''}
          onClick={() => setDepth(n)}
          title={disabled
            ? dirHint
            : `Trace depth ${n} (callers mode adds +2 hops automatically)`}
          disabled={disabled}
        >
          {n}
        </button>
      ))}
      {!disabled && (
        <span
          className="trace-count"
          title="Visible nodes after this trace. Hub anchors can saturate the 500-node cap, making depth 1..4 look identical."
        >
          {visibleCount} nodes
        </span>
      )}
    </div>
  );
}
