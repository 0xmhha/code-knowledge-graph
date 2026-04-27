import type { CommitGraph, NodeId } from '@/types';
import type { IAPI } from '@/lib/api';
import { useStore, computeFocusDistance } from '@/store/store';

const MAX_VISIBLE = 500;

// recomputeVisible builds the next CommitGraph and returns it. It does NOT
// commit — callers run store.commit() so the renderer sees one push.
//
// Side effects allowed: nodes / edges may be lazy-fetched into the store
// during traversal (loadNodes / addEdges), but those mutate cache only and
// do not trigger render (canvas listens to visibleIds/focusDistance only).
export async function recomputeVisible(api: IAPI): Promise<CommitGraph> {
  const s = useStore.getState();
  const { anchorId, depth } = s;

  if (!anchorId) {
    const top = await api.nodes('', MAX_VISIBLE);
    s.loadNodes(top);
    return {
      visibleIds: new Set(top.map(n => n.id)),
      focusDistance: new Map(),
      reason: 'boot',
    };
  }

  const visible = new Set<NodeId>([anchorId]);
  let frontier: NodeId[] = [anchorId];
  const needFetch = new Set<NodeId>();
  if ((s.edgesBySrc.get(anchorId)?.length ?? 0) === 0 &&
      (s.edgesByDst.get(anchorId)?.length ?? 0) === 0) {
    needFetch.add(anchorId);
  }

  for (let d = 0; d < depth && visible.size < MAX_VISIBLE; d++) {
    if (needFetch.size) {
      const ids = [...needFetch];
      needFetch.clear();
      const fresh = await api.edges(ids);
      if (fresh.length) s.addEdges(fresh);
    }

    const cur = useStore.getState();
    const next: NodeId[] = [];
    for (const id of frontier) {
      const outs = cur.edgesBySrc.get(id) ?? [];
      const ins = cur.edgesByDst.get(id) ?? [];
      for (const e of outs.concat(ins)) {
        const other = e.src === id ? e.dst : e.src;
        if (visible.has(other)) continue;
        visible.add(other);
        next.push(other);
        if (!cur.edgesBySrc.has(other) && !cur.edgesByDst.has(other)) {
          needFetch.add(other);
        }
        if (visible.size >= MAX_VISIBLE) break;
      }
      if (visible.size >= MAX_VISIBLE) break;
    }
    frontier = next;
    if (!frontier.length) break;
  }

  const cur = useStore.getState();
  const missing = [...visible].filter(id => !cur.nodes.has(id));
  if (missing.length) {
    const fetched = await api.nodesByIds(missing);
    if (fetched.length) cur.loadNodes(fetched);
  }

  const after = useStore.getState();
  const focus = computeFocusDistance(
    anchorId, after.edgesBySrc, after.edgesByDst, Math.min(depth, 2),
  );
  return { visibleIds: visible, focusDistance: focus, reason: 'navigate' };
}
