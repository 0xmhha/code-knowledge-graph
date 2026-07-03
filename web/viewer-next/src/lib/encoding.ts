import * as THREE from 'three';
import type { GraphNode, ColorMode } from '@/types';

// Language palette. Brightened for the dark canvas — the previous
// values were brand colours picked for light backgrounds: Solidity's
// 0x3c3c3d (near-black) was invisible and the 0x888888 fallback read
// as "disabled". Hues kept recognisable (Go cyan, TS blue, Sol amber).
const LANG_COLOR_HEX: Record<string, number> = { go: 0x29c7e8, ts: 0x5c9eff, sol: 0xffc46b };
const FALLBACK_HEX = 0xb0b3c6;

// As-you-type search highlight (projector-style): matched nodes tint
// warm red while the query is non-empty, restoring their palette colour
// the moment the query clears. Deliberately outside every palette above
// so a highlighted node is unambiguous in any color mode.
export const SEARCH_HIGHLIGHT_HEX = 0xff5a4f;
export const SEARCH_HIGHLIGHT_CSS = '#ff5a4f';

// hexCss: single source of truth between the 3D (hex number) and 2D
// (CSS string) colour paths — the old duplicated tables drifted.
function hexCss(hex: number): string {
  return `#${hex.toString(16).padStart(6, '0')}`;
}

// Node-type palette for ColorMode 'type'. Grouped into families so the
// canvas reads as "containers amber · types cyan · callables purple ·
// data green · concurrency pink · sol-contract yellow · statements
// muted". All values chosen bright-on-dark (#0d0e10 background).
const TYPE_COLOR_HEX: Record<string, number> = {
  // containers
  Package: 0xffd166, File: 0xf4a261,
  // type declarations
  Struct: 0x4cc9f0, Interface: 0x90e0ef, Class: 0x56cfe1,
  TypeAlias: 0x48bfe3, Type: 0x4cc9f0, Enum: 0x64dfdf,
  // callables
  Function: 0xc77dff, Method: 0xe0aaff, Constructor: 0x9d4edd,
  Modifier: 0xf72585, Decorator: 0xcdb4db,
  // data
  Constant: 0x80ed99, Variable: 0x57cc99, Field: 0x56e39f,
  Parameter: 0x99e2b4, LocalVariable: 0x67e0a3,
  // solidity
  Contract: 0xffd60a, Mapping: 0xffc300, Event: 0xff9e00,
  // concurrency
  Goroutine: 0xff5d8f, Channel: 0xff8fab, Mutex: 0xff4d6d,
  // infra / temporal
  Import: 0x8ecae6, Export: 0xffb703, Commit: 0xffe066,
  Hunk: 0xbdb2ff, Endpoint: 0x4ea8de,
  // statement-level noise recedes
  CallSite: 0x778091, IfStmt: 0x778091, LoopStmt: 0x778091,
  SwitchStmt: 0x778091, ReturnStmt: 0x778091,
};

export const ALPHA_BY_CONF: Record<string, number> = {
  EXTRACTED: 1.0, INFERRED: 0.7, AMBIGUOUS: 0.4,
};

// Decision #1 (recommended): hash-based deterministic HSL.
// - Same community always yields the same hue across sessions.
// - Hue spread uses the 137.5° golden angle so adjacent IDs land on
//   visually distinct hues (the same trick used by D3's interpolateRainbow
//   variants).
// - S/L: 55%/55% 는 2D 캔버스에선 읽혔지만 3D 에서 어두웠다 — Lambert
//   재질은 조명 각도에 따라 원색보다 어둡게 렌더되므로, 다크 배경에서
//   커뮤니티 색이 배경에 묻혔다(user feedback). 72%/66% 로 상향해 조명
//   감쇠 후에도 밝은 색으로 남게 한다. null 폴백도 0x888888 → 밝은 회색.
export function communityColorHex(communityId: number | undefined | null): number {
  if (communityId == null) return 0xb0b3c6;
  const hue = (communityId * 137.508) % 360;
  return hslToRgbHex(hue / 360, 0.72, 0.66);
}

export function communityColorCss(communityId: number | undefined | null): string {
  if (communityId == null) return '#b0b3c6';
  const hue = ((communityId * 137.508) % 360 + 360) % 360;
  return `hsl(${hue.toFixed(0)} 72% 66%)`;
}

// typeColorCss: CanvasLegend 등 UI 가 TYPE 팔레트를 노드 렌더와 동일한
// 소스에서 읽게 하는 헬퍼 — 범례 색과 캔버스 색이 드리프트하지 않는다.
export function typeColorCss(type: string): string {
  return hexCss(TYPE_COLOR_HEX[type] ?? FALLBACK_HEX);
}

export function nodeColorHex(node: GraphNode, mode: ColorMode): number {
  if (mode === 'community' && node.community_id != null) return communityColorHex(node.community_id);
  if (mode === 'type') return TYPE_COLOR_HEX[node.type ?? ''] ?? FALLBACK_HEX;
  return LANG_COLOR_HEX[node.language ?? ''] ?? FALLBACK_HEX;
}

export function nodeColorCss(node: GraphNode, mode: ColorMode): string {
  if (mode === 'community' && node.community_id != null) return communityColorCss(node.community_id);
  return hexCss(nodeColorHex(node, mode));
}

