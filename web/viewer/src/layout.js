// src/layout.js
// Wires either `3d-force-graph` (3D, Three.js) or `force-graph` (2D, Canvas2D)
// to the same store. Both share: linkSource('src')/linkTarget('dst') field
// mapping, edge styling via EDGE_STYLE, the rich hover tooltip, and the
// click handler. They differ in how nodes are drawn (THREE meshes vs. Canvas
// circles) and in the LOD trigger (3D uses camera Z, 2D uses zoom factor).
import ForceGraph3D from '3d-force-graph';
import ForceGraph2D from 'force-graph';
import { nodeMesh, EDGE_STYLE } from './encoding.js';

// LANG_COLOR mirrors encoding.js but keyed by string for Canvas2D fillStyle.
const LANG_COLOR_2D = { go: '#00add8', ts: '#3178c6', sol: '#3c3c3d' };
const ALPHA_BY_CONF = { EXTRACTED: 1.0, INFERRED: 0.7, AMBIGUOUS: 0.4 };

// Camera Z thresholds → LOD bucket (3D only). Larger Z = farther = lower LOD.
function lodFromZ(z) {
  if (z < 400) return 3;
  if (z < 800) return 2;
  if (z < 1500) return 1;
  return 0;
}
// Zoom thresholds → LOD bucket (2D). Larger zoom = closer = higher LOD.
function lodFromZoom(z) {
  if (z > 4) return 3;
  if (z > 2) return 2;
  if (z > 1) return 1;
  return 0;
}

function edgeColor(link) {
  const c = EDGE_STYLE[link.type]?.color ?? 0x999999;
  return '#' + c.toString(16).padStart(6, '0');
}

function tooltipHtml(node) {
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
  return `<div style="pointer-events:none;font-family:ui-monospace,monospace;font-size:11px;line-height:1.4;background:rgba(15,17,20,.96);color:#e6e7e9;padding:8px 10px;border:1px solid #2a2c30;border-radius:4px;max-width:420px;">
<div style="font-size:12px;margin-bottom:4px;"><strong style="color:#7ab8ff;">${t}</strong> <span style="color:#cfd0d3;">${q}</span></div>
<div style="color:#bbb;">📄 ${f}</div>${sig}
<div style="color:#888;margin-top:5px;">lang: <span style="color:#aaa">${lang}</span> · conf: <span style="color:#aaa">${conf}</span></div>
<div style="color:#888;">in-edges: <span style="color:#aaa">${inDeg}</span> · out-edges: <span style="color:#aaa">${outDeg}</span></div>
<div style="color:#888;">usage: <span style="color:#aaa">${usage}</span> · pagerank: <span style="color:#aaa">${pr}</span></div>
<div style="color:#666;margin-top:6px;font-size:10px;">click to expand · click again to collapse</div>
</div>`;
}

// drawNode2D: Canvas2D node renderer for the 2D mode. We keep the visual
// language consistent with 3D (color = source language, size scaled by
// usage_score) but skip per-NodeType shape variation — Canvas2D makes 29
// distinct primitives ugly fast. Important hubs get an inline name label
// at zoom levels where it won't pile up.
function drawNode2D(node, ctx, globalScale) {
  const r = 3 + Math.log10((node.usage_score || 0) + 1) * 1.5;
  ctx.globalAlpha = ALPHA_BY_CONF[node.confidence] ?? 1;
  ctx.fillStyle = LANG_COLOR_2D[node.language] || '#888';
  ctx.beginPath();
  ctx.arc(node.x, node.y, Math.max(2, r), 0, 2 * Math.PI);
  ctx.fill();
  ctx.globalAlpha = 1;
  // Label cutoff: only when we've zoomed in AND the node is a hub. Otherwise
  // 200K nodes' worth of text floods the canvas.
  const deg = (node.in_degree ?? 0) + (node.out_degree ?? 0);
  if (globalScale > 1.5 && deg > 5) {
    const fontSize = Math.max(8, 10 / globalScale);
    ctx.font = `${fontSize}px ui-monospace, monospace`;
    ctx.fillStyle = '#cfd0d3';
    ctx.textAlign = 'center';
    ctx.fillText(node.name || '', node.x, node.y - r - 2);
  }
}

// nodePointerArea2D: hit-target so click registration matches the visual
// circle (not the bounding-box default which would be a 1×1px target).
function nodePointerArea2D(node, color, ctx) {
  const r = Math.max(4, 3 + Math.log10((node.usage_score || 0) + 1) * 1.5);
  ctx.fillStyle = color;
  ctx.beginPath();
  ctx.arc(node.x, node.y, r + 2, 0, 2 * Math.PI);
  ctx.fill();
}

// mountGraph instantiates either a 3D or 2D force graph and wires it to the
// store. Returns the graph instance plus a teardown helper for the caller
// to call on remount.
export function mountGraph(container, store, api, mode = '3d') {
  // Wipe the previous canvas/three context — both libraries inject DOM into
  // the container so a remount-without-cleanup leaves duplicate canvases.
  container.innerHTML = '';

  const Factory = mode === '2d' ? ForceGraph2D : ForceGraph3D;
  const fg = Factory()(container)
    .linkSource('src')
    .linkTarget('dst')
    .nodeLabel(tooltipHtml)
    .nodeVisibility(node => store.visibleIds.has(node.id))
    .linkVisibility(link => !(EDGE_STYLE[link.type]?.hidden))
    .linkColor(edgeColor)
    .linkWidth(link => EDGE_STYLE[link.type]?.width ?? 1);

  if (mode === '3d') {
    fg.nodeThreeObject(node => nodeMesh(node))
      .linkDirectionalArrowLength(3)
      .linkDirectionalArrowRelPos(0.95)
      .cooldownTicks(200);
  } else {
    fg.nodeCanvasObject(drawNode2D)
      .nodePointerAreaPaint(nodePointerArea2D)
      .linkDirectionalArrowLength(3)
      .linkDirectionalArrowRelPos(0.95)
      .cooldownTicks(200)
      .backgroundColor('#0d0e10');
  }

  // Push current store state into the renderer. Rebuilds the data array; both
  // libraries diff by `id` internally so positions persist across re-syncs.
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
  };
  const unsubscribe = store.subscribe(sync);
  sync();

  // LOD trigger — on zoom-in, fetch children of currently visible nodes and
  // merge them. 3D listens to OrbitControls 'change'; 2D uses .onZoom(...).
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
        store.loadNodes(more);
        const next = new Set(store.visibleIds);
        for (const n of more) next.add(n.id);
        store.setVisible([...next]);
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

  // Teardown lets main.js cleanly remount on mode toggle.
  fg._ckgTeardown = () => {
    unsubscribe?.();
    container.innerHTML = '';
  };
  return fg;
}
