'use client';

import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef } from 'react';
import dynamic from 'next/dynamic';
import { useStore } from '@/store/store';
import { useShallow } from 'zustand/react/shallow';
import type { GraphEdge, GraphNode, NodeId, ViewMode } from '@/types';
import {
  ALPHA_BY_CONF, SEARCH_HIGHLIGHT_CSS, SEARCH_HIGHLIGHT_HEX,
  nodeColorCss, nodeColorHex, nodeMaterial, nodeMesh, nodeSizeScore,
} from '@/lib/encoding';
import { EDGE_STYLE, edgeToGroup } from '@/lib/edges';

const ForceGraph2D = dynamic(() => import('react-force-graph-2d'), { ssr: false });
const ForceGraph3D = dynamic(() => import('react-force-graph-3d'), { ssr: false });

// Focus model (user feedback, 2026-07-02): focus is VISIBILITY, not
// dimming. The previous FOCUS_OPACITY [1, .92, .55, .18] node tiers and
// FOCUS_LINK_BRIGHTNESS [1, 1, .55, .10] link tiers repainted everything
// outside the 2-hop ball toward black on the dark canvas — colours must
// stay exactly as the default state renders them. Off-focus elements are
// now hidden via nodeVisibility/linkVisibility instead (which is also
// cheaper: hidden objects skip draw entirely).

function hexAtBrightness(hex: number, b: number): string {
  const r = ((hex >> 16) & 0xff) * b;
  const g = ((hex >> 8) & 0xff) * b;
  const bl = (hex & 0xff) * b;
  return `rgb(${r | 0},${g | 0},${bl | 0})`;
}

