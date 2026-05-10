'use client';

import type { EvidencePack } from '@/lib/api';

// EvidenceView renders an EvidencePack as a list of commits, each with
// its hunks (file_path + line range + colour-coded patch text). Lifted
// out of TicketIndex so other surfaces (NodeDetail drawer, search-result
// drawer) can reuse the same patch presentation without copy-paste.
//
// Patch lines are colour-coded by their unified-diff prefix:
//   '+' added (green), '-' removed (red), '@' hunk header (blue),
//   ' ' context (default), other (default). The coloured spans are
//   wrapped in a single <pre> so users can still copy the whole patch
//   verbatim — the styling is purely visual.
//
// No `compact` prop yet; if a future drawer surface needs a truncated
// preview, add the prop here so callers don't have to reach into the
// internals.

interface Props {
  pack: EvidencePack;
}

export default function EvidenceView({ pack }: Props) {
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
              <pre className="ticket-patches-pre">
                {renderPatchLines(hunk.patch_text)}
              </pre>
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}

// renderPatchLines splits the unified-diff body and wraps each line in
// a className-based span so CSS owns the colour mapping. Empty patches
// render an empty <pre> rather than a placeholder so the user still
// sees the file/line header for context.
function renderPatchLines(patch: string): React.ReactNode {
  if (!patch) return null;
  const lines = patch.split('\n');
  return lines.map((line, i) => {
    const cls = classifyDiffLine(line);
    // Trailing newline preserved so copy-paste of the rendered <pre>
    // matches the raw patch text byte-for-byte.
    const text = i === lines.length - 1 ? line : line + '\n';
    return <span key={i} className={cls}>{text}</span>;
  });
}

function classifyDiffLine(line: string): string {
  if (line.length === 0) return 'diff-context';
  switch (line[0]) {
    case '+':
      // '+++ b/file' header line — same prefix but file-level. Colour
      // the same as additions; the @@ line above gives the cue.
      return 'diff-add';
    case '-':
      return 'diff-del';
    case '@':
      return 'diff-hunk-header';
    default:
      return 'diff-context';
  }
}
