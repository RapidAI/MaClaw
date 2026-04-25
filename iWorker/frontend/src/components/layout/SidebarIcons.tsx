/**
 * SVG line-style icons for iWorker sidebar navigation.
 * Inspired by WorkBuddy's clean, minimal icon design.
 */

const iconBase: React.CSSProperties = {
  width: '18px',
  height: '18px',
  strokeWidth: 1.8,
  stroke: 'currentColor',
  fill: 'none',
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
};

export function IconHome({ style }: { style?: React.CSSProperties }) {
  return (
    <svg viewBox="0 0 24 24" style={{ ...iconBase, ...style }}>
      <path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
      <polyline points="9 22 9 12 15 12 15 22" />
    </svg>
  );
}

export function IconNewTask({ style }: { style?: React.CSSProperties }) {
  return (
    <svg viewBox="0 0 24 24" style={{ ...iconBase, ...style }}>
      <path d="M12 20h9" />
      <path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z" />
    </svg>
  );
}

export function IconClaw({ style }: { style?: React.CSSProperties }) {
  return (
    <svg viewBox="0 0 24 24" style={{ ...iconBase, ...style }}>
      <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
      <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
    </svg>
  );
}

export function IconExpert({ style }: { style?: React.CSSProperties }) {
  return (
    <svg viewBox="0 0 24 24" style={{ ...iconBase, ...style }}>
      <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" />
      <circle cx="12" cy="7" r="4" />
    </svg>
  );
}

export function IconSkills({ style }: { style?: React.CSSProperties }) {
  return (
    <svg viewBox="0 0 24 24" style={{ ...iconBase, ...style }}>
      <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
    </svg>
  );
}

export function IconHistory({ style }: { style?: React.CSSProperties }) {
  return (
    <svg viewBox="0 0 24 24" style={{ ...iconBase, ...style }}>
      <circle cx="12" cy="12" r="10" />
      <polyline points="12 6 12 12 16 14" />
    </svg>
  );
}

export function IconSettings({ style }: { style?: React.CSSProperties }) {
  return (
    <svg viewBox="0 0 24 24" style={{ ...iconBase, ...style }}>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </svg>
  );
}

export function IconSearch({ style }: { style?: React.CSSProperties }) {
  return (
    <svg viewBox="0 0 24 24" style={{ ...iconBase, width: '14px', height: '14px', ...style }}>
      <circle cx="11" cy="11" r="8" />
      <line x1="21" y1="21" x2="16.65" y2="16.65" />
    </svg>
  );
}

export function IconCheck({ style }: { style?: React.CSSProperties }) {
  return (
    <svg viewBox="0 0 24 24" style={{ ...iconBase, width: '14px', height: '14px', ...style }}>
      <polyline points="20 6 9 17 4 12" />
    </svg>
  );
}

export function IconChevronRight({ style }: { style?: React.CSSProperties }) {
  return (
    <svg viewBox="0 0 24 24" style={{ ...iconBase, width: '14px', height: '14px', ...style }}>
      <polyline points="9 18 15 12 9 6" />
    </svg>
  );
}

export function IconFilter({ style }: { style?: React.CSSProperties }) {
  return (
    <svg viewBox="0 0 24 24" style={{ ...iconBase, width: '14px', height: '14px', ...style }}>
      <line x1="4" y1="21" x2="4" y2="14" />
      <line x1="4" y1="10" x2="4" y2="3" />
      <line x1="12" y1="21" x2="12" y2="12" />
      <line x1="12" y1="8" x2="12" y2="3" />
      <line x1="20" y1="21" x2="20" y2="16" />
      <line x1="20" y1="12" x2="20" y2="3" />
      <line x1="1" y1="14" x2="7" y2="14" />
      <line x1="9" y1="8" x2="15" y2="8" />
      <line x1="17" y1="16" x2="23" y2="16" />
    </svg>
  );
}

