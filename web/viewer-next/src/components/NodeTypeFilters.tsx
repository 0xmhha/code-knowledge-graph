'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useStore } from '@/store/store';

// Five visual groups for legibility. Order mirrors the spec § 5.1
// taxonomy (symbols → members → containers → statements → concurrency/
// VCS). A node type listed here but absent from the loaded graph is
// silently hidden in the UI — only types observed in the current
// nodes Map render checkboxes.
//
// The full set covers every NodeType in pkg/types/enums.go (33 kinds).
// Class / Enum / Contract / Mapping / Event / MessageType / Modifier /
// Decorator / Constructor / Parameter / LocalVariable / Export aren't
// listed in the user-facing groups because the self-graph never
// surfaces them; if a future graph emits them they'll be visible by
// default (the default whitelist accepts unknown types) but won't
// have a UI checkbox until they're added here.
interface NodeTypeGroupSpec {
  id: string;
  label: string;
  description: string;
  types: ReadonlyArray<string>;
}

const NODE_TYPE_GROUPS: ReadonlyArray<NodeTypeGroupSpec> = [
  {
    id: 'symbols',
    label: 'Symbols',
    description: 'Top-level type and function declarations',
    types: ['Function', 'Method', 'Type', 'Struct', 'Interface', 'TypeAlias',
            'Class', 'Enum', 'Contract', 'Constructor', 'Modifier'],
  },
  {
    id: 'members',
    label: 'Members',
    description: 'Per-symbol storage: fields, variables, constants',
    types: ['Field', 'Variable', 'Constant', 'Parameter', 'LocalVariable'],
  },
  {
    id: 'containers',
    label: 'Containers',
    description: 'File-/package-level scopes and route entrypoints',
    types: ['Package', 'File', 'Endpoint', 'MessageType'],
  },
  {
    id: 'statements',
    label: 'Statements',
    description: 'Control-flow and import statement nodes (verbose)',
    types: ['IfStmt', 'LoopStmt', 'ReturnStmt', 'SwitchStmt', 'CallSite',
            'Import', 'Export', 'Decorator', 'Event', 'Mapping'],
  },
  {
    id: 'concurrency',
    label: 'Concurrency / VCS',
    description: 'Goroutines, channels, mutexes, and git Commit / Hunk nodes',
    types: ['Goroutine', 'Channel', 'Mutex', 'Commit', 'Hunk'],
  },
];

const STORAGE_KEY = 'ckg.nodeTypeFiltersCollapsed';

// Default-collapsed: all five groups. Mirrors EdgeTypeFilters' default
// collapsed state — expanded sublists at panel-min width consume too
// much vertical space and push NodeList off-screen. Users expand a
// group when they want fine-grained per-type control.
const DEFAULT_COLLAPSED: ReadonlyArray<string> =
  NODE_TYPE_GROUPS.map(g => g.id);

function loadCollapsed(): Set<string> {
  if (typeof localStorage === 'undefined') return new Set(DEFAULT_COLLAPSED);
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) {
      const arr = JSON.parse(raw);
      if (Array.isArray(arr)) {
        return new Set(arr.filter((x): x is string => typeof x === 'string'));
      }
    }
  } catch { /* ignore */ }
  return new Set(DEFAULT_COLLAPSED);
}

function saveCollapsed(s: Set<string>): void {
  if (typeof localStorage === 'undefined') return;
  try { localStorage.setItem(STORAGE_KEY, JSON.stringify([...s])); }
  catch { /* ignore */ }
}

interface GroupSectionProps {
  group: NodeTypeGroupSpec;
  // observedTypes: only types actually present in the loaded graph.
  // Filtering at the group level avoids rendering checkboxes for
  // types the user could never enable usefully (no nodes to gate).
  observedTypes: ReadonlySet<string>;
  collapsed: boolean;
  onToggleCollapse: () => void;
}

