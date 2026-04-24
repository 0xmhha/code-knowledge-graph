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
  const tree = await api.hierarchy('pkg');
  // L0: render top-level nodes only.
  const top = tree.roots || [];
  const nodes = await api.nodes('', 5000);
  store.loadNodes(nodes);
  store.setVisible(top.map(t => t.id));
  console.log('viewer bootstrap', { nodes: nodes.length, top });
})();

// Mount the 3D graph once. It reads from `store` reactively (subscribe) and
// uses `api` for LOD-driven expansion. Safe to mount before bootstrap finishes
// because the store starts empty and `sync()` re-fires on every `setVisible`.
// `fg` is the 3d-force-graph instance — captured for future T26 camera focus.
const fg = mountGraph(document.getElementById('canvas'), store, api);

const panelEl = document.getElementById('panel');
const searchEl = document.getElementById('search');

// focusNode: load the node + its edges, dedupe against existing store.edges,
// render the selection panel. We call store.emit() after appending edges so
// the layout's `sync()` listener re-pushes graph data into 3d-force-graph;
// without it the new edges sit in the store but never reach the canvas.
const focusNode = async (id) => {
  const node = store.nodes.get(id);
  if (!node) return;
  const edges = await api.edges([id]);
  const fresh = edges.filter(
    e => !store.edges.some(x => x.src === e.src && x.dst === e.dst && x.type === e.type)
  );
  if (fresh.length) {
    store.edges = [...store.edges, ...fresh];
    store.emit();
  }
  renderPanel(panelEl, api, node, edges);
};

wireSearch(searchEl, api, store, focusNode);
