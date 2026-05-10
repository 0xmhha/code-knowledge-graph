'use client';

import { useCallback, useEffect, useState } from 'react';
import { useStore } from '@/store/store';
import {
  GRAPH_GROUPS, groupHasAllEdges, groupHasAnyEdge,
  type GraphGroupSpec, type GraphID,
} from '@/lib/edges';

// All six groups default-collapsed. Earlier we kept G3..G6 expanded so the
// individual edge-type checkboxes were visible, but within the panel's
// 240..360px clamp range the expanded sublists consumed ~520px of vertical
// space — that pushed NodeList down to ~110px (about one item visible) and
// made users think the visible-nodes list was empty. The pill strip alone
// is enough for the common toggle case; opening a section is a deliberate
// action when fine-grained per-edge-type control is needed.
const DEFAULT_COLLAPSED: ReadonlyArray<GraphID> = ['G1', 'G2', 'G3', 'G4', 'G5', 'G6'];
const STORAGE_KEY = 'ckg.edgeFiltersCollapsed';
const GRAPH_MODE_KEY = 'ckg.graphMode';

// Versioned migration sentinel for the collapsed-state default. Earlier
// builds shipped DEFAULT_COLLAPSED = ['G1','G2'] which left G3..G6 expanded
// and consumed ~520px of panel height — pushing NodeList / NodeDetail off
// the visible area. Returning users keep that stale value forever because
// loadCollapsed() short-circuits on the stored array; the new
// all-six-collapsed default never reaches them. Bumping MIGRATION_VAL
// forces exactly one reset, then we honour the user's saved choices again.
const MIGRATION_KEY = 'ckg.edgeFiltersV';
const MIGRATION_VAL = 'v2';

function loadCollapsed(): Set<GraphID> {
  try {
    if (typeof localStorage !== 'undefined') {
      const ver = localStorage.getItem(MIGRATION_KEY);
      if (ver !== MIGRATION_VAL) {
        // First load on v2 (or returning user with stale v1 default).
        // Reset to the new all-six-collapsed default exactly once and
        // mirror it to STORAGE_KEY so the persisted value matches the
        // in-memory Set on first paint (avoids a transient mismatch
        // between what the UI shows and what reload would restore).
        // Write data first, sentinel last — sentinel acts as a commit marker so
        // a crash between the two writes simply re-runs the migration on next
        // load instead of leaving a stale STORAGE_KEY with a satisfied sentinel.
        localStorage.setItem(STORAGE_KEY, JSON.stringify([...DEFAULT_COLLAPSED]));
        localStorage.setItem(MIGRATION_KEY, MIGRATION_VAL);
        return new Set(DEFAULT_COLLAPSED);
      }
      const raw = localStorage.getItem(STORAGE_KEY);
      if (raw) {
        const arr = JSON.parse(raw);
        if (Array.isArray(arr)) return new Set(arr.filter((x): x is GraphID =>
          typeof x === 'string' && /^G[1-6]$/.test(x)));
      }
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
  const edgeCounts = useStore(s => s.edgeCountsByType);

  // groupTotal: sum of edge counts across all edge types in the group.
  // Renders as the badge next to each pill so the user can read axis
  // weight at a glance — Track D goal. 0-count groups get a 'g-empty'
  // marker class so styling can warn (italic / dim / ⚠).
  const groupTotal = (g: GraphGroupSpec): number => {
    let n = 0;
    for (const t of g.edges) n += edgeCounts[t] ?? 0;
    return n;
  };
  // Format big numbers compactly: 13426 → "13.4k", 758 → "758".
  const fmt = (n: number): string =>
    n >= 10000 ? `${(n / 1000).toFixed(1)}k`
    : n >= 1000 ? `${(n / 1000).toFixed(1)}k`
    : String(n);

  return (
    <div className="graph-pills" role="group" aria-label="6-graph axis toggles">
      {GRAPH_GROUPS.map(g => {
        const allOn = groupHasAllEdges(g, whitelist);
        const anyOn = groupHasAnyEdge(g, whitelist);
        const total = groupTotal(g);
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
        const countSuffix = total === 0
          ? ' (no edges in this graph)'
          : ` — ${total.toLocaleString()} edges total`;
        const title = isolation
          ? `${g.id} ${g.label}${countSuffix} — ${g.description}\nClick to focus this graph (replaces whitelist).`
          : `${g.id} ${g.label}${countSuffix} — ${g.description}\nClick to ${allOn ? 'turn all off' : 'turn all on'}.`;
        return (
          <button
            key={g.id}
            type="button"
            className={`graph-pill ${cls}${total === 0 ? ' g-empty' : ''}`}
            style={{ borderColor: hex(g.color) }}
            onClick={onClick}
            title={title}
          >
            <span className="graph-pill-dot" style={{ background: hex(g.color) }} />
            <span className="graph-pill-id">{g.id}</span>
            <span className="graph-pill-label">{g.label}</span>
            <span className="graph-pill-count">
              {total === 0 ? '⚠' : fmt(total)}
            </span>
          </button>
        );
      })}
    </div>
  );
}

// GraphModeToggle is a small button that flips graphModeIsolation. We
// keep the visible label deliberately short ("Solo") so it can sit on
// the same row as the section heading without wrapping at the panel's
// narrow end (240px clamp floor).
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
  // SSR-safe initial set: all six axes collapsed (matches the v2
  // default). A useEffect on mount pulls the user's saved choices
  // from localStorage, so the static export HTML and the hydrated
  // DOM agree on the first render (no React #418).
  const [collapsed, setCollapsed] = useState<Set<GraphID>>(() => new Set(DEFAULT_COLLAPSED));
  // mounted gates the persist effect — without it, the post-hydrate
  // restore would immediately write back over the user's stored value.
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setCollapsed(loadCollapsed());
    setMounted(true);
  }, []);

  useEffect(() => {
    if (mounted) saveCollapsed(collapsed);
  }, [collapsed, mounted]);

  // graphModeIsolation hydrates synchronously from localStorage in
  // store.ts (initGraphMode), so no useEffect is needed here. Setter
  // writes still happen in GraphModeToggle below.

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
