// src/search.js
// Debounced search input. Results are loaded into the store (so focusNode and
// the sidebar list can find them by id), made visible on the 3D graph, and
// stashed on `store.searchResults` so the list panel can render them. The
// first hit is auto-focused; subsequent picks come from clicking either the
// sidebar list or the 3D graph node.
export function wireSearch(input, api, store, onPick) {
  let timer;
  input.addEventListener('input', () => {
    clearTimeout(timer);
    const q = input.value.trim();
    if (!q) {
      // Empty query — clear results so the list panel falls back to "visible".
      store.searchResults = [];
      store.emit();
      return;
    }
    timer = setTimeout(async () => {
      try {
        const results = await api.search(q);
        console.log('search', { q, count: Array.isArray(results) ? results.length : '(non-array)', sample: Array.isArray(results) ? results.slice(0, 3) : results });
        if (Array.isArray(results) && results.length) {
          // Register results in the store so they can be focused / hovered /
          // listed without an extra round-trip.
          store.loadNodes(results);
          // Make all hits visible on the 3D canvas.
          const next = new Set(store.visibleIds);
          for (const r of results) next.add(r.id);
          store.setVisible([...next]);
          store.searchResults = results;
          store.emit();
          // Auto-focus the top hit so the right-hand detail panel populates.
          onPick(results[0].id);
        } else {
          // Explicit empty-state — list panel will render "No results."
          store.searchResults = [];
          store.emit();
        }
      } catch (e) {
        console.error('search failed', e);
        store.searchResults = [];
        store.emit();
      }
    }, 200);
  });
}
