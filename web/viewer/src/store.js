// src/store.js — single source of truth for what's currently visible.
export class Store {
  constructor() {
    this.nodes = new Map(); // id -> node
    this.edges = [];
    this.visibleIds = new Set();
    this.lod = 0;
    this.hierarchyKind = 'pkg';
    this.listeners = new Set();
    this.searchResults = [];   // populated by search.js; empty -> list shows visible
    this.selectedId = null;    // most recently focused node
    // expanded[parentId] = Set of childIds revealed by an expand click. Used
    // by main.js focusNode to toggle: clicking an already-expanded node
    // collapses (recursively removing descendants from visibleIds).
    this.expanded = new Map();
  }
  subscribe(fn) { this.listeners.add(fn); return () => this.listeners.delete(fn); }
  emit() { this.listeners.forEach(fn => fn(this)); }
  loadNodes(arr) {
    if (!Array.isArray(arr)) return;
    for (const n of arr) {
      if (n && n.id) this.nodes.set(n.id, n);
    }
    this.emit();
  }
  setVisible(ids) { this.visibleIds = new Set(ids); this.emit(); }
  setLOD(n) { this.lod = n; this.emit(); }
  setHierarchy(k) { this.hierarchyKind = k; this.emit(); }
}
