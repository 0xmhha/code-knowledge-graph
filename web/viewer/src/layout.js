// src/layout.js
// Renders either 3D (3d-force-graph + Three.js) or 2D (force-graph +
// Canvas2D) on top of the same store. Both modes implement the
// "focus + halo" rendering pattern (docs/VIEWER-ROADMAP.md Phase B):
//
//   - the user's selected node is the brightest                       (d=0)
//   - its 1-hop neighbours are vivid                                  (d=1)
//   - its 2-hop neighbours are dimmed but legible                     (d=2)
//   - everything else fades to background                             (d=∞)
//
// Without that, a click on a hub package buries the relevant call path
// in 100+ equally-bright sibling nodes — defeating the purpose of the
// viewer. The distances are precomputed by store.computeFocusDistance
// during focusNode and re-applied here every time the store emits.
import ForceGraph3D from '3d-force-graph';
import ForceGraph2D from 'force-graph';
import { nodeMesh, EDGE_STYLE } from './encoding.js';

// Canvas2D-friendly mirror of encoding.LANG_COLOR.
const LANG_COLOR_2D = { go: '#00add8', ts: '#3178c6', sol: '#3c3c3d' };
const ALPHA_BY_CONF = { EXTRACTED: 1.0, INFERRED: 0.7, AMBIGUOUS: 0.4 };

// Distance → opacity. Index past length-1 (i.e. distance > 2 or undefined)
// uses the last entry as the dim default.
const FOCUS_OPACITY = [1.0, 0.92, 0.55, 0.18];
const FOCUS_LINK_BRIGHTNESS = [1.0, 1.0, 0.55, 0.10];

function focusOpacity(id, store) {
  if (store.focusDistance.size === 0) return 1.0;       // no focus → flat
  const d = store.focusDistance.get(id);
  if (d === undefined) return FOCUS_OPACITY[FOCUS_OPACITY.length - 1];
  return FOCUS_OPACITY[Math.min(d, FOCUS_OPACITY.length - 1)];
}

// edgeOnFocusPath: both endpoints inside the focus ball.
function edgeFocusBrightness(e, store) {
  if (store.focusDistance.size === 0) return 1.0;
  const a = store.focusDistance.get(e.src);
  const b = store.focusDistance.get(e.dst);
  if (a === undefined || b === undefined) return FOCUS_LINK_BRIGHTNESS[3];
  // Edges with at least one endpoint at distance 0 (focus) are loudest;
  // edges between two 1-hop neighbours are still loud (call paths through
  // the neighbourhood); 2-hop edges fade.
  return FOCUS_LINK_BRIGHTNESS[Math.min(Math.max(a, b), FOCUS_LINK_BRIGHTNESS.length - 1)];
}

// Hex int → CSS rgb scaled to brightness (1=full, 0=black). Used to fade
// out-of-focus edges without changing per-type colouring.
function hexAtBrightness(hex, brightness) {
  const r = ((hex >> 16) & 0xff) * brightness;
  const g = ((hex >> 8) & 0xff) * brightness;
  const b = (hex & 0xff) * brightness;
  return `rgb(${r | 0},${g | 0},${b | 0})`;
}

function edgeColor(e, store) {
  const base = EDGE_STYLE[e.type]?.color ?? 0x999999;
  return hexAtBrightness(base, edgeFocusBrightness(e, store));
}

function edgeWidth(e, store) {
  const base = EDGE_STYLE[e.type]?.width ?? 1;
  const brightness = edgeFocusBrightness(e, store);
  if (brightness >= 0.9) return base + 0.5;            // emphasise focus path
  if (brightness >= 0.5) return Math.max(0.7, base);
  return 0.25;
}

