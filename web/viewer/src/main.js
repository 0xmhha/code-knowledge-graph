// src/main.js
import { API, StaticAPI, detectMode } from './api.js';
import { Store } from './store.js';
import { mountGraph } from './layout.js';
import { wireSearch } from './search.js';
import { renderList, renderDetail, applyListSelection } from './panel.js';

// Top-level await in module load — detectMode probes ./manifest.json. Static
// export bundles ship with a manifest sibling; under `ckg serve` it 404s.
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
  // L0: top-level packages. setVisible re-emits, but the renderer only mounts
  // once below so this is fine.
  const nodes = await api.nodes('', 5000);
  store.batch(() => {
    store.loadNodes(nodes);
    store.setVisible(nodes.map(n => n.id));
  });
  console.log('viewer bootstrap', { nodes: nodes.length });
})();

// View mode (2D / 3D) is persisted; default 3D.
let viewMode = (localStorage.getItem('ckg.viewMode') === '2d') ? '2d' : '3d';
const canvasEl = document.getElementById('canvas');
let fg = mountGraph(canvasEl, store, api, viewMode);

const detailEl = document.getElementById('node-detail');
const listEl = document.getElementById('node-list');
const searchEl = document.getElementById('search');

// Phase A — children cap so a single click can't push thousands of stmt-level
// nodes onto the canvas.
const CHILDREN_PER_EXPAND = 100;
// Phase B — depth of the BFS halo (focus = 0, 1-hop, 2-hop highlighted).
const FOCUS_DEPTH = 2;

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

// focusNode: select + (toggle expand) + recompute focus halo. Wrapped in a
// single store.batch so the whole interaction emits ONCE — instead of the
// 3–4 emit storm we had before, which forced the renderer to rebuild
// graphData (and the simulation to re-warm) on every internal step.
const focusNode = async (id) => {
  const node = store.nodes.get(id);
  if (!node) {
    console.warn('focusNode: id not in store', id);
    return;
  }

  // Toggle: collapse if already expanded.
  if (store.expanded.has(id)) {
    store.batch(() => {
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
      }
      store.selectedId = id;
      store.computeFocusDistance(id, FOCUS_DEPTH);
    });
    renderDetail(detailEl, api, node, store.edgesIncidentTo(id));
    return;
  }

  // Expand path — fetch children + edges, then commit in one batch.
  const [edgesRaw, childrenRaw] = await Promise.all([
    api.edges([id]).catch(err => { console.warn('edges fetch failed', id, err); return []; }),
    api.nodes(id, CHILDREN_PER_EXPAND).catch(err => { console.warn('children fetch failed', id, err); return []; }),
  ]);
  const incomingEdges = Array.isArray(edgesRaw) ? edgesRaw : [];
  const children = Array.isArray(childrenRaw) ? childrenRaw.filter(c => c && c.id) : [];

  store.batch(() => {
    store.selectedId = id;
    store.addEdges(incomingEdges);
    if (children.length) {
      store.loadNodes(children);
      const next = new Set(store.visibleIds);
      for (const c of children) next.add(c.id);
      store.setVisible([...next]);
      store.expanded.set(id, new Set(children.map(c => c.id)));
    }
    store.computeFocusDistance(id, FOCUS_DEPTH);
  });
  renderDetail(detailEl, api, node, store.edgesIncidentTo(id));
};

const wireFG = (g) => {
  g.onNodeClick(node => {
    console.log('node clicked', node?.id, node?.qualified_name);
    if (node?.id) focusNode(node.id);
  });
};
wireFG(fg);

// List re-render is now SPLIT from sync. We rebuild the DOM only when the
// underlying source-of-items changes (visible set or search results); a
// pure selection change just toggles the .selected class on existing rows.
// On a 200-item list this drops the per-click reflow from full DOM rebuild
// to <1ms class flips.
let lastListSig = null;
const refreshList = () => {
  const isSearch = (store.searchResults?.length ?? 0) > 0;
  // Signature captures everything that changes the rendered ITEMS (not the
  // selection styling). selectedId is intentionally excluded.
  const sig = `${isSearch ? 's' : 'v'}|${(isSearch ? store.searchResults : [...store.visibleIds]).length}|${store.visibleIds.size}|${store.searchResults.length}`;
  if (sig !== lastListSig) {
    lastListSig = sig;
    renderList(listEl, store, focusNode);
  } else {
    applyListSelection(listEl, store.selectedId);
  }
};
store.subscribe(refreshList);
refreshList();

wireSearch(searchEl, api, store, focusNode);

// Panel toggle.
document.getElementById('panel-toggle')?.addEventListener('click', () => {
  document.getElementById('app').classList.toggle('no-panel');
  setTimeout(() => window.dispatchEvent(new Event('resize')), 130);
});

// Mode toggle.
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

// Zoom controls.
const zoomBy = (factor) => {
  if (viewMode === '3d') {
    const pos = fg.cameraPosition();
    fg.cameraPosition({ z: Math.max(50, pos.z * factor) }, undefined, 200);
  } else {
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
