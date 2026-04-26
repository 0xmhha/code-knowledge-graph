// src/store.js — single source of truth for the viewer.
//
// Two big pieces beyond plain mutable state:
//
// 1) batch() — coalesces multiple mutations into a single emit. focusNode
//    used to fire 3–4 emits per click (loadNodes / setVisible / setLOD /
//    explicit emit), each one rebuilding the entire renderer graphData and
//    rerendering the side list. Wrapping mutations in batch() drops that
//    to 1 emit per user action.
//
// 2) edge index (edgesBySrc / edgesByDst) — old code did
//    `store.edges.filter(e => visible.has(e.src) && visible.has(e.dst))`
//    on every emit. With 10K+ accumulated edges that's O(|edges|) per
//    sync. The index lets BFS and lookups run in O(neighbours).
export class Store {
  constructor() {
    this.nodes = new Map();           // id -> node
    this.edges = [];                  // append-only flat list
    this.edgesBySrc = new Map();      // src id -> [edge…]
    this.edgesByDst = new Map();      // dst id -> [edge…]
    this.visibleIds = new Set();      // ids currently rendered on canvas
    this.lod = 0;
    this.hierarchyKind = 'pkg';
    this.listeners = new Set();
    this.searchResults = [];
    this.selectedId = null;
    // focusDistance: id -> BFS distance from `selectedId`, capped at FOCUS_DEPTH.
    // Drives the focus+halo rendering layer (Phase B). Empty map = no focus
    // (initial state) → renderer falls back to flat encoding.
    this.focusDistance = new Map();
    this.expanded = new Map();        // parentId -> Set<childId>
    this._batchDepth = 0;
    this._dirty = false;
  }

  subscribe(fn) { this.listeners.add(fn); return () => this.listeners.delete(fn); }

  emit() {
    if (this._batchDepth > 0) { this._dirty = true; return; }
    for (const fn of this.listeners) fn(this);
  }

  // Run fn() with all emits coalesced. Re-entrant safe via depth counter.
  batch(fn) {
    this._batchDepth++;
    try { fn(); }
    finally {
      this._batchDepth--;
      if (this._batchDepth === 0 && this._dirty) {
        this._dirty = false;
        this.emit();
      }
    }
  }

  loadNodes(arr) {
    if (!Array.isArray(arr)) return;
    for (const n of arr) if (n && n.id) this.nodes.set(n.id, n);
    this.emit();
  }

  // addEdges: dedup against the index + populate both directions. Emits if
  // anything was actually added (no churn for repeated focusNode calls on
  // the same node).
  addEdges(arr) {
    if (!Array.isArray(arr)) return 0;
    let added = 0;
    for (const e of arr) {
      if (!e || !e.src || !e.dst) continue;
      const fromList = this.edgesBySrc.get(e.src);
      if (fromList && fromList.some(x => x.dst === e.dst && x.type === e.type)) continue;
      this.edges.push(e);
      added++;
      if (fromList) fromList.push(e);
      else this.edgesBySrc.set(e.src, [e]);
      const toList = this.edgesByDst.get(e.dst);
      if (toList) toList.push(e);
      else this.edgesByDst.set(e.dst, [e]);
    }
    if (added) this.emit();
    return added;
  }

  setVisible(ids) { this.visibleIds = new Set(ids); this.emit(); }
  setLOD(n) { this.lod = n; this.emit(); }
  setHierarchy(k) { this.hierarchyKind = k; this.emit(); }

  // BFS undirected over the known edge index, capped at maxDepth. The
  // result drives the focus+halo render layer; `undefined` for an id means
  // "outside the focus ball" → renderer dims it heavily. focus itself is
  // always at distance 0.
  //
  // Caller invokes via batch() so the single emit also republishes the new
  // distance map to subscribers.
  computeFocusDistance(focusId, maxDepth = 2) {
    const dist = new Map();
    if (focusId && this.nodes.has(focusId)) {
      dist.set(focusId, 0);
      let frontier = [focusId];
      for (let d = 0; d < maxDepth; d++) {
        const next = [];
        for (const id of frontier) {
          const outs = this.edgesBySrc.get(id) || [];
          const ins = this.edgesByDst.get(id) || [];
          for (const e of outs) if (!dist.has(e.dst)) { dist.set(e.dst, d + 1); next.push(e.dst); }
          for (const e of ins) if (!dist.has(e.src)) { dist.set(e.src, d + 1); next.push(e.src); }
        }
        frontier = next;
        if (!frontier.length) break;
      }
    }
    this.focusDistance = dist;
    this.emit();
  }

  // Edges adjacent to `id` (either direction). Used by the detail panel.
  edgesIncidentTo(id) {
    return (this.edgesBySrc.get(id) || []).concat(this.edgesByDst.get(id) || []);
  }
}
