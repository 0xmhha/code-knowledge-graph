export interface EdgeStyle {
  color?: number;
  width?: number;
  hidden?: boolean;
  dash?: boolean;
}

// Per-edge-type rendering style. Keys MUST match the backend EdgeType
// literals — keep in sync with pkg/types/enums.go AllEdgeTypes() (32 edges
// after schema 1.6 — timeout_path / cancellation_path appended for G3
// control-flow context propagation).
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
  // structural — previously all three shared 0x888888, which (with the
  // type-relation greys below) made the boot graph read as one uniform
  // dim colour on the dark canvas. Distinct hues per type; defines
  // stays the most muted since it is pure structure.
  contains:        { hidden: true },
  defines:         { color: 0x7f8fa6, width: 1, dash: true },
  imports:         { color: 0x64b5f6, width: 1 },
  exports:         { color: 0xffb74d, width: 1 },

  // call / invoke
  calls:           { color: 0xffffff, width: 1 },
  invokes:         { color: 0xffaa00, width: 1 },

  // G3 schema 1.6 — Go context.With* propagation. Both are self-loop
  // property markers (function/method → itself), so dashed dispels the
  // "flow" reading and matches the uses_type / accessed_under_lock idiom.
  // Color: amber for timeout (deadline budget) and red-orange for
  // cancellation (event-driven abort) — warm hues group them visually
  // with calls/invokes (the broader G3 family) without competing.
  timeout_path:      { color: 0xffcc44, width: 1, dash: true },
  cancellation_path: { color: 0xff5544, width: 1, dash: true },

  // G3 schema 1.10 (W-B W2) — TS async/await. Function/Method → AwaitPoint
  // ("suspension point"). Light blue dashed: dash signals "control yields
  // here" (annotation on a position, not a flow edge to another callable),
  // and blue family groups it with type relations / calls without
  // competing for the warm hues that own G3's main flow.
  awaits:            { color: 0x99ccff, width: 1, dash: true },

  // type relations — de-greyed (see structural note above): lavender /
  // green / bright grey so the three most common semantic edges are
  // tellable apart at a glance.
  uses_type:       { color: 0xb39ddb, width: 1, dash: true },
  instantiates:    { color: 0x81c784, width: 1, dash: true },
  references:      { color: 0xbcc6d4, width: 1, dash: true },
  extends:         { color: 0x6699ff, width: 2 },
  implements:      { color: 0x66ccff, width: 2, dash: true },
  // Schema 1.10 (W-C W2) — Solidity virtual/override inheritance link.
  // Deeper blue than extends so the inheritance chain reads top-down:
  // extends/implements (parent declaration) → overrides (specific method
  // override). Solid line because override IS a real call-resolution
  // target, not just an annotation.
  overrides:       { color: 0x3377cc, width: 2 },
  // Schema 1.10 (W-C W6) — Solidity `using Lib for T` library extension.
  // Amber dashed: warm hue groups it visually with has_modifier (cyan
  // metadata) / has_decorator (violet metadata) — all three are
  // "attached metadata" edges on a container, not flow. Distinct from
  // the cool blue inheritance family so the binding semantics doesn't
  // look like another inheritance hop. Dash signals "binding annotation"
  // (uses_type idiom).
  using_for:       { color: 0xddaa66, width: 1, dash: true },

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

  // G5 schema 1.9 — cross-language HTTP/gRPC interop (W1/W2 + W3a/b/c).
  //   http_calls:      Function/Method → Endpoint (HTTP client call site).
  //                    Warm orange family, lighter than rpc_calls so server-side
  //                    listens_on (bright blue) and client-side http_calls
  //                    (warm orange) read as a paired flow.
  //   grpc_listens_on: same shape as listens_on but bound to gRPC servers
  //                    (Go gRPC W3b). Purple to distinguish HTTP vs gRPC at
  //                    a glance; double-width like listens_on to match the
  //                    "entry point" reading weight.
  //   grpc_calls:      Function/Method → Endpoint (gRPC client call site;
  //                    Go W3b, TS W3c). Lighter purple than grpc_listens_on
  //                    so server vs client direction is visible in the same
  //                    family.
  http_calls:        { color: 0xffaa44, width: 1 },
  grpc_listens_on:   { color: 0x9966ff, width: 2 },
  grpc_calls:        { color: 0xcc88ff, width: 1 },

  // G6 Temporal (git history — schema 1.4, E4).
  // Off by default like other extension graphs; opt-in via filter UI.
  // Color choice: muted blue-grey + dashed for `changed_in` (annotation,
  // not a flow edge); muted brown for `blame` (file→last-touch commit).
  // Both kept low-contrast so they don't dominate when toggled on.
  changed_in:        { color: 0x888899, width: 1, dash: true },
  blame:             { color: 0xaa9988, width: 1 },

  // G6 Hunk-graph (schema 1.8, H1). The Hunk node sits between Commit
  // and CodeNode in the Temporal axis: `has_hunk` walks Commit → its
  // diff blocks; `adjacent` chains hunks within the same (commit, file)
  // pair in line-order so the EvidencePack assembler (H3) can stitch a
  // multi-hunk view without an extra ORDER BY query. Both stay low-
  // contrast and off by default — surfacing them by default would
  // double the visible edge count on real-history graphs (~1 has_hunk
  // per hunk + ~0.7 adjacent per hunk on average).
  has_hunk:          { color: 0x9988aa, width: 1 },
  adjacent:          { color: 0x776688, width: 1, dash: true },
  // H2 (schema 1.8): Hunk → CodeNode interval-overlap edge. Distinct
  // amber accent so it reads as the "what code did this hunk touch?"
  // surface — close in spirit to `defines` but originating from a
  // diff block rather than a containing file. Off by default; users
  // toggle on when they're inspecting a specific commit's footprint.
  modifies:          { color: 0xc4945c, width: 1 },

  // cross-language binding
  binds_to:        { color: 0xffd700, width: 3 },
};