function tooltipHtml(node, store) {
  const t = node.type || '?';
  const q = node.qualified_name || node.name || node.id;
  const f = node.file_path ? `${node.file_path}:${node.start_line || 0}` : '—';
  const lang = node.language || '';
  const conf = node.confidence || '';
  const inDeg = node.in_degree ?? 0;
  const outDeg = node.out_degree ?? 0;
  const usage = (node.usage_score ?? 0).toFixed(2);
  const pr = (node.pagerank ?? 0).toExponential(2);
  const sig = node.signature ? `<div style="color:#9ad;margin-top:4px;font-style:italic;max-width:380px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">${node.signature}</div>` : '';
  const dist = store.focusDistance.get(node.id);
  const distLabel = dist === 0 ? '· FOCUS' : dist === 1 ? '· direct' : dist === 2 ? '· 2-hop' : '';
  return `<div style="pointer-events:none;font-family:ui-monospace,monospace;font-size:11px;line-height:1.4;background:rgba(15,17,20,.96);color:#e6e7e9;padding:8px 10px;border:1px solid #2a2c30;border-radius:4px;max-width:420px;">
<div style="font-size:12px;margin-bottom:4px;"><strong style="color:#7ab8ff;">${t}</strong> <span style="color:#cfd0d3;">${q}</span> <span style="color:#7ab8ff">${distLabel}</span></div>
<div style="color:#bbb;">📄 ${f}</div>${sig}
<div style="color:#888;margin-top:5px;">lang: <span style="color:#aaa">${lang}</span> · conf: <span style="color:#aaa">${conf}</span></div>
<div style="color:#888;">in-edges: <span style="color:#aaa">${inDeg}</span> · out-edges: <span style="color:#aaa">${outDeg}</span></div>
<div style="color:#888;">usage: <span style="color:#aaa">${usage}</span> · pagerank: <span style="color:#aaa">${pr}</span></div>
<div style="color:#666;margin-top:6px;font-size:10px;">click to expand · click again to collapse</div>
</div>`;
}

// 2D node renderer. Reads opacity from store.focusDistance per frame so
// the focus halo updates without rebuilding graph data.
function makeDrawNode2D(store) {
  return function drawNode2D(node, ctx, globalScale) {
    const r = 3 + Math.log10((node.usage_score || 0) + 1) * 1.5;
    const op = focusOpacity(node.id, store) * (ALPHA_BY_CONF[node.confidence] ?? 1);
    ctx.globalAlpha = op;
    ctx.fillStyle = LANG_COLOR_2D[node.language] || '#888';
    ctx.beginPath();
    ctx.arc(node.x, node.y, Math.max(2, r), 0, 2 * Math.PI);
    ctx.fill();
    // Focus node gets a bright ring so it stands out even on monochrome corpora.
    const dist = store.focusDistance.get(node.id);
    if (dist === 0) {
      ctx.globalAlpha = 1;
      ctx.strokeStyle = '#ffffff';
      ctx.lineWidth = 2 / globalScale;
      ctx.beginPath();
      ctx.arc(node.x, node.y, Math.max(2, r) + 2 / globalScale, 0, 2 * Math.PI);
      ctx.stroke();
    }
    ctx.globalAlpha = 1;
    // Label only zoomed-in hubs OR nodes inside the focus ball.
    const inFocusBall = dist !== undefined;
    const deg = (node.in_degree ?? 0) + (node.out_degree ?? 0);
    if (inFocusBall || (globalScale > 1.5 && deg > 5)) {
      const fontSize = Math.max(8, 10 / globalScale);
      ctx.font = `${fontSize}px ui-monospace, monospace`;
      ctx.fillStyle = inFocusBall ? '#e6e7e9' : '#9aa';
      ctx.textAlign = 'center';
      ctx.fillText(node.name || '', node.x, node.y - r - 2);
    }
  };
}

function makePointerArea2D() {
  return function nodePointerArea2D(node, color, ctx) {
    const r = Math.max(4, 3 + Math.log10((node.usage_score || 0) + 1) * 1.5);
    ctx.fillStyle = color;
    ctx.beginPath();
    ctx.arc(node.x, node.y, r + 3, 0, 2 * Math.PI);
    ctx.fill();
  };
}

// LOD thresholds.
function lodFromZ(z) {
  if (z < 400) return 3;
  if (z < 800) return 2;
  if (z < 1500) return 1;
  return 0;
}
function lodFromZoom(k) {
  if (k > 4) return 3;
  if (k > 2) return 2;
  if (k > 1) return 1;
  return 0;
}

