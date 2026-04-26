// src/panel.js
// Plain-DOM rendering. Two functions:
//   renderList(el, store, onClick)   — sidebar top: search results or visible
//   renderDetail(el, api, node, edges) — sidebar bottom: selected node detail
// Plain DOM (vs lit-html) keeps the dependency surface small AND makes
// rendering bugs easy to debug — the DOM you see is the DOM the code wrote.

function escapeHtml(s) {
  if (s == null) return '';
  return String(s)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

export function renderList(el, store, onClick) {
  const isSearch = (store.searchResults?.length ?? 0) > 0;
  const source = isSearch
    ? store.searchResults
    : [...store.visibleIds].map(id => store.nodes.get(id)).filter(Boolean);
  const items = source.slice(0, 200);
  const meta = isSearch
    ? `🔎 ${source.length} search result${source.length === 1 ? '' : 's'}${source.length > 200 ? ' (showing 200)' : ''}`
    : `👁 ${source.length} visible node${source.length === 1 ? '' : 's'}${source.length > 200 ? ' (showing 200)' : ''}`;

  console.debug('renderList', { isSearch, total: source.length, shown: items.length, selectedId: store.selectedId });

  // Build via DocumentFragment for one paint.
  const frag = document.createDocumentFragment();

  const metaEl = document.createElement('div');
  metaEl.className = 'listmeta';
  metaEl.textContent = meta;
  frag.appendChild(metaEl);

  if (items.length === 0) {
    const empty = document.createElement('div');
    empty.style.cssText = 'padding:12px;color:#666;font-size:11px;';
    empty.textContent = isSearch ? 'No results.' : 'No visible nodes — bootstrap may still be running.';
    frag.appendChild(empty);
  } else {
    for (const n of items) {
      const item = document.createElement('div');
      item.className = 'item';
      if (n.id === store.selectedId) item.style.background = '#2a3140';
      item.title = n.qualified_name || '';
      item.innerHTML =
        `<div class="head"><span class="type">[${escapeHtml(n.type)}]</span> ${escapeHtml(n.name || n.id)}</div>` +
        `<div class="qname">${escapeHtml(n.qualified_name || '')}</div>` +
        (n.file_path ? `<div class="file">${escapeHtml(n.file_path)}:${n.start_line ?? 0}</div>` : '');
      item.addEventListener('click', () => onClick(n.id));
      frag.appendChild(item);
    }
  }

  el.replaceChildren(frag);
}

export function renderDetail(el, api, node, edges) {
  const inN = edges.filter(e => e.dst === node.id).length;
  const outN = edges.filter(e => e.src === node.id).length;
  el.innerHTML =
    `<h3>${escapeHtml(node.name)}</h3>` +
    `<div><strong>Type:</strong> ${escapeHtml(node.type)}</div>` +
    `<div><strong>Qualified:</strong> <span style="font-family:ui-monospace,monospace;font-size:11px;word-break:break-all;">${escapeHtml(node.qualified_name)}</span></div>` +
    `<div><strong>File:</strong> ${escapeHtml(node.file_path)}:${node.start_line}</div>` +
    `<div><strong>Lang:</strong> ${escapeHtml(node.language ?? '')} · <strong>Conf:</strong> ${escapeHtml(node.confidence ?? '')}</div>` +
    `<div><strong>Usage:</strong> ${(node.usage_score ?? 0).toFixed(2)} · <strong>PR:</strong> ${(node.pagerank ?? 0).toExponential(2)}</div>` +
    `<div><strong>Edges:</strong> in ${inN} · out ${outN}</div>` +
    (node.signature ? `<div style="margin-top:6px;color:#9ad;font-style:italic;font-family:ui-monospace,monospace;font-size:11px;">${escapeHtml(node.signature)}</div>` : '') +
    `<h4>Source</h4>` +
    `<pre id="blob" style="white-space: pre-wrap; max-height: 200px; overflow: auto; background: #0d0e10; padding: 6px; border:1px solid #2a2c30;">loading…</pre>`;
  api.blob(node.id).then(text => {
    const blobEl = el.querySelector('#blob');
    if (blobEl) blobEl.textContent = text || '(no source blob — non-leaf node)';
  }).catch(() => {
    const blobEl = el.querySelector('#blob');
    if (blobEl) blobEl.textContent = '(blob fetch failed)';
  });
}

// Backwards-compat alias.
export const renderPanel = renderDetail;
