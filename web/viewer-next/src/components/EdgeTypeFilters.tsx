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
const GRAPH_MODE_KEY = 'ckg.graphMode';

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

// GraphPillStrip is a compact, always-visible row of six group toggles
// pinned to the top of the panel. Each pill toggles its entire group
// (delegating to setEdgeTypeWhitelistBulk). Visual state encodes
// "all on" (full opacity) / "some on" (mid) / "all off" (dim) so the
// user can read the current 6-graph state at a glance without
// expanding any sublist.
//
// When `graphModeIsolation` is on, pill clicks REPLACE the whitelist
// with just the clicked group's edges (single-graph view). The pill
// for the currently-active group is marked `pill-active` so the user
// always sees which axis they're focused on.
function GraphPillStrip() {
  const whitelist = useStore(s => s.edgeTypeWhitelist);
  const setBulk = useStore(s => s.setEdgeTypeWhitelistBulk);
  const setOnly = useStore(s => s.setEdgeTypeWhitelistOnlyGroup);
  const isolation = useStore(s => s.graphModeIsolation);

  return (
    <div className="graph-pills" role="group" aria-label="6-graph axis toggles">
      {GRAPH_GROUPS.map(g => {
        const allOn = groupHasAllEdges(g, whitelist);
        const anyOn = groupHasAnyEdge(g, whitelist);
        // In isolation mode "active" means this group's edges are the
        // ENTIRE whitelist — i.e. allOn AND no other group contributes.
        // We approximate "no other group" by checking whitelist size
        // matches this group's edge count; allOn already guarantees the
        // forward direction, so equal sizes implies set equality.
        const isolatedActive = isolation && allOn && whitelist.size === g.edges.length;
        let cls: string;
        if (isolation) {
          cls = isolatedActive ? 'pill-on pill-active' : 'pill-off';
        } else {
          cls = allOn ? 'pill-on' : (anyOn ? 'pill-partial' : 'pill-off');
        }
        const onClick = () => {
          if (isolation) {
            setOnly(g);
          } else {
            // Mirrors GroupSection header toggle semantics — partial state
            // always turns on the rest, all-on state turns off.
            setBulk(g.edges, !allOn);
          }
        };
        const title = isolation
          ? `${g.id} ${g.label} — ${g.description}\nClick to focus this graph (replaces whitelist).`
          : `${g.id} ${g.label} — ${g.description}\nClick to ${allOn ? 'turn all off' : 'turn all on'}.`;
        return (
          <button
            key={g.id}
            type="button"
            className={`graph-pill ${cls}`}
            style={{ borderColor: hex(g.color) }}
            onClick={onClick}
            title={title}
          >
            <span className="graph-pill-dot" style={{ background: hex(g.color) }} />
            <span className="graph-pill-id">{g.id}</span>
            <span className="graph-pill-label">{g.label}</span>
          </button>
        );
      })}
    </div>
  );
}

// GraphModeToggle is a small button that flips graphModeIsolation. We
// keep the visible label deliberately short ("Solo") so it can sit on
// the same row as the section heading without wrapping on a 280px panel.
function GraphModeToggle() {
  const isolation = useStore(s => s.graphModeIsolation);
  const setIsolation = useStore(s => s.setGraphModeIsolation);
  const onClick = () => {
    const next = !isolation;
    setIsolation(next);
    try {
      if (typeof localStorage !== 'undefined') {
        localStorage.setItem(GRAPH_MODE_KEY, next ? '1' : '0');
      }
    } catch { /* localStorage may be blocked */ }
  };
  return (
    <button
      type="button"
      className={`graph-mode-toggle ${isolation ? 'on' : 'off'}`}
      onClick={onClick}
      title={
        isolation
          ? 'Solo mode ON — clicking a pill switches to that graph only.\nClick to turn OFF (cumulative toggling).'
          : 'Solo mode OFF — pills cumulatively toggle groups.\nClick to turn ON (single-graph view).'
      }
    >
      🎯 Solo {isolation ? 'ON' : 'OFF'}
    </button>
  );
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
  const setIsolation = useStore(s => s.setGraphModeIsolation);

  useEffect(() => { saveCollapsed(collapsed); }, [collapsed]);

  // Hydrate graphModeIsolation from localStorage on mount. We do this
  // here (not in the store initialiser) because the store also runs on
  // the SSR pass where `localStorage` is undefined; reading it during
  // initial render would throw.
  useEffect(() => {
    try {
      if (typeof localStorage === 'undefined') return;
      const raw = localStorage.getItem(GRAPH_MODE_KEY);
      if (raw === '1') setIsolation(true);
    } catch { /* localStorage may be blocked */ }
  }, [setIsolation]);

  const onToggle = useCallback((id: GraphID) => {
    setCollapsed(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  }, []);

  return (
    <div className="edge-filters">
      <div className="edge-filters-header">
        <h4>Edge Types (6-graph axis)</h4>
        <GraphModeToggle />
      </div>
      <GraphPillStrip />
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
