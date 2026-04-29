export interface EdgeStyle {
  color?: number;
  width?: number;
  hidden?: boolean;
  dash?: boolean;
}

// Per-edge-type rendering style. Keys MUST match the backend EdgeType
// literals — keep in sync with pkg/types/enums.go AllEdgeTypes() (30 edges).
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
//   - lock semantics (acquires_lock/releases_lock/accessed_under_lock):
//       red (acquire/write) / green (release) / amber dashed (annotation, not flow)
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

  // lock semantics (schema 1.1 slot reservation; emission lands in B1 / Wave 5).
  // Off by default like other concurrency edges — toggle on via filters.
  // Color choice: acquire=red (write/grab), release=green (free), accessed_under_lock=
  // amber dashed (annotation linking a field-access to the lock that guards it,
  // not a flow edge — dash signals "metadata", same idiom as uses_type).
  acquires_lock:        { color: 0xff5577, width: 1 },
  releases_lock:        { color: 0x55cc77, width: 1 },
  accessed_under_lock:  { color: 0xffcc66, width: 1, dash: true },

  // G5 Distributed (handler/RPC topology — schema 1.3, E3).
  // Off by default like other extension graphs; opt-in via filter UI.
  // Color choice: bright blue for entry points (HTTP/RPC routes), teal
  // dashed for message-dispatch annotation (mirrors uses_type idiom),
  // orange for outbound RPC client→server flow (mirrors invokes warmth).
  listens_on:        { color: 0x44aaff, width: 2 },
  handles_message:   { color: 0x44ccaa, width: 1, dash: true },
  rpc_calls:         { color: 0xff9944, width: 1 },

  // G6 Temporal (git history — schema 1.4, E4).
  // Off by default like other extension graphs; opt-in via filter UI.
  // Color choice: muted blue-grey + dashed for `changed_in` (annotation,
  // not a flow edge); muted brown for `blame` (file→last-touch commit).
  // Both kept low-contrast so they don't dominate when toggled on.
  changed_in:        { color: 0x888899, width: 1, dash: true },
  blame:             { color: 0xaa9988, width: 1 },

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