// Default edge-type whitelist for trace + general view. The whitelist
// covers at least one representative edge from every CKS graph (G1..G6)
// so the user's first paint shows real structure across all axes — the
// previous defaults (G2/G3 only) left G1/G4/G5/G6 invisible until the
// user manually toggled their filters on, which made the boot view
// look broken on real graphs.
//
// Excluded by design (toggle on via filter UI):
//   - exports: rarely emitted by current parsers; would just clutter
//   - changed_in: ~46K edges on real repos — opt-in only
//   - has_hunk / adjacent: schema 1.8 H1 — ~700 hunks on the self-graph
//     (~1.4K combined edges) which would dominate the boot view's
//     temporal pill until the user is intentionally inspecting commits
//
// G3 schema 1.6 timeout_path / cancellation_path are on by default
// because they are sparse self-loops that highlight time-budgeted /
// cancellable functions (a useful reading aid, not noise).
export const DEFAULT_EDGE_TYPES: ReadonlyArray<string> = [
  // G1 Structural
  'defines', 'imports',
  // G2 Semantic — uses_type / instantiates added 2026-05-09 (graphify
  // audit): they fire ~12K + ~5K on the self-graph, which is comparable
  // to G1 imports' ~9K — well within the canvas budget — and surface
  // type relationships that were invisible by default. The user's "data
  // is missing" complaint was driven partly by these being off.
  // `overrides` (W-C W2, schema 1.10) on by default: Solidity inheritance
  // chains are a primary reading target whenever the dataset contains
  // .sol; sparse on non-Solidity datasets so no clutter risk.
  // `using_for` (W-C W6, schema 1.10) on by default for the same reason —
  // library binding visibility is the core value of Q9-1 (b) and the
  // edge is sparse on non-Solidity datasets.
  'extends', 'implements', 'overrides', 'using_for', 'uses_type', 'instantiates',
  // G3 Execution — `awaits` (W-B W2, schema 1.10) on by default: TS
  // async-heavy code is unreadable without suspension points; dashed
  // style keeps it visually subordinate to calls/invokes.
  'calls', 'invokes', 'timeout_path', 'cancellation_path', 'awaits',
  // G4 Concurrency
  'spawns', 'sends_to', 'recvs_from',
  'acquires_lock', 'releases_lock', 'accessed_under_lock',
  // G5 Distributed — schema 1.9 (W series) added http_calls /
  // grpc_listens_on / grpc_calls. All three on by default because they
  // each surface a *direction* of cross-process flow that the existing
  // listens_on / rpc_calls don't cover:
  //   listens_on (HTTP server) ↔ http_calls (HTTP client)
  //   grpc_listens_on (Go gRPC server) ↔ grpc_calls (Go/TS gRPC client)
  // Without both sides the boot view shows a one-way arrow into "the
  // network" that confused first-time users.
  'listens_on', 'handles_message', 'rpc_calls', 'binds_to',
  'http_calls', 'grpc_listens_on', 'grpc_calls',
  // G6 Temporal
  'blame',
];

// Track C P0/P1c (uses_type / instantiates) are intentionally OFF by default:
// they fire densely on symbol-rich graphs (~500 + ~300 on the self-graph),
// which would dominate the boot view's edge count and obscure the call-flow
// signal that's the primary reading aid. Users opt in via the G2 group toggle
// in the filter UI.

// All known types — used by the EdgeTypeFilters component to render checkboxes.
// Derived from EDGE_STYLE so there is a single source of truth for the key set.
export const ALL_EDGE_TYPES: ReadonlyArray<string> =
  Object.keys(EDGE_STYLE).filter(k => !EDGE_STYLE[k].hidden);