function GroupSection({ group, observedTypes, collapsed, onToggleCollapse }: GroupSectionProps) {
  const whitelist = useStore(s => s.nodeTypeWhitelist);
  const toggle = useStore(s => s.toggleNodeType);
  const setBulk = useStore(s => s.setNodeTypeWhitelistBulk);

  // Only show types observed in the graph. Filtering here keeps the UI
  // honest — a "Concurrency" group with zero Goroutine/Channel/Mutex
  // nodes shouldn't pretend they're toggleable.
  const presentTypes = useMemo(
    () => group.types.filter(t => observedTypes.has(t)),
    [group.types, observedTypes],
  );

  // Hooks must be declared before any early return. allOn captured into
  // the useCallback closure means we can't lift only the callback;
  // instead we compute everything that hooks depend on, then return
  // null below if presentTypes is empty (cheap render — the JSX block
  // beneath the guard is the only payload).
  const allOn = presentTypes.every(t => whitelist.has(t));
  const anyOn = presentTypes.some(t => whitelist.has(t));
  const enabledCount = presentTypes.reduce(
    (acc, t) => acc + (whitelist.has(t) ? 1 : 0), 0);

  const groupClass = allOn ? 'all-on' : (anyOn ? 'partial' : 'all-off');
  const groupLabel = allOn ? 'all' : (anyOn ? 'some' : 'none');

  const onGroupToggle = useCallback((ev: React.MouseEvent) => {
    ev.stopPropagation();  // don't trigger collapse
    setBulk(presentTypes, !allOn);
  }, [setBulk, presentTypes, allOn]);

  // If no type from this group exists in the data, hide the group
  // entirely instead of rendering an empty header. Users with a small
  // graph (e.g. no concurrency primitives) shouldn't see a phantom
  // "Concurrency / VCS (0/0)" section.
  if (presentTypes.length === 0) return null;

  return (
    <div className="node-type-group">
      <div className="node-type-group-header" onClick={onToggleCollapse} title={group.description}>
        <span className="node-type-group-arrow">{collapsed ? '▶' : '▼'}</span>
        <span className="node-type-group-label">{group.label}</span>
        <span className="node-type-group-count">{enabledCount}/{presentTypes.length}</span>
        <button
          type="button"
          className={`node-type-group-toggle ${groupClass}`}
          onClick={onGroupToggle}
          title={`Group toggle: currently ${groupLabel}. Click to ${allOn ? 'turn all off' : 'turn all on'}.`}
        >
          {groupLabel}
        </button>
      </div>
      {!collapsed && (
        <div className="node-type-group-types">
          {presentTypes.map(t => (
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

export default function NodeTypeFilters() {
  const nodes = useStore(s => s.nodes);
  // SSR-safe initial value (all groups collapsed). A useEffect on mount
  // pulls the user's saved state from localStorage so the static
  // export HTML and the hydrated DOM agree on the first render
  // (avoids React #418).
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set(DEFAULT_COLLAPSED));
  // mounted flag prevents the post-hydrate setCollapsed from being
  // immediately persisted back over the user's saved value — only
  // user-driven state changes should write to localStorage.
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setCollapsed(loadCollapsed());
    setMounted(true);
  }, []);

  useEffect(() => {
    if (mounted) saveCollapsed(collapsed);
  }, [collapsed, mounted]);

  // Discover the set of node types currently in the graph. Iterating
  // a Map of 5–10K entries on every store change would be wasteful, so
  // useMemo scopes recomputation to nodes-Map identity changes
  // (loadNodes replaces the Map ref).
  const observedTypes = useMemo(() => {
    const s = new Set<string>();
    for (const n of nodes.values()) {
      if (n.type) s.add(n.type);
    }
    return s;
  }, [nodes]);

  const onToggleCollapse = useCallback((id: string) => {
    setCollapsed(prev => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id); else next.add(id);
      return next;
    });
  }, []);

  return (
    <div className="node-type-filters">
      <div className="node-type-filters-header">
        <h4>Node Types</h4>
      </div>
      {NODE_TYPE_GROUPS.map(g => (
        <GroupSection
          key={g.id}
          group={g}
          observedTypes={observedTypes}
          collapsed={collapsed.has(g.id)}
          onToggleCollapse={() => onToggleCollapse(g.id)}
        />
      ))}
    </div>
  );
}
