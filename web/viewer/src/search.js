// src/search.js
// Debounced search input. On each keystroke we cancel the previous timer and
// schedule a fetch 200ms later — this keeps the network quiet while the user
// is still typing. V0 logs the top-5 results to console and auto-focuses the
// first hit; a richer dropdown is a T26/V1 polish item.
export function wireSearch(input, api, store, onPick) {
  let timer;
  input.addEventListener('input', () => {
    clearTimeout(timer);
    const q = input.value.trim();
    if (!q) return;
    timer = setTimeout(async () => {
      const results = await api.search(q);
      console.log('search', q, results.slice(0, 5));
      if (results[0]) onPick(results[0].id);
    }, 200);
  });
}
