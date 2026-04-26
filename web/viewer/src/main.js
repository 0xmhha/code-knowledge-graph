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

// Mount the graph (3D by default; remembers user preference in localStorage).
// Safe to mount before the boot IIFE finishes — the store starts empty and
// sync() re-fires on every setVisible.
let viewMode = (localStorage.getItem('ckg.viewMode') === '2d') ? '2d' : '3d';
const canvasEl = document.getElementById('canvas');
let fg = mountGraph(canvasEl, store, api, viewMode);

const detailEl = document.getElementById('node-detail');
const listEl = document.getElementById('node-list');
const searchEl = document.getElementById('search');

// CHILDREN_PER_EXPAND caps how many children one expand-click can reveal.
// 200K-node corpora produce packages with hundreds of files and files with
// hundreds of statements; uncapped expands push the visible set past
// 3d-force-graph's interactive sweet spot. See docs/VIEWER-ROADMAP.md L3.
const CHILDREN_PER_EXPAND = 100;

// collectExpandedDescendants walks the store's expanded-tree rooted at id,
// returning every transitively-revealed descendant. Used by collapse so a
// re-click on a parent rolls up the entire sub-tree, not just one level.
function collectExpandedDescendants(id) {
  const out = new Set();
  const walk = (i) => {
    const kids = store.expanded.get(i);
    if (!kids) return;
    for (const k of kids) {
      out.add(k);
      walk(k);
    }
  };
  walk(id);
  return out;
}

// focusNode: load the node + its edges + its direct children, dedupe against
// existing store, render the detail panel. Toggles: clicking an already-
// expanded node collapses its descendants instead of re-expanding.
const focusNode = async (id) => {
  const node = store.nodes.get(id);
  if (!node) {
    console.warn('focusNode: id not in store', id);
    return;
  }
  store.selectedId = id;

  // Toggle: if this node is already expanded, collapse the entire sub-tree
  // and stop. The detail panel still re-renders so the user can re-check
  // the metadata after collapsing.
  if (store.expanded.has(id)) {
    const toRemove = collectExpandedDescendants(id);
    if (toRemove.size) {
      const next = new Set(store.visibleIds);
      for (const cid of toRemove) {
        next.delete(cid);
        store.expanded.delete(cid);
      }
      store.expanded.delete(id);
      store.setVisible([...next]);
    } else {
      store.expanded.delete(id);
      store.emit();
    }
    // Re-render detail without re-fetching edges (we already showed them
    // when the node was first focused).
    const cachedEdges = store.edges.filter(
      e => e.src === id || e.dst === id
    );
    renderDetail(detailEl, api, node, cachedEdges);
    return;
  }

  // Expand path: defensive fetches with array guards so one bad response
  // can't crash the interaction.
  const [edgesRaw, childrenRaw] = await Promise.all([
    api.edges([id]).catch(err => { console.warn('edges fetch failed', id, err); return []; }),
    api.nodes(id, CHILDREN_PER_EXPAND).catch(err => { console.warn('children fetch failed', id, err); return []; }),
  ]);
  const edges = Array.isArray(edgesRaw) ? edgesRaw : [];
  const children = Array.isArray(childrenRaw) ? childrenRaw.filter(c => c && c.id) : [];

  const fresh = edges.filter(
    e => e && e.src && e.dst && !store.edges.some(x => x.src === e.src && x.dst === e.dst && x.type === e.type)
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
    store.expanded.set(id, new Set(children.map(c => c.id)));
    pushed = true;
  }
  if (!pushed) store.emit();
  renderDetail(detailEl, api, node, edges);
};

// Click-on-node: expand its children + edges in place. Both 2D and 3D
// libraries emit onNodeClick with the node datum directly. Search and
// list-item picks funnel through the same focusNode so behaviour stays
// consistent.
const wireFG = (g) => {
  g.onNodeClick(node => {
    console.log('node clicked', node?.id, node?.qualified_name);
    if (node?.id) focusNode(node.id);
  });
};
wireFG(fg);

// Re-render the sidebar list on every store change (search results, visible
// set, selection). renderList is idempotent — lit-html diffs internally.
const refreshList = () => renderList(listEl, store, focusNode);
store.subscribe(refreshList);
refreshList();

wireSearch(searchEl, api, store, focusNode);

// Right-panel toggle. ⇆ button collapses #panel to 0px so the canvas can
// use the full window — useful on small laptop screens. We also fire a
// resize event so the renderer picks up the new dimensions immediately.
document.getElementById('panel-toggle')?.addEventListener('click', () => {
  document.getElementById('app').classList.toggle('no-panel');
  setTimeout(() => window.dispatchEvent(new Event('resize')), 130);
});

// 2D / 3D mode toggle. Tears down the current renderer and remounts on the
// same #canvas with the same store — node positions reset (the libraries
// don't share simulation state) but the visible set / expanded tree / list
// state all live in the store and are preserved.
const modeBtn = document.getElementById('mode-toggle');
function applyModeToButton() {
  if (modeBtn) modeBtn.textContent = viewMode === '2d' ? '2D' : '3D';
}
applyModeToButton();
modeBtn?.addEventListener('click', () => {
  viewMode = viewMode === '2d' ? '3d' : '2d';
  localStorage.setItem('ckg.viewMode', viewMode);
  fg._ckgTeardown?.();
  fg = mountGraph(canvasEl, store, api, viewMode);
  wireFG(fg);
  applyModeToButton();
  console.log('view mode →', viewMode);
});

// Zoom controls — both modes are supported. 3D adjusts camera Z; 2D
// multiplies the zoom factor. Smaller value = farther in 3D, larger value
// = closer in 2D.
const zoomBy = (factor) => {
  if (viewMode === '3d') {
    const pos = fg.cameraPosition();
    fg.cameraPosition({ z: Math.max(50, pos.z * factor) }, undefined, 200);
  } else {
    // 2D: factor < 1 means "zoom in" semantically (consistent with 3D),
    // but force-graph's zoom is the literal scale factor — invert it.
    const z = typeof fg.zoom === 'function' ? fg.zoom() : 1;
    fg.zoom(Math.max(0.05, z * (1 / factor)), 200);
  }
};
document.getElementById('zoom-in')?.addEventListener('click', () => zoomBy(0.7));
document.getElementById('zoom-out')?.addEventListener('click', () => zoomBy(1.4));
document.getElementById('zoom-reset')?.addEventListener('click', () => {
  if (viewMode === '3d') {
    fg.cameraPosition({ x: 0, y: 0, z: 1500 }, { x: 0, y: 0, z: 0 }, 400);
  } else if (typeof fg.zoomToFit === 'function') {
    fg.zoomToFit(400, 50);
  }
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
