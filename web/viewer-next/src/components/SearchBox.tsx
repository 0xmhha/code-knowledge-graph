'use client';

import { useEffect, useRef, useState } from 'react';
import { useStore } from '@/store/store';
import type { IAPI } from '@/lib/api';

interface Props { api: IAPI; }

export default function SearchBox({ api }: Props) {
  const [q, setQ] = useState('');
  const setSearchResults = useStore(s => s.setSearchResults);
  const loadNodes = useStore(s => s.loadNodes);
  const addEdges = useStore(s => s.addEdges);
  const commit = useStore(s => s.commit);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const handler = (ev: KeyboardEvent) => {
      if (ev.key === '/' && document.activeElement?.tagName !== 'INPUT') {
        ev.preventDefault();
        inputRef.current?.focus();
      }
      if (ev.key === 'Escape' && document.activeElement === inputRef.current) {
        inputRef.current?.blur();
      }
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);

  useEffect(() => {
    if (!q.trim()) {
      setSearchResults([]);
      return;
    }
    const t = setTimeout(async () => {
      try {
        const results = await api.search(q.trim());
        if (!results.length) {
          setSearchResults([]);
          return;
        }
        loadNodes(results);
        setSearchResults(results);

        // Push results onto the canvas as well as the sidebar — without
        // this, hits show up in the list but never appear in the graph,
        // which makes search feel broken (V1-5). We extend (∪) the
        // current visible set rather than replace so the user keeps
        // their existing context — typing while exploring shouldn't
        // wipe the canvas.
        const ids = results.map(n => n.id);
        const fresh = await api.edges(ids);
        if (fresh.length) addEdges(fresh);

        const cur = useStore.getState();
        const next = new Set(cur.visibleIds);
        for (const id of ids) next.add(id);
        commit({
          visibleIds: next,
          focusDistance: cur.focusDistance,
          reason: 'search-pick',
        });
      } catch (e) {
        console.error('search failed', e);
        setSearchResults([]);
      }
    }, 200);
    return () => clearTimeout(t);
  }, [q, api, setSearchResults, loadNodes, addEdges, commit]);

  return (
    <input
      ref={inputRef}
      className="search"
      type="text"
      placeholder="search… (/) "
      value={q}
      onChange={e => setQ(e.target.value)}
      autoComplete="off"
    />
  );
}
