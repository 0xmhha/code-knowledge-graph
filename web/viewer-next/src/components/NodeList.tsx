'use client';

import { memo, useEffect, useMemo, useState } from 'react';
import { useStore } from '@/store/store';
import { detectMode, API, StaticAPI } from '@/lib/api';
import { usePersistedBool } from '@/lib/usePersistedState';
import type { IAPI } from '@/lib/api';
import type { GraphNode, NodeId } from '@/types';

interface Props {
  onPick: (id: NodeId) => void;
  // apiReady is false during the brief window between mount and the
  // detectMode() / first commit completing. Without it the empty
  // state read "No visible nodes — bootstrap may still be running."
  // for both the loading case AND the legitimately-empty case, which
  // confused users on small graphs.
  apiReady: boolean;
}

// PKG_PREVIEW caps the visible package rows above the expander. 25 hits
// the legibility ceiling at the panel's narrow end (240px clamp floor)
// without forcing the user to scroll past Packages to reach Visible
// Nodes. The expander reveals the full list when needed.
const PKG_PREVIEW = 25;

// PKG_FETCH_LIMIT bounds the one-shot package fetch on mount. Real-world
// repos rarely exceed a few hundred packages; 500 is generous headroom
// without risking an unbounded payload on monorepos.
const PKG_FETCH_LIMIT = 500;

