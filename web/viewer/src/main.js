// src/main.js
import { API, StaticAPI, detectMode } from './api.js';
import { Store } from './store.js';
import { mountGraph } from './layout.js';
import { wireSearch } from './search.js';
import { renderPanel } from './panel.js';

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

const panelEl = document.getElementById('panel');
const searchEl = document.getElementById('search');

// focusNode: load the node + its edges + its direct children, dedupe against
// existing store, render the selection panel. We call store.emit() after the
// store mutations so the layout's `sync()` listener re-pushes graph data into
// 3d-force-graph; without it the new edges/nodes sit in the store but never
// reach the canvas.
const focusNode = async (id) => {
  const node = store.nodes.get(id);
  if (!node) return;
  const [edges, children] = await Promise.all([
    api.edges([id]),
    api.nodes(id, 1000).catch(() => []),
  ]);
  const fresh = edges.filter(
    e => !store.edges.some(x => x.src === e.src && x.dst === e.dst && x.type === e.type)
  );
  let dirty = false;
  if (fresh.length) {
    store.edges = [...store.edges, ...fresh];
    dirty = true;
  }
  if (children.length) {
    store.loadNodes(children);
    const next = new Set(store.visibleIds);
    for (const c of children) next.add(c.id);
    store.setVisible([...next]);
    dirty = true;
  }
  if (dirty && !children.length) store.emit();
  renderPanel(panelEl, api, node, edges);
};

// Click-on-node: expand its children + edges in place. 3d-force-graph emits
// onNodeClick with the node datum directly. Search results funnel through the
// same focusNode so click vs. search behaviour stays consistent.
fg.onNodeClick(node => focusNode(node.id));

wireSearch(searchEl, api, store, focusNode);
