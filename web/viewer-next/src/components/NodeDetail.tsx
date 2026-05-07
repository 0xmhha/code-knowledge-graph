'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { useStore } from '@/store/store';
import { traceFromNode } from '@/lib/trace';
import type { IAPI, ImpactResult, ImpactNode, ImpactBuckets } from '@/lib/api';
import type { NodeId } from '@/types';

interface Props { api: IAPI; }

// IMPACT_GROUPS is the canonical display order for the six buckets returned
// by /api/impact. We keep it co-located with the rendering so a future
// reordering only touches this file. Labels match the bucket keys 1:1 with
// human-readable descriptions for the section headings.
const IMPACT_GROUPS: Array<{ key: keyof ImpactBuckets; label: string }> = [
  { key: 'callers', label: 'Callers' },
  { key: 'interface_impact', label: 'Interface impact' },
  { key: 'type_users', label: 'Type users' },
  { key: 'distributed', label: 'Distributed' },
  { key: 'concurrent', label: 'Concurrent' },
  { key: 'other_refs', label: 'Other refs' },
];

// IMPACT_TOP_N caps how many items per bucket we render in the panel —
// surfacing 100+ items at the panel's 240px clamp floor buries the rest
// of the UI. The number lines up with what an LLM finds useful (~10 items
// per bucket per the dogfood plan); users wanting deeper traversal use
// the MCP tool.
const IMPACT_TOP_N = 10;

export default function NodeDetail({ api }: Props) {
  const selectedId = useStore(s => s.selectedId);
  const nodes = useStore(s => s.nodes);
  const edgesBySrc = useStore(s => s.edgesBySrc);
  const edgesByDst = useStore(s => s.edgesByDst);
  const setSelected = useStore(s => s.setSelected);
  const setAnchor = useStore(s => s.setAnchor);
  const commit = useStore(s => s.commit);
  // Derive via useMemo: returning a fresh array literal from a useStore
  // selector defeats Object.is equality and causes a render loop
  // (React error #185). Same pitfall as GraphCanvas.graphData.
  const node = useMemo(
    () => (selectedId ? nodes.get(selectedId) ?? null : null),
    [selectedId, nodes],
  );
  const edges = useMemo(() => {
    if (!selectedId) return [];
    return (edgesBySrc.get(selectedId) ?? []).concat(edgesByDst.get(selectedId) ?? []);
  }, [selectedId, edgesBySrc, edgesByDst]);
  const [blob, setBlob] = useState<string>('');

  // Impact panel state. Co-located here because a single node can show
  // both its blob AND its impact result; lifting it would force every
  // navigation to re-fetch.
  const [impact, setImpact] = useState<ImpactResult | null>(null);
  const [impactLoading, setImpactLoading] = useState(false);
  const [impactError, setImpactError] = useState<string | null>(null);

  useEffect(() => {
    setBlob('');
    // Selection change resets impact too — the previous result belongs
    // to a different seed and showing it would mislead the user.
    setImpact(null);
    setImpactError(null);
    if (!selectedId) return;
    let cancelled = false;
    api.blob(selectedId).then(t => {
      if (!cancelled) setBlob(t || '(no source blob — non-leaf node)');
    }).catch(() => {
      if (!cancelled) setBlob('(blob fetch failed)');
    });
    return () => { cancelled = true; };
  }, [selectedId, api]);

  // Mirrors App.traceAndCommit semantics so the canvas actually shows
  // the clicked impact node and its 1-hop neighbours — without this the
  // detail panel updated but the canvas stayed on the original seed,
  // confusing the user. Depth=1 because the impact list already supplied
  // multi-hop context; a deeper trace would just clutter the view.
  //
  // MUST be declared before the `if (!node) return ...` early-return below
  // — useCallback is a hook, and React requires the same hook order on
  // every render. Putting it after the early return triggered React error
  // #310 ("Rendered fewer hooks than expected") whenever the user clicked
  // a node, because the unselected-state render skipped this hook entirely.
  const onImpactItemClick = useCallback(async (id: NodeId) => {
    setSelected(id);
    const target = useStore.getState().nodes.get(id);
    if (!target?.qualified_name) {
      // Node lacks qname, trace would yield 0 results — fall back to
      // selection-only so the detail pane still updates.
      return;
    }
    const s = useStore.getState();
    const g = await traceFromNode(api, id, {
      direction: s.traceDirection,
      depth: 1,
      edgeTypes: s.edgeTypeWhitelist,
    });
    setAnchor(id, 1);
    commit(g);
  }, [api, setSelected, setAnchor, commit]);

  if (!node) {
    // Strong empty-state placeholder. The earlier single muted line read
    // as "panel is broken" on first paint because NodeDetail is the
    // tallest section in the panel and starts unselected. Centered
    // glyph + heading + hint gives the empty state unmistakable
    // presence, and role="status" + aria-live="polite" lets screen
    // readers announce the absence rather than silently skip past.
    return (
      <div className="node-detail">
        <div
          className="node-detail-empty"
          role="status"
          aria-live="polite"
        >
          <div className="node-detail-empty-glyph" aria-hidden="true">🔍</div>
          <h3 className="node-detail-empty-title">No node selected</h3>
          <p className="node-detail-empty-hint">
            Click a node in the canvas, or pick one from the Visible Nodes
            list above, to inspect its qualified name, source, edges, and
            reverse-dependency impact.
          </p>
        </div>
      </div>
    );
  }

  const inN = edges.filter(e => e.dst === node.id).length;
  const outN = edges.filter(e => e.src === node.id).length;

  const onImpactClick = async () => {
    if (!node.qualified_name) {
      setImpactError('Node has no qualified_name; impact lookup needs a qname.');
      return;
    }
    setImpactLoading(true);
    setImpactError(null);
    try {
      const r = await api.impact(node.qualified_name, 2);
      setImpact(r);
    } catch (e) {
      setImpactError(e instanceof Error ? e.message : String(e));
    } finally {
      setImpactLoading(false);
    }
  };

  return (
    <div className="node-detail">
      <h3>{node.name}</h3>
      <div><strong>Type:</strong> {node.type}</div>
      <div>
        <strong>Qualified:</strong>{' '}
        <span style={{ fontFamily: 'ui-monospace,monospace', fontSize: 11, wordBreak: 'break-all' }}>
          {node.qualified_name}
        </span>
      </div>
      <div><strong>File:</strong> {node.file_path}:{node.start_line}</div>
      <div><strong>Lang:</strong> {node.language ?? ''} · <strong>Conf:</strong> {node.confidence ?? ''}</div>
      <div>
        <strong>Usage:</strong> {(node.usage_score ?? 0).toFixed(2)}
        {' · '}
        <strong>PR:</strong> {(node.pagerank ?? 0).toExponential(2)}
      </div>
      <div><strong>Edges:</strong> in {inN} · out {outN}</div>
      {node.community_id != null && (
        <div><strong>Community:</strong> {node.community_id}{node.topic_label ? ` · ${node.topic_label}` : ''}</div>
      )}
      {node.signature && (
        <div style={{ marginTop: 6, color: '#9ad', fontStyle: 'italic',
                      fontFamily: 'ui-monospace,monospace', fontSize: 11 }}>
          {node.signature}
        </div>
      )}

      <div className="impact-actions">
        <button
          type="button"
          className="impact-btn"
          onClick={onImpactClick}
          disabled={impactLoading || !node.qualified_name}
          title="Reverse-dependency closure (depth=2): who would need to be examined if this node changes?"
        >
          {impactLoading ? '🔍 Loading…' : '🔍 Impact'}
        </button>
      </div>

      {impactError && (
        <div className="impact-error">⚠️ {impactError}</div>
      )}

      {impact && <ImpactPanel result={impact} onItemClick={onImpactItemClick} />}

      <h4>Source</h4>
      <pre style={{
        whiteSpace: 'pre-wrap', maxHeight: 200, overflow: 'auto',
        background: '#0d0e10', padding: 6, border: '1px solid #2a2c30',
      }}>{blob || 'loading…'}</pre>
    </div>
  );
}

