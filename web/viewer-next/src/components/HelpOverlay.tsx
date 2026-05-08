'use client';

import { useEffect, useRef } from 'react';

interface Props {
  open: boolean;
  onClose: () => void;
}

const SECTIONS: Array<{ title: string; rows: Array<[string, string]> }> = [
  {
    title: 'Navigation',
    rows: [
      ['[', 'depth out'],
      [']', 'depth in'],
      ['Backspace', 'go back to previous state'],
      ['Home', 'reset to root'],
      ['+  /  =', 'zoom in'],
      ['-', 'zoom out'],
      ['0', 'zoom reset'],
    ],
  },
  {
    title: 'Trace & Filter',
    rows: [
      ['t', 'cycle trace direction'],
      ['1 – 4', 'set trace depth'],
      ['m', 'toggle colour mode'],
      ['v', 'toggle 2D / 3D'],
    ],
  },
  {
    title: 'Search',
    rows: [
      ['/', 'focus search box'],
      ['Esc', 'blur search / close this'],
    ],
  },
  {
    title: 'Help',
    rows: [
      ['?', 'toggle this overlay'],
    ],
  },
];

export default function HelpOverlay({ open, onClose }: Props) {
  const panelRef = useRef<HTMLDivElement>(null);

  // Auto-focus the panel when opened so Escape key works without re-tabbing.
  useEffect(() => {
    if (open) {
      panelRef.current?.focus();
    }
  }, [open]);

  if (!open) return null;

  const handleBackdropClick = (e: React.MouseEvent<HTMLDivElement>) => {
    if (e.target === e.currentTarget) onClose();
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Escape') onClose();
  };

  return (
    <div
      className="help-overlay-backdrop"
      onClick={handleBackdropClick}
      onKeyDown={handleKeyDown}
    >
      <div
        ref={panelRef}
        className="help-overlay-panel"
        role="dialog"
        aria-modal="true"
        aria-label="Keyboard shortcuts"
        tabIndex={-1}
      >
        <div className="ho-header">
          <span className="ho-title">Keyboard Shortcuts</span>
          <button className="ho-close" onClick={onClose} title="Close (Esc)">✕</button>
        </div>
        {SECTIONS.map(section => (
          <div key={section.title} className="ho-section">
            <div className="ho-section-title">{section.title}</div>
            {section.rows.map(([key, desc]) => (
              <div key={key} className="ho-row">
                <kbd className="ho-key">{key}</kbd>
                <span className="ho-desc">{desc}</span>
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}
