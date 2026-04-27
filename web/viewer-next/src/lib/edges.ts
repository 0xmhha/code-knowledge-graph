export interface EdgeStyle {
  color?: number;
  width?: number;
  hidden?: boolean;
  dash?: boolean;
}

export const EDGE_STYLE: Record<string, EdgeStyle> = {
  contains:      { hidden: true },
  calls:         { color: 0xffffff, width: 1 },
  invokes:       { color: 0xffaa00, width: 1 },
  uses_type:     { color: 0xaaaaaa, width: 1, dash: true },
  instantiates:  { color: 0xaaaaaa, width: 1, dash: true },
  references:    { color: 0xaaaaaa, width: 1, dash: true },
  extends:       { color: 0x6699ff, width: 2 },
  implements:    { color: 0x66ccff, width: 2, dash: true },
  binds_to:      { color: 0xffd700, width: 3 },
  imports:       { color: 0x888888, width: 1 },
  exports:       { color: 0x888888, width: 1 },
  spawns:        { color: 0xff66cc, width: 1 },
  sends_to:      { color: 0xff99cc, width: 1 },
  receives_from: { color: 0xcc99ff, width: 1 },
  reads:         { color: 0x99ff99, width: 1 },
  writes:        { color: 0xff9999, width: 1 },
  modifies:      { color: 0xffcc66, width: 1 },
  decorates:     { color: 0xcc99ff, width: 1 },
  emits:         { color: 0xffaa00, width: 1, dash: true },
};

// Default edge-type whitelist for trace + general view. Call-flow oriented:
// the graph stays readable while preserving cross-language bindings and
// inheritance edges that are semantically structural, not just data-flow.
export const DEFAULT_EDGE_TYPES: ReadonlyArray<string> = [
  'calls', 'invokes', 'binds_to', 'extends', 'implements',
];

// All known types — used by the EdgeTypeFilters component to render checkboxes.
export const ALL_EDGE_TYPES: ReadonlyArray<string> =
  Object.keys(EDGE_STYLE).filter(k => !EDGE_STYLE[k].hidden);
