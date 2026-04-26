// src/depth.js
// Depth-driven visible-set recomputation.
//
// Old model: clicks accumulated children indefinitely; mouse-wheel zoom
// triggered camera-distance LOD that fetched more nodes implicitly. The
// resulting visible set was unbounded and rendering cost grew without
// the user knowing what to expect.
//
// New model: navigation is a pair (anchor, depth).
//   - anchor === null → root view: top-level packages
//   - anchor === <id> → BFS over the edge graph from that node, `depth`
//     hops outward. A neighbour is loaded into the store on demand via
//     /api/edges + /api/nodes-by-ids; nothing is fetched proactively.
//
// recomputeVisible(...) is called explicitly by the depth-in / depth-out
// / set-anchor / home buttons in main.js — never by camera changes — so
// users can predict exactly when the canvas redraws and how much data
// it draws.
//
// MAX_VISIBLE caps the working set regardless of depth, so a single
// pathological hub can't push the renderer past its interactive
// sweet-spot.

const MAX_VISIBLE = 500;

// recomputeVisible mutates the store: sets visibleIds + ensures all
// reachable nodes are loaded. Returns the resulting visible id set so
// callers can also read the count for the perf meter.
export async function recomputeVisible(store, api) {
  const { anchorId, depth } = store;

  if (!anchorId) {
    // Root view: top-level packages. Cap to MAX_VISIBLE in case the
    // corpus has more, though packages are usually <300.
    const top = await api.nodes('', MAX_VISIBLE);
    const ids = top.map(n => n.id);
    store.batch(() => {
      store.loadNodes(top);
      store.setVisible(ids);
      store.focusDistance = new Map();   // no focus halo at root
    });
    return new Set(ids);
  }

  // Anchor view: BFS over the edge graph. We treat depth=0 as
  // "anchor only", depth=1 as "anchor + 1-hop", etc. To make sure the
  // index has every neighbour edge, we lazy-fetch /api/edges for any
  // currently-unknown id while walking.
  const visible = new Set([anchorId]);
  let frontier = [anchorId];
  // Track ids whose neighbour edges we still need to fetch.
  const needFetch = new Set();
  if ((store.edgesBySrc.get(anchorId)?.length ?? 0) === 0 &&
      (store.edgesByDst.get(anchorId)?.length ?? 0) === 0) {
    needFetch.add(anchorId);
  }

  for (let d = 0; d < depth && visible.size < MAX_VISIBLE; d++) {
    // Bring in any frontier id's edges that we haven't seen yet.
    if (needFetch.size) {
      const ids = [...needFetch];
      needFetch.clear();
      const fresh = await api.edges(ids);
      if (fresh.length) store.addEdges(fresh);
    }

    // Walk the frontier one hop further.
    const nextFrontier = [];
    for (const id of frontier) {
      const outs = store.edgesBySrc.get(id) || [];
      const ins = store.edgesByDst.get(id) || [];
      for (const e of outs.concat(ins)) {
        const other = e.src === id ? e.dst : e.src;
        if (visible.has(other)) continue;
        visible.add(other);
        nextFrontier.push(other);
        if (!store.edgesBySrc.has(other) && !store.edgesByDst.has(other)) {
          needFetch.add(other);
        }
        if (visible.size >= MAX_VISIBLE) break;
      }
      if (visible.size >= MAX_VISIBLE) break;
    }
    frontier = nextFrontier;
    if (!frontier.length) break;
  }

  // Materialise any node that hasn't been loaded yet (search results,
  // children fetches all do this lazily; here we batch the misses).
  const missing = [...visible].filter(id => !store.nodes.has(id));
  if (missing.length) {
    const fetched = await api.nodesByIds(missing);
    if (fetched.length) {
      store.batch(() => store.loadNodes(fetched));
    }
  }

  store.batch(() => {
    store.setVisible([...visible]);
    store.computeFocusDistance(anchorId, Math.min(depth, 2));
  });
  return visible;
}
