'use client';

// SideNav (R2) — left-edge icon-only column hosting placeholder
// navigation items. Always collapsed (52px wide); items show their
// label as a floating tooltip on hover. The expand mode was removed
// per user request — the sidebar is reserved real estate for future
// navigation, not a primary surface, so the icon-strip footprint is
// the right default and the toggle was redundant chrome.
//
// Features get added later by appending to ITEMS or wiring an item's
// onClick.

interface NavItem {
  id: string;
  icon: string;
  label: string;
  onClick?: () => void;
  disabled?: boolean;
}

// Placeholder menu. Each entry has a single-char glyph so the 52px
// column never wraps. Add real handlers as features land.
const ITEMS: NavItem[] = [
  { id: 'search',   icon: '🔍', label: 'Search' },
  { id: 'filters',  icon: '⚙',  label: 'Filters' },
  { id: 'saved',    icon: '★',  label: 'Saved Views' },
  { id: 'settings', icon: '⋯',  label: 'Settings' },
];

export default function SideNav() {
  return (
    <nav className="side-nav" aria-label="Main navigation">
      <ul className="side-nav-items">
        {ITEMS.map(item => (
          <li key={item.id} className="side-nav-item">
            <button
              type="button"
              className="side-nav-btn"
              onClick={item.onClick}
              disabled={item.disabled}
              aria-label={item.label}>
              <span className="side-nav-icon" aria-hidden="true">{item.icon}</span>
            </button>
            {/* Tooltip rendered as a sibling of the button so absolute
                positioning can float it outside the column. */}
            <span className="side-nav-tooltip" role="tooltip">{item.label}</span>
          </li>
        ))}
      </ul>
    </nav>
  );
}