function NodeListImpl({ onPick, apiReady }: Props) {
  const isSearch = useStore(s => s.searchResults.length > 0);
  const searchResults = useStore(s => s.searchResults);
  const searchQuery = useStore(s => s.searchQuery);
  const visibleIds = useStore(s => s.visibleIds);
  const nodes = useStore(s => s.nodes);
  const selectedId = useStore(s => s.selectedId);
  const anchorId = useStore(s => s.anchorId);
  const depth = useStore(s => s.depth);

  // Packages list — fetched once on mount via /api/nodes (parent="").
  // The boot path's topNodes('pagerank', 200, ['Commit']) ranks Package
  // nodes outside the seed on most graphs, so they exist in the data
  // store but are invisible in NodeList. Explicit fetch sidesteps that
  // and gives users a stable package picker independent of pagerank.
  const [packages, setPackages] = useState<GraphNode[]>([]);
  const [pkgExpanded, setPkgExpanded] = useState(false);
  // Section-open toggles for the two NodeList sections (Packages and
  // Visible Nodes). Persisted via localStorage so the user's collapse
  // preference survives reloads. Default both open so first paint
  // shows the data.
  // Sections default open on SSR + first render so static export HTML
  // matches the hydrated DOM. usePersistedBool flips the state from
  // localStorage on mount if the user previously collapsed.
  const [pkgSectionOpen, setPkgSectionOpen] = usePersistedBool('ckg.nodelist.pkgOpen', true);
  const [nodesSectionOpen, setNodesSectionOpen] = usePersistedBool('ckg.nodelist.nodesOpen', true);
  const togglePkg = () => setPkgSectionOpen(!pkgSectionOpen);
  const toggleNodes = () => setNodesSectionOpen(!nodesSectionOpen);

  useEffect(() => {
    if (!apiReady) return;
    let cancelled = false;
    (async () => {
      try {
        const mode = await detectMode();
        const a: IAPI = mode === 'static' ? new StaticAPI() : new API('');
        // parent="" returns Package nodes (top of pkg_tree). Backend
        // QueryNodes('', N) is the documented contract; static mode
        // mirrors it client-side. Sort alphabetically by qualified
        // name so the user sees a stable ordering regardless of
        // backend ORDER BY.
        const pkgs = await a.nodes('', PKG_FETCH_LIMIT);
        if (cancelled) return;
        const sorted = [...pkgs].sort((x, y) => {
          const ax = x.qualified_name ?? x.name ?? x.id;
          const bx = y.qualified_name ?? y.name ?? y.id;
          return ax.localeCompare(bx);
        });
        setPackages(sorted);
      } catch (e) {
        // Fail-soft: if the package fetch fails the rest of NodeList
        // still works. console.warn so the issue is visible without
        // crashing the panel.
        console.warn('package list fetch failed', e);
      }
    })();
    return () => { cancelled = true; };
  }, [apiReady]);

  const source = useMemo<GraphNode[]>(() => {
    if (isSearch) return searchResults;
    const arr: GraphNode[] = [];
    for (const id of visibleIds) {
      const n = nodes.get(id);
      if (n) arr.push(n);
    }
    return arr;
  }, [isSearch, searchResults, visibleIds, nodes]);

  const items = source.slice(0, 200);
  const titleText = isSearch ? '🔎 Search Results' : '👁 Visible Nodes';
  const countText = source.length > 200 ? `${source.length} (showing 200)` : `${source.length}`;

  let ctxText = '';
  if (!isSearch) {
    if (!anchorId) ctxText = 'root view · click a node to set anchor';
    else {
      const a = nodes.get(anchorId);
      const aName = a?.qualified_name || a?.name || anchorId;
      ctxText = `anchor: ${aName} · depth ${depth}`;
    }
  }

  // Three real empty states:
  //   1. API not ready → bootstrap genuinely in flight
  //   2. User typed a query but server returned nothing
  //   3. Bootstrap done but visibleIds is empty (rare; small graphs,
  //      or a filter dropping everything visible)
  let emptyMessage = '';
  if (items.length === 0) {
    if (!apiReady) {
      emptyMessage = 'Loading graph…';
    } else if (searchQuery.trim()) {
      emptyMessage = `No matches for «${searchQuery.trim()}». Try a partial identifier.`;
    } else {
      emptyMessage = 'Graph empty — try a search or click the home button.';
    }
  }

  // Show the package section only on the non-search root view; search
  // results have their own context and a phantom Packages list above
  // would just dilute the result density.
  const showPackages = !isSearch && packages.length > 0;
  const visiblePkgs = pkgExpanded ? packages : packages.slice(0, PKG_PREVIEW);
  const hiddenPkgs = packages.length - visiblePkgs.length;

  return (
    <div className="node-list">
      {showPackages && (
        <div className={`pkg-section${pkgSectionOpen ? '' : ' collapsed'}`}>
          <div
            className="pkg-section-header"
            onClick={togglePkg}
            role="button"
            tabIndex={0}
            onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') togglePkg(); }}
            title={pkgSectionOpen ? 'Collapse Packages section' : 'Expand Packages section'}
          >
            <span className="section-arrow">{pkgSectionOpen ? '▼' : '▶'}</span>
            <span>📦 Packages</span>
            <span className="pkg-section-count">{packages.length}</span>
          </div>
          {pkgSectionOpen && visiblePkgs.map(p => (
            <div
              key={p.id}
              className={`pkg-item${p.id === selectedId ? ' selected' : ''}`}
              title={p.qualified_name ?? p.name ?? p.id}
              onClick={() => onPick(p.id)}
            >
              <span className="pkg-glyph" aria-hidden="true">📦</span>
              <span className="pkg-name">{p.name ?? p.id}</span>
              {p.qualified_name && p.qualified_name !== p.name && (
                <span className="pkg-qname">{p.qualified_name}</span>
              )}
            </div>
          ))}
          {pkgSectionOpen && hiddenPkgs > 0 && (
            <div
              className="pkg-section-more"
              onClick={() => setPkgExpanded(true)}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') setPkgExpanded(true);
              }}
            >
              show all ({packages.length})
            </div>
          )}
          {pkgSectionOpen && pkgExpanded && packages.length > PKG_PREVIEW && (
            <div
              className="pkg-section-more"
              onClick={() => setPkgExpanded(false)}
              role="button"
              tabIndex={0}
              onKeyDown={(e) => {
                if (e.key === 'Enter' || e.key === ' ') setPkgExpanded(false);
              }}
            >
              show less
            </div>
          )}
        </div>
      )}
      <div
        className="listmeta"
        onClick={toggleNodes}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') toggleNodes(); }}
        title={nodesSectionOpen ? 'Collapse Visible Nodes section' : 'Expand Visible Nodes section'}
        style={{ cursor: 'pointer' }}
      >
        <div className="title">
          <span className="section-arrow">{nodesSectionOpen ? '▼' : '▶'}</span>
          {' '}{titleText} <span className="count">({countText})</span>
        </div>
        {ctxText && <div className="ctx">{ctxText}</div>}
      </div>
      {nodesSectionOpen && (
        items.length === 0 ? (
          <div style={{ padding: 12, color: '#666', fontSize: 11 }}>
            {emptyMessage}
          </div>
        ) : items.map(n => (
          <div
            key={n.id}
            className={`item${n.id === selectedId ? ' selected' : ''}`}
            title={n.qualified_name ?? ''}
            onClick={() => onPick(n.id)}
          >
            <div className="head"><span className="type">[{n.type}]</span> {n.name ?? n.id}</div>
            <div className="qname">{n.qualified_name ?? ''}</div>
            {n.file_path && <div className="file">{n.file_path}:{n.start_line ?? 0}</div>}
          </div>
        ))
      )}
    </div>
  );
}

export default memo(NodeListImpl);
