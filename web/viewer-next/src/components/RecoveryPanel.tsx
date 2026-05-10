'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { usePersistedBool } from '@/lib/usePersistedState';
import type { IAPI } from '@/lib/api';
import type { GraphNode } from '@/types';

// RecoveryPanel surfaces the §11.3 unreachable-history track —
// AMBIGUOUS-confidence Commits (force-pushed-away SHAs) and the
// Hunks they introduced. Deliberately separated from the main
// NodeList / NodeDetail flow because:
//
//   1. The §11.3 contract hides AMBIGUOUS Hunk/Commit from LLM-bound
//      surfaces (MCP tools, future /api/evidence). The viewer is the
//      designated "human can deliberately consult this" surface.
//   2. Mixing them into the canvas would noise up the boot view; this
//      panel is opt-in and collapsed by default.
//   3. Visual treatment uses an amber/red accent so the operator
//      always knows they're looking at history that was rolled back.
//
// On graphs without any AMBIGUOUS Hunk/Commit rows the panel renders
// nothing (no header, no empty-state) so it doesn't waste vertical
// space in the panel column.

const STORAGE_KEY_COLLAPSED = 'ckg.recoveryCollapsed';

interface Props {
  api: IAPI | null;
}

interface CommitGroup {
  commit: GraphNode;
  hunks: GraphNode[];
}

export default function RecoveryPanel({ api }: Props) {
  // collapsed defaults `true` on SSR + first render to keep static
  // export HTML aligned with the hydrated DOM (avoids React #418).
  const [collapsed, setCollapsed] = usePersistedBool(STORAGE_KEY_COLLAPSED, true);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [nodes, setNodes] = useState<GraphNode[] | null>(null);
  const [openCommit, setOpenCommit] = useState<string | null>(null);
  const [blobs, setBlobs] = useState<Map<string, string>>(new Map());

  const toggle = useCallback(() => {
    setCollapsed(!collapsed);
  }, [collapsed, setCollapsed]);

  // Lazy-fetch on first expand so the panel costs nothing on graphs the
  // operator never opens it on.
  useEffect(() => {
    if (collapsed || nodes !== null || !api || loading) return;
    setLoading(true);
    api.ambiguousNodes()
      .then(ns => setNodes(ns))
      .catch(e => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setLoading(false));
  }, [collapsed, nodes, api, loading]);

  // Group hunks under their parent commit. The commit→hunk relationship
  // lives in `has_hunk` edges, but we don't need to fetch edges here —
  // hunk QualifiedName encodes the parent SHA as `hunk:<sha>:<file>:<idx>`,
  // so a single string-prefix match groups them client-side.
  const groups: CommitGroup[] = useMemo(() => {
    if (!nodes) return [];
    const commits = nodes.filter(n => n.type === 'Commit');
    const hunks = nodes.filter(n => n.type === 'Hunk');
    return commits.map(commit => {
      const qn = commit.qualified_name ?? '';
      const sha = qn.replace(/^commit:/, '');
      const matched = sha
        ? hunks.filter(h => (h.qualified_name ?? '').startsWith(`hunk:${sha}:`))
        : [];
      return { commit, hunks: matched };
    }).sort((a, b) => {
      // Sort by signature timestamp (Commit.signature is "<unix>: <subject>").
      const ts = (n: GraphNode) => parseInt((n.signature ?? '').split(':')[0], 10) || 0;
      return ts(b.commit) - ts(a.commit);
    });
  }, [nodes]);

  const onPickHunk = useCallback(async (hunk: GraphNode) => {
    if (!api || blobs.has(hunk.id)) return;
    try {
      const text = await api.blob(hunk.id);
      setBlobs(prev => {
        const next = new Map(prev);
        next.set(hunk.id, text);
        return next;
      });
    } catch { /* leave the row empty */ }
  }, [api, blobs]);

  // Don't render at all when there are no AMBIGUOUS rows. Avoid a
  // visual placeholder for the (common) case where the graph was
  // built on a clean repo with no force-pushed history.
  if (!collapsed && nodes !== null && nodes.length === 0) {
    return null;
  }

  return (
    <div className="recovery-panel" data-recovery-panel="true">
      <button className="recovery-header" onClick={toggle}
        aria-expanded={!collapsed}
        aria-label="Toggle Recovery panel">
        <span className="recovery-arrow">{collapsed ? '▶' : '▼'}</span>
        <span className="recovery-title">⚠ RECOVERY</span>
        <span className="recovery-meta">
          {nodes ? `${groups.length} commits / ${nodes.filter(n => n.type === 'Hunk').length} hunks` : '…'}
        </span>
      </button>
      {!collapsed && (
        <div className="recovery-body">
          <div className="recovery-disclaimer">
            Force-pushed-away history (confidence=AMBIGUOUS). Hidden from
            LLM tools per §11.3 — visible here so a human can recover code
            an agent overwrote.
          </div>
          {loading && <div className="recovery-loading">loading…</div>}
          {error && <div className="recovery-error">error: {error}</div>}
          {nodes && nodes.length > 0 && (
            <ul className="recovery-list">
              {groups.map(g => {
                const sha = (g.commit.qualified_name ?? '').replace(/^commit:/, '').slice(0, 12);
                const subject = (g.commit.signature ?? '').split(':').slice(1).join(':').trim();
                const isOpen = openCommit === g.commit.id;
                return (
                  <li key={g.commit.id} className={`recovery-commit ${isOpen ? 'open' : ''}`}>
                    <button className="recovery-commit-row"
                      onClick={() => setOpenCommit(isOpen ? null : g.commit.id)}
                      aria-expanded={isOpen}>
                      <span className="recovery-sha">{sha}</span>
                      <span className="recovery-subject">{subject || '(no subject)'}</span>
                      <span className="recovery-hunk-count">{g.hunks.length}h</span>
                    </button>
                    {isOpen && (
                      <ul className="recovery-hunks">
                        {g.hunks.length === 0 && (
                          <li className="recovery-empty">(no hunks captured)</li>
                        )}
                        {g.hunks.map(h => (
                          <li key={h.id} className="recovery-hunk">
                            <button className="recovery-hunk-row"
                              onClick={() => onPickHunk(h)}
                              aria-label={`Show patch for ${h.file_path}`}>
                              <span className="recovery-file">{h.file_path}</span>
                              <span className="recovery-line">L{h.start_line}</span>
                            </button>
                            {blobs.has(h.id) && (
                              <pre className="recovery-blob">
                                {blobs.get(h.id)}
                              </pre>
                            )}
                          </li>
                        ))}
                      </ul>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}
