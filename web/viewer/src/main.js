// src/main.js
import { API, StaticAPI, detectMode } from './api.js';
import { Store } from './store.js';
import { mountGraph } from './layout.js';
import { wireSearch } from './search.js';
import { renderList, renderDetail } from './panel.js';

// Transport selection happens once at boot. detectMode probes ./manifest.json
// — present in static export bundles, absent under `ckg serve`. Anything
// downstream sees the same API surface and stays transport-agnostic.
const mode = await detectMode();
const api = mode === 'static' ? new StaticAPI() : new API('');
const store = new Store();

(async () => {
  const manifest = await api.manifest();
  document.getElementById('src-info').textContent = manifest.src_root || '';
  if (manifest.graph_stale) {
    const banner = document.createElement('div');
    banner.className = 'stale-banner';
    banner.textContent = `⚠️ Graph built from ${manifest.src_commit} but src is now at ${manifest.current_commit}. Run \`ckg build\` to refresh.`;
    document.body.insertBefore(banner, document.body.firstChild);
  }
  // L0: render the top-level package nodes returned by /api/nodes?parent= .
  // The hierarchy endpoint returns a flat pkg_tree slice (no `.roots` field
  // — that was a stale assumption); top-level nodes are simply whatever the
  // store exposes when no parent is specified.
  const nodes = await api.nodes('', 5000);
  store.loadNodes(nodes);
  store.setVisible(nodes.map(n => n.id));
  console.log('viewer bootstrap', { nodes: nodes.length });
})();

// Mount the 3D graph once. It reads from `store` reactively (subscribe) and
// uses `api` for LOD-driven expansion. Safe to mount before bootstrap finishes
// because the store starts empty and `sync()` re-fires on every `setVisible`.
// `fg` is the 3d-force-graph instance — captured for future T26 camera focus.
const fg = mountGraph(document.getElementById('canvas'), store, api);

const detailEl = document.getElementById('node-detail');
const listEl = document.getElementById('node-list');
const searchEl = document.getElementById('search');

// focusNode: load the node + its edges + its direct children, dedupe against
// existing store, render the detail panel. Robust against the node not being
// in the store yet (search hits arrive via wireSearch, which now pre-loads
// results into the store, so this is a defensive fallback).
const focusNode = async (id) => {
  let node = store.nodes.get(id);
  if (!node) {
    // Last-resort: skip — wireSearch should have loaded the node already.
    console.warn('focusNode: id not in store', id);
    return;
  }
  store.selectedId = id;
  const [edges, children] = await Promise.all([
    api.edges([id]),
    api.nodes(id, 1000).catch(() => []),
  ]);
  const fresh = edges.filter(
    e => !store.edges.some(x => x.src === e.src && x.dst === e.dst && x.type === e.type)
  );
  let pushed = false;
  if (fresh.length) {
    store.edges = [...store.edges, ...fresh];
    pushed = true;
  }
  if (children.length) {
    store.loadNodes(children);
    const next = new Set(store.visibleIds);
    for (const c of children) next.add(c.id);
    store.setVisible([...next]);
    pushed = true;
  }
  // setVisible already emits; otherwise emit once so list highlight + canvas
  // pick up `selectedId` and any new edges.
  if (!pushed) store.emit();
  renderDetail(detailEl, api, node, edges);
};

// Click-on-node: expand its children + edges in place. 3d-force-graph emits
// onNodeClick with the node datum directly. Search and list-item picks
// funnel through the same focusNode so behaviour stays consistent.
fg.onNodeClick(node => {
  console.log('node clicked', node?.id, node?.qualified_name);
  if (node?.id) focusNode(node.id);
});

// Re-render the sidebar list on every store change (search results, visible
// set, selection). renderList is idempotent — lit-html diffs internally.
const refreshList = () => renderList(listEl, store, focusNode);
store.subscribe(refreshList);
refreshList();

wireSearch(searchEl, api, store, focusNode);

// Right-panel toggle. ⇆ button collapses #panel to 0px so the 3D canvas can
// use the full window — useful on small laptop screens. We also fire a resize
// event so 3d-force-graph picks up the new canvas dimensions immediately
// (the library wires its own resize observer but that lags one tick).
document.getElementById('panel-toggle')?.addEventListener('click', () => {
  document.getElementById('app').classList.toggle('no-panel');
  setTimeout(() => window.dispatchEvent(new Event('resize')), 130);
});

// Zoom controls — adjust camera Z. Smaller Z = closer (zoom in).
const zoomBy = (factor) => {
  const pos = fg.cameraPosition();
  fg.cameraPosition({ z: Math.max(50, pos.z * factor) }, undefined, 200);
};
document.getElementById('zoom-in')?.addEventListener('click', () => zoomBy(0.7));
document.getElementById('zoom-out')?.addEventListener('click', () => zoomBy(1.4));
document.getElementById('zoom-reset')?.addEventListener('click', () => {
  fg.cameraPosition({ x: 0, y: 0, z: 1500 }, { x: 0, y: 0, z: 0 }, 400);
});

// Keyboard shortcuts: + / = zoom in, - zoom out, 0 reset, / focus search,
// Escape to clear selection. Only when search box is not focused.
window.addEventListener('keydown', (ev) => {
  if (document.activeElement?.id === 'search') {
    if (ev.key === 'Escape') document.activeElement.blur();
    return;
  }
  if (ev.key === '=' || ev.key === '+') zoomBy(0.7);
  else if (ev.key === '-') zoomBy(1.4);
  else if (ev.key === '0') document.getElementById('zoom-reset')?.click();
  else if (ev.key === '/') { ev.preventDefault(); document.getElementById('search')?.focus(); }
});

console.log('viewer ready', { fg, store });
