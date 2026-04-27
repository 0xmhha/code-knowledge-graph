'use client';

import { useMemo } from 'react';
import { useStore } from '@/store/store';
import { communityColorCss } from '@/lib/encoding';

// M2 will populate this from /api/nodes responses with community_id.
// Until then we derive what we can from currently-loaded nodes.
export default function Legend() {
  const nodes = useStore(s => s.nodes);
  const dimmed = useStore(s => s.dimmedCommunities);
  const isolated = useStore(s => s.isolatedCommunity);
  const toggleDim = useStore(s => s.toggleDimCommunity);
  const setIsolated = useStore(s => s.setIsolatedCommunity);

  const groups = useMemo(() => {
    const map = new Map<number, { count: number; label: string }>();
    for (const n of nodes.values()) {
      if (n.community_id == null) continue;
      const cur = map.get(n.community_id);
      if (cur) cur.count += 1;
      else map.set(n.community_id, { count: 1, label: n.topic_label ?? '' });
    }
    return [...map.entries()]
      .sort((a, b) => b[1].count - a[1].count)
      .slice(0, 60);
  }, [nodes]);

  if (groups.length === 0) {
    return (
      <div className="legend">
        <h4>Communities</h4>
        <div style={{ color: '#666', fontSize: 10 }}>
          No community data yet — backend wiring lands in M2.
        </div>
      </div>
    );
  }

  return (
    <div className="legend">
      <h4>Communities</h4>
      {groups.map(([id, info]) => {
        const isDimmed = dimmed.has(id);
        const isIsolated = isolated === id;
        const cls = `legend-item${isDimmed ? ' dimmed' : ''}${isIsolated ? ' isolated' : ''}`;
        return (
          <div
            key={id}
            className={cls}
            onClick={(e) => {
              if (e.shiftKey) {
                setIsolated(isIsolated ? null : id);
              } else {
                toggleDim(id);
              }
            }}
            title="click: dim · shift-click: isolate"
          >
            <span className="dot" style={{ background: communityColorCss(id) }} />
            <span className="label">{info.label || `c${id}`}</span>
            <span className="count">{info.count}</span>
          </div>
        );
      })}
    </div>
  );
}
