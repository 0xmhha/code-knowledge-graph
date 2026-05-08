'use client';

import { forwardRef, useEffect, useImperativeHandle, useMemo, useRef } from 'react';
import dynamic from 'next/dynamic';
import { useStore } from '@/store/store';
import { useShallow } from 'zustand/react/shallow';
import type { GraphEdge, GraphNode, NodeId, ViewMode } from '@/types';
import {
  ALPHA_BY_CONF, nodeColorCss, nodeColorHex, nodeMesh,
} from '@/lib/encoding';
import { EDGE_STYLE } from '@/lib/edges';

const ForceGraph2D = dynamic(() => import('react-force-graph-2d'), { ssr: false });
const ForceGraph3D = dynamic(() => import('react-force-graph-3d'), { ssr: false });

const FOCUS_OPACITY = [1.0, 0.92, 0.55, 0.18];
const FOCUS_LINK_BRIGHTNESS = [1.0, 1.0, 0.55, 0.10];

function focusOpacity(id: NodeId, focusDistance: Map<NodeId, number>): number {
  if (focusDistance.size === 0) return 1.0;
  const d = focusDistance.get(id);
  if (d === undefined) return FOCUS_OPACITY[FOCUS_OPACITY.length - 1];
  return FOCUS_OPACITY[Math.min(d, FOCUS_OPACITY.length - 1)];
}

function edgeFocusBrightness(e: GraphEdge, focusDistance: Map<NodeId, number>): number {
  if (focusDistance.size === 0) return 1.0;
  const a = focusDistance.get(e.src);
  const b = focusDistance.get(e.dst);
  if (a === undefined || b === undefined) return FOCUS_LINK_BRIGHTNESS[3];
  return FOCUS_LINK_BRIGHTNESS[Math.min(Math.max(a, b), FOCUS_LINK_BRIGHTNESS.length - 1)];
}

function hexAtBrightness(hex: number, b: number): string {
  const r = ((hex >> 16) & 0xff) * b;
  const g = ((hex >> 8) & 0xff) * b;
  const bl = (hex & 0xff) * b;
  return `rgb(${r | 0},${g | 0},${bl | 0})`;
}

interface Props {
  onNodeClick: (id: NodeId) => void;
}

export interface GraphCanvasHandle {
  zoomIn: () => void;
  zoomOut: () => void;
  zoomReset: () => void;
  // centerOnNode pans the camera so the given node is on-screen and
  // recognisable. Called by App.onListPick so picking a list item visibly
  // moves the canvas to the chosen node — without this, the focus halo
  // updates but a node off-screen stays off-screen.
  centerOnNode: (id: NodeId) => void;
}

// 2D zoom uses fg.zoom(factor, durationMs).
// 3D zoom adjusts cameraPosition distance toward/away from origin.
const ZOOM_FACTOR_IN = 1.4;
const ZOOM_FACTOR_OUT = 1 / 1.4;
const ZOOM_DURATION_MS = 200;

