// src/panel.js
// Selection panel using lit-html. `render(tpl, el)` mutates `el` in place;
// the async `api.blob(node.id)` fetch fills in the source preview after the
// initial render (progressive enhancement — the panel shows metadata first,
// then the source blob arrives). lit-html `${...}` interpolation is text-safe.
import { html, render } from 'lit-html';

export function renderPanel(el, api, node, edges) {
  const tpl = html`
    <h3>${node.name}</h3>
    <div><strong>Type:</strong> ${node.type}</div>
    <div><strong>Qualified:</strong> ${node.qualified_name}</div>
    <div><strong>File:</strong> ${node.file_path}:${node.start_line}</div>
    <div><strong>Confidence:</strong> ${node.confidence}</div>
    <div><strong>Usage:</strong> ${node.usage_score?.toFixed(2) ?? 0}</div>
    <h4>Edges</h4>
    <div>In: ${edges.filter(e => e.dst === node.id).length}</div>
    <div>Out: ${edges.filter(e => e.src === node.id).length}</div>
    <h4>Source</h4>
    <pre id="blob" style="white-space: pre-wrap; max-height: 300px; overflow: auto; background: #0d0e10; padding: 6px;"></pre>
  `;
  render(tpl, el);
  api.blob(node.id).then(text => { el.querySelector('#blob').textContent = text; });
}
