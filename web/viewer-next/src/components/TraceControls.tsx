'use client';

import { useStore } from '@/store/store';
import type { TraceDirection } from '@/types';

const DIRS: Array<{ id: TraceDirection; label: string; title: string }> = [
  { id: 'callers', label: '◀ callers', title: 'Trace what calls the selected node' },
  { id: 'both',    label: '◆ both',    title: 'Trace both directions' },
  { id: 'callees', label: 'callees ▶', title: 'Trace what the selected node calls' },
];

export default function TraceControls() {
  const dir = useStore(s => s.traceDirection);
  const depth = useStore(s => s.traceDepth);
  const setDir = useStore(s => s.setTraceDirection);
  const setDepth = useStore(s => s.setTraceDepth);

  return (
    <div className="trace-controls">
      <h4>Trace</h4>
      {DIRS.map(d => (
        <button
          key={d.id}
          className={dir === d.id ? 'active' : ''}
          onClick={() => setDir(d.id)}
          title={d.title}
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
        >
          {n}
        </button>
      ))}
    </div>
  );
}
