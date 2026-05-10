'use client';

import { useCallback, useEffect, useState } from 'react';
import type { IAPI, TicketRow, EvidencePack } from '@/lib/api';

// TicketIndex surfaces the H4 issue-id rollup — every issue/PR ID
// the H4 extractor recognised in commit subjects, sorted by how many
// hunks cite it. Click an entry to expand the most-recent commit
// subjects under it (decorating signal that's helpful for
// "what tickets does this codebase work on most" exploration without
// a round-trip to GitHub). The "patches" button on each row launches
// /api/evidence?issue_id=… and inlines the resulting EvidencePack so
// the user can read the actual hunks without bouncing to git.
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
  // Per-ticket evidence cache. Keyed by issue_id; null = loading,
  // missing key = not yet requested. The cache is intentionally
  // un-bounded — a single user session is unlikely to expand more
  // than a handful of tickets, and re-fetching when the user
  // collapses + re-expands feels more wasteful than the memory cost.
  const [packs, setPacks] = useState<Record<string, EvidencePack | null | string>>({});

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

  // loadPatches is called when the user clicks "patches" on a ticket
  // row. It fetches /api/evidence?issue_id=X — IssueID-only mode
  // means the server returns the ticket's full footprint sorted by
  // recency (no BM25 needed since the user already specified what
  // they want to see).
  const loadPatches = useCallback(async (issueID: string) => {
    if (!api) return;
    if (packs[issueID]) return; // already loaded or loading
    setPacks(prev => ({ ...prev, [issueID]: null }));
    try {
      const pack = await api.evidence({ issueID, k: 20, budgetTokens: 12000 });
      setPacks(prev => ({ ...prev, [issueID]: pack }));
    } catch (e) {
      setPacks(prev => ({ ...prev, [issueID]: e instanceof Error ? e.message : String(e) }));
    }
  }, [api, packs]);

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
                    {isOpen && (
                      <div className="ticket-detail">
                        {row.sample_commits && row.sample_commits.length > 0 && (
                          <ul className="ticket-commits">
                            {row.sample_commits.map(c => (
                              <li key={c.sha} className="ticket-commit">
                                <span className="ticket-sha">{c.sha.slice(0, 12)}</span>
                                <span className="ticket-subject">{c.subject}</span>
                              </li>
                            ))}
                          </ul>
                        )}
                        {!packs[row.issue_id] && (
                          <button className="ticket-patches-button"
                            onClick={() => loadPatches(row.issue_id)}
                            aria-label={`Load patches for ${row.issue_id}`}>
                            ▸ patches
                          </button>
                        )}
                        {packs[row.issue_id] === null && (
                          <div className="ticket-patches-loading">loading patches…</div>
                        )}
                        {typeof packs[row.issue_id] === 'string' && (
                          <div className="ticket-patches-error">
                            evidence error: {packs[row.issue_id] as string}
                          </div>
                        )}
                        {packs[row.issue_id] && typeof packs[row.issue_id] === 'object' && (
                          <EvidenceView pack={packs[row.issue_id] as EvidencePack} />
                        )}
                      </div>
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

// EvidenceView renders one EvidencePack as a list of commits, each
// with its hunks (file_path + line range + patch_text in a <pre>).
// Kept inside this file because it has no other consumer; if the
// node-detail panel ever needs a similar surface, lift it out.
function EvidenceView({ pack }: { pack: EvidencePack }) {
  if (!pack.hits || pack.hits.length === 0) {
    return <div className="ticket-patches-empty">no patches found</div>;
  }
  return (
    <div className="ticket-patches">
      {pack.hits.map(hit => (
        <div key={hit.commit.sha} className="ticket-patches-commit">
          <div className="ticket-patches-commit-header">
            <span className="ticket-sha">{hit.commit.sha.slice(0, 12)}</span>
            <span className="ticket-subject">{hit.commit.subject}</span>
          </div>
          {hit.hunks.map(hunk => (
            <div key={hunk.id} className="ticket-patches-hunk">
              <div className="ticket-patches-hunk-header">
                <span className="ticket-patches-file">{hunk.file_path}</span>
                <span className="ticket-patches-lines">
                  L{hunk.start_line}-{hunk.end_line}
                </span>
              </div>
              <pre className="ticket-patches-pre">{hunk.patch_text}</pre>
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}
