'use client';

import { useEffect, useState } from 'react';
import { useStore } from '@/store/store';
import type { IAPI } from '@/lib/api';

interface Props { api: IAPI; }

export default function NodeDetail({ api }: Props) {
  const selectedId = useStore(s => s.selectedId);
  const node = useStore(s => (selectedId ? s.nodes.get(selectedId) : null));
  const edges = useStore(s => {
    if (!selectedId) return [];
    return (s.edgesBySrc.get(selectedId) ?? []).concat(s.edgesByDst.get(selectedId) ?? []);
  });
  const [blob, setBlob] = useState<string>('');

  useEffect(() => {
    setBlob('');
    if (!selectedId) return;
    let cancelled = false;
    api.blob(selectedId).then(t => {
      if (!cancelled) setBlob(t || '(no source blob — non-leaf node)');
    }).catch(() => {
      if (!cancelled) setBlob('(blob fetch failed)');
    });
    return () => { cancelled = true; };
  }, [selectedId, api]);

  if (!node) {
    return <div className="node-detail">Select a node to inspect.</div>;
  }

  const inN = edges.filter(e => e.dst === node.id).length;
  const outN = edges.filter(e => e.src === node.id).length;

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
      <h4>Source</h4>
      <pre style={{
        whiteSpace: 'pre-wrap', maxHeight: 200, overflow: 'auto',
        background: '#0d0e10', padding: 6, border: '1px solid #2a2c30',
      }}>{blob || 'loading…'}</pre>
    </div>
  );
}
