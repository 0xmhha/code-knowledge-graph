// src/panel.js
// Two render functions:
//   renderList(el, store, onClick)   — sidebar top: search results or visible
//   renderDetail(el, api, node, edges) — sidebar bottom: selected node detail
import { html, render } from 'lit-html';

// renderList shows search results when present; otherwise the currently
// visible nodes. Capped at 200 items to keep the DOM cheap on large graphs.
export function renderList(el, store, onClick) {
  const isSearch = (store.searchResults?.length ?? 0) > 0;
  const source = isSearch
    ? store.searchResults
    : [...store.visibleIds].map(id => store.nodes.get(id)).filter(Boolean);
  const items = source.slice(0, 200);
  const meta = isSearch
    ? `🔎 ${source.length} search result${source.length === 1 ? '' : 's'}${source.length > 200 ? ' (showing 200)' : ''}`
    : `👁 ${source.length} visible node${source.length === 1 ? '' : 's'}${source.length > 200 ? ' (showing 200)' : ''}`;

  const tpl = html`
    <div class="listmeta">${meta}</div>
    ${items.map(n => html`
      <div class="item ${n.id === store.selectedId ? 'selected' : ''}"
           style=${n.id === store.selectedId ? 'background:#2a3140;' : ''}
           @click=${() => onClick(n.id)}
           title=${n.qualified_name || ''}>
        <div class="head"><span class="type">[${n.type}]</span> ${n.name || n.id}</div>
        <div class="qname">${n.qualified_name || ''}</div>
        ${n.file_path ? html`<div class="file">${n.file_path}:${n.start_line ?? 0}</div>` : ''}
      </div>
    `)}
  `;
  render(tpl, el);
}

// renderDetail shows full info on the selected node. Source blob is fetched
// asynchronously; the panel renders immediately and the <pre> fills in once
// the blob arrives.
export function renderDetail(el, api, node, edges) {
  const tpl = html`
    <h3 style="margin:0 0 8px 0;font-size:14px;">${node.name}</h3>
    <div><strong>Type:</strong> ${node.type}</div>
    <div><strong>Qualified:</strong> <span style="font-family:ui-monospace,monospace;font-size:12px;">${node.qualified_name}</span></div>
    <div><strong>File:</strong> ${node.file_path}:${node.start_line}</div>
    <div><strong>Lang:</strong> ${node.language ?? ''} · <strong>Conf:</strong> ${node.confidence ?? ''}</div>
    <div><strong>Usage:</strong> ${(node.usage_score ?? 0).toFixed(2)} · <strong>PR:</strong> ${(node.pagerank ?? 0).toExponential(2)}</div>
    <div><strong>Edges:</strong> in ${edges.filter(e => e.dst === node.id).length} · out ${edges.filter(e => e.src === node.id).length}</div>
    ${node.signature ? html`<div style="margin-top:6px;color:#9ad;font-style:italic;font-family:ui-monospace,monospace;font-size:11px;">${node.signature}</div>` : ''}
    <h4 style="margin:10px 0 4px 0;font-size:12px;">Source</h4>
    <pre id="blob" style="white-space: pre-wrap; max-height: 240px; overflow: auto; background: #0d0e10; padding: 6px; font-size: 11px; border:1px solid #2a2c30;"></pre>
  `;
  render(tpl, el);
  api.blob(node.id).then(text => {
    const blobEl = el.querySelector('#blob');
    if (blobEl) blobEl.textContent = text || '(no source blob — non-leaf node)';
  });
}

// Backwards-compat alias used by older imports.
export const renderPanel = renderDetail;
