// src/main.js
import { API } from './api.js';
import { Store } from './store.js';
import { mountGraph } from './layout.js';

const api = new API('');
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
mountGraph(document.getElementById('canvas'), store, api);