export function mountGraph(container, store, api, mode = '3d') {
  container.innerHTML = '';

  const Factory = mode === '2d' ? ForceGraph2D : ForceGraph3D;
  const fg = Factory()(container);

  fg.linkSource('src')
    .linkTarget('dst')
    .nodeLabel(node => tooltipHtml(node, store))
    .nodeVisibility(node => store.visibleIds.has(node.id))
    .linkVisibility(link => !(EDGE_STYLE[link.type]?.hidden))
    .linkColor(e => edgeColor(e, store))
    .linkWidth(e => edgeWidth(e, store))
    .linkDirectionalArrowLength(3)
    .linkDirectionalArrowRelPos(0.95)
    // Tighter cooldown — Phase A. The default 200 ticks made every expand
    // visibly lag; 80 settles fast enough for our diff/incremental case.
    .cooldownTicks(80)
    .cooldownTime(2500);

  // Per-mode wiring.
  let meshIndex = null;
  if (mode === '3d') {
    meshIndex = new Map();
    fg.nodeThreeObject(node => {
      const m = nodeMesh(node);
      meshIndex.set(node.id, m);
      return m;
    });
  } else {
    fg.nodeCanvasObject(makeDrawNode2D(store))
      .nodePointerAreaPaint(makePointerArea2D())
      .backgroundColor('#0d0e10');
  }

  // Build initial graph data and re-sync on store changes. The diff inside
  // both libraries is keyed by node.id so positions persist across syncs.
  const sync = () => {
    const visible = store.visibleIds;
    const nodes = [];
    for (const id of visible) {
      const n = store.nodes.get(id);
      if (n) nodes.push(n);
    }
    const links = store.edges.filter(
      e => visible.has(e.src) && visible.has(e.dst)
    );
    fg.graphData({ nodes, links });

    // Apply 3D focus halo to mesh materials. We mutate material.opacity
    // directly so the highlight updates without rebuilding the THREE
    // scene; doing it here in the same listener that syncs graphData
    // means freshly-created meshes get their final opacity on first frame.
    if (meshIndex) {
      // Drop entries for nodes that were just unmounted by the diff.
      for (const id of meshIndex.keys()) {
        if (!store.visibleIds.has(id)) meshIndex.delete(id);
      }
      const focusActive = store.focusDistance.size > 0;
      for (const [id, mesh] of meshIndex) {
        if (!mesh?.material) continue;
        const op = focusActive
          ? focusOpacity(id, store) * (ALPHA_BY_CONF[store.nodes.get(id)?.confidence] ?? 1)
          : (ALPHA_BY_CONF[store.nodes.get(id)?.confidence] ?? 1);
        mesh.material.opacity = op;
        mesh.material.transparent = op < 1;
        mesh.material.needsUpdate = true;
      }
    }
  };
  const unsubscribe = store.subscribe(sync);
  sync();

  // LOD trigger.
  let lodFetchInFlight = false;
  const tryLODExpand = (lod) => {
    if (lod === store.lod) return;
    store.setLOD(lod);
    const lodEl = document.getElementById('lod');
    if (lodEl) lodEl.textContent = `L${lod}`;
    if (lod <= 0 || lodFetchInFlight) return;
    lodFetchInFlight = true;
    const parents = Array.from(store.visibleIds);
    Promise.all(
      parents.map(id =>
        api.nodes(id, 1000).catch(err => {
          console.warn('LOD fetch failed for', id, err);
          return [];
        })
      )
    )
      .then(batches => {
        const more = batches.flat().filter(n => n && n.id);
        if (!more.length) return;
        store.batch(() => {
          store.loadNodes(more);
          const next = new Set(store.visibleIds);
          for (const n of more) next.add(n.id);
          store.setVisible([...next]);
        });
      })
      .catch(err => console.warn('LOD expand failed', err))
      .finally(() => { lodFetchInFlight = false; });
  };

  if (mode === '3d') {
    fg.controls().addEventListener('change', () => {
      tryLODExpand(lodFromZ(fg.cameraPosition().z));
    });
  } else if (typeof fg.onZoom === 'function') {
    fg.onZoom(({ k }) => tryLODExpand(lodFromZoom(k)));
  }

  fg._ckgTeardown = () => {
    unsubscribe?.();
    if (meshIndex) meshIndex.clear();
    container.innerHTML = '';
  };
  return fg;
}
