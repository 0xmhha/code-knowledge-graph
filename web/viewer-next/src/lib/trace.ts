import type { CommitGraph, NodeId, TraceDirection } from '@/types';
import type { IAPI } from '@/lib/api';
import { useStore } from '@/store/store';

const MAX_TRACE_VISIBLE = 500;

export interface TraceOpts {
  direction: TraceDirection;
  depth: number;
  edgeTypes: Set<string>;
}

// traceFromNode walks the graph in the requested direction and depth,
// returning a CommitGraph the caller pushes via store.commit().
//
// Phase 2 decisions baked in:
//  - 'callers' follows reverse edges only (edgesByDst).  → "what calls me?"
//  - 'callees' follows forward edges only (edgesBySrc).  → "what do I call?"
//  - 'both'    walks both, preferred default for code reading.
//  - asymmetric depth: 'callers' mode searches `depth + 2` hops because
//    call paths usually need a deeper view to reach the entrypoint, while
//    callees within 2 hops is usually enough to spot the next decision.
//
// edgeTypes filter is applied on each hop — only edges of types in the
// whitelist are followed AND surfaced in the result.
export async function traceFromNode(
  api: IAPI, startId: NodeId, opts: TraceOpts,
): Promise<CommitGraph> {
  const { direction, edgeTypes } = opts;
  const effectiveDepth = direction === 'callers' ? opts.depth + 2 : opts.depth;

  // Ensure edges for the start node are loaded.
  await ensureEdgesLoaded(api, [startId]);

  const visible = new Set<NodeId>([startId]);
  const focus = new Map<NodeId, number>([[startId, 0]]);
  let frontier: NodeId[] = [startId];

  for (let d = 0; d < effectiveDepth && visible.size < MAX_TRACE_VISIBLE; d++) {
    const next: NodeId[] = [];
    const toLoad: NodeId[] = [];
    const cur = useStore.getState();

    for (const id of frontier) {
      const candidates: NodeId[] = [];

      if (direction === 'callees' || direction === 'both') {
        for (const e of cur.edgesBySrc.get(id) ?? []) {
          if (!edgeTypes.has(e.type)) continue;
          candidates.push(e.dst);
        }
      }
      if (direction === 'callers' || direction === 'both') {
        for (const e of cur.edgesByDst.get(id) ?? []) {
          if (!edgeTypes.has(e.type)) continue;
          candidates.push(e.src);
        }
      }

      for (const other of candidates) {
        if (visible.has(other)) continue;
        visible.add(other);
        focus.set(other, d + 1);
        next.push(other);
        if (!cur.edgesBySrc.has(other) && !cur.edgesByDst.has(other)) toLoad.push(other);
        if (visible.size >= MAX_TRACE_VISIBLE) break;
      }
      if (visible.size >= MAX_TRACE_VISIBLE) break;
    }

    if (toLoad.length) await ensureEdgesLoaded(api, toLoad);
    frontier = next;
    if (!frontier.length) break;
  }

  // Materialise any node we discovered but haven't loaded yet.
  const cur = useStore.getState();
  const missing = [...visible].filter(id => !cur.nodes.has(id));
  if (missing.length) {
    const fetched = await api.nodesByIds(missing);
    if (fetched.length) cur.loadNodes(fetched);
  }

  return { visibleIds: visible, focusDistance: focus, reason: 'trace' };
}

async function ensureEdgesLoaded(api: IAPI, ids: NodeId[]): Promise<void> {
  const cur = useStore.getState();
  const need = ids.filter(id => !cur.edgesBySrc.has(id) && !cur.edgesByDst.has(id));
  if (!need.length) return;
  const fresh = await api.edges(need);
  if (fresh.length) useStore.getState().addEdges(fresh);
}
