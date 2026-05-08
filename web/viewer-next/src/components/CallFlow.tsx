'use client';

import { memo, useMemo } from 'react';
import { useStore } from '@/store/store';
import type { GraphNode, NodeId } from '@/types';

interface Props {
  onPick: (id: NodeId) => void;
}

// CallFlow walks the callee tree (downward flow) from the active anchor
// using already-cached edges in the store. It does NOT call the API: the
// trace that anchored the user's attention has already loaded the
// neighbouring nodes & edges via traceFromNode, so a synchronous BFS is
// enough for the first cut.
//
// Bounds:
//   - DEPTH_MAX = 3 layers below the anchor.
//   - PER_DEPTH_CAP = 50 nodes at any single depth, to keep render cost
//     bounded on hubs (e.g. main() which calls 200+ helpers).
//   - Edge type filter: only `calls` and `invokes` count as call-flow
//     edges. `defines`/`imports`/`contains` don't represent runtime
//     control flow and would clutter the tree.
const DEPTH_MAX = 3;
const PER_DEPTH_CAP = 50;
const CALL_EDGE_TYPES = new Set(['calls', 'invokes']);

interface FlowRow {
  id: NodeId;
  depth: number;        // 0 = anchor, 1..DEPTH_MAX = callees
  // isLast tracks whether this row is the last child at its depth/parent
  // pair, so the connector renders └─ instead of ├─.
  isLast: boolean;
  // ancestors is the list of "is the i-th ancestor the LAST child of
  // its parent". When true at depth i, that column draws blank space;
  // when false, it draws "│ ". This produces standard ASCII tree
  // continuation lines.
  ancestors: ReadonlyArray<boolean>;
}

function CallFlowImpl({ onPick }: Props) {
  const anchorId = useStore(s => s.anchorId);
  const edgesBySrc = useStore(s => s.edgesBySrc);
  const nodes = useStore(s => s.nodes);
  const selectedId = useStore(s => s.selectedId);

  // Build the BFS-ordered flow. Re-computed when anchor / edges change.
  // We render the tree depth-first (so connectors visually connect a
  // parent to its descendants) but expand it one level at a time to
  // honour the per-depth cap and avoid exploding on hubs.
  const rows: ReadonlyArray<FlowRow> = useMemo(() => {
    if (!anchorId) return [];
    const visited = new Set<NodeId>([anchorId]);
    const out: FlowRow[] = [];

    const walk = (id: NodeId, depth: number, ancestors: ReadonlyArray<boolean>) => {
      // Push the anchor row at depth 0 with no connector (handled by
      // the renderer special-casing depth === 0).
      if (depth === 0) {
        out.push({ id, depth: 0, isLast: true, ancestors: [] });
      }
      if (depth >= DEPTH_MAX) return;
      const outs = edgesBySrc.get(id) ?? [];
      // Capture distinct callee ids in stable order, capped per depth.
      const callees: NodeId[] = [];
      const seen = new Set<NodeId>();
      for (const e of outs) {
        if (!CALL_EDGE_TYPES.has(e.type)) continue;
        if (seen.has(e.dst)) continue;
        if (visited.has(e.dst)) continue;
        seen.add(e.dst);
        callees.push(e.dst);
        if (callees.length >= PER_DEPTH_CAP) break;
      }
      for (let i = 0; i < callees.length; i++) {
        const cid = callees[i];
        const isLast = i === callees.length - 1;
        visited.add(cid);
        out.push({ id: cid, depth: depth + 1, isLast, ancestors });
        walk(cid, depth + 1, [...ancestors, isLast]);
      }
    };

    walk(anchorId, 0, []);
    return out;
  }, [anchorId, edgesBySrc]);

  // Render null when there's no anchor — the wrapper gets display:none
  // via the parent grid, but returning null avoids any DOM at all and
  // means the resize-observer (force-graph) doesn't see column-1 churn.
  if (!anchorId) return null;

  const anchorNode: GraphNode | undefined = nodes.get(anchorId);
  const anchorLabel = anchorNode?.qualified_name ?? anchorNode?.name ?? anchorId;

  return (
    <div className="call-flow">
      <div className="cf-header">
        <div className="cf-header-title">Call Flow</div>
        <div className="cf-header-anchor" title={anchorLabel}>{anchorLabel}</div>
      </div>
      <div className="cf-list">
        {rows.length === 0 && (
          <div className="cf-empty">
            No outbound calls within {DEPTH_MAX} hops.
          </div>
        )}
        {rows.map((row, idx) => {
          const n = nodes.get(row.id);
          const name = n?.name ?? row.id;
          const sig = n?.signature ?? '';
          const file = n?.file_path
            ? `${n.file_path}:${n.start_line ?? 0}`
            : '';
          // Connector: build "│ "/"  " columns for each ancestor depth,
          // then "├─ " / "└─ " for the last column. Anchor (depth 0)
          // renders without a connector.
          let connector = '';
          if (row.depth > 0) {
            for (const lastAncestor of row.ancestors) {
              connector += lastAncestor ? '   ' : '│  ';
            }
            connector += row.isLast ? '└─ ' : '├─ ';
          }
          return (
            <div
              key={`${row.id}-${idx}`}
              className={`cf-row${row.id === selectedId ? ' selected' : ''}`}
              onClick={() => onPick(row.id)}
              title={n?.qualified_name ?? row.id}
            >
              {connector && (
                <span className="cf-connector" aria-hidden="true">{connector}</span>
              )}
              <div className="cf-content">
                <div className="cf-name">
                  <span className="cf-type">[{n?.type ?? '?'}]</span> {name}
                </div>
                {sig && <div className="cf-sig">{sig}</div>}
                {file && <div className="cf-file">{file}</div>}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

export default memo(CallFlowImpl);