interface ImpactPanelProps {
  result: ImpactResult;
  onItemClick: (id: NodeId) => void;
}

function ImpactPanel({ result, onItemClick }: ImpactPanelProps) {
  if (result.not_found) {
    return (
      <div className="impact-panel">
        <h4>Impact</h4>
        <div className="impact-empty">No impact found for this seed.</div>
      </div>
    );
  }
  const buckets = result.impact;
  const totals = result.totals?.by_group ?? {};
  const warnings = result.metadata?.warnings ?? [];

  return (
    <div className="impact-panel">
      <h4>Impact (depth {result.depth ?? 2})</h4>
      <div className="impact-totals">
        nodes {result.totals?.nodes ?? 0} · edges {result.totals?.edges ?? 0}
      </div>
      {IMPACT_GROUPS.map(({ key, label }) => {
        const items = buckets?.[key] ?? [];
        const count = totals[key] ?? items.length;
        if (items.length === 0) return null;
        const visible = items.slice(0, IMPACT_TOP_N);
        const hidden = items.length - visible.length;
        return (
          <div key={key} className="impact-group">
            <div className="impact-group-header">
              <span className="impact-group-label">{label}</span>
              <span className="impact-group-count">{count}</span>
            </div>
            <ul className="impact-list">
              {visible.map(n => (
                <ImpactItem key={n.id} node={n} onClick={() => onItemClick(n.id)} />
              ))}
            </ul>
            {hidden > 0 && (
              <div className="impact-more">+{hidden} more (use MCP impact_of_change for full list)</div>
            )}
          </div>
        );
      })}
      {warnings.length > 0 && (
        <div className="impact-warnings">
          <div className="impact-warnings-title">⚠️ {warnings.length} warning(s)</div>
          {warnings.slice(0, 5).map((w, i) => (
            <div key={i} className="impact-warning-row">
              <span className="impact-warning-code">{w.code ?? 'warn'}</span>
              <span className="impact-warning-qname">{w.qname || w.node_id}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

interface ImpactItemProps {
  node: ImpactNode;
  onClick: () => void;
}

function ImpactItem({ node, onClick }: ImpactItemProps) {
  return (
    <li className="impact-item" onClick={onClick} title={node.qname ?? ''}>
      <div className="impact-item-head">
        <span className="impact-item-name">{node.name ?? node.id}</span>
        {node.type && <span className="impact-item-type"> · {node.type}</span>}
      </div>
      {node.citation && (
        <div className="impact-item-cite">{node.citation}</div>
      )}
    </li>
  );
}
