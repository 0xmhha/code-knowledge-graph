// src/encoding.js
// Visual encoding: 29 NodeType -> THREE primitive geometry, language -> color,
// usage_score (log10) -> mesh scale, confidence -> opacity. See spec §7.3.
import * as THREE from 'three';

const LANG_COLOR = { go: 0x00add8, ts: 0x3178c6, sol: 0x3c3c3d };
const ALPHA = { EXTRACTED: 1.0, INFERRED: 0.7, AMBIGUOUS: 0.4 };

const PRIMITIVE = {
  Package: 'sphereLg', File: 'hex', Struct: 'cube', Interface: 'torus',
  Class: 'cylinder', TypeAlias: 'diamond', Enum: 'pyramid', Contract: 'star',
  Mapping: 'donut', Event: 'starburst', Function: 'coneLg', Method: 'coneSm',
  Modifier: 'tetra', Constructor: 'coneSpec', Constant: 'sphereSm',
  Variable: 'cubeSm', Field: 'cubeFlat', Parameter: 'cubeFlatSm',
  LocalVariable: 'cubeTiny', Import: 'ring', Export: 'ringExp',
  Decorator: 'ringSpike', Goroutine: 'coneBranched', Channel: 'pipe',
  IfStmt: 'plane', LoopStmt: 'plane', SwitchStmt: 'plane',
  ReturnStmt: 'plane', CallSite: 'plane',
};

const GEOM = {};
function geom(kind) {
  if (GEOM[kind]) return GEOM[kind];
  switch (kind) {
    case 'sphereLg':     return GEOM[kind] = new THREE.SphereGeometry(8, 16, 12);
    case 'sphereSm':     return GEOM[kind] = new THREE.SphereGeometry(2, 8, 6);
    case 'hex':          return GEOM[kind] = new THREE.CylinderGeometry(5, 5, 8, 6);
    case 'cube':         return GEOM[kind] = new THREE.BoxGeometry(5, 5, 5);
    case 'cubeSm':       return GEOM[kind] = new THREE.BoxGeometry(3, 3, 3);
    case 'cubeFlat':     return GEOM[kind] = new THREE.BoxGeometry(4, 1, 4);
    case 'cubeFlatSm':   return GEOM[kind] = new THREE.BoxGeometry(2.5, 0.7, 2.5);
    case 'cubeTiny':     return GEOM[kind] = new THREE.BoxGeometry(1.5, 1.5, 1.5);
    case 'torus':        return GEOM[kind] = new THREE.TorusGeometry(4, 1, 8, 16);
    case 'cylinder':     return GEOM[kind] = new THREE.CylinderGeometry(4, 4, 7);
    case 'diamond':      return GEOM[kind] = new THREE.OctahedronGeometry(4);
    case 'pyramid':      return GEOM[kind] = new THREE.ConeGeometry(4, 6, 4);
    case 'star':         return GEOM[kind] = new THREE.OctahedronGeometry(6, 1);
    case 'donut':        return GEOM[kind] = new THREE.TorusGeometry(4, 2, 8, 16);
    case 'starburst':    return GEOM[kind] = new THREE.IcosahedronGeometry(5, 0);
    case 'coneLg':       return GEOM[kind] = new THREE.ConeGeometry(5, 8);
    case 'coneSm':       return GEOM[kind] = new THREE.ConeGeometry(3, 5);
    case 'coneSpec':     return GEOM[kind] = new THREE.ConeGeometry(5, 9, 6);
    case 'coneBranched': return GEOM[kind] = new THREE.ConeGeometry(4, 6, 4);
    case 'tetra':        return GEOM[kind] = new THREE.TetrahedronGeometry(5);
    case 'ring':         return GEOM[kind] = new THREE.TorusGeometry(3, 0.5, 4, 12);
    case 'ringExp':      return GEOM[kind] = new THREE.TorusGeometry(3, 0.5, 4, 12);
    case 'ringSpike':    return GEOM[kind] = new THREE.TorusGeometry(3, 1, 6, 12);
    case 'pipe':         return GEOM[kind] = new THREE.CylinderGeometry(2, 2, 8);
    case 'plane':        return GEOM[kind] = new THREE.PlaneGeometry(4, 4);
    default:             return GEOM[kind] = new THREE.SphereGeometry(3, 8, 6);
  }
}

export function nodeMesh(n) {
  const kind = PRIMITIVE[n.type] || 'sphereSm';
  const g = geom(kind);
  const mat = new THREE.MeshStandardMaterial({
    color: LANG_COLOR[n.language] || 0x888888,
    transparent: true,
    opacity: ALPHA[n.confidence] || 1.0,
  });
  const mesh = new THREE.Mesh(g, mat);
  const scale = 0.5 + Math.log10((n.usage_score || 0) + 1) * 0.6;
  mesh.scale.setScalar(Math.max(0.5, Math.min(3.5, scale)));
  return mesh;
}

// EDGE_STYLE: per spec §7.3 edge table. `hidden` removes edge from rendering.
// `width` overrides default thickness. Color is a hex int (rendered to '#rrggbb').
// Defaults (1px, 0x999999, solid) cover any unlisted edge type.
export const EDGE_STYLE = {
  // Structural (V0 hides containment to declutter; reachable via panel).
  contains:      { hidden: true },

  // Call / invocation — solid, bright.
  calls:         { color: 0xffffff, width: 1 },
  invokes:       { color: 0xffaa00, width: 1 },

  // Type relationships — dashed, muted.
  uses_type:     { color: 0xaaaaaa, width: 1, dash: true },
  instantiates:  { color: 0xaaaaaa, width: 1, dash: true },
  references:    { color: 0xaaaaaa, width: 1, dash: true },

  // Inheritance / interface — solid, blue-ish.
  extends:       { color: 0x6699ff, width: 2 },
  implements:    { color: 0x66ccff, width: 2, dash: true },

  // Cross-language binding — gold, thick.
  binds_to:      { color: 0xffd700, width: 3 },

  // Imports — thin grey.
  imports:       { color: 0x888888, width: 1 },
  exports:       { color: 0x888888, width: 1 },

  // Concurrency.
  spawns:        { color: 0xff66cc, width: 1 },
  sends_to:      { color: 0xff99cc, width: 1 },
  receives_from: { color: 0xcc99ff, width: 1 },

  // Data flow.
  reads:         { color: 0x99ff99, width: 1 },
  writes:        { color: 0xff9999, width: 1 },

  // Modifiers / decorators.
  modifies:      { color: 0xffcc66, width: 1 },
  decorates:     { color: 0xcc99ff, width: 1 },

  // Solidity events.
  emits:         { color: 0xffaa00, width: 1, dash: true },
};