const GraphCanvas = forwardRef<GraphCanvasHandle, Props>(function GraphCanvas(
  { onNodeClick },
  ref,
) {
  const fgRef = useRef<unknown>(null);
  const viewModeRef = useRef<ViewMode>('3d');

  // Subscribe with shallow check so we re-render only when these change.
  const { viewMode, colorMode, fontSize, edgeTypeWhitelist, dimmedCommunities, isolatedCommunity } =
    useStore(useShallow(s => ({
      viewMode: s.viewMode,
      colorMode: s.colorMode,
      fontSize: s.fontSize,
      edgeTypeWhitelist: s.edgeTypeWhitelist,
      dimmedCommunities: s.dimmedCommunities,
      isolatedCommunity: s.isolatedCommunity,
    })));

  // Keep a ref of current viewMode so imperative handle can read it without
  // re-creating the handle on every viewMode change.
  viewModeRef.current = viewMode;

  // Live ref to the latest graphData so the centerOnNode imperative
  // method (whose closure is captured once via useImperativeHandle with
  // an empty deps array) can read the current node positions instead
  // of a snapshot from first mount.
  const graphDataRef = useRef<{ nodes: GraphNode[]; links: GraphEdge[] } | null>(null);

  useImperativeHandle(ref, () => {
    // Local typed shim: react-force-graph ships without TS types, so we
    // declare the surface we actually call. Cast the imperative ref once
    // per method instead of re-asserting at every call site.
    type Vec3 = { x: number; y: number; z: number };
    interface FGShim {
      zoom?: (factor: number, duration?: number) => void;
      zoomToFit?: (duration?: number) => void;
      cameraPosition?: (pos?: Vec3, lookAt?: Vec3, duration?: number) => Vec3 | void;
      centerAt?: (x?: number, y?: number, durationMs?: number) => void;
    }
    const fg = (): FGShim | null => (fgRef.current as FGShim | null) ?? null;
    const RESET_3D: Vec3 = { x: 0, y: 0, z: 600 };
    const ORIGIN: Vec3 = { x: 0, y: 0, z: 0 };
    const CENTER_DURATION_MS = 600;

    const zoom3D = (factor: number) => {
      const f = fg();
      if (!f?.cameraPosition) return;
      const cur = f.cameraPosition() as Vec3 | undefined;
      if (!cur) return;
      const dist = Math.sqrt(cur.x ** 2 + cur.y ** 2 + cur.z ** 2);
      // Camera at origin would divide by zero; recover via reset distance
      // along z. Rare in normal flow but reachable after zooming all the
      // way in.
      if (dist === 0) {
        f.cameraPosition({ x: 0, y: 0, z: RESET_3D.z / factor }, ORIGIN, ZOOM_DURATION_MS);
        return;
      }
      const newDist = dist / factor;
      f.cameraPosition(
        { x: (cur.x / dist) * newDist, y: (cur.y / dist) * newDist, z: (cur.z / dist) * newDist },
        ORIGIN,
        ZOOM_DURATION_MS,
      );
    };

    return {
      zoomIn() {
        if (viewModeRef.current === '2d') fg()?.zoom?.(ZOOM_FACTOR_IN, ZOOM_DURATION_MS);
        else zoom3D(ZOOM_FACTOR_IN);
      },
      zoomOut() {
        if (viewModeRef.current === '2d') fg()?.zoom?.(ZOOM_FACTOR_OUT, ZOOM_DURATION_MS);
        else zoom3D(ZOOM_FACTOR_OUT);
      },
      zoomReset() {
        if (viewModeRef.current === '2d') {
          fg()?.zoomToFit?.(ZOOM_DURATION_MS);
        } else {
          // Pass an explicit lookAt so the camera both translates AND aims
          // at the origin — passing undefined would preserve the prior
          // look-at and produce a tilted "reset".
          fg()?.cameraPosition?.(RESET_3D, ORIGIN, ZOOM_DURATION_MS);
        }
      },
      centerOnNode(id: NodeId) {
        // Look up the node's current simulation position. force-graph
        // mutates x/y/z on the node objects in place; we read whatever
        // values are present at click time.
        const data = graphDataRef.current;
        if (!data) return;
        const n = data.nodes.find(x => x.id === id);
        if (!n) return;
        const f = fg();
        if (!f) return;
        if (viewModeRef.current === '2d') {
          // 2D: pan the canvas to the node's (x, y).
          if (typeof n.x === 'number' && typeof n.y === 'number') {
            f.centerAt?.(n.x, n.y, CENTER_DURATION_MS);
          }
        } else {
          // 3D: keep the camera's current distance, just move it so it
          // looks at the node. Easiest robust trick: place the camera
          // along the +z axis offset from the node by the current
          // viewing distance.
          const nz = (n as GraphNode & { z?: number }).z ?? 0;
          if (typeof n.x !== 'number' || typeof n.y !== 'number') return;
          const cur = f.cameraPosition?.() as Vec3 | undefined;
          const dist = cur
            ? Math.sqrt(cur.x ** 2 + cur.y ** 2 + cur.z ** 2) || RESET_3D.z
            : RESET_3D.z;
          f.cameraPosition?.(
            { x: n.x, y: n.y, z: nz + dist },
            { x: n.x, y: n.y, z: nz },
            CENTER_DURATION_MS,
          );
        }
      },
    };
  }, []);

  // graphData re-derives on visibleIds / edges / nodes change. We split
  // into individual selectors and useMemo because returning a fresh object
  // literal from a single selector defeats zustand's Object.is bail-out and
  // re-fires every store update — which then cascades into a render loop
  // through ForceGraph3D's simulation callbacks (React error #185).
  const visibleIds = useStore(s => s.visibleIds);
  const allNodes = useStore(s => s.nodes);
  const edgesBySrc = useStore(s => s.edgesBySrc);
  const graphData = useMemo(() => {
    const nodes: GraphNode[] = [];
    for (const id of visibleIds) {
      const n = allNodes.get(id);
      if (n) nodes.push(n);
    }
    const links: GraphEdge[] = [];
    for (const id of visibleIds) {
      const outs = edgesBySrc.get(id);
      if (!outs) continue;
      for (const e of outs) if (visibleIds.has(e.dst)) links.push(e);
    }
    return { nodes, links };
  }, [visibleIds, allNodes, edgesBySrc]);

  // Mirror graphData into a ref so the centerOnNode imperative method
  // (captured once in useImperativeHandle's empty-deps closure) reads
  // the live node positions instead of a snapshot from first mount.
  graphDataRef.current = graphData;

  const focusDistance = useStore(s => s.focusDistance);

  // Mesh index for 3D mode — keeps mesh references alive so we can mutate
  // material.opacity directly on focus changes without rebuilding the scene.
  const meshIndex = useMemo(() => new Map<NodeId, import('three').Mesh>(), [viewMode]);

  // Reapply focus halo to existing meshes whenever focusDistance changes.
  useEffect(() => {
    if (viewMode !== '3d') return;
    const focusActive = focusDistance.size > 0;
    const nodes = useStore.getState().nodes;
    for (const [id, mesh] of meshIndex) {
      const n = nodes.get(id);
      const conf = n?.confidence ?? '';
      const op = focusActive
        ? focusOpacity(id, focusDistance) * (ALPHA_BY_CONF[conf] ?? 1)
        : (ALPHA_BY_CONF[conf] ?? 1);
      const m = mesh.material as import('three').MeshStandardMaterial;
      m.opacity = op;
      m.transparent = op < 1;
      m.needsUpdate = true;
    }
  }, [focusDistance, viewMode, meshIndex]);

  const linkVisibility = (link: GraphEdge): boolean => {
    if (EDGE_STYLE[link.type]?.hidden) return false;
    return edgeTypeWhitelist.has(link.type);
  };

  const nodeVisibility = (node: GraphNode): boolean => {
    if (isolatedCommunity != null && node.community_id !== isolatedCommunity) return false;
    // Community legend toggle = visibility off, not just dim. Earlier
    // builds dropped the node opacity to 0.18 which read as "still
    // there but quiet"; user feedback was that off should mean fully
    // hidden until toggled back on. dimmedCommunities is the legend's
    // toggle-off set. brightness/dim role is now redundant for nodes
    // (still applies to edges connected to a hidden node, which won't
    // render anyway because both endpoints fail nodeVisibility).
    if (node.community_id != null && dimmedCommunities.has(node.community_id)) return false;
    return useStore.getState().visibleIds.has(node.id);
  };

  const linkColor = (e: GraphEdge): string => {
    const base = EDGE_STYLE[e.type]?.color ?? 0x999999;
    return hexAtBrightness(base, edgeFocusBrightness(e, focusDistance));
  };

  const linkWidth = (e: GraphEdge): number => {
    const base = EDGE_STYLE[e.type]?.width ?? 1;
    const brightness = edgeFocusBrightness(e, focusDistance);
    if (brightness >= 0.9) return base + 0.5;
    if (brightness >= 0.5) return Math.max(0.7, base);
    return 0.25;
  };

  const drawNode2D = (node: GraphNode, ctx: CanvasRenderingContext2D, globalScale: number) => {
    const r = 3 + Math.log10((node.usage_score ?? 0) + 1) * 1.5;
    const dimmed = node.community_id != null && dimmedCommunities.has(node.community_id);
    const baseAlpha = focusOpacity(node.id, focusDistance) * (ALPHA_BY_CONF[node.confidence ?? ''] ?? 1);
    const op = dimmed ? 0.18 : baseAlpha;
    ctx.globalAlpha = op;
    ctx.fillStyle = nodeColorCss(node, colorMode);
    ctx.beginPath();
    ctx.arc(node.x ?? 0, node.y ?? 0, Math.max(2, r), 0, 2 * Math.PI);
    ctx.fill();
    const dist = focusDistance.get(node.id);
    if (dist === 0) {
      // Anchor / selected node ring: cyan accent + double pass for
      // visibility against any node fill colour. Earlier 2px white was
      // washed out next to bright community palettes — users couldn't
      // tell which node they'd just clicked. Cyan #00ddff is distinct
      // from every entry in EDGE_STYLE / community palette.
      ctx.globalAlpha = 1;
      const ringR = Math.max(2, r) + 4 / globalScale;
      ctx.strokeStyle = '#00ddff';
      ctx.lineWidth = 3 / globalScale;
      ctx.beginPath();
      ctx.arc(node.x ?? 0, node.y ?? 0, ringR, 0, 2 * Math.PI);
      ctx.stroke();
      // Inner thin white ring boosts contrast on dark backgrounds where
      // cyan alone bleeds into nearby star-cluster glow.
      ctx.strokeStyle = '#ffffff';
      ctx.lineWidth = 1 / globalScale;
      ctx.beginPath();
      ctx.arc(node.x ?? 0, node.y ?? 0, ringR - 2 / globalScale, 0, 2 * Math.PI);
      ctx.stroke();
    } else if (dist === 1) {
      // 1-hop neighbour: subtle accent ring so the user can read the
      // immediate neighbourhood at a glance. 2-hop+ stays unringed so
      // the focus stays on dist=0 and dist=1.
      ctx.globalAlpha = 0.7;
      ctx.strokeStyle = '#7ab8ff';
      ctx.lineWidth = 1.2 / globalScale;
      ctx.beginPath();
      ctx.arc(node.x ?? 0, node.y ?? 0, Math.max(2, r) + 2 / globalScale, 0, 2 * Math.PI);
      ctx.stroke();
    }
    ctx.globalAlpha = 1;
    const inFocusBall = dist !== undefined;
    const deg = (node.in_degree ?? 0) + (node.out_degree ?? 0);
    if (inFocusBall || (globalScale > 1.5 && deg > 5)) {
      const fontPx = Math.max(8, (10 * fontSize) / globalScale);
      ctx.font = `${fontPx}px ui-monospace, monospace`;
      ctx.fillStyle = inFocusBall ? '#e6e7e9' : '#9aa';
      ctx.textAlign = 'center';
      ctx.fillText(node.name ?? '', node.x ?? 0, (node.y ?? 0) - r - 2);
    }
  };

  const pointerArea2D = (node: GraphNode, color: string, ctx: CanvasRenderingContext2D) => {
    const r = Math.max(4, 3 + Math.log10((node.usage_score ?? 0) + 1) * 1.5);
    ctx.fillStyle = color;
    ctx.beginPath();
    ctx.arc(node.x ?? 0, node.y ?? 0, r + 3, 0, 2 * Math.PI);
    ctx.fill();
  };

  const tooltip = (node: GraphNode): string => buildTooltip(node, focusDistance, fontSize);

  const onClick = (n: GraphNode | { id?: NodeId }) => {
    const id = (n as GraphNode).id;
    if (id) onNodeClick(id);
  };

  // Layout key forces a hard remount when viewMode flips so meshes don't leak.
  const key = `fg-${viewMode}-${colorMode}`;

  if (viewMode === '2d') {
    return (
      <ForceGraph2D
        key={key}
        ref={fgRef as never}
        graphData={graphData}
        linkSource="src"
        linkTarget="dst"
        nodeLabel={tooltip as never}
        nodeVisibility={nodeVisibility as never}
        linkVisibility={linkVisibility as never}
        linkColor={linkColor as never}
        linkWidth={linkWidth as never}
        linkDirectionalArrowLength={3}
        linkDirectionalArrowRelPos={0.95}
        nodeCanvasObject={drawNode2D as never}
        nodePointerAreaPaint={pointerArea2D as never}
        backgroundColor="#0d0e10"
        cooldownTicks={80}
        cooldownTime={2500}
        onNodeClick={onClick as never}
      />
    );
  }

  return (
    <ForceGraph3D
      key={key}
      ref={fgRef as never}
      graphData={graphData}
      linkSource="src"
      linkTarget="dst"
      nodeLabel={tooltip as never}
      nodeVisibility={nodeVisibility as never}
      linkVisibility={linkVisibility as never}
      linkColor={linkColor as never}
      linkWidth={linkWidth as never}
      linkDirectionalArrowLength={3}
      linkDirectionalArrowRelPos={0.95}
      cooldownTicks={80}
      cooldownTime={2500}
      onNodeClick={onClick as never}
      nodeThreeObject={((node: GraphNode) => {
        const m = nodeMesh(node, colorMode);
        meshIndex.set(node.id, m);
        // Color override for community mode happens inside nodeMesh; here we
        // also apply the immediate focus opacity so first-frame is correct.
        const focusActive = focusDistance.size > 0;
        const baseAlpha = ALPHA_BY_CONF[node.confidence ?? ''] ?? 1;
        const op = focusActive ? focusOpacity(node.id, focusDistance) * baseAlpha : baseAlpha;
        const mat = m.material as import('three').MeshStandardMaterial;
        mat.opacity = op;
        mat.transparent = op < 1;
        // Community color is already baked in via nodeMesh; this assignment
        // is a no-op when colorMode='lang' but lets us re-skin without
        // remounting when the toggle flips.
        mat.color.setHex(nodeColorHex(node, colorMode));
        return m;
      }) as never}
    />
  );
});