// 6-graph axis — CKS deep-dive § 4.1. EdgeTypeFilters renders one
// collapsible section per graph; group toggle selects/deselects all
// edges in the group at once.
//
// Source of truth: the backend AllEdgeTypes() in pkg/types/enums.go.
// Edges absent from this map (only `contains` today, which is hidden)
// don't appear in the filter UI.
//
// Placement notes (spec deviations):
//   - `uses_type`, `instantiates` → G2 (type relations; not enumerated
//     in spec § 4.1 G2 but conceptually fit there).
//   - `binds_to` → G5 (the existing implementation of `xlang_calls`
//     enumerated in spec § 4.1 G5).
//   - `emits_event` → G2 (equivalent to spec's `emits` in § 4.1 G2).
export type GraphID = 'G1' | 'G2' | 'G3' | 'G4' | 'G5' | 'G6';

export interface GraphGroupSpec {
  id: GraphID;
  label: string;        // human-readable label (e.g. "Structural")
  description: string;  // tooltip text
  color: number;        // header accent (matches dominant edge color in group)
  edges: ReadonlyArray<string>;  // edge type names belonging to this graph
}

export const GRAPH_GROUPS: ReadonlyArray<GraphGroupSpec> = [
  {
    id: 'G1', label: 'Structural', color: 0x888888,
    description: 'Physical code structure: contains, defines, imports, exports',
    edges: ['defines', 'imports', 'exports'],  // contains is hidden
  },
  {
    id: 'G2', label: 'Semantic', color: 0x6699ff,
    description: 'Type and field relations: references, implements, extends, overrides, using_for binding, field/mapping reads/writes, modifier/decorator',
    edges: ['uses_type', 'instantiates', 'references', 'implements', 'extends', 'overrides',
            'using_for',
            'reads_field', 'writes_field', 'reads_mapping', 'writes_mapping',
            'emits_event', 'has_modifier', 'has_decorator'],
  },
  {
    id: 'G3', label: 'Execution', color: 0xffffff,
    description: 'Call and invocation flow; context.With* timeout/cancellation markers; async/await suspension points',
    edges: ['calls', 'invokes', 'timeout_path', 'cancellation_path', 'awaits'],
  },
  {
    id: 'G4', label: 'Concurrency', color: 0xff66cc,
    description: 'Goroutines, channels, mutexes, lock semantics',
    edges: ['spawns', 'sends_to', 'recvs_from',
            'acquires_lock', 'releases_lock', 'accessed_under_lock'],
  },
  {
    id: 'G5', label: 'Distributed', color: 0x44aaff,
    description: 'Handler/RPC topology, HTTP/gRPC client↔server flow, cross-language bindings',
    edges: ['listens_on', 'handles_message', 'rpc_calls', 'binds_to',
            'http_calls', 'grpc_listens_on', 'grpc_calls'],
  },
  {
    id: 'G6', label: 'Temporal', color: 0x888899,
    description: 'Git history: changed_in (symbol→commit), blame (file→last commit), has_hunk (commit→hunk), adjacent (hunk→hunk in same file), modifies (hunk→CodeNode AST overlap)',
    edges: ['changed_in', 'blame', 'has_hunk', 'adjacent', 'modifies'],
  },
];

// edgeToGroup: reverse lookup. Returns null for unknown/hidden edges.
export function edgeToGroup(edgeType: string): GraphID | null {
  for (const g of GRAPH_GROUPS) {
    if (g.edges.includes(edgeType)) return g.id;
  }
  return null;
}

// groupHasAllEdges: returns true if every edge in the group is in the
// whitelist (used to render "fully on" group state).
export function groupHasAllEdges(group: GraphGroupSpec, whitelist: ReadonlySet<string>): boolean {
  return group.edges.every(e => whitelist.has(e));
}

// groupHasAnyEdge: returns true if at least one edge in the group is in
// the whitelist (used to render the indeterminate group toggle state).
export function groupHasAnyEdge(group: GraphGroupSpec, whitelist: ReadonlySet<string>): boolean {
  return group.edges.some(e => whitelist.has(e));
}

// ── Self-check (build-time sanity) ─────────────────────────────────────
// Every non-hidden edge in EDGE_STYLE MUST appear in exactly one group.
// When bumping the schema (new EdgeType in pkg/types/enums.go), add an
// EDGE_STYLE entry AND assign it to a GRAPH_GROUPS bucket — otherwise
// it silently disappears from the filter UI.
//
// Current state (schema 1.10 + W series + W-C W6):
//   40 non-hidden edges in EDGE_STYLE (41 total - `contains` hidden)
//   40 edges across GRAPH_GROUPS:
//     G1=3, G2=14 (+overrides, +using_for), G3=5 (+awaits), G4=6,
//     G5=7 (+http_calls, +grpc_listens_on, +grpc_calls), G6=5
//
// To verify after editing this file, eyeball the output of:
//   node -e "const {ALL_EDGE_TYPES, GRAPH_GROUPS} = require('./edges'); \
//     const g = new Set(GRAPH_GROUPS.flatMap(x => x.edges)); \
//     console.log('missing:', ALL_EDGE_TYPES.filter(e => !g.has(e))); \
//     console.log('orphan:', [...g].filter(e => !ALL_EDGE_TYPES.includes(e)));"
