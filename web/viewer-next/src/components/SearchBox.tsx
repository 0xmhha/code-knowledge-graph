'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { useStore } from '@/store/store';
import type { IAPI } from '@/lib/api';
import type { NodeId } from '@/types';

interface Props { api: IAPI; }

export default function SearchBox({ api }: Props) {
  const [q, setQ] = useState('');
  const setSearchResults = useStore(s => s.setSearchResults);
  const setSearchQuery = useStore(s => s.setSearchQuery);
  const setSearchHighlightIds = useStore(s => s.setSearchHighlightIds);
  const loadNodes = useStore(s => s.loadNodes);
  const addEdges = useStore(s => s.addEdges);
  const commit = useStore(s => s.commit);
  const inputRef = useRef<HTMLInputElement>(null);

  // Instant highlight (projector-style): every keystroke substring-
  // matches the ALREADY-LOADED nodes and tints them on the canvas —
  // no debounce, no fetch, no view change. The backend search below
  // (debounced) unions its results in once they land, catching nodes
  // the client-side pass can't know about. HIGHLIGHT_CAP bounds the
  // per-keystroke material churn on generic 1–2 letter queries.
  const HIGHLIGHT_CAP = 800;
  useEffect(() => {
    const ql = q.trim().toLowerCase();
    if (!ql) {
      setSearchHighlightIds(new Set());
      return;
    }
    const ids = new Set<NodeId>();
    for (const n of useStore.getState().nodes.values()) {
      if (ids.size >= HIGHLIGHT_CAP) break;
      if (
        n.name?.toLowerCase().includes(ql) ||
        n.qualified_name?.toLowerCase().includes(ql)
      ) ids.add(n.id);
    }
    setSearchHighlightIds(ids);
  }, [q, setSearchHighlightIds]);

  // clearSearch resets the query AND reverts visibleIds to the most
  // recent non-search root snapshot. Without the visibleIds revert,
  // search-introduced nodes would linger on the canvas after the user
  // emptied the input — the original V1 wiring just hid them from the
  // sidebar list, which made the UI feel "stuck on search results".
  const clearSearch = useCallback(() => {
    setQ('');
    setSearchQuery('');
    setSearchResults([]);
    setSearchHighlightIds(new Set());
    const cur = useStore.getState();
    // Search clear is part of the "leave search context" reset — wipe
    // the dim set so a stale impact spotlight from before the search
    // doesn't linger on the reverted root view.
    cur.clearDimmedNodes();
    commit({
      visibleIds: new Set(cur.visibleRootIds),
      // Preserve the trace's focus distances if any — clearing search reverts
      // to the pre-search view, which may be a trace; we want its anchor halo
      // to come back too. When the pre-search view was a fresh boot (no
      // trace), focusDistance is already empty so this is a no-op.
      focusDistance: cur.focusDistance,
      reason: 'search-pick',
    });
    inputRef.current?.focus();
  }, [setSearchResults, setSearchQuery, setSearchHighlightIds, commit]);

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

  // Mirror the input value into the store so other components (NodeList
  // empty state) can show the actual query the user typed.
  useEffect(() => { setSearchQuery(q); }, [q, setSearchQuery]);

  // External resets (Home button → store.setState({ searchQuery: '' }))
  // must visibly clear the input. We watch storeQuery only — including
  // `q` in deps caused a race: when the user typed 'S' both the q-sync
  // effect (line 61, writes q→store) and this guard ran in the same
  // render cycle. The guard captured a stale storeQuery (=='') from
  // the prior render while q had already advanced to 'S', so the
  // condition `storeQuery==='' && q!==''` fired and reset the input
  // every keystroke. Reading q via a ref breaks that loop: the effect
  // only fires on storeQuery transitions (true external resets), not
  // on every keystroke.
  const storeQuery = useStore(s => s.searchQuery);
  const qRef = useRef(q);
  qRef.current = q;
  useEffect(() => {
    if (storeQuery === '' && qRef.current !== '') {
      setQ('');
      setSearchResults([]);
      setSearchHighlightIds(new Set());
    }
  }, [storeQuery, setSearchResults, setSearchHighlightIds]);

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

        // Union backend hits into the live highlight — they can include
        // nodes the instant client-side pass couldn't see (not yet
        // loaded, or matched on backend-only fields).
        const hl = new Set(useStore.getState().searchHighlightIds);
        for (const r of results) hl.add(r.id);
        setSearchHighlightIds(hl);

        // Push results onto the canvas as well as the sidebar — without
        // this, hits show up in the list but never appear in the graph,
        // which makes search feel broken (V1-5).
        //
        // Critically: union onto visibleRootIds (the stable boot/trace
        // snapshot), NOT the current visibleIds. Unioning onto current
        // would accumulate hits across consecutive queries until we hit
        // MAX_VISIBLE for no good reason. Each new query REPLACES the
        // previous query's contribution while preserving the user's
        // boot/trace context.
        const ids = results.map(n => n.id);
        const fresh = await api.edges(ids);
        if (fresh.length) addEdges(fresh);

        const cur = useStore.getState();
        const next = new Set<NodeId>(cur.visibleRootIds);
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
  }, [q, api, setSearchResults, setSearchHighlightIds, loadNodes, addEdges, commit]);

  return (
    <span className="search-wrap">
      <input
        ref={inputRef}
        className="search"
        type="text"
        placeholder="search… (/) "
        value={q}
        onChange={e => setQ(e.target.value)}
        autoComplete="off"
      />
      {q && (
        <button
          type="button"
          className="search-clear"
          onClick={clearSearch}
          title="Clear search (revert to root view)"
          aria-label="Clear search"
        >
          ✕
        </button>
      )}
    </span>
  );
}
