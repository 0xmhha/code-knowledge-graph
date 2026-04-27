import * as THREE from 'three';
import type { GraphNode, ColorMode } from '@/types';

const LANG_COLOR_HEX: Record<string, number> = { go: 0x00add8, ts: 0x3178c6, sol: 0x3c3c3d };
const LANG_COLOR_2D: Record<string, string> = { go: '#00add8', ts: '#3178c6', sol: '#3c3c3d' };

export const ALPHA_BY_CONF: Record<string, number> = {
  EXTRACTED: 1.0, INFERRED: 0.7, AMBIGUOUS: 0.4,
};

// Decision #1 (recommended): hash-based deterministic HSL.
// - Same community always yields the same hue across sessions.
// - Hue spread uses the 137.5° golden angle so adjacent IDs land on
//   visually distinct hues (the same trick used by D3's interpolateRainbow
//   variants).
// - Saturation/Lightness fixed in a band that reads well on dark bg.
export function communityColorHex(communityId: number | undefined | null): number {
  if (communityId == null) return 0x888888;
  const hue = (communityId * 137.508) % 360;
  return hslToRgbHex(hue / 360, 0.55, 0.55);
}

export function communityColorCss(communityId: number | undefined | null): string {
  if (communityId == null) return '#888888';
  const hue = ((communityId * 137.508) % 360 + 360) % 360;
  return `hsl(${hue.toFixed(0)} 55% 55%)`;
}

export function nodeColorHex(node: GraphNode, mode: ColorMode): number {
  if (mode === 'community' && node.community_id != null) return communityColorHex(node.community_id);
  return LANG_COLOR_HEX[node.language ?? ''] ?? 0x888888;
}

export function nodeColorCss(node: GraphNode, mode: ColorMode): string {
  if (mode === 'community' && node.community_id != null) return communityColorCss(node.community_id);
  return LANG_COLOR_2D[node.language ?? ''] ?? '#888888';
}

const PRIMITIVE: Record<string, string> = {
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

const GEOM: Record<string, THREE.BufferGeometry> = {};
function geom(kind: string): THREE.BufferGeometry {
  if (GEOM[kind]) return GEOM[kind];
  let g: THREE.BufferGeometry;
  switch (kind) {
    case 'sphereLg':     g = new THREE.SphereGeometry(8, 16, 12); break;
    case 'sphereSm':     g = new THREE.SphereGeometry(2, 8, 6); break;
    case 'hex':          g = new THREE.CylinderGeometry(5, 5, 8, 6); break;
    case 'cube':         g = new THREE.BoxGeometry(5, 5, 5); break;
    case 'cubeSm':       g = new THREE.BoxGeometry(3, 3, 3); break;
    case 'cubeFlat':     g = new THREE.BoxGeometry(4, 1, 4); break;
    case 'cubeFlatSm':   g = new THREE.BoxGeometry(2.5, 0.7, 2.5); break;
    case 'cubeTiny':     g = new THREE.BoxGeometry(1.5, 1.5, 1.5); break;
    case 'torus':        g = new THREE.TorusGeometry(4, 1, 8, 16); break;
    case 'cylinder':     g = new THREE.CylinderGeometry(4, 4, 7); break;
    case 'diamond':      g = new THREE.OctahedronGeometry(4); break;
    case 'pyramid':      g = new THREE.ConeGeometry(4, 6, 4); break;
    case 'star':         g = new THREE.OctahedronGeometry(6, 1); break;
    case 'donut':        g = new THREE.TorusGeometry(4, 2, 8, 16); break;
    case 'starburst':    g = new THREE.IcosahedronGeometry(5, 0); break;
    case 'coneLg':       g = new THREE.ConeGeometry(5, 8); break;
    case 'coneSm':       g = new THREE.ConeGeometry(3, 5); break;
    case 'coneSpec':     g = new THREE.ConeGeometry(5, 9, 6); break;
    case 'coneBranched': g = new THREE.ConeGeometry(4, 6, 4); break;
    case 'tetra':        g = new THREE.TetrahedronGeometry(5); break;
    case 'ring':         g = new THREE.TorusGeometry(3, 0.5, 4, 12); break;
    case 'ringExp':      g = new THREE.TorusGeometry(3, 0.5, 4, 12); break;
    case 'ringSpike':    g = new THREE.TorusGeometry(3, 1, 6, 12); break;
    case 'pipe':         g = new THREE.CylinderGeometry(2, 2, 8); break;
    case 'plane':        g = new THREE.PlaneGeometry(4, 4); break;
    default:             g = new THREE.SphereGeometry(3, 8, 6); break;
  }
  GEOM[kind] = g;
  return g;
}

export function nodeMesh(n: GraphNode, mode: ColorMode): THREE.Mesh {
  const kind = PRIMITIVE[n.type ?? ''] || 'sphereSm';
  const g = geom(kind);
  const mat = new THREE.MeshStandardMaterial({
    color: nodeColorHex(n, mode),
    transparent: true,
    opacity: ALPHA_BY_CONF[n.confidence ?? ''] ?? 1.0,
  });
  const mesh = new THREE.Mesh(g, mat);
  const scale = 0.5 + Math.log10((n.usage_score ?? 0) + 1) * 0.6;
  mesh.scale.setScalar(Math.max(0.5, Math.min(3.5, scale)));
  return mesh;
}

function hslToRgbHex(h: number, s: number, l: number): number {
  const f = (n: number) => {
    const k = (n + h * 12) % 12;
    const a = s * Math.min(l, 1 - l);
    const c = l - a * Math.max(-1, Math.min(k - 3, 9 - k, 1));
    return Math.round(c * 255);
  };
  return (f(0) << 16) | (f(8) << 8) | f(4);
}
