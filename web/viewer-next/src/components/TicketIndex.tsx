'use client';

import { useCallback, useEffect, useState } from 'react';
import type { IAPI, TicketRow } from '@/lib/api';

// TicketIndex surfaces the H4 issue-id rollup — every issue/PR ID
// the H4 extractor recognised in commit subjects, sorted by how many
// hunks cite it. Click an entry to expand the most-recent commit
// subjects under it (decorating signal that's helpful for
// "what tickets does this codebase work on most" exploration without
// a round-trip to GitHub).
//
// Hidden when the graph has no Hunks with `issues:` doc_comment
// (a fresh repo, a build with H4 disabled, or static-export mode).

const STORAGE_KEY_COLLAPSED = 'ckg.ticketIndexCollapsed';

interface Props {
  api: IAPI | null;
}

export default function TicketIndex({ api }: Props) {
  const [collapsed, setCollapsed] = useState<boolean>(() => {
    if (typeof localStorage === 'undefined') return true;
    return localStorage.getItem(STORAGE_KEY_COLLAPSED) !== '0';
  });
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [rows, setRows] = useState<TicketRow[] | null>(null);
  const [openTicket, setOpenTicket] = useState<string | null>(null);

  const toggle = useCallback(() => {
    setCollapsed(prev => {
      const next = !prev;
      try { localStorage.setItem(STORAGE_KEY_COLLAPSED, next ? '1' : '0'); } catch { /* ignore */ }
      return next;
    });
  }, []);

  useEffect(() => {
    if (collapsed || rows !== null || !api || loading) return;
    setLoading(true);
    api.tickets(100)
      .then(setRows)
      .catch(e => setError(e instanceof Error ? e.message : String(e)))
      .finally(() => setLoading(false));
  }, [collapsed, rows, api, loading]);

  // Collapsed state is rendered even on empty graphs (so the user can
  // always toggle to confirm). Once expanded and we know there are
  // zero tickets, hide the body to free vertical space.
  if (!collapsed && rows !== null && rows.length === 0) {
    return null;
  }

  return (
    <div className="ticket-index" data-ticket-index="true">
      <button className="ticket-header" onClick={toggle}
        aria-expanded={!collapsed}
        aria-label="Toggle Ticket index panel">
        <span className="ticket-arrow">{collapsed ? '▶' : '▼'}</span>
        <span className="ticket-title">🎫 TICKETS</span>
        <span className="ticket-meta">
          {rows ? `${rows.length} tickets` : '…'}
        </span>
      </button>
      {!collapsed && (
        <div className="ticket-body">
          {loading && <div className="ticket-loading">loading…</div>}
          {error && <div className="ticket-error">error: {error}</div>}
          {rows && rows.length > 0 && (
            <ul className="ticket-list">
              {rows.map(row => {
                const isOpen = openTicket === row.issue_id;
                return (
                  <li key={row.issue_id} className={`ticket-row ${isOpen ? 'open' : ''}`}>
                    <button className="ticket-row-button"
                      onClick={() => setOpenTicket(isOpen ? null : row.issue_id)}
                      aria-expanded={isOpen}>
                      <span className="ticket-id">{row.issue_id}</span>
                      <span className="ticket-counts">
                        {row.hunk_count}h / {row.commit_count}c
                      </span>
                    </button>
                    {isOpen && row.sample_commits && row.sample_commits.length > 0 && (
                      <ul className="ticket-commits">
                        {row.sample_commits.map(c => (
                          <li key={c.sha} className="ticket-commit">
                            <span className="ticket-sha">{c.sha.slice(0, 12)}</span>
                            <span className="ticket-subject">{c.subject}</span>
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
