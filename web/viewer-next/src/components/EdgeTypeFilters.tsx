'use client';

import { useStore } from '@/store/store';
import { ALL_EDGE_TYPES } from '@/lib/edges';

export default function EdgeTypeFilters() {
  const whitelist = useStore(s => s.edgeTypeWhitelist);
  const toggle = useStore(s => s.toggleEdgeType);

  return (
    <div className="edge-filters">
      <h4>Edge Types</h4>
      {ALL_EDGE_TYPES.map(t => (
        <label key={t}>
          <input type="checkbox" checked={whitelist.has(t)} onChange={() => toggle(t)} />
          {t}
        </label>
      ))}
    </div>
  );
}
