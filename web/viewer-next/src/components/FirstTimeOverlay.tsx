'use client';

import { useStore } from '@/store/store';

const STORAGE_KEY = 'ckg.firstTimeSeen';

// HINTS is the four-line greeting shown the first time a user lands on
// the viewer. Co-located here so a future copy edit stays in one place.
// Order is "what to look at" → "what to interact with" → "what to type"
// → "how to get back" — i.e. the journey of first navigation.
const HINTS: Array<[string, string]> = [
  ['Click', 'a node to see its connections'],
  ['Pills', 'use the 6 graph pills to toggle edge types'],
  ['Search', 'press / to search, ? for help'],
  ['Home', '🏠 to return to root view'],
];

// FirstTimeOverlay is the lightweight onboarding card shown once per
// browser. The user dismisses it by clicking anywhere (the backdrop OR
// the panel itself); we persist firstTimeSeen=true so the overlay never
// reappears on the same machine. firstTimeSeen hydrates synchronously
// from localStorage in store.ts (initFirstTimeSeen), so no useEffect is
// needed here — first paint already reflects the persisted state.
export default function FirstTimeOverlay() {
  const seen = useStore(s => s.firstTimeSeen);
  const setSeen = useStore(s => s.setFirstTimeSeen);

  if (seen) return null;

  const dismiss = () => {
    try {
      if (typeof localStorage !== 'undefined') {
        localStorage.setItem(STORAGE_KEY, '1');
      }
    } catch { /* localStorage may be blocked */ }
    setSeen(true);
  };

  return (
    <div
      className="first-time-overlay"
      onClick={dismiss}
      role="dialog"
      aria-modal="true"
      aria-label="Welcome — first time hints"
    >
      <div
        className="first-time-card"
        // Stop propagation so a click *inside* the card still dismisses
        // (we want any click anywhere to count) — but we explicitly do
        // NOT call ev.stopPropagation here. The backdrop handler runs
        // either way; this onClick is just for keyboard parity.
        onClick={dismiss}
      >
        <div className="first-time-title">Welcome to ckg viewer</div>
        <div className="first-time-subtitle">
          A code knowledge graph — explore your codebase as a network of
          symbols, types, and call relationships.
        </div>
        <ul className="first-time-list">
          {HINTS.map(([key, desc]) => (
            <li key={key} className="first-time-item">
              <span className="first-time-key">{key}</span>
              <span className="first-time-desc">{desc}</span>
            </li>
          ))}
        </ul>
        <div className="first-time-dismiss">click anywhere to dismiss</div>
      </div>
    </div>
  );
}
