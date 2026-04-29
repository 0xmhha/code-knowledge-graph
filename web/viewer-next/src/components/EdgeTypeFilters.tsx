'use client';

import { useCallback, useEffect, useState } from 'react';
import { useStore } from '@/store/store';
import {
  GRAPH_GROUPS, groupHasAllEdges, groupHasAnyEdge,
  type GraphGroupSpec, type GraphID,
} from '@/lib/edges';

// Default-collapsed groups (most numerous, least interesting for trace mode).
const DEFAULT_COLLAPSED: ReadonlyArray<GraphID> = ['G1', 'G2'];
const STORAGE_KEY = 'ckg.edgeFiltersCollapsed';

function loadCollapsed(): Set<GraphID> {
  try {
    const raw = typeof localStorage !== 'undefined' ? localStorage.getItem(STORAGE_KEY) : null;
    if (raw) {
      const arr = JSON.parse(raw);
      if (Array.isArray(arr)) return new Set(arr.filter((x): x is GraphID =>
        typeof x === 'string' && /^G[1-6]$/.test(x)));
    }
  } catch { /* localStorage may be blocked */ }
  return new Set(DEFAULT_COLLAPSED);
}

function saveCollapsed(s: Set<GraphID>): void {
  try {
    if (typeof localStorage !== 'undefined') {
      localStorage.setItem(STORAGE_KEY, JSON.stringify([...s]));
    }
  } catch { /* localStorage may be blocked */ }
}

function hex(n: number): string {
  return '#' + n.toString(16).padStart(6, '0');
}

interface GroupSectionProps {
  group: GraphGroupSpec;
  collapsed: boolean;
  onToggleCollapse: () => void;
}

function GroupSection({ group, collapsed, onToggleCollapse }: GroupSectionProps) {
  const whitelist = useStore(s => s.edgeTypeWhitelist);
  const toggle = useStore(s => s.toggleEdgeType);
  const setBulk = useStore(s => s.setEdgeTypeWhitelistBulk);

  const allOn = groupHasAllEdges(group, whitelist);
  const anyOn = groupHasAnyEdge(group, whitelist);
  const enabledCount = group.edges.reduce((acc, e) => acc + (whitelist.has(e) ? 1 : 0), 0);

  const groupClass = allOn ? 'all-on' : (anyOn ? 'partial' : 'all-off');
  const groupLabel = allOn ? 'all' : (anyOn ? 'some' : 'none');

  const onGroupToggle = useCallback((ev: React.MouseEvent) => {
    ev.stopPropagation();  // don't trigger collapse
    setBulk(group.edges, !allOn);
  }, [setBulk, group.edges, allOn]);

  return (
    <div className="graph-group">
      <div className="graph-group-header" onClick={onToggleCollapse} title={group.description}>
        <span className="graph-group-arrow">{collapsed ? '▶' : '▼'}</span>
        <span className="graph-group-dot" style={{ background: hex(group.color) }} />
        <span className="graph-group-label">{group.id} {group.label}</span>
        <span className="graph-group-count">{enabledCount}/{group.edges.length}</span>
        <button
          type="button"
          className={`graph-group-toggle ${groupClass}`}
          onClick={onGroupToggle}
          title={`Group toggle: currently ${groupLabel}. Click to ${allOn ? 'turn all off' : 'turn all on'}.`}
        >
          {groupLabel}
        </button>
      </div>
      {!collapsed && (
        <div className="graph-group-edges">
          {group.edges.map(t => (
            <label key={t}>
              <input type="checkbox" checked={whitelist.has(t)} onChange={() => toggle(t)} />
              {t}
            </label>
          ))}
        </div>
      )}
    </div>
  );
}

export default function EdgeTypeFilters() {
  const [collapsed, setCollapsed] = useState<Set<GraphID>>(() => loadCollapsed());

  useEffect(() => { saveCollapsed(collapsed); }, [collapsed]);

  const onToggle = useCallback((id: GraphID) => {
    setCollapsed(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  }, []);

  return (
    <div className="edge-filters">
      <h4>Edge Types (6-graph axis)</h4>
      {GRAPH_GROUPS.map(g => (
        <GroupSection
          key={g.id}
          group={g}
          collapsed={collapsed.has(g.id)}
          onToggleCollapse={() => onToggle(g.id)}
        />
      ))}
    </div>
  );
}