interface Props {
  onNodeClick: (id: NodeId) => void;
  // width/height are REQUIRED for responsive sizing. The vanilla
  // `force-graph` Kapsule defaults width/height to
  // `window.innerWidth`/`window.innerHeight` at construction time
  // (force-graph.mjs ~line 1020) and writes those values directly to
  // canvas.style.width/height, overriding any parent CSS sizing. There
  // is no internal ResizeObserver — the only way to keep the canvas in
  // sync with its grid cell is to feed dimensions in as props and
  // update them via App-side ResizeObserver. Omitting them produced a
  // canvas frozen at full-viewport size that ignored browser resize
  // and overflowed the `canvas-host` column.
  width: number;
  height: number;
  // H: onEngineStop fires when the d3 force simulation settles. App.tsx
  // uses that signal to flip viewerReady → true and fade the canvas in;
  // the user never sees the camera mid-zoom.
  onEngineStop?: () => void;
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
  { onNodeClick, onEngineStop, width, height },
  ref,
) {
  const fgRef = useRef<unknown>(null);
  const viewModeRef = useRef<ViewMode>('3d');

  // Subscribe with shallow check so we re-render only when these change.
  const {
    viewMode, colorMode, fontSize,
    edgeTypeWhitelist, nodeTypeWhitelist,
    dimmedCommunities, isolatedCommunity, dimmedNodes,
  } = useStore(useShallow(s => ({
    viewMode: s.viewMode,
    colorMode: s.colorMode,
    fontSize: s.fontSize,
    edgeTypeWhitelist: s.edgeTypeWhitelist,
    nodeTypeWhitelist: s.nodeTypeWhitelist,
    dimmedCommunities: s.dimmedCommunities,
    isolatedCommunity: s.isolatedCommunity,
    dimmedNodes: s.dimmedNodes,
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
    interface OrbitControlsShim {
      // OrbitControls.target is a Three.Vector3 we mutate to lock the
      // orbit pivot onto the focused node (H1 — D1 decision). update()
      // re-syncs the camera after we move target so the next user drag
      // orbits around the node, not the world origin. x/y/z are read by
      // zoom3D/centerOnNode to preserve the current view axis.
      target: { set: (x: number, y: number, z: number) => void; x?: number; y?: number; z?: number };
      update: () => void;
    }
    interface FGShim {
      zoom?: (factor: number, duration?: number) => void;
      zoomToFit?: (duration?: number) => void;
      cameraPosition?: (pos?: Vec3, lookAt?: Vec3, duration?: number) => Vec3 | void;
      centerAt?: (x?: number, y?: number, durationMs?: number) => void;
      // controls() exists only on ForceGraph3D; absent on 2D. Defensive
      // optional + runtime check so the same shim is safe in both modes.
      controls?: () => OrbitControlsShim | undefined;
    }
    const fg = (): FGShim | null => (fgRef.current as FGShim | null) ?? null;
    const RESET_3D: Vec3 = { x: 0, y: 0, z: 600 };
    const ORIGIN: Vec3 = { x: 0, y: 0, z: 0 };
    // CENTER_DURATION_MS: 600ms felt sluggish in the mintscan A/B —
    // mintscan's camera settles in roughly half a tick. Drop to 400ms
    // so a click → focus transition feels reactive without being jarring.
    const CENTER_DURATION_MS = 400;
    // DOLLY_FACTOR: shrink current camera distance to 60% on focus so
    // the selected node fills more frame area (M1 — D5 decision). 1.0
    // would be the original "keep distance" behaviour; 0.4 was tested
    // visually but lost too much surrounding context.
    const DOLLY_FACTOR = 0.6;

    // NOTE (2026-07-02 revert): a "zoom along camera→orbit-target axis"
    // variant was tried here and rolled back. Deriving distances from
    // the live controls.target made successive pick+zoom operations
    // COMPOUND (each dolly/zoom shrank the camera↔target distance, and
    // the next operation started from the already-shrunk value), until
    // the orbit radius collapsed to ~0 — at which point dragging spun
    // the camera in place (first-person vertigo) staring at empty
    // space. The origin-based math below re-inflates the distance on
    // every call, which is what keeps drag = "orbit around the data".
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
        // F: both modes prefer zoomToFit — frames the actual node
        // distribution rather than a hard-coded camera distance. The
        // 3D branch used to teleport to (0,0,600) which was right for
        // some graphs and very wrong for others; zoomToFit asks the
        // library to compute the framing from the live bounding box.
        fg()?.zoomToFit?.(ZOOM_DURATION_MS);
        if (viewModeRef.current === '3d') {
          // Release the focus-lock so subsequent user drags orbit
          // around the world origin again (H1 D1).
          const ctrls = fg()?.controls?.();
          if (ctrls) { ctrls.target.set(0, 0, 0); ctrls.update(); }
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
          // 3D: dolly-in on the node — shrink the viewing distance to
          // DOLLY_FACTOR of current so the selected node grows in frame
          // (mintscan-style cinematic focus). Easiest robust trick: place
          // the camera along the +z axis offset from the node by the new
          // (shorter) viewing distance.
          //
          // NOTE (2026-07-02 revert): a direction-preserving variant
          // (keep the camera→target unit vector, slide it to the node)
          // was tried and rolled back — deriving the dolly distance
          // from |camera − target| made repeated picks compound the
          // 0.6× shrink until the orbit radius collapsed and drag spun
          // the camera in place. The origin-based distance below
          // re-inflates on every pick, keeping drag anchored on the
          // node/data.
          const nz = (n as GraphNode & { z?: number }).z ?? 0;
          if (typeof n.x !== 'number' || typeof n.y !== 'number') return;
          const cur = f.cameraPosition?.() as Vec3 | undefined;
          const dist = cur
            ? Math.sqrt(cur.x ** 2 + cur.y ** 2 + cur.z ** 2) || RESET_3D.z
            : RESET_3D.z;
          const newDist = dist * DOLLY_FACTOR;
          f.cameraPosition?.(
            { x: n.x, y: n.y, z: nz + newDist },
            { x: n.x, y: n.y, z: nz },
            CENTER_DURATION_MS,
          );
          // Focus-locked orbit (H1): point OrbitControls.target at the
          // node so the user's subsequent drag orbits around the node
          // rather than the world origin. Persists until next
          // centerOnNode (new target) or zoomReset (back to origin).
          const ctrls = f.controls?.();
          if (ctrls) { ctrls.target.set(n.x, n.y, nz); ctrls.update(); }
        }
      },
    };
  }, []);

  // P2 (perf): graphData identity is the lever that decides whether
  // react-force-graph re-ingests the dataset and re-heats the d3
  // simulation. The previous version derived {nodes, links} from
  // visibleIds, so EVERY visibility commit (search union, type filter,
  // community toggle, list-pick) produced a fresh object → full
  // re-ingest → layout restart. Raw 3d-force-graph apps keep graphData
  // stable and gate what's drawn via the nodeVisibility/linkVisibility
  // accessors — we now do the same:
  //
  //   - fullData: ALL cached nodes/edges. Rebuilt only when the data
  //     actually grows (loadNodes / addEdges commits) — rare after
  //     boot. Visibility toggles no longer touch it, so the simulation
  //     keeps its settled positions (no restart, no visual jump).
  //   - anchoredData: the BFS-scoped subset, used ONLY while an anchor
  //     drives dagMode. DAG levels must be computed on the focused
  //     subgraph (a global DAG over the full cache costs more AND lays
  //     the focused flow out differently), so this path retains the
  //     old rebuild-per-navigation semantics — the one interaction
  //     where a re-layout IS the product behaviour.
  //
  // Selectors stay split (not one object selector) because a fresh
  // object literal from a single selector defeats zustand's Object.is
  // bail-out and re-fires every store update — which cascades into a
  // render loop through ForceGraph3D's simulation callbacks (React
  // error #185).
  const visibleIds = useStore(s => s.visibleIds);
  const allNodes = useStore(s => s.nodes);
  const edgesBySrc = useStore(s => s.edgesBySrc);
  const anchorId = useStore(s => s.anchorId);
  const fullData = useMemo(() => {
    const nodes = [...allNodes.values()];
    const links: GraphEdge[] = [];
    for (const outs of edgesBySrc.values()) {
      // Edges fetched for a node can reference endpoints that were never
      // loaded (excluded types, beyond nodeLimit). force-graph throws on
      // a link whose endpoint id has no node record — gate on the cache.
      for (const e of outs) {
        if (allNodes.has(e.src) && allNodes.has(e.dst)) links.push(e);
      }
    }
    return { nodes, links };
  }, [allNodes, edgesBySrc]);
  const anchoredData = useMemo(() => {
    if (!anchorId) return null;
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
  }, [anchorId, visibleIds, allNodes, edgesBySrc]);
  // 계층(흐름) 모드 — 전역 DAG. dag 레벨 계산이 '보이는·허용된' 엣지에서만
  // 이뤄지도록 필터된 데이터셋을 별도로 만든다: fullData 로 dag 를 돌리면
  // 숨긴 엣지 타입(contains/defines 등)까지 레벨 배정에 끼어들어 호출
  // 흐름이 왜곡된다. force 모드의 P2 안정성(재시작 회피)은 그대로 —
  // 이 데이터셋은 layoutMode==='dag' 일 때만 존재한다.
  const layoutMode = useStore(s => s.layoutMode);
  const edgeTypeWhitelistForDag = useStore(s => s.edgeTypeWhitelist);
  const dagData = useMemo(() => {
    if (layoutMode !== 'dag' || anchorId) return null;
    const nodes: GraphNode[] = [];
    for (const id of visibleIds) {
      const n = allNodes.get(id);
      if (n) nodes.push(n);
    }
    const links: GraphEdge[] = [];
    for (const id of visibleIds) {
      const outs = edgesBySrc.get(id);
      if (!outs) continue;
      for (const e of outs) {
        if (!visibleIds.has(e.dst)) continue;
        if (EDGE_STYLE[e.type]?.hidden) continue;
        if (!edgeTypeWhitelistForDag.has(e.type)) continue;
        links.push(e);
      }
    }
    return { nodes, links };
  }, [layoutMode, anchorId, visibleIds, allNodes, edgesBySrc, edgeTypeWhitelistForDag]);

  const graphData = anchoredData ?? dagData ?? fullData;

  // Mirror graphData into refs so imperative code (centerOnNode, the
  // orbit-follow loop) reads live positions without being re-created.
  // nodeById replaces the previous O(N) Array.find-per-frame lookup in
  // the follow loop with O(1).
  graphDataRef.current = graphData;
  const nodeByIdRef = useRef<Map<NodeId, GraphNode>>(new Map());
  nodeByIdRef.current = useMemo(() => {
    const m = new Map<NodeId, GraphNode>();
    for (const n of graphData.nodes) m.set(n.id, n);
    return m;
  }, [graphData]);

  const focusDistance = useStore(s => s.focusDistance);

  // Mesh index for 3D mode — keeps mesh references alive so we can mutate
  // material.opacity directly on focus changes without rebuilding the scene.
  // viewMode is intentionally in the deps so a 3D↔2D toggle drops stale mesh
  // references (the scene is fully rebuilt on toggle and any held mesh from
  // the previous mode would point at a disposed object).
  // eslint-disable-next-line react-hooks/exhaustive-deps -- viewMode is the reset trigger, not a read dependency
  const meshIndex = useMemo(() => new Map<NodeId, import('three').Mesh>(), [viewMode]);

  // R1.5-a fix + B2 + A fix: OrbitControls.target init + per-frame
  // follow of the selected node + bootstrap saveState so the 'change'
  // event listeners (registered by react-force-graph-3d) don't read .x
  // on uninitialised target0/position0/zoom0 vectors.
  //
  // - Init: controlType="orbit" instantiates OrbitControls but the
  //   library's dynamic boot doesn't always init controls.target before
  //   the first update(); Three's update() then reads .x on undefined
  //   and throws. We additionally call cameraPosition(...) explicitly
  //   to seed both the camera and OrbitControls.target with a known
  //   Vector3 (lookAt sync), then saveState() to clone target/position/
  //   zoom into target0/position0/zoom0 — which is what the dispatch
  //   listeners crash on when uninitialised.
  // - Follow (B2): force simulation moves the selected node every tick.
  //   If controls.target stays at the position we set at click time, the
  //   visible node drifts away from the orbit pivot — and user drag then
  //   rotates around a stale point that looks like "wherever the mouse
  //   first clicked" rather than the selected node.
  //
  // We solve both with a single rAF loop: read the selected node's live
  // position from graphDataRef and copy it into controls.target each
  // frame (epsilon-gated so we don't churn when nothing moved). No
  // manual update() call — react-force-graph-3d's own render loop
  // already calls controls.update() every frame and picks up the new
  // target on the next pass.
  useEffect(() => {
    if (viewMode !== '3d') return;
    type Vec3Like = {
      set: (x: number, y: number, z: number) => void;
      x?: number; y?: number; z?: number;
    };
    type ControlsLike = {
      target?: Vec3Like;
      update?: () => void;
      saveState?: () => void;
      // The _onPointerUp slot is internal Three.js OrbitControls API —
      // typed loosely because we monkey-patch it once below to swallow
      // a known library bug. disconnect/connect re-register listeners
      // against the (now wrapped) reference. Three r182+ made `connect`
      // require an explicit element argument; without it the base
      // Controls class just warns and returns, leaving listeners
      // detached — so we must read `domElement` from the instance
      // BEFORE disconnect and pass it back on connect.
      _onPointerUp?: (e: PointerEvent) => void;
      _onPointerUpPatched?: boolean;
      domElement?: HTMLElement | null;
      disconnect?: () => void;
      connect?: (element?: HTMLElement) => void;
    };
    type Vec3 = { x: number; y: number; z: number };
    type FGShim = {
      controls?: () => ControlsLike | undefined;
      cameraPosition?: (pos?: Vec3, lookAt?: Vec3, duration?: number) => Vec3 | void;
    };
    // A-fix bootstrap: poll until controls() is ready (dynamic import +
    // canvas mount can take a few frames), then seed cameraPosition with
    // an explicit lookAt = origin so OrbitControls.target gets a valid
    // Vector3. Follow with saveState() to clone {target,position,zoom}
    // into their *0 backups — the dispatch listeners read those, and
    // crash when they were created as `new Vector3()` but the source
    // target was undefined at init time.
    // bootstrap removed (2026-05-21): every attempt to seed
    // controls.target on mount — even just target.set+update — caused
    // ForceGraph-3D's own camera placement to silently fail to render
    // the graph (canvas stayed blank while data committed correctly).
    // The TypeError noise from uninitialised target0/position0 vectors
    // is then a known harmless side effect — they fire from the
    // library's dispatch listeners but do not interrupt rendering or
    // user interaction. Re-introducing init is fine only if we also
    // delay it until ForceGraph's own first frame is past (e.g. via
    // an onEngineStop callback), which is a separate piece of work.
    let rafId = 0;
    let running = false;
    let dragging = false;
    const onDown = () => { dragging = true; };
    const onUp = () => { dragging = false; };
    window.addEventListener('mousedown', onDown);
    window.addEventListener('mouseup', onUp);

    // One-shot defensive patch: 3d-force-graph's dragend handler
    // dispatches a synthetic `new PointerEvent('pointerup')` on the
    // document to release OrbitControls' drag state (see
    // 3d-force-graph.mjs ~line 471). The event has the default
    // pointerId=0, and if OrbitControls still has an OTHER pointerId
    // in `_pointers` when this fires, its `case 1:` branch reads
    // `_pointerPositions[pointerId]` which can be undefined and
    // crashes on `position.x`. We can't fix the library; we wrap the
    // handler so the throw is silently swallowed.
    //
    // P3: this used to piggyback on an always-on rAF loop (a per-frame
    // check for the whole canvas lifetime). A short poller lands the
    // patch once and stops — same idempotence via _onPointerUpPatched,
    // zero steady-state cost.
    const patchTimer = setInterval(() => {
      const fg = fgRef.current as FGShim | null;
      const ctrls = fg?.controls?.();
      if (!ctrls) return;
      if (!ctrls._onPointerUpPatched && ctrls._onPointerUp && ctrls.disconnect && ctrls.connect && ctrls.domElement) {
        const original = ctrls._onPointerUp;
        const el = ctrls.domElement;
        ctrls.disconnect();
        ctrls._onPointerUp = (e: PointerEvent) => {
          try { original.call(ctrls, e); } catch { /* OrbitControls _pointerPositions race */ }
        };
        ctrls.connect(el);
        ctrls._onPointerUpPatched = true;
      }
      if (ctrls._onPointerUpPatched) clearInterval(patchTimer);
    }, 100);

    // P3: the follow loop runs ONLY while a node is selected. Boot /
    // post-Home / post-clear states previously paid a 60fps wakeup
    // (getState + controls() + bail) for nothing; now the loop stops
    // itself when selection clears and is restarted by the store
    // subscription below on the next select.
    const tick = () => {
      const sel = useStore.getState().selectedId;
      if (!sel) { running = false; return; }
      rafId = requestAnimationFrame(tick);
      // Drag-in-progress: leave controls.target alone. Mutating target
      // every frame while OrbitControls is computing a delta from the
      // mouse stream cancels out the rotation (camera and target both
      // chase the same vector) — drag visibly does nothing.
      if (dragging) return;
      const fg = fgRef.current as FGShim | null;
      const ctrls = fg?.controls?.();
      const t = ctrls?.target;
      if (!t) return;
      // O(1) lookup via nodeByIdRef — the previous Array.find was O(N)
      // per frame (5K nodes × 60fps = 300K comparisons/s while selected).
      const n = nodeByIdRef.current.get(sel) as
        (GraphNode & { z?: number }) | undefined;
      if (!n || typeof n.x !== 'number' || typeof n.y !== 'number') return;
      const tx = n.x, ty = n.y, tz = n.z ?? 0;
      const ctx = t.x ?? 0, cty = t.y ?? 0, ctz = t.z ?? 0;
      // 0.5 unit epsilon: simulation jitter at rest is well below this,
      // active migration is well above. Avoids per-frame mutation when
      // nothing meaningfully changed.
      if (Math.abs(ctx - tx) > 0.5
       || Math.abs(cty - ty) > 0.5
       || Math.abs(ctz - tz) > 0.5) {
        t.set(tx, ty, tz);
        // Call update() so the camera recomputes its position around
        // the new target. Without this, the library's internal render
        // loop may not pick the change up until the next user input,
        // leaving the orbit pivot visibly stale.
        ctrls.update?.();
      }
    };
    const start = () => {
      if (running) return;
      running = true;
      rafId = requestAnimationFrame(tick);
    };
    if (useStore.getState().selectedId) start();
    const unsub = useStore.subscribe(s => s.selectedId, sel => { if (sel) start(); });
    return () => {
      running = false;
      cancelAnimationFrame(rafId);
      clearInterval(patchTimer);
      unsub();
      window.removeEventListener('mousedown', onDown);
      window.removeEventListener('mouseup', onUp);
    };
  }, [viewMode]);

  // Force tuning — runs ONCE per mount as soon as fg.d3Force is callable.
  // The defaults (chargeStrength -30, linkDistance ~30) collapse big
  // graphs into a tight ball; user feedback was "노드들이 너무 뭉쳐있어
  // 보기 어렵다". We strengthen repulsion and stretch link distance so
  // hub neighbourhoods unfold legibly, then bump cooldownTicks so the
  // simulation has time to settle the 30K-node graph the new boot seed
  // produces. forceCollide would be ideal but requires a direct
  // dependency on d3-force-3d; deferred until we measure whether the
  // tuning alone fixes the legibility issue.
  const forceTunedRef = useRef(false);
  useEffect(() => {
    forceTunedRef.current = false;  // re-tune on viewMode/colorMode remount
  }, [viewMode, colorMode]);
  useEffect(() => {
    if (forceTunedRef.current) return;
    if (graphData.nodes.length === 0) return;
    type D3Force = { strength?: (n: number) => D3Force; distance?: (n: number) => D3Force };
    type FGForce = { d3Force?: (name: string, fn?: unknown) => D3Force | undefined };
    let attempts = 0;
    const tick = setInterval(() => {
      attempts++;
      const fg = fgRef.current as FGForce | null;
      if (!fg?.d3Force) {
        if (attempts > 40) clearInterval(tick);  // ~2s give up
        return;
      }
      try {
        fg.d3Force('charge')?.strength?.(-120);
        fg.d3Force('link')?.distance?.(80);
        forceTunedRef.current = true;
      } catch { /* fall through, library version mismatch */ }
      clearInterval(tick);
    }, 50);
    return () => clearInterval(tick);
  }, [graphData.nodes.length, viewMode]);

  // B-fix (post-bootstrap): once on first non-empty graphData, schedule
  // a zoomToFit so the camera frames the actual node distribution. This
  // doesn't race ForceGraph-3D's initial camera placement (we wait until
  // graphData is in the canvas) and runs exactly once per mount.
  //
  // Why 2.5s: the d3 force simulation runs for cooldownTicks×~16ms ≈
  // 1280ms and ForceGraph re-centres the camera during that window;
  // fitting earlier puts the camera around an unsettled centroid.
  // fitDoneRef makes this idempotent so panel toggles / data refreshes
  // don't re-fit and yank the user's view.
  const fitDoneRef = useRef(false);
  useEffect(() => {
    if (viewMode !== '3d') return;
    if (fitDoneRef.current) return;
    if (graphData.nodes.length === 0) return;
    fitDoneRef.current = true;
    const id = setTimeout(() => {
      type FGFit = { zoomToFit?: (ms?: number, padding?: number) => void };
      const f = fgRef.current as FGFit | null;
      f?.zoomToFit?.(400, 60);
    }, 2500);
    return () => clearTimeout(id);
  }, [graphData.nodes.length, viewMode]);

  // Camera-fix #3 (focus-fit): with focus-as-visibility, a search-pick /
  // list-pick leaves only the focus ball on screen — but the camera was
  // still parked at overview distance, showing a tiny cluster in a sea
  // of nothing. Frame the remaining subgraph via zoomToFit's nodeFilter.
  // Anchored navigation is excluded: the DAG re-layout is still moving
  // at this point and a fit against mid-flight positions frames wrong
  // (it also had no auto-fit before — behaviour preserved). The 350ms
  // delay lets the visibility accessors apply and any centerOnNode
  // dolly begin before the fit supersedes it. Works in 2D and 3D —
  // both engines expose zoomToFit(ms, px, nodeFilter).
  useEffect(() => {
    if (anchorId) return;
    if (focusDistance.size === 0) return;
    const id = setTimeout(() => {
      type FGFit = {
        zoomToFit?: (ms?: number, padding?: number, filter?: (n: GraphNode) => boolean) => void;
      };
      const f = fgRef.current as FGFit | null;
      f?.zoomToFit?.(500, 90, n => focusDistance.has(n.id));
    }, 350);
    return () => clearTimeout(id);
  }, [focusDistance, anchorId]);

  // 흐름(dag) 모드 y/z 압축 force. dag 는 x(계층축)만 구속하고 y/z 는
  // 반발력만 받아 소수 노드가 수직으로 폭주하는데, 그 아웃라이어가 fit
  // 이후 카메라 바로 앞에 걸리면 거대 반투명 메시가 화면 상부를 덮는
  // "블러 레이어"처럼 보인다(user 시각 확인 + DOM elementsFromPoint 프로브
  // 로 캔버스 내부 원인임을 확증). 원점 방향 약한 당김(0.08)으로 y/z 를
  // 밴드로 압축해 아웃라이어 자체를 제거한다. force 모드 복귀 시 해제.
  useEffect(() => {
    type ForceFn = ((alpha: number) => void) & { initialize?: (nodes: unknown[]) => void };
    type FGForce = { d3Force?: (name: string, fn?: ForceFn | null) => unknown };
    let attempts = 0;
    const timer = setInterval(() => {
      attempts++;
      const fg = fgRef.current as FGForce | null;
      if (!fg?.d3Force) {
        if (attempts > 40) clearInterval(timer);
        return;
      }
      if (layoutMode === 'dag') {
        const compact = (axis: 'y' | 'z'): ForceFn => {
          let ns: Array<Record<string, number | undefined>> = [];
          const f = ((alpha: number) => {
            const vKey = 'v' + axis;
            for (const n of ns) {
              const p = n[axis];
              if (typeof p === 'number') n[vKey] = (n[vKey] ?? 0) - p * 0.08 * alpha;
            }
          }) as ForceFn;
          f.initialize = nodes => { ns = nodes as Array<Record<string, number | undefined>>; };
          return f;
        };
        fg.d3Force('compactY', compact('y'));
        fg.d3Force('compactZ', compact('z'));
      } else {
        fg.d3Force('compactY', null);
        fg.d3Force('compactZ', null);
      }
      clearInterval(timer);
    }, 50);
    return () => clearInterval(timer);
  }, [layoutMode, viewMode]);

  // 레이아웃 모드 전환 시 카메라 재프레이밍. dag 'lr' 는 force 클라우드와
  // 전혀 다른 공간 범위(좌→우 계층 벽)로 노드를 재배치하는데, 카메라를
  // 그대로 두면 그래프가 원거리/측면에 놓여 "색이 어두워지고 배경이
  // 달라졌다"로 체감된다(스크린샷 대조로 확인 — force 뷰의 은은한 글로우는
  // 링크 헤이즈였고, 프레이밍이 빠지면 그것까지 사라진다). 재배치가 진행될
  // 시간을 두고 두 번 맞춘다: 1.5s(1차 안정) + 3.2s(cooldown 이후 확정).
  const layoutFitSkipRef = useRef(true);
  useEffect(() => {
    if (layoutFitSkipRef.current) { layoutFitSkipRef.current = false; return; } // 마운트 시 부팅 fit 과 중복 방지
    type Vec3 = { x: number; y: number; z: number };
    type FGFit = {
      zoomToFit?: (ms?: number, padding?: number, filter?: (n: GraphNode) => boolean) => void;
      cameraPosition?: (pos?: Vec3, lookAt?: Vec3, ms?: number) => void;
    };
    // dag 는 아웃라이어가 bbox 를 폭파시킨다: 깊은 호출 체인이 레벨 ×
    // dagLevelDistance(120) 로 수만 유닛까지 뻗어, 전체-bbox fit 은
    // 본체 밀집부를 티끌로 만든다(스크린샷 검증). 3~97 퍼센타일 x 범위의
    // 노드만 프레이밍해 밀집부를 화면에 채운다.
    const fitDense = () => {
      const fg = fgRef.current as FGFit | null;
      if (!fg?.zoomToFit) return;
      const nodes = (graphDataRef.current?.nodes ?? []) as Array<GraphNode & { z?: number }>;
      if (nodes.length <= 50) { fg.zoomToFit(500, 60); return; }
      // 3축 각각 3~97 퍼센타일 범위 계산 — dag 는 x 를 구속하지만 y/z 는
      // 반발력만 받아 소수 노드가 수직으로 폭주한다(실측: x 는 ±960 인데
      // 전체 bbox fit 이 티끌로 보임 = y/z 아웃라이어). 세 축 모두의
      // 밀집 범위 교집합에 드는 노드만 프레이밍한다.
      const range = (get: (n: GraphNode & { z?: number }) => number | undefined) => {
        const vs = nodes.map(get).filter((v): v is number => typeof v === 'number').sort((a, b) => a - b);
        if (!vs.length) return null;
        return [vs[Math.floor(vs.length * 0.03)], vs[Math.floor(vs.length * 0.97)]] as const;
      };
      const rx = range(n => n.x), ry = range(n => n.y), rz = range(n => n.z);
      fg.zoomToFit(500, 60, (n: GraphNode & { z?: number }) =>
        (!rx || (typeof n.x === 'number' && n.x >= rx[0] && n.x <= rx[1])) &&
        (!ry || (typeof n.y === 'number' && n.y >= ry[0] && n.y <= ry[1])) &&
        (!rz || (n.z == null || (n.z >= rz[0] && n.z <= rz[1]))));
    };
    const timers: ReturnType<typeof setTimeout>[] = [];
    if (layoutMode === 'dag') {
      // 카메라를 정면(+z)으로 정렬 — lr 계층 벽을 측면에서 보면 세로
      // 슬랩으로 보인다. 이후 재배치가 진행/안정되는 시점에 두 번 fit.
      timers.push(setTimeout(() => {
        (fgRef.current as FGFit | null)?.cameraPosition?.(
          { x: 0, y: 0, z: 900 }, { x: 0, y: 0, z: 0 }, 300);
      }, 1100));
      timers.push(setTimeout(fitDense, 1800));
      timers.push(setTimeout(fitDense, 3400));
    } else {
      timers.push(setTimeout(fitDense, 1500));
      timers.push(setTimeout(fitDense, 3200));
    }
    return () => timers.forEach(clearTimeout);
  }, [layoutMode]);

  // M2: BFS-ripple — when a fresh focusDistance lands (after a node
  // selection / trace) pulse the focused subgraph outward from dist=0,
  // staggered so dist=1 follows dist=0 by 50ms, dist=2 by 100ms, etc.
  // Pulse shape: scale 1 → 1.25 → 1 over 350ms via a half-sine. The
  // whole ripple completes inside ~700ms (safety cap), then every mesh
  // returns to scale 1. 3D-only this round: ForceGraph-2D's cooldown
  // loop stops drawing after ~2.5s of idle, so a pulse there needs a
  // separate refresh strategy (deferred).
  //
  // focusDistanceRef avoids restarting the rAF loop on every store
  // commit — we read the live value from the ref inside the tick.
  const rippleStartRef = useRef<number | null>(null);
  const focusDistanceRef = useRef(focusDistance);
  focusDistanceRef.current = focusDistance;
  // dimmedNodesRef mirrors dimmedNodes for the stable nodeThreeObject
  // accessor (P4) — same pattern as focusDistanceRef above.
  const dimmedNodesRef = useRef(dimmedNodes);
  dimmedNodesRef.current = dimmedNodes;
  // As-you-type search highlight set (SearchBox writes per keystroke).
  const searchHighlightIds = useStore(s => s.searchHighlightIds);
  const searchHighlightRef = useRef(searchHighlightIds);
  searchHighlightRef.current = searchHighlightIds;
  // P3: the ripple rAF used to run unconditionally for the lifetime of
  // the canvas — 60fps wakeups even when no ripple was in flight (the
  // overwhelmingly common state). The loop now starts when a fresh
  // focusDistance kicks a ripple off and self-terminates at TOTAL.
  // Existing ripple in flight gets truncated and replaced — clicking a
  // new node should always re-pulse, not queue.
  useEffect(() => {
    if (focusDistance.size === 0) return;
    rippleStartRef.current = performance.now();
    const PULSE_DUR = 350;
    const STAGGER = 50;
    const TOTAL = 700;
    const AMP = 0.25;
    // baseScale: nodeMesh stores the usage-derived size in
    // mesh.userData.baseScale. The pulse multiplies it and the restore
    // path returns to it — the old code reset to setScalar(1), which
    // flattened every node to uniform size after the first ripple.
    const base = (mesh: import('three').Mesh): number =>
      (mesh.userData.baseScale as number | undefined) ?? 1;
    let rafId = 0;
    const tick = () => {
      const start = rippleStartRef.current;
      if (start == null) return;
      const elapsed = performance.now() - start;
      const fd = focusDistanceRef.current;
      if (fd.size === 0 || elapsed > TOTAL) {
        if (viewMode === '3d') {
          for (const [, mesh] of meshIndex) mesh.scale.setScalar(base(mesh));
        } else {
          // 2D: one last refresh so drawNode2D paints at unit scale.
          const f = fgRef.current as { refresh?: () => void } | null;
          f?.refresh?.();
        }
        rippleStartRef.current = null;
        return; // self-terminate — no rAF reschedule
      }
      if (viewMode === '3d') {
        for (const [id, mesh] of meshIndex) {
          const b = base(mesh);
          const d = fd.get(id);
          if (d == null) { mesh.scale.setScalar(b); continue; }
          const e2 = elapsed - d * STAGGER;
          if (e2 < 0 || e2 > PULSE_DUR) { mesh.scale.setScalar(b); continue; }
          const t = e2 / PULSE_DUR;
          // Half-sine: 0 at t=0, 1 at t=0.5, 0 at t=1. Times AMP gives
          // an additive bump on top of the base scale.
          mesh.scale.setScalar(b * (1 + AMP * Math.sin(t * Math.PI)));
        }
      } else {
        // 2D: ripple scale is computed inside drawNode2D (it reads
        // rippleStartRef + focusDistance per-node). All we need to
        // do here is force a redraw every frame — ForceGraph-2D stops
        // its render loop after cooldown, so without refresh() the
        // mid-ripple frames never get re-drawn and the pulse is
        // invisible.
        const f = fgRef.current as { refresh?: () => void } | null;
        f?.refresh?.();
      }
      rafId = requestAnimationFrame(tick);
    };
    rafId = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(rafId);
  }, [focusDistance, viewMode, meshIndex]);

  // Reapply focus halo to existing meshes whenever focusDistance OR
  // dimmedNodes changes. Without dimmedNodes in the deps array the 3D
  // canvas wouldn't reflect Impact-item dimming until the user
  // triggered another navigation that reset focusDistance.
  useEffect(() => {
    if (viewMode !== '3d') return;
    const nodes = useStore.getState().nodes;
    for (const [id, mesh] of meshIndex) {
      const n = nodes.get(id);
      if (!n) continue;
      const baseAlpha = ALPHA_BY_CONF[n.confidence ?? ''] ?? 1;
      // Colours/opacity stay at their default-state values regardless of
      // focus (focus now hides instead of dims). Two explicit user-driven
      // modulations exist: the as-you-type search highlight (warm red,
      // reverts when the query clears) and the Impact-panel dim.
      const op = dimmedNodes.has(id) ? 0.2 * baseAlpha : baseAlpha;
      const hex = searchHighlightIds.has(id)
        ? SEARCH_HIGHLIGHT_HEX
        : nodeColorHex(n, colorMode);
      // P1: materials are SHARED via the encoding.ts cache — mutating
      // opacity in place would dim every node of the same colour. Swap
      // the mesh's material reference to the cache entry for the target
      // (color, opacity) instead; the discrete opacity tiers keep the
      // cache bounded and the swap is just a pointer write (no
      // needsUpdate/recompile).
      mesh.material = nodeMaterial(hex, op);
    }
  }, [dimmedNodes, searchHighlightIds, viewMode, meshIndex, colorMode]);

  // 2D repaint on highlight change — ForceGraph-2D idles after cooldown,
  // so without an explicit refresh the search tint would only appear on
  // the next interaction-driven redraw. Cheap: draw only, no sim tick.
  useEffect(() => {
    if (viewMode !== '2d') return;
    (fgRef.current as { refresh?: () => void } | null)?.refresh?.();
  }, [searchHighlightIds, viewMode]);

  // P4: every accessor below is memoized with its actual inputs as
  // deps. react-force-graph diffs props by identity — an inline closure
  // re-created per render forces the library to re-apply the accessor
  // to every node/link on EVERY React commit (focus change, tooltip
  // hover, …). With useCallback the re-apply happens exactly when the
  // accessor's inputs change, which is also the precise moment a
  // repaint is wanted.
  const linkVisibility = useCallback((link: GraphEdge): boolean => {
    if (EDGE_STYLE[link.type]?.hidden) return false;
    if (!edgeTypeWhitelist.has(link.type)) return false;
    // Focus-as-visibility: while a focus ball is active without an
    // anchor (search-pick / list-pick), only links INSIDE the ball
    // draw — everything else is hidden rather than dimmed-to-black.
    // Anchored navigation keeps its own BFS-scoped view (visibleIds).
    if (!anchorId && focusDistance.size > 0) {
      return focusDistance.has(link.src) && focusDistance.has(link.dst);
    }
    // P2: with fullData feeding the simulation, the "both endpoints
    // visible" gate moved from graphData construction into this
    // accessor — hidden-endpoint links stay in the dataset (stable
    // identity) but never draw.
    return visibleIds.has(link.src) && visibleIds.has(link.dst);
  }, [edgeTypeWhitelist, visibleIds, focusDistance, anchorId]);

  const nodeVisibility = useCallback((node: GraphNode): boolean => {
    if (isolatedCommunity != null && node.community_id !== isolatedCommunity) return false;
    // Community legend toggle = visibility off, not just dim. Earlier
    // builds dropped the node opacity to 0.18 which read as "still
    // there but quiet"; user feedback was that off should mean fully
    // hidden until toggled back on. dimmedCommunities is the legend's
    // toggle-off set. brightness/dim role is now redundant for nodes
    // (still applies to edges connected to a hidden node, which won't
    // render anyway because both endpoints fail nodeVisibility).
    if (node.community_id != null && dimmedCommunities.has(node.community_id)) return false;
    // Node-type whitelist gate. node.type undefined → always visible
    // (no UI to gate it on, so we treat it as opted-in by default).
    // Toggling the type off in NodeTypeFilters yields a hidden node
    // without a refetch — the data stays cached, only render flips.
    if (node.type && !nodeTypeWhitelist.has(node.type)) return false;
    // Focus-as-visibility (matches linkVisibility above): search-pick /
    // list-pick shows only the picked node + its traced neighbourhood.
    if (!anchorId && focusDistance.size > 0) return focusDistance.has(node.id);
    return visibleIds.has(node.id);
  }, [isolatedCommunity, dimmedCommunities, nodeTypeWhitelist, visibleIds, focusDistance, anchorId]);

  const linkColor = useCallback((e: GraphEdge): string => {
    const base = EDGE_STYLE[e.type]?.color ?? 0x999999;
    // Colours never change with focus (user requirement) — the ONLY
    // colour modulation left is the explicit Impact-panel dim, which is
    // a deliberate user action on a named subgraph.
    const dimmed = dimmedNodes.size > 0 &&
      (dimmedNodes.has(e.src) || dimmedNodes.has(e.dst));
    return hexAtBrightness(base, dimmed ? 0.2 : 1.0);
  }, [dimmedNodes]);

  const linkWidth = useCallback((e: GraphEdge): number => {
    const base = EDGE_STYLE[e.type]?.width ?? 1;
    if (dimmedNodes.size > 0 &&
        (dimmedNodes.has(e.src) || dimmedNodes.has(e.dst))) {
      // Impact-dim edges recede behind the spotlighted subgraph
      // without becoming invisible.
      return 0.25;
    }
    return base;
  }, [dimmedNodes]);

  // linkLineDash (#3b): per-edge dash pattern keyed off the CKS graph
  // group. Returning null defers to a solid line. The 2D force-graph
  // wires this prop into ctx.setLineDash on every link draw.
  //   G1 Structural   → solid
  //   G2 Semantic     → [6,3]   dashed
  //   G3 Execution    → solid (default; the most-traversed axis stays clean)
  //   G4 Concurrency  → [2,2]   dotted
  //   G5 Distributed  → [6,2,2,2] dash-dot
  //   G6 Temporal     → solid (the existing dim brightness already de-emphasises it)
  // Edges whose group is unknown also fall through to solid.
  const linkLineDash = useCallback((e: GraphEdge): number[] | null => {
    const g = edgeToGroup(e.type);
    switch (g) {
      case 'G2': return [6, 3];
      case 'G4': return [2, 2];
      case 'G5': return [6, 2, 2, 2];
      default:   return null;
    }
  }, []);

  // H3 (D3 decision): src→dst alpha gradient overlay. Drawn ON TOP of the
  // default link line (linkCanvasObjectMode='after'), so the dst end shows
  // the default colour underneath and the src end gets a brighter cap
  // that reads as "this is where the call comes from". The overlay line
  // is widened (×1.6) so the src cap is visibly thicker than the dst
  // trail — direction without needing to find the arrowhead.
  //
  // Trade-off: 'after' mode preserves react-force-graph's dash + arrow
  // draws for free. The dst-end fade is therefore subtle (the default
  // line is still there underneath). Switch to 'replace' if a stronger
  // fade is needed, but then we have to re-implement dash + arrow draws.
  const linkCanvasObjectMode = useMemo(() => (() => 'after') as never, []);
  const linkCanvasObject = useCallback(((
    link: GraphEdge & {
      source?: GraphNode; target?: GraphNode;
      // P4 gradient cache slots — see below.
      __gradKey?: string; __grad?: CanvasGradient;
    },
    ctx: CanvasRenderingContext2D,
  ) => {
    const a = link.source, b = link.target;
    if (!a || !b || a.x == null || b.x == null || a.y == null || b.y == null) return;
    // Brightness model matches linkColor: default 1.0, Impact-dim 0.2 —
    // focus never dims (off-focus links are hidden by linkVisibility).
    const dimmed = dimmedNodes.size > 0 &&
      (dimmedNodes.has(link.src) || dimmedNodes.has(link.dst));
    const brightness = dimmed ? 0.2 : 1.0;
    if (brightness < 0.3) return; // impact-dimmed: skip the overlay entirely
    const baseHex = EDGE_STYLE[link.type]?.color ?? 0x999999;
    const r = (baseHex >> 16) & 0xff;
    const g = (baseHex >> 8) & 0xff;
    const bl = baseHex & 0xff;
    const srcAlpha = 0.85 * brightness;
    // P4: cache the gradient on the link object, keyed by quantised
    // endpoint coords + colour + alpha. While the simulation is moving
    // the key churns (allocation rate same as before); once the layout
    // settles the coords are static, the key stops changing, and the
    // per-edge-per-frame createLinearGradient allocation drops to zero
    // — which is the steady state users actually interact in.
    const key = `${a.x | 0},${a.y | 0},${b.x | 0},${b.y | 0},${baseHex},${srcAlpha.toFixed(2)}`;
    let grad = link.__grad;
    if (!grad || link.__gradKey !== key) {
      grad = ctx.createLinearGradient(a.x, a.y, b.x, b.y);
      grad.addColorStop(0, `rgba(${r},${g},${bl},${srcAlpha})`);
      grad.addColorStop(1, `rgba(${r},${g},${bl},0)`);
      link.__gradKey = key;
      link.__grad = grad;
    }
    ctx.save();
    ctx.strokeStyle = grad;
    ctx.lineWidth = linkWidth(link) * 1.6;
    ctx.setLineDash([]); // overlay is always solid; the dash pattern lives in the underlying default line
    ctx.beginPath();
    ctx.moveTo(a.x, a.y);
    ctx.lineTo(b.x, b.y);
    ctx.stroke();
    ctx.restore();
  }) as never, [dimmedNodes, linkWidth]);

  const drawNode2D = useCallback((node: GraphNode, ctx: CanvasRenderingContext2D, globalScale: number) => {
    // nodeSizeScore: usage_score with a degree fallback — usage_score is
    // 0 across entire real indexes, which flattened every node to the
    // same 3px dot and hid the per-type shapes.
    let r = 3 + Math.log10(nodeSizeScore(node) + 1) * 1.5;
    // C: 2D ripple — apply the same dist-staggered half-sine pulse used
    // by 3D mesh.scale to the radius the 2D rasteriser uses below. The
    // ripple effect's rAF drives forceGraph.refresh() every frame
    // during the active window, so each drawNode2D call sees the
    // current `elapsed` and adjusts r accordingly. Outside the window
    // (rippleStartRef === null) the multiplier is unity and the cost
    // is two map lookups + one branch.
    const start = rippleStartRef.current;
    if (start != null) {
      const elapsed = performance.now() - start;
      if (elapsed <= 700) {
        const d = focusDistance.get(node.id);
        if (d != null) {
          const e2 = elapsed - d * 50;
          if (e2 >= 0 && e2 <= 350) {
            const t = e2 / 350;
            r *= 1 + 0.25 * Math.sin(t * Math.PI);
          }
        }
      }
    }
    const dimmedByCommunity = node.community_id != null && dimmedCommunities.has(node.community_id);
    // dimmedByImpact: Impact-item click pushed this node into the dim
    // set so the rest of the visible graph stays visible but recedes
    // behind the impact subgraph. 0.2 alpha matches the spec for this
    // feature; lower than the 0.18 community-dim used to be (now
    // hidden entirely) but visibly distinct from the FOCUS_OPACITY
    // far-cells (0.18) so the user reads "deliberately backgrounded"
    // rather than "out of focus".
    const dimmedByImpact = dimmedNodes.has(node.id);
    // Default-state alpha only (confidence tier) — focus never repaints;
    // off-focus nodes are hidden by nodeVisibility instead.
    const baseAlpha = ALPHA_BY_CONF[node.confidence ?? ''] ?? 1;
    const op = dimmedByCommunity ? 0.18 : (dimmedByImpact ? 0.2 : baseAlpha);
    ctx.globalAlpha = op;
    // As-you-type search highlight mirrors the 3D material swap.
    ctx.fillStyle = searchHighlightIds.has(node.id)
      ? SEARCH_HIGHLIGHT_CSS
      : nodeColorCss(node, colorMode);
    // Shape differentiation (#3a): map node.type to a 2D primitive.
    // The fallback is a circle so unknown types degrade gracefully.
    // Sizes derived from `r` so usage_score still drives node prominence
    // across shapes — bigger functions / packages stay visibly larger.
    const cx = node.x ?? 0;
    const cy = node.y ?? 0;
    const rr = Math.max(2, r);
    drawShape(ctx, node.type, cx, cy, rr);
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
  }, [colorMode, fontSize, dimmedCommunities, dimmedNodes, focusDistance, searchHighlightIds]);

  const pointerArea2D = useCallback((node: GraphNode, color: string, ctx: CanvasRenderingContext2D) => {
    const r = Math.max(4, 3 + Math.log10(nodeSizeScore(node) + 1) * 1.5);
    ctx.fillStyle = color;
    ctx.beginPath();
    ctx.arc(node.x ?? 0, node.y ?? 0, r + 3, 0, 2 * Math.PI);
    ctx.fill();
  }, []);

  const tooltip = useCallback(
    (node: GraphNode): string => buildTooltip(node, focusDistance, fontSize),
    [focusDistance, fontSize],
  );

  const onClick = useCallback((n: GraphNode | { id?: NodeId }) => {
    const id = (n as GraphNode).id;
    if (id) onNodeClick(id);
  }, [onNodeClick]);

  // Layout key forces a hard remount when viewMode flips so meshes don't leak.
  const key = `fg-${viewMode}-${colorMode}`;

  // anchorId drives dagMode — when set, the layout flips to hierarchical
  // 'lr' (left-to-right) so caller→anchor→callee lays out left-to-right
  // and the user can read flow direction by eye. Without an anchor we
  // stay in pure force mode so the boot view shows the natural
  // architectural cluster shape. onDagError='remove' tells force-graph
  // to skip cycle-forming edges in the level assignment (call graphs
  // routinely have mutual recursion); the edges themselves still render,
  // just without participating in the level computation.
  // (anchorId is subscribed above for the graphData split — P2.)
  // 전역 흐름 모드(layoutMode==='dag')에서도 좌→우 계층을 적용한다.
  const dagMode = (anchorId || layoutMode === 'dag') ? 'lr' : undefined;
  // Scale cooldown with node count: 80 ticks suffices for the 400-node
  // boot seed, but the new 30K-node production-only boot needs more
  // time to settle. cooldownTime caps the wall-clock budget so users
  // never wait more than ~6s, even on a 60K-node payload.
  const isLargeGraph = graphData.nodes.length > 8000;
  const cooldownTicks = isLargeGraph ? 250 : 80;
  const cooldownTime = isLargeGraph ? 6000 : 2500;
  // Arrow length pumped 3 → 6, slightly nudged toward dst (0.95 → 0.92)
  // so the arrowhead reads at the typical zoom level even with the
  // dst-end gradient overlay (linkCanvasObject) drawn on top.
  const arrowLength = 6;
  const arrowRelPos = 0.92;

  // P6: 3D link LOD. In 3d-force-graph, linkWidth > 0 upgrades every
  // link from a batched GL line to a per-link cylinder mesh, and each
  // directional arrow adds a cone mesh — at 30K+ links that is tens of
  // thousands of extra draw calls, the dominant 3D cost after
  // materials. The raw library's default (and the reason its demos
  // feel effortless) is width 0 → lines.
  //
  // Revision (user feedback): the first cut keyed this off
  // `graphData.links.length > 2000`, i.e. the CURRENTLY displayed
  // graph — so boot(5K) drew lines, clicking a node (≤800-node BFS
  // subset) flipped to cylinders+arrows, and changing nodeLimit across
  // the threshold flipped the whole canvas style. Unpredictable.
  // The LOD is now tied to VIEW INTENT, which is stable:
  //   - overview (no anchor): ALWAYS lines — regardless of nodeLimit,
  //     so 500 and 10K boots look alike and selection doesn't surprise.
  //   - anchored focus view: cylinders + arrows — the small subgraph
  //     is where direction/width actually carry meaning, and it can
  //     afford the meshes. A 4K-link safety cap keeps giant hub
  //     neighbourhoods from degrading (falls back to lines).
  const rich3D = !!anchorId && graphData.links.length <= 4000;

  // P4: stable nodeThreeObject. When this accessor's identity changes,
  // react-force-graph re-runs it for EVERY node — a full mesh rebuild.
  // The previous inline closure changed identity on every render, so
  // each focus click / hover-driven re-render rebuilt all N meshes.
  // Focus/dim state is read through refs; per-node appearance updates
  // after mount are handled by the focus-halo effect swapping shared
  // materials (P1). Identity now changes only with colorMode (a full
  // re-skin, where a rebuild is the intent).
  const nodeThreeObject = useCallback((node: GraphNode) => {
    const m = nodeMesh(node, colorMode);
    meshIndex.set(node.id, m);
    const baseAlpha = ALPHA_BY_CONF[node.confidence ?? ''] ?? 1;
    // Default-state colour always; explicit user-driven modulations only
    // (search highlight / Impact dim — focus hides via nodeVisibility,
    // never repaints). Refs keep this accessor's identity stable.
    const op = dimmedNodesRef.current.has(node.id) ? 0.2 * baseAlpha : baseAlpha;
    const hex = searchHighlightRef.current.has(node.id)
      ? SEARCH_HIGHLIGHT_HEX
      : nodeColorHex(node, colorMode);
    // Shared-material swap (P1): appearance = cache lookup, never an
    // in-place material mutation.
    m.material = nodeMaterial(hex, op);
    return m;
  }, [colorMode, meshIndex]);

  if (viewMode === '2d') {
    return (
      <ForceGraph2D
        key={key}
        ref={fgRef as never}
        width={width}
        height={height}
        graphData={graphData}
        linkSource="src"
        linkTarget="dst"
        dagMode={dagMode as never}
        dagLevelDistance={120}
        onDagError={(() => 'remove') as never}
        nodeLabel={tooltip as never}
        nodeVisibility={nodeVisibility as never}
        linkVisibility={linkVisibility as never}
        linkColor={linkColor as never}
        linkWidth={linkWidth as never}
        linkLineDash={linkLineDash as never}
        linkDirectionalArrowLength={arrowLength}
        linkDirectionalArrowRelPos={arrowRelPos}
        linkCanvasObject={linkCanvasObject}
        linkCanvasObjectMode={linkCanvasObjectMode}
        nodeCanvasObject={drawNode2D as never}
        nodePointerAreaPaint={pointerArea2D as never}
        backgroundColor="rgba(0,0,0,0)"
        cooldownTicks={cooldownTicks}
        cooldownTime={cooldownTime}
        onNodeClick={onClick as never}
        onEngineStop={onEngineStop as never}
      />
    );
  }

  return (
    <ForceGraph3D
      key={key}
      ref={fgRef as never}
      width={width}
      height={height}
      graphData={graphData}
      linkSource="src"
      linkTarget="dst"
      // controlType="orbit" so the camera orbits around a fixed target
      // (which we lock to the selected node in centerOnNode). The library
      // default is "trackball" — there's no target concept and a user
      // drag freely rotates the camera, which makes outer nodes drift
      // out of the frustum (R1.5-a).
      controlType="orbit"
      dagMode={dagMode as never}
      dagLevelDistance={120}
      onDagError={(() => 'remove') as never}
      nodeLabel={tooltip as never}
      nodeVisibility={nodeVisibility as never}
      linkVisibility={linkVisibility as never}
      linkColor={linkColor as never}
      // P6: overview = lines; anchored focus = cylinders + arrows —
      // see rich3D above.
      linkWidth={rich3D ? (linkWidth as never) : 0}
      linkDirectionalArrowLength={rich3D ? arrowLength : 0}
      linkDirectionalArrowRelPos={arrowRelPos}
      // Transparent clear color so the .canvas-host purple/navy
      // radial-gradient (globals.css) bleeds through. Without this,
      // react-force-graph-3d defaults to #000 and the gradient is
      // hidden behind the WebGL canvas.
      backgroundColor="rgba(0,0,0,0)"
      onEngineStop={onEngineStop as never}
      cooldownTicks={cooldownTicks}
      cooldownTime={cooldownTime}
      onNodeClick={onClick as never}
      nodeThreeObject={nodeThreeObject as never}
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

// drawShape (#3a): fill a 2D primitive at (cx, cy) sized off `r`. The
// caller has already set ctx.fillStyle / ctx.globalAlpha. Each branch
// builds a path and fills it. Stroke-only shapes (chevron, asterisk)
// override fillStyle's effect by stroking with the same colour.
//
// 3D parity is deferred — see lib/encoding.nodeMesh for the existing
// per-type Three.js geometry table; updating it to mirror this 1:1 is
// future work. The 2D mode shipping today gets the legend reading aid;
// the 3D mesh table remains as-is.
function drawShape(
  ctx: CanvasRenderingContext2D,
  type: string | undefined,
  cx: number, cy: number, r: number,
): void {
  switch (type) {
    case 'Type':
    case 'Struct':
    case 'Interface':
    case 'TypeAlias': {
      // Hexagon — pointy-top variant so it reads distinct from a square.
      ctx.beginPath();
      for (let i = 0; i < 6; i++) {
        const ang = (Math.PI / 3) * i + Math.PI / 6;
        const x = cx + r * Math.cos(ang);
        const y = cy + r * Math.sin(ang);
        if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
      }
      ctx.closePath();
      ctx.fill();
      return;
    }
    case 'Package': {
      // Rounded rectangle — the docked-square shape reads as "container".
      const s = r * 1.6;
      const rad = Math.min(2, s / 4);
      const x = cx - s / 2, y = cy - s / 2;
      ctx.beginPath();
      ctx.moveTo(x + rad, y);
      ctx.lineTo(x + s - rad, y);
      ctx.quadraticCurveTo(x + s, y, x + s, y + rad);
      ctx.lineTo(x + s, y + s - rad);
      ctx.quadraticCurveTo(x + s, y + s, x + s - rad, y + s);
      ctx.lineTo(x + rad, y + s);
      ctx.quadraticCurveTo(x, y + s, x, y + s - rad);
      ctx.lineTo(x, y + rad);
      ctx.quadraticCurveTo(x, y, x + rad, y);
      ctx.closePath();
      ctx.fill();
      return;
    }
    case 'Field':
    case 'Variable':
    case 'Constant':
    case 'LocalVariable':
    case 'Parameter': {
      // Small downward-pointing triangle — visually lighter than a circle.
      const tr = r * 0.95;
      ctx.beginPath();
      ctx.moveTo(cx, cy + tr);
      ctx.lineTo(cx - tr, cy - tr * 0.6);
      ctx.lineTo(cx + tr, cy - tr * 0.6);
      ctx.closePath();
      ctx.fill();
      return;
    }
    case 'File': {
      // Diamond / rotated square.
      ctx.beginPath();
      ctx.moveTo(cx, cy - r);
      ctx.lineTo(cx + r, cy);
      ctx.lineTo(cx, cy + r);
      ctx.lineTo(cx - r, cy);
      ctx.closePath();
      ctx.fill();
      return;
    }
    case 'Commit': {
      // 5-point star.
      ctx.beginPath();
      const outer = r * 1.1;
      const inner = r * 0.45;
      for (let i = 0; i < 10; i++) {
        const rad = i % 2 === 0 ? outer : inner;
        const ang = (Math.PI / 5) * i - Math.PI / 2;
        const x = cx + rad * Math.cos(ang);
        const y = cy + rad * Math.sin(ang);
        if (i === 0) ctx.moveTo(x, y); else ctx.lineTo(x, y);
      }
      ctx.closePath();
      ctx.fill();
      return;
    }
    case 'CallSite':
    case 'IfStmt':
    case 'LoopStmt':
    case 'ReturnStmt':
    case 'SwitchStmt':
    case 'Hunk': {
      // Micro dot — 1.5x smaller circle so statement-level + meta-Hunk nodes
      // recede when the type is enabled in NodeTypeFilters. Hunk shares the
      // micro shape because it's a fine-grained meta node (one diff block);
      // distinguishing it visually from CallSite/IfStmt would require a
      // separate shape we'd then have to legend, and the differentiation
      // is already carried by node color (NodeHunk gets the G6 muted-purple
      // family that EDGE_STYLE.has_hunk uses).
      ctx.beginPath();
      ctx.arc(cx, cy, r / 1.5, 0, 2 * Math.PI);
      ctx.fill();
      return;
    }
    case 'Goroutine': {
      // Upward-pointing equilateral triangle (concurrency family — pink).
      const tr = r;
      ctx.beginPath();
      ctx.moveTo(cx, cy - tr);
      ctx.lineTo(cx - tr * 0.95, cy + tr * 0.7);
      ctx.lineTo(cx + tr * 0.95, cy + tr * 0.7);
      ctx.closePath();
      ctx.fill();
      return;
    }
    case 'Channel': {
      // Chevron / open arrow — strokes a `>` glyph at radius r. Stroke-
      // only so the shape reads as "open" rather than a filled blob,
      // matching the channel-as-pipe metaphor.
      ctx.save();
      const stroke = ctx.fillStyle as string | CanvasGradient | CanvasPattern;
      ctx.strokeStyle = typeof stroke === 'string' ? stroke : '#cc66cc';
      ctx.lineWidth = Math.max(1, r / 3);
      ctx.lineCap = 'round';
      ctx.lineJoin = 'round';
      ctx.beginPath();
      ctx.moveTo(cx - r * 0.7, cy - r * 0.7);
      ctx.lineTo(cx + r * 0.4, cy);
      ctx.lineTo(cx - r * 0.7, cy + r * 0.7);
      ctx.stroke();
      ctx.restore();
      return;
    }
    case 'Mutex': {
      // Filled square with a hollow centre — proxies a lock glyph.
      // Outer fill uses the current fillStyle; inner hollow is the
      // canvas background so it reads as a hole regardless of palette.
      const s = r * 1.5;
      ctx.beginPath();
      ctx.rect(cx - s / 2, cy - s / 2, s, s);
      ctx.fill();
      ctx.save();
      ctx.fillStyle = '#0d0e10';
      const hs = s * 0.4;
      ctx.beginPath();
      ctx.rect(cx - hs / 2, cy - hs / 2, hs, hs);
      ctx.fill();
      ctx.restore();
      return;
    }
    case 'Endpoint': {
      // Asterisk — three lines through (cx, cy). Stroke-only so the
      // glyph reads as a "burst" against the canvas.
      ctx.save();
      const stroke = ctx.fillStyle as string | CanvasGradient | CanvasPattern;
      ctx.strokeStyle = typeof stroke === 'string' ? stroke : '#44aaff';
      ctx.lineWidth = Math.max(1, r / 3);
      ctx.lineCap = 'round';
      for (const deg of [0, 60, 120]) {
        const ang = (deg * Math.PI) / 180;
        ctx.beginPath();
        ctx.moveTo(cx - r * Math.cos(ang), cy - r * Math.sin(ang));
        ctx.lineTo(cx + r * Math.cos(ang), cy + r * Math.sin(ang));
        ctx.stroke();
      }
      ctx.restore();
      return;
    }
    default: {
      // Function / Method / unknown → circle (current behaviour).
      ctx.beginPath();
      ctx.arc(cx, cy, r, 0, 2 * Math.PI);
      ctx.fill();
      return;
    }
  }
}

