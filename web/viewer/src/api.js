// src/api.js
// In `serve` mode the viewer fetches /api/* (live SQLite). In static export
// mode it fetches ./nodes/chunk_NNNN.json etc. Both expose the same surface.
// asArray normalises any list-shaped response. The Go backend serialises
// nil slices as JSON `null` (rather than `[]`), and an unforeseen shape
// (e.g. a malformed proxy response) would explode if downstream code
// assumed an array. Force-array everywhere we call /api/*.
const asArray = v => (Array.isArray(v) ? v : []);

export class API {
  constructor(base = '') { this.base = base; }
  async manifest() { return fetch(`${this.base}/api/manifest`).then(r => r.json()); }
  async hierarchy(kind = 'pkg') { return fetch(`${this.base}/api/hierarchy?kind=${kind}`).then(r => r.json()).then(asArray); }
  async nodes(parentId = '', limit = 5000) {
    const q = new URLSearchParams({ limit: String(limit) });
    if (parentId) q.set('parent', parentId);
    return fetch(`${this.base}/api/nodes?${q}`).then(r => r.json()).then(asArray);
  }
  async edges(nodeIds) {
    return fetch(`${this.base}/api/edges`, {
      method: 'POST', headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ ids: nodeIds })
    }).then(r => r.json()).then(asArray);
  }
  async blob(nodeId) { return fetch(`${this.base}/api/blob/${nodeId}`).then(r => r.text()); }
  async search(q) { return fetch(`${this.base}/api/search?q=${encodeURIComponent(q)}`).then(r => r.json()).then(asArray); }
}

// detectMode probes ./manifest.json — if a static export placed a manifest
// next to index.html, we're in static mode. Wrapped in try/catch because a
// failed fetch (CORS, network, 404) throws rather than resolving with !ok.
export async function detectMode() {
  try {
    const r = await fetch('./manifest.json', { cache: 'no-store' });
    if (r.ok) return 'static';
  } catch (_) { /* fall through to serve */ }
  return 'serve';
}

// StaticAPI mirrors API's surface but sources everything from the chunked
// JSON bundle written by `ckg export-static`. No server: all filtering that
// the HTTP API does server-side (parent scoping, edge neighbourhoods) is
// done client-side here over the full node/edge arrays.
//
// V0 trade-off: search() returns [] because client-side FTS is V1 work.
export class StaticAPI {
  constructor() {
    this._nodesCache = null;
    this._edgesCache = null;
    this._pkgTreeCache = null;
  }

  async manifest() {
    return fetch('./manifest.json', { cache: 'no-store' }).then(r => r.json());
  }

  async hierarchy(kind = 'pkg') {
    const file = kind === 'topic' ? 'topic_tree.json' : 'pkg_tree.json';
    return fetch(`./hierarchy/${file}`).then(r => r.json()).then(v => v || []);
  }

  async _allNodes() {
    if (!this._nodesCache) this._nodesCache = await concatChunks('nodes');
    return this._nodesCache;
  }

  async _allEdges() {
    if (!this._edgesCache) this._edgesCache = await concatChunks('edges');
    return this._edgesCache;
  }

  async _pkgTree() {
    if (!this._pkgTreeCache) this._pkgTreeCache = await this.hierarchy('pkg');
    return this._pkgTreeCache;
  }

  async nodes(parentId = '', limit = 5000) {
    const all = await this._allNodes();
    if (!parentId) {
      // Serve-mode returns top-level Packages when parent is empty; mirror
      // that so main.js bootstrap doesn't diverge by transport.
      return all.filter(n => n.type === 'Package').slice(0, limit);
    }
    const tree = await this._pkgTree();
    const childIds = new Set(
      tree.filter(r => r.parent_id === parentId).map(r => r.child_id)
    );
    return all.filter(n => childIds.has(n.id)).slice(0, limit);
  }

  async edges(nodeIds) {
    const ids = new Set(nodeIds);
    const all = await this._allEdges();
    return all.filter(e => ids.has(e.src) || ids.has(e.dst));
  }

  async blob(nodeId) {
    return fetch(`./blobs/${nodeId}.txt`).then(r => r.ok ? r.text() : '');
  }

  // V0: FTS is server-only; static bundle has no index, and shipping one is
  // V1 work. Return empty so the search box fails gracefully.
  async search(_q) { return []; }
}

// concatChunks walks ./prefix/chunk_0000.json, 0001, ... until a non-OK
// response (typically 404), concatenating the arrays. The chunk naming is
// dense (no gaps) so stopping on the first miss is safe.
async function concatChunks(prefix) {
  const out = [];
  for (let i = 0; ; i++) {
    const path = `./${prefix}/chunk_${String(i).padStart(4, '0')}.json`;
    let r;
    try { r = await fetch(path); } catch (_) { break; }
    if (!r.ok) break;
    const arr = await r.json();
    if (Array.isArray(arr)) out.push(...arr);
  }
  return out;
}
