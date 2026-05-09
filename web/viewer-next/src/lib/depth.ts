import type { CommitGraph, NodeId } from '@/types';
import type { IAPI } from '@/lib/api';
import { useStore, computeFocusDistance } from '@/store/store';

const MAX_VISIBLE = 500;

// BOOT_VISIBLE caps the initial seed at 200 nodes — below MAX_VISIBLE so
// after the user navigates (anchor-driven BFS) more nodes can join the
// visible set. 200 keeps the canvas readable on first paint while still
// surfacing enough hub symbols that 1-hop expansion shows real call/import
// structure rather than disconnected packages.
const BOOT_VISIBLE = 200;

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
    // Prefer top-by-pagerank: hub functions/methods/types surface naturally
    // and 1-hop neighbours show real call/import structure. Fall back to
    // nodes('') when the new endpoint is missing (older backends, or when
    // the static export has no pagerank/usage_score values populated).
    // excludeTypes=['Commit', 'Hunk']: git meta nodes (Commit, Hunk —
    // schema 1.4/1.8 G6 Temporal) are excluded from PageRank participation
    // in score.Compute (§11.7 decision), so they land at zero rank and
    // trail real symbols. The SQL-layer filter still applies defensively
    // so a future PageRank-rule change can't surprise the boot seed —
    // and so older graph.dbs (where Commits did outrank symbols, e.g.
    // 104/200 were Commits in the self-graph) keep the corrected boot
    // behaviour. Hunk's only inbound edge is `has_hunk` (off in
    // DEFAULT_EDGE_TYPES) — without exclusion it would produce the same
    // "node visible but no edges" symptom that drove the original Commit
    // exclusion in pre-1.8.
    let top = await api.topNodes('pagerank', BOOT_VISIBLE, ['Commit', 'Hunk']);
    if (top.length === 0) top = await api.nodes('', BOOT_VISIBLE);
    s.loadNodes(top);

    // Fetch edges for the seed in one batch — without this the canvas
    // shows the seed nodes but zero edges between them (V1-1 bug).
    const ids = top.map(n => n.id);
    if (ids.length) {
      const fresh = await api.edges(ids);
      if (fresh.length) s.addEdges(fresh);
    }

    return {
      visibleIds: new Set(ids),
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
