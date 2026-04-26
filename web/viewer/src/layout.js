// src/layout.js
// Wires `3d-force-graph` to the store: visible nodes/edges sync,
// per-node THREE meshes via encoding.nodeMesh, per-edge color/width via EDGE_STYLE,
// and an LOD trigger that expands the visible set on camera zoom-in.
import ForceGraph3D from '3d-force-graph';
import { nodeMesh, EDGE_STYLE } from './encoding.js';

// Camera Z thresholds → LOD bucket. Larger Z = farther = lower LOD.
function lodFromZ(z) {
  if (z < 400) return 3;
  if (z < 800) return 2;
  if (z < 1500) return 1;
  return 0;
}

function edgeColor(link) {
  const c = EDGE_STYLE[link.type]?.color ?? 0x999999;
  return '#' + c.toString(16).padStart(6, '0');
}

export function mountGraph(container, store, api) {
  const fg = ForceGraph3D()(container)
    .nodeThreeObject(node => nodeMesh(node))
    .nodeLabel(node => {
      // Hover tooltip — concise so it doesn't drown the canvas.
      const t = node.type || '?';
      const q = node.qualified_name || node.name || node.id;
      const f = node.file_path ? `\n${node.file_path}:${node.start_line || 0}` : '';
      return `<div style="font-family:ui-monospace,monospace;font-size:12px;line-height:1.3;background:rgba(15,17,20,.95);color:#e6e7e9;padding:6px 8px;border:1px solid #2a2c30;border-radius:4px;"><strong>${t}</strong>&nbsp;<span style="color:#9aa">${q}</span>${f}</div>`;
    })
    .nodeVisibility(node => store.visibleIds.has(node.id))
    .linkVisibility(link => !(EDGE_STYLE[link.type]?.hidden))
    .linkColor(edgeColor)
    .linkWidth(link => EDGE_STYLE[link.type]?.width ?? 1)
    .linkDirectionalArrowLength(3)
    .linkDirectionalArrowRelPos(0.95)
    .cooldownTicks(200);

  // Push current store state into ForceGraph3D. Rebuilds the data array; the
  // simulation keeps positions for nodes whose `id` is unchanged (3d-force-graph
  // diffs by id internally), so re-syncs after LOD expansion are cheap.
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
  store.subscribe(sync);
  sync();

  // LOD trigger: on every camera change, recompute the LOD bucket from Z and
  // — if it moved closer (higher LOD) — fetch children of currently visible
  // nodes and merge them into the store. Errors are logged; never throw inside
  // the camera callback or OrbitControls will get wedged.
  let lodFetchInFlight = false;
  fg.controls().addEventListener('change', () => {
    const z = fg.cameraPosition().z;
    const lod = lodFromZ(z);
    if (lod === store.lod) return;

    store.setLOD(lod);
    const lodEl = document.getElementById('lod');
    if (lodEl) lodEl.textContent = `L${lod}`;

    // Only expand on zoom-in (closer => higher LOD). Don't refetch on zoom-out.
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
        const more = batches.flat();
        if (!more.length) return;
        store.loadNodes(more);
        const next = new Set(store.visibleIds);
        for (const n of more) next.add(n.id);
        store.setVisible([...next]);
      })
      .catch(err => console.warn('LOD expand failed', err))
      .finally(() => { lodFetchInFlight = false; });
  });

  return fg;
}
