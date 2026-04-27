'use client';

import { memo, useMemo } from 'react';
import { useStore } from '@/store/store';
import type { GraphNode, NodeId } from '@/types';

interface Props { onPick: (id: NodeId) => void; }

function NodeListImpl({ onPick }: Props) {
  const isSearch = useStore(s => s.searchResults.length > 0);
  const searchResults = useStore(s => s.searchResults);
  const visibleIds = useStore(s => s.visibleIds);
  const nodes = useStore(s => s.nodes);
  const selectedId = useStore(s => s.selectedId);
  const anchorId = useStore(s => s.anchorId);
  const depth = useStore(s => s.depth);

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

  return (
    <div className="node-list">
      <div className="listmeta">
        <div className="title">{titleText} <span className="count">({countText})</span></div>
        {ctxText && <div className="ctx">{ctxText}</div>}
      </div>
      {items.length === 0 ? (
        <div style={{ padding: 12, color: '#666', fontSize: 11 }}>
          {isSearch ? 'No results.' : 'No visible nodes — bootstrap may still be running.'}
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
      ))}
    </div>
  );
}

export default memo(NodeListImpl);
