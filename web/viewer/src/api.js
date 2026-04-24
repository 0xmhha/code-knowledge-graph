// src/api.js
// In `serve` mode the viewer fetches /api/* (live SQLite). In static export
// mode it fetches ./nodes/chunk_NNNN.json etc. Both expose the same surface.
export class API {
  constructor(base = '') { this.base = base; }
  async manifest() { return fetch(`${this.base}/api/manifest`).then(r => r.json()); }
  async hierarchy(kind = 'pkg') { return fetch(`${this.base}/api/hierarchy?kind=${kind}`).then(r => r.json()); }
  async nodes(parentId = '', limit = 5000) {
    const q = new URLSearchParams({ limit: String(limit) });
    if (parentId) q.set('parent', parentId);
    return fetch(`${this.base}/api/nodes?${q}`).then(r => r.json());
  }
  async edges(nodeIds) {
    return fetch(`${this.base}/api/edges`, {
      method: 'POST', headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ ids: nodeIds })
    }).then(r => r.json());
  }
  async blob(nodeId) { return fetch(`${this.base}/api/blob/${nodeId}`).then(r => r.text()); }
  async search(q) { return fetch(`${this.base}/api/search?q=${encodeURIComponent(q)}`).then(r => r.json()); }
}
