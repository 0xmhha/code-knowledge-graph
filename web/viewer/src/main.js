// src/main.js
//
// Navigation model: (anchorId, depth) pair, never accumulating.
//   - Click on a node                → set anchor to that id, depth = 1
//   - ⇲ depth-in button (or "]")     → depth + 1, recompute visible
//   - ⇱ depth-out button (or "[")    → depth − 1, recompute visible
//   - 🏠 home button (or "Home")     → anchorId = null, depth = 0
//   - Mouse wheel / drag             → pure camera (no data change)
//
// This replaces the old click-toggle/expand model. Every navigation step
// is a single, explicit, measured event: t0 = perf.now() at user action,
// t1 = perf.now() inside requestAnimationFrame after sync — the delta is
// surfaced in the bottombar so we can profile interactively.
import { API, StaticAPI, detectMode } from './api.js';
import { Store } from './store.js';
import { mountGraph } from './layout.js';
import { wireSearch } from './search.js';
import { renderList, renderDetail, applyListSelection } from './panel.js';
import { recomputeVisible } from './depth.js';

const FONT_SIZES = { S: 0.85, M: 1.0, L: 1.2 };
const DEPTH_MAX = 6;     // soft cap so depth-in doesn't run forever

const mode = await detectMode();
const api = mode === 'static' ? new StaticAPI() : new API('');
const store = new Store();

let viewMode = (localStorage.getItem('ckg.viewMode') === '2d') ? '2d' : '3d';
store.fontSize = FONT_SIZES[localStorage.getItem('ckg.fontSize')] ?? FONT_SIZES.M;

const canvasEl = document.getElementById('canvas');
const detailEl = document.getElementById('node-detail');
const listEl = document.getElementById('node-list');
const searchEl = document.getElementById('search');
const depthEl = document.getElementById('depth-indicator');
const renderEl = document.getElementById('render-meter');

let fg = mountGraph(canvasEl, store, api, viewMode);

function updateMeters() {
  if (depthEl) {
    depthEl.textContent = store.anchorId
      ? `depth ${store.depth}/${DEPTH_MAX}`
      : 'depth root';
  }
  if (renderEl) {
    const v = store.visibleIds.size;
    const e = store.edges.filter(x => store.visibleIds.has(x.src) && store.visibleIds.has(x.dst)).length;
    renderEl.textContent = `${store.lastRenderMs.toFixed(0)} ms · ${v} nodes / ${e} edges`;
  }
}

// navigate runs a single user-driven navigation step (depth-in / out, set
// anchor, home, etc.) and measures its render cost. The work is wrapped in
// store.batch so the renderer sees one consolidated change. Render time is
// captured on the next animation frame after graphData has been pushed.
async function navigate(mutator) {
  const t0 = performance.now();
  await mutator();
  // After the batch settles, the store has emitted, layout's sync() has
  // pushed graphData into the renderer, and the engine has started its
  // simulation. We capture t1 on the next frame so the measurement covers
  // the synchronous DOM work + first frame; settle time is reported by
  // the engine-stop handler set up below.
  requestAnimationFrame(() => {
    store.lastRenderMs = performance.now() - t0;
    updateMeters();
  });
}

function setEngineStopHook() {
  if (typeof fg.onEngineStop === 'function') {
    fg.onEngineStop(() => {
      // settled — refresh the meter so the user sees the steady-state cost.
      updateMeters();
    });
  }
}
setEngineStopHook();

async function bootstrap() {
  const manifest = await api.manifest();
  document.getElementById('src-info').textContent = manifest.src_root || '';
  if (manifest.graph_stale) {
    const banner = document.createElement('div');
    banner.className = 'stale-banner';
    banner.textContent = `⚠️ Graph built from ${manifest.src_commit} but src is now at ${manifest.current_commit}. Run \`ckg build\` to refresh.`;
    document.body.insertBefore(banner, document.body.firstChild);
  }
  await navigate(async () => {
    store.anchorId = null;
    store.depth = 0;
    await recomputeVisible(store, api);
  });
  console.log('viewer bootstrap', { visible: store.visibleIds.size });
}
bootstrap();

// Selection / detail panel without changing the navigation anchor. Used
// from sidebar list clicks where the user wants to inspect, not navigate.
function selectOnly(id) {
  const node = store.nodes.get(id);
  if (!node) return;
  store.batch(() => {
    store.selectedId = id;
    store.computeFocusDistance(id, 2);
  });
  renderDetail(detailEl, api, node, store.edgesIncidentTo(id));
}