export default GraphCanvas;

function buildTooltip(node: GraphNode, focusDistance: Map<NodeId, number>, fs: number): string {
  const t = node.type ?? '?';
  const q = node.qualified_name ?? node.name ?? node.id;
  const f = node.file_path ? `${node.file_path}:${node.start_line ?? 0}` : '—';
  const lang = node.language ?? '';
  const conf = node.confidence ?? '';
  const inDeg = node.in_degree ?? 0;
  const outDeg = node.out_degree ?? 0;
  const usage = (node.usage_score ?? 0).toFixed(2);
  const pr = (node.pagerank ?? 0).toExponential(2);
  const sig = node.signature
    ? `<div style="color:#9ad;margin-top:4px;font-style:italic;max-width:380px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">${escape(node.signature)}</div>` : '';
  const dist = focusDistance.get(node.id);
  const distLabel = dist === 0 ? '· FOCUS' : dist === 1 ? '· direct' : dist === 2 ? '· 2-hop' : '';
  const community = node.community_id != null
    ? `<div style="color:#888;">community: <span style="color:#aaa">${node.community_id}${node.topic_label ? ` · ${escape(node.topic_label)}` : ''}</span></div>` : '';
  const f1 = (11 * fs).toFixed(1);
  const f2 = (12 * fs).toFixed(1);
  const f3 = (10 * fs).toFixed(1);
  return `<div style="pointer-events:none;font-family:ui-monospace,monospace;font-size:${f1}px;line-height:1.4;background:rgba(15,17,20,.96);color:#e6e7e9;padding:8px 10px;border:1px solid #2a2c30;border-radius:4px;max-width:${420 * fs}px;">
<div style="font-size:${f2}px;margin-bottom:4px;"><strong style="color:#7ab8ff;">${escape(t)}</strong> <span style="color:#cfd0d3;">${escape(q)}</span> <span style="color:#7ab8ff">${distLabel}</span></div>
<div style="color:#bbb;">📄 ${escape(f)}</div>${sig}
<div style="color:#888;margin-top:5px;">lang: <span style="color:#aaa">${escape(lang)}</span> · conf: <span style="color:#aaa">${escape(conf)}</span></div>
<div style="color:#888;">in-edges: <span style="color:#aaa">${inDeg}</span> · out-edges: <span style="color:#aaa">${outDeg}</span></div>
<div style="color:#888;">usage: <span style="color:#aaa">${usage}</span> · pagerank: <span style="color:#aaa">${pr}</span></div>
${community}
<div style="color:#666;margin-top:6px;font-size:${f3}px;">click → set anchor · ⇲/⇱ to navigate depth</div>
</div>`;
}

function escape(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

