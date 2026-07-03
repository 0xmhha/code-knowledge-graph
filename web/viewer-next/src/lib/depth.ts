import type { CommitGraph, GraphEdge, NodeId } from '@/types';
import type { IAPI } from '@/lib/api';
import { useStore, computeFocusDistance } from '@/store/store';
import { isTestPath } from '@/lib/testFilter';

const MAX_VISIBLE = 800;

// Node limit is now driven by store.nodeLimit (user-controllable via
// the TopBar select). The store enforces a clamp of [100, 100000];
// recomputeVisible reads the live value at fetch time so a toggle
// applies on the very next refetch without prop-drilling.
//
// Earlier iteration (2026-05-22 morning) loaded 32K production nodes
// to honour the "load everything once" intent. Real-world test on the
// user's notebook (2026-05-22 afternoon) showed that scale exceeded
// available GPU/CPU — every interaction stalled. The default is 5K
// (notebook-friendly) but the user can dial up or down freely.

// Statement-level + identifier-level node types pulled out of the boot
// seed. Statements (CallSite/IfStmt/...) are noisy micro-shapes that
// dominate count without adding architectural insight; Field/Variable/
// Constant balloon the node count by ~18K on the target repo (mostly
// implementation detail). Hunk/Commit/Import are infrastructural
// G6 Temporal nodes the boot view doesn't need either. Users opt
// any of these in through the NodeTypeFilters panel once they want
// finer detail — the data stays cached client-side after first fetch.
const BOOT_EXCLUDED_TYPES: ReadonlyArray<string> = [
  'CallSite', 'IfStmt', 'ReturnStmt', 'LoopStmt', 'SwitchStmt',
  'Hunk', 'Commit', 'Import',
  'Field', 'Variable', 'Constant',
];

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
    // Boot seed = ALL production non-statement nodes (filtered by
    // excludeTests). topNodes('pagerank', big-limit, BOOT_EXCLUDED_TYPES)
    // returns every node of the kept types ranked by PageRank — when the
    // limit exceeds the row count, we get the whole population.
    //
    // Fallback to api.nodes('') is preserved for older backends that
    // don't expose /api/nodes/top, but with a much smaller default since
    // those backends don't support the excludeTypes filter and the
    // payload would otherwise include 100K+ statement nodes.
    let top = await api.topNodes('pagerank', s.nodeLimit, [...BOOT_EXCLUDED_TYPES]);
    if (top.length === 0) top = await api.nodes('', 5000);

    // Apply test-code filter client-side. Keeping the filter on the
    // client means the toggle in TopBar reapplies without re-fetching;
    // the trade-off is wire-time bandwidth, accepted because the
    // payload still stays well under 32 MB on the target repo.
    if (s.excludeTests) {
      top = top.filter(n => !isTestPath(n.file_path));
    }

    s.loadNodes(top);

    // Fetch edges for the boot set. With 30K+ nodes we batch the IDs
    // to keep the POST body under a sane size — Go backend handles
    // ~5K IDs per request comfortably.
    //
    // P5: accumulate every batch and commit through addEdges ONCE. The
    // previous per-batch addEdges fired a store notification each time,
    // which (a) re-copied the edge indexes per batch and (b) — with the
    // P2 fullData derivation in GraphCanvas — would re-ingest the graph
    // and restart the simulation once per batch during boot. One call =
    // one index rebuild = one ingest. addEdges still dedupes internally.
    const ids = top.map(n => n.id);
    const BATCH = 5000;
    const collected: GraphEdge[] = [];
    for (let i = 0; i < ids.length; i += BATCH) {
      const slice = ids.slice(i, i + BATCH);
      const fresh = await api.edges(slice);
      // NOT push(...fresh): spread passes every element as a call
      // argument, and a 5K-id batch can return 100K+ edges — enough to
      // blow the call stack (RangeError: Maximum call stack size
      // exceeded). A plain loop appends without stack growth.
      for (const e of fresh) collected.push(e);
    }
    if (collected.length) s.addEdges(collected);

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
