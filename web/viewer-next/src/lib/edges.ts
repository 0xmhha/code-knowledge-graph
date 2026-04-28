export interface EdgeStyle {
  color?: number;
  width?: number;
  hidden?: boolean;
  dash?: boolean;
}

// Per-edge-type rendering style. Keys MUST match the backend EdgeType
// literals — keep in sync with pkg/types/enums.go AllEdgeTypes() (22 edges).
//
// `contains` is intentionally hidden in the viewer: it's the structural
// parent-child edge that would otherwise dominate the layout.
//
// Visual grouping conventions:
//   - structural (contains/defines/imports/exports): muted grey, dashed for "soft"
//   - call/invoke: high-contrast (white / orange)
//   - type relations (uses_type/instantiates/implements/extends/references): blue family
//   - field & mapping reads: green; writes: red; emits_event: orange dashed
//   - has_modifier / has_decorator: cyan / violet
//   - concurrency (spawns/sends_to/recvs_from): pink/magenta family
//   - binds_to: gold (highest-attention cross-language link)
//
// TODO: when vitest lands (WORK-PLAN Wave-5+), add edges.test.ts asserting
// EDGE_STYLE keys match schema.
export const EDGE_STYLE: Record<string, EdgeStyle> = {
  // structural
  contains:        { hidden: true },
  defines:         { color: 0x888888, width: 1, dash: true },
  imports:         { color: 0x888888, width: 1 },
  exports:         { color: 0x888888, width: 1 },

  // call / invoke
  calls:           { color: 0xffffff, width: 1 },
  invokes:         { color: 0xffaa00, width: 1 },

  // type relations
  uses_type:       { color: 0xaaaaaa, width: 1, dash: true },
  instantiates:    { color: 0xaaaaaa, width: 1, dash: true },
  references:      { color: 0xaaaaaa, width: 1, dash: true },
  extends:         { color: 0x6699ff, width: 2 },
  implements:      { color: 0x66ccff, width: 2, dash: true },

  // field reads/writes
  reads_field:     { color: 0x99ff99, width: 1 },
  writes_field:    { color: 0xff9999, width: 1 },

  // solidity mapping reads/writes + event emission
  reads_mapping:   { color: 0x66cc99, width: 1 },
  writes_mapping:  { color: 0xcc6666, width: 1 },
  emits_event:     { color: 0xff7733, width: 1, dash: true },

  // attached metadata
  has_modifier:    { color: 0x66e0e0, width: 1 },
  has_decorator:   { color: 0xcc99ff, width: 1 },

  // concurrency / channels / goroutines (kept tightly within the magenta family
  // so the triple reads as a group; recvs_from stays distinct from has_decorator)
  spawns:          { color: 0xff66cc, width: 1 },
  sends_to:        { color: 0xff99cc, width: 1 },
  recvs_from:      { color: 0xcc66cc, width: 1 },

  // cross-language binding
  binds_to:        { color: 0xffd700, width: 3 },
};

// Default edge-type whitelist for trace + general view. Call-flow oriented:
// the graph stays readable while preserving cross-language bindings and
// inheritance edges that are semantically structural, not just data-flow.
export const DEFAULT_EDGE_TYPES: ReadonlyArray<string> = [
  'calls', 'invokes', 'binds_to', 'extends', 'implements',
];

// All known types — used by the EdgeTypeFilters component to render checkboxes.
// Derived from EDGE_STYLE so there is a single source of truth for the key set.
export const ALL_EDGE_TYPES: ReadonlyArray<string> =
  Object.keys(EDGE_STYLE).filter(k => !EDGE_STYLE[k].hidden);