// nodeSizeScore: the value that drives node prominence (3D mesh scale,
// 2D radius). usage_score is the intended signal, but on real indexes
// it is frequently 0 for every node — which collapsed all nodes to the
// minimum size and made the per-type shapes indistinguishable. Fall
// back to graph degree so hubs still stand out.
export function nodeSizeScore(n: GraphNode): number {
  const usage = n.usage_score ?? 0;
  if (usage > 0) return usage;
  return ((n.in_degree ?? 0) + (n.out_degree ?? 0)) / 8;
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
  // Types the 2D drawShape table already knew but 3D silently dropped
  // to the default small sphere. Mutex mattered most in practice: it
  // was the single most common type in the target repo's boot view
  // (82/200 top-pagerank nodes), so most of the canvas rendered as
  // identical fallback spheres.
  Type: 'cube', Mutex: 'lockBox', Endpoint: 'starburst',
  Commit: 'star', Hunk: 'cubeTiny',
};

const GEOM: Record<string, THREE.BufferGeometry> = {};
function geom(kind: string): THREE.BufferGeometry {
  if (GEOM[kind]) return GEOM[kind];
  let g: THREE.BufferGeometry;
  switch (kind) {
    // P6 LOD: segment counts trimmed toward 3d-force-graph's defaults
    // (nodeResolution=8 → low-poly spheres). At 5K+ meshes the vertex
    // count is a real vertex-shader cost; the visual difference at
    // typical camera distance is nil.
    case 'sphereLg':     g = new THREE.SphereGeometry(8, 12, 8); break;
    case 'sphereSm':     g = new THREE.SphereGeometry(2, 8, 6); break;
    case 'hex':          g = new THREE.CylinderGeometry(5, 5, 8, 6); break;
    case 'cube':         g = new THREE.BoxGeometry(5, 5, 5); break;
    case 'cubeSm':       g = new THREE.BoxGeometry(3, 3, 3); break;
    case 'cubeFlat':     g = new THREE.BoxGeometry(4, 1, 4); break;
    case 'cubeFlatSm':   g = new THREE.BoxGeometry(2.5, 0.7, 2.5); break;
    case 'cubeTiny':     g = new THREE.BoxGeometry(1.5, 1.5, 1.5); break;
    case 'torus':        g = new THREE.TorusGeometry(4, 1, 6, 12); break;
    case 'cylinder':     g = new THREE.CylinderGeometry(4, 4, 7); break;
    case 'diamond':      g = new THREE.OctahedronGeometry(4); break;
    case 'pyramid':      g = new THREE.ConeGeometry(4, 6, 4); break;
    case 'star':         g = new THREE.OctahedronGeometry(6, 1); break;
    case 'donut':        g = new THREE.TorusGeometry(4, 2, 6, 12); break;
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
    case 'lockBox':      g = new THREE.BoxGeometry(4.5, 3.2, 4.5); break;
    case 'plane':        g = new THREE.PlaneGeometry(4, 4); break;
    default:             g = new THREE.SphereGeometry(3, 8, 6); break;
  }
  GEOM[kind] = g;
  return g;
}

// P1: shared-material cache. The previous implementation allocated a
// fresh MeshStandardMaterial (PBR — the most expensive stock material)
// per node, which meant N materials + N uniform uploads + zero batching
// at 5K+ nodes. Raw 3d-force-graph's default is a *cached* Lambert
// material keyed by color/opacity — we mirror that exactly. Opacity
// values come from small discrete sets (ALPHA_BY_CONF × FOCUS_OPACITY ×
// dim 0.2), so the cache stays bounded (~palette × ~8 opacity tiers)
// regardless of node count.
//
// IMPORTANT: because materials are shared, callers must never mutate a
// mesh's material in place (opacity/color). To change appearance, swap
// the mesh's material reference for another cache entry via
// nodeMaterial(colorHex, opacity) — see GraphCanvas's focus-halo effect.
const MAT = new Map<string, THREE.MeshLambertMaterial>();
export function nodeMaterial(colorHex: number, opacity: number): THREE.MeshLambertMaterial {
  // Quantise to 2 decimals so float jitter can't grow the cache.
  const op = Math.round(opacity * 100) / 100;
  const key = `${colorHex}|${op}`;
  let m = MAT.get(key);
  if (!m) {
    m = new THREE.MeshLambertMaterial({
      color: colorHex,
      transparent: op < 1,
      opacity: op,
    });
    MAT.set(key, m);
  }
  return m;
}

export function nodeMesh(n: GraphNode, mode: ColorMode): THREE.Mesh {
  const kind = PRIMITIVE[n.type ?? ''] || 'sphereSm';
  const g = geom(kind);
  const mat = nodeMaterial(nodeColorHex(n, mode), ALPHA_BY_CONF[n.confidence ?? ''] ?? 1.0);
  const mesh = new THREE.Mesh(g, mat);
  const scale = 0.5 + Math.log10(nodeSizeScore(n) + 1) * 0.6;
  const clamped = Math.max(0.5, Math.min(3.5, scale));
  mesh.scale.setScalar(clamped);
  // baseScale lets animation code (BFS ripple) multiply and then restore
  // the usage-derived size. The previous ripple reset to setScalar(1),
  // silently flattening every node to uniform size after the first pulse.
  mesh.userData.baseScale = clamped;
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
