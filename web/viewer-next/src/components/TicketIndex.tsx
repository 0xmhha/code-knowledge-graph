'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import { useStore } from '@/store/store';
import EvidenceView from '@/components/EvidenceView';
import { usePersistedBool } from '@/lib/usePersistedState';
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
  // collapsed defaults to `true` on SSR + first client render so the
  // static-export HTML matches the hydrated DOM (no React #418).
  // The hook then reads localStorage on mount and flips state if the
  // user's last session had the panel expanded.
  const [collapsed, setCollapsed] = usePersistedBool(STORAGE_KEY_COLLAPSED, true);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [rows, setRows] = useState<TicketRow[] | null>(null);
  const [openTicket, setOpenTicket] = useState<string | null>(null);
  // Per-ticket evidence cache. Keyed by issue_id; null = loading,
  // missing key = not yet requested. The cache is intentionally
  // un-bounded — a single user session is unlikely to expand more
  // than a handful of tickets, and re-fetching when the user
  // collapses + re-expands feels more wasteful than the memory cost.
  // Subsequent "Load more" clicks call /api/evidence?offset=… and
  // append the returned hits to this same EvidencePack object so the
  // EvidenceView always shows the user's full traversal.
  const [packs, setPacks] = useState<Record<string, EvidencePack | null | string>>({});
  // ended[id] = true when the last loadMore returned 0 commits — the
  // server has no more pages for this ticket, so the button hides.
  const [ended, setEnded] = useState<Set<string>>(() => new Set());
  // loadingMore[id] = true while a paging fetch is in flight (separate
  // from initial-load null because the existing pack should keep
  // rendering during the fetch).
  const [loadingMore, setLoadingMore] = useState<Set<string>>(() => new Set());
  // Cross-panel jump signal from NodeDetail's H4 issue pills. Subscribed
  // here (rather than read inside an effect) so TanStack-style
  // selectors recompute when the store changes.
  const selectedIssueID = useStore(s => s.selectedIssueID);
  const setSelectedIssueID = useStore(s => s.setSelectedIssueID);
  // bodyRef wraps the .ticket-body so the auto-expand path can
  // querySelector the row inside *this* panel rather than the document
  // (multi-panel rendering would otherwise scroll to the wrong row).
  const bodyRef = useRef<HTMLDivElement | null>(null);

  const toggle = useCallback(() => {
    setCollapsed(!collapsed);
  }, [collapsed, setCollapsed]);

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
      // No hits at all → mark ended so the Load more button never appears.
      if (!pack.hits || pack.hits.length === 0) {
        setEnded(prev => new Set(prev).add(issueID));
      }
    } catch (e) {
      setPacks(prev => ({ ...prev, [issueID]: e instanceof Error ? e.message : String(e) }));
    }
  }, [api, packs]);

  // loadMore fetches the next page (offset = current commit count) and
  // appends hits to the existing pack. Stops registering as a "next
  // page available" once the server returns 0 new commits.
  const loadMore = useCallback(async (issueID: string) => {
    if (!api) return;
    const cur = packs[issueID];
    if (!cur || typeof cur !== 'object') return;
    if (ended.has(issueID) || loadingMore.has(issueID)) return;
    const offset = cur.hits.length;
    setLoadingMore(prev => new Set(prev).add(issueID));
    try {
      const next = await api.evidence({ issueID, k: 20, budgetTokens: 12000, offset });
      if (!next.hits || next.hits.length === 0) {
        setEnded(prev => new Set(prev).add(issueID));
        return;
      }
      setPacks(prev => {
        const existing = prev[issueID];
        if (!existing || typeof existing !== 'object') return prev;
        return {
          ...prev,
          [issueID]: { ...existing, hits: existing.hits.concat(next.hits) },
        };
      });
    } catch (e) {
      // Non-fatal — keep existing pack visible, surface the failure
      // through the patch error placeholder so the user can retry.
      setPacks(prev => ({ ...prev, [issueID]: e instanceof Error ? e.message : String(e) }));
    } finally {
      setLoadingMore(prev => {
        const n = new Set(prev);
        n.delete(issueID);
        return n;
      });
    }
  }, [api, packs, ended, loadingMore]);

  // Cross-panel jump: when NodeDetail's H4 pill stages selectedIssueID,
  // force-expand the panel + open the matching row + auto-load
  // patches + scroll the row into view. Two-phase because rows are
  // lazy-loaded on first expand: phase 1 expands the panel (which
  // triggers the rows-fetch effect); phase 2 runs once rows arrive.
  useEffect(() => {
    if (!selectedIssueID) return;
    if (collapsed) {
      setCollapsed(false);
    }
  }, [selectedIssueID, collapsed, setCollapsed]);

  useEffect(() => {
    if (!selectedIssueID || !rows) return;
    // Tickets with no matching row (e.g. an old graph that doesn't
    // index this issue) silently no-op rather than scrolling to top.
    const exists = rows.some(r => r.issue_id === selectedIssueID);
    if (!exists) {
      setSelectedIssueID(null);
      return;
    }
    setOpenTicket(selectedIssueID);
    void loadPatches(selectedIssueID);
    const target = bodyRef.current?.querySelector(`[data-ticket-id="${CSS.escape(selectedIssueID)}"]`);
    if (target instanceof HTMLElement) {
      target.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    }
    // Clear the signal so a second click on the same pill re-fires
    // (otherwise the unchanged value would skip this effect).
    setSelectedIssueID(null);
  }, [selectedIssueID, rows, loadPatches, setSelectedIssueID]);

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
        <div className="ticket-body" ref={bodyRef}>
          {loading && <div className="ticket-loading">loading…</div>}
          {error && <div className="ticket-error">error: {error}</div>}
          {rows && rows.length > 0 && (
            <ul className="ticket-list">
              {rows.map(row => {
                const isOpen = openTicket === row.issue_id;
                return (
                  <li key={row.issue_id} className={`ticket-row ${isOpen ? 'open' : ''}`} data-ticket-id={row.issue_id}>
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
                                <div className="ticket-commit-line">
                                  <span className="ticket-sha">{c.sha.slice(0, 12)}</span>
                                  <span className="ticket-subject">{c.subject}</span>
                                </div>
                                {c.top_files && c.top_files.length > 0 && (
                                  <div className="ticket-commit-files">
                                    {c.top_files.map(f => (
                                      // title surfaces the full dirname when
                                      // the pill's max-width clips it (deep
                                      // paths like crypto/secp256k1/libsecp256k1/include).
                                      <span key={f} className="ticket-file-pill" title={f}>{f}</span>
                                    ))}
                                  </div>
                                )}
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
                          <>
                            <EvidenceView pack={packs[row.issue_id] as EvidencePack} />
                            {!ended.has(row.issue_id) && (
                              <button className="ticket-patches-button"
                                onClick={() => loadMore(row.issue_id)}
                                disabled={loadingMore.has(row.issue_id)}
                                aria-label={`Load more patches for ${row.issue_id}`}>
                                {loadingMore.has(row.issue_id) ? '… loading' : '▾ load more'}
                              </button>
                            )}
                          </>
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