// setAnchor navigates: this id becomes the new anchor at depth 1 (so the
// user immediately sees neighbours). Click on a node = navigate.
async function setAnchor(id) {
  if (!store.nodes.has(id)) {
    console.warn('setAnchor: id not in store', id);
    return;
  }
  await navigate(async () => {
    store.anchorId = id;
    store.depth = 1;
    store.selectedId = id;
    await recomputeVisible(store, api);
  });
  const node = store.nodes.get(id);
  if (node) renderDetail(detailEl, api, node, store.edgesIncidentTo(id));
}

async function depthIn() {
  if (!store.anchorId) return;
  if (store.depth >= DEPTH_MAX) return;
  await navigate(async () => {
    store.depth += 1;
    await recomputeVisible(store, api);
  });
}

async function depthOut() {
  if (!store.anchorId) return;
  if (store.depth <= 0) {
    // depth=0 → going further out = back to root view.
    await navigate(async () => {
      store.anchorId = null;
      store.depth = 0;
      store.selectedId = null;
      await recomputeVisible(store, api);
    });
    return;
  }
  await navigate(async () => {
    store.depth -= 1;
    await recomputeVisible(store, api);
  });
}

async function goHome() {
  await navigate(async () => {
    store.anchorId = null;
    store.depth = 0;
    store.selectedId = null;
    await recomputeVisible(store, api);
  });
}

const wireFG = (g) => {
  g.onNodeClick(node => {
    console.log('node clicked', node?.id, node?.qualified_name);
    if (node?.id) setAnchor(node.id);
  });
};
wireFG(fg);

// Sidebar list: clicking a row inspects without navigating (keeps anchor).
let lastListSig = null;
const refreshList = () => {
  const isSearch = (store.searchResults?.length ?? 0) > 0;
  const sig = `${isSearch ? 's' : 'v'}|${(isSearch ? store.searchResults : [...store.visibleIds]).length}|${store.visibleIds.size}|${store.searchResults.length}`;
  if (sig !== lastListSig) {
    lastListSig = sig;
    renderList(listEl, store, selectOnly);
  } else {
    applyListSelection(listEl, store.selectedId);
  }
  updateMeters();
};
store.subscribe(refreshList);
refreshList();

wireSearch(searchEl, api, store, selectOnly);

// ─── Top-bar buttons ─────────────────────────────────────────────────────
document.getElementById('panel-toggle')?.addEventListener('click', () => {
  document.getElementById('app').classList.toggle('no-panel');
  setTimeout(() => window.dispatchEvent(new Event('resize')), 130);
});

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
  setEngineStopHook();
  applyModeToButton();
  console.log('view mode →', viewMode);
});

// ─── Bottom-bar: depth, render meter, font size, zoom ────────────────────
document.getElementById('depth-in')?.addEventListener('click', depthIn);
document.getElementById('depth-out')?.addEventListener('click', depthOut);
document.getElementById('depth-home')?.addEventListener('click', goHome);

function applyFontFromStore() {
  if (canvasEl) canvasEl.style.setProperty('--ckg-font-scale', store.fontSize.toFixed(2));
  // Mode-toggle button is also a touch larger when font scale is large.
  store.emit();
}
['S', 'M', 'L'].forEach(label => {
  document.getElementById(`font-${label.toLowerCase()}`)?.addEventListener('click', () => {
    store.fontSize = FONT_SIZES[label];
    localStorage.setItem('ckg.fontSize', label);
    applyFontFromStore();
  });
});
applyFontFromStore();

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

// Keyboard shortcuts.
window.addEventListener('keydown', (ev) => {
  if (document.activeElement?.id === 'search') {
    if (ev.key === 'Escape') document.activeElement.blur();
    return;
  }
  if (ev.key === '=' || ev.key === '+') zoomBy(0.7);
  else if (ev.key === '-') zoomBy(1.4);
  else if (ev.key === '0') document.getElementById('zoom-reset')?.click();
  else if (ev.key === ']') depthIn();
  else if (ev.key === '[') depthOut();
  else if (ev.key === 'Home') goHome();
  else if (ev.key === '/') { ev.preventDefault(); document.getElementById('search')?.focus(); }
});

console.log('viewer ready', { fg, store });
